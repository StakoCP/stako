// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/StakoCP/stako/internal/version"
	"gopkg.in/yaml.v3"
)

var oaPathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

type oaSpecOpts struct {
	Req         any
	Resp        any
	Status      string
	QueryParams []string
	Public      bool
}

func op(tag, summary string, opts ...oaSpecOpts) map[string]any {
	var opt oaSpecOpts
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Status == "" {
		opt.Status = "200"
	}
	m := map[string]any{
		"tags":    []string{tag},
		"summary": summary,
	}
	if !opt.Public {
		m["security"] = []map[string][]string{
			{"bearerAuth": {}},
			{"cookieAuth": {}},
		}
	}
	if len(opt.QueryParams) > 0 {
		params := make([]map[string]any, 0, len(opt.QueryParams))
		for _, qp := range opt.QueryParams {
			params = append(params, map[string]any{
				"name":   qp,
				"in":     "query",
				"schema": map[string]any{"type": "string"},
			})
		}
		m["parameters"] = params
	}
	if opt.Req != nil {
		m["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{
					"schema":  map[string]any{"type": "object"},
					"example": opt.Req,
				},
			},
		}
	}
	respObj := map[string]any{
		"description": "Success",
	}
	if opt.Resp != nil {
		respObj["content"] = map[string]any{
			"application/json": map[string]any{
				"schema":  map[string]any{"type": "object"},
				"example": opt.Resp,
			},
		}
	}
	m["responses"] = map[string]any{
		opt.Status: respObj,
		"422": map[string]any{
			"description": "Validation Error (RFC 7807)",
			"content": map[string]any{
				"application/problem+json": map[string]any{
					"example": map[string]any{
						"type":   "urn:stako:problem:validation",
						"title":  "Validation failed",
						"status": 422,
						"detail": "Invalid input field",
					},
				},
			},
		},
	}
	return m
}

func attachPathParams(path string, methods map[string]any) {
	matches := oaPathParamRe.FindAllStringSubmatch(path, -1)
	if len(matches) == 0 {
		return
	}
	pathParams := make([]map[string]any, 0, len(matches))
	for _, mm := range matches {
		pathParams = append(pathParams, map[string]any{
			"name":     mm[1],
			"in":       "path",
			"required": true,
			"schema":   map[string]any{"type": "string"},
		})
	}
	for _, rawOp := range methods {
		opMap, ok := rawOp.(map[string]any)
		if !ok {
			continue
		}
		existing, _ := opMap["parameters"].([]map[string]any)
		opMap["parameters"] = append(pathParams, existing...)
	}
}

