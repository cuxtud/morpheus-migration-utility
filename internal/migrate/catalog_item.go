package migrate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

func migrateCatalogItem(src, dst *morpheus.Client, item SelectedItem, state *automationState) ItemResult {
	name := strings.TrimSpace(item.Name)
	if src == nil {
		return ItemResult{
			Name: name, Type: "catalogItem", Status: "error",
			Message: "source appliance is required to read full catalog item details",
		}
	}

	obj, err := fetchFullCatalogItem(src, item.ID)
	if err != nil {
		return ItemResult{Name: name, Type: "catalogItem", Status: "error", Message: err.Error()}
	}
	if n := strings.TrimSpace(stringFromAny(obj["name"])); n != "" {
		name = n
	}
	code := strings.TrimSpace(stringFromAny(obj["code"]))
	if code == "" {
		code = strings.TrimSpace(name)
	}

	catType := strings.TrimSpace(stringFromAny(obj["type"]))
	if catType == "" {
		catType = "instance"
	}

	optIDs, _, dep := ensureOptionTypeIDs(src, dst, obj["optionTypes"], state, name)
	if dep != nil {
		dep.Type = "catalogItem"
		if dep.Name == "" {
			dep.Name = name
		}
		return *dep
	}

	if catType == "workflow" {
		if wf, ok := obj["workflow"].(map[string]interface{}); ok && wf != nil {
			wfID, blocked := ensureWorkflowFromRef(src, dst, wf, state)
			if blocked != nil {
				blocked.Type = "catalogItem"
				if blocked.Name == "" {
					blocked.Name = name
				}
				return *blocked
			}
			if wfID > 0 {
				obj["workflow"] = map[string]interface{}{"id": wfID}
			}
		}
	}

	formNotes, blocked := ensureCatalogFormRefs(src, dst, obj, state, name)
	if blocked != nil {
		blocked.Type = "catalogItem"
		if blocked.Name == "" {
			blocked.Name = name
		}
		return *blocked
	}

	var remapNotes []string
	if len(formNotes) > 0 {
		remapNotes = append(remapNotes, formNotes...)
	}
	if cfg, ok := obj["config"].(map[string]interface{}); ok && cfg != nil && catType == "instance" {
		instanceTypeCode := strings.TrimSpace(stringFromAny(cfg["type"]))
		if err := requireDestInstanceType(dst, instanceTypeCode); err != nil {
			return ItemResult{Name: name, Type: "catalogItem", Status: "blocked", Message: err.Error()}
		}
		notes, err := remapCatalogInstanceConfig(src, dst, cfg)
		if err != nil {
			status := "error"
			if strings.Contains(strings.ToLower(err.Error()), "not found") ||
				strings.Contains(strings.ToLower(err.Error()), "must be migrated") {
				status = "blocked"
			}
			return ItemResult{Name: name, Type: "catalogItem", Status: status, Message: err.Error()}
		}
		remapNotes = append(remapNotes, notes...)
		obj["config"] = cfg
		specBytes, err := json.MarshalIndent(cfg, "", " ")
		if err == nil {
			obj["instanceSpec"] = string(specBytes)
		}
	}

	payload, err := buildCatalogItemWritePayload(obj, optIDs)
	if err != nil {
		return ItemResult{Name: name, Type: "catalogItem", Status: "error", Message: err.Error()}
	}

	destID, err := findDestCatalogItemIDByCode(dst, code)
	if err != nil {
		return ItemResult{Name: name, Type: "catalogItem", Status: "error", Message: err.Error()}
	}

	if destID > 0 {
		_, err = dst.PutRaw(fmt.Sprintf("/api/catalog-item-types/%d", destID), payload)
		if err != nil {
			return ItemResult{Name: name, Type: "catalogItem", Status: "error", Message: err.Error()}
		}
		return catalogItemResult(name, "updated", remapNotes)
	}

	_, err = dst.PostRaw("/api/catalog-item-types", payload)
	if err != nil {
		if isDuplicateErr(err) {
			destID, findErr := findDestCatalogItemIDByCode(dst, code)
			if findErr == nil && destID > 0 {
				_, err = dst.PutRaw(fmt.Sprintf("/api/catalog-item-types/%d", destID), payload)
				if err == nil {
					return catalogItemResult(name, "updated", remapNotes)
				}
			}
		}
		return ItemResult{Name: name, Type: "catalogItem", Status: "error", Message: err.Error()}
	}
	return catalogItemResult(name, "created", remapNotes)
}

