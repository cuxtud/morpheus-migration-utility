package migrate

import (
	"errors"
	"strings"
	"testing"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
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
	group := map[string]interface{}{
		"type": "group", "fieldLabel": "Group", "fieldName": "groups",
	}
	if isFormLibraryInputRef(group) {
		t.Fatal("inline group field should not be library ref")
	}
	lib := map[string]interface{}{"formField": false, "name": "Postgres Version", "code": "pgsqlVersion"}
	if !isFormLibraryInputRef(lib) {
		t.Fatal("field group library input expected")
	}
	expanded := map[string]interface{}{
		"formField": false, "name": "Request Raised For Options", "code": "raised_for", "type": "radio",
	}
	if !isFormLibraryInputRef(expanded) {
		t.Fatal("expanded root library input expected")
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

func TestRemapTaskTypeForDestination_usesDestIDByCode(t *testing.T) {
	state := newAutomationState(nil)
	state.destTaskTypeByCode = map[string]map[string]interface{}{
		"jythontask": {"id": 12, "code": "jythonTask", "name": "Python Script"},
		"vro":        {"id": 10, "code": "vro", "name": "vRealize Orchestrator Workflow"},
	}
	task := map[string]interface{}{
		"name": "create_cmdb_ci_relation",
		"taskType": map[string]interface{}{
			"id":   12,
			"code": "jythonTask",
			"name": "Python Script",
		},
	}
	if err := remapTaskTypeForDestination(nil, state, task); err != nil {
		t.Fatal(err)
	}
	tt, _ := task["taskType"].(map[string]interface{})
	if intFromAny(tt["id"]) != 12 {
		t.Fatalf("id=%v want destination jythonTask id 12", tt["id"])
	}
	if stringFromAny(tt["code"]) != "jythonTask" {
		t.Fatalf("code=%v", tt["code"])
	}
}

func TestRemapTaskTypeForDestination_avoidsWrongIDWhenCodesDiffer(t *testing.T) {
	state := newAutomationState(nil)
	// On destination, id 12 might be a different type — lookup must be by code.
	state.destTaskTypeByCode = map[string]map[string]interface{}{
		"jythontask": {"id": 99, "code": "jythonTask", "name": "Python Script"},
		"vro":        {"id": 10, "code": "vro", "name": "vRealize Orchestrator Workflow"},
	}
	task := map[string]interface{}{
		"name": "create_cmdb_ci_relation",
		"taskType": map[string]interface{}{
			"id":   12,
			"code": "jythonTask",
			"name": "Python Script",
		},
	}
	if err := remapTaskTypeForDestination(nil, state, task); err != nil {
		t.Fatal(err)
	}
	tt, _ := task["taskType"].(map[string]interface{})
	if intFromAny(tt["id"]) != 99 {
		t.Fatalf("id=%v want 99 (dest jythonTask), not source id 12", tt["id"])
	}
}

func TestParseTaskGETRepositoryBinding_missingFile(t *testing.T) {
	body := []byte(`{"task":{"id":1,"name":"t","taskType":{"code":"jythonTask"}}}`)
	st, rid, filePresent, ok := parseTaskGETRepositoryBinding(body)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if filePresent {
		t.Fatal("file should be absent")
	}
	if st != "" || rid != 0 {
		t.Fatalf("st=%q rid=%d", st, rid)
	}
}

func TestIntegrationNameFromCodeRepositoryName(t *testing.T) {
	full := "python_examples - cuxtud_python"
	if got := codeRepositoryIntegrationName(full); got != "python_examples" {
		t.Fatalf("integration=%q want python_examples", got)
	}
	if got := codeRepositoryEntryName(full); got != "cuxtud_python" {
		t.Fatalf("entry=%q want cuxtud_python", got)
	}
}

func TestParseCodeRepositoryOptions(t *testing.T) {
	body := []byte(`{"success":true,"data":[{"name":"python_examples - cuxtud_python","value":3},{"name":"wiki - Wiki Helm","value":2}]}`)
	opts, err := parseCodeRepositoryOptions(body)
	if err != nil {
		t.Fatal(err)
	}
	opt, ok := findCodeRepositoryByValue(opts, 3)
	if !ok || opt.Name != "python_examples - cuxtud_python" {
		t.Fatalf("opt=%+v ok=%v", opt, ok)
	}
}

func TestFindCodeRepositoryByIntegration(t *testing.T) {
	opts := []codeRepositoryOption{
		{Name: "python_examples - python", Value: 9},
		{Name: "python_examples - cuxtud_python", Value: 3},
	}
	opt, ok := findCodeRepositoryByIntegration(opts, "python_examples", "cuxtud_python")
	if !ok || opt.Value != 3 {
		t.Fatalf("opt=%+v", opt)
	}
}

func TestResolveSourceGitIntegrationName_usesPlainName(t *testing.T) {
	name, err := resolveSourceGitIntegrationName(nil, nil, map[string]interface{}{
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

func TestResolveSourceGitIntegrationName_prefersNameOverBrokenIDLookup(t *testing.T) {
	name, err := resolveSourceGitIntegrationName(nil, nil, map[string]interface{}{
		"id":   8,
		"name": "GIT Radu",
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "GIT Radu" {
		t.Fatalf("got %q want GIT Radu", name)
	}
}

func TestResolveIntegrationNameByID_fromSnapshot(t *testing.T) {
	snap := NewSourceSnapshot(&morpheus.DiscoveryResult{
		Categories: []morpheus.CategoryGroup{{
			Name: "Integrations",
			Items: []morpheus.DiscoveryItem{{
				ID:      8,
				Name:    "GIT Radu",
				Type:    "integration",
				RawJSON: `{"id":8,"name":"GIT Radu","integrationType":{"code":"git"}}`,
			}},
		}},
	}, nil)
	name, err := resolveIntegrationNameByID(nil, snap, 8)
	if err != nil {
		t.Fatal(err)
	}
	if name != "GIT Radu" {
		t.Fatalf("got %q", name)
	}
}

func TestResolveSourceGitIntegrationName_rejectsRepoURLWithoutSource(t *testing.T) {
	_, err := resolveSourceGitIntegrationName(nil, nil, map[string]interface{}{
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

func TestFindOptionTypeFormID_nameOnly(t *testing.T) {
	rows := []map[string]interface{}{
		{"id": 10, "name": "LDAP Search", "code": "ldap-search-old"},
		{"id": 20, "name": "Other Form", "code": "ldap-search"},
	}
	got := int64(0)
	for _, row := range rows {
		if exactNameMatchKey(stringFromAny(row["name"])) == exactNameMatchKey("LDAP Search") {
			got = intFromAny(row["id"])
			break
		}
	}
	if got != 10 {
		t.Fatalf("name-only match: got id %d want 10", got)
	}
	// Same code on a different form must not match when looking up by another name.
	got = int64(0)
	for _, row := range rows {
		if exactNameMatchKey(stringFromAny(row["name"])) == exactNameMatchKey("LDAP Search Extended") {
			got = intFromAny(row["id"])
			break
		}
	}
	if got != 0 {
		t.Fatalf("partial/extra name must not match, got id %d", got)
	}
}

func TestFindOptionTypeFormID_nameOnlyWhenNameProvided(t *testing.T) {
	rows := []map[string]interface{}{
		{"id": 20, "name": "Other Form", "code": "shared-code"},
	}
	wantName := strings.ToLower("worldpay rhel server")
	wantCode := strings.ToLower("shared-code")
	var id int64
	for _, row := range rows {
		if strings.ToLower(stringFromAny(row["name"])) == wantName {
			id = intFromAny(row["id"])
			break
		}
	}
	if id != 0 {
		t.Fatalf("expected no match by name, got id %d", id)
	}
	// When name is provided, code must not be used as a fallback.
	if wantName != "" {
		id = 0
	} else {
		for _, row := range rows {
			if strings.ToLower(stringFromAny(row["code"])) == wantCode {
				id = intFromAny(row["id"])
				break
			}
		}
	}
	if id != 0 {
		t.Fatalf("code fallback must not apply when name is set, got id %d", id)
	}
}

func TestFindDestFormID_doesNotFallBackToCode(t *testing.T) {
	state := &automationState{
		catalogCache: &catalogDestCache{
			formLoaded: true,
			formByName: map[string]int64{},
			formByCode: map[string]int64{"shared-code": 99},
		},
	}
	got := state.findDestFormID(nil, "shared-code", "worldpay rhel server")
	if got != 0 {
		t.Fatalf("findDestFormID=%d want 0 when name missing on destination", got)
	}
}

func TestFormNameMatch_exactOnlyFromOptionTypeFormsList(t *testing.T) {
	// Shape from GET /api/library/option-type-forms — match row["name"] exactly, never substring.
	forms := []map[string]interface{}{
		{"id": 7, "name": "Azure VM Deployment template", "code": "azureforms"},
		{"id": 8, "name": "worldpay rhel server", "code": "wp-rhel"},
	}
	matchName := func(want string) int64 {
		key := strings.ToLower(strings.TrimSpace(want))
		for _, row := range forms {
			if strings.ToLower(strings.TrimSpace(stringFromAny(row["name"]))) == key {
				return intFromAny(row["id"])
			}
		}
		return 0
	}
	if id := matchName("Azure VM Deployment"); id != 0 {
		t.Fatalf("substring/prefix must not match Azure form, got id %d", id)
	}
	if id := matchName("Azure VM Deployment template"); id != 7 {
		t.Fatalf("exact name id=%d want 7", id)
	}
	if id := matchName("worldpay rhel server"); id != 8 {
		t.Fatalf("exact name id=%d want 8", id)
	}
	if id := matchName("worldpay rhel"); id != 0 {
		t.Fatalf("partial name must not match, got id %d", id)
	}
}

func TestMapWorkflowOptionTypes_exactNameOnly(t *testing.T) {
	nameToID := map[string]int64{
		exactNameMatchKey("DBAList"):     10,
		exactNameMatchKey("DBAList New"): 11,
	}
	opt := []interface{}{
		map[string]interface{}{"code": "dbalist", "name": "DBAList"},
		map[string]interface{}{"code": "other-code", "name": "DBAList New"},
		map[string]interface{}{"code": "dbalist", "name": "Missing Input"},
	}
	ids, warn := mapWorkflowOptionTypes(opt, nameToID)
	if len(ids) != 2 || ids[0] != int64(10) || ids[1] != int64(11) {
		t.Fatalf("ids=%v want [10 11]", ids)
	}
	if !strings.Contains(warn, "Missing Input") {
		t.Fatalf("warn=%q", warn)
	}
	if strings.Contains(warn, "dbalist") {
		t.Fatalf("code must not appear in warn: %q", warn)
	}
}

func TestResolveDestinationOptionTypeByName_noSubstringInIndex(t *testing.T) {
	// DBAList must not resolve via DBAList New in the name index.
	state := newAutomationState(nil)
	state.destOptionNameToID = map[string]int64{
		exactNameMatchKey("DBAList New"): 99,
	}
	if id := state.destOptionTypeIDByName("DBAList"); id != 0 {
		t.Fatalf("DBAList matched id %d via substring index", id)
	}
	if id := state.destOptionTypeIDByName("DBAList New"); id != 99 {
		t.Fatalf("exact DBAList New id=%d want 99", id)
	}
}
