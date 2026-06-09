# Release notes

Binaries and checksums are on [GitHub Releases](https://github.com/cuxtud/morpheus-migration-utility/releases). The [Download](download.md) page links to the latest build.

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
