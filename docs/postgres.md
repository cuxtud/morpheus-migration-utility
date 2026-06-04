# PostgreSQL storage

When **`DATABASE_URL`** is set, profiles, fleet discoveries, migration discoveries, migration runs, and workflow sessions are stored in **PostgreSQL JSONB**.

## Quick start (Docker)

```bash
docker compose up -d postgres
export DATABASE_URL='postgres://morpheus:morpheus@localhost:5432/morpheus_snapshot?sslmode=disable'
go run ./cmd/server
# or: sudo -E ./morpheus-snapshot-linux-amd64
```

Schema is applied automatically on startup. On first connect to an empty database, existing `appliance-profiles.json` is imported once.

## Tables

| Table | JSONB document |
|-------|----------------|
| `appliance_profiles` | Profile (name, url, token, credentials, skipTls) |
| `appliance_discoveries` | Full fleet `ApplianceSnapshot` (discovery + license + health) |
| `migration_discoveries` | Source connection + `DiscoveryResult` |
| `migration_runs` | Migration request + results + `source_discovery_id` |
| `workflow_sessions` | Saved migration UI state |

## APIs (when Postgres is enabled)

| Endpoint | Purpose |
|----------|---------|
| `GET /api/storage` | `{ "postgres": true, "jsonb": true }` |
| `GET /api/discoveries` | List saved migration discoveries |
| `GET /api/discoveries?id=` | Load one discovery |
| `DELETE /api/discoveries?id=` | Delete a discovery |
| `GET/POST/DELETE /api/session` | Workflow session restore |

## Without PostgreSQL

- Profiles: `appliance-profiles.json` (mode `0600`)
- Fleet discovery: in-memory until restart
- Migration discoveries: not persisted to the server

Copy [`.env.example`](https://github.com/cuxtud/morpheus-migration-utility/blob/master/.env.example) for local settings.
