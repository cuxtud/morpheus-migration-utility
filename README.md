# Morpheus Snapshot Utility 🔄

A self-contained tool for HPE Morpheus — **fleet inventory** across many appliances (discovery, license, health) and **migration** from a source appliance to a destination.

## Features

### Appliance profiles (shared)
- **Appliance profiles** are saved once and used in both **Migration** and **Fleet inventory**
- Auth per profile: **API token** or **username + password** (OAuth via `morph-api`)
- **PostgreSQL (JSONB)** when `DATABASE_URL` is set — profiles + full discovery snapshots persist across restarts
- Without Postgres: `appliance-profiles.json` (mode `0600`) and in-memory discovery cache; legacy `connections.json` / `appliances.json` merge on first run

### Fleet inventory (multi-appliance)
- Pick a saved profile or **discover all** profiles: resource inventory, **`GET /api/license`**, and health (`/api/health` where available)
- Dashboard cards per appliance: resource counts, license summary, health status

### Migration
- **Discovery** across Morpheus library and automation types:
  - Clouds, Integrations, Instance Types, Layouts, Node Types
  - Tasks, Workflows, Inputs, Option Lists, Forms
  - Catalog Items, Blueprints, Apps
  - Tenants, Roles, Users, Policies, Groups, Cypher
- **Grouped checkbox UI** — select individual items or entire categories
- **Live search & filter** across discovered items
- **Migration with results** — per-item success/skip/fail with export to JSON
- **Single binary** — no runtime, no Docker, no dependencies
- **HTTPS on port 443** — auto-generates a self-signed TLS cert on first run

---

## PostgreSQL storage

Discovery snapshots and appliance profiles are stored in Postgres when you set **`DATABASE_URL`**.

```bash
# Local database (Docker)
docker compose up -d postgres
export DATABASE_URL=postgres://morpheus:morpheus@localhost:5432/morpheus_snapshot?sslmode=disable

# Run the server (schema is applied automatically on startup)
go run ./cmd/server
```

All persisted rows use a **`data` JSONB** column (full document). Tables (created automatically):

| Table | JSONB document |
|-------|----------------|
| `appliance_profiles` | Full profile (name, url, token, password, skipTls) |
| `appliance_discoveries` | Full `ApplianceSnapshot` (discovery + license + health) |
| `migration_discoveries` | Source connection + `DiscoveryResult` from migration wizard |
| `migration_runs` | Migration request + results |
| `workflow_sessions` | Saved migration UI state (discovery, selection, source) |

On first connect with an empty database, existing `appliance-profiles.json` is imported once.

APIs:
- `GET /api/profiles/snapshots` — latest fleet discovery per profile
- `GET/POST/DELETE /api/session` — workflow session in Postgres (UI “Remember discovery” when `DATABASE_URL` is set)
- `GET /api/storage` — `{ "postgres": true, "jsonb": true }`

Copy [`.env.example`](.env.example) for local settings.

---

## Quick Start

### On a Linux VM

```bash
# Copy the binary to your VM
scp dist/morpheus-snapshot-linux-amd64 user@your-vm:/opt/morpheus-snapshot

# SSH in and run (requires root for port 443)
ssh user@your-vm
chmod +x /opt/morpheus-snapshot/morpheus-snapshot-linux-amd64

# Run as root for port 443
sudo /opt/morpheus-snapshot/morpheus-snapshot-linux-amd64
```

Then open **https://your-vm-ip** in your browser.

> **Note:** The tool generates a self-signed cert (`cert.pem` / `key.pem`) on first run. Your browser will show a security warning — click "Advanced → Proceed" to continue. If you have your own cert, place `cert.pem` and `key.pem` in the same directory as the binary before starting.

---

### Custom Port (non-root)

```bash
# Run on port 8443 instead
PORT=8443 ./morpheus-snapshot-linux-amd64
# Access at https://your-vm-ip:8443
```

