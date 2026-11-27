// SPDX-License-Identifier: MIT
package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/StakoCP/stako/internal/agentclient"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/jobs"
	"github.com/StakoCP/stako/internal/store"
	"github.com/StakoCP/stako/internal/version"
)

type Server struct {
	store        *store.Store
	agent        *agentclient.Client
	jobs         *jobs.Queue
	enc          *crypto.EnvelopeEncryptor
	logger       *slog.Logger
	limiter      *RateLimiter
	webFS        fs.FS
	insecureHTTP bool
	idleTimeout  time.Duration
	absTimeout   time.Duration
	whMu         sync.Mutex
	whDeliveries []webhookDelivery
}

type Config struct {
	Store        *store.Store
	Agent        *agentclient.Client
	Jobs         *jobs.Queue
	Encryptor    *crypto.EnvelopeEncryptor
	Logger       *slog.Logger
	WebFS        fs.FS
	InsecureHTTP bool
}

func NewServer(cfg Config) *Server {
	return &Server{
		store:        cfg.Store,
		agent:        cfg.Agent,
		jobs:         cfg.Jobs,
		enc:          cfg.Encryptor,
		logger:       cfg.Logger,
		limiter:      NewRateLimiter(5, 5*time.Minute),
		webFS:        cfg.WebFS,
		insecureHTTP: cfg.InsecureHTTP,
		idleTimeout:  30 * time.Minute,
		absTimeout:   12 * time.Hour,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/openapi.json", s.handleOpenAPIJSON)
	mux.HandleFunc("GET /api/v1/openapi.yaml", s.handleOpenAPIYAML)
	mux.HandleFunc("GET /api/v1/docs", s.handleOpenAPIDocs)

	// Auth
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("POST /api/v1/auth/reauth", s.requireAuth(s.handleReauth))
	mux.HandleFunc("POST /api/v1/auth/password", s.requireAuth(s.handleChangePassword))
	mux.HandleFunc("POST /api/v1/auth/2fa/setup", s.requireAuth(s.handle2FASetup))
	mux.HandleFunc("POST /api/v1/auth/2fa/enable", s.requireAuth(s.handle2FAEnable))
	mux.HandleFunc("POST /api/v1/auth/2fa/disable", s.requireAuth(s.handle2FADisable))
	mux.HandleFunc("GET /api/v1/auth/sessions", s.requireAuth(s.handleListSessions))
	mux.HandleFunc("DELETE /api/v1/auth/sessions/{id}", s.requireAuth(s.handleDeleteSession))
	mux.HandleFunc("POST /api/v1/auth/sessions/revoke-others", s.requireAuth(s.handleRevokeOtherSessions))

	// Users
	mux.HandleFunc("GET /api/v1/users", s.requireAuth(s.handleListUsers))
	mux.HandleFunc("POST /api/v1/users", s.requireAuth(s.handleCreateUser))
	mux.HandleFunc("GET /api/v1/users/{id}", s.requireAuth(s.handleGetUser))
	mux.HandleFunc("PATCH /api/v1/users/{id}", s.requireAuth(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.requireAuth(s.handleDeleteUser))
	mux.HandleFunc("POST /api/v1/users/{id}/suspend", s.requireAuth(s.handleSuspendUser))
	mux.HandleFunc("POST /api/v1/users/{id}/unsuspend", s.requireAuth(s.handleUnsuspendUser))

	// Packages
	mux.HandleFunc("GET /api/v1/packages", s.requireAuth(s.handleListPackages))
	mux.HandleFunc("POST /api/v1/packages", s.requireAuth(s.handleCreatePackage))
	mux.HandleFunc("GET /api/v1/packages/{id}", s.requireAuth(s.handleGetPackage))
	mux.HandleFunc("PATCH /api/v1/packages/{id}", s.requireAuth(s.handleUpdatePackage))
	mux.HandleFunc("DELETE /api/v1/packages/{id}", s.requireAuth(s.handleDeletePackage))

	// API Keys
	mux.HandleFunc("GET /api/v1/api-keys", s.requireAuth(s.handleListAPIKeys))
	mux.HandleFunc("POST /api/v1/api-keys", s.requireAuth(s.handleCreateAPIKey))
	mux.HandleFunc("DELETE /api/v1/api-keys/{id}", s.requireAuth(s.handleDeleteAPIKey))

	// Impersonation (7.16)
	mux.HandleFunc("POST /api/v1/auth/impersonate", s.requireAuth(s.handleImpersonate))
	mux.HandleFunc("POST /api/v1/auth/impersonate/exit", s.requireAuth(s.handleExitImpersonate))

	// IPs (7.14, 7.15)
	mux.HandleFunc("GET /api/v1/ips", s.requireAuth(s.handleListIPs))
	mux.HandleFunc("POST /api/v1/ips", s.requireAuth(s.handleCreateIP))
	mux.HandleFunc("POST /api/v1/ips/sync", s.requireAuth(s.handleSyncIPs))
	mux.HandleFunc("DELETE /api/v1/ips/{id}", s.requireAuth(s.handleDeleteIP))

	// Domains & Proxy Rules (7.2, 7.3)
	mux.HandleFunc("GET /api/v1/domains", s.requireAuth(s.handleListDomains))
	mux.HandleFunc("POST /api/v1/domains", s.requireAuth(s.handleCreateDomain))
	mux.HandleFunc("GET /api/v1/domains/{id}", s.requireAuth(s.handleGetDomain))
	mux.HandleFunc("PATCH /api/v1/domains/{id}", s.requireAuth(s.handleUpdateDomain))
	mux.HandleFunc("DELETE /api/v1/domains/{id}", s.requireAuth(s.handleDeleteDomain))
	mux.HandleFunc("POST /api/v1/domains/{id}/ssl", s.requireAuth(s.handleIssueSSL))
	mux.HandleFunc("GET /api/v1/domains/{id}/logs", s.requireAuth(s.handleDomainLogs))
	mux.HandleFunc("GET /api/v1/domains/{id}/custom-nginx", s.requireAuth(s.handleGetDomainCustomNginx))
	mux.HandleFunc("PUT /api/v1/domains/{id}/custom-nginx", s.requireAuth(s.handleSetDomainCustomNginx))
	mux.HandleFunc("GET /api/v1/domains/{id}/proxy-rules", s.requireAuth(s.handleListProxyRules))
	mux.HandleFunc("POST /api/v1/domains/{id}/proxy-rules", s.requireAuth(s.handleCreateProxyRule))
	mux.HandleFunc("DELETE /api/v1/domains/{id}/proxy-rules/{ruleId}", s.requireAuth(s.handleDeleteProxyRule))

	// Apps / Process Manager (7.3)
	mux.HandleFunc("GET /api/v1/apps", s.requireAuth(s.handleListApps))
	mux.HandleFunc("POST /api/v1/apps", s.requireAuth(s.handleCreateApp))
	mux.HandleFunc("GET /api/v1/apps/{id}", s.requireAuth(s.handleGetApp))
	mux.HandleFunc("GET /api/v1/apps/{id}/env", s.requireAuth(s.handleGetAppEnv))
	mux.HandleFunc("PATCH /api/v1/apps/{id}", s.requireAuth(s.handleUpdateApp))
	mux.HandleFunc("DELETE /api/v1/apps/{id}", s.requireAuth(s.handleDeleteApp))
	mux.HandleFunc("POST /api/v1/apps/{id}/start", s.requireAuth(s.handleAppAction("start")))
	mux.HandleFunc("POST /api/v1/apps/{id}/stop", s.requireAuth(s.handleAppAction("stop")))
	mux.HandleFunc("POST /api/v1/apps/{id}/restart", s.requireAuth(s.handleAppAction("restart")))
	mux.HandleFunc("GET /api/v1/apps/{id}/logs", s.requireAuth(s.handleGetAppLogs))

	// Databases (7.5)
	mux.HandleFunc("GET /api/v1/databases", s.requireAuth(s.handleListDatabases))
	mux.HandleFunc("POST /api/v1/databases", s.requireAuth(s.handleCreateDatabase))
	mux.HandleFunc("DELETE /api/v1/databases/{id}", s.requireAuth(s.handleDeleteDatabase))
	mux.HandleFunc("POST /api/v1/databases/{id}/backup", s.requireAuth(s.handleBackupDatabase))

	// FTP / SFTP (7.7)
	mux.HandleFunc("GET /api/v1/ftp", s.requireAuth(s.handleListFTP))
	mux.HandleFunc("POST /api/v1/ftp", s.requireAuth(s.handleCreateFTP))
	mux.HandleFunc("DELETE /api/v1/ftp/{id}", s.requireAuth(s.handleDeleteFTP))

	// Firewall & Bans (7.10)
	mux.HandleFunc("GET /api/v1/firewall/rules", s.requireAuth(s.handleListFirewallRules))
	mux.HandleFunc("POST /api/v1/firewall/rules", s.requireAuth(s.handleCreateFirewallRule))
	mux.HandleFunc("DELETE /api/v1/firewall/rules/{id}", s.requireAuth(s.handleDeleteFirewallRule))
	mux.HandleFunc("GET /api/v1/firewall/bans", s.requireAuth(s.handleListFirewallBans))
	mux.HandleFunc("POST /api/v1/firewall/bans/unban", s.requireAuth(s.handleUnbanIP))

	// Security & Updates (7.18, 9.3)
	mux.HandleFunc("GET /api/v1/security/findings", s.requireAuth(s.handleListSecurityFindings))
	mux.HandleFunc("POST /api/v1/security/scan", s.requireAuth(s.handleRunSecurityScan))
	mux.HandleFunc("PATCH /api/v1/security/findings/{id}", s.requireAuth(s.handleUpdateSecurityFinding))
	mux.HandleFunc("GET /api/v1/updates", s.requireAuth(s.handleListUpdates))
	mux.HandleFunc("POST /api/v1/updates/apply", s.requireAuth(s.handleApplyUpdates))

	// Docker Rootless (7.4)
	mux.HandleFunc("GET /api/v1/docker/containers", s.requireAuth(s.handleListContainers))
	mux.HandleFunc("POST /api/v1/docker/containers", s.requireAuth(s.handleCreateContainer))
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/start", s.requireAuth(s.handleContainerAction("start")))
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/stop", s.requireAuth(s.handleContainerAction("stop")))
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/restart", s.requireAuth(s.handleContainerAction("restart")))
	mux.HandleFunc("POST /api/v1/docker/containers/{id}/rebuild", s.requireAuth(s.handleRebuildContainer))
	mux.HandleFunc("DELETE /api/v1/docker/containers/{id}", s.requireAuth(s.handleDeleteContainer))
	mux.HandleFunc("GET /api/v1/docker/containers/{id}/logs", s.requireAuth(s.handleContainerLogs))
	mux.HandleFunc("GET /api/v1/docker/stacks", s.requireAuth(s.handleListComposeStacks))
	mux.HandleFunc("POST /api/v1/docker/stacks", s.requireAuth(s.handleCreateComposeStack))
	mux.HandleFunc("DELETE /api/v1/docker/stacks/{id}", s.requireAuth(s.handleDeleteComposeStack))

	// App Installer Catalog (7.11)
	mux.HandleFunc("GET /api/v1/catalog", s.requireAuth(s.handleListCatalog))
	mux.HandleFunc("GET /api/v1/catalog/installs", s.requireAuth(s.handleListAppInstalls))
	mux.HandleFunc("POST /api/v1/catalog/install", s.requireAuth(s.handleInstallCatalogApp))

	// Redis & RabbitMQ (7.9)
	mux.HandleFunc("GET /api/v1/services/redis", s.requireAuth(s.handleListRedis))
	mux.HandleFunc("POST /api/v1/services/redis", s.requireAuth(s.handleCreateRedis))
	mux.HandleFunc("DELETE /api/v1/services/redis/{id}", s.requireAuth(s.handleDeleteRedis))
	mux.HandleFunc("GET /api/v1/services/rabbitmq", s.requireAuth(s.handleListRabbit))
	mux.HandleFunc("POST /api/v1/services/rabbitmq", s.requireAuth(s.handleCreateRabbit))
	mux.HandleFunc("DELETE /api/v1/services/rabbitmq/{id}", s.requireAuth(s.handleDeleteRabbit))

	// Mail Server (7.6)
	mux.HandleFunc("GET /api/v1/mail/domains", s.requireAuth(s.handleListMailDomains))
	mux.HandleFunc("POST /api/v1/mail/domains", s.requireAuth(s.handleCreateMailDomain))
	mux.HandleFunc("DELETE /api/v1/mail/domains/{id}", s.requireAuth(s.handleDeleteMailDomain))
	mux.HandleFunc("GET /api/v1/mail/domains/{id}/mailboxes", s.requireAuth(s.handleListMailboxes))
	mux.HandleFunc("POST /api/v1/mail/domains/{id}/mailboxes", s.requireAuth(s.handleCreateMailbox))
	mux.HandleFunc("DELETE /api/v1/mail/mailboxes/{id}", s.requireAuth(s.handleDeleteMailbox))
	mux.HandleFunc("GET /api/v1/mail/relay", s.requireAuth(s.handleGetServerRelay))
	mux.HandleFunc("PUT /api/v1/mail/relay", s.requireAuth(s.handleSetServerRelay))
	mux.HandleFunc("PUT /api/v1/mail/domains/{id}/relay", s.requireAuth(s.handleSetDomainRelay))
	mux.HandleFunc("POST /api/v1/mail/relay/test", s.requireAuth(s.handleTestRelay))

	// Backups (7.12, 7.19)
	mux.HandleFunc("GET /api/v1/backups", s.requireAuth(s.handleListBackups))
	mux.HandleFunc("POST /api/v1/backups", s.requireAuth(s.handleCreateBackup))
	mux.HandleFunc("POST /api/v1/backups/{id}/restore", s.requireAuth(s.handleRestoreBackup))
	mux.HandleFunc("DELETE /api/v1/backups/{id}", s.requireAuth(s.handleDeleteBackup))

	// Webhooks (7.17)
	mux.HandleFunc("GET /api/v1/webhooks", s.requireAuth(s.handleListWebhooks))
	mux.HandleFunc("POST /api/v1/webhooks", s.requireAuth(s.handleCreateWebhook))
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", s.requireAuth(s.handleDeleteWebhook))
	mux.HandleFunc("GET /api/v1/webhooks/deliveries", s.requireAuth(s.handleListWebhookDeliveries))
	mux.HandleFunc("POST /api/v1/webhooks/{id}/test", s.requireAuth(s.handleTestWebhook))

	// Lockdown Mode (SEC-S23)
	mux.HandleFunc("GET /api/v1/security/lockdown", s.requireAuth(s.handleGetLockdown))
	mux.HandleFunc("POST /api/v1/security/lockdown", s.requireAuth(s.handleSetLockdown))

	// DNS (PowerDNS / Cloudflare) (7.13)
	mux.HandleFunc("GET /api/v1/dns/zones", s.requireAuth(s.handleListDNSZones))
	mux.HandleFunc("POST /api/v1/dns/zones", s.requireAuth(s.handleCreateDNSZone))
	mux.HandleFunc("DELETE /api/v1/dns/zones/{id}", s.requireAuth(s.handleDeleteDNSZone))
	mux.HandleFunc("GET /api/v1/dns/zones/{id}/records", s.requireAuth(s.handleListDNSRecords))
	mux.HandleFunc("POST /api/v1/dns/zones/{id}/records", s.requireAuth(s.handleCreateDNSRecord))
	mux.HandleFunc("DELETE /api/v1/dns/records/{recordId}", s.requireAuth(s.handleDeleteDNSRecord))

	// Cron Jobs (7.13)
	mux.HandleFunc("GET /api/v1/cron", s.requireAuth(s.handleListCron))
	mux.HandleFunc("POST /api/v1/cron", s.requireAuth(s.handleCreateCron))
	mux.HandleFunc("DELETE /api/v1/cron/{id}", s.requireAuth(s.handleDeleteCron))

	// File Manager (7.13, SEC-A12)
	mux.HandleFunc("GET /api/v1/files", s.requireAuth(s.handleListFiles))
	mux.HandleFunc("GET /api/v1/files/read", s.requireAuth(s.handleReadFile))
	mux.HandleFunc("GET /api/v1/files/download", s.requireAuth(s.handleDownloadFile))
	mux.HandleFunc("GET /api/v1/files/raw", s.requireAuth(s.handleRawFile))
	mux.HandleFunc("POST /api/v1/files/write", s.requireAuth(s.handleWriteFile))
	mux.HandleFunc("POST /api/v1/files/upload", s.requireAuth(s.handleUploadFile))
	mux.HandleFunc("POST /api/v1/files/delete", s.requireAuth(s.handleDeleteFile))
	mux.HandleFunc("POST /api/v1/files/mkdir", s.requireAuth(s.handleMkdirFile))
	mux.HandleFunc("POST /api/v1/files/copy", s.requireAuth(s.handleCopyFile))
	mux.HandleFunc("POST /api/v1/files/move", s.requireAuth(s.handleMoveFile))
	mux.HandleFunc("POST /api/v1/files/chmod", s.requireAuth(s.handleChmodFile))
	mux.HandleFunc("POST /api/v1/files/extract", s.requireAuth(s.handleExtractFile))
	mux.HandleFunc("POST /api/v1/files/compress", s.requireAuth(s.handleCompressFile))

	// Monitoring & Alerts (7.13)
	mux.HandleFunc("GET /api/v1/monitoring/metrics", s.requireAuth(s.handleMonitoringMetrics))
	mux.HandleFunc("GET /api/v1/monitoring/alerts", s.requireAuth(s.handleListAlerts))
	mux.HandleFunc("POST /api/v1/monitoring/alerts", s.requireAuth(s.handleCreateAlert))
	mux.HandleFunc("DELETE /api/v1/monitoring/alerts/{id}", s.requireAuth(s.handleDeleteAlert))

	// Web Terminal (7.8 FR-S5)
	mux.HandleFunc("POST /api/v1/terminal/exec", s.requireAuth(s.handleTerminalExec))

	// WAF (7.10 FR-X5)
	mux.HandleFunc("GET /api/v1/waf", s.requireAuth(s.handleGetWAF))
	mux.HandleFunc("POST /api/v1/waf", s.requireAuth(s.handleSetWAF))

	// Git Deploy (7.3 FR-P8)
	mux.HandleFunc("GET /api/v1/git-deploys", s.requireAuth(s.handleListGitDeploys))
	mux.HandleFunc("POST /api/v1/git-deploys", s.requireAuth(s.handleCreateGitDeploy))
	mux.HandleFunc("POST /api/v1/git-deploys/{id}/trigger", s.requireAuth(s.handleTriggerGitDeploy))
	mux.HandleFunc("DELETE /api/v1/git-deploys/{id}", s.requireAuth(s.handleDeleteGitDeploy))

	// Multi-Server Nodes & Panel Import / Migrations (7.13)
	mux.HandleFunc("GET /api/v1/nodes", s.requireAuth(s.handleListNodes))
	mux.HandleFunc("POST /api/v1/nodes", s.requireAuth(s.handleCreateNode))
	mux.HandleFunc("DELETE /api/v1/nodes/{id}", s.requireAuth(s.handleDeleteNode))
	mux.HandleFunc("POST /api/v1/import", s.requireAuth(s.handlePanelImport))
	mux.HandleFunc("GET /api/v1/migrations/bundles", s.requireAuth(s.handleListMigrationBundles))
	mux.HandleFunc("POST /api/v1/migrations/inspect", s.requireAuth(s.handleInspectMigration))
	mux.HandleFunc("POST /api/v1/migrations/export", s.requireAuth(s.handleExportAccount))
	mux.HandleFunc("POST /api/v1/migrations/import", s.requireAuth(s.handleMigrationImport))
	mux.HandleFunc("POST /api/v1/migrations/transfer", s.requireAuth(s.handleMigrationTransfer))

	// Security Incident Response, Backup Download, Billing SSO
	mux.HandleFunc("POST /api/v1/security/rotate-keys", s.requireAuth(s.handleRotateKeys))
	mux.HandleFunc("GET /api/v1/security/forensics", s.requireAuth(s.handleExportForensics))
	mux.HandleFunc("GET /api/v1/backups/{id}/download", s.requireAuth(s.handleDownloadBackup))
	mux.HandleFunc("POST /api/v1/billing/sso", s.requireAuth(s.handleBillingSSO))
	mux.HandleFunc("GET /api/v1/billing/usage", s.requireAuth(s.handleBillingUsage))

	// System Service Config Editor (Admin)
	mux.HandleFunc("GET /api/v1/system/configs", s.requireAuth(s.handleListSysConfigs))
	mux.HandleFunc("GET /api/v1/system/configs/{service}", s.requireAuth(s.handleGetSysConfig))
	mux.HandleFunc("PUT /api/v1/system/configs/{service}", s.requireAuth(s.handleUpdateSysConfig))

	// Stako Panel Self-Update (7.18 FR-UP6, Admin)
	mux.HandleFunc("GET /api/v1/system/update", s.requireAuth(s.handleCheckPanelUpdate))
	mux.HandleFunc("POST /api/v1/system/update", s.requireAuth(s.handleApplyPanelUpdate))

	// Smart System Services Manager (7.21 FR-IN7, Admin)
	mux.HandleFunc("GET /api/v1/system/services", s.requireAuth(s.handleListSysServices))
	mux.HandleFunc("GET /api/v1/system/php-versions", s.requireAuth(s.handleListPHPVersions))
	mux.HandleFunc("POST /api/v1/system/services/{id}/action", s.requireAuth(s.handleSysServiceAction))
	mux.HandleFunc("POST /api/v1/system/services/{id}/install", s.requireAuth(s.handleInstallSysService))
	mux.HandleFunc("DELETE /api/v1/system/services/{id}", s.requireAuth(s.handleUninstallSysService))

	// Audit, Jobs, System
	mux.HandleFunc("GET /api/v1/audit", s.requireAuth(s.handleListAudit))
	mux.HandleFunc("GET /api/v1/audit/verify", s.requireAuth(s.handleVerifyAudit))
	mux.HandleFunc("GET /api/v1/logs", s.requireAuth(s.handleQueryCentralLogs))
	mux.HandleFunc("GET /api/v1/logs/rotation", s.requireAuth(s.handleGetLogRotation))
	mux.HandleFunc("PUT /api/v1/logs/rotation", s.requireAuth(s.handleSetLogRotation))
	mux.HandleFunc("POST /api/v1/logs/rotate", s.requireAuth(s.handleRunLogRotation))
	mux.HandleFunc("GET /api/v1/logs/shipping", s.requireAuth(s.handleGetLogShipping))
	mux.HandleFunc("PUT /api/v1/logs/shipping", s.requireAuth(s.handleSetLogShipping))
	mux.HandleFunc("POST /api/v1/logs/shipping/test", s.requireAuth(s.handleTestLogShipping))
	mux.HandleFunc("GET /api/v1/jobs", s.requireAuth(s.handleListJobs))
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.requireAuth(s.handleGetJob))
	mux.HandleFunc("GET /api/v1/system/info", s.requireAuth(s.handleSystemInfo))

	// Database Web Managers (phpMyAdmin & phpPgAdmin via local Nginx on 127.0.0.1:8084)
	if dbToolsURL, err := url.Parse("http://127.0.0.1:8084"); err == nil {
		dbProxy := httputil.NewSingleHostReverseProxy(dbToolsURL)
		origDirector := dbProxy.Director
		dbProxy.Director = func(req *http.Request) {
			origDirector(req)
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-Forwarded-Host", req.Host)
		}
		dbProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			WriteProblem(w, http.StatusServiceUnavailable, "service-unavailable", "Database web manager unavailable", "Ensure phpmyadmin/phppgadmin, nginx, and php-fpm are installed and running.")
		}
		mux.HandleFunc("/phpmyadmin", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/phpmyadmin/", http.StatusMovedPermanently)
		})
		mux.Handle("/phpmyadmin/", dbProxy)
		mux.HandleFunc("/phppgadmin", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/phppgadmin/", http.StatusMovedPermanently)
		})
		mux.Handle("/phppgadmin/", dbProxy)
	}

	// Static Web UI with SPA fallback
	if s.webFS != nil {
		fileServer := http.FileServer(http.FS(s.webFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				WriteProblem(w, http.StatusNotFound, "not-found", "Endpoint not found", "")
				return
			}
			clean := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
			if clean == "" {
				clean = "index.html"
			}
			if _, err := fs.Stat(s.webFS, clean); err != nil {
				// Fallback to index.html for SPA
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	return s.securityHeaders(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": version.Version,
	})
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, 200)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}
