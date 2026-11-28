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
	"time"

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

func (s *Server) handleGetMailDNS(w http.ResponseWriter, r *http.Request) {
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
	selector := md.DKIMSelector
	if selector == "" {
		selector = "default"
	}
	dnsRecords := []map[string]string{
		{"type": "MX", "name": md.Domain, "value": "10 mail." + md.Domain},
		{"type": "TXT", "name": md.Domain, "value": "v=spf1 mx a ~all"},
		{"type": "TXT", "name": selector + "._domainkey." + md.Domain, "value": md.DKIMPublicKey},
		{"type": "TXT", "name": "_dmarc." + md.Domain, "value": "v=DMARC1; p=quarantine; rua=mailto:postmaster@" + md.Domain},
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"mail_domain": md,
		"dns_records": dnsRecords,
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

func (s *Server) syncMailAliases(ctx context.Context) {
	if s.agent == nil {
		return
	}
	all, err := s.store.ListAllMailAliases(ctx)
	if err != nil {
		return
	}
	entries := make([]map[string]any, 0, len(all))
	for _, a := range all {
		entries = append(entries, map[string]any{
			"source":       a.Source,
			"destination":  a.Destination,
			"is_catch_all": a.IsCatchAll,
		})
	}
	_, _ = s.agent.Call(ctx, agentproto.CmdMailAliasSync, map[string]any{"aliases": entries})
}

func (s *Server) handleListMailAliases(w http.ResponseWriter, r *http.Request) {
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
	items, err := s.store.ListMailAliases(r.Context(), md.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateMailAlias(w http.ResponseWriter, r *http.Request) {
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
		LocalPart   string `json:"local_part"`
		Destination string `json:"destination"`
		IsCatchAll  bool   `json:"is_catch_all"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Destination = strings.TrimSpace(strings.ToLower(req.Destination))
	if req.Destination == "" || !strings.Contains(req.Destination, "@") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Valid destination email is required", "")
		return
	}
	var source string
	if req.IsCatchAll || strings.TrimSpace(req.LocalPart) == "*" {
		req.IsCatchAll = true
		source = "@" + md.Domain
	} else {
		lp := strings.TrimSpace(strings.ToLower(req.LocalPart))
		if !localPartRe.MatchString(lp) {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid local_part", "")
			return
		}
		source = lp + "@" + md.Domain
	}
	id, _ := crypto.RandomID("ma")
	alias := &store.MailAlias{
		ID: id, MailDomainID: md.ID, Source: source, Destination: req.Destination, IsCatchAll: req.IsCatchAll,
	}
	if err := s.store.CreateMailAlias(r.Context(), alias); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncMailAliases(r.Context())
	s.audit(r, ac.User, "mail.alias_create", "mail_alias", alias.ID, "success", map[string]any{"source": alias.Source, "destination": alias.Destination})
	WriteJSON(w, http.StatusCreated, alias)
}

func (s *Server) handleDeleteMailAlias(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	a, err := s.store.GetMailAlias(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	md, err := s.store.GetMailDomain(r.Context(), a.MailDomainID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, md.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteMailAlias(r.Context(), a.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncMailAliases(r.Context())
	s.audit(r, ac.User, "mail.alias_delete", "mail_alias", a.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMailQueue(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	if s.agent == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	res, err := s.agent.Call(r.Context(), agentproto.CmdMailQueueList, map[string]any{})
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}, "error": err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, res.Data)
}

func (s *Server) handleFlushMailQueue(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	if s.agent != nil {
		if _, err := s.agent.Call(r.Context(), agentproto.CmdMailQueueAction, map[string]any{"action": "flush"}); err != nil {
			WriteProblem(w, http.StatusBadGateway, "agent_error", "Mail queue flush failed", err.Error())
			return
		}
	}
	s.audit(r, ac.User, "mail.queue_flush", "mail_queue", "all", "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"status": "flushed"})
}

func (s *Server) handleDeleteMailQueueItem(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	qid := strings.TrimSpace(r.PathValue("id"))
	if qid == "" {
		WriteProblem(w, http.StatusBadRequest, "validation", "Queue ID required", "")
		return
	}
	if s.agent != nil {
		if _, err := s.agent.Call(r.Context(), agentproto.CmdMailQueueAction, map[string]any{"action": "delete", "queue_id": qid}); err != nil {
			WriteProblem(w, http.StatusBadGateway, "agent_error", "Mail queue delete failed", err.Error())
			return
		}
	}
	s.audit(r, ac.User, "mail.queue_delete", "mail_queue", qid, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

type serverRelayConfig struct {
	Enabled     bool   `json:"enabled"`
	Provider    string `json:"provider"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordEnc string `json:"password_enc,omitempty"`
	TLSMode     string `json:"tls_mode"`
}

func validateRelayHost(host string) error {
	h := strings.TrimSpace(host)
	if h == "" || strings.ContainsAny(h, " \t\n\r/\\:;|&$`\"'<>") {
		return fmt.Errorf("invalid SMTP relay host")
	}
	return nil
}

func (s *Server) applyAllMailRelays(ctx context.Context) error {
	var srv serverRelayConfig
	if raw, err := s.store.GetSetting(ctx, "smtp_relay_server"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &srv)
	}
	srvPass := ""
	if srv.PasswordEnc != "" {
		srvPass, _ = s.enc.Decrypt(srv.PasswordEnc)
	}
	doms, err := s.store.ListMailDomains(ctx, nil, nil)
	if err != nil {
		return err
	}
	domRelays := make([]map[string]any, 0)
	for _, d := range doms {
		if d.RelayHost == "" {
			continue
		}
		p := ""
		if d.RelayPassEnc != "" {
			p, _ = s.enc.Decrypt(d.RelayPassEnc)
		}
		domRelays = append(domRelays, map[string]any{
			"domain":   d.Domain,
			"host":     d.RelayHost,
			"port":     d.RelayPort,
			"username": d.RelayUser,
			"password": p,
		})
	}
	resp, err := s.agent.Call(ctx, agentproto.CmdMailRelayApply, map[string]any{
		"server_relay": map[string]any{
			"enabled":  srv.Enabled,
			"host":     srv.Host,
			"port":     srv.Port,
			"username": srv.Username,
			"password": srvPass,
			"tls_mode": srv.TLSMode,
		},
		"domain_relays": domRelays,
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s %s", resp.Error, resp.Output)
	}
	return nil
}

func (s *Server) handleGetServerRelay(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	cfg := serverRelayConfig{Provider: "ses", Port: 587, TLSMode: "starttls"}
	if raw, err := s.store.GetSetting(r.Context(), "smtp_relay_server"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":      cfg.Enabled,
		"provider":     cfg.Provider,
		"host":         cfg.Host,
		"port":         cfg.Port,
		"username":     cfg.Username,
		"tls_mode":     cfg.TLSMode,
		"has_password": cfg.PasswordEnc != "",
	})
}

func (s *Server) handleSetServerRelay(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Enabled  bool   `json:"enabled"`
		Provider string `json:"provider"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		TLSMode  string `json:"tls_mode"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Provider == "" {
		req.Provider = "custom"
	}
	if req.Port <= 0 || req.Port > 65535 {
		req.Port = 587
	}
	if req.TLSMode == "" {
		req.TLSMode = "starttls"
	}
	if req.Enabled {
		if err := validateRelayHost(req.Host); err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
			return
		}
	}
	var prev serverRelayConfig
	if raw, err := s.store.GetSetting(r.Context(), "smtp_relay_server"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &prev)
	}
	passEnc := prev.PasswordEnc
	if req.Password != "" {
		passEnc, _ = s.enc.Encrypt(req.Password)
	}
	cfg := serverRelayConfig{
		Enabled:     req.Enabled,
		Provider:    req.Provider,
		Host:        strings.TrimSpace(req.Host),
		Port:        req.Port,
		Username:    strings.TrimSpace(req.Username),
		PasswordEnc: passEnc,
		TLSMode:     req.TLSMode,
	}
	b, _ := json.Marshal(cfg)
	if err := s.store.SetSetting(r.Context(), "smtp_relay_server", string(b)); err != nil {
		handleStoreErr(w, err)
		return
	}
	if err := s.applyAllMailRelays(r.Context()); err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to apply SMTP relay", err.Error())
		return
	}
	s.audit(r, ac.User, "mail.relay_server", "mail", "server", "success", map[string]any{"enabled": cfg.Enabled, "host": cfg.Host})
	WriteJSON(w, http.StatusOK, map[string]any{
		"enabled": cfg.Enabled, "provider": cfg.Provider, "host": cfg.Host, "port": cfg.Port,
		"username": cfg.Username, "tls_mode": cfg.TLSMode, "has_password": cfg.PasswordEnc != "",
	})
}

