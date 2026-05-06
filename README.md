# Morpheus Migration Utility 🔄

A self-contained migration tool for HPE Morpheus — discovers all resources on a source appliance, lets you cherry-pick what to migrate, then copies selected items to a destination appliance.

## Features

- **Full discovery** across all Morpheus resource types:
  - Clouds, Integrations (Git, Ansible, ServiceNow, etc.)
  - Instances, Virtual Images, Instance Types, Layouts, Node Types
  - Tasks, Workflows, Catalog Items, Blueprints, Apps
  - Tenants, Roles, Users, Policies, Groups
  - Networks, Network Pools, Network Domains
  - Credentials, Storage, Monitors, Clusters, Cypher
- **Grouped checkbox UI** — select individual items or entire categories
- **Live search & filter** across discovered items
- **Migration with results** — per-item success/skip/fail with export to JSON
- **Single binary** — no runtime, no Docker, no dependencies
- **HTTPS on port 443** — auto-generates a self-signed TLS cert on first run

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
Enter your source Morpheus appliance URL and API token.

> API token location: **User Settings → API Access → Regenerate** in Morpheus UI.
> Administrator-level token recommended for full discovery.

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
Enter destination appliance credentials. The migration preview shows exactly what will be created.

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
git clone https://github.com/anish/morpheus-snapshot
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
