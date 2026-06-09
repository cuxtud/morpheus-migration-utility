# Morpheus Snapshot Utility

**HPE Morpheus** fleet inventory and migration tool — discover resources across appliances, inspect license and health, and migrate selected library and automation objects from a source appliance to a destination.

## What it does

| Capability | Description |
|------------|-------------|
| **Discovery** | Inventory clouds, integrations, instance types, layouts, node types, tasks, workflows, inputs, catalogs, RBAC, cypher, and more |
| **Migration** | Create or update selected objects on a destination appliance via the Morpheus API |
| **Fleet** | Multi-appliance view with license, health dashboard, and resource counts |
| **Persistence** | Optional PostgreSQL JSONB for profiles, snapshots, migration history, and UI sessions |

## Quick links

- **[Download binaries](download.md)** — Linux, Windows, macOS (no runtime required)
- **[Release notes](releases.md)** — What changed in each version (latest: **v1.0.3**)
- **[Installation](installation.md)** — VM install, HTTPS, systemd
- **[Usage guide](usage.md)** — Profiles, fleet, 5-step migration
- **[PostgreSQL](postgres.md)** — Persistent storage setup
- **[Releases on GitHub](https://github.com/cuxtud/morpheus-migration-utility/releases)**

## Requirements

- Network access from the snapshot server to source and destination Morpheus HTTPS URLs
- Morpheus API user with sufficient permissions (administrator recommended)
- **Linux:** `amd64` for typical RHEL/x86_64; `arm64` for ARM servers (`uname -m`)
- **Port:** 443 by default (or set `PORT`) — allow operator browsers through the firewall
- **Optional:** PostgreSQL 14+ for persistent fleet and migration history

## Single binary

No Go runtime, no Docker, and no app dependencies on the host. The UI is embedded in the binary. HTTPS uses a self-signed certificate on first run (`cert.pem` / `key.pem`).

---

[Download →](download.md){ .md-button .md-button--primary }
