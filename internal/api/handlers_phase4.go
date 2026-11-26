// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

// --- Lockdown Mode (SEC-S23) ---

func (s *Server) isLockdown(ctx context.Context) bool {
	v, _ := s.store.GetSetting(ctx, "lockdown_enabled")
	return v == "true"
}

func (s *Server) handleGetLockdown(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	ipsRaw, _ := s.store.GetSetting(r.Context(), "lockdown_allowed_ips")
	var ips []string
	if ipsRaw != "" {
		_ = json.Unmarshal([]byte(ipsRaw), &ips)
	}
	if ips == nil {
		ips = []string{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":     s.isLockdown(r.Context()),
		"allowed_ips": ips,
	})
}

func (s *Server) handleSetLockdown(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Enabled    bool     `json:"enabled"`
		AllowedIPs []string `json:"allowed_ips"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	val := "false"
	if req.Enabled {
		val = "true"
		_ = s.store.DeleteNonAdminSessions(r.Context())
	}
	_ = s.store.SetSetting(r.Context(), "lockdown_enabled", val)
	if req.AllowedIPs != nil {
		b, _ := json.Marshal(req.AllowedIPs)
		_ = s.store.SetSetting(r.Context(), "lockdown_allowed_ips", string(b))
	} else {
		req.AllowedIPs = []string{}
	}
	s.audit(r, ac.User, "security.lockdown", "system", "lockdown", "success", map[string]any{"enabled": req.Enabled})
	WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":     req.Enabled,
		"allowed_ips": req.AllowedIPs,
	})
}

// --- Mail Server (7.6) ---

var localPartRe = regexp.MustCompile(`^[a-zA-Z0-9._+-]{1,64}$`)

func generateDKIMPublicRecord() string {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "v=DKIM1; k=rsa; p=unavailable"
	}
	der, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der)
}

func (s *Server) handleListMailDomains(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListMailDomains(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateMailDomain(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Domain string  `json:"domain"`
		UserID *string `json:"user_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	if err := agentproto.ValidateDomain(req.Domain); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid domain", err.Error())
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	id, _ := crypto.RandomID("md")
	md := &store.MailDomain{
		ID: id, UserID: owner.ID, Domain: req.Domain,
		DKIMSelector: "default", DKIMPublicKey: generateDKIMPublicRecord(),
	}
	if err := s.store.CreateMailDomain(r.Context(), md); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "mail.domain_add", ac.User.ID, map[string]any{"domain": md.Domain}); err == nil {
		jobID = &j.ID
	}
	dnsRecords := []map[string]string{
		{"type": "TXT", "name": md.Domain, "value": "v=spf1 mx a ~all"},
		{"type": "TXT", "name": "default._domainkey." + md.Domain, "value": md.DKIMPublicKey},
		{"type": "TXT", "name": "_dmarc." + md.Domain, "value": "v=DMARC1; p=quarantine; rua=mailto:postmaster@" + md.Domain},
	}
	s.audit(r, ac.User, "mail.domain_create", "mail_domain", md.ID, "success", map[string]any{"domain": md.Domain})
	WriteJSON(w, http.StatusCreated, map[string]any{
		"mail_domain": md, "dns_records": dnsRecords, "job_id": jobID,
	})
}

