package migrate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

func migrateCredentialItem(src, dst *morpheus.Client, item SelectedItem) ItemResult {
	name := strings.TrimSpace(item.Name)
	if src == nil {
		return ItemResult{
			Name: name, Type: "credential", Status: "error",
			Message: "source appliance is required to read credential details",
		}
	}

	obj, err := fetchFullCredential(src, item.ID)
	if err != nil {
		return ItemResult{Name: name, Type: "credential", Status: "error", Message: err.Error()}
	}
	if n := strings.TrimSpace(stringFromAny(obj["name"])); n != "" {
		name = n
	}

	destID, err := findDestCredentialIDByName(dst, name)
	if err != nil {
		return ItemResult{Name: name, Type: "credential", Status: "error", Message: err.Error()}
	}
	if destID > 0 {
		return ItemResult{
			Name: name, Type: "credential", Status: "success", Outcome: "updated",
			Message: "Credential already exists on destination",
		}
	}

	payload, err := buildCredentialWritePayload(obj)
	if err != nil {
		return ItemResult{Name: name, Type: "credential", Status: "error", Message: err.Error()}
	}
	_, err = dst.PostRaw("/api/credentials", payload)
	if err != nil {
		status := "error"
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "secret") ||
			strings.Contains(strings.ToLower(msg), "password") ||
			strings.Contains(strings.ToLower(msg), "required") {
			status = "blocked"
			msg = fmt.Sprintf("credential %q cannot be created on destination (%v) — create it manually with the same name, then re-run migration", name, err)
		}
		return ItemResult{Name: name, Type: "credential", Status: status, Message: msg}
	}
	return ItemResult{
		Name: name, Type: "credential", Status: "success", Outcome: "created",
		Message: "Created credential on destination",
	}
}

func migrateGroupWithClouds(src, dst *morpheus.Client, item SelectedItem) ItemResult {
	name := strings.TrimSpace(item.Name)
	if src == nil {
		return ItemResult{
			Name: name, Type: "group", Status: "error",
			Message: "source appliance is required to read group details",
		}
	}

	obj, err := fetchFullGroup(src, item.ID)
	if err != nil {
		return ItemResult{Name: name, Type: "group", Status: "error", Message: err.Error()}
	}
	if n := strings.TrimSpace(stringFromAny(obj["name"])); n != "" {
		name = n
	}

	destID, err := ensureDestGroup(src, dst, name, obj)
	if err != nil {
		return ItemResult{Name: name, Type: "group", Status: "error", Message: err.Error()}
	}

	cloudNames := extractGroupCloudNames(obj)
	if len(cloudNames) == 0 {
		return ItemResult{
			Name: name, Type: "group", Status: "success", Outcome: "updated",
			Message: "Group exists on destination",
		}
	}

	if err := associateGroupCloudsByName(dst, destID, cloudNames); err != nil {
		return ItemResult{
			Name: name, Type: "group", Status: "partial",
			Message: fmt.Sprintf("Group exists on destination but cloud association incomplete: %v", err),
		}
	}
	return ItemResult{
		Name: name, Type: "group", Status: "success", Outcome: "updated",
		Message: "Group synced on destination with associated clouds",
	}
}

