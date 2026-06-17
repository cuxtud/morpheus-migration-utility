# Troubleshooting

| Issue | What to try |
|-------|-------------|
| Browser TLS warning | Expected with auto-generated cert; accept warning or install `cert.pem` / `key.pem` |
| `pq: role "morpheus" does not exist` | Fix `DATABASE_URL` user/database or create the Postgres role |
| `column "source_discovery_id" does not exist` | Restart server so schema migration runs (upgrade adds column) |
| Discovery 403 warnings | Token lacks permission on some endpoints — shown as warnings, not fatal |
| Discovery timeout / `context deadline exceeded` | Increase `MORPHEUS_SNAPSHOT_HTTP_TIMEOUT` (default 3m) on the snapshot server |
| Migration **blocked** (virtual image) | Ensure the virtual image **name** exists on the destination |
| Migration **blocked** (Git task) | Create a Git integration on the destination with the **same name** as source (e.g. `GIT Radu`) and attach the SSH key pair |
| Migration **blocked** (option list) | Plugin lists and inline-credential REST/LDAP lists must be created manually; credential-backed lists need matching destination credentials |
| Migration **skipped** (option list / integration) | **Integrations** are discovery-only; update to **v1.0.5+** for standalone option list migration |
| Migration stream disconnected | Long runs may drop the browser stream; migration can continue server-side — check destination and server logs |
| Port 443 permission denied | Run as root or set `PORT=8443` |
| Empty fleet after restart | Set `DATABASE_URL` or re-run discover |
| `go run` / build errors on macOS | Use Go 1.22+; upgrade Go if linker errors on new macOS |

## Logs

```bash
# systemd
sudo journalctl -u morpheus-snapshot -f

# foreground — stderr includes HTTP debug when enabled in UI
sudo /opt/morpheus-snapshot/morpheus-snapshot-linux-amd64
```
