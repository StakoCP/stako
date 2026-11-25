// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/render"
	"github.com/StakoCP/stako/internal/store"
)

// --- Impersonation (7.16) ---

func (s *Server) handleImpersonate(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	if ac.Session == nil {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Session required", "")
		return
	}
	if ac.Impersonator != nil {
		WriteProblem(w, http.StatusConflict, "conflict", "Cannot chain impersonation", "Exit current impersonation session first")
		return
	}
	if ac.User.Role != "admin" && ac.User.Role != "reseller" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "Only admin or reseller can impersonate")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
		Reason string `json:"reason"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	target, err := s.store.GetUserByID(r.Context(), req.UserID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if target.ID == ac.User.ID || target.Role == "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Cannot impersonate this account", "")
		return
	}
	if ac.User.Role == "reseller" {
		if target.Role != "user" || target.OwnerID == nil || *target.OwnerID != ac.User.ID {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Resellers can only impersonate their own users", "")
			return
		}
		if target.Status == "suspended" {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Only admin can enter a suspended account", "")
			return
		}
	}

	impID, _ := crypto.RandomID("imp")
	imp := &store.ImpersonationSession{
		ID:       impID,
		ActorID:  ac.User.ID,
		TargetID: target.ID,
		Reason:   strings.TrimSpace(req.Reason),
		IP:       ClientIP(r),
	}
	if err := s.store.StartImpersonation(r.Context(), ac.Session.ID, imp); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "auth.impersonate.start", "user", target.ID, "success", map[string]any{
		"target_username": target.Username, "reason": imp.Reason,
	})
	WriteJSON(w, http.StatusOK, map[string]any{
		"user":         target,
		"impersonator": ac.User,
		"csrf_token":   ac.Session.CSRFToken,
	})
}

func (s *Server) handleExitImpersonate(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.Session == nil || ac.Impersonator == nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Not impersonating", "")
		return
	}
	if err := s.store.EndImpersonation(r.Context(), ac.Session.ID, ac.Session.ImpersonationID); err != nil {
		handleStoreErr(w, err)
		return
	}
	_ = s.store.AppendAudit(r.Context(), &store.AuditEntry{
		ActorID:       ac.Impersonator.ID,
		ActorUsername: ac.Impersonator.Username,
		ActingAs:      &ac.User.Username,
		Action:        "auth.impersonate.exit",
		ObjectType:    "user",
		ObjectID:      ac.User.ID,
		Result:        "success",
		IP:            ClientIP(r),
		UserAgent:     r.UserAgent(),
	})
	WriteJSON(w, http.StatusOK, map[string]any{
		"user":         ac.Impersonator,
		"impersonator": nil,
		"csrf_token":   ac.Session.CSRFToken,
	})
}

// --- IPs (7.14, 7.15) ---

func (s *Server) handleListIPs(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	items, err := s.store.ListIPs(r.Context(), ac.User.Role, ac.User.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleCreateIP(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		IP            string  `json:"ip"`
		Scope         string  `json:"scope"`
		Visibility    string  `json:"visibility"`
		InterfaceName string  `json:"interface_name"`
		OwnerID       *string `json:"owner_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	parsed := net.ParseIP(strings.TrimSpace(req.IP))
	if parsed == nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid IP address", "",
			FieldError{Field: "ip", Message: "Must be a valid IPv4 or IPv6 address"})
		return
	}
	family := "ipv6"
	if parsed.To4() != nil {
		family = "ipv4"
	}
	if req.Scope == "" {
		req.Scope = "shared"
	}
	if req.Visibility == "" {
		req.Visibility = "public"
	}
	if req.InterfaceName == "" {
		req.InterfaceName = "eth0"
	}
	id, _ := crypto.RandomID("ip")
	ip := &store.IPAddress{
		ID:            id,
		IP:            parsed.String(),
		Family:        family,
		Scope:         req.Scope,
		Visibility:    req.Visibility,
		InterfaceName: req.InterfaceName,
		OwnerID:       req.OwnerID,
	}
	if err := s.store.CreateIP(r.Context(), ip); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "ip.create", "ip", ip.ID, "success", map[string]any{"ip": ip.IP})
	WriteJSON(w, http.StatusCreated, ip)
}

