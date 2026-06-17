# Release notes

Binaries and checksums are on [GitHub Releases](https://github.com/cuxtud/morpheus-migration-utility/releases). The [Download](download.md) page links to the latest build.

## v1.0.5

**[Download v1.0.5](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.5)** · June 2026

### Migration fixes

- **Option lists** — Directly selected option lists migrate correctly (type normalization and dedicated migrator routing).
- **Git integrations** — Repository-backed tasks match destination Git integrations by **integration name** (e.g. `GIT Radu`), not by repository host URL.

### Downloads (v1.0.5)

| Platform | File |
|----------|------|
| Linux x86_64 | `morpheus-snapshot-linux-amd64` |
| Linux ARM64 | `morpheus-snapshot-linux-arm64` |
| Windows x64 | `morpheus-snapshot-windows-amd64.exe` |
| macOS Intel | `morpheus-snapshot-mac-intel` |
| macOS Apple Silicon | `morpheus-snapshot-mac-apple-silicon` |

Verify downloads with `checksums.txt` in the [release assets](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.5).

---

## v1.0.4

**[Download v1.0.4](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.4)** · June 2026

### Migration improvements

- **Parallel dependency waves** — Items in the same dependency tier migrate concurrently (default 4 workers); catalog items still run in a parallel pass after serial waves.
- **Discovery snapshot reuse** — Migration resolves clouds, instance types, forms, layouts, and workflows from the saved discovery snapshot before hitting the source API.
- **Catalog migration** — Improved cloud/layout/form/workflow resolution; inline creation of missing forms and layouts; destination caches for faster lookups.
- **System instance types** — Built-in system instance types are not migrated; catalogs using them resolve against destination built-ins only.
- **Option lists** — Library option list migration support.
- **Thread-safe automation state** — Fixes concurrent map writes during parallel form/catalog migration.

### Discovery & UI

- **Faster discovery** — Parallel list pagination and enrichment; configurable HTTP timeout (default 3 minutes).
- **Discovery duration** — Elapsed time shown in discovery stats, overview, history, and fleet views.
- **Form relations** — UI shows form links to library inputs and catalog items.
- **Migration stream** — Resilient NDJSON parsing when the browser disconnects; server serializes heartbeat/progress writes.

### Documentation

- **Download counter** — GitHub release download count in the docs site footer.

### Downloads (v1.0.4)

| Platform | File |
|----------|------|
| Linux x86_64 | `morpheus-snapshot-linux-amd64` |
| Linux ARM64 | `morpheus-snapshot-linux-arm64` |
| Windows x64 | `morpheus-snapshot-windows-amd64.exe` |
| macOS Intel | `morpheus-snapshot-mac-intel` |
| macOS Apple Silicon | `morpheus-snapshot-mac-apple-silicon` |

Verify downloads with `checksums.txt` in the [release assets](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.4).

---

## v1.0.3

**[Download v1.0.3](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.3)** · June 2026

### Migration improvements

- **Catalog migration** — Smarter fallbacks when destination resources are missing: networks (first available on cloud), groups/clouds/service plans, and layouts remapped by code.
- **Auto-create catalog forms** — If a catalog references a form that does not exist on the destination, the form is fetched from source, field-group library inputs are created, and the form is provisioned before the catalog is saved.
- **Parallel catalog migration** — Multiple catalog items can migrate concurrently (default 4 workers) after dependencies are resolved.
- **Streaming progress** — Long migrations stream progress and heartbeats to the UI; server write timeout extended to avoid disconnects on large runs.
- **Instance type & layout migration** — Expanded dependency handling for layouts, node types, workflows, and virtual images on instance types.

### Discovery & relations

- **Virtual images** — Added to discovery with deep-fetch of node types for `virtualImage` links.
- **Relation graphs** — Richer graphs for catalogs and instance types: instance type → layout → node type → virtual image, plus workflow → task → integration chains.
- **Catalog relations** — Resolves layouts from `config.layout` and embedded instance type data.
- **Discovery naming** — Better display names for cyphers, clouds, catalog items, and virtual images.

### Fleet & connectivity

- **Skip TLS fix** — Fleet discover no longer ignores saved “Skip TLS” when only `profileId` is sent; password/OAuth login respects the profile setting.
- **Node type details API** — `POST /api/node-type-details` for enriching relations with full container type records.

### Documentation

- MkDocs site and GitHub Pages workflow for installation, usage, and troubleshooting guides.

### Downloads (v1.0.3)

| Platform | File |
|----------|------|
| Linux x86_64 | `morpheus-snapshot-linux-amd64` |
| Linux ARM64 | `morpheus-snapshot-linux-arm64` |
| Windows x64 | `morpheus-snapshot-windows-amd64.exe` |
| macOS Intel | `morpheus-snapshot-mac-intel` |
| macOS Apple Silicon | `morpheus-snapshot-mac-apple-silicon` |

Verify downloads with `checksums.txt` in the [release assets](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.3).

---

## v1.0.2

**[Download v1.0.2](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.2)**

- PostgreSQL persistence for profiles, fleet snapshots, migration history, and UI sessions
- Fleet inventory UI with license, health, and discovery dashboards
- Dependency-aware migration for tasks, workflows, inputs, forms, and instance types
- Password/OAuth login for Morpheus appliances alongside API tokens
