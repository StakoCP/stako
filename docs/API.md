# Stako REST API v1 (Fase 0)

Base path: `/api/v1`. Semua body JSON (`Content-Type: application/json`).
Spesifikasi mesin-baca: `GET /api/v1/openapi.json` dan `GET /api/v1/openapi.yaml`.

## Autentikasi

- **Sesi cookie** (Web UI): cookie `stako_session` (HttpOnly, SameSite=Strict, Secure bila TLS).
  Setiap request yang mengubah data (`POST`, `PATCH`, `PUT`, `DELETE`) dengan sesi cookie **wajib**
  menyertakan header `X-CSRF-Token` berisi `csrf_token` dari respons login atau `GET /auth/me`.
- **API key** (CLI/otomasi/billing): header `Authorization: Bearer stk_...`. Tidak perlu CSRF.
  Scope `read` hanya boleh `GET`; scope `write` boleh semua metode sesuai peran pemilik key.

## Format error (RFC 9457, `application/problem+json`)

```json
{ "type": "urn:stako:problem:validation", "title": "Validation failed", "status": 422,
  "detail": "...", "errors": [ { "field": "username", "message": "..." } ] }
```

| type | status | arti |
| --- | --- | --- |
| `urn:stako:problem:unauthenticated` | 401 | belum login / sesi habis |
| `urn:stako:problem:invalid-credentials` | 401 | username/password/kode salah |
| `urn:stako:problem:totp-required` | 401 | password benar, perlu `totp_code` |
| `urn:stako:problem:forbidden` | 403 | tidak berhak |
| `urn:stako:problem:csrf` | 403 | header CSRF tidak ada/salah |
| `urn:stako:problem:reauth-required` | 403 | perlu `POST /auth/reauth` lalu ulangi request |
| `urn:stako:problem:not-found` | 404 | objek tidak ada |
| `urn:stako:problem:conflict` | 409 | duplikat / bentrok |
| `urn:stako:problem:validation` | 422 | input tidak valid (`errors[]`) |
| `urn:stako:problem:rate-limited` | 429 | terlalu banyak percobaan |
| `urn:stako:problem:internal` | 500 | kesalahan server |

## Paginasi

Query `limit` (default 50, maks 200) dan `offset`. Respons: `{ "items": [...], "total": 123 }`.

## Objek

```jsonc
// User
{ "id": "u_8f3a...", "username": "budi", "email": "budi@example.com",
  "role": "admin|reseller|user|readonly", "status": "active|suspended",
  "package_id": "p_..." , "owner_id": "u_..." , "totp_enabled": false,
  "shell": "nologin|sftp|rbash|bash", "created_at": "RFC3339", "updated_at": "RFC3339",
  "last_login_at": "RFC3339|null" }

// Package (-1 = tak terbatas)
{ "id": "p_...", "name": "Basic", "owner_id": null, "disk_mb": 10240, "bandwidth_mb": -1,
  "domains": 5, "databases": 5, "mail_accounts": 10, "ftp_accounts": 2, "containers": 2,
  "cpu_percent": 100, "memory_mb": 1024, "shell_access": false, "php_versions": ["8.3"],
  "created_at": "...", "updated_at": "..." }

// ApiKey
{ "id": "k_...", "name": "billing", "prefix": "stk_AB12", "scopes": ["read","write"],
  "allowed_ips": ["203.0.113.0/24"], "expires_at": "RFC3339|null", "last_used_at": "RFC3339|null",
  "created_at": "..." }

// Session
{ "id": "s_...", "ip": "203.0.113.5", "user_agent": "...", "created_at": "...",
  "last_seen_at": "...", "current": true }

// AuditEntry
{ "id": 42, "at": "...", "actor_id": "u_...", "actor_username": "admin", "acting_as": null,
  "action": "user.create", "object_type": "user", "object_id": "u_...",
  "result": "success|failure", "ip": "...", "user_agent": "...", "detail": { }, "hash": "hex" }

// Job
{ "id": "j_...", "type": "user.provision", "status": "queued|running|succeeded|failed",
  "created_by": "u_...", "payload": { }, "log": "teks multi-baris", "error": "string|null",
  "created_at": "...", "started_at": "...|null", "finished_at": "...|null" }
```

