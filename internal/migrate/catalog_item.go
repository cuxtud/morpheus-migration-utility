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

	var snap *SourceSnapshot
	if state != nil {
		snap = state.sourceSnap
	}
	obj, err := snapCatalogObject(snap, src, item)
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

	optIDs, dep := resolveCatalogOptionTypeRefs(dst, obj["optionTypes"], state)
	if dep != nil {
		dep.Type = "catalogItem"
		if dep.Name == "" {
			dep.Name = name
		}
		return *dep
	}

	if catType == "workflow" {
		if wf, ok := obj["workflow"].(map[string]interface{}); ok && wf != nil {
			wfID, blocked := resolveCatalogWorkflowRef(src, dst, wf, state)
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
		if err := requireDestInstanceTypeCached(src, dst, state, instanceTypeCode); err != nil {
			return ItemResult{Name: name, Type: "catalogItem", Status: "blocked", Message: err.Error()}
		}
		notes, err := remapCatalogInstanceConfig(src, dst, state, cfg)
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

	destID, err := state.findDestCatalogID(dst, code, name)
	if err != nil {
		return ItemResult{Name: name, Type: "catalogItem", Status: "error", Message: err.Error()}
	}

	if destID > 0 {
		if err := putCatalogItemWithFormRetry(src, dst, state, obj, optIDs, destID, payload); err != nil {
			return ItemResult{Name: name, Type: "catalogItem", Status: "error", Message: err.Error()}
		}
		return catalogItemResult(name, "updated", remapNotes)
	}

	if err := postCatalogItemWithFormRetry(src, dst, state, obj, optIDs, code, name, payload); err != nil {
		return ItemResult{Name: name, Type: "catalogItem", Status: "error", Message: err.Error()}
	}
	return catalogItemResult(name, "created", remapNotes)
}

func resolveCatalogWorkflowRef(src, dst *morpheus.Client, ref map[string]interface{}, state *automationState) (int64, *ItemResult) {
	return ensureWorkflowFromRef(src, dst, ref, state)
}

func resolveCatalogOptionTypeRefs(dst *morpheus.Client, opt interface{}, state *automationState) ([]interface{}, *ItemResult) {
	arr, ok := opt.([]interface{})
	if !ok || len(arr) == 0 {
		return nil, nil
	}
	state.reloadDestOptionTypes(dst)
	out := make([]interface{}, 0, len(arr))
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
		if id := state.destOptionTypeID(code); id > 0 {
			out = append(out, map[string]interface{}{"id": id})
			continue
		}
		if inline := stripCatalogInlineOptionType(om); inline != nil {
			out = append(out, inline)
		}
	}
	return out, nil
}

func stripCatalogInlineOptionType(opt map[string]interface{}) map[string]interface{} {
	raw, err := json.Marshal(opt)
	if err != nil {
		return nil
	}
	var clone map[string]interface{}
	if json.Unmarshal(raw, &clone) != nil {
		return nil
	}
	for _, k := range []string{
		"id", "dateCreated", "lastUpdated", "account", "accountId", "uuid", "owner", "stats",
	} {
		delete(clone, k)
	}
	if len(clone) == 0 {
		return nil
	}
	return clone
}

func postCatalogItemWithFormRetry(src, dst *morpheus.Client, state *automationState, obj map[string]interface{}, optIDs []interface{}, code, name string, payload []byte) error {
	_, err := dst.PostRaw("/api/catalog-item-types", payload)
	if err == nil {
		return nil
	}
	if isCatalogFormPayloadErr(err) {
		if retry, ok := rebuildCatalogItemPayloadAfterFormErr(src, dst, state, obj, optIDs); ok {
			if _, retryErr := dst.PostRaw("/api/catalog-item-types", retry); retryErr == nil {
				return nil
			} else if !isCatalogFormPayloadErr(retryErr) {
				return retryErr
			}
		}
	}
	if isDuplicateErr(err) {
		destID, findErr := state.findDestCatalogID(dst, code, name)
		if findErr == nil && destID > 0 {
			return putCatalogItemWithFormRetry(src, dst, state, obj, optIDs, destID, payload)
		}
	}
	return err
}