func buildOpenAPISpec() map[string]any {
	paths := map[string]any{
		"/health": map[string]any{
			"get": op("System", "Health check", oaSpecOpts{Public: true, Resp: map[string]any{"status": "ok", "version": version.Version}}),
		},
		"/status": map[string]any{
			"get": op("Monitoring", "Public uptime status page", oaSpecOpts{Public: true, Resp: map[string]any{
				"overall_status": "operational",
				"checked_at":     "2026-11-27T08:00:00Z",
				"monitors":       []map[string]any{{"domain": "example.com", "status": "up", "last_http_status": 200, "last_latency_ms": 42, "ssl_days_left": 75, "uptime_pct": 99.98}},
			}}),
		},
		"/openapi.json": map[string]any{"get": op("OpenAPI", "OpenAPI 3.1 JSON specification", oaSpecOpts{Public: true})},
		"/openapi.yaml": map[string]any{"get": op("OpenAPI", "OpenAPI 3.1 YAML specification", oaSpecOpts{Public: true})},
		"/docs":         map[string]any{"get": op("OpenAPI", "Interactive API endpoint reference", oaSpecOpts{Public: true})},

		// Auth
		"/auth/login": map[string]any{
			"post": op("Auth", "Authenticate user", oaSpecOpts{
				Public: true,
				Req:    map[string]any{"username": "admin", "password": "Strong-Password-123"},
				Resp:   map[string]any{"user": map[string]any{"id": "u_123", "username": "admin", "role": "admin"}, "csrf_token": "csrf_abc123"},
			}),
		},
		"/auth/logout":                 map[string]any{"post": op("Auth", "Sign out", oaSpecOpts{Status: "204"})},
		"/auth/me":                     map[string]any{"get": op("Auth", "Current user & CSRF token", oaSpecOpts{Resp: map[string]any{"user": map[string]any{"id": "u_123", "username": "admin", "role": "admin"}, "csrf_token": "csrf_abc123"}})},
		"/auth/reauth":                 map[string]any{"post": op("Auth", "Step-up re-authentication", oaSpecOpts{Status: "204", Req: map[string]any{"password": "Strong-Password-123"}})},
		"/auth/password":               map[string]any{"post": op("Auth", "Change own password", oaSpecOpts{Status: "204", Req: map[string]any{"current_password": "Old-Password-123", "new_password": "New-Strong-Password-456"}})},
		"/auth/2fa/setup":              map[string]any{"post": op("Auth", "Initialize TOTP 2FA", oaSpecOpts{Resp: map[string]any{"secret": "JBSWY3DPEHPK3PXP", "otpauth_url": "otpauth://totp/Stako:admin?secret=JBSWY3DPEHPK3PXP"}})},
		"/auth/2fa/enable":             map[string]any{"post": op("Auth", "Enable TOTP 2FA", oaSpecOpts{Req: map[string]any{"code": "123456"}, Resp: map[string]any{"enabled": true}})},
		"/auth/2fa/disable":            map[string]any{"post": op("Auth", "Disable TOTP 2FA", oaSpecOpts{Status: "204", Req: map[string]any{"code": "123456"}})},
		"/auth/sessions":               map[string]any{"get": op("Auth", "List active sessions", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "ses_1", "ip": "203.0.113.10", "created_at": "2026-11-27T07:00:00Z"}}}})},
		"/auth/sessions/{id}":          map[string]any{"delete": op("Auth", "Revoke a session", oaSpecOpts{Status: "204"})},
		"/auth/sessions/revoke-others": map[string]any{"post": op("Auth", "Revoke all other sessions", oaSpecOpts{Status: "204"})},
		"/auth/impersonate":            map[string]any{"post": op("Auth", "Impersonate subordinate user", oaSpecOpts{Req: map[string]any{"user_id": "u_456"}, Resp: map[string]any{"user": map[string]any{"id": "u_456", "username": "client1"}}})},
		"/auth/impersonate/exit":       map[string]any{"post": op("Auth", "Exit impersonation", oaSpecOpts{Resp: map[string]any{"user": map[string]any{"id": "u_123", "username": "admin"}}})},
		"/api-keys": map[string]any{
			"get":  op("Auth", "List API keys", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "ak_1", "name": "whmcs-billing", "prefix": "stk_live"}}}}),
			"post": op("Auth", "Create API key", oaSpecOpts{Status: "201", Req: map[string]any{"name": "ci-deploy"}, Resp: map[string]any{"id": "ak_2", "name": "ci-deploy", "token": "stk_live_xxx"}}),
		},
		"/api-keys/{id}": map[string]any{"delete": op("Auth", "Revoke API key", oaSpecOpts{Status: "204"})},

		// Users & Packages
		"/users": map[string]any{
			"get":  op("Users", "List users", oaSpecOpts{QueryParams: []string{"limit", "offset", "q"}, Resp: map[string]any{"items": []map[string]any{{"id": "u_1", "username": "mad", "email": "mad@example.com", "role": "user", "status": "active"}}, "total": 1}}),
			"post": op("Users", "Create user", oaSpecOpts{Status: "201", Req: map[string]any{"username": "mad", "email": "mad@example.com", "password": "Strong-Password-123", "role": "user", "package_id": "pkg_default"}, Resp: map[string]any{"user": map[string]any{"id": "u_1", "username": "mad", "role": "user"}}}),
		},
		"/users/{id}": map[string]any{
			"get":    op("Users", "Get user", oaSpecOpts{Resp: map[string]any{"id": "u_1", "username": "mad", "email": "mad@example.com", "role": "user"}}),
			"patch":  op("Users", "Update user", oaSpecOpts{Req: map[string]any{"email": "new@example.com", "package_id": "pkg_pro"}, Resp: map[string]any{"id": "u_1", "email": "new@example.com"}}),
			"delete": op("Users", "Delete user", oaSpecOpts{Status: "204"}),
		},
		"/users/{id}/suspend":   map[string]any{"post": op("Users", "Suspend user", oaSpecOpts{Resp: map[string]any{"status": "suspended"}})},
		"/users/{id}/unsuspend": map[string]any{"post": op("Users", "Unsuspend user", oaSpecOpts{Resp: map[string]any{"status": "active"}})},
		"/packages": map[string]any{
			"get":  op("Packages", "List packages", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "pkg_default", "name": "Starter", "max_domains": 5, "max_databases": 10}}}}),
			"post": op("Packages", "Create package", oaSpecOpts{Status: "201", Req: map[string]any{"name": "Pro", "max_domains": 25, "max_databases": 50, "disk_quota_mb": 20480}, Resp: map[string]any{"id": "pkg_pro", "name": "Pro"}}),
		},
		"/packages/{id}": map[string]any{
			"get":    op("Packages", "Get package", oaSpecOpts{Resp: map[string]any{"id": "pkg_default", "name": "Starter"}}),
			"patch":  op("Packages", "Update package", oaSpecOpts{Req: map[string]any{"max_domains": 50}, Resp: map[string]any{"id": "pkg_default", "max_domains": 50}}),
			"delete": op("Packages", "Delete package", oaSpecOpts{Status: "204"}),
		},

		// Domains, Cache & Cloudflare
		"/domains": map[string]any{
			"get":  op("Domains", "List domains", oaSpecOpts{QueryParams: []string{"limit", "offset", "q"}, Resp: map[string]any{"items": []map[string]any{{"id": "d_1", "domain": "example.com", "stack": "nginx_apache", "php_version": "8.3"}}, "total": 1}}),
			"post": op("Domains", "Create domain", oaSpecOpts{Status: "201", Req: map[string]any{"domain": "example.com", "stack": "nginx_apache", "template": "php", "php_version": "8.3", "ssl_mode": "letsencrypt"}, Resp: map[string]any{"domain": map[string]any{"id": "d_1", "domain": "example.com"}, "job_id": "job_1"}}),
		},
		"/domains/{id}": map[string]any{
			"get":    op("Domains", "Get domain", oaSpecOpts{Resp: map[string]any{"id": "d_1", "domain": "example.com", "docroot": "/home/mad/web/example.com/public_html"}}),
			"patch":  op("Domains", "Update domain", oaSpecOpts{Req: map[string]any{"php_version": "8.4", "force_https": true}, Resp: map[string]any{"domain": map[string]any{"id": "d_1", "php_version": "8.4"}, "job_id": "job_2"}}),
			"delete": op("Domains", "Delete domain", oaSpecOpts{Status: "204"}),
		},
		"/domains/{id}/ssl":  map[string]any{"post": op("Domains", "Issue Let's Encrypt SSL", oaSpecOpts{Status: "202", Resp: map[string]any{"job_id": "job_ssl_1"}})},
		"/domains/{id}/logs": map[string]any{"get": op("Domains", "Read domain access/error/goaccess logs", oaSpecOpts{QueryParams: []string{"type", "lines"}, Resp: map[string]any{"path": "/home/mad/web/example.com/logs/access.log", "logs": "203.0.113.10 - GET / 200"}})},
		"/domains/{id}/custom-nginx": map[string]any{
			"get": op("Domains", "Read custom Nginx include", oaSpecOpts{Resp: map[string]any{"content": "# custom rules"}}),
			"put": op("Domains", "Write & validate custom Nginx include", oaSpecOpts{Req: map[string]any{"content": "client_max_body_size 64M;"}, Resp: map[string]any{"ok": true}}),
		},
		"/domains/{id}/proxy-rules": map[string]any{
			"get":  op("Domains", "List proxy rules", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "pr_1", "path": "/api", "upstream": "127.0.0.1:3000"}}}}),
			"post": op("Domains", "Add proxy rule", oaSpecOpts{Status: "201", Req: map[string]any{"path": "/api", "upstream": "127.0.0.1:3000", "websocket": true}, Resp: map[string]any{"id": "pr_1", "path": "/api"}}),
		},
		"/domains/{id}/proxy-rules/{ruleId}": map[string]any{"delete": op("Domains", "Delete proxy rule", oaSpecOpts{Status: "204"})},
		"/domains/{id}/cache":                map[string]any{"get": op("Cache", "Get domain Cache & Cloudflare config", oaSpecOpts{Resp: map[string]any{"domain_id": "d_1", "microcache_enabled": true, "microcache_ttl_sec": 10, "redis_object_cache": true, "redis_port": 6380, "cf_zone_id": "zone123", "has_cf_token": true}})},
		"/domains/{id}/cache/microcache":     map[string]any{"put": op("Cache", "Configure Nginx Microcache", oaSpecOpts{Req: map[string]any{"enabled": true, "ttl_sec": 30}, Resp: map[string]any{"microcache_enabled": true, "microcache_ttl_sec": 30}})},
		"/domains/{id}/cache/redis-object":   map[string]any{"post": op("Cache", "Toggle 1-click Redis Object Cache", oaSpecOpts{Req: map[string]any{"enabled": true}, Resp: map[string]any{"redis_object_cache": true, "redis_port": 6380}})},
		"/domains/{id}/cloudflare":           map[string]any{"put": op("Cache", "Configure Cloudflare CDN & protection modes", oaSpecOpts{Req: map[string]any{"cf_zone_id": "zone123", "cf_api_token": "cf_token", "cf_under_attack": true, "cf_dev_mode": false}, Resp: map[string]any{"cf_under_attack": true, "has_cf_token": true}})},
		"/domains/{id}/cloudflare/purge":     map[string]any{"post": op("Cache", "Purge Cloudflare zone cache", oaSpecOpts{Resp: map[string]any{"ok": true, "purged": true}})},

		// Apps
		"/apps": map[string]any{
			"get":  op("Apps", "List apps", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "app_1", "name": "api-node", "runtime": "node", "port": 3001, "status": "running"}}}}),
			"post": op("Apps", "Create app", oaSpecOpts{Status: "201", Req: map[string]any{"name": "api-node", "runtime": "node", "entry_cmd": "node server.js", "port": 3001}, Resp: map[string]any{"app": map[string]any{"id": "app_1", "name": "api-node"}, "job_id": "job_app_1"}}),
		},
		"/apps/{id}": map[string]any{
			"get":    op("Apps", "Get app", oaSpecOpts{Resp: map[string]any{"id": "app_1", "name": "api-node", "status": "running"}}),
			"patch":  op("Apps", "Update app", oaSpecOpts{Req: map[string]any{"entry_cmd": "node dist/main.js"}, Resp: map[string]any{"id": "app_1"}}),
			"delete": op("Apps", "Delete app", oaSpecOpts{Status: "204"}),
		},
		"/apps/{id}/start":   map[string]any{"post": op("Apps", "Start app", oaSpecOpts{Resp: map[string]any{"job_id": "job_start_1"}})},
		"/apps/{id}/stop":    map[string]any{"post": op("Apps", "Stop app", oaSpecOpts{Resp: map[string]any{"job_id": "job_stop_1"}})},
		"/apps/{id}/restart": map[string]any{"post": op("Apps", "Restart app", oaSpecOpts{Resp: map[string]any{"job_id": "job_rst_1"}})},
		"/apps/{id}/env":     map[string]any{"get": op("Apps", "Reveal decrypted app env", oaSpecOpts{Resp: map[string]any{"env": map[string]string{"NODE_ENV": "production"}}})},

		// IPs
		"/ips": map[string]any{
			"get":  op("IPs", "List IP addresses", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "ip_1", "address": "203.0.113.10", "family": "ipv4"}}}}),
			"post": op("IPs", "Add IP address", oaSpecOpts{Status: "201", Req: map[string]any{"address": "203.0.113.11", "family": "ipv4"}, Resp: map[string]any{"id": "ip_2", "address": "203.0.113.11"}}),
		},
		"/ips/sync": map[string]any{"post": op("IPs", "Sync IPs from host", oaSpecOpts{Resp: map[string]any{"synced": 2}})},
		"/ips/{id}": map[string]any{"delete": op("IPs", "Delete IP address", oaSpecOpts{Status: "204"})},

		// Databases & PostgreSQL Advanced
		"/databases": map[string]any{
			"get":  op("Databases", "List databases", oaSpecOpts{QueryParams: []string{"limit", "offset"}, Resp: map[string]any{"items": []map[string]any{{"id": "db_1", "name": "mad_blog", "engine": "mariadb", "db_user": "mad_blog"}}, "total": 1}}),
			"post": op("Databases", "Create database (auto-prefixed with username)", oaSpecOpts{Status: "201", Req: map[string]any{"name": "blog", "engine": "mariadb", "db_password": "Strong-DB-Password-123"}, Resp: map[string]any{"database": map[string]any{"id": "db_1", "name": "mad_blog"}, "job_id": "job_db_1"}}),
		},
		"/databases/{id}":        map[string]any{"delete": op("Databases", "Delete database", oaSpecOpts{Status: "204"})},
		"/databases/{id}/backup": map[string]any{"post": op("Databases", "Backup database", oaSpecOpts{Status: "202", Resp: map[string]any{"job_id": "job_dbb_1"}})},
		"/databases/postgres/advanced": map[string]any{
			"get": op("Databases", "Get PostgreSQL PgBouncer, replication & PITR config/status", oaSpecOpts{Resp: map[string]any{
				"pgbouncer":   map[string]any{"enabled": true, "listen_port": 6432, "pool_mode": "transaction"},
				"replication": map[string]any{"role": "primary", "wal_level": "replica", "has_replication_password": true},
				"pitr":        map[string]any{"enabled": true, "archive_dir": "/var/lib/postgresql/wal_archive"},
				"status":      map[string]any{"pgbouncer_active": true, "in_recovery": false},
			}}),
		},
		"/databases/postgres/pgbouncer":       map[string]any{"put": op("Databases", "Configure PgBouncer connection pooler", oaSpecOpts{Req: map[string]any{"enabled": true, "listen_port": 6432, "pool_mode": "transaction", "max_client_conn": 300, "default_pool_size": 25}, Resp: map[string]any{"enabled": true, "listen_port": 6432}})},
		"/databases/postgres/replication":     map[string]any{"put": op("Databases", "Configure PostgreSQL streaming replication", oaSpecOpts{Req: map[string]any{"role": "primary", "wal_level": "replica", "max_wal_senders": 10, "replication_user": "replicator", "replication_pass": "Strong-Repl-Pass-123", "standby_hosts": []string{"10.0.0.12/32"}}, Resp: map[string]any{"role": "primary", "has_replication_password": true}})},
		"/databases/postgres/pitr":            map[string]any{"put": op("Databases", "Configure PostgreSQL WAL archiving (PITR)", oaSpecOpts{Req: map[string]any{"enabled": true, "archive_dir": "/var/lib/postgresql/wal_archive", "retention_days": 14}, Resp: map[string]any{"enabled": true}})},
		"/databases/postgres/pitr/basebackup": map[string]any{"post": op("Databases", "Take PostgreSQL PITR base backup", oaSpecOpts{Status: "202", Resp: map[string]any{"job_id": "job_pgb_1"}})},
		"/databases/postgres/pitr/restore":    map[string]any{"post": op("Databases", "Restore PostgreSQL to point-in-time", oaSpecOpts{Status: "202", Req: map[string]any{"target_time": "2026-11-27T08:00:00Z"}, Resp: map[string]any{"job_id": "job_pgr_1"}})},

		// Docker
		"/docker/containers": map[string]any{
			"get":  op("Docker", "List containers", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "c_1", "name": "web-app", "image": "nginx:alpine", "status": "running"}}}}),
			"post": op("Docker", "Create container from image, GitHub, or local path", oaSpecOpts{Status: "201", Req: map[string]any{"name": "my-local-app", "local_path": "/myapp", "dockerfile": "Dockerfile", "ports": []string{"8095:3000"}, "memory_mb": 512}, Resp: map[string]any{"container": map[string]any{"id": "c_1", "name": "my-local-app"}, "job_id": "job_dk_1"}}),
		},
		"/docker/containers/{id}/start":   map[string]any{"post": op("Docker", "Start container", oaSpecOpts{Resp: map[string]any{"job_id": "job_1"}})},
		"/docker/containers/{id}/stop":    map[string]any{"post": op("Docker", "Stop container", oaSpecOpts{Resp: map[string]any{"job_id": "job_2"}})},
		"/docker/containers/{id}/restart": map[string]any{"post": op("Docker", "Restart container", oaSpecOpts{Resp: map[string]any{"job_id": "job_3"}})},
		"/docker/containers/{id}/rebuild": map[string]any{"post": op("Docker", "Rebuild container from GitHub or local path", oaSpecOpts{Status: "202", Resp: map[string]any{"job_id": "job_4"}})},
		"/docker/containers/{id}/logs":    map[string]any{"get": op("Docker", "Get container logs", oaSpecOpts{Resp: map[string]any{"logs": "INFO listening on :3000"}})},
		"/docker/containers/{id}":         map[string]any{"delete": op("Docker", "Delete container", oaSpecOpts{Status: "204"})},
		"/docker/stacks": map[string]any{
			"get":  op("Docker", "List Compose stacks", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "cs_1", "name": "redis-stack", "status": "running"}}}}),
			"post": op("Docker", "Deploy Compose stack", oaSpecOpts{Status: "201", Req: map[string]any{"name": "redis-stack", "compose_yaml": "services:\n  redis:\n    image: redis:7-alpine"}, Resp: map[string]any{"stack": map[string]any{"id": "cs_1"}, "job_id": "job_cs_1"}}),
		},
		"/docker/stacks/{id}": map[string]any{"delete": op("Docker", "Delete Compose stack", oaSpecOpts{Status: "204"})},

		// Catalog
		"/catalog":          map[string]any{"get": op("Catalog", "List 1-click app catalog (built-in + plugin apps)", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"slug": "wordpress", "name": "WordPress", "type": "php", "version": "6.7"}}}})},
		"/catalog/installs": map[string]any{"get": op("Catalog", "List installed catalog apps", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "ai_1", "app_slug": "wordpress", "domain_id": "d_1", "status": "installed"}}}})},
		"/catalog/install":  map[string]any{"post": op("Catalog", "Install catalog app", oaSpecOpts{Status: "201", Req: map[string]any{"app_slug": "wordpress", "domain_id": "d_1"}, Resp: map[string]any{"install": map[string]any{"id": "ai_1", "app_slug": "wordpress"}, "job_id": "job_cat_1"}})},
		"/catalog/installs/{id}": map[string]any{
			"patch":  op("Catalog", "Update domain or redeploy installed app", oaSpecOpts{Req: map[string]any{"domain_id": "d_2", "reinstall": true}, Resp: map[string]any{"install": map[string]any{"id": "ai_1", "domain_id": "d_2"}, "job_id": "job_cat_2"}}),
			"delete": op("Catalog", "Uninstall catalog app", oaSpecOpts{Status: "204"}),
		},

		// Services, FTP, Firewall
		"/services/redis":        map[string]any{"get": op("Services", "List Redis instances"), "post": op("Services", "Create Redis instance", oaSpecOpts{Status: "201", Req: map[string]any{"name": "cache1"}, Resp: map[string]any{"connection_string": "redis://:secret@127.0.0.1:6380"}})},
		"/services/redis/{id}":   map[string]any{"delete": op("Services", "Delete Redis instance", oaSpecOpts{Status: "204"})},
		"/services/rabbitmq":     map[string]any{"get": op("Services", "List RabbitMQ vhosts"), "post": op("Services", "Create RabbitMQ vhost", oaSpecOpts{Status: "201", Req: map[string]any{"vhost": "/app", "username": "app_mq", "password": "Strong-MQ-Password-123"}})},
		"/services/rabbitmq/{id}": map[string]any{"delete": op("Services", "Delete RabbitMQ vhost", oaSpecOpts{Status: "204"})},
		"/ftp":                   map[string]any{"get": op("FTP", "List FTP accounts"), "post": op("FTP", "Create FTP account", oaSpecOpts{Status: "201", Req: map[string]any{"username": "ftp_user", "password": "Strong-FTP-Password-123", "protocol": "sftp"}})},
		"/ftp/{id}":              map[string]any{"delete": op("FTP", "Delete FTP account", oaSpecOpts{Status: "204"})},
		"/firewall/rules":        map[string]any{"get": op("Firewall", "List firewall rules"), "post": op("Firewall", "Create firewall rule", oaSpecOpts{Status: "201", Req: map[string]any{"port": "443", "protocol": "tcp", "action": "allow"}})},
		"/firewall/rules/{id}":   map[string]any{"delete": op("Firewall", "Delete firewall rule", oaSpecOpts{Status: "204"})},
		"/firewall/bans":         map[string]any{"get": op("Firewall", "List Fail2Ban bans")},
		"/firewall/unban":        map[string]any{"post": op("Firewall", "Unban IP address", oaSpecOpts{Req: map[string]any{"ip": "198.51.100.44", "jail": "sshd"}})},

		// Mail & SMTP Relay
		"/mail/domains":                map[string]any{"get": op("Mail", "List mail domains"), "post": op("Mail", "Create mail domain", oaSpecOpts{Status: "201", Req: map[string]any{"domain": "example.com"}})},
		"/mail/domains/{id}":           map[string]any{"delete": op("Mail", "Delete mail domain", oaSpecOpts{Status: "204"})},
		"/mail/domains/{id}/dns":       map[string]any{"get": op("Mail", "Get DKIM/SPF/DMARC DNS records")},
		"/mail/domains/{id}/mailboxes": map[string]any{"get": op("Mail", "List mailboxes"), "post": op("Mail", "Create mailbox", oaSpecOpts{Status: "201", Req: map[string]any{"local_part": "info", "password": "Strong-Mail-Password-123", "quota_mb": 2048}})},
		"/mail/mailboxes/{mailboxId}":  map[string]any{"delete": op("Mail", "Delete mailbox", oaSpecOpts{Status: "204"})},
		"/mail/relay": map[string]any{
			"get": op("Mail", "Get server-wide SMTP relay config", oaSpecOpts{Resp: map[string]any{"enabled": true, "provider": "ses", "host": "email-smtp.us-east-1.amazonaws.com", "port": 587, "has_password": true}}),
			"put": op("Mail", "Set server-wide SMTP relay (smarthost)", oaSpecOpts{Req: map[string]any{"enabled": true, "provider": "ses", "host": "email-smtp.us-east-1.amazonaws.com", "port": 587, "username": "AKIA...", "password": "secret", "tls_mode": "starttls"}, Resp: map[string]any{"enabled": true, "has_password": true}}),
		},
		"/mail/domains/{id}/relay": map[string]any{"put": op("Mail", "Set per-domain SMTP relay (smarthost)", oaSpecOpts{Req: map[string]any{"enabled": true, "provider": "mailgun", "host": "smtp.mailgun.org", "port": 587, "username": "postmaster@mg.example.com", "password": "secret"}, Resp: map[string]any{"ok": true}})},
		"/mail/relay/test":         map[string]any{"post": op("Mail", "Test SMTP relay TCP connectivity", oaSpecOpts{Req: map[string]any{"host": "email-smtp.us-east-1.amazonaws.com", "port": 587}, Resp: map[string]any{"ok": true, "address": "email-smtp.us-east-1.amazonaws.com:587"}})},

		// Backups, Webhooks, DNS, Cron, Files
		"/backups":                map[string]any{"get": op("Backups", "List backups"), "post": op("Backups", "Create backup", oaSpecOpts{Status: "201", Req: map[string]any{"kind": "user_data", "target": "mad"}})},
		"/backups/{id}/restore":   map[string]any{"post": op("Backups", "Restore backup", oaSpecOpts{Status: "202"})},
		"/backups/{id}/download":  map[string]any{"get": op("Backups", "Download backup manifest")},
		"/backups/{id}":           map[string]any{"delete": op("Backups", "Delete backup", oaSpecOpts{Status: "204"})},
		"/webhooks":               map[string]any{"get": op("Webhooks", "List webhooks"), "post": op("Webhooks", "Create webhook", oaSpecOpts{Status: "201", Req: map[string]any{"url": "https://hooks.example.com/stako", "events": []string{"domain.created", "backup.created"}}})},
		"/webhooks/{id}":          map[string]any{"delete": op("Webhooks", "Delete webhook", oaSpecOpts{Status: "204"})},
		"/webhooks/deliveries":    map[string]any{"get": op("Webhooks", "List recent webhook deliveries")},
		"/webhooks/{id}/test":     map[string]any{"post": op("Webhooks", "Send test webhook delivery")},
		"/dns/zones":              map[string]any{"get": op("DNS", "List DNS zones"), "post": op("DNS", "Create DNS zone", oaSpecOpts{Status: "201", Req: map[string]any{"domain": "example.com", "provider": "powerdns"}})},
		"/dns/zones/{id}":         map[string]any{"delete": op("DNS", "Delete DNS zone", oaSpecOpts{Status: "204"})},
		"/dns/zones/{id}/records": map[string]any{"get": op("DNS", "List DNS records"), "post": op("DNS", "Create DNS record", oaSpecOpts{Status: "201", Req: map[string]any{"type": "A", "name": "@", "content": "203.0.113.10", "ttl": 300}})},
		"/dns/records/{recordId}": map[string]any{"delete": op("DNS", "Delete DNS record", oaSpecOpts{Status: "204"})},
		"/cron":                   map[string]any{"get": op("Cron", "List cron jobs"), "post": op("Cron", "Create cron job", oaSpecOpts{Status: "201", Req: map[string]any{"schedule": "0 2 * * *", "command": "/usr/bin/php /home/mad/cron.php"}})},
		"/cron/{id}":              map[string]any{"delete": op("Cron", "Delete cron job", oaSpecOpts{Status: "204"})},
		"/files":                  map[string]any{"get": op("Files", "List directory", oaSpecOpts{QueryParams: []string{"path"}})},
		"/files/read":             map[string]any{"get": op("Files", "Read file", oaSpecOpts{QueryParams: []string{"path"}})},
		"/files/write":            map[string]any{"post": op("Files", "Write file", oaSpecOpts{Req: map[string]any{"path": "/public_html/index.html", "content": "<h1>Hello</h1>"}})},
		"/files/delete":           map[string]any{"post": op("Files", "Delete file or directory", oaSpecOpts{Req: map[string]any{"path": "/public_html/old.txt"}})},

		// Monitoring, Uptime, Hardware, Quotas & Logs
		"/monitoring/metrics":     map[string]any{"get": op("Monitoring", "Live CPU, memory, disk & service metrics", oaSpecOpts{Resp: map[string]any{"cpu_percent": 12.4, "memory_percent": 41.2, "disk_percent": 28.5}})},
		"/monitoring/alerts":      map[string]any{"get": op("Monitoring", "List alert rules"), "post": op("Monitoring", "Create alert rule", oaSpecOpts{Status: "201", Req: map[string]any{"name": "High CPU", "channel": "email", "target": "ops@example.com", "threshold": 85}})},
		"/monitoring/alerts/{id}": map[string]any{"delete": op("Monitoring", "Delete alert rule", oaSpecOpts{Status: "204"})},
		"/monitoring/uptime": map[string]any{
			"get":  op("Monitoring", "List per-domain uptime monitors", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "up_1", "domain": "example.com", "status": "up", "last_http_status": 200, "ssl_days_left": 75}}}}),
			"post": op("Monitoring", "Create uptime monitor", oaSpecOpts{Status: "201", Req: map[string]any{"domain": "example.com", "url": "https://example.com/", "interval_sec": 300, "expected_status": 200, "public_status": true}, Resp: map[string]any{"id": "up_1", "domain": "example.com", "status": "unknown"}}),
		},
		"/monitoring/uptime/{id}/check": map[string]any{"post": op("Monitoring", "Run HTTP/SSL/domain expiry check", oaSpecOpts{Resp: map[string]any{"id": "up_1", "status": "up", "last_http_status": 200, "last_latency_ms": 48, "ssl_days_left": 75, "domain_days_left": 210}})},
		"/monitoring/uptime/{id}":       map[string]any{"delete": op("Monitoring", "Delete uptime monitor", oaSpecOpts{Status: "204"})},
		"/monitoring/hardware":          map[string]any{"get": op("Monitoring", "SMART disk health, inode usage & memory/swap PSI", oaSpecOpts{Resp: map[string]any{"disks": []map[string]any{{"device": "/dev/sda", "health_status": "PASSED", "temperature_c": 34}}, "inodes": []map[string]any{{"mount": "/", "inodes_pct": 4.1}}, "pressure": map[string]any{"mem_pct": 40.0, "swap_pct": 6.2, "psi_mem_avg10": 0.12}}})},
		"/monitoring/quotas":            map[string]any{"get": op("Monitoring", "List per-user disk & inode project quotas", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"user_id": "u_1", "username": "mad", "project_id": 10421, "disk_limit_mb": 10240, "disk_used_mb": 240}}}})},
		"/monitoring/quotas/{userId}":   map[string]any{"put": op("Monitoring", "Set per-user disk & inode project quota", oaSpecOpts{Req: map[string]any{"disk_limit_mb": 10240, "inode_limit": 250000}, Resp: map[string]any{"user_id": "u_1", "disk_limit_mb": 10240, "inode_limit": 250000}})},
		"/logs":                         map[string]any{"get": op("Logs", "Query centralized cross-service logs", oaSpecOpts{QueryParams: []string{"service", "level", "source", "q", "lines"}, Resp: map[string]any{"entries": []map[string]any{{"timestamp": "2026-11-27T08:00:00Z", "service": "web", "source": "nginx/access.log", "level": "info", "message": "GET / 200"}}, "total": 1}})},
		"/logs/rotation": map[string]any{
			"get": op("Logs", "Get log rotation policy", oaSpecOpts{Resp: map[string]any{"frequency": "daily", "rotate": 14, "maxsize": "100M", "compress": true}}),
			"put": op("Logs", "Update log rotation policy", oaSpecOpts{Req: map[string]any{"frequency": "weekly", "rotate": 30, "maxsize": "250M", "compress": true}, Resp: map[string]any{"frequency": "weekly", "rotate": 30}}),
		},
		"/logs/rotate": map[string]any{"post": op("Logs", "Trigger immediate log rotation", oaSpecOpts{Resp: map[string]any{"ok": true, "output": "Log rotation completed"}})},
		"/logs/shipping": map[string]any{
			"get": op("Logs", "Get Loki & syslog shipping config", oaSpecOpts{Resp: map[string]any{"syslog_enabled": true, "syslog_host": "logs.example.com", "loki_enabled": true, "has_loki_password": true}}),
			"put": op("Logs", "Update Loki & syslog shipping config", oaSpecOpts{Req: map[string]any{"syslog_enabled": true, "syslog_host": "logs.example.com", "syslog_port": 6514, "syslog_protocol": "tls", "loki_enabled": true, "loki_url": "https://loki.example.com/loki/api/v1/push"}, Resp: map[string]any{"syslog_enabled": true, "loki_enabled": true}}),
		},
		"/logs/shipping/test": map[string]any{"post": op("Logs", "Test Loki or syslog connectivity", oaSpecOpts{Req: map[string]any{"target": "loki", "loki_url": "https://loki.example.com/loki/api/v1/push"}, Resp: map[string]any{"ok": true, "target": "loki"}})},

		// Git Deploy & Preview Environments
		"/git-deploys":                map[string]any{"get": op("GitDeploy", "List Git deploy targets"), "post": op("GitDeploy", "Create Git deploy target", oaSpecOpts{Status: "201", Req: map[string]any{"domain_id": "d_1", "repo_url": "https://github.com/org/app.git", "branch": "main", "build_cmd": "npm run build"}})},
		"/git-deploys/{id}/trigger":   map[string]any{"post": op("GitDeploy", "Trigger Git deploy", oaSpecOpts{Status: "202", Resp: map[string]any{"job_id": "job_gd_1"}})},
		"/git-deploys/{id}":           map[string]any{"delete": op("GitDeploy", "Delete Git deploy target", oaSpecOpts{Status: "204"})},
		"/git-previews":               map[string]any{"get": op("GitDeploy", "List branch/PR preview environments", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "prev_1", "subdomain": "pr-42.example.com", "branch": "feat/ui", "pr_number": 42, "status": "running"}}}})},
		"/git-deploys/{id}/previews":  map[string]any{"post": op("GitDeploy", "Create branch/PR preview environment", oaSpecOpts{Status: "201", Req: map[string]any{"branch": "feat/new-ui", "pr_number": 42, "container_port": 3000}, Resp: map[string]any{"preview": map[string]any{"id": "prev_1", "subdomain": "pr-42.example.com", "host_port": 18001}, "job_id": "job_prev_1"}})},
		"/git-previews/{id}/redeploy": map[string]any{"post": op("GitDeploy", "Redeploy preview environment", oaSpecOpts{Status: "202", Resp: map[string]any{"job_id": "job_prev_2"}})},
		"/git-previews/{id}":          map[string]any{"delete": op("GitDeploy", "Destroy preview environment", oaSpecOpts{Status: "204"})},

		// WordPress Toolkit
		"/wordpress":                   map[string]any{"get": op("WordPress", "List WordPress installations", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "wp_1", "domain": "blog.example.com", "wp_version": "6.7.1", "security_score": 100}}}})},
		"/wordpress/scan":              map[string]any{"post": op("WordPress", "Discover & scan WordPress sites", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "wp_1", "domain": "blog.example.com", "updates_plugins": 2, "security_score": 75}}}})},
		"/wordpress/bulk-update":       map[string]any{"post": op("WordPress", "Bulk update WordPress core/plugins/themes", oaSpecOpts{Status: "202", Req: map[string]any{"site_ids": []string{"wp_1", "wp_2"}}, Resp: map[string]any{"job_ids": []string{"job_wp_1"}, "updated_count": 2}})},
		"/wordpress/{id}/staging":      map[string]any{"post": op("WordPress", "Create WordPress staging clone", oaSpecOpts{Status: "201", Req: map[string]any{"staging_domain": "staging.blog.example.com"}, Resp: map[string]any{"site": map[string]any{"id": "wp_stg_1", "is_staging": true}, "job_id": "job_stg_1"}})},
		"/wordpress/{id}/push-staging": map[string]any{"post": op("WordPress", "Push WordPress staging to live", oaSpecOpts{Status: "202", Resp: map[string]any{"job_id": "job_push_1"}})},
		"/wordpress/{id}/harden":       map[string]any{"post": op("WordPress", "Apply WordPress security hardening", oaSpecOpts{Resp: map[string]any{"ok": true, "security_score": 100}})},
		"/wordpress/{id}/autologin":    map[string]any{"post": op("WordPress", "Generate one-time WP Admin auto-login URL", oaSpecOpts{Resp: map[string]any{"login_url": "https://blog.example.com/_stako_login_abc.php?token=...", "expires_in": 60}})},
		"/wordpress/{id}":              map[string]any{"delete": op("WordPress", "Remove WordPress site from toolkit", oaSpecOpts{Status: "204"})},

		// Nodes, Migrations & Plugins
		"/nodes":               map[string]any{"get": op("Nodes", "List cluster nodes"), "post": op("Nodes", "Register node", oaSpecOpts{Status: "201", Req: map[string]any{"name": "worker-02", "host": "10.0.0.20", "port": 9443, "role": "worker"}})},
		"/nodes/{id}":          map[string]any{"delete": op("Nodes", "Delete node", oaSpecOpts{Status: "204"})},
		"/import":              map[string]any{"post": op("Migrations", "Import panel archive", oaSpecOpts{Status: "202", Req: map[string]any{"source_panel": "hestiacp", "archive_path": "/var/backups/user.tar.gz"}})},
		"/migrations/bundles":  map[string]any{"get": op("Migrations", "List migration bundles", oaSpecOpts{Resp: map[string]any{"bundles": []map[string]any{{"name": "stako-mad-20261127.tar.gz", "size_bytes": 1048576}}}})},
		"/migrations/inspect":  map[string]any{"post": op("Migrations", "Inspect HestiaCP/cPanel/VestaCP/Stako archive", oaSpecOpts{Req: map[string]any{"source_panel": "auto", "archive_path": "/var/backups/hestia-user.tar.gz"}, Resp: map[string]any{"source_panel": "hestiacp", "username": "mad", "domains": []string{"example.com"}, "databases": []string{"mad_wp"}}})},
		"/migrations/export":   map[string]any{"post": op("Migrations", "Export Stako user account bundle", oaSpecOpts{Status: "202", Req: map[string]any{"user_id": "u_1"}, Resp: map[string]any{"job_id": "job_exp_1", "username": "mad"}})},
		"/migrations/import":   map[string]any{"post": op("Migrations", "Import & provision account from archive", oaSpecOpts{Status: "202", Req: map[string]any{"source_panel": "cpanel", "archive_path": "/var/backups/cpmove-bob.tar.gz", "target_username": "bob", "domains": []string{"bob.com"}, "include_files": true, "include_databases": true, "include_mail": true}, Resp: map[string]any{"job_id": "job_imp_1", "provisioned": map[string]int{"domains": 1}}})},
		"/migrations/transfer": map[string]any{"post": op("Migrations", "Transfer archive via SSH/SCP", oaSpecOpts{Status: "202", Req: map[string]any{"direction": "pull", "remote_host": "203.0.113.50", "remote_port": 22, "remote_user": "root", "remote_path": "/var/backups/user.tar.gz"}, Resp: map[string]any{"job_id": "job_scp_1"}})},
		"/plugins": map[string]any{
			"get":  op("Plugins", "List installed community plugins", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"id": "community-plausible", "name": "Plausible Analytics", "version": "1.0.0", "enabled": true}}}}),
			"post": op("Plugins", "Install plugin manifest (hooks, dashboard pages, catalog apps)", oaSpecOpts{Status: "201", Req: map[string]any{"id": "community-plausible", "name": "Plausible Analytics", "version": "1.0.0", "author": "Stako Community", "hooks": []map[string]any{{"event": "domain.created", "url": "https://hooks.example.com/stako"}}, "pages": []map[string]any{{"slug": "plausible-stats", "title": "Plausible Stats", "url": "https://plausible.example.com/share"}}, "apps": []map[string]any{{"slug": "plausible-ce", "name": "Plausible CE", "type": "docker", "image": "ghcr.io/plausible/community-edition:v2.1", "default_port": 8000}}}, Resp: map[string]any{"id": "community-plausible", "enabled": true}}),
		},
		"/plugins/pages":       map[string]any{"get": op("Plugins", "List enabled plugin dashboard pages", oaSpecOpts{Resp: map[string]any{"items": []map[string]any{{"plugin_id": "community-plausible", "slug": "plausible-stats", "title": "Plausible Stats", "url": "https://plausible.example.com/share"}}}})},
		"/plugins/{id}/toggle": map[string]any{"post": op("Plugins", "Enable or disable plugin", oaSpecOpts{Req: map[string]any{"enabled": false}, Resp: map[string]any{"id": "community-plausible", "enabled": false}})},
		"/plugins/{id}":        map[string]any{"delete": op("Plugins", "Uninstall plugin", oaSpecOpts{Status: "204"})},

		// Security, System, Terminal, Billing, Audit & Jobs
		"/security/scan":                map[string]any{"post": op("Security", "Run host security scan", oaSpecOpts{Status: "202"})},
		"/security/findings":            map[string]any{"get": op("Security", "List security findings")},
		"/security/findings/{id}":       map[string]any{"patch": op("Security", "Update finding status", oaSpecOpts{Req: map[string]any{"status": "resolved"}})},
		"/security/lockdown":            map[string]any{"get": op("Security", "Get lockdown status"), "post": op("Security", "Toggle emergency lockdown", oaSpecOpts{Req: map[string]any{"enabled": true, "allowed_ips": []string{"203.0.113.10"}}})},
		"/security/rotate-keys":         map[string]any{"post": op("Security", "Rotate encryption keys")},
		"/security/forensics":           map[string]any{"get": op("Security", "Export forensics bundle")},
		"/waf":                          map[string]any{"get": op("Security", "Get WAF configuration"), "post": op("Security", "Update & apply WAF configuration", oaSpecOpts{Req: map[string]any{"enabled": true, "engine": "modsecurity", "mode": "on", "paranoia_level": 1}})},
		"/updates":                      map[string]any{"get": op("System", "List pending OS updates")},
		"/updates/apply":                map[string]any{"post": op("System", "Apply OS updates", oaSpecOpts{Status: "202"})},
		"/capabilities":                 map[string]any{"get": op("System", "Installed service capabilities")},
		"/system/configs":               map[string]any{"get": op("System", "List editable service configs")},
		"/system/configs/{service}":     map[string]any{"get": op("System", "Read service config"), "put": op("System", "Update & validate service config", oaSpecOpts{Req: map[string]any{"content": "worker_processes auto;"}})},
		"/system/update":                map[string]any{"get": op("System", "Check for Stako updates"), "post": op("System", "Apply self-update", oaSpecOpts{Status: "202"})},
		"/system/services":              map[string]any{"get": op("System", "List detected system services")},
		"/system/php-versions":          map[string]any{"get": op("System", "List installed PHP-FPM versions")},
		"/system/services/{id}/action":  map[string]any{"post": op("System", "Start/stop/restart system service", oaSpecOpts{Req: map[string]any{"action": "restart"}})},
		"/system/services/{id}/install": map[string]any{"post": op("System", "Install system service", oaSpecOpts{Status: "202"})},
		"/system/services/{id}":         map[string]any{"delete": op("System", "Uninstall system service", oaSpecOpts{Status: "202"})},
		"/system/info":                  map[string]any{"get": op("System", "System status and metrics")},
		"/terminal/exec":                map[string]any{"post": op("Terminal", "Execute audited shell command", oaSpecOpts{Req: map[string]any{"command": "uptime"}, Resp: map[string]any{"output": "up 14 days"}})},
		"/billing/sso":                  map[string]any{"post": op("Billing", "Issue WHMCS single sign-on token", oaSpecOpts{Req: map[string]any{"username": "mad"}, Resp: map[string]any{"sso_url": "https://panel.example.com/#/?sso=..."}})},
		"/billing/usage":                map[string]any{"get": op("Billing", "Per-user resource usage metering")},
		"/audit":                        map[string]any{"get": op("Audit", "List audit log entries", oaSpecOpts{QueryParams: []string{"limit", "offset", "action", "actor"}})},
		"/audit/verify":                 map[string]any{"get": op("Audit", "Verify audit log hash chain", oaSpecOpts{Resp: map[string]any{"valid": true, "checked": 120}})},
		"/jobs":                         map[string]any{"get": op("Jobs", "List background jobs")},
		"/jobs/{id}":                    map[string]any{"get": op("Jobs", "Get job status and log")},
	}

	for p, raw := range paths {
		if m, ok := raw.(map[string]any); ok {
			attachPathParams(p, m)
		}
	}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Stako Control Panel API",
			"version":     version.Version,
			"description": "REST API for Stako Control Panel. Authenticate via Bearer API Key (`Authorization: Bearer stk_...`) or Session Cookie + `X-CSRF-Token`.",
			"license":     map[string]any{"name": "MIT"},
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
			upperM := strings.ToUpper(m)
			var details strings.Builder
			var reqJSON string
			if rb, ok := mMap["requestBody"].(map[string]any); ok {
				if ct, ok := rb["content"].(map[string]any); ok {
					if aj, ok := ct["application/json"].(map[string]any); ok && aj["example"] != nil {
						if b, err := json.MarshalIndent(aj["example"], "", "  "); err == nil {
							reqJSON = string(b)
						}
					}
				}
			}
			curlCmd := fmt.Sprintf("curl -sS -X %s \\\n  \"https://YOUR_PANEL_HOST:8083/api/v1%s\" \\\n  -H \"Authorization: Bearer $STAKO_TOKEN\"", upperM, p)
			if reqJSON != "" {
				curlCmd += " \\\n  -H \"Content-Type: application/json\" \\\n  -d '" + reqJSON + "'"
			}
			details.WriteString("<div><strong>cURL:</strong><pre>" + html.EscapeString(curlCmd) + "</pre></div>")
			if reqJSON != "" {
				details.WriteString("<div><strong>Request Body:</strong><pre>" + html.EscapeString(reqJSON) + "</pre></div>")
			}
			if rs, ok := mMap["responses"].(map[string]any); ok {
				for code, rv := range rs {
					if code == "422" {
						continue
					}
					if rm, ok := rv.(map[string]any); ok {
						if ct, ok := rm["content"].(map[string]any); ok {
							if aj, ok := ct["application/json"].(map[string]any); ok && aj["example"] != nil {
								if b, err := json.MarshalIndent(aj["example"], "", "  "); err == nil {
									details.WriteString("<div><strong>Response (" + html.EscapeString(code) + "):</strong><pre>" + html.EscapeString(string(b)) + "</pre></div>")
								}
							}
						}
					}
				}
			}
			exCell := "<details><summary>cURL / Request / Response</summary>" + details.String() + "</details>"
			rows.WriteString(fmt.Sprintf(
				"<tr><td><code>%s</code></td><td><code>/api/v1%s</code></td><td>%s</td><td>%s%s</td></tr>\n",
				html.EscapeString(upperM),
				html.EscapeString(p),
				html.EscapeString(tag),
				html.EscapeString(sum),
				exCell,
			))
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Stako API Reference</title>
<style>body{font-family:system-ui,sans-serif;max-width:1100px;margin:2rem auto;padding:0 1rem;color:#0f172a;line-height:1.5}table{width:100%%;border-collapse:collapse}th,td{border-bottom:1px solid #e2e8f0;padding:.55rem;text-align:left;font-size:.9rem;vertical-align:top}code{background:#f1f5f9;padding:.15rem .35rem;border-radius:4px}pre{background:#f8fafc;border:1px solid #e2e8f0;padding:.6rem;border-radius:6px;overflow:auto;font-size:.8rem}details{margin-top:.35rem}.guide{background:#f8fafc;border:1px solid #e2e8f0;border-radius:8px;padding:1rem 1.25rem;margin:1rem 0 1.5rem}.guide h2{margin:0 0 .5rem;font-size:1.05rem}.guide h3{margin:.85rem 0 .35rem;font-size:.92rem}</style>
</head>
<body>
<h1>Stako Control Panel API (%s)</h1>
<p>OpenAPI 3.1: <a href="/api/v1/openapi.json">openapi.json</a> | <a href="/api/v1/openapi.yaml">openapi.yaml</a></p>
<section class="guide">
  <h2>Cara Request API (Quick Start)</h2>
  <p>Base URL: <code>https://YOUR_PANEL_HOST:8083/api/v1</code></p>
  <h3>1. Autentikasi dengan API Token (Direkomendasikan)</h3>
  <p>Buat token di dashboard <strong>Account &rarr; API Tokens</strong>, lalu kirim melalui header <code>Authorization: Bearer &lt;TOKEN&gt;</code>:</p>
  <pre>export STAKO_TOKEN="stk_xxxxxxxxxxxxxxxx"

# Contoh GET (List Domains)
curl -sS "https://YOUR_PANEL_HOST:8083/api/v1/domains" \
  -H "Authorization: Bearer $STAKO_TOKEN"

# Contoh POST (Create Domain)
curl -sS -X POST "https://YOUR_PANEL_HOST:8083/api/v1/domains" \
  -H "Authorization: Bearer $STAKO_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","php_version":"8.4","web_server":"nginx"}'</pre>
  <h3>2. Autentikasi Session Cookie + CSRF</h3>
  <p>Untuk login berbasis cookie, simpan cookie dari <code>POST /api/v1/auth/login</code> dan sertakan header <code>X-CSRF-Token</code> pada setiap request <code>POST</code>/<code>PUT</code>/<code>PATCH</code>/<code>DELETE</code>.</p>
  <h3>3. Format Error</h3>
  <p>Semua error mengembalikan JSON standar <strong>RFC 7807</strong> (<code>application/problem+json</code>) dengan field <code>type</code>, <code>title</code>, <code>status</code>, dan <code>detail</code>.</p>
</section>
<table><thead><tr><th>Method</th><th>Endpoint</th><th>Tag</th><th>Summary &amp; Examples</th></tr></thead><tbody>%s</tbody></table>
</body></html>`, html.EscapeString(version.Version), rows.String())
}