func (s *Server) handleDeleteMailDomain(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	md, err := s.store.GetMailDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, md.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteMailDomain(r.Context(), md.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	_, _ = s.jobs.Enqueue(r.Context(), "mail.domain_del", ac.User.ID, map[string]any{"domain": md.Domain})
	s.audit(r, ac.User, "mail.domain_delete", "mail_domain", md.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMailboxes(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	md, err := s.store.GetMailDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, md.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	items, err := s.store.ListMailboxes(r.Context(), md.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateMailbox(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	md, err := s.store.GetMailDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, md.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		LocalPart string `json:"local_part"`
		Password  string `json:"password"`
		QuotaMB   int    `json:"quota_mb"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if !localPartRe.MatchString(req.LocalPart) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid local_part", "")
		return
	}
	if err := crypto.ValidatePasswordStrength(req.Password); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Weak password", err.Error())
		return
	}
	if req.QuotaMB <= 0 {
		req.QuotaMB = 1024
	}
	id, _ := crypto.RandomID("mb")
	mb := &store.Mailbox{
		ID: id, MailDomainID: md.ID, Email: strings.ToLower(req.LocalPart) + "@" + md.Domain, QuotaMB: req.QuotaMB,
	}
	if err := s.store.CreateMailbox(r.Context(), mb); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "mail.mailbox_create", "mailbox", mb.ID, "success", map[string]any{"email": mb.Email})
	WriteJSON(w, http.StatusCreated, mb)
}

func (s *Server) handleDeleteMailbox(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	mb, err := s.store.GetMailbox(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	md, err := s.store.GetMailDomain(r.Context(), mb.MailDomainID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, md.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteMailbox(r.Context(), mb.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "mail.mailbox_delete", "mailbox", mb.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- Backups (7.12, 7.19) ---

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter *string
	if ac.User.Role != "admin" {
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListBackups(r.Context(), userFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Kind   string  `json:"kind"`
		Target string  `json:"target"`
		UserID *string `json:"user_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Kind == "" {
		req.Kind = "user_data"
	}
	if req.Kind == "system_config" && ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Only admin can backup system configuration", "")
		return
	}
	if !slices.Contains([]string{"user_data", "system_config"}, req.Kind) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid kind", "")
		return
	}
	if req.Target == "" {
		req.Target = "local"
	}
	if !slices.Contains([]string{"local", "s3", "sftp"}, req.Target) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid target", "")
		return
	}

	var uid *string
	var username string
	if req.Kind == "user_data" {
		owner, ok := s.resolveTargetUser(r, ac, req.UserID)
		if !ok {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
			return
		}
		uid = &owner.ID
		username = owner.Username
	}
	id, _ := crypto.RandomID("bk")
	checksum := "sha256:" + crypto.SHA256Hex(id+req.Kind+req.Target)
	bk := &store.Backup{
		ID: id, UserID: uid, Kind: req.Kind, Target: req.Target,
		Checksum: checksum, Status: "completed",
	}
	if err := s.store.CreateBackup(r.Context(), bk); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "backup.run", ac.User.ID, map[string]any{
		"id": bk.ID, "kind": bk.Kind, "target": bk.Target, "username": username,
	}); err == nil {
		jobID = &j.ID
	}
	s.dispatchWebhookEvent("backup.created", map[string]any{"id": bk.ID, "kind": bk.Kind, "target": bk.Target})
	s.audit(r, ac.User, "backup.create", "backup", bk.ID, "success", map[string]any{"kind": bk.Kind, "target": bk.Target})
	WriteJSON(w, http.StatusCreated, map[string]any{"backup": bk, "job_id": jobID})
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	bk, err := s.store.GetBackup(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if ac.User.Role != "admin" && (bk.UserID == nil || *bk.UserID != ac.User.ID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "backup.restore", ac.User.ID, map[string]any{"id": bk.ID}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "backup.restore", "backup", bk.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	bk, err := s.store.GetBackup(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if ac.User.Role != "admin" && (bk.UserID == nil || *bk.UserID != ac.User.ID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteBackup(r.Context(), bk.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "backup.delete", "backup", bk.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- Webhooks (7.17, SSRF-protected per SEC-A14) ---

func validateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return fmt.Errorf("invalid webhook URL")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("internal hostnames blocked (SEC-A14)")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("private/loopback/metadata IP blocked (SEC-A14)")
		}
	}
	return nil
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" && ac.User.Role != "reseller" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var userFilter *string
	if ac.User.Role != "admin" {
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListWebhooks(r.Context(), userFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" && ac.User.Role != "reseller" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
		Secret string   `json:"secret"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if err := validateWebhookURL(req.URL); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Blocked webhook URL", err.Error(),
			FieldError{Field: "url", Message: err.Error()})
		return
	}
	if len(req.Events) == 0 {
		req.Events = []string{"account.created"}
	}
	if req.Secret == "" {
		req.Secret, _ = crypto.RandomHex(24)
	}
	secEnc, _ := s.enc.Encrypt(req.Secret)
	id, _ := crypto.RandomID("wh")
	wh := &store.Webhook{
		ID: id, UserID: ac.User.ID, URL: req.URL, Events: req.Events, SecretEnc: secEnc,
	}
	if err := s.store.CreateWebhook(r.Context(), wh); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "webhook.create", "webhook", wh.ID, "success", map[string]any{"url": wh.URL})
	WriteJSON(w, http.StatusCreated, map[string]any{
		"webhook": wh,
		"secret":  req.Secret,
	})
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" && ac.User.Role != "reseller" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteWebhook(r.Context(), r.PathValue("id"), ac.User.ID, ac.User.Role == "admin"); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "webhook.delete", "webhook", r.PathValue("id"), "success", nil)
	w.WriteHeader(http.StatusNoContent)
}
