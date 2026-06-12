package migrate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

func isSystemInstanceTypeItem(item SelectedItem) bool {
	raw := strings.TrimSpace(item.RawJSON)
	if raw == "" {
		return false
	}
	return morpheus.IsSeededSystemLibraryItem(json.RawMessage(raw))
}

func isSystemInstanceTypeObject(obj map[string]interface{}) bool {
	if obj == nil {
		return false
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return false
	}
	return morpheus.IsSeededSystemLibraryItem(b)
}

func catalogInstanceTypeIsSystem(src *morpheus.Client, state *automationState, code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	if state != nil && state.sourceSnap != nil {
		if it, ok := state.sourceSnap.FindInstanceTypeByCode(code); ok {
			return isSystemInstanceTypeItem(it)
		}
	}
	if src == nil {
		return false
	}
	it, err := findSourceInstanceTypeByCodeLive(src, code)
	if err != nil {
		return false
	}
	return isSystemInstanceTypeItem(it)
}

func migrateInstanceTypeWithAutomation(src, dst *morpheus.Client, item SelectedItem, state *automationState) ItemResult {
	name := strings.TrimSpace(item.Name)
	if isSystemInstanceTypeItem(item) {
		return ItemResult{
			Name: name, Type: "instanceType", Status: "skipped",
			Message: "System instance types are built-in and cannot be migrated",
		}
	}
	if src == nil {
		return ItemResult{
			Name: name, Type: "instanceType", Status: "error",
			Message: "source appliance is required to read full instance type details",
		}
	}

	var itObj map[string]interface{}
	var err error
	if state != nil && state.sourceSnap != nil {
		itObj, err = state.sourceSnap.InstanceTypeObject(src, item)
	} else {
		itObj, err = fetchFullInstanceType(src, item.ID)
	}
	if err != nil {
		return ItemResult{
			Name: name, Type: "instanceType", Status: "error",
			Message: fmt.Sprintf("fetch instance type from source: %v", err),
		}
	}
	if n := strings.TrimSpace(stringFromAny(itObj["name"])); n != "" {
		name = n
	}
	if isSystemInstanceTypeObject(itObj) {
		return ItemResult{
			Name: name, Type: "instanceType", Status: "skipped",
			Message: "System instance types are built-in and cannot be migrated",
		}
	}
	code := strings.TrimSpace(stringFromAny(itObj["code"]))
	if code == "" {
		return ItemResult{Name: name, Type: "instanceType", Status: "error", Message: "instance type code is required"}
	}

	itOptIDs, itWarn, dep := ensureOptionTypeIDs(src, dst, itObj["optionTypes"], state, name)
	if dep != nil {
		dep.Type = "instanceType"
		if dep.Name == "" {
			dep.Name = name
		}
		return *dep
	}

	destITID, existed, err := findDestInstanceTypeID(dst, code)
	if err != nil {
		return ItemResult{Name: name, Type: "instanceType", Status: "error", Message: err.Error()}
	}

	itPayload, err := buildInstanceTypeWritePayload(itObj, itOptIDs)
	if err != nil {
		return ItemResult{Name: name, Type: "instanceType", Status: "error", Message: err.Error()}
	}

	if destITID > 0 {
		_, err = dst.PutRaw(fmt.Sprintf("/api/library/instance-types/%d", destITID), itPayload)
	} else {
		body, postErr := dst.PostRaw("/api/library/instance-types", itPayload)
		err = postErr
		if err == nil {
			destITID = parseWrappedID(body, "instanceType")
			if destITID <= 0 {
				destITID, _, _ = findDestInstanceTypeID(dst, code)
			}
		}
	}
	if err != nil {
		if isDuplicateErr(err) {
			destITID, _, _ = findDestInstanceTypeID(dst, code)
			if destITID <= 0 {
				return ItemResult{Name: name, Type: "instanceType", Status: "error", Message: err.Error()}
			}
			_, err = dst.PutRaw(fmt.Sprintf("/api/library/instance-types/%d", destITID), itPayload)
		}
		if err != nil {
			return ItemResult{Name: name, Type: "instanceType", Status: "error", Message: err.Error()}
		}
		existed = true
	}
	if destITID <= 0 {
		return ItemResult{Name: name, Type: "instanceType", Status: "error", Message: "could not resolve destination instance type id"}
	}

	layouts, _ := itObj["instanceTypeLayouts"].([]interface{})
	var layoutMsgs []string
	layoutOK := 0
	for _, le := range layouts {
		layout, ok := le.(map[string]interface{})
		if !ok {
			continue
		}
		layoutName := stringFromAny(layout["name"])
		res := migrateInstanceTypeLayout(src, dst, destITID, layout, state)
		switch res.Status {
		case "success":
			layoutOK++
		case "blocked":
			return ItemResult{
				Name: name, Type: "instanceType", Status: "blocked",
				Message: fmt.Sprintf("layout %q: %s", layoutName, res.Message),
			}
		default:
			layoutMsgs = append(layoutMsgs, fmt.Sprintf("%s: %s", layoutName, res.Message))
		}
	}

	outcome := "created"
	msg := "Created instance type on destination"
	if existed {
		outcome = "updated"
		msg = "Updated instance type on destination"
	}
	if itWarn != "" {
		msg += "; " + itWarn
	}
	if len(layoutMsgs) > 0 {
		return ItemResult{
			Name: name, Type: "instanceType", Status: "partial", Outcome: outcome,
			Message: msg + "; layout issues: " + strings.Join(layoutMsgs, "; "),
		}
	}
	if len(layouts) > 0 {
		msg += fmt.Sprintf(" (%d layout(s) synced)", layoutOK)
	}
	return ItemResult{Name: name, Type: "instanceType", Status: "success", Outcome: outcome, Message: msg}
}