func putCatalogItemWithFormRetry(src, dst *morpheus.Client, state *automationState, obj map[string]interface{}, optIDs []interface{}, destID int64, payload []byte) error {
	_, err := dst.PutRaw(fmt.Sprintf("/api/catalog-item-types/%d", destID), payload)
	if err == nil {
		return nil
	}
	if isCatalogFormPayloadErr(err) {
		if retry, ok := rebuildCatalogItemPayloadAfterFormErr(src, dst, state, obj, optIDs); ok {
			if _, retryErr := dst.PutRaw(fmt.Sprintf("/api/catalog-item-types/%d", destID), retry); retryErr == nil {
				return nil
			}
		}
	}
	return err
}

func isCatalogFormPayloadErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "the form contains") ||
		(strings.Contains(s, "must be unique") && strings.Contains(s, "form"))
}

func rebuildCatalogItemPayloadAfterFormErr(src, dst *morpheus.Client, state *automationState, obj map[string]interface{}, optIDs []interface{}) ([]byte, bool) {
	if state == nil {
		state = newAutomationState(nil)
	}
	_, blocked := ensureCatalogFormRefs(src, dst, obj, state, "")
	if blocked != nil {
		return nil, false
	}
	finalizeCatalogFormWriteFields(obj)
	payload, err := buildCatalogItemWritePayload(obj, optIDs)
	if err != nil {
		return nil, false
	}
	return payload, true
}

func ensureCatalogFormRefs(src, dst *morpheus.Client, obj map[string]interface{}, state *automationState, catalogName string) ([]string, *ItemResult) {
	var notes []string
	resolved := map[string]int64{}

	noted := map[int64]struct{}{}
	resolve := func(ref map[string]interface{}) (int64, *ItemResult) {
		key := catalogFormRefKey(ref)
		if key == "" {
			return 0, nil
		}
		if id, ok := resolved[key]; ok {
			return id, nil
		}
		formID, formNote, blocked := resolveCatalogFormRef(src, dst, state, ref)
		if blocked != nil {
			if blocked.Name == "" {
				blocked.Name = catalogName
			}
			return 0, blocked
		}
		resolved[key] = formID
		if formNote != "" {
			notes = append(notes, formNote)
			if formID > 0 {
				noted[formID] = struct{}{}
			}
		}
		return formID, nil
	}
	assign := func(field string, ref map[string]interface{}, formID int64) {
		if formID <= 0 {
			delete(obj, field)
			return
		}
		obj[field] = map[string]interface{}{"id": formID}
		if _, ok := noted[formID]; ok {
			return
		}
		noted[formID] = struct{}{}
		if n := strings.TrimSpace(stringFromAny(ref["name"])); n != "" {
			notes = append(notes, fmt.Sprintf("Form: linked catalog to existing form %q (#%d)", n, formID))
		} else {
			notes = append(notes, fmt.Sprintf("Form: linked catalog to destination form #%d", formID))
		}
	}

	for _, field := range []string{"form", "optionTypeForm"} {
		ref, ok := obj[field].(map[string]interface{})
		if !ok || ref == nil {
			continue
		}
		formID, blocked := resolve(ref)
		if blocked != nil {
			return notes, blocked
		}
		assign(field, ref, formID)
	}

	if fc, ok := obj["formConfig"].(map[string]interface{}); ok && fc != nil {
		for _, field := range []string{"form", "optionTypeForm"} {
			ref, ok := fc[field].(map[string]interface{})
			if !ok || ref == nil {
				continue
			}
			formID, blocked := resolve(ref)
			if blocked != nil {
				return notes, blocked
			}
			if formID > 0 {
				fc[field] = map[string]interface{}{"id": formID}
			} else {
				delete(fc, field)
			}
		}
		stripCatalogFormRefs(fc)
		obj["formConfig"] = fc
	}

	if arr, ok := obj["optionTypeForms"].([]interface{}); ok {
		out := make([]interface{}, 0, len(arr))
		for _, e := range arr {
			ref, ok := e.(map[string]interface{})
			if !ok || ref == nil {
				continue
			}
			formID, blocked := resolve(ref)
			if blocked != nil {
				return notes, blocked
			}
			if formID > 0 {
				out = append(out, map[string]interface{}{"id": formID})
			}
		}
		if len(out) > 0 {
			obj["optionTypeForms"] = out
		} else {
			delete(obj, "optionTypeForms")
		}
	}

	finalizeCatalogFormWriteFields(obj)
	return notes, nil
}

