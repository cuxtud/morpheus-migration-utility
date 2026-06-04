# Troubleshooting

| Issue | What to try |
|-------|-------------|
| Browser TLS warning | Expected with auto-generated cert; accept warning or install `cert.pem` / `key.pem` |
| `pq: role "morpheus" does not exist` | Fix `DATABASE_URL` user/database or create the Postgres role |
| `column "source_discovery_id" does not exist` | Restart server so schema migration runs (upgrade adds column) |
| Discovery 403 warnings | Token lacks permission on some endpoints — shown as warnings, not fatal |
| Migration **blocked** (virtual image) | Ensure the virtual image **name** exists on the destination |
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

## Documentation site 404

After the first docs deploy, enable **GitHub Pages** for this repository:

**Settings → Pages → Build and deployment → Source: Deploy from branch → Branch: `gh-pages` / `/ (root)`**

Site URL: **https://cuxtud.github.io/morpheus-migration-utility/**