func migrateInstanceTypeLayout(src, dst *morpheus.Client, destITID int64, layout map[string]interface{}, state *automationState) ItemResult {
	layoutName := strings.TrimSpace(stringFromAny(layout["name"]))
	layoutVersion := strings.TrimSpace(stringFromAny(layout["instanceVersion"]))

	layoutOptIDs, _, dep := ensureOptionTypeIDs(src, dst, layout["optionTypes"], state, layoutName)
	if dep != nil {
		dep.Type = "layout"
		if dep.Name == "" {
			dep.Name = layoutName
		}
		return *dep
	}

	var destContainerIDs []int64
	containers, _ := layout["containerTypes"].([]interface{})
	for _, ce := range containers {
		ct, ok := ce.(map[string]interface{})
		if !ok {
			continue
		}
		destID, blocked := ensureNodeTypeOnDestination(dst, ct)
		if blocked != nil {
			blocked.Type = "layout"
			if blocked.Name == "" {
				blocked.Name = layoutName
			}
			return *blocked
		}
		if destID <= 0 {
			return ItemResult{
				Name: layoutName, Type: "layout", Status: "error",
				Message: fmt.Sprintf("could not resolve node type %q on destination", stringFromAny(ct["name"])),
			}
		}
		destContainerIDs = append(destContainerIDs, destID)
	}

	var destWorkflowIDs []int64
	if arr, ok := layout["taskSets"].([]interface{}); ok {
		for _, we := range arr {
			ref, ok := we.(map[string]interface{})
			if !ok {
				continue
			}
			wfID, blocked := ensureWorkflowFromRef(src, dst, ref, state)
			if blocked != nil {
				blocked.Type = "layout"
				if blocked.Name == "" {
					blocked.Name = layoutName
				}
				return *blocked
			}
			if wfID > 0 {
				destWorkflowIDs = append(destWorkflowIDs, wfID)
			}
		}
	}

	layoutPayload, err := buildInstanceTypeLayoutWritePayload(layout, destContainerIDs, layoutOptIDs, destWorkflowIDs)
	if err != nil {
		return ItemResult{Name: layoutName, Type: "layout", Status: "error", Message: err.Error()}
	}

	destLayoutID, err := findDestLayoutID(dst, destITID, layoutName, layoutVersion, stringFromAny(layout["code"]))
	if err != nil {
		return ItemResult{Name: layoutName, Type: "layout", Status: "error", Message: err.Error()}
	}

	if destLayoutID > 0 {
		_, err = dst.PutRaw(fmt.Sprintf("/api/library/layouts/%d", destLayoutID), layoutPayload)
		if err != nil {
			return ItemResult{Name: layoutName, Type: "layout", Status: "error", Message: err.Error()}
		}
		return ItemResult{Name: layoutName, Type: "layout", Status: "success", Outcome: "updated", Message: "Updated layout on destination"}
	}

	body, err := dst.PostRaw(fmt.Sprintf("/api/library/instance-types/%d/layouts", destITID), layoutPayload)
	if err != nil {
		if isDuplicateErr(err) {
			destLayoutID, findErr := findDestLayoutID(dst, destITID, layoutName, layoutVersion, stringFromAny(layout["code"]))
			if findErr == nil && destLayoutID > 0 {
				_, err = dst.PutRaw(fmt.Sprintf("/api/library/layouts/%d", destLayoutID), layoutPayload)
				if err == nil {
					return ItemResult{Name: layoutName, Type: "layout", Status: "success", Outcome: "updated", Message: "Updated layout on destination"}
				}
			}
		}
		return ItemResult{Name: layoutName, Type: "layout", Status: "error", Message: err.Error()}
	}
	if parseWrappedID(body, "instanceTypeLayout") > 0 {
		return ItemResult{Name: layoutName, Type: "layout", Status: "success", Outcome: "created", Message: "Created layout on destination"}
	}
	return ItemResult{Name: layoutName, Type: "layout", Status: "success", Outcome: "created", Message: "Created layout on destination"}
}

