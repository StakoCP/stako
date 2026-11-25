# Status Implementasi Terhadap PRD

Dokumen ini merangkum pemetaan fitur [PRD](PRD.md) terhadap implementasi aktual di repositori `StakoCP/stako`.

| Fase | Modul / Fitur | Status | Lokasi Utama |
| --- | --- | --- | --- |
| **Fase 0** | `stako-core`, `stako-agent` (Unix socket + allowlist + dry-run), `stako` CLI, SQLite WAL, Argon2id + TOTP, RBAC, Audit Log hash-chain, OpenAPI 3.1, Astro+Svelte UI (CSP ketat) | Selesai | `cmd/`, `internal/api/`, `internal/crypto/`, `internal/store/`, `web/` |
| **Fase 1** | Domain (nginx/Apache/PHP-FPM), Reverse Proxy (WS/SSE/gRPC), SSL, Process Manager (Node/Go/Python + env terenkripsi), IPv4/IPv6 Manager, Impersonation | Selesai | `internal/api/handlers_phase1.go`, `internal/render/`, `templates/`, `web/src/views/DomainsView.svelte`, `AppsView.svelte`, `IPsView.svelte` |
| **Fase 2** | MariaDB/PostgreSQL, FTPS/SFTP (plain FTP ditolak), Firewall nftables + fail2ban unban, Security & CVE Scanner, Apt Update Manager | Selesai | `internal/api/handlers_phase2.go`, `web/src/views/DatabasesView.svelte`, `FtpView.svelte`, `FirewallView.svelte`, `SecurityView.svelte` |
| **Fase 3** | Docker Rootless & Compose (validasi anti-privileged/docker.sock), App Installer (10 aplikasi), Redis & RabbitMQ | Selesai | `internal/api/handlers_phase3.go`, `internal/catalog/`, `web/src/views/DockerView.svelte`, `CatalogView.svelte`, `ServicesView.svelte` |
| **Fase 4** | Mail Server (DKIM/SPF/DMARC), Backup Data & Konfigurasi, Webhook anti-SSRF, Modul WHMCS + SSO, Mode Lockdown, Rotasi Kunci & Forensik | Selesai | `internal/api/handlers_phase4.go`, `handlers_extra.go`, `modules/whmcs/stako/`, `web/src/views/MailView.svelte`, `BackupsView.svelte`, `WebhooksView.svelte` |
| **Fase 5** | DNS (PowerDNS/Cloudflare), Cron Jobs, File Manager aman, Monitoring & Alert, Web Terminal teraudit | Selesai | `internal/api/handlers_phase5.go`, `web/src/views/DnsView.svelte`, `CronView.svelte`, `FilesView.svelte`, `MonitoringView.svelte`, `TerminalView.svelte` |
| **Fase 6** | WAF (Coraza/ModSecurity), Git Deploy, Multi-Server Nodes, Import HestiaCP/cPanel/VestaCP | Selesai | `internal/api/handlers_phase6.go`, `web/src/views/GitDeployView.svelte`, `NodesView.svelte` |
| **Operasional** | `stako doctor`, Installer Ubuntu + baseline hardening, CI & Release multi-arch (`amd64`/`arm64`), Lisensi & Dokumen Open-Source | Selesai | `installer/install.sh`, `.github/workflows/`, `THIRD_PARTY_LICENSES.md`, `SECURITY.md` |

## Catatan Pengujian Lanjutan di Ubuntu Nyata
Di lingkungan Windows, `stako-agent` berjalan dalam mode `--dry-run`. Pengujian integrasi nyata terhadap `systemd`, `nginx`, `certbot`, `nftables`, `fail2ban`, `docker` rootless, `postfix`/`dovecot`, dan `restic` perlu dijalankan pada VM/server Ubuntu 22.04/24.04 LTS.
