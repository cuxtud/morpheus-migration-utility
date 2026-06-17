package migrate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

func migrateOptionListWithAutomation(src, dst *morpheus.Client, item SelectedItem, state *automationState) ItemResult {
	name := strings.TrimSpace(item.Name)
	item.Type = normalizeType(item.Type)

	var obj map[string]interface{}
	if state != nil && state.sourceSnap != nil {
		if snapObj, err := state.sourceSnap.OptionListObject(src, item); err == nil && snapObj != nil {
			obj = snapObj
		}
	}
	if obj == nil && src != nil && item.ID > 0 {
		full, err := fetchSourceOptionTypeList(src, item.ID)
		if err != nil {
			return ItemResult{Name: name, Type: item.Type, Status: "error", Message: err.Error()}
		}
		obj = full
	}
	if obj == nil {
		if err := json.Unmarshal([]byte(item.RawJSON), &obj); err != nil || obj == nil {
			return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("invalid option list json: %v", err)}
		}
	}
	if n := strings.TrimSpace(stringFromAny(obj["name"])); n != "" {
		name = n
	}
	if dst == nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: "destination appliance is required"}
	}

	existingID := findOptionTypeListIDByName(dst, name)
	outcome, dstID, err := writeOptionTypeList(src, dst, obj, existingID)
	if err != nil {
		status := "blocked"
		if existingID > 0 {
			status = "error"
		}
		return ItemResult{Name: name, Type: item.Type, Status: status, Message: err.Error()}
	}
	return ItemResult{
		Name:    name,
		Type:    item.Type,
		Status:  "success",
		Outcome: outcome,
		Message: fmt.Sprintf("%s option list on destination (id %d)", outcome, dstID),
	}
}

func ensureOptionTypeListDependency(src, dst *morpheus.Client, optionType map[string]interface{}) error {
	ol, ok := optionType["optionList"].(map[string]interface{})
	if !ok || ol == nil {
		return nil
	}
	srcListID := intFromAny(ol["id"])
	srcListName := strings.TrimSpace(stringFromAny(ol["name"]))
	if srcListName == "" && srcListID > 0 && src != nil {
		name, err := fetchSourceOptionTypeListName(src, srcListID)
		if err == nil {
			srcListName = name
		}
	}
	if srcListName == "" {
		return nil
	}

	if dstID := findOptionTypeListIDByName(dst, srcListName); dstID > 0 {
		optionType["optionList"] = map[string]interface{}{"id": dstID}
		return nil
	}

	if src == nil || srcListID <= 0 {
		return fmt.Errorf("option list %q is not present on destination and source details are unavailable", srcListName)
	}

	srcObj, err := fetchSourceOptionTypeList(src, srcListID)
	if err != nil {
		return fmt.Errorf("option list %q is not present on destination and could not be loaded from source id %d: %v", srcListName, srcListID, err)
	}
	_, dstID, err := writeOptionTypeList(src, dst, srcObj, 0)
	if err != nil {
		return fmt.Errorf("failed to create option list %q on destination: %v", srcListName, err)
	}
	optionType["optionList"] = map[string]interface{}{"id": dstID}
	return nil
}

func writeOptionTypeList(src, dst *morpheus.Client, srcObj map[string]interface{}, existingID int64) (outcome string, dstID int64, err error) {
	listName := strings.TrimSpace(stringFromAny(srcObj["name"]))
	if err := validateOptionListAuthForMigrate(src, dst, srcObj); err != nil {
		return "", 0, err
	}

	payload, err := buildOptionTypeListWritePayload(src, dst, srcObj)
	if err != nil {
		return "", 0, err
	}

	if existingID > 0 {
		_, err = dst.PutRaw(fmt.Sprintf("/api/library/option-type-lists/%d", existingID), payload)
		if err != nil {
			return "", 0, fmt.Errorf("update option list: %v", err)
		}
		return "updated", existingID, nil
	}

	body, err := dst.PostRaw("/api/library/option-type-lists", payload)
	if err != nil {
		if isDuplicateErr(err) {
			if id := findOptionTypeListIDByName(dst, listName); id > 0 {
				_, putErr := dst.PutRaw(fmt.Sprintf("/api/library/option-type-lists/%d", id), payload)
				if putErr != nil {
					return "", 0, fmt.Errorf("update option list: %v", putErr)
				}
				return "updated", id, nil
			}
		}
		return "", 0, err
	}
	if id := parseOptionTypeListIDFromResponse(body); id > 0 {
		return "created", id, nil
	}
	if id := findOptionTypeListIDByName(dst, listName); id > 0 {
		return "created", id, nil
	}
	return "", 0, fmt.Errorf("POST /api/library/option-type-lists: response missing optionTypeList id")
}

