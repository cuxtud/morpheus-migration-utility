-- JSONB-first storage: full documents in data columns; minimal columns for keys and ordering.

CREATE TABLE IF NOT EXISTS appliance_profiles (
    id          TEXT PRIMARY KEY,
    data        JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_appliance_profiles_name ON appliance_profiles ((data->>'name'));
CREATE INDEX IF NOT EXISTS idx_appliance_profiles_url ON appliance_profiles ((LOWER(data->>'url')));

-- Fleet / profile discovery: full ApplianceSnapshot JSONB
CREATE TABLE IF NOT EXISTS appliance_discoveries (
    id          BIGSERIAL PRIMARY KEY,
    profile_id  TEXT NOT NULL REFERENCES appliance_profiles(id) ON DELETE CASCADE,
    data        JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_appliance_discoveries_profile_at
    ON appliance_discoveries (profile_id, created_at DESC);

-- Migration wizard source discovery (DiscoveryResult + source connection metadata)
CREATE TABLE IF NOT EXISTS migration_discoveries (
    id          BIGSERIAL PRIMARY KEY,
    data        JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_migration_discoveries_created
    ON migration_discoveries (created_at DESC);

-- Migration execution history (request + result JSONB)
CREATE TABLE IF NOT EXISTS migration_runs (
    id          BIGSERIAL PRIMARY KEY,
    data        JSONB NOT NULL,
    source_discovery_id BIGINT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE migration_runs ADD COLUMN IF NOT EXISTS source_discovery_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_migration_runs_created ON migration_runs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_migration_runs_source_discovery ON migration_runs (source_discovery_id);

-- Migration workflow session (discovery, selection, source — full UI state)
CREATE TABLE IF NOT EXISTS workflow_sessions (
    id          TEXT PRIMARY KEY,
    data        JSONB NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