func ensureCatalogFormRefs(src, dst *morpheus.Client, obj map[string]interface{}, state *automationState, catalogName string) ([]string, *ItemResult) {
	var notes []string
	remap := func(field string, ref map[string]interface{}) *ItemResult {
		formID, blocked := ensureFormFromRef(src, dst, ref, state)
		if blocked != nil {
			if blocked.Name == "" {
				blocked.Name = catalogName
			}
			return blocked
		}
		if formID > 0 {
			obj[field] = map[string]interface{}{"id": formID}
			if n := strings.TrimSpace(stringFromAny(ref["name"])); n != "" {
				notes = append(notes, fmt.Sprintf("Form: created or mapped %q (#%d) on destination", n, formID))
			} else {
				notes = append(notes, fmt.Sprintf("Form: mapped to destination form #%d", formID))
			}
		}
		return nil
	}

	if f, ok := obj["form"].(map[string]interface{}); ok && f != nil {
		if blocked := remap("form", f); blocked != nil {
			return notes, blocked
		}
	}
	if f, ok := obj["optionTypeForm"].(map[string]interface{}); ok && f != nil {
		if blocked := remap("optionTypeForm", f); blocked != nil {
			return notes, blocked
		}
	}
	if arr, ok := obj["optionTypeForms"].([]interface{}); ok {
		for i, e := range arr {
			ref, ok := e.(map[string]interface{})
			if !ok || ref == nil {
				continue
			}
			formID, blocked := ensureFormFromRef(src, dst, ref, state)
			if blocked != nil {
				if blocked.Name == "" {
					blocked.Name = catalogName
				}
				return notes, blocked
			}
			if formID > 0 {
				arr[i] = map[string]interface{}{"id": formID}
			}
		}
		obj["optionTypeForms"] = arr
	}
	return notes, nil
}

func catalogItemResult(name, outcome string, notes []string) ItemResult {
	verb := "Created"
	if outcome == "updated" {
		verb = "Updated"
	}
	msg := fmt.Sprintf("%s catalog item on destination", verb)
	if len(notes) > 0 {
		msg += ". " + strings.Join(notes, " ")
	}
	return ItemResult{Name: name, Type: "catalogItem", Status: "success", Outcome: outcome, Message: msg}
}

