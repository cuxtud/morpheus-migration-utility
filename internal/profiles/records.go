package profiles

import (
	"encoding/json"
	"time"

	"github.com/cuxtud/morpheus-migration-utility/internal/migrate"
	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

// MigrationDiscoveryRecord is persisted after POST /api/discover.
type MigrationDiscoveryRecord struct {
	Source    migrate.ApplInfo          `json:"source"`
	Discovery *morpheus.DiscoveryResult `json:"discovery"`
	CreatedAt string                    `json:"createdAt,omitempty"`
}

// MigrationDiscoveryListItem is a light summary for discovery history lists.
type MigrationDiscoveryListItem struct {
	ID              int64  `json:"id"`
	CreatedAt       string `json:"createdAt"`
	ApplianceName   string `json:"applianceName,omitempty"`
	SourceURL       string `json:"sourceUrl,omitempty"`
	SourceProfileID string `json:"sourceProfileId,omitempty"`
	TotalItems      int    `json:"totalItems"`
	Categories      int    `json:"categories"`
	DurationMs      int64  `json:"durationMs,omitempty"`
	MigrationRuns   int    `json:"migrationRuns"`
}

// MigrationRunRecord is persisted after POST /api/migrate.
type MigrationRunRecord struct {
	Request             migrate.MigrateRequest `json:"request"`
	Result              migrate.MigrateResult  `json:"result"`
	StartedAt           string                 `json:"startedAt"`
	FinishedAt          string                 `json:"finishedAt"`
	SourceDiscoveryID   int64                  `json:"sourceDiscoveryId,omitempty"`
	SourceDiscoveryTime string                 `json:"sourceDiscoveryTime,omitempty"`
}

// WorkflowSessionData is the migration UI state saved for restore.
type WorkflowSessionData struct {
	Discovery  *morpheus.DiscoveryResult `json:"discovery"`
	Source     migrate.ApplInfo          `json:"source"`
	Selected   []string                  `json:"selected"`
	SrcURL     string                    `json:"srcUrl,omitempty"`
	SrcToken   string                    `json:"srcToken,omitempty"`
	SrcSkipTLS bool                      `json:"srcSkipTLS,omitempty"`
	SavedAt    string                    `json:"savedAt,omitempty"`
}

func profileToJSON(p Profile) ([]byte, error) {
	return json.Marshal(p)
}

func profileFromJSON(raw []byte) (Profile, error) {
	var p Profile
	err := json.Unmarshal(raw, &p)
	return p, err
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