func validateOptionListAuthForMigrate(src, dst *morpheus.Client, obj map[string]interface{}) error {
	typ := normalizeOptionListType(obj)
	listName := strings.TrimSpace(stringFromAny(obj["name"]))

	switch typ {
	case "api", "manual":
		return nil
	case "plugin":
		return fmt.Errorf("option list %q (plugin) cannot be migrated automatically", listName)
	case "rest":
		return validateAuthBackedOptionList(src, dst, obj, listName, false)
	case "ldap":
		return validateAuthBackedOptionList(src, dst, obj, listName, true)
	default:
		return nil
	}
}

func buildOptionTypeListWritePayload(src, dst *morpheus.Client, srcObj map[string]interface{}) ([]byte, error) {
	var clone map[string]interface{}
	raw, err := json.Marshal(srcObj)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}

	for _, k := range []string{
		"id", "dateCreated", "lastUpdated", "createdBy", "account",
		"owner", "stats", "uuid",
	} {
		delete(clone, k)
	}

	typ := normalizeOptionListType(clone)
	if typ == "rest" || typ == "ldap" {
		stripOptionListInlineAuth(clone)
		delete(clone, "integration")
		if err := applyOptionListDestinationAuth(src, dst, clone); err != nil {
			return nil, err
		}
	}

	return json.Marshal(map[string]interface{}{"optionTypeList": clone})
}

func validateAuthBackedOptionList(src, dst *morpheus.Client, obj map[string]interface{}, listName string, ldap bool) error {
	if optionListHasInlineAuth(obj) {
		return fmt.Errorf("option list %q uses inline credentials — create it on destination with a stored credential %q or migrate manually", listName, describeExpectedCredential(src, obj))
	}
	if optionListHasIntegrationRef(obj) {
		return fmt.Errorf("option list %q references an integration — create it on destination with a stored credential or migrate manually", listName)
	}
	credName, err := resolveOptionListCredentialName(src, obj)
	if err != nil {
		return err
	}
	if credName != "" {
		destCredID, err := findDestCredentialIDByName(dst, credName)
		if err != nil {
			return err
		}
		if destCredID <= 0 {
			return fmt.Errorf("destination credential %q required for option list %q is not present — create it with the same name, then re-run", credName, listName)
		}
		return nil
	}
	if optionListHasHeaderAuth(obj) {
		return nil
	}
	if ldap {
		return fmt.Errorf("option list %q (ldap) can only be migrated when mapped to a stored credential on source", listName)
	}
	return nil
}

func applyOptionListDestinationAuth(src, dst *morpheus.Client, clone map[string]interface{}) error {
	credName, err := resolveOptionListCredentialName(src, clone)
	if err != nil {
		return err
	}
	if credName != "" {
		if err := applyOptionListDestinationCredential(src, dst, clone); err != nil {
			return err
		}
		stripOptionListHeaderAuth(clone)
		return nil
	}
	if optionListHasHeaderAuth(clone) {
		normalizeOptionListSourceHeaders(clone)
		delete(clone, "credential")
		delete(clone, "accountCredential")
		return nil
	}
	delete(clone, "credential")
	delete(clone, "accountCredential")
	return nil
}