func fetchFullCatalogItem(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid catalog item id")
	}
	body, err := src.GetRaw(fmt.Sprintf("/api/catalog-item-types/%d", id))
	if err != nil {
		return nil, fmt.Errorf("fetch catalog item from source: %v", err)
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	raw, ok := wrap["catalogItemType"]
	if !ok {
		return nil, fmt.Errorf("source response missing catalogItemType")
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func buildCatalogItemWritePayload(obj map[string]interface{}, optionTypeIDs []interface{}) ([]byte, error) {
	clone := cloneMap(obj)
	for _, k := range []string{
		"id", "dateCreated", "lastUpdated", "account", "owner",
		"contentFormatted", "layoutCode",
	} {
		delete(clone, k)
	}
	if len(optionTypeIDs) > 0 {
		clone["optionTypes"] = optionTypeIDs
	} else {
		delete(clone, "optionTypes")
	}
	return json.Marshal(map[string]interface{}{"catalogItemType": clone})
}

func remapCatalogInstanceConfig(src, dst *morpheus.Client, cfg map[string]interface{}) ([]string, error) {
	instanceTypeCode := strings.TrimSpace(stringFromAny(cfg["type"]))
	var notes []string

	if g, ok := cfg["group"].(map[string]interface{}); ok && g != nil {
		ref, note, err := resolveCatalogGroupRef(dst, g)
		if err != nil {
			return notes, err
		}
		if ref != nil {
			cfg["group"] = ref
		} else {
			delete(cfg, "group")
		}
		if note != "" {
			notes = append(notes, note)
		}
	}

	if c, ok := cfg["cloud"].(map[string]interface{}); ok && c != nil {
		ref, note, err := resolveCatalogCloudRef(dst, c)
		if err != nil {
			return notes, err
		}
		if ref != nil {
			cfg["cloud"] = ref
		} else {
			delete(cfg, "cloud")
		}
		if note != "" {
			notes = append(notes, note)
		}
	}

	if l, ok := cfg["layout"].(map[string]interface{}); ok && l != nil {
		layoutCode := strings.TrimSpace(stringFromAny(l["code"]))
		layoutName := catalogLayoutNameFromSource(src, l)
		version := strings.TrimSpace(stringFromAny(cfg["version"]))
		destLayoutID, destLayoutCode, err := findDestCatalogLayoutRef(dst, instanceTypeCode, layoutCode, layoutName, version)
		if err != nil {
			return notes, err
		}
		if destLayoutID <= 0 {
			return notes, fmt.Errorf("layout reference %q not found on destination instance type %q — migrate the instance type (and its layouts) first", layoutCode, instanceTypeCode)
		}
		ref := map[string]interface{}{"id": destLayoutID}
		if destLayoutCode != "" {
			ref["code"] = destLayoutCode
		}
		cfg["layout"] = ref
	}

	if p, ok := cfg["plan"].(map[string]interface{}); ok && p != nil {
		ref, note, err := resolveCatalogPlanRef(dst, p)
		if err != nil {
			return notes, err
		}
		if ref != nil {
			cfg["plan"] = ref
		} else {
			delete(cfg, "plan")
		}
		if note != "" {
			notes = append(notes, note)
		}
	}

	cloudID := intFromAny(objectID(cfg["cloud"]))
	if arr, ok := cfg["networkInterfaces"].([]interface{}); ok {
		for i, e := range arr {
			nm, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			net, ok := nm["network"].(map[string]interface{})
			if !ok || net == nil {
				continue
			}
			idName := strings.TrimSpace(stringFromAny(net["idName"]))
			if idName == "" {
				idName = strings.TrimSpace(stringFromAny(net["name"]))
			}
			if idName == "" {
				continue
			}
			destNet, note, err := resolveCatalogNetworkRef(dst, cloudID, idName)
			if err != nil {
				return notes, err
			}
			if destNet != nil {
				nm["network"] = destNet
			} else {
				delete(nm, "network")
			}
			if note != "" {
				notes = append(notes, note)
			}
			arr[i] = nm
		}
		cfg["networkInterfaces"] = arr
	}

	if arr, ok := cfg["volumes"].([]interface{}); ok {
		for i, e := range arr {
			vm, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			viName := virtualImageNameFromVolume(vm)
			if viName != "" {
				dstVI, err := findDestinationVirtualImageIDByName(dst, viName)
				if err != nil {
					return notes, err
				}
				if dstVI <= 0 {
					return notes, fmt.Errorf("virtual image %q not found on destination (catalog volume)", viName)
				}
				vm["virtualImageId"] = dstVI
			}
			stripCatalogVolumeSourceFields(vm)
			arr[i] = vm
		}
		cfg["volumes"] = arr
	}

	return notes, nil
}

func resolveCatalogGroupRef(dst *morpheus.Client, sourceRef map[string]interface{}) (map[string]interface{}, string, error) {
	sourceName := strings.TrimSpace(stringFromAny(sourceRef["name"]))
	sourceID := strings.TrimSpace(stringFromAny(sourceRef["id"]))

	if sourceName != "" {
		gid, err := findDestGroupIDByName(dst, sourceName)
		if err != nil {
			return nil, "", err
		}
		if gid != "" {
			return map[string]interface{}{"id": gid, "name": sourceName}, "", nil
		}
	}

	if sourceID != "" {
		if gid, gname, ok, err := lookupDestGroupByID(dst, sourceID); err != nil {
			return nil, "", err
		} else if ok {
			note := fmt.Sprintf("Group: using destination group %q (#%s)", gname, gid)
			if sourceName != "" {
				note += fmt.Sprintf(" (source group %q not found by name)", sourceName)
			}
			return map[string]interface{}{"id": gid, "name": gname}, note, nil
		}
	}

	if gid, gname, ok, err := firstDestGroup(dst); err != nil {
		return nil, "", err
	} else if ok {
		note := fmt.Sprintf("Group: using destination group %q (#%s)", gname, gid)
		if sourceName != "" {
			note += fmt.Sprintf(" (source group %q not found)", sourceName)
		} else if sourceID != "" {
			note += fmt.Sprintf(" (source group id %s not found)", sourceID)
		}
		return map[string]interface{}{"id": gid, "name": gname}, note, nil
	}

	note := "Group: omitted — no groups on destination"
	if sourceName != "" {
		note = fmt.Sprintf("Group: omitted — source group %q not found and no groups on destination", sourceName)
	}
	return nil, note, nil
}

func resolveCatalogCloudRef(dst *morpheus.Client, sourceRef map[string]interface{}) (map[string]interface{}, string, error) {
	sourceName := strings.TrimSpace(stringFromAny(sourceRef["name"]))
	sourceID := intFromAny(sourceRef["id"])

	if sourceName != "" {
		cid, err := findDestCloudIDByName(dst, sourceName)
		if err != nil {
			return nil, "", err
		}
		if cid > 0 {
			return map[string]interface{}{"id": cid, "name": sourceName}, "", nil
		}
	}

	if sourceID > 0 {
		if cid, cname, ok, err := lookupDestCloudByID(dst, sourceID); err != nil {
			return nil, "", err
		} else if ok {
			note := fmt.Sprintf("Cloud: using destination cloud %q (#%d)", cname, cid)
			if sourceName != "" {
				note += fmt.Sprintf(" (source cloud %q not found by name)", sourceName)
			}
			return map[string]interface{}{"id": cid, "name": cname}, note, nil
		}
	}

	if cid, cname, ok, err := firstDestCloud(dst); err != nil {
		return nil, "", err
	} else if ok {
		note := fmt.Sprintf("Cloud: using destination cloud %q (#%d)", cname, cid)
		if sourceName != "" {
			note += fmt.Sprintf(" (source cloud %q not found)", sourceName)
		} else if sourceID > 0 {
			note += fmt.Sprintf(" (source cloud id %d not found)", sourceID)
		}
		return map[string]interface{}{"id": cid, "name": cname}, note, nil
	}

	note := "Cloud: omitted — no clouds on destination"
	if sourceName != "" {
		note = fmt.Sprintf("Cloud: omitted — source cloud %q not found and no clouds on destination", sourceName)
	}
	return nil, note, nil
}

func lookupDestGroupByID(dst *morpheus.Client, id string) (gid, gname string, ok bool, err error) {
	want := strings.TrimSpace(id)
	if want == "" {
		return "", "", false, nil
	}
	raws, err := paginateList(dst, "/api/groups", "groups")
	if err != nil {
		return "", "", false, err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if strings.TrimSpace(stringFromAny(row["id"])) != want {
			continue
		}
		gid = stringFromAny(row["id"])
		gname = strings.TrimSpace(stringFromAny(row["name"]))
		return gid, gname, gid != "", nil
	}
	return "", "", false, nil
}

func lookupDestCloudByID(dst *morpheus.Client, id int64) (cid int64, cname string, ok bool, err error) {
	if id <= 0 {
		return 0, "", false, nil
	}
	raws, err := paginateList(dst, "/api/zones", "zones")
	if err != nil {
		return 0, "", false, err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if intFromAny(row["id"]) != id {
			continue
		}
		return id, strings.TrimSpace(stringFromAny(row["name"])), true, nil
	}
	return 0, "", false, nil
}

func firstDestGroup(dst *morpheus.Client) (gid, gname string, ok bool, err error) {
	raws, err := paginateList(dst, "/api/groups", "groups")
	if err != nil {
		return "", "", false, err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		gid = stringFromAny(row["id"])
		if gid == "" {
			continue
		}
		return gid, strings.TrimSpace(stringFromAny(row["name"])), true, nil
	}
	return "", "", false, nil
}

func firstDestCloud(dst *morpheus.Client) (cid int64, cname string, ok bool, err error) {
	raws, err := paginateList(dst, "/api/zones", "zones")
	if err != nil {
		return 0, "", false, err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		cid = intFromAny(row["id"])
		if cid <= 0 {
			continue
		}
		return cid, strings.TrimSpace(stringFromAny(row["name"])), true, nil
	}
	return 0, "", false, nil
}

func virtualImageNameFromVolume(vol map[string]interface{}) string {
	if internal := strings.TrimSpace(stringFromAny(vol["internalId"])); internal != "" {
		parts := strings.Split(internal, "/")
		last := strings.TrimSpace(parts[len(parts)-1])
		last = strings.TrimSuffix(last, ".vmdk")
		last = strings.TrimSuffix(last, ".iso")
		if last != "" {
			return last
		}
	}
	return ""
}

func stripCatalogVolumeSourceFields(vol map[string]interface{}) {
	for _, k := range []string{
		"id", "controllerId", "controllerMountPoint", "externalId", "internalId",
		"uuid", "uniqueId", "vId", "typeId", "maxStorage", "minStorage", "maxIOPS",
		"displayOrder", "configurableIOPS",
	} {
		delete(vol, k)
	}
}

func findDestCatalogItemIDByCode(dst *morpheus.Client, code string) (int64, error) {
	want := strings.ToLower(strings.TrimSpace(code))
	if want == "" {
		return 0, nil
	}
	raws, err := paginateList(dst, "/api/catalog-item-types", "catalogItemTypes")
	if err != nil {
		return 0, err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))) == want {
			return intFromAny(row["id"]), nil
		}
	}
	return 0, nil
}