## Endpoint

### Publik
- `GET /health` → `{ "status": "ok", "version": "0.1.0" }`

### Auth
- `POST /auth/login` `{ "username", "password", "totp_code"? }` → `200 { "user": User, "csrf_token": "..." }`
  (atau 401 `totp-required` bila 2FA aktif dan `totp_code` kosong; `totp_code` boleh berupa recovery code)
- `POST /auth/logout` → `204`
- `GET /auth/me` → `{ "user": User, "csrf_token": "...", "two_factor_required": bool }`
  (`two_factor_required` = peran admin/reseller wajib 2FA tetapi belum aktif → UI harus mengarahkan ke setup 2FA)
- `POST /auth/reauth` `{ "password", "totp_code"? }` → `204` (berlaku 5 menit)
- `POST /auth/password` `{ "current_password", "new_password" }` → `204` (mencabut sesi lain)
- `POST /auth/2fa/setup` → `{ "secret": "BASE32", "otpauth_url": "otpauth://totp/..." }`
- `POST /auth/2fa/enable` `{ "code" }` → `{ "recovery_codes": ["xxxx-xxxx", ...] }`
- `POST /auth/2fa/disable` `{ "code" }` → `204` (perlu reauth)
- `GET /auth/sessions` → `{ "items": [Session] }`
- `DELETE /auth/sessions/{id}` → `204`
- `POST /auth/sessions/revoke-others` → `204`

### Users (admin: semua; reseller: miliknya; user/readonly: dirinya)
- `GET /users?q=&limit=&offset=` → `{ items: [User], total }`
- `POST /users` `{ "username", "email", "password", "role", "package_id"?, "shell"? }` → `201 { "user": User, "job_id": "j_...|null" }`
- `GET /users/{id}` → `User`
- `PATCH /users/{id}` `{ "email"?, "role"?, "package_id"?, "shell"?, "password"? }` → `User` (ganti `password` perlu reauth)
- `DELETE /users/{id}` → `200 { "job_id": "j_...|null" }` (perlu reauth)
- `POST /users/{id}/suspend` / `POST /users/{id}/unsuspend` → `User`

### Packages (admin: CRUD; reseller: CRUD paket miliknya + baca paket global; user: baca paketnya)
- `GET /packages` → `{ items: [Package], total }`
- `POST /packages` (body = field Package tanpa id/timestamps) → `201 Package`
- `GET /packages/{id}` → `Package`
- `PATCH /packages/{id}` → `Package`
- `DELETE /packages/{id}` → `204` (409 bila masih dipakai user)

### API keys (milik user yang login)
- `GET /api-keys` → `{ items: [ApiKey] }`
- `POST /api-keys` `{ "name", "scopes": ["read"|"write"], "allowed_ips"?: [], "expires_in_days"?: 90 }` → `201 { "api_key": ApiKey, "token": "stk_..." }` (perlu reauth; token hanya ditampilkan sekali)
- `DELETE /api-keys/{id}` → `204`

### Audit (admin)
- `GET /audit?action=&actor=&limit=&offset=` → `{ items: [AuditEntry], total }`
- `GET /audit/verify` → `{ "valid": true, "checked": 120, "broken_at": null }`

### Jobs (admin: semua; lainnya: miliknya)
- `GET /jobs?limit=&offset=` → `{ items: [Job], total }`
- `GET /jobs/{id}` → `Job`

