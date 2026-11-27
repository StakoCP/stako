// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"

	"github.com/StakoCP/stako/internal/version"
	"gopkg.in/yaml.v3"
)

func op(tag, summary string) map[string]any {
	return map[string]any{
		"tags":    []string{tag},
		"summary": summary,
	}
}

func buildOpenAPISpec() map[string]any {
	paths := map[string]any{
		"/health":                       map[string]any{"get": op("System", "Health check")},
		"/status":                       map[string]any{"get": op("Monitoring", "Public uptime status page")},
		"/openapi.json":                 map[string]any{"get": op("OpenAPI", "OpenAPI 3.1 JSON specification")},
		"/openapi.yaml":                 map[string]any{"get": op("OpenAPI", "OpenAPI 3.1 YAML specification")},
		"/docs":                         map[string]any{"get": op("OpenAPI", "Interactive API endpoint reference")},
		"/auth/login":                   map[string]any{"post": op("Auth", "Authenticate user")},
		"/auth/logout":                  map[string]any{"post": op("Auth", "Sign out")},
		"/auth/me":                      map[string]any{"get": op("Auth", "Current user & CSRF token")},
		"/auth/reauth":                  map[string]any{"post": op("Auth", "Step-up re-authentication")},
		"/auth/password":                map[string]any{"post": op("Auth", "Change own password")},
		"/auth/2fa/setup":               map[string]any{"post": op("Auth", "Initialize TOTP 2FA")},
		"/auth/2fa/enable":              map[string]any{"post": op("Auth", "Enable TOTP 2FA")},
		"/auth/2fa/disable":             map[string]any{"post": op("Auth", "Disable TOTP 2FA")},
		"/auth/sessions":                map[string]any{"get": op("Auth", "List active sessions")},
		"/auth/sessions/{id}":           map[string]any{"delete": op("Auth", "Revoke a session")},
		"/auth/sessions/revoke-others":  map[string]any{"post": op("Auth", "Revoke all other sessions")},
		"/auth/impersonate":             map[string]any{"post": op("Auth", "Impersonate subordinate user")},
		"/auth/impersonate/exit":        map[string]any{"post": op("Auth", "Exit impersonation")},
		"/capabilities":                 map[string]any{"get": op("System", "Installed service capabilities")},
		"/users":                        map[string]any{"get": op("Users", "List users"), "post": op("Users", "Create user")},
		"/users/{id}":                   map[string]any{"get": op("Users", "Get user"), "patch": op("Users", "Update user"), "delete": op("Users", "Delete user")},
		"/users/{id}/suspend":           map[string]any{"post": op("Users", "Suspend user")},
		"/users/{id}/unsuspend":         map[string]any{"post": op("Users", "Unsuspend user")},
		"/packages":                     map[string]any{"get": op("Packages", "List packages"), "post": op("Packages", "Create package")},
		"/packages/{id}":                map[string]any{"get": op("Packages", "Get package"), "patch": op("Packages", "Update package"), "delete": op("Packages", "Delete package")},
		"/domains":                      map[string]any{"get": op("Domains", "List domains"), "post": op("Domains", "Create domain")},
		"/domains/{id}":                 map[string]any{"get": op("Domains", "Get domain"), "patch": op("Domains", "Update domain"), "delete": op("Domains", "Delete domain")},
		"/domains/{id}/ssl":             map[string]any{"post": op("Domains", "Issue Let's Encrypt SSL")},
		"/domains/{id}/logs":            map[string]any{"get": op("Domains", "Read domain access/error logs")},
		"/domains/{id}/custom-nginx":    map[string]any{"get": op("Domains", "Read custom Nginx include"), "put": op("Domains", "Write & validate custom Nginx include")},
		"/domains/{id}/cache":           map[string]any{"get": op("Cache", "Get domain Cache & Cloudflare config")},
		"/domains/{id}/cache/microcache": map[string]any{"put": op("Cache", "Configure Nginx Microcache")},
		"/domains/{id}/cache/redis-object": map[string]any{"post": op("Cache", "Toggle 1-click Redis Object Cache")},
		"/domains/{id}/cloudflare":      map[string]any{"put": op("Cache", "Configure Cloudflare CDN & protection modes")},
		"/domains/{id}/cloudflare/purge": map[string]any{"post": op("Cache", "Purge Cloudflare zone cache")},
		"/domains/{id}/proxy-rules":     map[string]any{"get": op("Domains", "List proxy rules"), "post": op("Domains", "Add proxy rule")},
		"/domains/{id}/proxy-rules/{ruleId}": map[string]any{"delete": op("Domains", "Delete proxy rule")},
		"/apps":                         map[string]any{"get": op("Apps", "List apps"), "post": op("Apps", "Create app")},
		"/apps/{id}":                    map[string]any{"get": op("Apps", "Get app"), "patch": op("Apps", "Update app"), "delete": op("Apps", "Delete app")},
		"/apps/{id}/start":              map[string]any{"post": op("Apps", "Start app")},
		"/apps/{id}/stop":               map[string]any{"post": op("Apps", "Stop app")},
		"/apps/{id}/restart":            map[string]any{"post": op("Apps", "Restart app")},
		"/apps/{id}/env":                map[string]any{"get": op("Apps", "Reveal decrypted app env")},
		"/ips":                          map[string]any{"get": op("IPs", "List IP addresses"), "post": op("IPs", "Add IP address")},
		"/ips/sync":                     map[string]any{"post": op("IPs", "Sync IPs from host")},
		"/ips/{id}":                     map[string]any{"delete": op("IPs", "Delete IP address")},
		"/databases":                    map[string]any{"get": op("Databases", "List databases"), "post": op("Databases", "Create database")},
		"/databases/{id}":               map[string]any{"delete": op("Databases", "Delete database")},
		"/databases/{id}/backup":        map[string]any{"post": op("Databases", "Backup database")},
		"/databases/postgres/advanced":  map[string]any{"get": op("Databases", "Get PostgreSQL PgBouncer, replication & PITR config/status")},
		"/databases/postgres/pgbouncer": map[string]any{"put": op("Databases", "Configure PgBouncer connection pooler")},
		"/databases/postgres/replication": map[string]any{"put": op("Databases", "Configure PostgreSQL streaming replication")},
		"/databases/postgres/pitr":      map[string]any{"put": op("Databases", "Configure PostgreSQL WAL archiving (PITR)")},
		"/databases/postgres/pitr/basebackup": map[string]any{"post": op("Databases", "Take PostgreSQL PITR base backup")},
		"/databases/postgres/pitr/restore": map[string]any{"post": op("Databases", "Restore PostgreSQL to point-in-time")},
		"/ftp":                          map[string]any{"get": op("FTP", "List FTP accounts"), "post": op("FTP", "Create FTP account")},
		"/ftp/{id}":                     map[string]any{"delete": op("FTP", "Delete FTP account")},
		"/firewall/rules":               map[string]any{"get": op("Firewall", "List firewall rules"), "post": op("Firewall", "Create firewall rule")},
		"/firewall/rules/{id}":          map[string]any{"delete": op("Firewall", "Delete firewall rule")},
		"/firewall/bans":                map[string]any{"get": op("Firewall", "List Fail2Ban bans")},
		"/firewall/unban":               map[string]any{"post": op("Firewall", "Unban IP address")},
		"/security/scan":                map[string]any{"post": op("Security", "Run host security scan")},
		"/security/findings":            map[string]any{"get": op("Security", "List security findings")},
		"/security/findings/{id}":       map[string]any{"patch": op("Security", "Update finding status")},
		"/security/lockdown":            map[string]any{"get": op("Security", "Get lockdown status"), "post": op("Security", "Toggle emergency lockdown")},
		"/security/rotate-keys":         map[string]any{"post": op("Security", "Rotate encryption keys")},
		"/security/forensics":           map[string]any{"get": op("Security", "Export forensics bundle")},
		"/updates":                      map[string]any{"get": op("System", "List pending OS updates")},
		"/updates/apply":                map[string]any{"post": op("System", "Apply OS updates")},
		"/docker/containers":            map[string]any{"get": op("Docker", "List containers"), "post": op("Docker", "Create container from image or GitHub")},
		"/docker/containers/{id}/start":   map[string]any{"post": op("Docker", "Start container")},
		"/docker/containers/{id}/stop":    map[string]any{"post": op("Docker", "Stop container")},
		"/docker/containers/{id}/restart": map[string]any{"post": op("Docker", "Restart container")},
		"/docker/containers/{id}/rebuild": map[string]any{"post": op("Docker", "Rebuild container from GitHub")},
		"/docker/containers/{id}/logs":    map[string]any{"get": op("Docker", "Get container logs")},
		"/docker/containers/{id}":         map[string]any{"delete": op("Docker", "Delete container")},
		"/docker/stacks":                  map[string]any{"get": op("Docker", "List Compose stacks"), "post": op("Docker", "Deploy Compose stack")},
		"/docker/stacks/{id}":             map[string]any{"delete": op("Docker", "Delete Compose stack")},
		"/catalog":                        map[string]any{"get": op("Catalog", "List 1-click app catalog")},
		"/catalog/installs":               map[string]any{"get": op("Catalog", "List installed catalog apps")},
		"/catalog/install":                map[string]any{"post": op("Catalog", "Install catalog app")},
		"/catalog/installs/{id}":          map[string]any{"patch": op("Catalog", "Update or redeploy installed app"), "delete": op("Catalog", "Uninstall catalog app")},
		"/services/redis":                 map[string]any{"get": op("Services", "List Redis instances"), "post": op("Services", "Create Redis instance")},
		"/services/redis/{id}":            map[string]any{"delete": op("Services", "Delete Redis instance")},
		"/services/rabbitmq":              map[string]any{"get": op("Services", "List RabbitMQ vhosts"), "post": op("Services", "Create RabbitMQ vhost")},
		"/services/rabbitmq/{id}":         map[string]any{"delete": op("Services", "Delete RabbitMQ vhost")},
		"/mail/domains":                   map[string]any{"get": op("Mail", "List mail domains"), "post": op("Mail", "Create mail domain")},
		"/mail/domains/{id}":              map[string]any{"delete": op("Mail", "Delete mail domain")},
		"/mail/domains/{id}/dns":          map[string]any{"get": op("Mail", "Get DKIM/SPF/DMARC DNS records")},
		"/mail/domains/{id}/mailboxes":    map[string]any{"get": op("Mail", "List mailboxes"), "post": op("Mail", "Create mailbox")},
		"/mail/mailboxes/{mailboxId}":     map[string]any{"delete": op("Mail", "Delete mailbox")},
		"/mail/relay":                     map[string]any{"get": op("Mail", "Get server-wide SMTP relay config"), "put": op("Mail", "Set server-wide SMTP relay (smarthost)")},
		"/mail/domains/{id}/relay":        map[string]any{"put": op("Mail", "Set per-domain SMTP relay (smarthost)")},
		"/mail/relay/test":                map[string]any{"post": op("Mail", "Test SMTP relay TCP connectivity")},
		"/backups":                        map[string]any{"get": op("Backups", "List backups"), "post": op("Backups", "Create backup")},
		"/backups/{id}/restore":           map[string]any{"post": op("Backups", "Restore backup")},
		"/backups/{id}/download":          map[string]any{"get": op("Backups", "Download backup manifest")},
		"/backups/{id}":                   map[string]any{"delete": op("Backups", "Delete backup")},
		"/webhooks":                       map[string]any{"get": op("Webhooks", "List webhooks"), "post": op("Webhooks", "Create webhook")},
		"/webhooks/{id}":                  map[string]any{"delete": op("Webhooks", "Delete webhook")},
		"/webhooks/deliveries":            map[string]any{"get": op("Webhooks", "List recent webhook deliveries")},
		"/webhooks/{id}/test":             map[string]any{"post": op("Webhooks", "Send test webhook delivery")},
		"/dns/zones":                      map[string]any{"get": op("DNS", "List DNS zones"), "post": op("DNS", "Create DNS zone")},
		"/dns/zones/{id}":                 map[string]any{"delete": op("DNS", "Delete DNS zone")},
		"/dns/zones/{id}/records":         map[string]any{"get": op("DNS", "List DNS records"), "post": op("DNS", "Create DNS record")},
		"/dns/records/{recordId}":         map[string]any{"delete": op("DNS", "Delete DNS record")},
		"/cron":                           map[string]any{"get": op("Cron", "List cron jobs"), "post": op("Cron", "Create cron job")},
		"/cron/{id}":                      map[string]any{"delete": op("Cron", "Delete cron job")},
		"/files":                          map[string]any{"get": op("Files", "List directory")},
		"/files/read":                     map[string]any{"get": op("Files", "Read file")},
		"/files/write":                    map[string]any{"post": op("Files", "Write file")},
		"/files/delete":                   map[string]any{"post": op("Files", "Delete file or directory")},
		"/monitoring/metrics":             map[string]any{"get": op("Monitoring", "Live CPU, memory, disk & service metrics")},
		"/monitoring/alerts":              map[string]any{"get": op("Monitoring", "List alert rules"), "post": op("Monitoring", "Create alert rule")},
		"/monitoring/alerts/{id}":         map[string]any{"delete": op("Monitoring", "Delete alert rule")},
		"/monitoring/uptime":              map[string]any{"get": op("Monitoring", "List per-domain uptime monitors"), "post": op("Monitoring", "Create uptime monitor")},
		"/monitoring/uptime/{id}/check":   map[string]any{"post": op("Monitoring", "Run HTTP/SSL/domain expiry check")},
		"/monitoring/uptime/{id}":         map[string]any{"delete": op("Monitoring", "Delete uptime monitor")},
		"/monitoring/hardware":            map[string]any{"get": op("Monitoring", "SMART disk health, inode usage & memory/swap PSI")},
		"/monitoring/quotas":              map[string]any{"get": op("Monitoring", "List per-user disk & inode project quotas")},
		"/monitoring/quotas/{userId}":     map[string]any{"put": op("Monitoring", "Set per-user disk & inode project quota")},
		"/logs":                           map[string]any{"get": op("Logs", "Query centralized cross-service logs")},
		"/logs/rotation":                  map[string]any{"get": op("Logs", "Get log rotation policy"), "put": op("Logs", "Update log rotation policy")},
		"/logs/rotate":                    map[string]any{"post": op("Logs", "Trigger immediate log rotation")},
		"/logs/shipping":                  map[string]any{"get": op("Logs", "Get Loki & syslog shipping config"), "put": op("Logs", "Update Loki & syslog shipping config")},
		"/logs/shipping/test":             map[string]any{"post": op("Logs", "Test Loki or syslog connectivity")},
		"/terminal/exec":                  map[string]any{"post": op("Terminal", "Execute audited shell command")},
		"/waf":                            map[string]any{"get": op("Security", "Get WAF configuration"), "post": op("Security", "Update & apply WAF configuration")},
		"/git-deploys":                    map[string]any{"get": op("GitDeploy", "List Git deploy targets"), "post": op("GitDeploy", "Create Git deploy target")},
		"/git-deploys/{id}/trigger":       map[string]any{"post": op("GitDeploy", "Trigger Git deploy")},
		"/git-deploys/{id}":               map[string]any{"delete": op("GitDeploy", "Delete Git deploy target")},
		"/git-previews":                   map[string]any{"get": op("GitDeploy", "List branch/PR preview environments")},
		"/git-deploys/{id}/previews":      map[string]any{"post": op("GitDeploy", "Create branch/PR preview environment")},
		"/git-previews/{id}/redeploy":     map[string]any{"post": op("GitDeploy", "Redeploy preview environment")},
		"/git-previews/{id}":              map[string]any{"delete": op("GitDeploy", "Destroy preview environment")},
		"/wordpress":                      map[string]any{"get": op("WordPress", "List WordPress installations")},
		"/wordpress/scan":                 map[string]any{"post": op("WordPress", "Discover & scan WordPress sites")},
		"/wordpress/bulk-update":          map[string]any{"post": op("WordPress", "Bulk update WordPress core/plugins/themes")},
		"/wordpress/{id}/staging":         map[string]any{"post": op("WordPress", "Create WordPress staging clone")},
		"/wordpress/{id}/push-staging":    map[string]any{"post": op("WordPress", "Push WordPress staging to live")},
		"/wordpress/{id}/harden":          map[string]any{"post": op("WordPress", "Apply WordPress security hardening")},
		"/wordpress/{id}/autologin":       map[string]any{"post": op("WordPress", "Generate one-time WP Admin auto-login URL")},
		"/wordpress/{id}":                 map[string]any{"delete": op("WordPress", "Remove WordPress site from toolkit")},
		"/nodes":                          map[string]any{"get": op("Nodes", "List cluster nodes"), "post": op("Nodes", "Register node")},
		"/nodes/{id}":                     map[string]any{"delete": op("Nodes", "Delete node")},
		"/import":                         map[string]any{"post": op("Migrations", "Import panel archive")},
		"/migrations/bundles":             map[string]any{"get": op("Migrations", "List migration bundles")},
		"/migrations/inspect":             map[string]any{"post": op("Migrations", "Inspect HestiaCP/cPanel/VestaCP/Stako archive")},
		"/migrations/export":              map[string]any{"post": op("Migrations", "Export Stako user account bundle")},
		"/migrations/import":              map[string]any{"post": op("Migrations", "Import & provision account from archive")},
		"/migrations/transfer":            map[string]any{"post": op("Migrations", "Transfer archive via SSH/SCP")},
		"/plugins":                        map[string]any{"get": op("Plugins", "List installed community plugins"), "post": op("Plugins", "Install plugin manifest")},
		"/plugins/pages":                  map[string]any{"get": op("Plugins", "List enabled plugin dashboard pages")},
		"/plugins/{id}/toggle":            map[string]any{"post": op("Plugins", "Enable or disable plugin")},
		"/plugins/{id}":                   map[string]any{"delete": op("Plugins", "Uninstall plugin")},
		"/billing/sso":                    map[string]any{"post": op("Billing", "Issue WHMCS single sign-on token")},
		"/billing/usage":                  map[string]any{"get": op("Billing", "Per-user resource usage metering")},
		"/system/configs":                 map[string]any{"get": op("System", "List editable service configs")},
		"/system/configs/{service}":       map[string]any{"get": op("System", "Read service config"), "put": op("System", "Update & validate service config")},
		"/system/update":                  map[string]any{"get": op("System", "Check for Stako updates"), "post": op("System", "Apply self-update")},
		"/system/services":                map[string]any{"get": op("System", "List detected system services")},
		"/system/php-versions":            map[string]any{"get": op("System", "List installed PHP-FPM versions")},
		"/system/services/{id}/action":    map[string]any{"post": op("System", "Start/stop/restart system service")},
		"/system/services/{id}/install":   map[string]any{"post": op("System", "Install system service")},
		"/system/services/{id}":           map[string]any{"delete": op("System", "Uninstall system service")},
		"/api-keys":                       map[string]any{"get": op("Auth", "List API keys"), "post": op("Auth", "Create API key")},
		"/api-keys/{id}":                  map[string]any{"delete": op("Auth", "Revoke API key")},
		"/audit":                          map[string]any{"get": op("Audit", "List audit log entries")},
		"/audit/verify":                   map[string]any{"get": op("Audit", "Verify audit log hash chain")},
		"/jobs":                           map[string]any{"get": op("Jobs", "List background jobs")},
		"/jobs/{id}":                      map[string]any{"get": op("Jobs", "Get job status and log")},
		"/system/info":                    map[string]any{"get": op("System", "System status and metrics")},
	}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Stako Control Panel API",
			"version": version.Version,
			"license": map[string]any{"name": "MIT"},
		},
		"servers": []map[string]any{{"url": "/api/v1"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"},
				"cookieAuth": map[string]any{"type": "apiKey", "in": "cookie", "name": "stako_session"},
			},
		},
		"paths": paths,
	}
}