func findDestCloudIDByName(dst *morpheus.Client, name string) (int64, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	raws, err := paginateList(dst, "/api/zones", "zones")
	if err != nil {
		return 0, err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(row["name"]))) == want {
			return intFromAny(row["id"]), nil
		}
	}
	return 0, nil
}

func findDestGroupIDByName(dst *morpheus.Client, name string) (string, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	raws, err := paginateList(dst, "/api/groups", "groups")
	if err != nil {
		return "", err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(row["name"]))) == want {
			id := stringFromAny(row["id"])
			if id != "" {
				return id, nil
			}
		}
	}
	return "", nil
}

func findDestServicePlanIDByCode(dst *morpheus.Client, code string) (int64, error) {
	id, _, _, err := lookupDestServicePlanByCode(dst, code)
	return id, err
}

func lookupDestServicePlanByCode(dst *morpheus.Client, code string) (id int64, pcode, pname string, err error) {
	want := strings.ToLower(strings.TrimSpace(code))
	if want == "" {
		return 0, "", "", nil
	}
	raws, err := paginateList(dst, "/api/service-plans", "servicePlans")
	if err != nil {
		return 0, "", "", err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))) != want {
			continue
		}
		return intFromAny(row["id"]), strings.TrimSpace(stringFromAny(row["code"])), strings.TrimSpace(stringFromAny(row["name"])), nil
	}
	return 0, "", "", nil
}