### System (admin)
- `GET /system/info` →
  ```json
  { "hostname": "srv1", "os": "Ubuntu 24.04 LTS", "kernel": "6.8.0", "version": "0.1.0",
    "uptime_seconds": 12345, "cpu_cores": 4, "load_avg": [0.1,0.2,0.3],
    "memory_total_bytes": 8000000000, "memory_available_bytes": 5000000000,
    "agent": { "connected": true, "dry_run": false, "version": "0.1.0" },
    "counts": { "users": 3, "packages": 2, "jobs_running": 0 } }
  ```
  `load_avg`, memori, dan `kernel` bisa `null` di OS non-Linux.


---

# Stako REST API v1 — Tambahan Fase 1 (Web Inti)

## Impersonation (`7.16`)
- `GET /auth/me` sekarang menyertakan `"impersonator": User|null` bila sesi sedang dalam mode *act-as* (`user` adalah akun target yang sedang dimasuki).
- `POST /auth/impersonate` `{ "user_id": "u_...", "reason"?: "..." }` → `200 { "user": User, "impersonator": User, "csrf_token": "..." }` (perlu reauth; admin bisa masuk ke reseller/user, reseller hanya ke user miliknya; tidak boleh berantai).
- `POST /auth/impersonate/exit` → `200 { "user": User, "impersonator": null, "csrf_token": "..." }` (kembali ke akun asli tanpa login ulang).

## IP Manager (`7.14`, `7.15`)
```jsonc
// IPAddress
{ "id": "ip_...", "ip": "203.0.113.10", "family": "ipv4|ipv6", "scope": "shared|dedicated",
  "visibility": "public|internal", "interface_name": "eth0", "owner_id": "u_...|null", "created_at": "..." }
```
- `GET /ips` → `{ "items": [IPAddress], "total": N }` (admin: semua; reseller: shared + miliknya; user: shared)
- `POST /ips` `{ "ip", "scope", "visibility"?, "interface_name"?, "owner_id"? }` → `201 IPAddress` (admin)
- `POST /ips/sync` → `200 { "added": N, "items": [IPAddress] }` (admin, deteksi IP server lewat agent)
- `DELETE /ips/{id}` → `204` (admin, 409 bila dipakai domain)

## Domains / Sites & Reverse Proxy (`7.2`, `7.3`)
```jsonc
// Domain
{ "id": "d_...", "user_id": "u_...", "domain": "example.com", "aliases": ["www.example.com"],
  "stack": "nginx|nginx_apache|apache", "template": "php|wordpress|laravel|static|spa|proxy|redirect",
  "docroot": "/home/budi/web/example.com/public_html", "php_version": "8.3",
  "ssl_enabled": true, "ssl_mode": "none|letsencrypt|custom", "force_https": true, "hsts": false,
  "ipv4_id": "ip_...|null", "ipv6_id": "ip_...|null", "proxy_target": "127.0.0.1:3000",
  "status": "active|suspended", "created_at": "...", "updated_at": "..." }

// ProxyRule
{ "id": "pr_...", "domain_id": "d_...", "path": "/api", "upstream": "127.0.0.1:8080",
  "websocket": true, "sse": false, "grpc": false, "created_at": "..." }
```
- `GET /domains?q=&limit=&offset=` → `{ "items": [Domain], "total": N }`
- `POST /domains` `{ "domain", "user_id"?, "aliases"?, "stack"?, "template"?, "php_version"?, "ssl_mode"?, "force_https"?, "hsts"?, "ipv4_id"?, "ipv6_id"?, "proxy_target"? }` → `201 { "domain": Domain, "job_id": "j_..." }`
- `GET /domains/{id}` → `Domain`
- `PATCH /domains/{id}` `{ "aliases"?, "stack"?, "template"?, "php_version"?, "force_https"?, "hsts"?, "ipv4_id"?, "ipv6_id"?, "proxy_target"? }` → `200 { "domain": Domain, "job_id": "j_..." }`
- `DELETE /domains/{id}` → `200 { "job_id": "j_..." }`
- `POST /domains/{id}/ssl` `{ "mode": "letsencrypt" }` → `200 { "job_id": "j_..." }`
- `GET /domains/{id}/proxy-rules` → `{ "items": [ProxyRule] }`
- `POST /domains/{id}/proxy-rules` `{ "path", "upstream", "websocket"?, "sse"?, "grpc"? }` → `201 ProxyRule`
- `DELETE /domains/{id}/proxy-rules/{ruleId}` → `204`

