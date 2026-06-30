# Release notes

Binaries and checksums are on [GitHub Releases](https://github.com/cuxtud/morpheus-migration-utility/releases). The [Download](download.md) page links to the latest build.

## v1.0.8

**[Download v1.0.8](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.8)** · May 2026

### Migration fixes

- **Inputs — exact name matching** — Destination inputs are matched by **exact name** (trimmed, case-insensitive), not Morpheus `phrase=` search or shared `code`. Creates when missing, updates when content differs, skips when already in sync.
- **Forms — name only** — Form lookup no longer falls back to `code`; only the exact form **name** resolves an existing destination form.
- **Form library inputs — name only** — Library inputs on forms resolve existing destination inputs by **exact name** only (never `code` or `fieldName`).
- **Workflows — option mapping by name** — Workflow input references map to destination option types by **name** only.
- **Option lists — content sync** — Skips when an existing list with the same name already matches source content.
- **Virtual images / Ansible** — Removed fuzzy `phrase=` and name-prefix fallbacks; matching is exact name only.

### Downloads (v1.0.8)

| Platform | File |
|----------|------|
| Linux x86_64 | `morpheus-snapshot-linux-amd64` |
| Linux ARM64 | `morpheus-snapshot-linux-arm64` |
| Windows x64 | `morpheus-snapshot-windows-amd64.exe` |
| macOS Intel | `morpheus-snapshot-mac-intel` |
| macOS Apple Silicon | `morpheus-snapshot-mac-apple-silicon` |

Verify downloads with `checksums.txt` in the [release assets](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.8).

---

## v1.0.7

**[Download v1.0.7](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.7)** · June 2026

### Migration fixes

- **Forms — library input references** — Library inputs on forms are resolved on the destination and attached as `{"id": <destination input id>}` only (inline form fields such as `group` keep their full definition). Fixes form create failures when source expanded definitions included wrong `optionList` ids.
- **Tasks — task type remapping** — Destination task type is matched by `taskType.code` (e.g. `jythonTask`) via `/api/task-types`, not the source numeric id.
- **Tasks — code repository resolution** — `file.repository.id` is resolved via `/api/options/codeRepositories` using the `{integration} - {repo}` label (integration prefix, repository suffix).
- **Tasks — post-create verification** — Skips false partial/rollback when Morpheus GET omits the `file` block on the created task.

### UI

- **Verbose migration log** — Per-item step logs and payloads (on failure) appear in collapsible sections on the results page; activity log is preserved after migration completes.
- **Collapsible logs** — Migration activity log during the run and per-result debug sections use expand/collapse; failures auto-expand.

### Downloads (v1.0.7)

| Platform | File |
|----------|------|
| Linux x86_64 | `morpheus-snapshot-linux-amd64` |
| Linux ARM64 | `morpheus-snapshot-linux-arm64` |
| Windows x64 | `morpheus-snapshot-windows-amd64.exe` |
| macOS Intel | `morpheus-snapshot-mac-intel` |
| macOS Apple Silicon | `morpheus-snapshot-mac-apple-silicon` |

Verify downloads with `checksums.txt` in the [release assets](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.7).

---

## v1.0.6

**[Download v1.0.6](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.6)** · June 2026

### Migration fixes

- **Forms — exact name matching** — Destination forms are matched by the exact `name` from `optionTypeForms` (case-insensitive, trimmed). A shared `code` no longer matches a different form when the name differs.
- **Forms — content sync** — When a form exists on the destination with the same name, migration compares field content and **updates** when it differs, or skips when it already matches.
- **Catalog forms** — Catalog migration always runs form sync instead of linking to a destination form without checking content.

### UI

- **Clear cache** — Clear saved discovery snapshots, migration runs, and workflow sessions from the Appliance profiles panel (profiles are not removed).

### Downloads (v1.0.6)

| Platform | File |
|----------|------|
| Linux x86_64 | `morpheus-snapshot-linux-amd64` |
| Linux ARM64 | `morpheus-snapshot-linux-arm64` |
| Windows x64 | `morpheus-snapshot-windows-amd64.exe` |
| macOS Intel | `morpheus-snapshot-mac-intel` |
| macOS Apple Silicon | `morpheus-snapshot-mac-apple-silicon` |

Verify downloads with `checksums.txt` in the [release assets](https://github.com/cuxtud/morpheus-migration-utility/releases/tag/v1.0.6).

---

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
