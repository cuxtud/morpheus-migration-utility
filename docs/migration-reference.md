# Migration reference

Current release: **v1.0.6**. See [Release notes](releases.md) for version history.

## What gets migrated

### Library & automation (discovery categories)

| Category | Type | Supported | Notes |
|----------|------|-----------|-------|
| Tasks | `task` | ✅ | Git/Ansible integrations resolved on destination (see below) |
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

For **Git-backed tasks**, the utility resolves the source integration by id, then matches the destination integration by **integration name** (e.g. `GIT Radu`), not by repository host URL. The destination integration must exist with the same name and a valid SSH key pair.

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
