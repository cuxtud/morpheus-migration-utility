package migrate

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCatalogItemResult_withNotes(t *testing.T) {
	r := catalogItemResult("MySQL 8", "created", []string{
		"Group: using destination group \"Default\" (#1) (source group \"VMware\" not found)",
	})
	if r.Status != "success" {
		t.Fatalf("status=%q", r.Status)
	}
	if !strings.Contains(r.Message, "VMware") || !strings.Contains(r.Message, "Default") {
		t.Fatalf("message=%q", r.Message)
	}
}

func TestStripCatalogFormRefs_idOnly(t *testing.T) {
	obj := map[string]interface{}{
		"form": map[string]interface{}{
			"id":   42,
			"name": "My Form",
			"code": "my-form",
			"options": []interface{}{
				map[string]interface{}{"name": "field"},
			},
		},
		"optionTypeForm": map[string]interface{}{
			"id":   42,
			"name": "My Form",
		},
	}
	stripCatalogFormRefs(obj)
	form, _ := obj["form"].(map[string]interface{})
	if len(form) != 1 || intFromAny(form["id"]) != 42 {
		t.Fatalf("form=%v", form)
	}
	otf, _ := obj["optionTypeForm"].(map[string]interface{})
	if len(otf) != 1 || intFromAny(otf["id"]) != 42 {
		t.Fatalf("optionTypeForm=%v", otf)
	}
}

func TestBuildCatalogItemWritePayload_stripsNestedForm(t *testing.T) {
	shared := map[string]interface{}{
		"id":   7,
		"name": "Existing Form",
		"code": "existing",
	}
	obj := map[string]interface{}{
		"name":           "Catalog A",
		"code":           "catalog-a",
		"type":           "workflow",
		"form":           map[string]interface{}{"id": 7},
		"optionTypeForm": shared,
	}
	payload, err := buildCatalogItemWritePayload(obj, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wrap map[string]map[string]interface{}
	if err := json.Unmarshal(payload, &wrap); err != nil {
		t.Fatal(err)
	}
	cit := wrap["catalogItemType"]
	otf, _ := cit["optionTypeForm"].(map[string]interface{})
	if len(otf) != 1 || intFromAny(otf["id"]) != 7 {
		t.Fatalf("optionTypeForm in payload=%v", otf)
	}
	if _, ok := otf["name"]; ok {
		t.Fatalf("payload must not include nested form name: %v", otf)
	}
	if _, ok := cit["form"]; ok {
		t.Fatalf("payload must omit form when optionTypeForm is set: %v", cit["form"])
	}
}

func TestExtractCatalogFormIDs(t *testing.T) {
	obj := map[string]interface{}{
		"optionTypeForm": map[string]interface{}{"id": 13, "name": "vCD VM Deployment"},
		"formConfig": map[string]interface{}{
			"optionTypeForm": map[string]interface{}{"id": 13},
		},
		"optionTypeForms": []interface{}{
			map[string]interface{}{"id": 42, "name": "Other"},
		},
	}
	ids := extractCatalogFormIDs(obj)
	if len(ids) != 2 {
		t.Fatalf("expected 2 unique form ids, got %v", ids)
	}
}

func TestFindDestCloudIDCached_byNameAndCode(t *testing.T) {
	state := &automationState{
		catalogCache: &catalogDestCache{
			cloudLoaded: true,
			cloudByName: map[string]int64{"denver": 12},
			cloudByCode: map[string]int64{"vmware": 12},
			cloudByID:   map[int64]string{12: "denver"},
		},
	}
	id, _, err := state.findDestCloudIDCached(nil, "denver", "")
	if err != nil || id != 12 {
		t.Fatalf("name lookup id=%d err=%v", id, err)
	}
	id, _, err = state.findDestCloudIDCached(nil, "", "vmware")
	if err != nil || id != 12 {
		t.Fatalf("code lookup id=%d err=%v", id, err)
	}
	if got := state.destCloudDisplayName(nil, 12); got != "denver" {
		t.Fatalf("display name=%q", got)
	}
}

func TestUniqueNonEmptyStrings_dedupes(t *testing.T) {
	got := uniqueNonEmptyStrings("denver", "devnverVMware", "DENVER", "vmware", "vmware")
	want := []string{"denver", "devnverVMware", "vmware"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestResolveDestinationFormID_sourceFormMapping(t *testing.T) {
	state := &automationState{
		catalogCache: &catalogDestCache{
			formLoaded:    true,
			formByName:    map[string]int64{},
			formByCode:    map[string]int64{},
			srcFormToDest: map[int64]int64{13: 42},
		},
	}
	_, _, id := resolveDestinationFormID(nil, nil, state, map[string]interface{}{"id": 13})
	if id != 42 {
		t.Fatalf("dest id=%d want 42", id)
	}
}

func TestRegisterDestForm_recordsSourceID(t *testing.T) {
	state := newAutomationState(nil)
	state.registerDestForm(99, "vm-deploy", "VM Deployment", 13)
	if got := state.destFormIDForSource(13); got != 99 {
		t.Fatalf("source map id=%d", got)
	}
	c := state.destCache()
	c.mu.RLock()
	gotName := c.formByName["vm deployment"]
	gotCode := c.formByCode["vm-deploy"]
	c.mu.RUnlock()
	if gotName != 99 || gotCode != 99 {
		t.Fatalf("cache name=%d code=%d", gotName, gotCode)
	}
}

func TestIsCatalogFormPayloadErr(t *testing.T) {
	err := fmt.Errorf(`HTTP 400: {"success":false,"msg":"The form contains missing or invalid data","errors":{"name":"must be unique"}}`)
	if !isCatalogFormPayloadErr(err) {
		t.Fatal("expected catalog form validation error")
	}
}