func lookupDestServicePlanByID(dst *morpheus.Client, id int64) (pid int64, pcode string, ok bool, err error) {
	if id <= 0 {
		return 0, "", false, nil
	}
	raws, err := paginateList(dst, "/api/service-plans", "servicePlans")
	if err != nil {
		return 0, "", false, err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if intFromAny(row["id"]) != id {
			continue
		}
		return id, strings.TrimSpace(stringFromAny(row["code"])), true, nil
	}
	return 0, "", false, nil
}

func firstDestServicePlan(dst *morpheus.Client) (id int64, code, name string, ok bool, err error) {
	raws, err := paginateList(dst, "/api/service-plans", "servicePlans")
	if err != nil {
		return 0, "", "", false, err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id = intFromAny(row["id"])
		if id <= 0 {
			continue
		}
		return id, strings.TrimSpace(stringFromAny(row["code"])), strings.TrimSpace(stringFromAny(row["name"])), true, nil
	}
	return 0, "", "", false, nil
}

func resolveCatalogPlanRef(dst *morpheus.Client, sourceRef map[string]interface{}) (map[string]interface{}, string, error) {
	sourceCode := strings.TrimSpace(stringFromAny(sourceRef["code"]))
	sourceID := intFromAny(sourceRef["id"])

	if sourceCode != "" {
		pid, pcode, _, err := lookupDestServicePlanByCode(dst, sourceCode)
		if err != nil {
			return nil, "", err
		}
		if pid > 0 {
			return map[string]interface{}{"id": pid, "code": pcode}, "", nil
		}
	}

	if sourceID > 0 {
		pid, pcode, found, err := lookupDestServicePlanByID(dst, sourceID)
		if err != nil {
			return nil, "", err
		}
		if found {
			label := pcode
			if label == "" {
				label = fmt.Sprintf("#%d", pid)
			}
			note := fmt.Sprintf("Plan: using destination plan %q (#%d)", label, pid)
			if sourceCode != "" {
				note += fmt.Sprintf(" (source plan %q not found by code)", sourceCode)
			}
			return map[string]interface{}{"id": pid, "code": pcode}, note, nil
		}
	}

	if pid, pcode, pname, found, err := firstDestServicePlan(dst); err != nil {
		return nil, "", err
	} else if found {
		label := pname
		if label == "" {
			label = pcode
		}
		if label == "" {
			label = fmt.Sprintf("#%d", pid)
		}
		note := fmt.Sprintf("Plan: using default destination plan %q (#%d)", label, pid)
		if sourceCode != "" {
			note += fmt.Sprintf(" (source plan %q not found)", sourceCode)
		}
		ref := map[string]interface{}{"id": pid}
		if pcode != "" {
			ref["code"] = pcode
		}
		return ref, note, nil
	}

	note := "Plan: omitted — no service plans on destination"
	if sourceCode != "" {
		note = fmt.Sprintf("Plan: omitted — source plan %q not found and no plans on destination", sourceCode)
	}
	return nil, note, nil
}

