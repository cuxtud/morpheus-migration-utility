# Migration reference

Current release: **v1.0.7**. See [Release notes](releases.md) for version history.

## What gets migrated

### Library & automation (discovery categories)

| Category | Type | Supported | Notes |
|----------|------|-----------|-------|
| Tasks | `task` | ✅ | Task type remapped by `taskType.code` (`/api/task-types`); code repos via `/api/options/codeRepositories` |
| Workflows | `workflow` | ✅ | Related tasks auto-included; task names must resolve on destination |
| Inputs | `input` | ✅ | Option types; list-backed inputs can create option lists on destination |
| Option Lists | `optionList` | ✅ | Standalone selection or via inputs/forms; plugin/inline-auth lists may be **blocked** |
| Forms | `form` | ✅ | Exact name match on destination; updates when content differs; may inline-create missing inputs |
| Instance Types | `instanceType` | ✅ | User-created types only; **system** seeded types are **skipped** |
| Layouts | `layout` | ✅ | Often auto-included with instance types |
| Node Types | `nodeType` | ✅ | Virtual image remapped by **name** on destination |
| Virtual Images | `virtualImage` | ✅ | Metadata only — file content is not transferred |
| Catalog Items | `catalogItem` | ✅ | Forms, layouts, clouds, groups remapped; system instance types use destination built-ins |
| Blueprints | `blueprint` | ✅ | Generic create/update |
| Groups | `group` | ✅ | Cloud zone associations remapped |
| Clouds | `cloud` | ✅ | Credential references remapped by name; endpoints are appliance-specific |
| Cypher | `cypher` | ✅ | Secret values often not returned by API |
| Credentials | `credential` | ✅ | Secret values often not returned by API |

### RBAC & platform

| Category | Type | Supported | Notes |
|----------|------|-----------|-------|
| Tenants | `tenant` | ✅ | |
| Roles | `role` | ✅ | |
| Policies | `policy` | ✅ | |
| Users | `user` | ❌ | Not yet supported |
| Apps | `app` | ❌ | Not yet supported |

### Integrations

| Category | Type | Supported | Notes |
|----------|------|-----------|-------|
| Integrations | `integration` | ⚠ | **Discovery only** — not selectable for migration. Repository-backed **tasks** require a matching integration on the destination |

For **Git-backed tasks**, `file.repository.id` is a **code repository** option value (from `/api/options/codeRepositories`), not a Git integration id. Each option is labeled `{integration name} - {repository name}` (e.g. `python_examples - cuxtud_python`). Migration matches the same full label on the destination when present, otherwise matches the Git integration by the **prefix** and the repository by the **suffix**.

For **Ansible tasks**, destination integrations are matched by integration name from `ansibleGitId`.

### Other types (not in discovery UI)

| Type | Supported | Notes |
|------|-----------|-------|
| Storage buckets | `storageBucket` | ✅ if added to migration queue |
| Networks / pools / domains | `network`, `networkPool`, `networkDomain` | ✅ Cloud zone references stripped |

### Not migrated

| Item | Notes |
|------|-------|
| **System library items** | Morpheus-seeded instance types, layouts, and node types (`account: null`) are excluded from discovery |
| **Live instances / workloads** | Inventory only |
| **Integration records** | Configure manually on destination; tasks/workflows reference them by name |

!!! tip
    Clouds and integrations are appliance-specific. Even when clouds migrate, review credentials, endpoints, and integration SSH keys on the destination before relying on migrated automation.

## Migration behaviour

### Dependency expansion

Selecting items can auto-include dependencies:

- **Instance types** → layouts → node types, workflows
- **Workflows** → tasks
- **Catalog items** → forms, workflows, instance types (user-created only)
- **Forms** → inputs, option lists

### Parallel execution

Items in the same dependency tier migrate concurrently (default **4 workers**). Catalog items run in a parallel pass after earlier waves complete.

### Discovery snapshot reuse

When migration uses a saved discovery (`discoveryId` from PostgreSQL), the server reuses discovery JSON for lookups (clouds, instance types, forms, layouts) before calling the source API again.

### Form matching (v1.0.6+)

Destination forms are resolved from `GET /api/library/option-type-forms` by **exact** `name` match (trimmed, case-insensitive). A different form with the same `code` is **not** treated as a match.

When the name exists on the destination, migration compares field groups and options:

- **Content matches** → skipped (`Form already exists and matches source`)
- **Content differs** → updated on the destination
- **Name missing** → created (or blocked if Morpheus rejects duplicate `code`)

Catalog items always run this form sync; they no longer link to a destination form without checking content.

### Form library inputs (v1.0.7+)

When a form references **library inputs** (`formField: false`), migration ensures each input exists on the destination (creating it with remapped option lists when needed), then attaches it to the form as **`{"id": <destination option-type id>}`** only — in `fieldGroups[].options` and in root `options[]` when the source API returns expanded definitions there.

**Inline form fields** (`formField: true`, e.g. `type: "group"`) remain full field definitions in `options[]`.

## Outcome statuses

| Status | Meaning |
|--------|---------|
| **success** | Created or updated on destination |
| **skipped** | Already exists, not applicable, or non-migratable type (e.g. system instance type) |
| **error** | API or validation failure |
| **blocked** | Dependency missing (virtual image, credential, integration, option list auth, etc.) |
| **partial** | Created but follow-up verification failed (e.g. task repository link); item may be rolled back |

## Instance type hierarchy

Morpheus models provisioning as:

```text
Instance Type
  └── Layout (technology: VMware, cloud, Terraform, workflow, …)
        └── Node type(s) and/or workflow
```

Migrating an instance type without selecting layouts may still pull in required layouts, node types, and workflows automatically. Catalog items that reference **system** instance types resolve layouts on the destination built-in type — they are not migrated from source.