func (s *Server) handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(buildOpenAPISpec())
}

func (s *Server) handleOpenAPIYAML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	out, _ := yaml.Marshal(buildOpenAPISpec())
	_, _ = w.Write(out)
}

func (s *Server) handleOpenAPIDocs(w http.ResponseWriter, r *http.Request) {
	spec := buildOpenAPISpec()
	paths, _ := spec["paths"].(map[string]any)
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var rows strings.Builder
	for _, p := range keys {
		methods, _ := paths[p].(map[string]any)
		for m, rawOp := range methods {
			mMap, _ := rawOp.(map[string]any)
			sum, _ := mMap["summary"].(string)
			tag := ""
			if tags, ok := mMap["tags"].([]string); ok && len(tags) > 0 {
				tag = tags[0]
			}
			rows.WriteString(fmt.Sprintf(
				"<tr><td><code>%s</code></td><td><code>/api/v1%s</code></td><td>%s</td><td>%s</td></tr>\n",
				html.EscapeString(strings.ToUpper(m)),
				html.EscapeString(p),
				html.EscapeString(tag),
				html.EscapeString(sum),
			))
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Stako API Docs (%s)</title></head>
<body>
<h1>Stako Control Panel API (%s)</h1>
<p>Specs: <a href="/api/v1/openapi.json">openapi.json</a> | <a href="/api/v1/openapi.yaml">openapi.yaml</a></p>
<table border="1" cellpadding="6" cellspacing="0">
<thead><tr><th>Method</th><th>Path</th><th>Tag</th><th>Summary</th></tr></thead>
<tbody>%s</tbody>
</table>
</body>
</html>`, html.EscapeString(version.Version), html.EscapeString(version.Version), rows.String())
}