func migrateCloudWithCredential(src, dst *morpheus.Client, item SelectedItem) ItemResult {
	name := strings.TrimSpace(item.Name)
	if src == nil {
		return ItemResult{
			Name: name, Type: "cloud", Status: "error",
			Message: "source appliance is required to read cloud details",
		}
	}

	obj, err := fetchFullCloud(src, item.ID)
	if err != nil {
		return ItemResult{Name: name, Type: "cloud", Status: "error", Message: err.Error()}
	}
	if n := strings.TrimSpace(stringFromAny(obj["name"])); n != "" {
		name = n
	}

	credID, credName := extractCloudCredentialRef(obj)
	if credID <= 0 && credName == "" {
		return ItemResult{
			Name: name, Type: "cloud", Status: "blocked",
			Message: fmt.Sprintf("cloud %q has no associated credential on source — cloud cannot be migrated automatically", name),
		}
	}

	destCredID, err := ensureDestCredentialForCloud(src, dst, credID, credName)
	if err != nil {
		status := "blocked"
		if !strings.Contains(strings.ToLower(err.Error()), "credential") {
			status = "error"
		}
		return ItemResult{Name: name, Type: "cloud", Status: status, Message: err.Error()}
	}

	groupName := primaryGroupNameFromCloud(obj)
	destGroupID := ""
	if groupName != "" {
		srcGroup, loadErr := sourceGroupObjForCloud(src, obj, groupName)
		if loadErr != nil {
			return ItemResult{Name: name, Type: "cloud", Status: "blocked", Message: loadErr.Error()}
		}
		destGroupID, err = ensureDestGroup(src, dst, groupName, srcGroup)
		if err != nil {
			status := "error"
			if strings.Contains(strings.ToLower(err.Error()), "source") {
				status = "blocked"
			}
			return ItemResult{Name: name, Type: "cloud", Status: status, Message: err.Error()}
		}
	}

	destCloudID, err := findDestCloudIDByName(dst, name)
	if err != nil {
		return ItemResult{Name: name, Type: "cloud", Status: "error", Message: err.Error()}
	}
	alreadyExists := destCloudID > 0

	if destCloudID <= 0 {
		payload, err := buildCloudWritePayload(obj, destCredID, destGroupID)
		if err != nil {
			return ItemResult{Name: name, Type: "cloud", Status: "error", Message: err.Error()}
		}
		body, err := dst.PostRaw("/api/zones", payload)
		if err != nil {
			status := "error"
			if strings.Contains(strings.ToLower(err.Error()), "credential") ||
				strings.Contains(strings.ToLower(err.Error()), "group") {
				status = "blocked"
			}
			return ItemResult{Name: name, Type: "cloud", Status: status, Message: err.Error()}
		}
		destCloudID = cloudIDFromWriteResponse(body)
		if destCloudID <= 0 {
			destCloudID, _ = findDestCloudIDByName(dst, name)
		}
	}

	if destCloudID <= 0 {
		return ItemResult{Name: name, Type: "cloud", Status: "error", Message: "cloud write succeeded but destination id could not be resolved"}
	}

	groupNames := extractCloudGroupNames(obj)
	if groupName != "" {
		groupNames = appendUniqueString(groupNames, groupName)
	}
	if len(groupNames) > 0 {
		for _, gn := range groupNames {
			srcGroup, loadErr := sourceGroupObjForCloud(src, obj, gn)
			if loadErr != nil {
				continue
			}
			gid, ensureErr := ensureDestGroup(src, dst, gn, srcGroup)
			if ensureErr != nil || gid == "" {
				continue
			}
			_ = associateGroupCloudsByName(dst, gid, []string{name})
		}
	}

	return ItemResult{
		Name: name, Type: "cloud", Status: "success",
		Outcome: outcomeForExists(alreadyExists),
		Message: cloudSyncMessage(alreadyExists),
	}
}

func outcomeForExists(already bool) string {
	if already {
		return "updated"
	}
	return "created"
}

func cloudSyncMessage(already bool) string {
	if already {
		return "Cloud already exists on destination; group associations synced"
	}
	return "Cloud synced on destination"
}

func fetchFullCloud(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid cloud id")
	}
	body, err := src.GetRaw(fmt.Sprintf("/api/zones/%d", id))
	if err != nil {
		return nil, fmt.Errorf("fetch cloud from source: %v", err)
	}
	return unwrapObject(body, "zone")
}

func fetchFullGroup(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid group id")
	}
	body, err := src.GetRaw(fmt.Sprintf("/api/groups/%d", id))
	if err != nil {
		body, err = src.GetRaw(fmt.Sprintf("/api/groups/%s", stringFromAny(id)))
		if err != nil {
			return nil, fmt.Errorf("fetch group from source: %v", err)
		}
	}
	return unwrapObject(body, "group")
}

func fetchFullCredential(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid credential id")
	}
	body, err := src.GetRaw(fmt.Sprintf("/api/credentials/%d", id))
	if err != nil {
		return nil, fmt.Errorf("fetch credential from source: %v", err)
	}
	return unwrapObject(body, "credential")
}

