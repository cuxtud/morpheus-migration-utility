package profiles

import "testing"

func TestClearCacheOptions_Normalize_defaultsAll(t *testing.T) {
	opts := ClearCacheOptions{}
	opts.Normalize()
	if !opts.FleetSnapshots || !opts.MigrationDiscoveries || !opts.MigrationRuns || !opts.WorkflowSessions {
		t.Fatalf("expected all true after normalize: %+v", opts)
	}
}

func TestClearCacheOptions_Normalize_respectsExplicit(t *testing.T) {
	opts := ClearCacheOptions{FleetSnapshots: true}
	opts.Normalize()
	if !opts.FleetSnapshots || opts.MigrationDiscoveries || opts.MigrationRuns || opts.WorkflowSessions {
		t.Fatalf("expected only fleet: %+v", opts)
	}
}