// FetchFullInstanceType loads a complete instance type record from the source appliance.
func FetchFullInstanceType(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	return fetchFullInstanceType(src, id)
}

func fetchFullInstanceType(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid instance type id")
	}
	paths := []string{
		fmt.Sprintf("/api/library/instance-types/%d", id),
		fmt.Sprintf("/api/instance-types/%d", id),
	}
	for _, path := range paths {
		body, err := src.GetRaw(path)
		if err != nil {
			continue
		}
		var wrap map[string]json.RawMessage
		if err := json.Unmarshal(body, &wrap); err != nil {
			continue
		}
		raw, ok := wrap["instanceType"]
		if !ok {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		return obj, nil
	}
	return nil, fmt.Errorf("could not load instance type %d from source", id)
}

func buildInstanceTypeWritePayload(obj map[string]interface{}, optionTypeIDs []interface{}) ([]byte, error) {
	clone := cloneMap(obj)
	for _, k := range []string{
		"id", "dateCreated", "lastUpdated", "account", "accountId",
		"instanceTypeLayouts", "layouts", "imagePath", "darkImagePath",
	} {
		delete(clone, k)
	}
	if len(optionTypeIDs) > 0 {
		clone["optionTypes"] = optionTypeIDs
	} else {
		delete(clone, "optionTypes")
	}
	return json.Marshal(map[string]interface{}{"instanceType": clone})
}

func buildInstanceTypeLayoutWritePayload(layout map[string]interface{}, containerTypeIDs []int64, optionTypeIDs []interface{}, taskSetIDs []int64) ([]byte, error) {
	out := map[string]interface{}{
		"name":                       layout["name"],
		"labels":                     layout["labels"],
		"instanceVersion":            layout["instanceVersion"],
		"description":                layout["description"],
		"sortOrder":                  layout["sortOrder"],
		"creatable":                  layout["creatable"],
		"supportsConvertToManaged":   layout["supportsConvertToManaged"],
		"provisionTypeCode":          provisionTypeCodeFrom(layout),
		"memoryRequirement":          formatMemoryRequirement(layout["memoryRequirement"]),
	}
	if out["provisionTypeCode"] == nil || strings.TrimSpace(stringFromAny(out["provisionTypeCode"])) == "" {
		return nil, fmt.Errorf("layout %q is missing provisionTypeCode", stringFromAny(layout["name"]))
	}

	if len(containerTypeIDs) > 0 {
		ids := make([]interface{}, len(containerTypeIDs))
		for i, id := range containerTypeIDs {
			ids[i] = id
		}
		out["containerTypes"] = ids
	}
	if len(optionTypeIDs) > 0 {
		out["optionTypes"] = optionTypeIDs
	}
	if len(taskSetIDs) > 0 {
		ids := make([]interface{}, len(taskSetIDs))
		for i, id := range taskSetIDs {
			ids[i] = id
		}
		out["taskSets"] = ids
	}
	if code := strings.TrimSpace(stringFromAny(layout["code"])); code != "" {
		out["code"] = code
	}

	for k, v := range out {
		if v == nil {
			delete(out, k)
		}
	}
	return json.Marshal(map[string]interface{}{"instanceTypeLayout": out})
}