func findDestCatalogLayoutRef(dst *morpheus.Client, instanceTypeCode, layoutCode, layoutName, version string) (int64, string, error) {
	destITID, exists, err := findDestInstanceTypeID(dst, instanceTypeCode)
	if err != nil {
		return 0, "", err
	}
	if !exists || destITID <= 0 {
		return 0, "", fmt.Errorf("instance type %q must be migrated to destination before catalog item", instanceTypeCode)
	}

	// Layout code is the stable cross-environment reference.
	if layoutCode != "" {
		destLayoutID, err := findDestLayoutID(dst, destITID, "", "", layoutCode)
		if err != nil {
			return 0, "", err
		}
		if destLayoutID > 0 {
			code, _ := lookupDestLayoutCode(dst, destITID, destLayoutID)
			return destLayoutID, code, nil
		}
	}

	if layoutName != "" || version != "" {
		destLayoutID, err := findDestLayoutID(dst, destITID, layoutName, version, "")
		if err != nil {
			return 0, "", err
		}
		if destLayoutID > 0 {
			code, _ := lookupDestLayoutCode(dst, destITID, destLayoutID)
			return destLayoutID, code, nil
		}
	}

	return 0, "", nil
}

func lookupDestLayoutCode(dst *morpheus.Client, instanceTypeID, layoutID int64) (string, error) {
	body, err := dst.GetRaw(fmt.Sprintf("/api/library/instance-types/%d", instanceTypeID))
	if err != nil {
		body, err = dst.GetRaw(fmt.Sprintf("/api/instance-types/%d", instanceTypeID))
		if err != nil {
			return "", err
		}
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return "", err
	}
	raw, ok := wrap["instanceType"]
	if !ok {
		return "", nil
	}
	var it map[string]interface{}
	if err := json.Unmarshal(raw, &it); err != nil {
		return "", err
	}
	arr, _ := it["instanceTypeLayouts"].([]interface{})
	for _, le := range arr {
		layout, ok := le.(map[string]interface{})
		if !ok {
			continue
		}
		if intFromAny(layout["id"]) == layoutID {
			return strings.TrimSpace(stringFromAny(layout["code"])), nil
		}
	}
	return "", nil
}

func requireDestInstanceType(dst *morpheus.Client, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("catalog item is missing instance type code in config.type")
	}
	destITID, exists, err := findDestInstanceTypeID(dst, code)
	if err != nil {
		return err
	}
	if !exists || destITID <= 0 {
		return fmt.Errorf("instance type %q must exist on destination before catalog item migration — migrate the instance type first", code)
	}
	return nil
}

func extractCatalogInstanceTypeCode(obj map[string]interface{}) string {
	if cfg, ok := obj["config"].(map[string]interface{}); ok && cfg != nil {
		if c := strings.TrimSpace(stringFromAny(cfg["type"])); c != "" {
			return c
		}
	}
	return ""
}

func catalogLayoutNameFromSource(src *morpheus.Client, layoutRef map[string]interface{}) string {
	if src == nil || layoutRef == nil {
		return ""
	}
	layoutID := intFromAny(objectID(layoutRef))
	if layoutID <= 0 {
		return ""
	}
	dep, err := fetchSourceByID(src, "layout", layoutID)
	if err != nil {
		return ""
	}
	obj := parseObject(dep.RawJSON)
	return strings.TrimSpace(stringFromAny(obj["name"]))
}