### Run as a systemd service

```ini
# /etc/systemd/system/morpheus-snapshot.service
[Unit]
Description=Morpheus Snapshot Migration Tool
After=network.target

[Service]
ExecStart=/opt/morpheus-snapshot/morpheus-snapshot-linux-amd64
WorkingDirectory=/opt/morpheus-snapshot
Restart=on-failure
User=root

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now morpheus-snapshot
sudo journalctl -u morpheus-snapshot -f
```

---

## Usage Walkthrough

### Step 1 — Connect Source
Choose a saved **appliance profile**, or enter URL with **API token** or **username & password**.

> API token location: **User Settings → API Access → Regenerate** in Morpheus UI.
> Administrator-level token recommended for full discovery.
> Manage profiles under **Appliance profiles** in the sidebar (shared with Fleet).

Click **Test Connection** to verify, then **Connect & Discover**.

### Step 2 — Discovery
The tool queries ~25 endpoints in parallel. Non-admin tokens may get 403s on some endpoints (shown as warnings, not errors).

### Step 3 — Select Items
- Items are grouped by category with expand/collapse
- Use the search box to filter by name
- Category buttons filter to a single resource type
- Check individual items or use the category checkbox to select/deselect all in a group
- **Select All Visible** respects the current search filter

### Step 4 — Destination
Choose a destination **appliance profile** or enter credentials inline. The migration preview shows exactly what will be created.

### Step 5 — Results
Per-item status: **success**, **skipped** (already exists), or **failed** (error detail shown).
Export the full results as JSON for audit/documentation purposes.

---

## What Gets Migrated

| Type | Supported | Notes |
|------|-----------|-------|
| Roles | ✅ | |
| Tenants | ✅ | |
| Groups | ✅ | Zone associations stripped |
| Policies | ✅ | |
| Tasks | ✅ | |
| Workflows | ✅ | Task references by name must exist on dest |
| Instance Types | ✅ | Layouts migrated separately |
| Catalog Items | ✅ | |
| Blueprints | ✅ | |
| Credentials | ✅ | Secrets not exported by API — values will be blank |
| Storage Buckets | ✅ | |
| Cypher | ✅ | Values not exported by API |
| Networks | ✅ | Cloud zone reference stripped |
| Network Pools | ✅ | |
| Network Domains | ✅ | |
| Virtual Images | ✅ | File content not transferred |
| Clouds | ⚠ | Use Morpheus native cloud config instead |
| Instances | ⚠ | Live workload migration not supported |
| Integrations | ⚠ | Credentials must be re-entered |
| Users | ⚠ | Passwords not transferred |

> Clouds, instances, and integrations are **discovered and shown** so you have a complete inventory picture, but migrating them requires manual re-configuration on the destination appliance (credentials, endpoints, etc. are appliance-specific).

---

## Building from Source

```bash
git clone https://github.com/cuxtud/morpheus-migration-utility
cd morpheus-snapshot
go mod tidy

# Build for current platform
go run ./cmd/server

# Build all platforms
make all

# Output in ./dist/
```

Requires Go 1.21+.

---

## Security Notes

- The tool runs locally on a VM you control — credentials are never stored, only held in memory during the session
- Use a dedicated API token with the minimum required permissions
- The auto-generated self-signed cert is valid for 10 years; replace with a CA-signed cert for production use
- For production use, place behind a VPN or restrict firewall access to port 443

---

## Project Structure

```
morpheus-snapshot/
├── cmd/server/
│   ├── main.go              # HTTP server, TLS, API routes
│   └── web/static/
│       └── index.html       # Full SPA frontend (embedded into binary)
├── internal/
│   ├── morpheus/
│   │   └── client.go        # Morpheus API client + discovery engine
│   └── migrate/
│       └── migrate.go       # Migration logic
├── dist/                    # Compiled binaries (after make all)
├── Makefile
└── go.mod
```