## Apps / Process Manager (`7.3`)
```jsonc
// App (env keys disensor pada listing/get biasa; nilai asli hanya via GET /apps/{id}/env dengan reauth)
{ "id": "a_...", "user_id": "u_...", "domain_id": "d_...|null", "name": "api-go",
  "runtime": "node|go|python", "runtime_version": "22", "work_dir": "/home/budi/apps/api-go",
  "command": "./server", "port": 3001, "env_keys": ["PORT","DB_URL"],
  "status": "stopped|running|failed", "unit_name": "stako-app-a_...", "created_at": "...", "updated_at": "..." }
```
- `GET /apps?limit=&offset=` → `{ "items": [App], "total": N }`
- `POST /apps` `{ "name", "user_id"?, "domain_id"?, "runtime", "runtime_version"?, "work_dir"?, "command", "port"?, "env"?: { "K": "V" } }` → `201 { "app": App, "job_id": "j_..." }` (port `0`/kosong = panel pilihkan port bebas `3000-3999`)
- `GET /apps/{id}` → `App`
- `GET /apps/{id}/env` → `{ "env": { "K": "V" } }` (perlu reauth; tercatat di audit log)
- `PATCH /apps/{id}` `{ "command"?, "runtime_version"?, "port"?, "domain_id"?, "env"? }` → `200 { "app": App, "job_id": "j_..." }`
- `DELETE /apps/{id}` → `200 { "job_id": "j_..." }`
- `POST /apps/{id}/start` | `/stop` | `/restart` → `200 { "job_id": "j_..." }`
- `GET /apps/{id}/logs` → `{ "logs": "..." }`


---

# Stako REST API v1 — Tambahan Fase 2 (Data dan Akses)

## Databases (`7.5`)
```jsonc
// Database
{ "id": "db_...", "user_id": "u_...", "name": "app_prod", "engine": "mariadb|postgres",
  "db_user": "app_user", "remote_hosts": ["127.0.0.1"], "size_mb": 0, "created_at": "..." }
```
- `GET /databases?limit=&offset=` → `{ "items": [Database], "total": N }`
- `POST /databases` `{ "name", "engine": "mariadb"|"postgres", "user_id"?, "db_user"?, "db_password", "remote_hosts"?: [] }` → `201 { "database": Database, "job_id": "j_..." }`
- `DELETE /databases/{id}` → `200 { "job_id": "j_..." }`
- `POST /databases/{id}/backup` → `200 { "job_id": "j_..." }`

## FTP / SFTP (`7.7`)
```jsonc
// FtpAccount (plain FTP selalu nonaktif; hanya FTPS atau SFTP)
{ "id": "ftp_...", "user_id": "u_...", "username": "budi_ftp", "home_dir": "/home/budi/web",
  "quota_mb": 1024, "protocol": "ftps|sftp", "created_at": "..." }
```
- `GET /ftp` → `{ "items": [FtpAccount], "total": N }`
- `POST /ftp` `{ "username", "password", "user_id"?, "home_dir"?, "quota_mb"?, "protocol"?: "ftps"|"sftp" }` → `201 { "ftp": FtpAccount, "job_id": "j_..." }`
- `DELETE /ftp/{id}` → `200 { "job_id": "j_..." }`