func resolveCatalogFormRef(src, dst *morpheus.Client, state *automationState, ref map[string]interface{}) (int64, string, *ItemResult) {
	formCode, formName, destID := resolveDestinationFormID(src, dst, state, ref)
	if src == nil {
		if destID > 0 {
			return destID, "", nil
		}
		return 0, "", catalogFormBlockedRef(ref, formCode, formName, "source appliance is required to migrate forms")
	}

	dep, err := selectedItemFromFormRef(src, state, ref)
	if err != nil {
		return 0, "", catalogFormBlockedRef(ref, formCode, formName, err.Error())
	}
	res := migrateFormWithAutomation(src, dst, dep, state)
	if res.Status != "success" && res.Status != "skipped" {
		if res.Name == "" {
			res.Name = formName
		}
		return 0, "", &res
	}

	_ = state.refreshDestFormIndex(dst)
	formCode, formName, destID = resolveDestinationFormID(src, dst, state, ref)
	if destID <= 0 {
		return 0, "", catalogFormBlockedRef(ref, formCode, formName, "form migration completed but destination form id could not be resolved")
	}

	label := formName
	if label == "" {
		label = formCode
	}
	if label == "" {
		label = strings.TrimSpace(dep.Name)
	}
	note := fmt.Sprintf("Form: migrated form %q to destination (#%d)", label, destID)
	if res.Status == "skipped" {
		note = fmt.Sprintf("Form: linked catalog to existing form %q (#%d)", label, destID)
	} else if res.Outcome == "updated" {
		note = fmt.Sprintf("Form: updated existing form %q on destination (#%d)", label, destID)
	}
	return destID, note, nil
}

func catalogFormBlockedRef(ref map[string]interface{}, formCode, formName, detail string) *ItemResult {
	label := formName
	if label == "" {
		label = formCode
	}
	if label == "" {
		if id := intFromAny(ref["id"]); id > 0 {
			label = fmt.Sprintf("#%d", id)
		}
	}
	if label == "" {
		label = "unknown"
	}
	return &ItemResult{
		Status:  "blocked",
		Message: fmt.Sprintf("form %q: %s", label, detail),
	}
}

func selectedItemFromFormRef(src *morpheus.Client, state *automationState, ref map[string]interface{}) (SelectedItem, error) {
	srcID := intFromAny(ref["id"])
	name := strings.TrimSpace(stringFromAny(ref["name"]))
	code := strings.TrimSpace(stringFromAny(ref["code"]))

	if state != nil && state.sourceSnap != nil && srcID > 0 {
		if it, err := state.sourceSnap.ResolveSourceItem(src, "form", srcID); err == nil {
			return it, nil
		}
	}
	if src != nil && srcID > 0 {
		if obj, err := fetchFullOptionTypeForm(src, srcID); err == nil && obj != nil {
			if name == "" {
				name = strings.TrimSpace(stringFromAny(obj["name"]))
			}
			if code == "" {
				code = strings.TrimSpace(stringFromAny(obj["code"]))
			}
			raw, _ := json.Marshal(obj)
			return SelectedItem{
				Category: "Forms",
				Type:     "form",
				ID:       srcID,
				Name:     name,
				RawJSON:  string(raw),
			}, nil
		}
	}
	if name == "" && code == "" && srcID <= 0 {
		return SelectedItem{}, fmt.Errorf("form reference has no id, name, or code")
	}
	raw, _ := json.Marshal(ref)
	return SelectedItem{
		Category: "Forms",
		Type:     "form",
		ID:       srcID,
		Name:     name,
		RawJSON:  string(raw),
	}, nil
}

