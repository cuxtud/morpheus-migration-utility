package db

import (
	"context"
	"database/sql"
)

// upgradeLegacySchema drops pre-JSONB tables when the old relational columns exist.
func upgradeLegacySchema(ctx context.Context, db *sql.DB) error {
	var hasLegacy bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'appliance_profiles' AND column_name = 'username'
		)`).Scan(&hasLegacy)
	if err != nil || !hasLegacy {
		return err
	}
	_, err = db.ExecContext(ctx, `
		DROP TABLE IF EXISTS appliance_discoveries CASCADE;
		DROP TABLE IF EXISTS appliance_profiles CASCADE`)
	return err
}
