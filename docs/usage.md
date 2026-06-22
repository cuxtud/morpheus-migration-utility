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
| 2 | Discovery queries the source API (~25 resource types); **duration** is shown when complete; warnings appear for endpoints the token cannot access |
| 3 | **Select items** — search, filter by category, select resources; preview shows **auto-included dependencies** |
| 4 | **Destination** — profile or inline credentials → review preview → **Begin migration** |
| 5 | **Results** — per-item success, skipped, failed, blocked, or partial; live progress stream; export JSON; discovery timestamp when Postgres is enabled |

### Saved discoveries

With PostgreSQL, past discovery snapshots appear under **Migration → Connect** and **Appliance profiles**:

- **Load** — restore discovery into the selection step (switches to Migration)
- **Delete** — remove a snapshot (warns if migration runs still reference it)
- **Clear cache** — under **Appliance profiles**, remove all cached discoveries, migration history, and saved sessions in one step (profiles are kept)
- Columns include **appliance name**, item counts, and **used by runs**

### Remember session

On the select-items step, enable **Remember discovery and selections** to persist the workflow in PostgreSQL (or browser tab storage without a database).

### Export / import discovery JSON

Download discovery JSON from the select step, or load a previous export from **Connect** to skip a live API discovery (token may still be required for migration).

## Dependency-aware migration

- **Instance types** — related **layouts**, **node types**, and **workflows** can be auto-included. **System** (Morpheus-seeded) instance types are excluded from discovery and skipped if referenced directly.
- **Workflows** — related **tasks** can be auto-included.
- **Catalog items** — related **forms**, **workflows**, and user **instance types** can be auto-included. Catalogs using system instance types resolve against destination built-ins.
- **Forms** — matched on the destination by **exact form name** (`optionTypeForms[].name`); field content is compared and **updated** when it differs. Library inputs are linked by destination id; related **option lists** are created or remapped when migrating inputs.
- **Option lists** — can be migrated by selecting the **Option Lists** category directly, or created when migrating list-backed inputs.
- **Node types** — **virtual images** are matched on the destination by **name**; missing images are **blocked** with a clear message.
- **Tasks** — repository-backed tasks require a Git integration on the destination with the **same integration name** as on the source (not the repository URL).
- **Integrations** — appear in discovery for inventory and relations but are **not migrated**; create matching integrations on the destination before migrating dependent tasks.
- **Parallel waves** — items in the same dependency tier migrate concurrently (default 4 workers); multiple catalog items can migrate in parallel after dependencies complete.

See [Migration reference](migration-reference.md) for the full per-type matrix.

## Verbose migration log

On the destination step, enable **Verbose migration log** to capture step-by-step details in the results UI (collapsible per item and activity log on the results page). Full HTTP request bodies are also written to the server terminal — useful when diagnosing form or task migration failures.
