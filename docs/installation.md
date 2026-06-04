# Installation

Single static binary — no Go runtime or Docker required to run the application.

## Linux VM (RHEL / Rocky / Alma / Ubuntu)

```bash
sudo mkdir -p /opt/morpheus-snapshot
cd /opt/morpheus-snapshot

# amd64 for typical x86_64 servers (see Download page for arm64)
sudo curl -LO https://github.com/cuxtud/morpheus-migration-utility/releases/latest/download/morpheus-snapshot-linux-amd64
sudo chmod +x morpheus-snapshot-linux-amd64

sudo ln -sf /opt/morpheus-snapshot/morpheus-snapshot-linux-amd64 /usr/local/bin/morpheus-snapshot
```

## Run the server

Default: **HTTPS on port 443** (requires root). On first start the tool creates a self-signed `cert.pem` and `key.pem` in the working directory.

```bash
sudo /opt/morpheus-snapshot/morpheus-snapshot-linux-amd64
```

Open **https://\<server-ip\>** in your browser.

!!! note "TLS warning"
    Browsers will warn on the self-signed certificate. Use **Advanced → Proceed**, or place your own `cert.pem` and `key.pem` in the working directory before starting.

## Custom port (non-root)

```bash
PORT=8443 /opt/morpheus-snapshot/morpheus-snapshot-linux-amd64
```

Access at **https://\<server-ip\>:8443**.

## Optional PostgreSQL

For persistent profiles, fleet snapshots, saved discoveries, and migration history, set `DATABASE_URL` before starting. See [PostgreSQL](postgres.md).

```bash
export DATABASE_URL='postgres://morpheus:morpheus@localhost:5432/morpheus_snapshot?sslmode=disable'
sudo -E /opt/morpheus-snapshot/morpheus-snapshot-linux-amd64
```

## systemd service

```ini title="/etc/systemd/system/morpheus-snapshot.service"
[Unit]
Description=Morpheus Snapshot Utility
After=network.target

[Service]
ExecStart=/opt/morpheus-snapshot/morpheus-snapshot-linux-amd64
WorkingDirectory=/opt/morpheus-snapshot
Environment=DATABASE_URL=postgres://morpheus:morpheus@127.0.0.1:5432/morpheus_snapshot?sslmode=disable
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now morpheus-snapshot
sudo journalctl -u morpheus-snapshot -f
```

## Security notes

- Run on a VM you control; credentials stay in memory for the session (or in Postgres when configured).
- Use a dedicated API token with minimum required permissions.
- Restrict firewall access to port 443 (or your `PORT`) to operators only.
- Replace the self-signed cert with a CA-signed certificate for production.

Next: [Usage →](usage.md)
