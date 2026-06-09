package profiles

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

// PostgresRepository stores all records as JSONB documents in PostgreSQL.
type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(4)
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Close() error {
	return r.db.Close()
}

func (r *PostgresRepository) SupportsJSONB() bool { return true }

func (r *PostgresRepository) ImportFromFileIfEmpty(path string) error {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM appliance_profiles`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	s, err := Load(path)
	if err != nil {
		return err
	}
	for _, p := range s.Profiles {
		if _, err := r.Upsert(p); err != nil {
			return fmt.Errorf("import profile %q: %w", p.Name, err)
		}
	}
	return nil
}

func (r *PostgresRepository) List() ([]Profile, error) {
	rows, err := r.db.Query(`
		SELECT data FROM appliance_profiles
		ORDER BY data->>'name'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		p, err := profileFromJSON(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) Find(id string) (*Profile, error) {
	var raw []byte
	err := r.db.QueryRow(`SELECT data FROM appliance_profiles WHERE id = $1`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p, err := profileFromJSON(raw)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PostgresRepository) Upsert(p Profile) (Profile, error) {
	isNew := strings.TrimSpace(p.ID) == ""
	var existing *Profile
	if !isNew {
		ex, err := r.Find(p.ID)
		if err != nil && err != ErrNotFound {
			return Profile{}, err
		}
		existing = ex
		if existing == nil {
			return Profile{}, ErrNotFound
		}
	}
	hadPassword := existing != nil && existing.Password != ""
	hadToken := existing != nil && existing.Token != ""
	if err := p.ValidateSave(isNew, hadPassword, hadToken); err != nil {
		return Profile{}, err
	}
	if p.ID == "" {
		p.ID = NewID()
	}
	if existing != nil {
		if strings.TrimSpace(p.Password) == "" {
			p.Password = existing.Password
		}
		if strings.TrimSpace(p.Token) == "" && p.Username == "" {
			p.Token = existing.Token
		}
	}
	// Password-auth profile must not keep a stale API token (Client prefers token over password).
	if p.Token != "" {
		p.Password = ""
		p.Username = ""
	} else if p.Username != "" && p.Password != "" {
		p.Token = ""
	}
	raw, err := profileToJSON(p)
	if err != nil {
		return Profile{}, err
	}
	_, err = r.db.Exec(`
		INSERT INTO appliance_profiles (id, data, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()`,
		p.ID, string(raw))
	if err != nil {
		return Profile{}, err
	}
	return p, nil
}

func (r *PostgresRepository) Delete(id string) (bool, error) {
	res, err := r.db.Exec(`DELETE FROM appliance_profiles WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *PostgresRepository) ListPublic() ([]PublicView, error) {
	list, err := r.List()
	if err != nil {
		return nil, err
	}
	v := make([]PublicView, 0, len(list))
	for _, p := range list {
		v = append(v, p.Public())
	}
	return v, nil
}

func (r *PostgresRepository) SaveSnapshot(profileID string, snap *morpheus.ApplianceSnapshot) error {
	if snap == nil {
		return nil
	}
	if snap.DiscoveredAt == "" {
		snap.DiscoveredAt = nowRFC3339()
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO appliance_discoveries (profile_id, data)
		VALUES ($1, $2::jsonb)`,
		profileID, string(raw))
	return err
}

func (r *PostgresRepository) LatestSnapshot(profileID string) (*morpheus.ApplianceSnapshot, error) {
	var raw []byte
	err := r.db.QueryRow(`
		SELECT data FROM appliance_discoveries
		WHERE profile_id = $1
		ORDER BY created_at DESC
		LIMIT 1`, profileID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var snap morpheus.ApplianceSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (r *PostgresRepository) LatestSnapshots() (map[string]*morpheus.ApplianceSnapshot, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT ON (profile_id) profile_id, data
		FROM appliance_discoveries
		ORDER BY profile_id, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]*morpheus.ApplianceSnapshot)
	for rows.Next() {
		var profileID string
		var raw []byte
		if err := rows.Scan(&profileID, &raw); err != nil {
			return nil, err
		}
		var snap morpheus.ApplianceSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return nil, err
		}
		out[profileID] = &snap
	}
	return out, rows.Err()
}

func (r *PostgresRepository) DeleteSnapshots(profileID string) error {
	_, err := r.db.Exec(`DELETE FROM appliance_discoveries WHERE profile_id = $1`, profileID)
	return err
}

func (r *PostgresRepository) SaveMigrationDiscovery(rec *MigrationDiscoveryRecord) (int64, error) {
	if rec == nil {
		return 0, nil
	}
	if rec.CreatedAt == "" {
		rec.CreatedAt = nowRFC3339()
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.db.QueryRow(`
		INSERT INTO migration_discoveries (data) VALUES ($1::jsonb) RETURNING id`,
		string(raw)).Scan(&id)
	return id, err
}

func (r *PostgresRepository) LatestMigrationDiscovery() (*MigrationDiscoveryRecord, error) {
	var raw []byte
	err := r.db.QueryRow(`
		SELECT data FROM migration_discoveries
		ORDER BY created_at DESC LIMIT 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var rec MigrationDiscoveryRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *PostgresRepository) LoadMigrationDiscovery(id int64) (*MigrationDiscoveryRecord, error) {
	var raw []byte
	err := r.db.QueryRow(`
		SELECT data FROM migration_discoveries
		WHERE id = $1`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var rec MigrationDiscoveryRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *PostgresRepository) ListMigrationDiscoveries(limit int) ([]MigrationDiscoveryListItem, error) {
	if limit <= 0 {
		limit = 100
	}
	profileNames := map[string]string{}
	if list, err := r.List(); err == nil {
		for _, p := range list {
			profileNames[p.ID] = p.Name
		}
	}
	rows, err := r.db.Query(`
		SELECT d.id, d.data, d.created_at, COUNT(r.id) AS run_count
		FROM migration_discoveries d
		LEFT JOIN migration_runs r ON r.source_discovery_id = d.id
		GROUP BY d.id, d.data, d.created_at
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MigrationDiscoveryListItem
	for rows.Next() {
		var id int64
		var raw []byte
		var createdAt string
		var runCount int
		if err := rows.Scan(&id, &raw, &createdAt, &runCount); err != nil {
			return nil, err
		}
		var rec MigrationDiscoveryRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return nil, err
		}
		item := MigrationDiscoveryListItem{
			ID:              id,
			CreatedAt:       rec.CreatedAt,
			SourceURL:       rec.Source.URL,
			SourceProfileID: rec.Source.ProfileID,
			MigrationRuns:   runCount,
		}
		if rec.Source.ProfileID != "" {
			item.ApplianceName = profileNames[rec.Source.ProfileID]
		}
		if item.ApplianceName == "" && rec.Source.URL != "" {
			item.ApplianceName = rec.Source.URL
		}
		if item.CreatedAt == "" {
			item.CreatedAt = createdAt
		}
		if rec.Discovery != nil {
			item.TotalItems = rec.Discovery.Total
			item.Categories = len(rec.Discovery.Categories)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) DeleteMigrationDiscovery(id int64) error {
	_, err := r.db.Exec(`DELETE FROM migration_discoveries WHERE id = $1`, id)
	return err
}

func (r *PostgresRepository) SaveMigrationRun(rec *MigrationRunRecord, sourceDiscoveryID int64) (int64, error) {
	if rec == nil {
		return 0, nil
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.db.QueryRow(`
		INSERT INTO migration_runs (data, source_discovery_id) VALUES ($1::jsonb, $2) RETURNING id`,
		string(raw), sourceDiscoveryID).Scan(&id)
	return id, err
}

func (r *PostgresRepository) ListMigrationRuns(limit int) ([]MigrationRunRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(`
		SELECT data FROM migration_runs
		ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MigrationRunRecord
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var rec MigrationRunRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveWorkflowSession(id string, data *WorkflowSessionData) error {
	if id == "" || data == nil {
		return fmt.Errorf("session id and data required")
	}
	if data.SavedAt == "" {
		data.SavedAt = nowRFC3339()
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO workflow_sessions (id, data, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()`,
		id, string(raw))
	return err
}

func (r *PostgresRepository) LoadWorkflowSession(id string) (*WorkflowSessionData, error) {
	var raw []byte
	err := r.db.QueryRow(`SELECT data FROM workflow_sessions WHERE id = $1`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var data WorkflowSessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (r *PostgresRepository) DeleteWorkflowSession(id string) error {
	_, err := r.db.Exec(`DELETE FROM workflow_sessions WHERE id = $1`, id)
	return err
}

func (r *PostgresRepository) LatestWorkflowSession() (*WorkflowSessionData, string, error) {
	var id string
	var raw []byte
	err := r.db.QueryRow(`
		SELECT id, data FROM workflow_sessions
		ORDER BY updated_at DESC LIMIT 1`).Scan(&id, &raw)
	if err == sql.ErrNoRows {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	var data WorkflowSessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, "", err
	}
	return &data, id, nil
}