func findSourceInstanceTypeByCode(src *morpheus.Client, code string) (SelectedItem, error) {
	want := strings.ToLower(strings.TrimSpace(code))
	if want == "" {
		return SelectedItem{}, fmt.Errorf("empty instance type code")
	}
	raws, err := paginateList(src, "/api/library/instance-types", "instanceTypes")
	if err != nil {
		raws, err = paginateList(src, "/api/instance-types", "instanceTypes")
		if err != nil {
			return SelectedItem{}, err
		}
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))) != want {
			continue
		}
		id := intFromAny(row["id"])
		if id <= 0 {
			continue
		}
		obj, err := fetchFullInstanceType(src, id)
		if err != nil {
			return SelectedItem{}, err
		}
		name := strings.TrimSpace(stringFromAny(obj["name"]))
		if name == "" {
			name = code
		}
		rawJSON, _ := json.Marshal(obj)
		return SelectedItem{
			Category: "Instance Types",
			Type:     "instanceType",
			ID:       id,
			Name:     name,
			RawJSON:  string(rawJSON),
		}, nil
	}
	return SelectedItem{}, fmt.Errorf("instance type %q not found on source", code)
}

func resolveCatalogNetworkRef(dst *morpheus.Client, cloudID int64, idName string) (map[string]interface{}, string, error) {
	idName = strings.TrimSpace(idName)
	if idName == "" {
		return nil, "", nil
	}

	destNet, err := findDestNetworkRef(dst, cloudID, idName)
	if err != nil {
		return nil, "", err
	}
	if destNet != nil {
		return destNet, "", nil
	}

	if cloudID <= 0 {
		return nil, fmt.Sprintf("Network: omitted — source network %q not found (no destination cloud)", idName), nil
	}

	if first, fname, ok, err := firstDestNetwork(dst, cloudID); err != nil {
		return nil, "", err
	} else if ok {
		note := fmt.Sprintf("Network: using destination network %q (source network %q not found on destination cloud)", fname, idName)
		return first, note, nil
	}

	return nil, fmt.Sprintf("Network: omitted — source network %q not found and no networks on destination cloud", idName), nil
}

func findDestNetworkRef(dst *morpheus.Client, cloudID int64, idName string) (map[string]interface{}, error) {
	if cloudID <= 0 || strings.TrimSpace(idName) == "" {
		return nil, nil
	}
	rows, err := listDestNetworksForCloud(dst, cloudID)
	if err != nil {
		return nil, err
	}
	want := strings.ToLower(strings.TrimSpace(idName))
	for _, row := range rows {
		candidates := []string{
			stringFromAny(row["idName"]),
			stringFromAny(row["name"]),
			stringFromAny(row["label"]),
		}
		for _, c := range candidates {
			if strings.ToLower(strings.TrimSpace(c)) == want {
				return networkRefFromRow(row, idName), nil
			}
		}
	}
	return nil, nil
}

func firstDestNetwork(dst *morpheus.Client, cloudID int64) (ref map[string]interface{}, name string, ok bool, err error) {
	if cloudID <= 0 {
		return nil, "", false, nil
	}
	rows, err := listDestNetworksForCloud(dst, cloudID)
	if err != nil {
		return nil, "", false, err
	}
	if len(rows) == 0 {
		return nil, "", false, nil
	}
	row := rows[0]
	name = strings.TrimSpace(stringFromAny(row["idName"]))
	if name == "" {
		name = strings.TrimSpace(stringFromAny(row["name"]))
	}
	if name == "" {
		name = strings.TrimSpace(stringFromAny(row["label"]))
	}
	return networkRefFromRow(row, name), name, true, nil
}

func listDestNetworksForCloud(dst *morpheus.Client, cloudID int64) ([]map[string]interface{}, error) {
	if cloudID <= 0 {
		return nil, nil
	}
	path := fmt.Sprintf("/api/networks?zoneId=%d&max=200&offset=0", cloudID)
	body, err := dst.GetRaw(path)
	if err != nil {
		return nil, err
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	raw, ok := wrap["networks"]
	if !ok {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		var row map[string]interface{}
		if json.Unmarshal(it, &row) != nil {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func networkRefFromRow(row map[string]interface{}, idNameFallback string) map[string]interface{} {
	ref := map[string]interface{}{
		"id":     stringFromAny(row["id"]),
		"idName": stringFromAny(row["idName"]),
	}
	if ref["idName"] == "" {
		ref["idName"] = strings.TrimSpace(idNameFallback)
	}
	if ref["idName"] == "" {
		ref["idName"] = stringFromAny(row["name"])
	}
	ref["hasPool"] = row["hasPool"]
	return ref
}
