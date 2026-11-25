# Changelog

## v0.1.0 (2026-11-25)

Initial implementation covering **Phase 0 through Phase 6**:
- **Phase 0 (Foundation):** `stako-core`, `stako-agent` (Unix socket + `SO_PEERCRED` + allowlist), `stako` CLI, SQLite WAL state store, Argon2id + TOTP 2FA, RBAC, tamper-evident audit log, job queue, OpenAPI 3.1, Astro + Svelte 5 strict-CSP Web UI.
- **Phase 1 (Core Web):** Nginx/Apache/PHP-FPM vhost & reverse proxy (WebSocket/SSE/gRPC), Let's Encrypt SSL, Node/Go/Python process manager with encrypted env, IPv4/IPv6 manager, hierarchical impersonation.
- **Phase 2 (Data & Access):** MariaDB/PostgreSQL, FTPS/SFTP (plain FTP disabled), nftables firewall & fail2ban unban, security posture & CVE scanner, apt update manager.
- **Phase 3 (Docker & Apps):** Rootless Docker & Compose (SEC-S14 policy enforcement), 10-app catalog installer, Redis & RabbitMQ per-tenant instances.
- **Phase 4 (Mail, Backup, Billing, Lockdown):** Postfix/Dovecot/Rspamd mail domains & DKIM/SPF/DMARC, user & system config backups, anti-SSRF webhooks, official WHMCS module, emergency lockdown mode.
- **Phase 5 (Enhancements):** PowerDNS/Cloudflare DNS zones, cron jobs, path-safe File Manager, monitoring & alerts, audited Web Terminal.
- **Phase 6 (Advanced):** Coraza/ModSecurity WAF, Git Deploy, Multi-Server nodes, HestiaCP/cPanel/VestaCP importer.
