// SPDX-License-Identifier: MIT
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/StakoCP/stako/internal/agentclient"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/jobs"
	"github.com/StakoCP/stako/internal/store"
)

func setupTestServer(t *testing.T) (*Server, *store.Store, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	enc, err := crypto.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatalf("master key: %v", err)
	}
	ac := agentclient.New("", true)
	jq := jobs.NewQueue(st, ac)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	jq.Start(ctx)

	// Seed admin & regular user
	pwHash, _ := crypto.HashPassword("Admin-Strong-Password-123")
	_ = st.CreateUser(ctx, &store.User{
		ID: "u_admin", Username: "admin", Email: "admin@example.com",
		PasswordHash: pwHash, Role: "admin", Status: "active", Shell: "nologin",
	})
	_ = st.CreateUser(ctx, &store.User{
		ID: "u_user1", Username: "alice", Email: "alice@example.com",
		PasswordHash: pwHash, Role: "user", Status: "active", Shell: "nologin",
	})

	srv := NewServer(Config{
		Store:        st,
		Agent:        ac,
		Jobs:         jq,
		Encryptor:    enc,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		InsecureHTTP: true,
	})
	return srv, st, srv.Handler()
}

func doReq(t *testing.T, h http.Handler, method, path string, body any, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthCSRFAndRBAC(t *testing.T) {
	_, st, h := setupTestServer(t)

	// 1. Health check & security headers
	rec := doReq(t, h, "GET", "/api/v1/health", nil, nil, "")
	if rec.Code != 200 {
		t.Fatalf("health = %d", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP header")
	}

	// 2. Login as admin
	rec = doReq(t, h, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "Admin-Strong-Password-123",
	}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("login = %d: %s", rec.Code, rec.Body.String())
	}
	var loginResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	cookies := rec.Result().Cookies()

	// 3. POST without CSRF fails 403
	rec = doReq(t, h, "POST", "/api/v1/packages", map[string]any{"name": "Pro"}, cookies, "")
	if rec.Code != 403 {
		t.Fatalf("expected 403 without CSRF, got %d", rec.Code)
	}

	// 4. POST with CSRF succeeds
	rec = doReq(t, h, "POST", "/api/v1/packages", map[string]any{"name": "Pro", "disk_mb": 20480}, cookies, loginResp.CSRFToken)
	if rec.Code != 201 {
		t.Fatalf("create package = %d: %s", rec.Code, rec.Body.String())
	}

	// 5. Login as regular user alice -> cannot access /audit or /users/u_admin (IDOR check)
	rec = doReq(t, h, "POST", "/api/v1/auth/login", map[string]string{
		"username": "alice", "password": "Admin-Strong-Password-123",
	}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("alice login = %d", rec.Code)
	}
	var aliceLogin struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &aliceLogin)
	aliceCookies := rec.Result().Cookies()

	if rec := doReq(t, h, "GET", "/api/v1/audit", nil, aliceCookies, ""); rec.Code != 403 {
		t.Fatalf("expected 403 for alice on /audit, got %d", rec.Code)
	}
	if rec := doReq(t, h, "GET", "/api/v1/users/u_admin", nil, aliceCookies, ""); rec.Code != 403 {
		t.Fatalf("expected 403 IDOR block for alice on u_admin, got %d", rec.Code)
	}

	// 6. Verify tamper-evident audit chain
	valid, checked, _, err := st.VerifyAuditChain(context.Background())
	if err != nil || !valid || checked < 2 {
		t.Fatalf("audit verify failed: valid=%v checked=%d err=%v", valid, checked, err)
	}
}