func normalizeOptionListSourceHeaders(clone map[string]interface{}) {
	cfg, ok := clone["config"].(map[string]interface{})
	if !ok || cfg == nil {
		cfg = map[string]interface{}{}
		clone["config"] = cfg
	}
	headers, ok := cfg["sourceHeaders"].([]interface{})
	if !ok || len(headers) == 0 {
		return
	}
	out := make([]interface{}, 0, len(headers))
	for _, h := range headers {
		hm, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringFromAny(hm["name"]))
		if name == "" {
			continue
		}
		row := map[string]interface{}{
			"name":  name,
			"value": stringFromAny(hm["value"]),
		}
		switch masked := hm["masked"].(type) {
		case bool:
			if masked {
				row["masked"] = "on"
			} else {
				row["masked"] = "off"
			}
		default:
			if s := strings.TrimSpace(stringFromAny(masked)); s != "" {
				row["masked"] = s
			} else {
				row["masked"] = "off"
			}
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		delete(cfg, "sourceHeaders")
		return
	}
	cfg["sourceHeaders"] = out
}

func applyOptionListDestinationCredential(src, dst *morpheus.Client, clone map[string]interface{}) error {
	credName, err := resolveOptionListCredentialName(src, clone)
	if err != nil {
		return err
	}
	if credName == "" {
		return fmt.Errorf("stored credential reference is required")
	}
	destCredID, err := findDestCredentialIDByName(dst, credName)
	if err != nil {
		return err
	}
	if destCredID <= 0 {
		return fmt.Errorf("destination credential %q is not present", credName)
	}
	clone["credential"] = map[string]interface{}{"id": destCredID}
	delete(clone, "accountCredential")
	return nil
}

func resolveOptionListCredentialName(src *morpheus.Client, obj map[string]interface{}) (string, error) {
	credID, credName, ok := extractOptionListCredentialRef(obj)
	if !ok {
		return "", nil
	}
	if credName != "" {
		return credName, nil
	}
	if src == nil || credID <= 0 {
		return "", fmt.Errorf("option list credential id %d has no name and source is unavailable", credID)
	}
	credObj, err := fetchFullCredential(src, credID)
	if err != nil {
		return "", fmt.Errorf("load source credential id %d: %v", credID, err)
	}
	name := strings.TrimSpace(stringFromAny(credObj["name"]))
	if name == "" {
		return "", fmt.Errorf("source credential id %d has empty name", credID)
	}
	return name, nil
}

func describeExpectedCredential(src *morpheus.Client, obj map[string]interface{}) string {
	name, err := resolveOptionListCredentialName(src, obj)
	if err != nil || name == "" {
		return "(same name as source)"
	}
	return name
}

func extractOptionListCredentialRef(obj map[string]interface{}) (id int64, name string, ok bool) {
	for _, key := range []string{"credential", "accountCredential"} {
		if ref, okMap := obj[key].(map[string]interface{}); okMap && ref != nil {
			id = intFromAny(ref["id"])
			name = strings.TrimSpace(stringFromAny(ref["name"]))
			if id > 0 || name != "" {
				return id, name, true
			}
		}
	}
	if cfg, okCfg := obj["config"].(map[string]interface{}); okCfg && cfg != nil {
		if ref, okMap := cfg["credential"].(map[string]interface{}); okMap && ref != nil {
			id = intFromAny(ref["id"])
			name = strings.TrimSpace(stringFromAny(ref["name"]))
			if id > 0 || name != "" {
				return id, name, true
			}
		}
		if id = intFromAny(cfg["credentialId"]); id > 0 {
			return id, "", true
		}
	}
	return 0, "", false
}

func normalizeOptionListType(obj map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(stringFromAny(obj["type"])))
}

func optionListHasInlineAuth(obj map[string]interface{}) bool {
	if strings.TrimSpace(stringFromAny(obj["serviceUsername"])) != "" {
		return true
	}
	if strings.TrimSpace(stringFromAny(obj["servicePassword"])) != "" {
		return true
	}
	if strings.TrimSpace(stringFromAny(obj["servicePasswordHash"])) != "" {
		return true
	}
	return false
}

