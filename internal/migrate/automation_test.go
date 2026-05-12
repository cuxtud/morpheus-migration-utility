package migrate

import (
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
	if _, ok := cfg["poolField"]; ok {
		t.Fatalf("expected poolField removed, got %#v", cfg)
	}
	if cfg["groupField"] != "mgroup" || cfg["cloudField"] != "mcloud" {
		t.Fatalf("expected refs kept: %#v", cfg)
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

func TestIntegrationSearchPhrases(t *testing.T) {
	p := integrationSearchPhrases("python_examples")
	if len(p) < 2 {
		t.Fatalf("expected multiple phrases, got %v", p)
	}
	if p[0] != "python_examples" {
		t.Fatalf("first phrase: %v", p)
	}
}