func provisionTypeCodeFrom(obj map[string]interface{}) string {
	if pt, ok := obj["provisionType"].(map[string]interface{}); ok && pt != nil {
		if c := strings.TrimSpace(stringFromAny(pt["code"])); c != "" {
			return c
		}
	}
	return strings.TrimSpace(stringFromAny(obj["provisionTypeCode"]))
}

func formatMemoryRequirement(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case int:
		return fmt.Sprintf("%d", t)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(v)
	}
}

func ensureOptionTypeIDs(src, dst *morpheus.Client, opt interface{}, state *automationState, parentName string) ([]interface{}, string, *ItemResult) {
	arr, ok := opt.([]interface{})
	if !ok || len(arr) == 0 {
		return []interface{}{}, "", nil
	}

	state.reloadDestOptionTypes(dst)
	for _, e := range arr {
		om, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		code := strings.TrimSpace(stringFromAny(om["code"]))
		if code == "" {
			code = strings.TrimSpace(stringFromAny(om["fieldName"]))
		}
		if code == "" {
			code = strings.TrimSpace(stringFromAny(om["name"]))
		}
		if code == "" {
			continue
		}
		state.reportStep(fmt.Sprintf("Checking if input %q exists on destination", code))
		if state.destOptionTypeID(code) > 0 {
			continue
		}
		state.reportStep(fmt.Sprintf("Creating input %q on destination", code))
		id, err := createOptionTypeOnDestination(src, dst, om, code)
		if err != nil {
			return nil, "", &ItemResult{
				Name: parentName, Status: "blocked",
				Message: fmt.Sprintf("could not create input %q on destination: %v", code, err),
			}
		}
		state.setDestOptionTypeID(code, id)
	}
	state.reloadDestOptionTypes(dst)
	ids, warn := mapWorkflowOptionTypes(opt, state.destOptionTypeMapCopy())
	return ids, warn, nil
}

func ensureWorkflowFromRef(src, dst *morpheus.Client, ref map[string]interface{}, state *automationState) (int64, *ItemResult) {
	wfName := strings.TrimSpace(stringFromAny(ref["name"]))
	wfID := intFromAny(ref["id"])

	if err := state.refreshDestWorkflows(dst); err != nil {
		return 0, &ItemResult{Status: "error", Message: fmt.Sprintf("list destination workflows: %v", err)}
	}
	if wfName != "" {
		if id := state.destWorkflowIDByName(wfName); id > 0 {
			return id, nil
		}
	}

	if src == nil || wfID <= 0 {
		return 0, &ItemResult{
			Status: "blocked",
			Message: fmt.Sprintf("workflow %q is not on destination and could not be loaded from source", wfName),
		}
	}

	var dep SelectedItem
	var err error
	if state != nil && state.sourceSnap != nil {
		dep, err = state.sourceSnap.ResolveSourceItem(src, "workflow", wfID)
	} else {
		dep, err = fetchSourceByIDLive(src, "workflow", wfID)
	}
	if err != nil {
		return 0, &ItemResult{Status: "blocked", Message: fmt.Sprintf("workflow %q: %v", wfName, err)}
	}
	res := migrateWorkflowWithAutomation(src, dst, dep, state)
	if res.Status != "success" && res.Status != "skipped" {
		return 0, &res
	}
	if err := state.refreshDestWorkflows(dst); err != nil {
		return 0, &ItemResult{Status: "error", Message: err.Error()}
	}
	if wfName != "" {
		if id := state.destWorkflowIDByName(wfName); id > 0 {
			return id, nil
		}
	}
	return 0, &ItemResult{Status: "blocked", Message: fmt.Sprintf("workflow %q migrated but not found on destination", wfName)}
}