func (s *Server) handleSetDomainRelay(w http.ResponseWriter, r *http.Request) {
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
		Enabled  bool   `json:"enabled"`
		Provider string `json:"provider"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if !req.Enabled {
		if err := s.store.UpdateMailDomainRelay(r.Context(), md.ID, "", "", 0, "", ""); err != nil {
			handleStoreErr(w, err)
			return
		}
	} else {
		if err := validateRelayHost(req.Host); err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
			return
		}
		if req.Port <= 0 || req.Port > 65535 {
			req.Port = 587
		}
		if req.Provider == "" {
			req.Provider = "custom"
		}
		passEnc := md.RelayPassEnc
		if req.Password != "" {
			passEnc, _ = s.enc.Encrypt(req.Password)
		}
		if err := s.store.UpdateMailDomainRelay(r.Context(), md.ID, req.Provider, strings.TrimSpace(req.Host), req.Port, strings.TrimSpace(req.Username), passEnc); err != nil {
			handleStoreErr(w, err)
			return
		}
	}
	if err := s.applyAllMailRelays(r.Context()); err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to apply domain SMTP relay", err.Error())
		return
	}
	updated, _ := s.store.GetMailDomain(r.Context(), md.ID)
	s.audit(r, ac.User, "mail.relay_domain", "mail_domain", md.ID, "success", map[string]any{"enabled": req.Enabled, "host": req.Host})
	WriteJSON(w, http.StatusOK, updated)
}

func (s *Server) handleTestRelay(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if err := validateRelayHost(req.Host); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		req.Port = 587
	}
	addr := net.JoinHostPort(strings.TrimSpace(req.Host), fmt.Sprintf("%d", req.Port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		WriteProblem(w, http.StatusBadGateway, "relay-unreachable", "Cannot connect to SMTP relay", err.Error())
		return
	}
	_ = conn.Close()
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "address": addr})
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
	if !slices.Contains([]string{"local", "s3", "r2", "b2", "sftp"}, req.Target) {
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
	repoURL, repoEnv := s.buildResticArgs(r.Context(), bk.Target)
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "backup.run", ac.User.ID, map[string]any{
		"id": bk.ID, "kind": bk.Kind, "target": bk.Target, "username": username,
		"repo_url": repoURL, "repo_env": repoEnv,
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
	repoURL, repoEnv := s.buildResticArgs(r.Context(), bk.Target)
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "backup.restore", ac.User.ID, map[string]any{
		"id": bk.ID, "repo_url": repoURL, "repo_env": repoEnv,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "backup.restore", "backup", bk.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

type backupConfig struct {
	Target       string `json:"target"`
	Endpoint     string `json:"endpoint,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	Region       string `json:"region,omitempty"`
	AccessKey    string `json:"access_key,omitempty"`
	SecretKeyEnc string `json:"secret_key_enc,omitempty"`
	SFTPHost     string `json:"sftp_host,omitempty"`
	SFTPPort     int    `json:"sftp_port,omitempty"`
	SFTPUser     string `json:"sftp_user,omitempty"`
	SFTPPath     string `json:"sftp_path,omitempty"`
	KeepDaily    int    `json:"keep_daily"`
	KeepWeekly   int    `json:"keep_weekly"`
	KeepMonthly  int    `json:"keep_monthly"`
	ScheduleCron string `json:"schedule_cron"`
}