func extractCatalogFormIDs(obj map[string]interface{}) []int64 {
	if obj == nil {
		return nil
	}
	seen := map[int64]struct{}{}
	var ids []int64
	add := func(ref map[string]interface{}) {
		if ref == nil {
			return
		}
		id := intFromAny(ref["id"])
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, key := range []string{"form", "optionTypeForm"} {
		if m, ok := obj[key].(map[string]interface{}); ok {
			add(m)
		}
	}
	if fc, ok := obj["formConfig"].(map[string]interface{}); ok {
		for _, key := range []string{"form", "optionTypeForm"} {
			if m, ok := fc[key].(map[string]interface{}); ok {
				add(m)
			}
		}
	}
	if arr, ok := obj["optionTypeForms"].([]interface{}); ok {
		for _, e := range arr {
			if m, ok := e.(map[string]interface{}); ok {
				add(m)
			}
		}
	}
	return ids
}

func catalogFormRefKey(ref map[string]interface{}) string {
	if ref == nil {
		return ""
	}
	if id := intFromAny(ref["id"]); id > 0 {
		return fmt.Sprintf("id:%d", id)
	}
	code := strings.TrimSpace(stringFromAny(ref["code"]))
	name := strings.TrimSpace(stringFromAny(ref["name"]))
	if code == "" && name == "" {
		return ""
	}
	return code + "\x00" + name
}

func finalizeCatalogFormWriteFields(obj map[string]interface{}) {
	formType := strings.ToLower(strings.TrimSpace(stringFromAny(obj["formType"])))

	if otf, ok := obj["optionTypeForm"].(map[string]interface{}); ok {
		if id := intFromAny(otf["id"]); id > 0 {
			obj["optionTypeForm"] = map[string]interface{}{"id": id}
			obj["formType"] = "optionTypeForm"
			delete(obj, "form")
			delete(obj, "formConfig")
			delete(obj, "optionTypeForms")
			return
		}
	}

	if formType == "optiontypes" || formType == "" {
		delete(obj, "optionTypeForm")
		delete(obj, "form")
		delete(obj, "formConfig")
		delete(obj, "optionTypeForms")
	}
	stripCatalogFormRefs(obj)
}

func stripCatalogFormRefs(obj map[string]interface{}) {
	idOnly := func(m map[string]interface{}) map[string]interface{} {
		if id := intFromAny(m["id"]); id > 0 {
			return map[string]interface{}{"id": id}
		}
		return nil
	}
	for _, key := range []string{"form", "optionTypeForm"} {
		m, ok := obj[key].(map[string]interface{})
		if !ok || m == nil {
			continue
		}
		if stripped := idOnly(m); stripped != nil {
			obj[key] = stripped
		} else {
			delete(obj, key)
		}
	}
	if arr, ok := obj["optionTypeForms"].([]interface{}); ok {
		out := make([]interface{}, 0, len(arr))
		for _, e := range arr {
			m, ok := e.(map[string]interface{})
			if !ok || m == nil {
				continue
			}
			if stripped := idOnly(m); stripped != nil {
				out = append(out, stripped)
			}
		}
		if len(out) > 0 {
			obj["optionTypeForms"] = out
		} else {
			delete(obj, "optionTypeForms")
		}
	}
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
	finalizeCatalogFormWriteFields(clone)
	return json.Marshal(map[string]interface{}{"catalogItemType": clone})
}

func remapCatalogInstanceConfig(src, dst *morpheus.Client, state *automationState, cfg map[string]interface{}) ([]string, error) {
	instanceTypeCode := strings.TrimSpace(stringFromAny(cfg["type"]))
	var notes []string

	if g, ok := cfg["group"].(map[string]interface{}); ok && g != nil {
		ref, note, err := resolveCatalogGroupRef(dst, state, g)
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
		ref, note, err := resolveCatalogCloudRef(src, dst, state, c)
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
		version := strings.TrimSpace(stringFromAny(cfg["version"]))
		destLayoutID, destLayoutCode, layoutNote, err := ensureCatalogLayoutRef(src, dst, state, instanceTypeCode, l, version)
		if err != nil {
			return notes, err
		}
		if layoutNote != "" {
			notes = append(notes, layoutNote)
		}
		ref := map[string]interface{}{"id": destLayoutID}
		if destLayoutCode != "" {
			ref["code"] = destLayoutCode
		}
		cfg["layout"] = ref
	}

	if p, ok := cfg["plan"].(map[string]interface{}); ok && p != nil {
		ref, note, err := resolveCatalogPlanRef(dst, state, p)
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
			destNet, note, err := resolveCatalogNetworkRef(dst, state, cloudID, idName)
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
				dstVI, err := state.findDestVirtualImageIDCached(dst, viName)
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

func resolveCatalogGroupRef(dst *morpheus.Client, state *automationState, sourceRef map[string]interface{}) (map[string]interface{}, string, error) {
	sourceName := strings.TrimSpace(stringFromAny(sourceRef["name"]))
	sourceID := strings.TrimSpace(stringFromAny(sourceRef["id"]))

	if sourceName != "" {
		gid, err := state.findDestGroupIDCached(dst, sourceName)
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

func resolveCatalogCloudRef(src, dst *morpheus.Client, state *automationState, sourceRef map[string]interface{}) (map[string]interface{}, string, error) {
	embeddedName := strings.TrimSpace(stringFromAny(sourceRef["name"]))
	embeddedCode := strings.TrimSpace(stringFromAny(sourceRef["code"]))
	sourceID := intFromAny(sourceRef["id"])

	cloudName := embeddedName
	cloudCode := embeddedCode

	// Catalog instanceSpec often embeds a stale display label; resolve the real cloud from source.
	if src != nil && sourceID > 0 {
		var zone map[string]interface{}
		var err error
		if state != nil && state.sourceSnap != nil {
			zone, err = state.sourceSnap.CloudObject(src, sourceID)
		} else {
			zone, err = fetchFullCloud(src, sourceID)
		}
		if err == nil && zone != nil {
			if n := strings.TrimSpace(stringFromAny(zone["name"])); n != "" {
				cloudName = n
			}
			if c := strings.TrimSpace(stringFromAny(zone["code"])); c != "" {
				cloudCode = c
			}
			if cloudCode == "" {
				cloudCode = strings.TrimSpace(stringFromAny(zone["zoneCode"]))
			}
		}
	}

	tryNames := uniqueNonEmptyStrings(cloudName, embeddedName)
	tryCodes := uniqueNonEmptyStrings(cloudCode, embeddedCode)

	for _, n := range tryNames {
		cid, _, err := state.findDestCloudIDCached(dst, n, "")
		if err != nil {
			return nil, "", err
		}
		if cid > 0 {
			destName := n
			if dn := state.destCloudDisplayName(dst, cid); dn != "" {
				destName = dn
			}
			note := ""
			if !strings.EqualFold(n, embeddedName) && embeddedName != "" {
				note = fmt.Sprintf("Cloud: mapped source cloud %q (#%d) to destination %q (#%d)", cloudName, sourceID, destName, cid)
			}
			return map[string]interface{}{"id": cid, "name": destName}, note, nil
		}
	}
	for _, c := range tryCodes {
		cid, _, err := state.findDestCloudIDCached(dst, "", c)
		if err != nil {
			return nil, "", err
		}
		if cid > 0 {
			destName := state.destCloudDisplayName(dst, cid)
			if destName == "" {
				destName = c
			}
			note := fmt.Sprintf("Cloud: mapped source cloud code %q to destination %q (#%d)", c, destName, cid)
			return map[string]interface{}{"id": cid, "name": destName}, note, nil
		}
	}

	if cid, cname, ok, err := firstDestCloud(dst); err != nil {
		return nil, "", err
	} else if ok {
		label := cloudName
		if label == "" {
			label = cloudCode
		}
		if label == "" {
			label = embeddedName
		}
		note := fmt.Sprintf("Cloud: using destination cloud %q (#%d)", cname, cid)
		if label != "" {
			note += fmt.Sprintf(" (source cloud %q", label)
			if cloudCode != "" && !strings.EqualFold(cloudCode, label) {
				note += fmt.Sprintf(" code %q", cloudCode)
			}
			note += " not found on destination)"
		}
		return map[string]interface{}{"id": cid, "name": cname}, note, nil
	}

	label := cloudName
	if label == "" {
		label = embeddedName
	}
	note := "Cloud: omitted — no clouds on destination"
	if label != "" || cloudCode != "" {
		note = fmt.Sprintf("Cloud: omitted — source cloud %q", label)
		if cloudCode != "" && !strings.EqualFold(cloudCode, label) {
			note += fmt.Sprintf(" (code %q)", cloudCode)
		}
		note += " not found on destination"
	}
	return nil, note, nil
}

func uniqueNonEmptyStrings(vals ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
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

func resolveCatalogPlanRef(dst *morpheus.Client, state *automationState, sourceRef map[string]interface{}) (map[string]interface{}, string, error) {
	sourceCode := strings.TrimSpace(stringFromAny(sourceRef["code"]))
	sourceID := intFromAny(sourceRef["id"])

	if pid, matchedCode, err := state.findDestServicePlanIDCached(dst, sourceCode, 0); err != nil {
		return nil, "", err
	} else if pid > 0 {
		code := matchedCode
		if code == "" {
			code = sourceCode
		}
		return map[string]interface{}{"id": pid, "code": code}, "", nil
	}

	if sourceID > 0 {
		if pid, _, err := state.findDestServicePlanIDCached(dst, "", sourceID); err != nil {
			return nil, "", err
		} else if pid > 0 {
			label := sourceCode
			if label == "" {
				label = fmt.Sprintf("#%d", pid)
			}
			note := fmt.Sprintf("Plan: using destination plan %q (#%d)", label, pid)
			if sourceCode != "" {
				note += fmt.Sprintf(" (source plan %q not found by code)", sourceCode)
			}
			return map[string]interface{}{"id": pid, "code": sourceCode}, note, nil
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

func ensureCatalogLayoutRef(src, dst *morpheus.Client, state *automationState, instanceTypeCode string, layoutRef map[string]interface{}, version string) (int64, string, string, error) {
	layoutCode := strings.TrimSpace(stringFromAny(layoutRef["code"]))
	layoutName := strings.TrimSpace(stringFromAny(layoutRef["name"]))
	if layoutName == "" {
		layoutName = catalogLayoutNameFromSource(src, state, layoutRef)
	}
	systemIT := catalogInstanceTypeIsSystem(src, state, instanceTypeCode)

	destLayoutID, destLayoutCode, err := findDestCatalogLayoutRef(src, dst, state, instanceTypeCode, layoutCode, layoutName, version, systemIT)
	if err != nil {
		return 0, "", "", err
	}
	if destLayoutID > 0 {
		return destLayoutID, destLayoutCode, "", nil
	}
	if systemIT {
		label := layoutCode
		if label == "" {
			label = layoutName
		}
		return 0, "", "", fmt.Errorf("layout %q not found on destination system instance type %q — system layouts cannot be migrated", label, instanceTypeCode)
	}
	if src == nil {
		return 0, "", "", fmt.Errorf("layout %q not found on destination instance type %q and source is unavailable", layoutCode, instanceTypeCode)
	}

	layoutObj, err := resolveSourceLayoutForCatalog(src, state, instanceTypeCode, layoutRef, layoutCode, layoutName, version)
	if err != nil {
		return 0, "", "", fmt.Errorf("layout %q on instance type %q: %v", layoutCode, instanceTypeCode, err)
	}

	destITID, exists, err := state.findDestInstanceTypeIDCached(dst, instanceTypeCode)
	if err != nil {
		return 0, "", "", err
	}
	if !exists || destITID <= 0 {
		return 0, "", "", fmt.Errorf("instance type %q must exist on destination before catalog layout migration", instanceTypeCode)
	}

	res := migrateInstanceTypeLayout(src, dst, destITID, layoutObj, state)
	if res.Status != "success" && res.Status != "skipped" {
		return 0, "", "", fmt.Errorf("layout %q: %s", strings.TrimSpace(stringFromAny(layoutObj["name"])), res.Message)
	}

	destLayoutID, err = findDestLayoutID(dst, destITID, layoutName, version, layoutCode)
	if err != nil {
		return 0, "", "", err
	}
	if destLayoutID <= 0 {
		destLayoutID, _, err = findDestCatalogLayoutRef(src, dst, state, instanceTypeCode, layoutCode, layoutName, version, false)
		if err != nil {
			return 0, "", "", err
		}
	}
	if destLayoutID <= 0 {
		return 0, "", "", fmt.Errorf("layout %q migrated but could not be resolved on destination instance type %q", layoutCode, instanceTypeCode)
	}
	destLayoutCode, _ = lookupDestLayoutCode(dst, destITID, destLayoutID)
	label := layoutName
	if label == "" {
		label = layoutCode
	}
	note := fmt.Sprintf("Layout: migrated layout %q to destination (#%d)", label, destLayoutID)
	return destLayoutID, destLayoutCode, note, nil
}

func resolveSourceLayoutForCatalog(src *morpheus.Client, state *automationState, instanceTypeCode string, layoutRef map[string]interface{}, layoutCode, layoutName, version string) (map[string]interface{}, error) {
	layoutID := intFromAny(objectID(layoutRef))
	if layoutID > 0 {
		var dep SelectedItem
		var err error
		if state != nil && state.sourceSnap != nil {
			dep, err = state.sourceSnap.ResolveSourceItem(src, "layout", layoutID)
		} else {
			dep, err = fetchSourceByIDLive(src, "layout", layoutID)
		}
		if err == nil {
			if obj := parseObject(dep.RawJSON); obj != nil {
				return obj, nil
			}
		}
	}

	var srcIT SelectedItem
	var err error
	if state != nil && state.sourceSnap != nil {
		var ok bool
		srcIT, ok = state.sourceSnap.FindInstanceTypeByCode(instanceTypeCode)
		if !ok {
			srcIT, err = state.sourceSnap.ResolveInstanceTypeByCode(src, instanceTypeCode)
		} else {
			err = nil
		}
	} else {
		srcIT, err = findSourceInstanceTypeByCodeLive(src, instanceTypeCode)
	}
	if err != nil {
		return nil, err
	}
	var itObj map[string]interface{}
	if state != nil && state.sourceSnap != nil {
		itObj, err = state.sourceSnap.InstanceTypeObject(src, srcIT)
	} else {
		itObj, err = fetchFullInstanceType(src, srcIT.ID)
	}
	if err != nil {
		return nil, err
	}
	if obj := findLayoutOnInstanceType(itObj, layoutCode, layoutName, version); obj != nil {
		return obj, nil
	}
	return nil, fmt.Errorf("layout not found on source instance type %q", instanceTypeCode)
}

func findLayoutOnInstanceType(itObj map[string]interface{}, layoutCode, layoutName, version string) map[string]interface{} {
	if itObj == nil {
		return nil
	}
	wantCode := strings.ToLower(strings.TrimSpace(layoutCode))
	wantName := strings.ToLower(strings.TrimSpace(layoutName))
	wantVer := strings.ToLower(strings.TrimSpace(version))
	arr, _ := itObj["instanceTypeLayouts"].([]interface{})
	for _, le := range arr {
		layout, ok := le.(map[string]interface{})
		if !ok {
			continue
		}
		code := strings.ToLower(strings.TrimSpace(stringFromAny(layout["code"])))
		name := strings.ToLower(strings.TrimSpace(stringFromAny(layout["name"])))
		ver := strings.ToLower(strings.TrimSpace(stringFromAny(layout["instanceVersion"])))
		if wantCode != "" && code == wantCode {
			return layout
		}
		if wantName != "" && name == wantName {
			if wantVer == "" || ver == wantVer {
				return layout
			}
		}
	}
	return nil
}

func findDestCatalogLayoutRef(src, dst *morpheus.Client, state *automationState, instanceTypeCode, layoutCode, layoutName, version string, systemIT bool) (int64, string, error) {
	destITID, exists, err := state.findDestInstanceTypeIDCached(dst, instanceTypeCode)
	if err != nil {
		return 0, "", err
	}
	if !exists || destITID <= 0 {
		if systemIT || catalogInstanceTypeIsSystem(src, state, instanceTypeCode) {
			return 0, "", fmt.Errorf("system instance type %q not found on destination — built-in instance types should exist on both appliances", instanceTypeCode)
		}
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

func requireDestInstanceTypeCached(src, dst *morpheus.Client, state *automationState, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("catalog item is missing instance type code in config.type")
	}
	destITID, exists, err := state.findDestInstanceTypeIDCached(dst, code)
	if err != nil {
		return err
	}
	if !exists || destITID <= 0 {
		if catalogInstanceTypeIsSystem(src, state, code) {
			return fmt.Errorf("system instance type %q not found on destination — built-in instance types should exist on both appliances", code)
		}
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

func catalogLayoutNameFromSource(src *morpheus.Client, state *automationState, layoutRef map[string]interface{}) string {
	if src == nil || layoutRef == nil {
		return ""
	}
	layoutID := intFromAny(objectID(layoutRef))
	if layoutID <= 0 {
		return ""
	}
	var dep SelectedItem
	var err error
	if state != nil && state.sourceSnap != nil {
		dep, err = state.sourceSnap.ResolveSourceItem(src, "layout", layoutID)
	} else {
		dep, err = fetchSourceByIDLive(src, "layout", layoutID)
	}
	if err != nil {
		return ""
	}
	obj := parseObject(dep.RawJSON)
	return strings.TrimSpace(stringFromAny(obj["name"]))
}

func snapCatalogObject(snap *SourceSnapshot, src *morpheus.Client, item SelectedItem) (map[string]interface{}, error) {
	if snap != nil {
		return snap.CatalogObject(src, item)
	}
	return fetchFullCatalogItem(src, item.ID)
}

func findSourceInstanceTypeByCodeLive(src *morpheus.Client, code string) (SelectedItem, error) {
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

func resolveCatalogNetworkRef(dst *morpheus.Client, state *automationState, cloudID int64, idName string) (map[string]interface{}, string, error) {
	idName = strings.TrimSpace(idName)
	if idName == "" {
		return nil, "", nil
	}

	destNet, err := findDestNetworkRef(dst, state, cloudID, idName)
	if err != nil {
		return nil, "", err
	}
	if destNet != nil {
		return destNet, "", nil
	}

	if cloudID <= 0 {
		return nil, fmt.Sprintf("Network: omitted — source network %q not found (no destination cloud)", idName), nil
	}

	if first, fname, ok, err := firstDestNetwork(dst, state, cloudID); err != nil {
		return nil, "", err
	} else if ok {
		note := fmt.Sprintf("Network: using destination network %q (source network %q not found on destination cloud)", fname, idName)
		return first, note, nil
	}

	return nil, fmt.Sprintf("Network: omitted — source network %q not found and no networks on destination cloud", idName), nil
}

func findDestNetworkRef(dst *morpheus.Client, state *automationState, cloudID int64, idName string) (map[string]interface{}, error) {
	if cloudID <= 0 || strings.TrimSpace(idName) == "" {
		return nil, nil
	}
	rows, err := state.listDestNetworksCached(dst, cloudID)
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

func firstDestNetwork(dst *morpheus.Client, state *automationState, cloudID int64) (ref map[string]interface{}, name string, ok bool, err error) {
	if cloudID <= 0 {
		return nil, "", false, nil
	}
	rows, err := state.listDestNetworksCached(dst, cloudID)
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
