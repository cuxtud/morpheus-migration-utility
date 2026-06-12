package migrate

import (
	"testing"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

func TestNewSourceSnapshot_cloudByID(t *testing.T) {
	snap := NewSourceSnapshot(&morpheus.DiscoveryResult{
		Categories: []morpheus.CategoryGroup{{
			Name: "Clouds",
			Items: []morpheus.DiscoveryItem{{
				ID:      7,
				Name:    "denver",
				Type:    "cloud",
				RawJSON: `{"id":7,"name":"denver","code":"vmware","zoneType":"vmware"}`,
			}},
		}},
	}, nil)

	obj, ok := snap.LookupObject("cloud", 7)
	if !ok {
		t.Fatal("expected cloud in snapshot")
	}
	if got := stringFromAny(obj["code"]); got != "vmware" {
		t.Fatalf("code=%q", got)
	}
}

func TestNewSourceSnapshot_selectedOverridesList(t *testing.T) {
	snap := NewSourceSnapshot(nil, []SelectedItem{{
		Type:    "catalogItem",
		ID:      99,
		Name:    "DB",
		RawJSON: `{"id":99,"name":"DB","code":"db","config":{"type":"mysql"}}`,
	}})

	obj, ok := snap.LookupObject("catalogItem", 99)
	if !ok || !catalogItemLooksComplete(obj) {
		t.Fatalf("catalog snapshot missing config: %#v", obj)
	}
}

func TestGroupMigrationWaves(t *testing.T) {
	items := []SelectedItem{
		{Type: "workflow", Name: "w1"},
		{Type: "task", Name: "t2"},
		{Type: "task", Name: "t1"},
		{Type: "input", Name: "i1"},
		{Type: "input", Name: "i2"},
	}
	waves := groupMigrationWaves(items)
	if len(waves) != 3 {
		t.Fatalf("waves=%d", len(waves))
	}
	if len(waves[0]) != 2 || waves[0][0].Type != "task" {
		t.Fatalf("wave0=%#v", waves[0])
	}
	if len(waves[1]) != 1 || waves[1][0].Type != "workflow" {
		t.Fatalf("wave1=%#v", waves[1])
	}
	if len(waves[2]) != 2 || waves[2][0].Type != "input" {
		t.Fatalf("wave2=%#v", waves[2])
	}
}

func TestFindInstanceTypeByCode_fromSnapshot(t *testing.T) {
	snap := NewSourceSnapshot(&morpheus.DiscoveryResult{
		Categories: []morpheus.CategoryGroup{{
			Name: "Instance Types",
			Items: []morpheus.DiscoveryItem{{
				ID:      5,
				Name:    "Linux",
				Type:    "instanceType",
				RawJSON: `{"id":5,"name":"Linux","code":"linux","instanceTypeLayouts":[]}`,
			}},
		}},
	}, nil)
	it, ok := snap.FindInstanceTypeByCode("linux")
	if !ok || it.ID != 5 {
		t.Fatalf("lookup=%v ok=%v", it, ok)
	}
}