## Firewall & Proteksi Brute Force (`7.10`, admin)
```jsonc
// FirewallRule
{ "id": "fw_...", "action": "allow|deny", "protocol": "tcp|udp|any",
  "port": "443", "source_cidr": "0.0.0.0/0", "comment": "HTTPS", "created_at": "..." }
// BannedIP
{ "ip": "198.51.100.42", "jail": "sshd", "reason": "brute-force", "banned_at": "..." }
```
- `GET /firewall/rules` → `{ "items": [FirewallRule], "total": N }`
- `POST /firewall/rules` `{ "action", "protocol", "port", "source_cidr"?, "comment"? }` → `201 { "rule": FirewallRule, "job_id": "j_..." }`
- `DELETE /firewall/rules/{id}` → `200 { "job_id": "j_..." }`
- `GET /firewall/bans` → `{ "items": [BannedIP] }`
- `POST /firewall/bans/unban` `{ "ip", "jail"? }` → `200 { "job_id": "j_..." }`

## Security Check, Updates & Pemindai CVE (`7.18`, `9.3`, admin)
```jsonc
// SecurityFinding
{ "id": "sf_...", "category": "cve|hardening|ssh|tls", "severity": "critical|high|medium|low",
  "title": "...", "description": "...", "remediation": "...",
  "status": "open|resolved|accepted", "component": "openssh-server", "detected_at": "...", "updated_at": "..." }
```
- `GET /security/findings` → `{ "score": 92, "items": [SecurityFinding], "total": N }`
- `POST /security/scan` → `200 { "score": 92, "items": [SecurityFinding] }`
- `PATCH /security/findings/{id}` `{ "status": "open|resolved|accepted" }` → `200 SecurityFinding`
- `GET /updates` → `{ "reboot_required": false, "items": [{ "package": "...", "current": "...", "candidate": "...", "category": "security|regular|kernel" }] }`
- `POST /updates/apply` `{ "mode": "security|all", "dry_run"?: true }` → `200 { "job_id": "j_..." }`


---

# Stako REST API v1 — Tambahan Fase 3 (Docker, App Installer, Redis, RabbitMQ)

## Docker Rootless Manager (`7.4`, `SEC-S13`, `SEC-S14`)
```jsonc
// Container
{ "id": "c_...", "user_id": "u_...", "domain_id": "d_...|null", "name": "web-redis",
  "image": "nginx:alpine", "ports": ["127.0.0.1:8081:80"], "volumes": [],
  "cpu_limit": "1.0", "memory_mb": 512, "status": "running|stopped", "created_at": "..." }

// ComposeStack
{ "id": "cs_...", "user_id": "u_...", "name": "my-stack", "compose_yaml": "...",
  "status": "running|stopped", "created_at": "..." }
```
- `GET /docker/containers` → `{ "items": [Container], "total": N }`
- `POST /docker/containers` `{ "name", "image", "user_id"?, "domain_id"?, "ports"?: [], "volumes"?: [], "env"?: {}, "cpu_limit"?, "memory_mb"? }` → `201 { "container": Container, "job_id": "j_..." }` (menolak `privileged`, `docker.sock`, atau bind mount sistem)
- `POST /docker/containers/{id}/start` | `/stop` | `/restart` → `200 { "job_id": "j_..." }`
- `DELETE /docker/containers/{id}` → `200 { "job_id": "j_..." }`
- `GET /docker/containers/{id}/logs` → `{ "logs": "..." }`
- `GET /docker/stacks` → `{ "items": [ComposeStack] }`
- `POST /docker/stacks` `{ "name", "compose_yaml", "user_id"? }` → `201 { "stack": ComposeStack, "job_id": "j_..." }`
- `DELETE /docker/stacks/{id}` → `200 { "job_id": "j_..." }`

## App Installer (`7.11`)
```jsonc
// CatalogApp
{ "slug": "wordpress", "name": "WordPress", "type": "php|docker", "category": "CMS|Automation|DevTools|Analytics",
  "version": "6.7", "description": "..." }
// AppInstall
{ "id": "ai_...", "user_id": "u_...", "domain_id": "d_...", "app_slug": "wordpress",
  "status": "installed|installing|failed", "created_at": "..." }
```
- `GET /catalog` → `{ "items": [CatalogApp] }`
- `GET /catalog/installs` → `{ "items": [AppInstall] }`
- `POST /catalog/install` `{ "app_slug", "domain_id", "user_id"?, "admin_email"?, "site_title"? }` → `201 { "install": AppInstall, "job_id": "j_..." }`

