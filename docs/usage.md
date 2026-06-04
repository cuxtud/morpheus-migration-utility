# Usage

The web UI has three main areas: **Migration**, **Fleet inventory**, and **Appliance profiles**.

## Appliance profiles (setup once)

1. Open **Appliance profiles** or **Fleet → Add appliances**.
2. Save each Morpheus URL with **API token** or **username + password**.
3. Profiles are reused in Migration and Fleet.

**API token:** Morpheus → **User Settings → API Access → Regenerate**. An administrator token is recommended for full discovery.

## Fleet inventory

1. **Fleet inventory** — select profiles and **Discover selected**, or open **Appliances** for cached snapshots.
2. Click an appliance for detail: resources, **license** (start date, end date, max instances), **health** summary, and per-category inventory.
3. **Relations** — dependency graph for tasks, workflows, catalogs, inputs, option lists, and related items.

## Migration workflow (5 steps)

| Step | Action |
|------|--------|
| 1 | **Connect source** — profile or URL + credentials → **Test connection** → **Connect & Discover** |
| 2 | Discovery queries the source API (~25 resource types); warnings appear for endpoints the token cannot access |
| 3 | **Select items** — search, filter by category, select resources; preview shows **auto-included dependencies** |
| 4 | **Destination** — profile or inline credentials → review preview → **Begin migration** |
| 5 | **Results** — per-item success, skipped, failed, blocked, or partial; export JSON; discovery timestamp when Postgres is enabled |

### Saved discoveries

With PostgreSQL, past discovery snapshots appear under **Migration → Connect** and **Appliance profiles**:

- **Load** — restore discovery into the selection step (switches to Migration)
- **Delete** — remove a snapshot (warns if migration runs still reference it)
- Columns include **appliance name**, item counts, and **used by runs**

### Remember session

On the select-items step, enable **Remember discovery and selections** to persist the workflow in PostgreSQL (or browser tab storage without a database).

### Export / import discovery JSON

Download discovery JSON from the select step, or load a previous export from **Connect** to skip a live API discovery (token may still be required for migration).

## Dependency-aware migration

- **Instance types** — related **layouts**, **node types**, and **workflows** can be auto-included.
- **Workflows** — related **tasks** can be auto-included.
- **Node types** — **virtual images** are matched on the destination by **name**; missing images are **blocked** with a clear message.
- **System library items** — Morpheus-seeded objects (`account: null`) are excluded from discovery and migration.

See [Migration reference](migration-reference.md) for per-type support.

## HTTP debug

On the destination step, enable **Log outgoing HTTP** to write Morpheus API calls to the server stderr (useful when diagnosing migration failures).
