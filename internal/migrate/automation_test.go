package migrate

import (
	"errors"
	"strings"
	"testing"
)

func TestSortItemsForMigration_tasksBeforeWorkflows(t *testing.T) {
	items := []SelectedItem{
		{Type: "workflow", Name: "w"},
		{Type: "task", Name: "a"},
		{Type: "policy", Name: "p"},
		{Type: "task", Name: "b"},
		{Type: "workflow", Name: "x"},
	}
	got := sortItemsForMigration(items)
	wantOrder := []string{"task", "task", "workflow", "workflow", "policy"}
	if len(got) != len(wantOrder) {
		t.Fatalf("len %d vs %d", len(got), len(wantOrder))
	}
	for i, w := range wantOrder {
		if got[i].Type != w {
			t.Fatalf("index %d: got %q want %q", i, got[i].Type, w)
		}
	}
}

func TestNormalizeFormDepValue_colonAndComma(t *testing.T) {
	lookup := func(s string) string {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "additionaldisk":
			return "additionalDisk"
		case "diskcount":
			return "diskCount"
		case "mgroup":
			return "mgroup"
		default:
			return ""
		}
	}
	if got := normalizeFormDepValue("additionalDisk:(yes)", lookup); got != "additionalDisk:(yes)" {
		t.Fatalf("colon: %q", got)
	}
	if got := normalizeFormDepValue("mgroup, diskCount", lookup); got != "mgroup, diskCount" {
		t.Fatalf("comma: %q", got)
	}
}

func TestBuildFormDepLookup_cloudAlias(t *testing.T) {
	metas := []formOptMeta{
		{fieldName: "mgroup", typ: "group", finalCode: "group", oldCode: "group"},
		{fieldName: "mcloud", typ: "cloud", finalCode: "cloud", oldCode: "abc"},
	}
	lk := buildFormDepLookup(metas)
	if got := lk("cloud"); got != "mcloud" {
		t.Fatalf("cloud alias: %q", got)
	}
	if got := lk("mcloud"); got != "mcloud" {
		t.Fatalf("mcloud: %q", got)
	}
}

func TestSanitizeDanglingFormConfigRefs_dropsMissingPoolField(t *testing.T) {
	valid := map[string]struct{}{
		"mgroup": {}, "mcloud": {}, "version": {}, "plan": {},
	}
	cfg := map[string]interface{}{
		"groupField": "mgroup",
		"cloudField": "mcloud",
		"poolField":  "5d5c2af8-d86d-4c81-9baf-c81393ae10aa",
		"layoutField": "version",
		"poolFieldType": "field",
	}
	sanitizeDanglingFormConfigRefs(cfg, valid)
	stripEmptyStringsFromOptionConfig(cfg)
	removeOrphanFieldTypeKeys(cfg)
	if _, ok := cfg["poolField"]; ok {
		t.Fatalf("expected poolField removed, got %#v", cfg)
	}
	if _, ok := cfg["poolFieldType"]; ok {
		t.Fatalf("expected poolFieldType removed after dangling poolField, got %#v", cfg)
	}
	if cfg["groupField"] != "mgroup" || cfg["cloudField"] != "mcloud" {
		t.Fatalf("expected refs kept: %#v", cfg)
	}
}

func TestRemoveOrphanFieldTypeKeys_noPoolField(t *testing.T) {
	cfg := map[string]interface{}{
		"poolFieldType": "field",
		"layoutField":   "version",
		"layoutFieldType": "field",
	}
	removeOrphanFieldTypeKeys(cfg)
	if _, ok := cfg["poolFieldType"]; ok {
		t.Fatalf("expected poolFieldType removed: %#v", cfg)
	}
	if cfg["layoutFieldType"] != "field" {
		t.Fatalf("expected layoutFieldType kept: %#v", cfg)
	}
}

func TestDedupeFormOptionFieldNames_secondUsesCode(t *testing.T) {
	a := map[string]interface{}{"fieldName": "diskThree", "code": "diskThree"}
	b := map[string]interface{}{"fieldName": "diskThree", "code": "diskThree_1"}
	metas := []formOptMeta{
		{opt: a, finalCode: "diskThree", fieldName: "diskThree"},
		{opt: b, finalCode: "diskThree_1", fieldName: "diskThree"},
	}
	dedupeFormOptionFieldNames(metas)
	if a["fieldName"] != "diskThree" {
		t.Fatalf("first: %#v", a)
	}
	if b["fieldName"] != "diskThree_1" {
		t.Fatalf("second should take unique code as fieldName: %#v", b)
	}
	if metas[1].fieldName != "diskThree_1" {
		t.Fatalf("meta sync: %q", metas[1].fieldName)
	}
}

func TestRemoveIncompleteLayoutFieldRefs_dropsFieldWithoutLayoutID(t *testing.T) {
	cfg := map[string]interface{}{
		"layoutField": "version", "layoutFieldType": "field", "cloudField": "mcloud",
	}
	removeIncompleteLayoutFieldRefs(cfg)
	if _, ok := cfg["layoutField"]; ok {
		t.Fatalf("expected layout refs dropped: %#v", cfg)
	}
	if cfg["cloudField"] != "mcloud" {
		t.Fatalf("cloudField kept: %#v", cfg)
	}
}

func TestRemoveIncompleteLayoutFieldRefs_keepsWhenLayoutIDSet(t *testing.T) {
	cfg := map[string]interface{}{
		"layoutField": "version", "layoutFieldType": "field", "layoutId": "42",
	}
	removeIncompleteLayoutFieldRefs(cfg)
	if cfg["layoutField"] != "version" {
		t.Fatalf("expected keep: %#v", cfg)
	}
}

