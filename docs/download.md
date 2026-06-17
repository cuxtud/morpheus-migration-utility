# Download

Pre-built binaries are published on [GitHub Releases](https://github.com/cuxtud/morpheus-migration-utility/releases). Links below always point at the **latest** release. See **[Release notes](releases.md)** for what changed in v1.0.6 and earlier versions.

## Binaries

| Platform | Architecture | File | Download |
|----------|--------------|------|----------|
| **Linux** (RHEL, Rocky, Alma, Ubuntu, …) | x86_64 | `morpheus-snapshot-linux-amd64` | [Download](https://github.com/cuxtud/morpheus-migration-utility/releases/latest/download/morpheus-snapshot-linux-amd64) |
| **Linux** | ARM64 | `morpheus-snapshot-linux-arm64` | [Download](https://github.com/cuxtud/morpheus-migration-utility/releases/latest/download/morpheus-snapshot-linux-arm64) |
| **Windows** | x64 | `morpheus-snapshot-windows-amd64.exe` | [Download](https://github.com/cuxtud/morpheus-migration-utility/releases/latest/download/morpheus-snapshot-windows-amd64.exe) |
| **macOS** | Intel | `morpheus-snapshot-mac-intel` | [Download](https://github.com/cuxtud/morpheus-migration-utility/releases/latest/download/morpheus-snapshot-mac-intel) |
| **macOS** | Apple Silicon | `morpheus-snapshot-mac-apple-silicon` | [Download](https://github.com/cuxtud/morpheus-migration-utility/releases/latest/download/morpheus-snapshot-mac-apple-silicon) |
| **Checksums** | all | `checksums.txt` | [Download](https://github.com/cuxtud/morpheus-migration-utility/releases/latest/download/checksums.txt) |

### Which Linux binary?

```bash
uname -m
```

| Output | Binary |
|--------|--------|
| `x86_64` | **morpheus-snapshot-linux-amd64** (most Red Hat / VMware / bare-metal servers) |
| `aarch64` | **morpheus-snapshot-linux-arm64** (ARM servers, e.g. AWS Graviton) |

## Verify checksum (Linux)

```bash
mkdir -p /opt/morpheus-snapshot && cd /opt/morpheus-snapshot

curl -LO https://github.com/cuxtud/morpheus-migration-utility/releases/latest/download/checksums.txt
curl -LO https://github.com/cuxtud/morpheus-migration-utility/releases/latest/download/morpheus-snapshot-linux-amd64

sha256sum -c checksums.txt --ignore-missing
```

## Build from source

```bash
git clone https://github.com/cuxtud/morpheus-migration-utility.git
cd morpheus-migration-utility
go mod tidy
make all   # outputs under ./dist/
```

Requires Go 1.22+.

Next: [Installation →](installation.md)
