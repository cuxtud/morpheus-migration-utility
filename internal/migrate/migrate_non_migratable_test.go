package migrate

import "testing"

func TestPartitionNonMigratableItems_skipsIntegrations(t *testing.T) {
	items := []SelectedItem{
		{Type: "task", Name: "a"},
		{Type: "integration", Name: "git"},
		{Type: "form", Name: "f"},
	}
	skipped, migratable := partitionNonMigratableItems(items)
	if len(skipped) != 1 || skipped[0].Type != "integration" || skipped[0].Status != "skipped" {
		t.Fatalf("skipped: %+v", skipped)
	}
	if len(migratable) != 2 {
		t.Fatalf("migratable len %d", len(migratable))
	}
}