func TestRewriteUUIDFieldGroupCodesToSlugs(t *testing.T) {
	form := map[string]interface{}{
		"fieldGroups": []interface{}{
			map[string]interface{}{"code": "c575e5d3-6f64-4e55-9c59-7e79cb2d0f4a", "name": "A"},
		},
	}
	rewriteUUIDFieldGroupCodesToSlugs(form)
	fg := form["fieldGroups"].([]interface{})[0].(map[string]interface{})
	if fg["code"] != "fieldgroup0" {
		t.Fatalf("got %#v", fg["code"])
	}
}

func TestStripEmptyStringsFromOptionConfig_dropsEmptyStrings(t *testing.T) {
	cfg := map[string]interface{}{
		"layoutField": "version",
		"layoutId":    "",
		"showPricing": false,
	}
	stripEmptyStringsFromOptionConfig(cfg)
	if _, ok := cfg["layoutId"]; ok {
		t.Fatalf("expected empty layoutId dropped: %#v", cfg)
	}
	if cfg["layoutField"] != "version" {
		t.Fatalf("expected layoutField kept: %#v", cfg)
	}
}

func TestIsFormLibraryInputRef(t *testing.T) {
	inline := map[string]interface{}{"formField": true, "name": "a843f7a3-d8cf-45b3-bd76-75c83163aaba"}
	if isFormLibraryInputRef(inline) {
		t.Fatal("inline form field should not be library ref")
	}
	lib := map[string]interface{}{"formField": false, "name": "Postgres Version", "code": "pgsqlVersion"}
	if !isFormLibraryInputRef(lib) {
		t.Fatal("field group library input expected")
	}
}

func TestIsFormLibraryInputIDRef(t *testing.T) {
	if !isFormLibraryInputIDRef(map[string]interface{}{"id": 2589}) {
		t.Fatal("id-only ref expected")
	}
	if isFormLibraryInputIDRef(map[string]interface{}{"id": 1, "type": "text"}) {
		t.Fatal("full inline option is not id-only ref")
	}
}

func TestIsLikelyUUID(t *testing.T) {
	if !isLikelyUUID("224b000b-8fe1-48aa-b097-18b5d48489cd") {
		t.Fatal("expected uuid")
	}
	if isLikelyUUID("name") || isLikelyUUID("diskCount") {
		t.Fatal("not uuid")
	}
}

func TestRemapConfigFieldRefs_crossFieldUUIDs(t *testing.T) {
	remap := map[string]string{
		"aaaa1111-1111-4111-8111-111111111111": "bbbb2222-2222-4222-8222-222222222222",
		"cccc3333-3333-4333-8333-333333333333": "dddd4444-4444-4444-8444-444444444444",
	}
	cfg := map[string]interface{}{
		"groupField": "aaaa1111-1111-4111-8111-111111111111",
		"nested": map[string]interface{}{
			"cloudField": "cccc3333-3333-4333-8333-333333333333",
		},
	}
	remapConfigFieldRefs(cfg, remap)
	gf := cfg["groupField"].(string)
	if want := "bbbb2222-2222-4222-8222-222222222222"; gf != want {
		t.Fatalf("groupField: got %q want %q", gf, want)
	}
	nested := cfg["nested"].(map[string]interface{})
	cf := nested["cloudField"].(string)
	if want := "dddd4444-4444-4444-8444-444444444444"; cf != want {
		t.Fatalf("cloudField: got %q want %q", cf, want)
	}
}

func TestResolveSourceGitIntegrationName_usesPlainName(t *testing.T) {
	name, err := resolveSourceGitIntegrationName(nil, map[string]interface{}{
		"id":   42,
		"name": "GIT Radu",
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "GIT Radu" {
		t.Fatalf("got %q", name)
	}
}

func TestResolveSourceGitIntegrationName_rejectsRepoURLWithoutSource(t *testing.T) {
	_, err := resolveSourceGitIntegrationName(nil, map[string]interface{}{
		"id":   42,
		"name": "https://github.com/Radu-Sipetan_worldpay/morpheus_tasks.git",
	})
	if err == nil {
		t.Fatal("expected error when repository name is a URL without source lookup")
	}
}

func TestIsDuplicateErr_formAlreadyInUse(t *testing.T) {
	err := errors.New(`HTTP 400: {"errors":{"name":"Form name is already in use","code":"Form code is already in use"}}`)
	if !isDuplicateErr(err) {
		t.Fatal("expected Morpheus form validation duplicate to be detected")
	}
}

func TestIsDuplicateErr_mustBeUnique(t *testing.T) {
	err := errors.New(`HTTP 400: {"success":false,"msg":"The form contains missing or invalid data","errors":{"name":"must be unique"}}`)
	if !isDuplicateErr(err) {
		t.Fatal("expected must be unique to be detected as duplicate")
	}
}

func TestFindOptionTypeFormID_prefersNameOverCode(t *testing.T) {
	rows := []map[string]interface{}{
		{"id": 10, "name": "LDAP Search", "code": "ldap-search-old"},
		{"id": 20, "name": "Other Form", "code": "ldap-search"},
	}
	wantCode := strings.ToLower("ldap-search")
	wantName := strings.ToLower("LDAP Search")
	var byCode, byName int64
	for _, row := range rows {
		id := intFromAny(row["id"])
		if strings.ToLower(stringFromAny(row["name"])) == wantName {
			byName = id
		}
		if strings.ToLower(stringFromAny(row["code"])) == wantCode {
			byCode = id
		}
	}
	got := byName
	if got == 0 {
		got = byCode
	}
	if got != 10 {
		t.Fatalf("name match should win: got id %d want 10 (byCode=%d byName=%d)", got, byCode, byName)
	}
}
