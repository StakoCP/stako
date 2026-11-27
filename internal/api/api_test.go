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

	// Create domain as alice (acting_as) -> default docroot /home/alice/web/alice.example.com/public_html
	rec = doReq(t, h, "POST", "/api/v1/domains", map[string]any{
		"domain": "alice.example.com", "template": "proxy", "proxy_target": "127.0.0.1:3000",
	}, cookies, csrf)
	if rec.Code != 201 || !bytes.Contains(rec.Body.Bytes(), []byte("/home/alice/web/alice.example.com/public_html")) {
		t.Fatalf("create domain = %d: %s", rec.Code, rec.Body.String())
	}
	var domResp struct {
		Domain struct {
			ID string `json:"id"`
		} `json:"domain"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &domResp)
	// Reject docroot outside /home/alice/
	rec = doReq(t, h, "POST", "/api/v1/domains", map[string]any{
		"domain": "bad.example.com", "docroot": "/etc/nginx",
	}, cookies, csrf)
	if rec.Code != 422 {
		t.Fatalf("expected 422 for outside docroot, got %d", rec.Code)
	}

	// Add proxy rule
	if rec := doReq(t, h, "POST", "/api/v1/domains/"+domResp.Domain.ID+"/proxy-rules", map[string]any{
		"path": "/ws", "upstream": "127.0.0.1:3001", "websocket": true,
	}, cookies, csrf); rec.Code != 201 {
		t.Fatalf("create proxy rule = %d: %s", rec.Code, rec.Body.String())
	}

	// Read domain access, error, and goaccess logs
	for _, lt := range []string{"access", "error", "goaccess"} {
		if rec := doReq(t, h, "GET", "/api/v1/domains/"+domResp.Domain.ID+"/logs?type="+lt, nil, cookies, ""); rec.Code != 200 {
			t.Fatalf("domain logs (%s) = %d: %s", lt, rec.Code, rec.Body.String())
		}
	}

	// List installed PHP versions
	if rec := doReq(t, h, "GET", "/api/v1/system/php-versions", nil, cookies, ""); rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"items"`)) {
		t.Fatalf("php-versions = %d: %s", rec.Code, rec.Body.String())
	}

	// Create domain with nginx_apache stack
	if rec := doReq(t, h, "POST", "/api/v1/domains", map[string]any{
		"domain": "hybrid.example.com", "stack": "nginx_apache", "template": "php",
	}, cookies, csrf); rec.Code != 201 {
		t.Fatalf("create nginx_apache domain = %d: %s", rec.Code, rec.Body.String())
	}

	// Read & update per-domain custom nginx config
	if rec := doReq(t, h, "GET", "/api/v1/domains/"+domResp.Domain.ID+"/custom-nginx", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("get custom-nginx = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "PUT", "/api/v1/domains/"+domResp.Domain.ID+"/custom-nginx", map[string]string{
		"content": "client_max_body_size 64M;\n",
	}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("put custom-nginx = %d: %s", rec.Code, rec.Body.String())
	}

	// Set then clear ipv4_id with explicit null
	rec = doReq(t, h, "GET", "/api/v1/ips", nil, cookies, "")
	var ipsResp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ipsResp)
	if len(ipsResp.Items) > 0 {
		ipID := ipsResp.Items[0].ID
		if rec := doReq(t, h, "PATCH", "/api/v1/domains/"+domResp.Domain.ID, map[string]any{
			"ipv4_id": ipID,
		}, cookies, csrf); rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(ipID)) {
			t.Fatalf("set ipv4_id = %d: %s", rec.Code, rec.Body.String())
		}
		if rec := doReq(t, h, "PATCH", "/api/v1/domains/"+domResp.Domain.ID, map[string]any{
			"ipv4_id": nil,
		}, cookies, csrf); rec.Code != 200 || bytes.Contains(rec.Body.Bytes(), []byte(ipID)) {
			t.Fatalf("clear ipv4_id with null = %d: %s", rec.Code, rec.Body.String())
		}
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
	if rec.Code != 201 || !bytes.Contains(rec.Body.Bytes(), []byte(`"admin_blog_db"`)) {
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

	// Reject non-GitHub repo_url
	rec = doReq(t, h, "POST", "/api/v1/docker/containers", map[string]any{
		"name": "badgh", "repo_url": "https://evil.example.com/foo/bar",
	}, cookies, csrf)
	if rec.Code != 422 {
		t.Fatalf("expected 422 for non-github repo_url, got %d", rec.Code)
	}

	// Create and rebuild container from GitHub
	rec = doReq(t, h, "POST", "/api/v1/docker/containers", map[string]any{
		"name": "ghapp", "repo_url": "https://github.com/StakoCP/sample-app", "repo_branch": "main",
	}, cookies, csrf)
	if rec.Code != 201 || !bytes.Contains(rec.Body.Bytes(), []byte("stako-local/ghapp:latest")) {
		t.Fatalf("create github container = %d: %s", rec.Code, rec.Body.String())
	}
	var ghResp struct {
		Container struct {
			ID string `json:"id"`
		} `json:"container"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ghResp)
	if rec := doReq(t, h, "POST", "/api/v1/docker/containers/"+ghResp.Container.ID+"/rebuild", nil, cookies, csrf); rec.Code != 202 {
		t.Fatalf("rebuild github container = %d: %s", rec.Code, rec.Body.String())
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

	// Configure server-wide and per-domain SMTP relay (smarthost)
	rec = doReq(t, h, "PUT", "/api/v1/mail/relay", map[string]any{
		"enabled": true, "provider": "ses", "host": "email-smtp.us-east-1.amazonaws.com",
		"port": 587, "username": "AKIAEXAMPLE", "password": "secret-smtp-password", "tls_mode": "starttls",
	}, cookies, csrf)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"has_password":true`)) || bytes.Contains(rec.Body.Bytes(), []byte("secret-smtp-password")) {
		t.Fatalf("set server relay = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "GET", "/api/v1/mail/relay", nil, cookies, ""); rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("email-smtp.us-east-1.amazonaws.com")) {
		t.Fatalf("get server relay = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, h, "PUT", "/api/v1/mail/domains/"+mdResp.MailDomain.ID+"/relay", map[string]any{
		"enabled": true, "provider": "brevo", "host": "smtp-relay.brevo.com",
		"port": 587, "username": "user@example.com", "password": "brevo-secret",
	}, cookies, csrf)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("smtp-relay.brevo.com")) {
		t.Fatalf("set domain relay = %d: %s", rec.Code, rec.Body.String())
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
	if rec := doReq(t, h, "POST", "/api/v1/files/mkdir", map[string]any{"path": "/public_html/assets"}, cookies, csrf); rec.Code != 201 {
		t.Fatalf("mkdir = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/files/copy", map[string]any{"src": "/index.html", "dst": "/index2.html"}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("copy = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/files/move", map[string]any{"src": "/index2.html", "dst": "/index3.html"}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("move = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/files/chmod", map[string]any{"path": "/index3.html", "mode": "0644"}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("chmod = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/files/chmod", map[string]any{"path": "/index3.html", "mode": "999"}, cookies, csrf); rec.Code != 422 {
		t.Fatalf("expected 422 for invalid chmod, got %d", rec.Code)
	}
	if rec := doReq(t, h, "GET", "/api/v1/files/download?path=/index.html", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("download = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "GET", "/api/v1/files/raw?path=/logo.png", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("raw = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/files/compress", map[string]any{"src": "/public_html", "dst": "/public_html.zip"}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("compress = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/files/extract", map[string]any{"src": "/public_html.zip", "dst_dir": "/extracted"}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("extract = %d: %s", rec.Code, rec.Body.String())
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

func TestSecurityIncidentAndBillingSSO(t *testing.T) {
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

	// Billing SSO (user + admin) & Usage & OpenAPI Docs & Webhook deliveries
	rec = doReq(t, h, "POST", "/api/v1/billing/sso", map[string]string{"user_id": "u_user1"}, cookies, csrf)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("sso_token")) {
		t.Fatalf("billing sso = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/billing/sso", map[string]string{}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("admin billing sso = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "GET", "/api/v1/billing/usage", nil, cookies, ""); rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("items")) {
		t.Fatalf("billing usage = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "GET", "/api/v1/docs", nil, nil, ""); rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("/api/v1/billing/usage")) {
		t.Fatalf("api docs = %d", rec.Code)
	}
	if rec := doReq(t, h, "GET", "/api/v1/webhooks/deliveries", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("webhook deliveries = %d: %s", rec.Code, rec.Body.String())
	}

	// Reauth -> rotate keys + forensics export
	if rec := doReq(t, h, "POST", "/api/v1/auth/reauth", map[string]string{"password": "Admin-Strong-Password-123"}, cookies, csrf); rec.Code != 204 {
		t.Fatalf("reauth = %d", rec.Code)
	}
	if rec := doReq(t, h, "POST", "/api/v1/security/rotate-keys", nil, cookies, csrf); rec.Code != 200 {
		t.Fatalf("rotate-keys = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "GET", "/api/v1/security/forensics", nil, cookies, ""); rec.Code != 200 || rec.Header().Get("X-Checksum-Sha256") == "" {
		t.Fatalf("forensics = %d", rec.Code)
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestSystemConfigEditor(t *testing.T) {
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

	// List managed configs
	rec = doReq(t, h, "GET", "/api/v1/system/configs", nil, cookies, "")
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("nginx")) {
		t.Fatalf("list sysconfigs = %d: %s", rec.Code, rec.Body.String())
	}

	// PUT without reauth -> 403 reauth-required
	rec = doReq(t, h, "PUT", "/api/v1/system/configs/nginx", map[string]string{"content": "worker_processes 4;"}, cookies, csrf)
	if rec.Code != 403 {
		t.Fatalf("expected 403 without reauth, got %d", rec.Code)
	}

	// Reauth -> PUT succeeds and saves revision
	if rec := doReq(t, h, "POST", "/api/v1/auth/reauth", map[string]string{"password": "Admin-Strong-Password-123"}, cookies, csrf); rec.Code != 204 {
		t.Fatalf("reauth = %d", rec.Code)
	}
	rec = doReq(t, h, "PUT", "/api/v1/system/configs/nginx", map[string]string{"content": "worker_processes 4;", "comment": "tune workers"}, cookies, csrf)
	if rec.Code != 200 {
		t.Fatalf("update sysconfig = %d: %s", rec.Code, rec.Body.String())
	}

	// GET returns updated content and history
	rec = doReq(t, h, "GET", "/api/v1/system/configs/nginx", nil, cookies, "")
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("worker_processes 4;")) {
		t.Fatalf("get sysconfig = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestPanelSelfUpdate(t *testing.T) {
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

	// Apply update without reauth -> 403
	rec = doReq(t, h, "POST", "/api/v1/system/update", map[string]string{"version": "latest"}, cookies, csrf)
	if rec.Code != 403 {
		t.Fatalf("expected 403 without reauth, got %d", rec.Code)
	}

	// Reauth -> apply update returns 202 with job_id and pre-update backup_id
	if rec := doReq(t, h, "POST", "/api/v1/auth/reauth", map[string]string{"password": "Admin-Strong-Password-123"}, cookies, csrf); rec.Code != 204 {
		t.Fatalf("reauth = %d", rec.Code)
	}
	rec = doReq(t, h, "POST", "/api/v1/system/update", map[string]string{"version": "latest"}, cookies, csrf)
	if rec.Code != 202 || !bytes.Contains(rec.Body.Bytes(), []byte("backup_id")) {
		t.Fatalf("apply update = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestSystemServicesManager(t *testing.T) {
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

	// List detected services
	rec = doReq(t, h, "GET", "/api/v1/system/services", nil, cookies, "")
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("nginx")) {
		t.Fatalf("list sysservices = %d: %s", rec.Code, rec.Body.String())
	}

	// Start/restart nginx
	rec = doReq(t, h, "POST", "/api/v1/system/services/nginx/action", map[string]string{"action": "restart"}, cookies, csrf)
	if rec.Code != 200 {
		t.Fatalf("restart nginx = %d: %s", rec.Code, rec.Body.String())
	}

	// Protected service (ssh) cannot be stopped
	rec = doReq(t, h, "POST", "/api/v1/system/services/ssh/action", map[string]string{"action": "stop"}, cookies, csrf)
	if rec.Code != 403 {
		t.Fatalf("expected 403 stopping ssh, got %d", rec.Code)
	}

	// Install service
	rec = doReq(t, h, "POST", "/api/v1/system/services/redis/install", nil, cookies, csrf)
	if rec.Code != 202 {
		t.Fatalf("install redis = %d", rec.Code)
	}

	// Reauth -> uninstall non-protected service; protected ssh still 403
	if rec := doReq(t, h, "POST", "/api/v1/auth/reauth", map[string]string{"password": "Admin-Strong-Password-123"}, cookies, csrf); rec.Code != 204 {
		t.Fatalf("reauth = %d", rec.Code)
	}
	if rec := doReq(t, h, "DELETE", "/api/v1/system/services/ssh", nil, cookies, csrf); rec.Code != 403 {
		t.Fatalf("expected 403 uninstalling ssh, got %d", rec.Code)
	}
	if rec := doReq(t, h, "DELETE", "/api/v1/system/services/redis", nil, cookies, csrf); rec.Code != 202 {
		t.Fatalf("uninstall redis = %d", rec.Code)
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestCentralizedLogsRotationAndShipping(t *testing.T) {
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

	// Query centralized logs
	for _, svc := range []string{"all", "web", "mail", "auth", "container", "panel"} {
		rec = doReq(t, h, "GET", "/api/v1/logs?service="+svc+"&lines=50", nil, cookies, "")
		if rec.Code != 200 {
			t.Fatalf("query logs (%s) = %d: %s", svc, rec.Code, rec.Body.String())
		}
	}
	if rec := doReq(t, h, "GET", "/api/v1/logs?service=invalid", nil, cookies, ""); rec.Code != 422 {
		t.Fatalf("expected 422 for invalid service, got %d", rec.Code)
	}

	// Rotation policy get/put/run
	if rec := doReq(t, h, "GET", "/api/v1/logs/rotation", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("get rotation = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "PUT", "/api/v1/logs/rotation", map[string]any{
		"frequency": "weekly", "rotate": 30, "maxsize": "250M", "compress": true,
	}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("set rotation = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/logs/rotate", nil, cookies, csrf); rec.Code != 200 {
		t.Fatalf("run rotation = %d: %s", rec.Code, rec.Body.String())
	}

	// Shipping config get/put (secret not returned in plaintext)
	if rec := doReq(t, h, "PUT", "/api/v1/logs/shipping", map[string]any{
		"syslog_enabled":       true,
		"syslog_host":          "logs.example.com",
		"syslog_port":          6514,
		"syslog_protocol":      "tls",
		"syslog_format":        "rfc5424",
		"loki_enabled":         true,
		"loki_url":             "https://loki.example.com/loki/api/v1/push",
		"loki_tenant_id":       "stako-prod",
		"loki_username":        "shipper",
		"loki_password":        "super-secret-token",
		"loki_labels":          "env=prod,panel=stako",
		"loki_tls_skip_verify": false,
	}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("set shipping = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, h, "GET", "/api/v1/logs/shipping", nil, cookies, "")
	if rec.Code != 200 {
		t.Fatalf("get shipping = %d: %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("super-secret-token")) {
		t.Fatalf("loki password leaked in GET /api/v1/logs/shipping")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"has_loki_password":true`)) {
		t.Fatalf("expected has_loki_password:true, got %s", rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestMigrationsExportInspectImportAndTransfer(t *testing.T) {
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

	// Create a user to export
	rec = doReq(t, h, "POST", "/api/v1/users", map[string]string{
		"username": "migalice", "email": "migalice@example.com", "password": "Alice-Strong-Password-123", "role": "user",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	var uResp struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &uResp)

	// List bundles
	if rec := doReq(t, h, "GET", "/api/v1/migrations/bundles", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("list bundles = %d: %s", rec.Code, rec.Body.String())
	}

	// Export account
	if rec := doReq(t, h, "POST", "/api/v1/migrations/export", map[string]any{
		"user_id": uResp.User.ID,
	}, cookies, csrf); rec.Code != 202 {
		t.Fatalf("export account = %d: %s", rec.Code, rec.Body.String())
	}

	// Inspect HestiaCP/cPanel/VestaCP archive
	if rec := doReq(t, h, "POST", "/api/v1/migrations/inspect", map[string]any{
		"source_panel": "hestiacp",
		"archive_path": "/var/backups/hestia-alice.tar.gz",
	}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("inspect archive = %d: %s", rec.Code, rec.Body.String())
	}

	// Import account with auto-provisioning
	if rec := doReq(t, h, "POST", "/api/v1/migrations/import", map[string]any{
		"source_panel":      "cpanel",
		"archive_path":      "/var/backups/cpmove-bob.tar.gz",
		"target_username":   "bob",
		"target_email":      "bob@example.com",
		"domains":           []string{"bobsite.example.com"},
		"databases":         []string{"bob_wp"},
		"mail_domains":      []string{"bobsite.example.com"},
		"include_files":     true,
		"include_databases": true,
		"include_mail":      true,
	}, cookies, csrf); rec.Code != 202 {
		t.Fatalf("import account = %d: %s", rec.Code, rec.Body.String())
	}

	// Server-to-server transfer
	if rec := doReq(t, h, "POST", "/api/v1/migrations/transfer", map[string]any{
		"direction":   "pull",
		"remote_host": "203.0.113.50",
		"remote_port": 22,
		"remote_user": "root",
		"remote_path": "/var/backups/user.tar.gz",
	}, cookies, csrf); rec.Code != 202 {
		t.Fatalf("transfer = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestUptimeMonitoringAndPublicStatusPage(t *testing.T) {
	_, st, h := setupTestServer(t)

	// Start a local test HTTP server as the monitored target
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

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

	// Create uptime monitor
	rec = doReq(t, h, "POST", "/api/v1/monitoring/uptime", map[string]any{
		"domain":          "app.example.local",
		"url":             ts.URL + "/",
		"interval_sec":    60,
		"expected_status": 200,
		"public_status":   true,
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create uptime monitor = %d: %s", rec.Code, rec.Body.String())
	}
	var mon struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &mon)

	// Run check
	rec = doReq(t, h, "POST", "/api/v1/monitoring/uptime/"+mon.ID+"/check", nil, cookies, csrf)
	if rec.Code != 200 {
		t.Fatalf("check uptime monitor = %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"up"`)) {
		t.Fatalf("expected status up, got %s", rec.Body.String())
	}

	// List monitors
	if rec := doReq(t, h, "GET", "/api/v1/monitoring/uptime", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("list uptime = %d: %s", rec.Code, rec.Body.String())
	}

	// Public status page (no cookies / unauthenticated)
	rec = doReq(t, h, "GET", "/api/v1/status", nil, nil, "")
	if rec.Code != 200 {
		t.Fatalf("public status = %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"overall_status":"operational"`)) {
		t.Fatalf("expected operational status, got %s", rec.Body.String())
	}

	// Delete monitor
	if rec := doReq(t, h, "DELETE", "/api/v1/monitoring/uptime/"+mon.ID, nil, cookies, csrf); rec.Code != 204 {
		t.Fatalf("delete uptime = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestGitPreviewEnvironments(t *testing.T) {
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

	// Create domain + git deploy target
	rec = doReq(t, h, "POST", "/api/v1/domains", map[string]any{
		"domain": "previewapp.example.com", "stack": "nginx_apache", "template": "php",
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

	rec = doReq(t, h, "POST", "/api/v1/git-deploys", map[string]any{
		"domain_id": domResp.Domain.ID,
		"repo_url":  "https://github.com/example/webapp.git",
		"branch":    "main",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create git deploy = %d: %s", rec.Code, rec.Body.String())
	}
	var gdResp struct {
		Deploy struct {
			ID string `json:"id"`
		} `json:"deploy"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &gdResp)

	// Create PR preview environment
	rec = doReq(t, h, "POST", "/api/v1/git-deploys/"+gdResp.Deploy.ID+"/previews", map[string]any{
		"branch":         "feat/new-ui",
		"pr_number":      42,
		"container_port": 3000,
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create preview = %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"subdomain":"pr-42.previewapp.example.com"`)) {
		t.Fatalf("expected subdomain pr-42.previewapp.example.com, got %s", rec.Body.String())
	}
	var prevResp struct {
		Preview struct {
			ID string `json:"id"`
		} `json:"preview"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &prevResp)

	// List previews
	if rec := doReq(t, h, "GET", "/api/v1/git-previews", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("list previews = %d: %s", rec.Code, rec.Body.String())
	}

	// Redeploy preview
	if rec := doReq(t, h, "POST", "/api/v1/git-previews/"+prevResp.Preview.ID+"/redeploy", nil, cookies, csrf); rec.Code != 202 {
		t.Fatalf("redeploy preview = %d: %s", rec.Code, rec.Body.String())
	}

	// Delete preview
	if rec := doReq(t, h, "DELETE", "/api/v1/git-previews/"+prevResp.Preview.ID, nil, cookies, csrf); rec.Code != 204 {
		t.Fatalf("delete preview = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestWordPressToolkit(t *testing.T) {
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

	// Create a WordPress domain
	rec = doReq(t, h, "POST", "/api/v1/domains", map[string]any{
		"domain": "wpblog.example.com", "stack": "nginx_apache", "template": "wordpress",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create wp domain = %d: %s", rec.Code, rec.Body.String())
	}

	// Scan WordPress sites
	rec = doReq(t, h, "POST", "/api/v1/wordpress/scan", nil, cookies, csrf)
	if rec.Code != 200 {
		t.Fatalf("scan wp = %d: %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Items []struct {
			ID     string `json:"id"`
			Domain string `json:"domain"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	if len(listResp.Items) == 0 {
		t.Fatalf("expected at least 1 scanned WP site, got 0: %s", rec.Body.String())
	}
	wpID := listResp.Items[0].ID

	// Bulk update
	if rec := doReq(t, h, "POST", "/api/v1/wordpress/bulk-update", map[string]any{
		"site_ids": []string{wpID},
	}, cookies, csrf); rec.Code != 202 {
		t.Fatalf("bulk update = %d: %s", rec.Code, rec.Body.String())
	}

	// Harden security
	if rec := doReq(t, h, "POST", "/api/v1/wordpress/"+wpID+"/harden", nil, cookies, csrf); rec.Code != 200 {
		t.Fatalf("harden = %d: %s", rec.Code, rec.Body.String())
	}

	// Auto-login
	rec = doReq(t, h, "POST", "/api/v1/wordpress/"+wpID+"/autologin", nil, cookies, csrf)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"login_url"`)) {
		t.Fatalf("autologin = %d: %s", rec.Code, rec.Body.String())
	}

	// Create staging + push staging
	rec = doReq(t, h, "POST", "/api/v1/wordpress/"+wpID+"/staging", map[string]any{
		"staging_domain": "staging.wpblog.example.com",
	}, cookies, csrf)
	if rec.Code != 201 {
		t.Fatalf("create staging = %d: %s", rec.Code, rec.Body.String())
	}
	var stgResp struct {
		Site struct {
			ID string `json:"id"`
		} `json:"site"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &stgResp)

	if rec := doReq(t, h, "POST", "/api/v1/wordpress/"+stgResp.Site.ID+"/push-staging", nil, cookies, csrf); rec.Code != 202 {
		t.Fatalf("push staging = %d: %s", rec.Code, rec.Body.String())
	}

	// Delete staging site entry
	if rec := doReq(t, h, "DELETE", "/api/v1/wordpress/"+stgResp.Site.ID, nil, cookies, csrf); rec.Code != 204 {
		t.Fatalf("delete wp site = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestPostgreSQLAdvancedPgBouncerReplicationPITR(t *testing.T) {
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

	// Get initial config
	if rec := doReq(t, h, "GET", "/api/v1/databases/postgres/advanced", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("get pg advanced = %d: %s", rec.Code, rec.Body.String())
	}

	// Configure PgBouncer
	if rec := doReq(t, h, "PUT", "/api/v1/databases/postgres/pgbouncer", map[string]any{
		"enabled":           true,
		"listen_port":       6432,
		"pool_mode":         "transaction",
		"max_client_conn":   300,
		"default_pool_size": 25,
		"reserve_pool_size": 5,
	}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("set pgbouncer = %d: %s", rec.Code, rec.Body.String())
	}

	// Configure Replication (password must not leak in GET)
	if rec := doReq(t, h, "PUT", "/api/v1/databases/postgres/replication", map[string]any{
		"role":             "primary",
		"wal_level":        "replica",
		"max_wal_senders":  10,
		"wal_keep_size_mb": 512,
		"replication_user": "replicator",
		"replication_pass": "repl-secret-pw-99",
		"standby_hosts":    []string{"10.0.0.12/32"},
	}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("set replication = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, h, "GET", "/api/v1/databases/postgres/advanced", nil, cookies, "")
	if rec.Code != 200 {
		t.Fatalf("get pg advanced = %d: %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("repl-secret-pw-99")) {
		t.Fatalf("replication password leaked in GET /api/v1/databases/postgres/advanced")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"has_replication_password":true`)) {
		t.Fatalf("expected has_replication_password:true, got %s", rec.Body.String())
	}

	// Configure PITR + Basebackup + Restore
	if rec := doReq(t, h, "PUT", "/api/v1/databases/postgres/pitr", map[string]any{
		"enabled":        true,
		"archive_dir":    "/var/lib/postgresql/wal_archive",
		"retention_days": 14,
	}, cookies, csrf); rec.Code != 200 {
		t.Fatalf("set pitr = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/databases/postgres/pitr/basebackup", nil, cookies, csrf); rec.Code != 202 {
		t.Fatalf("pitr basebackup = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(t, h, "POST", "/api/v1/databases/postgres/pitr/restore", map[string]any{
		"target_time": "2026-11-27T08:00:00Z",
	}, cookies, csrf); rec.Code != 202 {
		t.Fatalf("pitr restore = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}

func TestDomainCacheRedisAndCloudflare(t *testing.T) {
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

	// Create domain
	rec = doReq(t, h, "POST", "/api/v1/domains", map[string]any{
		"domain": "cachedsite.example.com", "stack": "nginx_apache", "template": "wordpress",
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
	domID := domResp.Domain.ID

	// Get initial cache config
	if rec := doReq(t, h, "GET", "/api/v1/domains/"+domID+"/cache", nil, cookies, ""); rec.Code != 200 {
		t.Fatalf("get cache = %d: %s", rec.Code, rec.Body.String())
	}

	// Enable Nginx Microcache
	rec = doReq(t, h, "PUT", "/api/v1/domains/"+domID+"/cache/microcache", map[string]any{
		"enabled": true, "ttl_sec": 30,
	}, cookies, csrf)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"microcache_enabled":true`)) {
		t.Fatalf("set microcache = %d: %s", rec.Code, rec.Body.String())
	}

	// One-click Redis Object Cache
	rec = doReq(t, h, "POST", "/api/v1/domains/"+domID+"/cache/redis-object", map[string]any{
		"enabled": true,
	}, cookies, csrf)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"redis_object_cache":true`)) {
		t.Fatalf("redis object cache = %d: %s", rec.Code, rec.Body.String())
	}

	// Cloudflare CDN settings & purge (token must not leak)
	rec = doReq(t, h, "PUT", "/api/v1/domains/"+domID+"/cloudflare", map[string]any{
		"cf_zone_id":      "zone123456",
		"cf_api_token":    "test-cf-secret-token",
		"cf_under_attack": true,
		"cf_dev_mode":     false,
	}, cookies, csrf)
	if rec.Code != 200 || bytes.Contains(rec.Body.Bytes(), []byte("test-cf-secret-token")) {
		t.Fatalf("set cloudflare = %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"has_cf_token":true`)) {
		t.Fatalf("expected has_cf_token:true, got %s", rec.Body.String())
	}

	if rec := doReq(t, h, "POST", "/api/v1/domains/"+domID+"/cloudflare/purge", nil, cookies, csrf); rec.Code != 200 {
		t.Fatalf("purge cloudflare = %d: %s", rec.Code, rec.Body.String())
	}

	if valid, _, _, err := st.VerifyAuditChain(context.Background()); err != nil || !valid {
		t.Fatalf("audit chain broken: valid=%v err=%v", valid, err)
	}
}