func optionListHasHeaderAuth(obj map[string]interface{}) bool {
	cfg, ok := obj["config"].(map[string]interface{})
	if !ok || cfg == nil {
		return false
	}
	headers, ok := cfg["sourceHeaders"].([]interface{})
	if !ok || len(headers) == 0 {
		return false
	}
	for _, h := range headers {
		hm, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if strings.TrimSpace(stringFromAny(hm["name"])) != "" {
			return true
		}
	}
	return false
}

func optionListHasIntegrationRef(obj map[string]interface{}) bool {
	if intFromAny(obj["integrationId"]) > 0 {
		return true
	}
	if ref, ok := obj["integration"].(map[string]interface{}); ok && ref != nil {
		if intFromAny(ref["id"]) > 0 || strings.TrimSpace(stringFromAny(ref["name"])) != "" {
			return true
		}
	}
	if cfg, ok := obj["config"].(map[string]interface{}); ok && cfg != nil {
		if intFromAny(cfg["integrationId"]) > 0 {
			return true
		}
		if ref, ok := cfg["integration"].(map[string]interface{}); ok && ref != nil {
			if intFromAny(ref["id"]) > 0 || strings.TrimSpace(stringFromAny(ref["name"])) != "" {
				return true
			}
		}
	}
	return false
}

func stripOptionListInlineAuth(clone map[string]interface{}) {
	delete(clone, "serviceUsername")
	delete(clone, "servicePassword")
	delete(clone, "servicePasswordHash")
}

func stripOptionListHeaderAuth(clone map[string]interface{}) {
	cfg, ok := clone["config"].(map[string]interface{})
	if !ok || cfg == nil {
		return
	}
	delete(cfg, "sourceHeaders")
	if len(cfg) == 0 {
		delete(clone, "config")
	}
}

func fetchSourceOptionTypeListName(src *morpheus.Client, id int64) (string, error) {
	obj, err := fetchSourceOptionTypeList(src, id)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(stringFromAny(obj["name"]))
	if name == "" {
		return "", fmt.Errorf("option list id %d has empty name", id)
	}
	return name, nil
}

func fetchSourceOptionTypeList(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	paths := []string{
		fmt.Sprintf("/api/library/option-type-lists/%d", id),
		fmt.Sprintf("/api/option-type-lists/%d", id),
	}
	var lastErr error
	for _, p := range paths {
		body, err := src.GetRaw(p)
		if err != nil {
			lastErr = err
			continue
		}
		var wrap map[string]json.RawMessage
		if err := json.Unmarshal(body, &wrap); err != nil {
			lastErr = err
			continue
		}
		raw, ok := wrap["optionTypeList"]
		if !ok {
			lastErr = fmt.Errorf("missing optionTypeList key in response")
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			lastErr = err
			continue
		}
		return obj, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("could not fetch option list id %d", id)
}

func findOptionTypeListIDByName(dst *morpheus.Client, name string) int64 {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return 0
	}
	raws, err := paginateList(dst, "/api/library/option-type-lists", "optionTypeLists")
	if err != nil {
		return 0
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(row["name"]))) != want {
			continue
		}
		if id := intFromAny(row["id"]); id > 0 {
			return id
		}
	}
	return 0
}

func parseOptionTypeListIDFromResponse(body []byte) int64 {
	var root map[string]interface{}
	if json.Unmarshal(body, &root) == nil {
		if id := intFromAny(root["id"]); id > 0 {
			return id
		}
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal(body, &wrap) != nil {
		return 0
	}
	raw, ok := wrap["optionTypeList"]
	if !ok {
		return 0
	}
	var row map[string]interface{}
	if json.Unmarshal(raw, &row) != nil {
		return 0
	}
	return intFromAny(row["id"])
}