func unwrapObject(body []byte, key string) (map[string]interface{}, error) {
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	raw, ok := wrap[key]
	if !ok {
		return nil, fmt.Errorf("response missing %q", key)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func extractCloudCredentialRef(zone map[string]interface{}) (int64, string) {
	for _, key := range []string{"credential", "accountCredential"} {
		if ref, ok := zone[key].(map[string]interface{}); ok && ref != nil {
			id := intFromAny(ref["id"])
			name := strings.TrimSpace(stringFromAny(ref["name"]))
			if id > 0 || name != "" {
				return id, name
			}
		}
	}
	if cfg, ok := zone["config"].(map[string]interface{}); ok && cfg != nil {
		if cred, ok := cfg["credential"].(map[string]interface{}); ok && cred != nil {
			id := intFromAny(cred["id"])
			name := strings.TrimSpace(stringFromAny(cred["name"]))
			if id > 0 || name != "" {
				return id, name
			}
		}
		if id := intFromAny(cfg["credentialId"]); id > 0 {
			return id, ""
		}
	}
	return 0, ""
}

func extractGroupCloudNames(group map[string]interface{}) []string {
	var names []string
	if arr, ok := group["zones"].([]interface{}); ok {
		for _, e := range arr {
			if zm, ok := e.(map[string]interface{}); ok {
				if n := strings.TrimSpace(stringFromAny(zm["name"])); n != "" {
					names = appendUniqueString(names, n)
				}
			}
		}
	}
	return names
}

func extractCloudGroupNames(zone map[string]interface{}) []string {
	var names []string
	if arr, ok := zone["groups"].([]interface{}); ok {
		for _, e := range arr {
			if gm, ok := e.(map[string]interface{}); ok {
				if n := strings.TrimSpace(stringFromAny(gm["name"])); n != "" {
					names = appendUniqueString(names, n)
				}
			}
		}
	}
	if gid := intFromAny(zone["groupId"]); gid > 0 {
		_ = gid
	}
	return names
}

func primaryGroupNameFromCloud(zone map[string]interface{}) string {
	names := extractCloudGroupNames(zone)
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

func appendUniqueString(list []string, val string) []string {
	val = strings.TrimSpace(val)
	if val == "" {
		return list
	}
	lower := strings.ToLower(val)
	for _, existing := range list {
		if strings.ToLower(existing) == lower {
			return list
		}
	}
	return append(list, val)
}

func buildCredentialWritePayload(obj map[string]interface{}) ([]byte, error) {
	clone := cloneMap(obj)
	for _, k := range []string{"id", "dateCreated", "lastUpdated", "account", "accountId", "owner"} {
		delete(clone, k)
	}
	if typ, ok := clone["type"].(map[string]interface{}); ok && typ != nil {
		code := strings.TrimSpace(stringFromAny(typ["code"]))
		if code != "" {
			clone["type"] = map[string]interface{}{"code": code}
		}
	}
	return json.Marshal(map[string]interface{}{"credential": clone})
}

func buildGroupWritePayload(obj map[string]interface{}, zoneRefs []map[string]interface{}) ([]byte, error) {
	clone := cloneMap(obj)
	for _, k := range []string{"id", "dateCreated", "lastUpdated", "stats", "account", "accountId", "owner"} {
		delete(clone, k)
	}
	delete(clone, "zones")
	if len(zoneRefs) > 0 {
		clone["zones"] = zoneRefs
	}
	return json.Marshal(map[string]interface{}{"group": clone})
}

func buildCloudWritePayload(zone map[string]interface{}, destCredID int64, destGroupID string) ([]byte, error) {
	clone := cloneMap(zone)
	for _, k := range []string{
		"id", "dateCreated", "lastUpdated", "stats", "account", "accountId", "owner",
		"groups", "servers", "resources", "lastSync", "status", "statusMessage",
	} {
		delete(clone, k)
	}
	code := cloudZoneTypeCode(clone)
	if code != "" {
		clone["zoneType"] = map[string]interface{}{"code": code}
	}
	if destCredID > 0 {
		clone["credential"] = map[string]interface{}{"id": destCredID}
		delete(clone, "accountCredential")
	}
	if destGroupID != "" {
		if gid := intFromAny(destGroupID); gid > 0 {
			clone["groupId"] = gid
		} else {
			clone["groupId"] = destGroupID
		}
	}
	stripCloudZoneWriteFields(clone)
	normalizeCloudZoneForWrite(clone, code)
	return json.Marshal(map[string]interface{}{"zone": clone})
}

// stripCloudZoneWriteFields removes read-only or appliance-specific zone fields Morpheus rejects on POST.
func stripCloudZoneWriteFields(clone map[string]interface{}) {
	for _, key := range []string{
		"networkDomain", "domain", "accountDomain", "domainName",
		"zoneTypeId", "networkDomainId",
	} {
		delete(clone, key)
	}
	if zt, ok := clone["zoneType"].(map[string]interface{}); ok && zt != nil {
		delete(zt, "id")
	}
	cfg, ok := clone["config"].(map[string]interface{})
	if !ok || cfg == nil {
		return
	}
	for _, key := range []string{
		"domain", "networkDomain", "networkDomainId", "domainId", "domainName",
		"defaultDomain", "defaultDomainId", "zoneTypeId",
	} {
		delete(cfg, key)
	}
}

func cloudZoneTypeCode(zone map[string]interface{}) string {
	if zt, ok := zone["zoneType"].(map[string]interface{}); ok && zt != nil {
		if code := strings.TrimSpace(stringFromAny(zt["code"])); code != "" {
			return strings.ToLower(code)
		}
		if name := strings.TrimSpace(stringFromAny(zt["name"])); name != "" {
			return strings.ToLower(name)
		}
	}
	return ""
}

// normalizeCloudZoneForWrite shapes zone payloads the way Morpheus expects on POST /api/zones
// (aligned with legacy VMware cloud migration scripts: on/off toggles, vmware config defaults).
func normalizeCloudZoneForWrite(clone map[string]interface{}, zoneTypeCode string) {
	clone["enabled"] = morpheusOnOffString(clone["enabled"], true)
	clone["autoRecoverPowerState"] = morpheusOnOffString(clone["autoRecoverPowerState"], false)

	cfg, ok := clone["config"].(map[string]interface{})
	if !ok || cfg == nil {
		cfg = map[string]interface{}{}
		clone["config"] = cfg
	}
	for _, key := range []string{"hideHostSelection", "enableVnc", "importExisting", "enableStorageTypeSelection"} {
		if _, exists := cfg[key]; exists {
			defaultOn := key == "enableStorageTypeSelection"
			cfg[key] = morpheusOnOffString(cfg[key], defaultOn)
		}
	}

	if zoneTypeCode == "vmware" {
		if strings.TrimSpace(stringFromAny(cfg["certificateProvider"])) == "" {
			cfg["certificateProvider"] = "internal"
		}
		if _, exists := cfg["enableStorageTypeSelection"]; !exists {
			cfg["enableStorageTypeSelection"] = "on"
		}
		if _, exists := cfg["importExisting"]; !exists {
			cfg["importExisting"] = "off"
		}
		if strings.TrimSpace(stringFromAny(cfg["resourcePoolId"])) == "" {
			cfg["resourcePoolId"] = "All"
		}
	}
}

func morpheusOnOffString(v interface{}, defaultOn bool) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "on"
		}
		return "off"
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		switch s {
		case "on", "true", "yes", "1":
			return "on"
		case "off", "false", "no", "0", "":
			return "off"
		default:
			return s
		}
	case float64:
		if t != 0 {
			return "on"
		}
		return "off"
	case int:
		if t != 0 {
			return "on"
		}
		return "off"
	case int64:
		if t != 0 {
			return "on"
		}
		return "off"
	default:
		if defaultOn {
			return "on"
		}
		return "off"
	}
}

