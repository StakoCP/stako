# Stako

Open-source, Docker-first hosting control panel for **Ubuntu Server (22.04 LTS / 24.04 LTS)**, licensed under **MIT** (`github.com/StakoCP/stako`).

Documentation: [PRD](docs/PRD.md) · [API Reference](docs/API.md) · [Ubuntu Install Guide](docs/INSTALL_UBUNTU.md) ([HTML](docs/INSTALL_UBUNTU.html)) · [Changelog](CHANGELOG.md)

---

## Simple Installation (Ubuntu 22.04 / 24.04 LTS)

Run as `root` (or with `sudo`) on a clean Ubuntu Server (`amd64` / `arm64`):

```bash
curl -fsSLO https://raw.githubusercontent.com/StakoCP/stako/refs/heads/main/installer/install.sh
sudo bash install.sh
```

After installation completes, open the panel at:

```text
https://<YOUR_SERVER_IP>:8083
```

### Installer Presets & Options

| Flag | Description |
| :--- | :--- |
| `--preset basic` | Minimal stack (Nginx, PHP-FPM 8.4, MariaDB, nftables, Fail2Ban) |
| `--preset standard` | *(Default)* Basic + Docker, Redis, Certbot, Restic backups |
| `--preset full` | Standard + PHP 8.3/8.5, PostgreSQL, RabbitMQ, ProFTPD, Postfix/Dovecot/Rspamd, ModSecurity |
| `--with-dns` | Also install PowerDNS server |
| `--skip-upgrade` | Skip `apt-get upgrade` during install |
| `--version TAG` | Install a specific release tag (default: `latest`) |
| `--dry-run` | Print the installation plan without making changes |

### Update In-Place

Update `stako-core`, `stako-agent`, and `stako` CLI without touching your database, master key, or existing sites:

```bash
curl -fsSLO https://raw.githubusercontent.com/StakoCP/stako/refs/heads/main/installer/install.sh
sudo bash install.sh --update
```

You can also run `sudo bash update.sh` or trigger updates directly from the Dashboard.

---

## Key Features

- **Domains & Web Hosting:**
  - Nginx / Apache2 / PHP-FPM (`8.3`–`8.5`) with isolated per-user PHP-FPM pools.
  - Configurable per-domain document root (default `/home/<user>/<domain>/public_html`) with strict path isolation and `www-data` traversal permissions.
  - Per-domain access & error logs stored in `/home/<user>/web/<domain>/logs/` and viewable directly from the panel.
  - Path-based reverse proxy rules (HTTP, WebSocket, SSE, gRPC) and Let's Encrypt SSL automation.
- **Docker, GitHub & Apps:**
  - Rootless Docker container & Compose stack management.
  - Build and rebuild containers directly from **GitHub repositories** (`git clone` + `docker build`) or container images.
  - 1-click app catalog installer (WordPress, Nextcloud, Ghost, Gitea, etc.), Node/Go/Python process manager, and Git Deploy webhooks.
- **System Services & Configs:**
  - Smart service detection with start, stop, restart, install, and uninstall controls.
  - Capability-aware navigation (only shows menus and config tabs for installed services) and syntax-validated config editor.
- **Databases, Mail & Backups:**
  - MariaDB / PostgreSQL with auto-generated strong passwords, Redis instances, and RabbitMQ vhosts.
  - Postfix / Dovecot / Rspamd mail stack with DKIM/SPF/DMARC DNS helpers, plus FTPS/SFTP (plain FTP disabled).
  - Encrypted `restic` backups for `user_data` and `system_config` with automatic local repository initialization.
- **Security & Operations:**
  - Argon2id password hashing, mandatory 2FA (admin/reseller), step-up re-authentication, CSRF + strict CSP, scoped API keys, tamper-evident audit log chain, and emergency lockdown mode.
  - WAF management with automatic `libnginx-mod-http-modsecurity` / `libapache2-mod-security2` installation, `nftables` firewall, Fail2Ban unban, CVE/posture scanner, and `apt` updates.
  - PowerDNS / Cloudflare DNS zones, cron jobs, File Manager, audited Web Terminal, multi-server nodes, HestiaCP/cPanel/VestaCP importer, and official WHMCS module (`modules/whmcs/stako/`).

---

## Architecture

- **`cmd/stako-core`**: Non-root REST API server, SQLite (WAL) store, Argon2id + TOTP 2FA, RBAC, audit chain, job queue, AES-256-GCM envelope encryption, embedded Astro + Svelte Web UI (port `8083`).
- **`cmd/stako-agent`**: Root systemd daemon on `/run/stako/agent.sock` with `SO_PEERCRED` peer verification and strict command allowlist (`--dry-run` on dev machines).
- **`cmd/stako`**: CLI client talking to `/api/v1` (including `stako doctor`).
- **`web/`**: Astro + Svelte 5 + TypeScript SPA with strict CSP compatibility.
- **`installer/install.sh`**: Ubuntu 22.04 / 24.04 smart installer & in-place updater.
- **`modules/whmcs/stako/`**: Official WHMCS provisioning module.

---

## Development Quickstart

```bash
# 1. Build Web UI
cd web && npm install && npm run build && cd ..

# 2. Create initial admin
go run ./cmd/stako-core bootstrap-admin --username admin --email admin@localhost --password "ChangeMe-Strong-1234"

# 3. Run API + embedded UI in dev mode
go run ./cmd/stako-core serve --insecure-http --dry-run-agent

# 4. Run diagnostics & tests
go run ./cmd/stako --url http://127.0.0.1:8083 doctor
go vet ./... && go test ./...
```
