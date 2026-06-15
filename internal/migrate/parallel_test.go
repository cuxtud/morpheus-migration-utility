package migrate

import (
	"strings"
	"testing"
)

func TestNormalizeType_optionList(t *testing.T) {
	for _, in := range []string{"optionList", "optionlist", "optionTypeList", "optiontypelist", " OptionList "} {
		if got := normalizeType(in); got != "optionList" {
			t.Fatalf("normalizeType(%q) = %q, want optionList", in, got)
		}
	}
}

func TestMigrateOneItem_routesOptionList(t *testing.T) {
	for _, typ := range []string{"optionList", "optionlist"} {
		res := migrateOneItem(migrateItemContext{
			item: SelectedItem{
				Type:    typ,
				Name:    "Site",
				ID:      1,
				RawJSON: `{"name":"Site","type":"manual"}`,
			},
		})
		if res.Status == "skipped" && strings.Contains(res.Message, "not yet supported") {
			t.Fatalf("type %q fell through to generic skip: %s", typ, res.Message)
		}
		if res.Type != "optionList" && normalizeType(typ) == "optionList" {
			t.Fatalf("type %q result type %q", typ, res.Type)
		}
	}
}

func TestNormalizeSelectedItems_canonicalizesTypes(t *testing.T) {
	items := normalizeSelectedItems([]SelectedItem{{Type: "optionlist"}, {Type: "catalogitem"}})
	if items[0].Type != "optionList" || items[1].Type != "catalogItem" {
		t.Fatalf("got %#v", items)
	}
}