func findDestCredentialIDByName(dst *morpheus.Client, name string) (int64, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return 0, nil
	}
	raws, err := paginateList(dst, "/api/credentials", "credentials")
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

func findSourceGroupIDByName(src *morpheus.Client, name string) (int64, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return 0, nil
	}
	raws, err := paginateList(src, "/api/groups", "groups")
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

func sourceGroupObjForCloud(src *morpheus.Client, cloud map[string]interface{}, groupName string) (map[string]interface{}, error) {
	if src == nil {
		return nil, fmt.Errorf("source appliance is required to load group %q", groupName)
	}
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return nil, fmt.Errorf("empty group name")
	}
	if cloud != nil {
		if arr, ok := cloud["groups"].([]interface{}); ok {
			for _, e := range arr {
				gm, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				if !strings.EqualFold(strings.TrimSpace(stringFromAny(gm["name"])), groupName) {
					continue
				}
				if id := intFromAny(gm["id"]); id > 0 {
					return fetchFullGroup(src, id)
				}
			}
		}
	}
	srcID, err := findSourceGroupIDByName(src, groupName)
	if err != nil {
		return nil, err
	}
	if srcID <= 0 {
		return nil, fmt.Errorf("group %q not found on source", groupName)
	}
	return fetchFullGroup(src, srcID)
}