func ensureNodeTypeOnDestination(dst *morpheus.Client, ct map[string]interface{}) (int64, *ItemResult) {
	name := strings.TrimSpace(stringFromAny(ct["name"]))
	shortName := strings.TrimSpace(stringFromAny(ct["shortName"]))
	code := strings.TrimSpace(stringFromAny(ct["code"]))

	destID, err := findDestContainerTypeID(dst, name, shortName, code)
	if err != nil {
		return 0, &ItemResult{Status: "error", Message: err.Error()}
	}

	payload, err := buildContainerTypeWritePayload(dst, ct)
	if err != nil {
		status := "error"
		if strings.Contains(strings.ToLower(err.Error()), "virtual image") {
			status = "blocked"
		}
		return 0, &ItemResult{Name: name, Type: "nodeType", Status: status, Message: err.Error()}
	}

	if destID > 0 {
		// Reuse an existing node type by name/code. Morpheus often returns 403 when updating
		// provision-linked or system container types ("Unable to modify container type").
		return destID, nil
	}

	body, err := dst.PostRaw("/api/library/container-types", payload)
	if err != nil {
		if isDuplicateErr(err) {
			destID, findErr := findDestContainerTypeID(dst, name, shortName, code)
			if findErr == nil && destID > 0 {
				return destID, nil
			}
		}
		return 0, &ItemResult{Name: name, Type: "nodeType", Status: "error", Message: err.Error()}
	}
	if id := parseWrappedID(body, "containerType"); id > 0 {
		return id, nil
	}
	destID, err = findDestContainerTypeID(dst, name, shortName, code)
	if err != nil || destID <= 0 {
		return 0, &ItemResult{Name: name, Type: "nodeType", Status: "error", Message: "created node type but could not resolve destination id"}
	}
	return destID, nil
}

func buildContainerTypeWritePayload(dst *morpheus.Client, ct map[string]interface{}) ([]byte, error) {
	name := strings.TrimSpace(stringFromAny(ct["name"]))
	out := map[string]interface{}{
		"name":              ct["name"],
		"labels":            ct["labels"],
		"shortName":         ct["shortName"],
		"description":       ct["description"],
		"containerVersion":  ct["containerVersion"],
		"provisionTypeCode": provisionTypeCodeFrom(ct),
	}
	serverType := strings.TrimSpace(stringFromAny(ct["serverType"]))
	if serverType == "" {
		serverType = "vm"
	}
	out["serverType"] = serverType

	viName := ""
	if vi, ok := ct["virtualImage"].(map[string]interface{}); ok && vi != nil {
		viName = strings.TrimSpace(stringFromAny(vi["name"]))
	}
	if viName == "" {
		viName = strings.TrimSpace(stringFromAny(ct["virtualImageName"]))
	}
	if viName != "" {
		dstID, err := findDestinationVirtualImageIDByName(dst, viName)
		if err != nil {
			return nil, err
		}
		if dstID <= 0 {
			return nil, fmt.Errorf("node type %q cannot be created: virtual image %q was not found on destination", name, viName)
		}
		out["virtualImageId"] = dstID
	}

	for k, v := range out {
		if v == nil {
			delete(out, k)
		}
	}
	return json.Marshal(map[string]interface{}{"containerType": out})
}

func findDestInstanceTypeID(dst *morpheus.Client, code string) (int64, bool, error) {
	want := strings.ToLower(strings.TrimSpace(code))
	if want == "" {
		return 0, false, nil
	}
	raws, err := paginateList(dst, "/api/library/instance-types", "instanceTypes")
	if err != nil {
		raws, err = paginateList(dst, "/api/instance-types", "instanceTypes")
		if err != nil {
			return 0, false, fmt.Errorf("list destination instance types: %v", err)
		}
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))) == want {
			return intFromAny(row["id"]), true, nil
		}
	}
	return 0, false, nil
}

func findDestContainerTypeID(dst *morpheus.Client, name, shortName, code string) (int64, error) {
	raws, err := paginateList(dst, "/api/library/container-types", "containerTypes")
	if err != nil {
		return 0, err
	}
	wantName := strings.ToLower(strings.TrimSpace(name))
	wantShort := strings.ToLower(strings.TrimSpace(shortName))
	wantCode := strings.ToLower(strings.TrimSpace(code))
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if wantCode != "" && strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))) == wantCode {
			return intFromAny(row["id"]), nil
		}
		if wantShort != "" && strings.ToLower(strings.TrimSpace(stringFromAny(row["shortName"]))) == wantShort {
			return intFromAny(row["id"]), nil
		}
		if wantName != "" && strings.ToLower(strings.TrimSpace(stringFromAny(row["name"]))) == wantName {
			return intFromAny(row["id"]), nil
		}
	}
	return 0, nil
}

