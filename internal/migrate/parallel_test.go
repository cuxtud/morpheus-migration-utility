package migrate

import "testing"

func TestPartitionCatalogItems(t *testing.T) {
	items := []SelectedItem{
		{Type: "catalogItem", Name: "a"},
		{Type: "instanceType", Name: "it"},
		{Type: "catalogItem", Name: "b"},
		{Type: "input", Name: "in"},
	}
	serial, catalogs := partitionCatalogItems(items)
	if len(serial) != 2 || len(catalogs) != 2 {
		t.Fatalf("serial=%d catalogs=%d", len(serial), len(catalogs))
	}
	if serial[0].Name != "it" || serial[1].Name != "in" {
		t.Fatalf("serial order: %#v", serial)
	}
}

func TestCatalogParallelWorkers(t *testing.T) {
	req := MigrateRequest{}
	if got := catalogParallelWorkers(req, 1); got != 1 {
		t.Fatalf("single catalog: %d", got)
	}
	if got := catalogParallelWorkers(req, 10); got != defaultCatalogParallelism {
		t.Fatalf("auto: %d want %d", got, defaultCatalogParallelism)
	}
	req.ParallelCatalog = 1
	if got := catalogParallelWorkers(req, 10); got != 1 {
		t.Fatalf("sequential override: %d", got)
	}
	req.ParallelCatalog = 8
	if got := catalogParallelWorkers(req, 3); got != 3 {
		t.Fatalf("cap at catalog count: %d", got)
	}
}