func ensureDestGroup(src, dst *morpheus.Client, groupName string, sourceGroup map[string]interface{}) (string, error) {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return "", nil
	}
	destID, err := findDestGroupIDByName(dst, groupName)
	if err != nil {
		return "", err
	}
	if destID != "" {
		return destID, nil
	}
	groupObj := sourceGroup
	if groupObj == nil {
		groupObj, err = sourceGroupObjForCloud(src, nil, groupName)
		if err != nil {
			return "", err
		}
	}
	return createGroupOnDestination(dst, groupObj)
}

func createGroupOnDestination(dst *morpheus.Client, groupObj map[string]interface{}) (string, error) {
	name := strings.TrimSpace(stringFromAny(groupObj["name"]))
	if name == "" {
		return "", fmt.Errorf("group payload has empty name")
	}
	payload, err := buildGroupWritePayload(groupObj, nil)
	if err != nil {
		return "", err
	}
	_, err = dst.PostRaw("/api/groups", payload)
	if err != nil && !isDuplicateErr(err) {
		return "", err
	}
	destID, err := findDestGroupIDByName(dst, name)
	if err != nil {
		return "", err
	}
	if destID == "" {
		return "", fmt.Errorf("group %q created but could not resolve destination id", name)
	}
	return destID, nil
}

func ensureDestCredentialForCloud(src, dst *morpheus.Client, srcCredID int64, srcCredName string) (int64, error) {
	name := strings.TrimSpace(srcCredName)
	if name == "" && srcCredID > 0 {
		obj, err := fetchFullCredential(src, srcCredID)
		if err != nil {
			return 0, fmt.Errorf("cloud credential id %d not retrievable from source (%v)", srcCredID, err)
		}
		name = strings.TrimSpace(stringFromAny(obj["name"]))
	}
	if name == "" {
		return 0, fmt.Errorf("cloud requires a credential but source credential has no name")
	}
	destID, err := findDestCredentialIDByName(dst, name)
	if err != nil {
		return 0, err
	}
	if destID > 0 {
		return destID, nil
	}
	item := SelectedItem{Type: "credential", ID: srcCredID, Name: name}
	res := migrateCredentialItem(src, dst, item)
	if res.Status != "success" {
		return 0, fmt.Errorf("%s", res.Message)
	}
	destID, err = findDestCredentialIDByName(dst, name)
	if err != nil {
		return 0, err
	}
	if destID <= 0 {
		return 0, fmt.Errorf("credential %q required for cloud migration is not available on destination", name)
	}
	return destID, nil
}

func associateGroupCloudsByName(dst *morpheus.Client, groupID string, cloudNames []string) error {
	if groupID == "" || len(cloudNames) == 0 {
		return nil
	}
	body, err := dst.GetRaw(fmt.Sprintf("/api/groups/%s", groupID))
	if err != nil {
		return fmt.Errorf("load destination group: %v", err)
	}
	groupObj, err := unwrapObject(body, "group")
	if err != nil {
		return err
	}

	existing := map[int64]struct{}{}
	var zoneRefs []map[string]interface{}
	if arr, ok := groupObj["zones"].([]interface{}); ok {
		for _, e := range arr {
			if zm, ok := e.(map[string]interface{}); ok {
				id := intFromAny(zm["id"])
				if id > 0 {
					existing[id] = struct{}{}
					zoneRefs = append(zoneRefs, map[string]interface{}{"id": id})
				}
			}
		}
	}

	added := false
	for _, cloudName := range cloudNames {
		cloudID, err := findDestCloudIDByName(dst, cloudName)
		if err != nil {
			return err
		}
		if cloudID <= 0 {
			return fmt.Errorf("cloud %q not found on destination", cloudName)
		}
		if _, ok := existing[cloudID]; ok {
			continue
		}
		zoneRefs = append(zoneRefs, map[string]interface{}{"id": cloudID})
		existing[cloudID] = struct{}{}
		added = true
	}
	if !added {
		return nil
	}

	payload, err := buildGroupWritePayload(groupObj, zoneRefs)
	if err != nil {
		return err
	}
	_, err = dst.PutRaw(fmt.Sprintf("/api/groups/%s", groupID), payload)
	if err != nil {
		return fmt.Errorf("associate clouds with group: %v", err)
	}
	return nil
}

func cloudIDFromWriteResponse(body []byte) int64 {
	obj, err := unwrapObject(body, "zone")
	if err != nil {
		return 0
	}
	return intFromAny(obj["id"])
}