func TestPhase1DomainsAppsIPsAndImpersonation(t *testing.T) {
	_, st, h := setupTestServer(t)

	// Login as admin
	rec := doReq(t, h, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "Admin-Strong-Password-123",
	}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("login = %d", rec.Code)
	}
	var loginResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	cookies := rec.Result().Cookies()
	csrf := loginResp.CSRFToken

	// Sync IPs
	if rec := doReq(t, h, "POST", "/api/v1/ips/sync", nil, cookies, csrf); rec.Code != 200 {
		t.Fatalf("ip sync = %d: %s", rec.Code, rec.Body.String())
	}

	// Reauth before impersonating alice
	if rec := doReq(t, h, "POST", "/api/v1/auth/reauth", map[string]string{
		"password": "Admin-Strong-Password-123",
	}, cookies, csrf); rec.Code != 204 {
		t.Fatalf("reauth = %d", rec.Code)
	}

	// Impersonate alice
	if rec := doReq(t, h, "POST", "/api/v1/auth/impersonate", map[string]string{
		"user_id": "u_user1", "reason": "support",
	}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("impersonate = %d: %s", rec.Code, rec.Body.String())
	}

	// Create domain as alice (acting_as)
	rec = doReq(t, h, "POST", "/api/v1/domains", map[string]any{
		"domain": "alice.example.com", "template": "proxy", "proxy_target": "127.0.0.1:3000",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create domain = %d: %s", rec.Code, rec.Body.String())
	}
	var domResp struct {
		Domain struct {
			ID string `json:"id"`
		} `json:"domain"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &domResp)

	// Add proxy rule
	if rec := doReq(t, h, "POST", "/api/v1/domains/"+domResp.Domain.ID+"/proxy-rules", map[string]any{
		"path": "/ws", "upstream": "127.0.0.1:3001", "websocket": true,
	}, cookies, csrf); rec.Code != 201 {
		t.Fatalf("create proxy rule = %d: %s", rec.Code, rec.Body.String())
	}

	// Create Go app with encrypted env
	rec = doReq(t, h, "POST", "/api/v1/apps", map[string]any{
		"name": "api", "runtime": "go", "command": "./api",
		"env": map[string]string{"DB_PASS": "super-secret-value"},
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create app = %d: %s", rec.Code, rec.Body.String())
	}
	var appResp struct {
		App struct {
			ID string `json:"id"`
		} `json:"app"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &appResp)

	// Reveal env (reauth is still valid)
	rec = doReq(t, h, "GET", "/api/v1/apps/"+appResp.App.ID+"/env", nil, cookies, "")
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("super-secret-value")) {
		t.Fatalf("reveal env = %d: %s", rec.Code, rec.Body.String())
	}

	// Exit impersonation
	if rec := doReq(t, h, "POST", "/api/v1/auth/impersonate/exit", nil, cookies, csrf); rec.Code != 200 {
		t.Fatalf("exit impersonate = %d", rec.Code)
	}

	// Verify audit chain still valid
	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestPhase2DatabasesFTPFirewallAndSecurity(t *testing.T) {
	_, st, h := setupTestServer(t)

	rec := doReq(t, h, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "Admin-Strong-Password-123",
	}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("login = %d", rec.Code)
	}
	var loginResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	cookies := rec.Result().Cookies()
	csrf := loginResp.CSRFToken

	// Create Postgres DB
	rec = doReq(t, h, "POST", "/api/v1/databases", map[string]any{
		"name": "blog_db", "engine": "postgres", "db_password": "DB-Strong-Password-123",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create db = %d: %s", rec.Code, rec.Body.String())
	}

	// Create SFTP account (plain FTP rejected)
	rec = doReq(t, h, "POST", "/api/v1/ftp", map[string]any{
		"username": "deploy_ftp", "password": "FTP-Strong-Password-123", "protocol": "ftp",
	}, cookies, csrf)
	if rec.Code != 422 {
		t.Fatalf("expected 422 for plain ftp, got %d", rec.Code)
	}
	rec = doReq(t, h, "POST", "/api/v1/ftp", map[string]any{
		"username": "deploy_ftp", "password": "FTP-Strong-Password-123", "protocol": "sftp",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create sftp = %d: %s", rec.Code, rec.Body.String())
	}

	// Firewall rule
	rec = doReq(t, h, "POST", "/api/v1/firewall/rules", map[string]any{
		"action": "allow", "protocol": "tcp", "port": "5432", "source_cidr": "10.0.0.0/8", "comment": "PG internal",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create fw rule = %d: %s", rec.Code, rec.Body.String())
	}

	// Security scan + updates
	rec = doReq(t, h, "POST", "/api/v1/security/scan", nil, cookies, csrf)
	if rec.Code != 200 {
		t.Fatalf("security scan = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, h, "GET", "/api/v1/updates", nil, cookies, "")
	if rec.Code != 200 {
		t.Fatalf("updates = %d", rec.Code)
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestPhase3DockerCatalogAndServices(t *testing.T) {
	_, st, h := setupTestServer(t)

	rec := doReq(t, h, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "Admin-Strong-Password-123",
	}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("login = %d", rec.Code)
	}
	var loginResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	cookies := rec.Result().Cookies()
	csrf := loginResp.CSRFToken

	// Reject forbidden docker.sock bind mount (SEC-S14)
	rec = doReq(t, h, "POST", "/api/v1/docker/containers", map[string]any{
		"name": "bad", "image": "alpine", "volumes": []string{"/var/run/docker.sock:/var/run/docker.sock"},
	}, cookies, csrf)
	if rec.Code != 403 {
		t.Fatalf("expected 403 for docker.sock mount, got %d", rec.Code)
	}

	// Create valid container
	rec = doReq(t, h, "POST", "/api/v1/docker/containers", map[string]any{
		"name": "web1", "image": "nginx:alpine", "ports": []string{"127.0.0.1:8081:80"},
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create container = %d: %s", rec.Code, rec.Body.String())
	}

	// Catalog has 10 apps
	rec = doReq(t, h, "GET", "/api/v1/catalog", nil, cookies, "")
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("wordpress")) {
		t.Fatalf("catalog = %d", rec.Code)
	}

	// Create Redis + RabbitMQ
	rec = doReq(t, h, "POST", "/api/v1/services/redis", map[string]any{"name": "cache1"}, cookies, csrf)
	if rec.Code != 201 || !bytes.Contains(rec.Body.Bytes(), []byte("redis://")) {
		t.Fatalf("redis = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, h, "POST", "/api/v1/services/rabbitmq", map[string]any{
		"vhost": "/app", "username": "app_mq", "password": "Rabbit-Strong-Password-123",
	}, cookies, csrf)
	if rec.Code != 201 || !bytes.Contains(rec.Body.Bytes(), []byte("amqp://")) {
		t.Fatalf("rabbitmq = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestPhase4MailBackupsWebhooksAndLockdown(t *testing.T) {
	_, st, h := setupTestServer(t)

	rec := doReq(t, h, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "Admin-Strong-Password-123",
	}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("login = %d", rec.Code)
	}
	var loginResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	cookies := rec.Result().Cookies()
	csrf := loginResp.CSRFToken

	// Mail domain + mailbox
	rec = doReq(t, h, "POST", "/api/v1/mail/domains", map[string]any{"domain": "mail.example.com"}, cookies, csrf)
	if rec.Code != 201 || !bytes.Contains(rec.Body.Bytes(), []byte("v=DKIM1")) {
		t.Fatalf("mail domain = %d: %s", rec.Code, rec.Body.String())
	}
	var mdResp struct {
		MailDomain struct {
			ID string `json:"id"`
		} `json:"mail_domain"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &mdResp)
	rec = doReq(t, h, "POST", "/api/v1/mail/domains/"+mdResp.MailDomain.ID+"/mailboxes", map[string]any{
		"local_part": "info", "password": "Mail-Strong-Password-123",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("mailbox = %d: %s", rec.Code, rec.Body.String())
	}

	// System config backup
	rec = doReq(t, h, "POST", "/api/v1/backups", map[string]any{"kind": "system_config", "target": "s3"}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("backup = %d: %s", rec.Code, rec.Body.String())
	}

	// Webhook SSRF block (169.254.169.254) vs valid public URL
	rec = doReq(t, h, "POST", "/api/v1/webhooks", map[string]any{"url": "http://169.254.169.254/latest"}, cookies, csrf)
	if rec.Code != 422 {
		t.Fatalf("expected 422 SSRF block, got %d", rec.Code)
	}
	rec = doReq(t, h, "POST", "/api/v1/webhooks", map[string]any{"url": "https://billing.example.com/hook"}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("webhook = %d: %s", rec.Code, rec.Body.String())
	}

	// Reauth + enable lockdown -> alice login must be blocked (403)
	if rec := doReq(t, h, "POST", "/api/v1/auth/reauth", map[string]string{
		"password": "Admin-Strong-Password-123",
	}, cookies, csrf); rec.Code != 204 {
		t.Fatalf("reauth = %d", rec.Code)
	}
	rec = doReq(t, h, "POST", "/api/v1/security/lockdown", map[string]any{"enabled": true}, cookies, csrf)
	if rec.Code != 200 {
		t.Fatalf("lockdown = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, h, "POST", "/api/v1/auth/login", map[string]string{
		"username": "alice", "password": "Admin-Strong-Password-123",
	}, nil, "")
	if rec.Code != 403 {
		t.Fatalf("expected 403 for alice during lockdown, got %d", rec.Code)
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestPhase5DNSCronFilesMonitoringAndTerminal(t *testing.T) {
	_, st, h := setupTestServer(t)

	rec := doReq(t, h, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "Admin-Strong-Password-123",
	}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("login = %d", rec.Code)
	}
	var loginResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	cookies := rec.Result().Cookies()
	csrf := loginResp.CSRFToken

	// DNS zone + record
	rec = doReq(t, h, "POST", "/api/v1/dns/zones", map[string]any{"domain": "zone.example.com", "provider": "powerdns"}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("dns zone = %d: %s", rec.Code, rec.Body.String())
	}
	var zResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &zResp)
	rec = doReq(t, h, "POST", "/api/v1/dns/zones/"+zResp.ID+"/records", map[string]any{
		"type": "A", "name": "@", "content": "203.0.113.10",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("dns record = %d", rec.Code)
	}

	// Cron job
	rec = doReq(t, h, "POST", "/api/v1/cron", map[string]any{"schedule": "0 2 * * *", "command": "/usr/bin/php /home/admin/cron.php"}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("cron = %d", rec.Code)
	}

	// File Manager + Monitoring + Terminal
	if rec := doReq(t, h, "GET", "/api/v1/files?path=/", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("files = %d", rec.Code)
	}
	if rec := doReq(t, h, "GET", "/api/v1/monitoring/metrics", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("metrics = %d", rec.Code)
	}
	if rec := doReq(t, h, "POST", "/api/v1/terminal/exec", map[string]any{"command": "uptime"}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("terminal = %d", rec.Code)
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestPhase6WAFGitDeployNodesAndImport(t *testing.T) {
	_, st, h := setupTestServer(t)

	rec := doReq(t, h, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "Admin-Strong-Password-123",
	}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("login = %d", rec.Code)
	}
	var loginResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
	cookies := rec.Result().Cookies()
	csrf := loginResp.CSRFToken

	// WAF enable
	rec = doReq(t, h, "POST", "/api/v1/waf", map[string]any{"enabled": true, "engine": "coraza", "mode": "blocking", "paranoia_level": 2}, cookies, csrf)
	if rec.Code != 200 {
		t.Fatalf("waf = %d: %s", rec.Code, rec.Body.String())
	}

	// Create domain then Git Deploy
	rec = doReq(t, h, "POST", "/api/v1/domains", map[string]any{"domain": "git.example.com", "template": "static"}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("domain = %d", rec.Code)
	}
	var domResp struct {
		Domain struct {
			ID string `json:"id"`
		} `json:"domain"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &domResp)

	rec = doReq(t, h, "POST", "/api/v1/git-deploys", map[string]any{
		"domain_id": domResp.Domain.ID, "repo_url": "https://github.com/StakoCP/example.git", "branch": "main",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("git deploy = %d: %s", rec.Code, rec.Body.String())
	}

	// Multi-server node + panel import
	rec = doReq(t, h, "POST", "/api/v1/nodes", map[string]any{"name": "node-2", "host": "203.0.113.50", "role": "worker"}, cookies, csrf)
	if rec.Code != 201 || !bytes.Contains(rec.Body.Bytes(), []byte("stn_")) {
		t.Fatalf("node = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, h, "POST", "/api/v1/import", map[string]any{"source_panel": "hestiacp", "archive_path": "/var/backups/hestia-user.tar"}, cookies, csrf)
	if rec.Code != 202 {
		t.Fatalf("import = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}