func (s *Server) handleSyncIPs(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdIPList, nil)
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Agent IP sync failed", "")
		return
	}
	added := 0
	if rawList, ok := resp.Data["ips"].([]any); ok {
		for _, item := range rawList {
			m, _ := item.(map[string]any)
			ipStr, _ := m["ip"].(string)
			fam, _ := m["family"].(string)
			iface, _ := m["interface"].(string)
			if ipStr == "" {
				continue
			}
			id, _ := crypto.RandomID("ip")
			vis := "public"
			if parsed := net.ParseIP(ipStr); parsed != nil && (parsed.IsLoopback() || parsed.IsPrivate()) {
				vis = "internal"
			}
			if err := s.store.CreateIP(r.Context(), &store.IPAddress{
				ID: id, IP: ipStr, Family: fam, Scope: "shared", Visibility: vis, InterfaceName: iface,
			}); err == nil {
				added++
			}
		}
	}
	items, _ := s.store.ListIPs(r.Context(), ac.User.Role, ac.User.ID)
	s.audit(r, ac.User, "ip.sync", "ip", "server", "success", map[string]any{"added": added})
	WriteJSON(w, http.StatusOK, map[string]any{"added": added, "items": items})
}

func (s *Server) handleDeleteIP(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	id := r.PathValue("id")
	if err := s.store.DeleteIP(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "ip.delete", "ip", id, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- Domains & Proxy Rules (7.2, 7.3) ---

func (s *Server) resolveTargetUser(r *http.Request, ac *AuthContext, reqUserID *string) (*store.User, bool) {
	if reqUserID == nil || *reqUserID == "" || *reqUserID == ac.User.ID {
		return ac.User, true
	}
	target, err := s.store.GetUserByID(r.Context(), *reqUserID)
	if err != nil {
		return nil, false
	}
	if !canManageUser(ac.User, target) {
		return nil, false
	}
	return target, true
}

func (s *Server) canAccessOwned(r *http.Request, ac *AuthContext, ownerUserID string) bool {
	if ac.User.Role == "admin" || ownerUserID == ac.User.ID {
		return true
	}
	if ac.User.Role == "reseller" {
		u, err := s.store.GetUserByID(r.Context(), ownerUserID)
		return err == nil && u.OwnerID != nil && *u.OwnerID == ac.User.ID
	}
	return false
}

func (s *Server) enqueueDomainApply(r *http.Request, actorID string, d *store.Domain, owner *store.User) *string {
	rules, _ := s.store.ListProxyRules(r.Context(), d.ID)
	ruleData := make([]render.ProxyRuleData, len(rules))
	for i, pr := range rules {
		ruleData[i] = render.ProxyRuleData{
			Path: pr.Path, Upstream: pr.Upstream, WebSocket: pr.WebSocket, SSE: pr.SSE, GRPC: pr.GRPC,
		}
	}
	var v4, v6 string
	if d.IPv4ID != nil {
		if ip, err := s.store.GetIP(r.Context(), *d.IPv4ID); err == nil {
			v4 = ip.IP
		}
	}
	if d.IPv6ID != nil {
		if ip, err := s.store.GetIP(r.Context(), *d.IPv6ID); err == nil {
			v6 = ip.IP
		}
	}
	conf, _ := render.RenderNginxVhost(render.DomainVhostData{
		Username: owner.Username, Domain: d.Domain, Aliases: d.Aliases,
		Stack: d.Stack, Template: d.Template, Docroot: d.Docroot,
		PHPVersion: d.PHPVersion, SSLEnabled: d.SSLEnabled,
		ForceHTTPS: d.ForceHTTPS, HSTS: d.HSTS,
		ListenIPv4: v4, ListenIPv6: v6, ProxyTarget: d.ProxyTarget, ProxyRules: ruleData,
	})
	if j, err := s.jobs.Enqueue(r.Context(), "domain.apply", actorID, map[string]any{
		"domain":      d.Domain,
		"username":    owner.Username,
		"docroot":     d.Docroot,
		"template":    d.Template,
		"php_version": d.PHPVersion,
		"nginx_conf":  conf,
	}); err == nil {
		return &j.ID
	}
	return nil
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	limit, offset := parsePagination(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, total, err := s.store.ListDomains(r.Context(), userFilter, resellerFilter, q, limit, offset)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func validateDomainDocroot(username, domain, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Sprintf("/home/%s/%s/public_html", username, domain), nil
	}
	if strings.ContainsAny(raw, "\n\r\t ") {
		return "", fmt.Errorf("docroot must not contain spaces or control characters")
	}
	clean := path.Clean(raw)
	prefix := "/home/" + username + "/"
	if !strings.HasPrefix(clean, prefix) || len(clean) <= len(prefix) {
		return "", fmt.Errorf("docroot must be inside %s", prefix)
	}
	return clean, nil
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Read-only role cannot create domains", "")
		return
	}
	var req struct {
		Domain      string   `json:"domain"`
		UserID      *string  `json:"user_id"`
		Aliases     []string `json:"aliases"`
		Stack       string   `json:"stack"`
		Template    string   `json:"template"`
		Docroot     string   `json:"docroot"`
		PHPVersion  string   `json:"php_version"`
		SSLMode     string   `json:"ssl_mode"`
		ForceHTTPS  *bool    `json:"force_https"`
		HSTS        bool     `json:"hsts"`
		IPv4ID      *string  `json:"ipv4_id"`
		IPv6ID      *string  `json:"ipv6_id"`
		ProxyTarget string   `json:"proxy_target"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	if err := agentproto.ValidateDomain(req.Domain); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid domain", err.Error(),
			FieldError{Field: "domain", Message: err.Error()})
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	// Quota check
	if owner.PackageID != nil {
		if pkg, err := s.store.GetPackageByID(r.Context(), *owner.PackageID); err == nil && pkg.Domains >= 0 {
			if cnt, _ := s.store.CountDomainsByUser(r.Context(), owner.ID); cnt >= pkg.Domains {
				WriteProblem(w, http.StatusForbidden, "forbidden", "Domain quota exceeded", "Upgrade package to add more domains")
				return
			}
		}
	}
	if req.Stack == "" {
		req.Stack = "nginx"
	}
	if !slices.Contains([]string{"nginx", "nginx_apache", "apache"}, req.Stack) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid stack", "")
		return
	}
	if req.Template == "" {
		req.Template = "php"
	}
	if req.Template == "proxy" {
		if err := agentproto.ValidateUpstream(req.ProxyTarget); err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid proxy_target", err.Error(),
				FieldError{Field: "proxy_target", Message: err.Error()})
			return
		}
	}
	if req.PHPVersion == "" {
		req.PHPVersion = "8.4"
	}
	if req.SSLMode == "" {
		req.SSLMode = "none"
	}
	forceHTTPS := true
	if req.ForceHTTPS != nil {
		forceHTTPS = *req.ForceHTTPS
	}
	if req.Aliases == nil {
		req.Aliases = []string{}
	}
	for _, al := range req.Aliases {
		if err := agentproto.ValidateDomain(strings.ToLower(strings.TrimSpace(al))); err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid alias: "+al, err.Error())
			return
		}
	}

	docroot, err := validateDomainDocroot(owner.Username, req.Domain, req.Docroot)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid docroot", err.Error(),
			FieldError{Field: "docroot", Message: err.Error()})
		return
	}

	id, _ := crypto.RandomID("d")
	d := &store.Domain{
		ID: id, UserID: owner.ID, Domain: req.Domain, Aliases: req.Aliases,
		Stack: req.Stack, Template: req.Template,
		Docroot:    docroot,
		PHPVersion: req.PHPVersion, SSLEnabled: req.SSLMode == "letsencrypt", SSLMode: req.SSLMode,
		ForceHTTPS: forceHTTPS, HSTS: req.HSTS, IPv4ID: req.IPv4ID, IPv6ID: req.IPv6ID,
		ProxyTarget: req.ProxyTarget, Status: "active",
	}
	if err := s.store.CreateDomain(r.Context(), d); err != nil {
		handleStoreErr(w, err)
		return
	}
	jobID := s.enqueueDomainApply(r, ac.User.ID, d, owner)
	s.audit(r, ac.User, "domain.create", "domain", d.ID, "success", map[string]any{"domain": d.Domain})
	WriteJSON(w, http.StatusCreated, map[string]any{"domain": d, "job_id": jobID})
}

func (s *Server) handleGetDomain(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	d, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, d.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	WriteJSON(w, http.StatusOK, d)
}

type nullableString struct {
	Set bool
	Val string
}

func (n *nullableString) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Val = ""
		return nil
	}
	return json.Unmarshal(b, &n.Val)
}

func (s *Server) handleUpdateDomain(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	d, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, d.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Aliases     []string       `json:"aliases"`
		Stack       *string        `json:"stack"`
		Template    *string        `json:"template"`
		Docroot     *string        `json:"docroot"`
		PHPVersion  *string        `json:"php_version"`
		ForceHTTPS  *bool          `json:"force_https"`
		HSTS        *bool          `json:"hsts"`
		IPv4ID      nullableString `json:"ipv4_id"`
		IPv6ID      nullableString `json:"ipv6_id"`
		ProxyTarget *string        `json:"proxy_target"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Aliases != nil {
		d.Aliases = req.Aliases
	}
	if req.Stack != nil {
		d.Stack = *req.Stack
	}
	if req.Template != nil {
		d.Template = *req.Template
	}
	if req.PHPVersion != nil {
		d.PHPVersion = *req.PHPVersion
	}
	if req.ForceHTTPS != nil {
		d.ForceHTTPS = *req.ForceHTTPS
	}
	if req.HSTS != nil {
		d.HSTS = *req.HSTS
	}
	if req.IPv4ID.Set {
		if req.IPv4ID.Val == "" {
			d.IPv4ID = nil
		} else {
			v := req.IPv4ID.Val
			d.IPv4ID = &v
		}
	}
	if req.IPv6ID.Set {
		if req.IPv6ID.Val == "" {
			d.IPv6ID = nil
		} else {
			v := req.IPv6ID.Val
			d.IPv6ID = &v
		}
	}
	if req.ProxyTarget != nil {
		if *req.ProxyTarget != "" {
			if err := agentproto.ValidateUpstream(*req.ProxyTarget); err != nil {
				WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid proxy_target", err.Error())
				return
			}
		}
		d.ProxyTarget = *req.ProxyTarget
	}
	owner, _ := s.store.GetUserByID(r.Context(), d.UserID)
	if req.Docroot != nil {
		dr, err := validateDomainDocroot(owner.Username, d.Domain, *req.Docroot)
		if err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid docroot", err.Error(),
				FieldError{Field: "docroot", Message: err.Error()})
			return
		}
		d.Docroot = dr
	}
	if err := s.store.UpdateDomain(r.Context(), d); err != nil {
		handleStoreErr(w, err)
		return
	}
	jobID := s.enqueueDomainApply(r, ac.User.ID, d, owner)
	s.audit(r, ac.User, "domain.update", "domain", d.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"domain": d, "job_id": jobID})
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	d, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, d.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteDomain(r.Context(), d.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "domain.delete", ac.User.ID, map[string]any{"domain": d.Domain}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "domain.delete", "domain", d.ID, "success", map[string]any{"domain": d.Domain})
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleIssueSSL(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	d, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, d.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	d.SSLEnabled = true
	d.SSLMode = "letsencrypt"
	_ = s.store.UpdateDomain(r.Context(), d)
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "ssl.issue", ac.User.ID, map[string]any{"domain": d.Domain}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "domain.ssl_issue", "domain", d.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleListProxyRules(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	d, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, d.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	items, err := s.store.ListProxyRules(r.Context(), d.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateProxyRule(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	d, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, d.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Path      string `json:"path"`
		Upstream  string `json:"upstream"`
		WebSocket *bool  `json:"websocket"`
		SSE       bool   `json:"sse"`
		GRPC      bool   `json:"grpc"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if err := agentproto.ValidateURLPath(req.Path); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid path", err.Error(),
			FieldError{Field: "path", Message: err.Error()})
		return
	}
	if err := agentproto.ValidateUpstream(req.Upstream); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid upstream", err.Error(),
			FieldError{Field: "upstream", Message: err.Error()})
		return
	}
	ws := true
	if req.WebSocket != nil {
		ws = *req.WebSocket
	}
	id, _ := crypto.RandomID("pr")
	pr := &store.ProxyRule{
		ID: id, DomainID: d.ID, Path: req.Path, Upstream: req.Upstream,
		WebSocket: ws, SSE: req.SSE, GRPC: req.GRPC,
	}
	if err := s.store.CreateProxyRule(r.Context(), pr); err != nil {
		handleStoreErr(w, err)
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), d.UserID)
	s.enqueueDomainApply(r, ac.User.ID, d, owner)
	s.audit(r, ac.User, "proxy_rule.create", "proxy_rule", pr.ID, "success", map[string]any{"domain": d.Domain, "path": pr.Path})
	WriteJSON(w, http.StatusCreated, pr)
}

func (s *Server) handleDeleteProxyRule(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	d, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, d.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteProxyRule(r.Context(), d.ID, r.PathValue("ruleId")); err != nil {
		handleStoreErr(w, err)
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), d.UserID)
	s.enqueueDomainApply(r, ac.User.ID, d, owner)
	w.WriteHeader(http.StatusNoContent)
}

// --- Apps / Process Manager (7.3) ---

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	limit, offset := parsePagination(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, total, err := s.store.ListApps(r.Context(), userFilter, resellerFilter, limit, offset)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Name           string            `json:"name"`
		UserID         *string           `json:"user_id"`
		DomainID       *string           `json:"domain_id"`
		Runtime        string            `json:"runtime"`
		RuntimeVersion string            `json:"runtime_version"`
		WorkDir        string            `json:"work_dir"`
		Command        string            `json:"command"`
		Port           int               `json:"port"`
		Env            map[string]string `json:"env"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || strings.ContainsAny(req.Name, " \n\r/") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid app name", "",
			FieldError{Field: "name", Message: "App name must not be empty or contain spaces"})
		return
	}
	if !slices.Contains([]string{"node", "go", "python"}, req.Runtime) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid runtime", "Must be node, go, or python")
		return
	}
	if strings.TrimSpace(req.Command) == "" || strings.ContainsAny(req.Command, "\n\r") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid command", "")
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	port := req.Port
	if port == 0 {
		var err error
		port, err = s.store.NextFreeAppPort(r.Context())
		if err != nil {
			WriteProblem(w, http.StatusConflict, "conflict", "No free port", err.Error())
			return
		}
	} else if port < 1025 || port > 65535 {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Port must be 1025-65535", "")
		return
	}
	if req.WorkDir == "" {
		req.WorkDir = fmt.Sprintf("/home/%s/apps/%s", owner.Username, req.Name)
	}
	envKeys, envEnc := s.encryptEnvMap(req.Env)
	id, _ := crypto.RandomID("a")
	a := &store.App{
		ID: id, UserID: owner.ID, DomainID: req.DomainID, Name: req.Name,
		Runtime: req.Runtime, RuntimeVersion: req.RuntimeVersion,
		WorkDir: req.WorkDir, Command: req.Command, Port: port,
		EnvKeys: envKeys, EnvEnc: envEnc, Status: "running",
		UnitName: "stako-app-" + id,
	}
	if err := s.store.CreateApp(r.Context(), a); err != nil {
		handleStoreErr(w, err)
		return
	}
	unitBody, _ := render.RenderAppSystemdUnit(render.AppUnitData{
		ID: a.ID, Name: a.Name, Username: owner.Username, WorkDir: a.WorkDir, Port: a.Port, Command: a.Command,
	})
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "app.deploy", ac.User.ID, map[string]any{
		"unit_name": a.UnitName, "unit_body": unitBody,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "app.create", "app", a.ID, "success", map[string]any{"name": a.Name, "port": a.Port})
	WriteJSON(w, http.StatusCreated, map[string]any{"app": a, "job_id": jobID})
}

func (s *Server) encryptEnvMap(env map[string]string) ([]string, string) {
	if len(env) == 0 {
		return []string{}, ""
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	raw, _ := json.Marshal(env)
	enc, _ := s.enc.Encrypt(string(raw))
	return keys, enc
}

func (s *Server) decryptEnvMap(enc string) (map[string]string, error) {
	if enc == "" {
		return nil, nil
	}
	raw, err := s.enc.Decrypt(enc)
	if err != nil {
		return nil, err
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, a.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	WriteJSON(w, http.StatusOK, a)
}

func (s *Server) handleGetAppEnv(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, a.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	env := map[string]string{}
	if a.EnvEnc != "" {
		if pt, err := s.enc.Decrypt(a.EnvEnc); err == nil {
			_ = json.Unmarshal([]byte(pt), &env)
		}
	}
	s.audit(r, ac.User, "app.env_reveal", "app", a.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"env": env})
}

func (s *Server) handleUpdateApp(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, a.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Command        *string           `json:"command"`
		RuntimeVersion *string           `json:"runtime_version"`
		Port           *int              `json:"port"`
		DomainID       *string           `json:"domain_id"`
		Env            map[string]string `json:"env"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Command != nil {
		a.Command = *req.Command
	}
	if req.RuntimeVersion != nil {
		a.RuntimeVersion = *req.RuntimeVersion
	}
	if req.Port != nil && *req.Port >= 1025 && *req.Port <= 65535 {
		a.Port = *req.Port
	}
	if req.DomainID != nil {
		if *req.DomainID == "" {
			a.DomainID = nil
		} else {
			a.DomainID = req.DomainID
		}
	}
	if req.Env != nil {
		a.EnvKeys, a.EnvEnc = s.encryptEnvMap(req.Env)
	}
	if err := s.store.UpdateApp(r.Context(), a); err != nil {
		handleStoreErr(w, err)
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), a.UserID)
	unitBody, _ := render.RenderAppSystemdUnit(render.AppUnitData{
		ID: a.ID, Name: a.Name, Username: owner.Username, WorkDir: a.WorkDir, Port: a.Port, Command: a.Command,
	})
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "app.deploy", ac.User.ID, map[string]any{
		"unit_name": a.UnitName, "unit_body": unitBody,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "app.update", "app", a.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"app": a, "job_id": jobID})
}

func (s *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, a.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteApp(r.Context(), a.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "app.delete", ac.User.ID, map[string]any{
		"unit_name": a.UnitName, "action": "delete",
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "app.delete", "app", a.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleAppAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ac := GetAuth(r)
		if ac.User.Role == "readonly" {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
			return
		}
		a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		if !s.canAccessOwned(r, ac, a.UserID) {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
			return
		}
		if action == "stop" {
			a.Status = "stopped"
		} else {
			a.Status = "running"
		}
		_ = s.store.UpdateApp(r.Context(), a)
		var jobID *string
		if j, err := s.jobs.Enqueue(r.Context(), "app."+action, ac.User.ID, map[string]any{
			"unit_name": a.UnitName, "action": action,
		}); err == nil {
			jobID = &j.ID
		}
		s.audit(r, ac.User, "app."+action, "app", a.ID, "success", nil)
		WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
	}
}

func (s *Server) handleGetAppLogs(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	a, err := s.store.GetApp(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, a.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdAppLogs, map[string]any{"unit_name": a.UnitName})
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"logs": "Error fetching logs: " + err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"logs": resp.Output})
}