func findDestLayoutID(dst *morpheus.Client, instanceTypeID int64, name, version, layoutCode string) (int64, error) {
	body, err := dst.GetRaw(fmt.Sprintf("/api/library/instance-types/%d", instanceTypeID))
	if err != nil {
		body, err = dst.GetRaw(fmt.Sprintf("/api/instance-types/%d", instanceTypeID))
		if err != nil {
			return 0, err
		}
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return 0, err
	}
	raw, ok := wrap["instanceType"]
	if !ok {
		return 0, nil
	}
	var it map[string]interface{}
	if err := json.Unmarshal(raw, &it); err != nil {
		return 0, err
	}
	arr, _ := it["instanceTypeLayouts"].([]interface{})
	wantName := strings.ToLower(strings.TrimSpace(name))
	wantVersion := strings.TrimSpace(version)
	wantCode := strings.ToLower(strings.TrimSpace(layoutCode))
	for _, le := range arr {
		layout, ok := le.(map[string]interface{})
		if !ok {
			continue
		}
		if wantCode != "" && strings.ToLower(strings.TrimSpace(stringFromAny(layout["code"]))) == wantCode {
			return intFromAny(layout["id"]), nil
		}
		if wantName != "" &&
			strings.ToLower(strings.TrimSpace(stringFromAny(layout["name"]))) == wantName &&
			strings.TrimSpace(stringFromAny(layout["instanceVersion"])) == wantVersion {
			return intFromAny(layout["id"]), nil
		}
	}
	if wantVersion != "" {
		for _, le := range arr {
			layout, ok := le.(map[string]interface{})
			if !ok {
				continue
			}
			if strings.TrimSpace(stringFromAny(layout["instanceVersion"])) == wantVersion {
				return intFromAny(layout["id"]), nil
			}
		}
	}
	return 0, nil
}

func parseWrappedID(body []byte, wrapperKey string) int64 {
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return 0
	}
	raw, ok := wrap[wrapperKey]
	if !ok {
		return 0
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0
	}
	return intFromAny(obj["id"])
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func filterItemsForInstanceTypeOrchestration(items []SelectedItem) []SelectedItem {
	selectedIT := map[int64]struct{}{}
	for _, it := range items {
		if normalizeType(it.Type) == "instanceType" && it.ID > 0 {
			selectedIT[it.ID] = struct{}{}
		}
	}
	if len(selectedIT) == 0 {
		return items
	}

	ownedLayouts := map[int64]struct{}{}
	ownedNodeTypes := map[int64]struct{}{}
	ownedWorkflows := map[int64]struct{}{}

	for _, it := range items {
		if normalizeType(it.Type) != "instanceType" {
			continue
		}
		if _, ok := selectedIT[it.ID]; !ok {
			continue
		}
		for _, lid := range extractInstanceTypeLayoutIDs(parseObject(it.RawJSON)) {
			ownedLayouts[lid] = struct{}{}
		}
	}
	for _, it := range items {
		if normalizeType(it.Type) != "layout" {
			continue
		}
		if _, ok := ownedLayouts[it.ID]; !ok {
			continue
		}
		nodeIDs, wfIDs := extractLayoutDeps(parseObject(it.RawJSON))
		for _, nid := range nodeIDs {
			ownedNodeTypes[nid] = struct{}{}
		}
		for _, wid := range wfIDs {
			ownedWorkflows[wid] = struct{}{}
		}
	}

	out := make([]SelectedItem, 0, len(items))
	for _, it := range items {
		switch normalizeType(it.Type) {
		case "layout":
			if _, skip := ownedLayouts[it.ID]; skip {
				continue
			}
		case "nodeType":
			if _, skip := ownedNodeTypes[it.ID]; skip {
				continue
			}
		case "workflow":
			if _, skip := ownedWorkflows[it.ID]; skip {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}