## Redis & RabbitMQ (`7.9`)
```jsonc
// RedisInstance
{ "id": "rd_...", "user_id": "u_...", "name": "cache-1", "port": 6380, "memory_mb": 256,
  "persistent": false, "status": "running", "created_at": "..." }
// RabbitVhost
{ "id": "rmq_...", "user_id": "u_...", "vhost": "/budi", "username": "budi_mq",
  "status": "active", "created_at": "..." }
```
- `GET /services/redis` → `{ "items": [RedisInstance] }`
- `POST /services/redis` `{ "name", "user_id"?, "memory_mb"?, "persistent"? }` → `201 { "instance": RedisInstance, "connection_string": "redis://...", "job_id": "j_..." }`
- `DELETE /services/redis/{id}` → `200 { "job_id": "j_..." }`
- `GET /services/rabbitmq` → `{ "items": [RabbitVhost] }`
- `POST /services/rabbitmq` `{ "vhost", "username", "password", "user_id"? }` → `201 { "vhost": RabbitVhost, "connection_string": "amqp://...", "job_id": "j_..." }`
- `DELETE /services/rabbitmq/{id}` → `200 { "job_id": "j_..." }`


---

# Stako REST API v1 — Tambahan Fase 4 (Mail, Backup, Webhook, Lockdown)

## Mail Server (`7.6`, Postfix + Dovecot + Rspamd)
```jsonc
// MailDomain
{ "id": "md_...", "user_id": "u_...", "domain": "example.com", "dkim_selector": "default",
  "dkim_public_key": "v=DKIM1; k=rsa; p=...", "created_at": "..." }
// Mailbox
{ "id": "mb_...", "mail_domain_id": "md_...", "email": "budi@example.com",
  "quota_mb": 1024, "created_at": "..." }
```
- `GET /mail/domains` → `{ "items": [MailDomain] }`
- `POST /mail/domains` `{ "domain", "user_id"? }` → `201 { "mail_domain": MailDomain, "dns_records": [{ "type", "name", "value" }], "job_id": "j_..." }`
- `DELETE /mail/domains/{id}` → `204`
- `GET /mail/domains/{id}/mailboxes` → `{ "items": [Mailbox] }`
- `POST /mail/domains/{id}/mailboxes` `{ "local_part", "password", "quota_mb"? }` → `201 Mailbox`
- `DELETE /mail/mailboxes/{id}` → `204`

## Backup Data & Konfigurasi Sistem (`7.12`, `7.19`)
```jsonc
// Backup
{ "id": "bk_...", "user_id": "u_...|null", "kind": "user_data|system_config",
  "target": "local|s3|sftp", "size_bytes": 0, "checksum": "sha256:...",
  "status": "completed|running|failed", "created_at": "..." }
```
- `GET /backups` → `{ "items": [Backup] }`
- `POST /backups` `{ "kind": "user_data"|"system_config", "target": "local"|"s3"|"sftp", "user_id"? }` → `201 { "backup": Backup, "job_id": "j_..." }`
- `POST /backups/{id}/restore` → `200 { "job_id": "j_..." }` (perlu reauth)
- `DELETE /backups/{id}` → `204`

## Webhook & Billing (`7.17`)
- `GET /webhooks` → `{ "items": [Webhook] }`
- `POST /webhooks` `{ "url", "events": ["account.created", ...], "secret"? }` → `201 { "webhook": Webhook, "secret": "..." }` (menolak URL internal/metadata SSRF)
- `DELETE /webhooks/{id}` → `204`

