package profiles

import (
	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

// Repository persists all application data (PostgreSQL JSONB when configured).
type Repository interface {
	List() ([]Profile, error)
	Find(id string) (*Profile, error)
	Upsert(p Profile) (Profile, error)
	Delete(id string) (bool, error)
	ListPublic() ([]PublicView, error)

	SaveSnapshot(profileID string, snap *morpheus.ApplianceSnapshot) error
	LatestSnapshot(profileID string) (*morpheus.ApplianceSnapshot, error)
	LatestSnapshots() (map[string]*morpheus.ApplianceSnapshot, error)
	DeleteSnapshots(profileID string) error

	SaveMigrationDiscovery(rec *MigrationDiscoveryRecord) (int64, error)
	LatestMigrationDiscovery() (*MigrationDiscoveryRecord, error)
	LoadMigrationDiscovery(id int64) (*MigrationDiscoveryRecord, error)
	ListMigrationDiscoveries(limit int) ([]MigrationDiscoveryListItem, error)
	DeleteMigrationDiscovery(id int64) error

	SaveMigrationRun(rec *MigrationRunRecord, sourceDiscoveryID int64) (int64, error)
	ListMigrationRuns(limit int) ([]MigrationRunRecord, error)

	SaveWorkflowSession(id string, data *WorkflowSessionData) error
	LoadWorkflowSession(id string) (*WorkflowSessionData, error)
	DeleteWorkflowSession(id string) error
	LatestWorkflowSession() (*WorkflowSessionData, string, error)

	// SupportsJSONB is true when the backend persists full JSON documents (Postgres).
	SupportsJSONB() bool
}

// NoopExtras provides stub implementations for file-backed storage.
type NoopExtras struct{}

func (NoopExtras) SaveMigrationDiscovery(*MigrationDiscoveryRecord) (int64, error) {
	return 0, ErrDBRequired
}
func (NoopExtras) LatestMigrationDiscovery() (*MigrationDiscoveryRecord, error) {
	return nil, ErrDBRequired
}
func (NoopExtras) LoadMigrationDiscovery(int64) (*MigrationDiscoveryRecord, error) {
	return nil, ErrDBRequired
}
func (NoopExtras) ListMigrationDiscoveries(int) ([]MigrationDiscoveryListItem, error) {
	return nil, ErrDBRequired
}
func (NoopExtras) DeleteMigrationDiscovery(int64) error { return ErrDBRequired }
func (NoopExtras) SaveMigrationRun(*MigrationRunRecord, int64) (int64, error) {
	return 0, ErrDBRequired
}
func (NoopExtras) ListMigrationRuns(int) ([]MigrationRunRecord, error) {
	return nil, ErrDBRequired
}
func (NoopExtras) SaveWorkflowSession(string, *WorkflowSessionData) error { return ErrDBRequired }
func (NoopExtras) LoadWorkflowSession(string) (*WorkflowSessionData, error) {
	return nil, ErrDBRequired
}
func (NoopExtras) DeleteWorkflowSession(string) error { return ErrDBRequired }
func (NoopExtras) LatestWorkflowSession() (*WorkflowSessionData, string, error) {
	return nil, "", ErrDBRequired
}
func (NoopExtras) SupportsJSONB() bool { return false }
