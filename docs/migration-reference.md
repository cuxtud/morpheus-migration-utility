# Migration reference

## What gets migrated

| Type | Supported | Notes |
|------|-----------|-------|
| Roles | ✅ | |
| Tenants | ✅ | |
| Groups | ✅ | Zone associations stripped |
| Policies | ✅ | |
| Tasks | ✅ | |
| Workflows | ✅ | Task references by name must exist on destination |
| Instance types | ✅ | Related layouts/node types may auto-include |
| Layouts | ✅ | |
| Node types | ✅ | Virtual image remapped by name on destination |
| Catalog items | ✅ | |
| Blueprints | ✅ | |
| Credentials | ✅ | Secret values often not returned by API |
| Storage buckets | ✅ | |
| Cypher | ✅ | Values often not exported by API |
| Networks / pools / domains | ✅ | Cloud zone references stripped |
| Virtual images | ✅ | File content not transferred |
| Clouds | ⚠ | Discovered; configure manually on destination |
| Live instances | ⚠ | Not supported as workload migration |
| Integrations | ⚠ | Discovered; credentials must be re-entered |
| Users | ⚠ | Passwords not transferred |

!!! tip
    Clouds, instances, and integrations appear in discovery for inventory. Migrating them usually requires manual re-configuration on the destination (endpoints and credentials are appliance-specific).

## Outcome statuses

| Status | Meaning |
|--------|---------|
| **success** | Created or updated on destination |
| **skipped** | Already exists or not applicable |
| **error** | API or validation failure |
| **blocked** | Dependency missing (e.g. virtual image not found by name) |
| **partial** | Some sub-operations failed |

## Instance type hierarchy

Morpheus models provisioning as:

```text
Instance Type
  └── Layout (technology: VMware, cloud, Terraform, workflow, …)
        └── Node type(s) and/or workflow
```

Migrating an instance type without selecting layouts may still pull in required layouts, node types, and workflows automatically.
