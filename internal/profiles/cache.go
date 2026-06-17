package profiles

// ClearCacheOptions selects which cached data to remove. Profiles are never deleted.
type ClearCacheOptions struct {
	FleetSnapshots       bool `json:"fleetSnapshots"`
	MigrationDiscoveries bool `json:"migrationDiscoveries"`
	MigrationRuns        bool `json:"migrationRuns"`
	WorkflowSessions     bool `json:"workflowSessions"`
}

// ClearCacheResult reports how many rows were removed per category.
type ClearCacheResult struct {
	FleetSnapshots       int64 `json:"fleetSnapshots"`
	MigrationDiscoveries int64 `json:"migrationDiscoveries"`
	MigrationRuns        int64 `json:"migrationRuns"`
	WorkflowSessions     int64 `json:"workflowSessions"`
	Postgres             bool  `json:"postgres"`
}

func (o *ClearCacheOptions) Normalize() {
	if o == nil {
		return
	}
	if !o.FleetSnapshots && !o.MigrationDiscoveries && !o.MigrationRuns && !o.WorkflowSessions {
		o.FleetSnapshots = true
		o.MigrationDiscoveries = true
		o.MigrationRuns = true
		o.WorkflowSessions = true
	}
}
