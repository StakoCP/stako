# Stako

Open-source, Docker-first hosting control panel for Ubuntu Server (22.04 LTS / 24.04 LTS), licensed under **MIT** (`github.com/StakoCP/stako`).

See [docs/PRD.md](docs/PRD.md), [docs/API.md](docs/API.md), and [CHANGELOG.md](CHANGELOG.md).

## Architecture

- **`cmd/stako-core`**: Non-root REST API server, SQLite (WAL) state store, Argon2id + TOTP 2FA, RBAC, tamper-evident audit log, job queue, AES-256-GCM envelope encryption, embedded Astro + Svelte Web UI (port `8083` over TLS).
- **`cmd/stako-agent`**: Root systemd daemon listening on `/run/stako/agent.sock` with `SO_PEERCRED` verification and strict command allowlist (`--dry-run` on dev machines).
- **`cmd/stako`**: CLI client talking to `/api/v1` (including `stako doctor`).
- **`web/`**: Astro + Svelte 5 + TypeScript SPA with strict CSP compatibility.
- **`modules/whmcs/stako/`**: Official WHMCS provisioning module.
- **`installer/install.sh`**: Ubuntu 22.04 / 24.04 installer with presets (`basic`, `standard`, `full`) and OS baseline hardening.

## Implemented Feature Set (Phase 0 – Phase 6)

- **Auth & Security:** Argon2id, mandatory 2FA (admin/reseller), step-up re-authentication, CSRF + strict CSP, API keys with scope & IP allowlist, tamper-evident audit chain, emergency lockdown mode, WAF (Coraza/ModSecurity), CVE/posture scanner, apt updates.
- **Web & Apps:** Nginx / Apache / PHP-FPM vhosts, path-based reverse proxy (WebSocket/SSE/gRPC), Let's Encrypt SSL, Node/Go/Python process manager, Git Deploy, IPv4/IPv6 manager, hierarchical impersonation.
- **Data, Mail & Containers:** MariaDB/PostgreSQL, FTPS/SFTP (plain FTP disabled), nftables firewall & fail2ban unban, rootless Docker & Compose, 10-app catalog installer, Redis & RabbitMQ, Postfix/Dovecot/Rspamd mail + DKIM/SPF/DMARC, encrypted backups.
- **Operations & Scale:** PowerDNS/Cloudflare DNS zones, cron jobs, path-safe File Manager, monitoring & alerts, audited Web Terminal, multi-server nodes, HestiaCP/cPanel/VestaCP importer.

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
go test ./...
```