## Mode Lockdown (`SEC-S23`, admin)
- `GET /security/lockdown` → `{ "enabled": false, "allowed_ips": [] }`
- `POST /security/lockdown` `{ "enabled": true|false, "allowed_ips"?: ["..."] }` → `200 { "enabled": true, "allowed_ips": [...] }` (perlu reauth)


---

# Stako REST API v1 — Tambahan Fase 5 (DNS, Cron, File Manager, Monitoring, Web Terminal)

## DNS (PowerDNS / Cloudflare) (`7.13`)
- `GET /dns/zones` → `{ "items": [DNSZone] }`
- `POST /dns/zones` `{ "domain", "provider": "powerdns"|"cloudflare", "user_id"? }` → `201 DNSZone`
- `DELETE /dns/zones/{id}` → `204`
- `GET /dns/zones/{id}/records` → `{ "items": [DNSRecord] }`
- `POST /dns/zones/{id}/records` `{ "type", "name", "content", "ttl"?, "priority"? }` → `201 DNSRecord`
- `DELETE /dns/records/{recordId}` → `204`

## Cron Jobs (`7.13`)
- `GET /cron` → `{ "items": [CronJob] }`
- `POST /cron` `{ "schedule", "command", "user_id"? }` → `201 CronJob`
- `DELETE /cron/{id}` → `204`

## File Manager (`7.13`, `SEC-A12`)
- `GET /files?path=` → `{ "path": "...", "items": [{ "name", "is_dir", "size", "mode", "mod_time" }] }`
- `GET /files/read?path=` → `{ "path": "...", "content": "..." }`
- `POST /files/write` `{ "path", "content" }` → `200 { "ok": true }`
- `POST /files/delete` `{ "path" }` → `200 { "ok": true }`

## Monitoring & Alerts (`7.13`, admin)
- `GET /monitoring/metrics` → `{ "cpu_percent": 12.5, "memory_percent": 45.0, "disk_percent": 28.0, "services": [{ "name": "nginx", "status": "active" }], "timestamp": "..." }`
- `GET /monitoring/alerts` → `{ "items": [AlertRule] }`
- `POST /monitoring/alerts` `{ "name", "channel": "email|telegram|slack|webhook", "target", "threshold" }` → `201 AlertRule`
- `DELETE /monitoring/alerts/{id}` → `204`

## Web Terminal (`7.8 FR-S5`, `SEC-A18`)
- `POST /terminal/exec` `{ "command" }` → `{ "output": "...", "exit_code": 0 }` (wajib shell != `nologin`, tercatat di audit log)


---

# Stako REST API v1 — Tambahan Fase 6 (WAF, Git Deploy, Multi-Server, Import)

## WAF (Coraza / ModSecurity) (`7.10 FR-X5`, admin)
- `GET /waf` → `{ "enabled": false, "engine": "coraza", "mode": "detection", "paranoia_level": 1 }`
- `POST /waf` `{ "enabled", "engine": "coraza|modsecurity", "mode": "detection|blocking", "paranoia_level": 1..4 }` → `200 { "waf": ..., "job_id": "j_..." }`

## Git Deploy (`7.3 FR-P8`)
- `GET /git-deploys` → `{ "items": [GitDeploy] }`
- `POST /git-deploys` `{ "domain_id", "repo_url", "branch"?, "build_cmd"?, "user_id"? }` → `201 { "deploy": GitDeploy, "webhook_secret": "..." }`
- `POST /git-deploys/{id}/trigger` → `200 { "job_id": "j_..." }`
- `DELETE /git-deploys/{id}` → `204`

## Multi-Server Nodes & Panel Import (`7.13`, admin)
- `GET /nodes` → `{ "items": [ServerNode] }`
- `POST /nodes` `{ "name", "host", "port"?, "role"?: "worker|db|mail" }` → `201 { "node": ServerNode, "agent_token": "..." }`
- `DELETE /nodes/{id}` → `204`
- `POST /import` `{ "source_panel": "hestiacp|cpanel|vestacp", "archive_path" }` → `202 { "job_id": "j_..." }`