func (s *Server) loadBackupConfig(ctx context.Context) backupConfig {
	cfg := backupConfig{
		Target:       "local",
		KeepDaily:    7,
		KeepWeekly:   4,
		KeepMonthly:  6,
		ScheduleCron: "0 2 * * *",
		SFTPPort:     22,
	}
	if raw, err := s.store.GetSetting(ctx, "backup_config"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg
}

func (s *Server) buildResticArgs(ctx context.Context, target string) (string, map[string]string) {
	cfg := s.loadBackupConfig(ctx)
	env := map[string]string{}
	secret := ""
	if cfg.SecretKeyEnc != "" {
		secret, _ = s.enc.Decrypt(cfg.SecretKeyEnc)
	}
	switch target {
	case "s3", "r2":
		if cfg.Bucket != "" {
			ep := strings.TrimPrefix(strings.TrimPrefix(cfg.Endpoint, "https://"), "http://")
			if ep != "" {
				if cfg.AccessKey != "" {
					env["AWS_ACCESS_KEY_ID"] = cfg.AccessKey
				}
				if secret != "" {
					env["AWS_SECRET_ACCESS_KEY"] = secret
				}
				return "s3:https://" + ep + "/" + cfg.Bucket, env
			}
			if cfg.AccessKey != "" {
				env["AWS_ACCESS_KEY_ID"] = cfg.AccessKey
			}
			if secret != "" {
				env["AWS_SECRET_ACCESS_KEY"] = secret
			}
			if cfg.Region != "" {
				env["AWS_DEFAULT_REGION"] = cfg.Region
			}
			return "s3:s3.amazonaws.com/" + cfg.Bucket, env
		}
	case "b2":
		if cfg.Bucket != "" {
			if cfg.AccessKey != "" {
				env["B2_ACCOUNT_ID"] = cfg.AccessKey
			}
			if secret != "" {
				env["B2_ACCOUNT_KEY"] = secret
			}
			return "b2:" + cfg.Bucket + ":stako", env
		}
	case "sftp":
		if cfg.SFTPHost != "" && cfg.SFTPUser != "" {
			p := cfg.SFTPPath
			if p == "" {
				p = "/backups/stako"
			}
			port := cfg.SFTPPort
			if port <= 0 {
				port = 22
			}
			return fmt.Sprintf("sftp:%s@%s:%d:%s", cfg.SFTPUser, cfg.SFTPHost, port, p), env
		}
	}
	return "", env
}

func (s *Server) handleGetBackupConfig(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	cfg := s.loadBackupConfig(r.Context())
	WriteJSON(w, http.StatusOK, map[string]any{
		"target":        cfg.Target,
		"endpoint":      cfg.Endpoint,
		"bucket":        cfg.Bucket,
		"region":        cfg.Region,
		"access_key":    cfg.AccessKey,
		"has_secret":    cfg.SecretKeyEnc != "",
		"sftp_host":     cfg.SFTPHost,
		"sftp_port":     cfg.SFTPPort,
		"sftp_user":     cfg.SFTPUser,
		"sftp_path":     cfg.SFTPPath,
		"keep_daily":    cfg.KeepDaily,
		"keep_weekly":   cfg.KeepWeekly,
		"keep_monthly":  cfg.KeepMonthly,
		"schedule_cron": cfg.ScheduleCron,
	})
}

func (s *Server) handleSetBackupConfig(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Target       string `json:"target"`
		Endpoint     string `json:"endpoint"`
		Bucket       string `json:"bucket"`
		Region       string `json:"region"`
		AccessKey    string `json:"access_key"`
		SecretKey    string `json:"secret_key"`
		SFTPHost     string `json:"sftp_host"`
		SFTPPort     int    `json:"sftp_port"`
		SFTPUser     string `json:"sftp_user"`
		SFTPPath     string `json:"sftp_path"`
		KeepDaily    int    `json:"keep_daily"`
		KeepWeekly   int    `json:"keep_weekly"`
		KeepMonthly  int    `json:"keep_monthly"`
		ScheduleCron string `json:"schedule_cron"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Target == "" {
		req.Target = "local"
	}
	if !slices.Contains([]string{"local", "s3", "r2", "b2", "sftp"}, req.Target) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid target", "")
		return
	}
	prev := s.loadBackupConfig(r.Context())
	secretEnc := prev.SecretKeyEnc
	if strings.TrimSpace(req.SecretKey) != "" {
		enc, err := s.enc.Encrypt(strings.TrimSpace(req.SecretKey))
		if err != nil {
			WriteProblem(w, http.StatusInternalServerError, "internal", "Encryption failed", err.Error())
			return
		}
		secretEnc = enc
	}
	if req.KeepDaily <= 0 {
		req.KeepDaily = 7
	}
	if req.KeepWeekly <= 0 {
		req.KeepWeekly = 4
	}
	if req.KeepMonthly <= 0 {
		req.KeepMonthly = 6
	}
	if req.SFTPPort <= 0 {
		req.SFTPPort = 22
	}
	if strings.TrimSpace(req.ScheduleCron) == "" {
		req.ScheduleCron = "0 2 * * *"
	}
	cfg := backupConfig{
		Target:       req.Target,
		Endpoint:     strings.TrimSpace(req.Endpoint),
		Bucket:       strings.TrimSpace(req.Bucket),
		Region:       strings.TrimSpace(req.Region),
		AccessKey:    strings.TrimSpace(req.AccessKey),
		SecretKeyEnc: secretEnc,
		SFTPHost:     strings.TrimSpace(req.SFTPHost),
		SFTPPort:     req.SFTPPort,
		SFTPUser:     strings.TrimSpace(req.SFTPUser),
		SFTPPath:     strings.TrimSpace(req.SFTPPath),
		KeepDaily:    req.KeepDaily,
		KeepWeekly:   req.KeepWeekly,
		KeepMonthly:  req.KeepMonthly,
		ScheduleCron: strings.TrimSpace(req.ScheduleCron),
	}
	raw, _ := json.Marshal(cfg)
	if err := s.store.SetSetting(r.Context(), "backup_config", string(raw)); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "backup.config_update", "backup", "config", "success", map[string]any{"target": cfg.Target})
	WriteJSON(w, http.StatusOK, map[string]any{"status": "saved", "target": cfg.Target})
}

func (s *Server) handlePruneBackups(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	cfg := s.loadBackupConfig(r.Context())
	repoURL, repoEnv := s.buildResticArgs(r.Context(), cfg.Target)
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "backup.prune", ac.User.ID, map[string]any{
		"keep_daily":   cfg.KeepDaily,
		"keep_weekly":  cfg.KeepWeekly,
		"keep_monthly": cfg.KeepMonthly,
		"repo_url":     repoURL,
		"repo_env":     repoEnv,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "backup.prune", "backup", "retention", "success", map[string]any{
		"keep_daily": cfg.KeepDaily, "keep_weekly": cfg.KeepWeekly, "keep_monthly": cfg.KeepMonthly,
	})
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
