// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

// --- Databases (7.5) ---

func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
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
	items, total, err := s.store.ListDatabases(r.Context(), userFilter, resellerFilter, limit, offset)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Name        string   `json:"name"`
		Engine      string   `json:"engine"`
		UserID      *string  `json:"user_id"`
		DBUser      string   `json:"db_user"`
		DBPassword  string   `json:"db_password"`
		RemoteHosts []string `json:"remote_hosts"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Engine != "mariadb" && req.Engine != "postgres" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid engine", "Must be mariadb or postgres")
		return
	}
	if err := crypto.ValidatePasswordStrength(req.DBPassword); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Weak db_password", err.Error(),
			FieldError{Field: "db_password", Message: err.Error()})
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	prefix := strings.ReplaceAll(owner.Username, "-", "_") + "_"
	req.Name = strings.TrimSpace(req.Name)
	if req.Name != "" && !strings.HasPrefix(req.Name, prefix) {
		req.Name = prefix + req.Name
	}
	if err := agentproto.ValidateDBIdent(req.Name); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid database name", err.Error(),
			FieldError{Field: "name", Message: err.Error()})
		return
	}
	req.DBUser = strings.TrimSpace(req.DBUser)
	if req.DBUser == "" {
		req.DBUser = req.Name
	} else if !strings.HasPrefix(req.DBUser, prefix) {
		req.DBUser = prefix + req.DBUser
	}
	if err := agentproto.ValidateDBIdent(req.DBUser); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid db_user", err.Error())
		return
	}
	if len(req.RemoteHosts) == 0 {
		req.RemoteHosts = []string{"127.0.0.1"}
	}
	id, _ := crypto.RandomID("db")
	db := &store.Database{
		ID: id, UserID: owner.ID, Name: req.Name, Engine: req.Engine,
		DBUser: req.DBUser, RemoteHosts: req.RemoteHosts,
	}
	if err := s.store.CreateDatabase(r.Context(), db); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "db.create", ac.User.ID, map[string]any{
		"engine":       db.Engine,
		"name":         db.Name,
		"db_user":      db.DBUser,
		"db_password":  req.DBPassword,
		"remote_hosts": db.RemoteHosts,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "db.create", "database", db.ID, "success", map[string]any{"name": db.Name, "engine": db.Engine})
	WriteJSON(w, http.StatusCreated, map[string]any{"database": db, "job_id": jobID})
}

func (s *Server) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	db, err := s.store.GetDatabase(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, db.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteDatabase(r.Context(), db.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "db.delete", ac.User.ID, map[string]any{
		"engine": db.Engine, "name": db.Name,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "db.delete", "database", db.ID, "success", map[string]any{"name": db.Name})
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleBackupDatabase(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	db, err := s.store.GetDatabase(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, db.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "db.backup", ac.User.ID, map[string]any{
		"engine": db.Engine, "name": db.Name,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "db.backup", "database", db.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleRestoreDatabase(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	db, err := s.store.GetDatabase(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, db.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		BackupPath string `json:"backup_path"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.BackupPath = strings.TrimSpace(req.BackupPath)
	if req.BackupPath == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "backup_path is required", "")
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "db.restore", ac.User.ID, map[string]any{
		"engine": db.Engine, "name": db.Name, "backup_path": req.BackupPath,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "db.restore", "database", db.ID, "success", map[string]any{"backup_path": req.BackupPath})
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleRotateDBPassword(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	db, err := s.store.GetDatabase(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, db.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if r.Body != nil {
		_ = DecodeJSON(r, &req)
	}
	newPass := strings.TrimSpace(req.Password)
	if newPass == "" {
		gen, err := crypto.RandomID("pw")
		if err != nil {
			WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to generate password", "")
			return
		}
		newPass = gen + "A1!"
	} else if err := crypto.ValidatePasswordStrength(newPass); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Weak password", err.Error())
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "db.rotate_password", ac.User.ID, map[string]any{
		"engine": db.Engine, "name": db.Name, "username": db.DBUser, "password": newPass,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "db.rotate_password", "database", db.ID, "success", map[string]any{"username": db.DBUser})
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID, "username": db.DBUser, "password": newPass})
}

// --- FTP / SFTP (7.7) ---

func (s *Server) handleListFTP(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListFtpAccounts(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleCreateFTP(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Username string  `json:"username"`
		Password string  `json:"password"`
		UserID   *string `json:"user_id"`
		HomeDir  string  `json:"home_dir"`
		QuotaMB  int     `json:"quota_mb"`
		Protocol string  `json:"protocol"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if err := agentproto.ValidateLinuxUsername(req.Username); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid username", err.Error(),
			FieldError{Field: "username", Message: err.Error()})
		return
	}
	if err := crypto.ValidatePasswordStrength(req.Password); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Weak password", err.Error(),
			FieldError{Field: "password", Message: err.Error()})
		return
	}
	if req.Protocol == "" {
		req.Protocol = "sftp"
	}
	if req.Protocol != "ftps" && req.Protocol != "sftp" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Only ftps or sftp allowed (plain FTP disabled)", "")
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	if req.HomeDir == "" {
		req.HomeDir = fmt.Sprintf("/home/%s/web", owner.Username)
	}
	if req.QuotaMB == 0 {
		req.QuotaMB = 1024
	}
	id, _ := crypto.RandomID("ftp")
	f := &store.FtpAccount{
		ID: id, UserID: owner.ID, Username: req.Username,
		HomeDir: req.HomeDir, QuotaMB: req.QuotaMB, Protocol: req.Protocol,
	}
	if err := s.store.CreateFtpAccount(r.Context(), f); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "ftp.create", ac.User.ID, map[string]any{
		"username": f.Username, "password": req.Password, "home_dir": f.HomeDir, "protocol": f.Protocol, "quota_mb": f.QuotaMB,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "ftp.create", "ftp", f.ID, "success", map[string]any{"username": f.Username})
	WriteJSON(w, http.StatusCreated, map[string]any{"ftp": f, "job_id": jobID})
}

func (s *Server) handleUpdateFTP(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	f, err := s.store.GetFtpAccount(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, f.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Password string `json:"password"`
		HomeDir  string `json:"home_dir"`
		QuotaMB  int    `json:"quota_mb"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Password != "" {
		if err := crypto.ValidatePasswordStrength(req.Password); err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Weak password", err.Error(),
				FieldError{Field: "password", Message: err.Error()})
			return
		}
	}
	homeDir := strings.TrimSpace(req.HomeDir)
	if homeDir == "" {
		homeDir = f.HomeDir
	}
	quotaMB := req.QuotaMB
	if quotaMB <= 0 {
		quotaMB = f.QuotaMB
	}
	if err := s.store.UpdateFtpAccount(r.Context(), f.ID, homeDir, quotaMB); err != nil {
		handleStoreErr(w, err)
		return
	}
	f.HomeDir = homeDir
	f.QuotaMB = quotaMB
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "ftp.update", ac.User.ID, map[string]any{
		"username": f.Username, "password": req.Password, "home_dir": f.HomeDir, "protocol": f.Protocol, "quota_mb": f.QuotaMB,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "ftp.update", "ftp", f.ID, "success", map[string]any{"username": f.Username})
	WriteJSON(w, http.StatusOK, map[string]any{"ftp": f, "job_id": jobID})
}

func (s *Server) handleDeleteFTP(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	f, err := s.store.GetFtpAccount(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, f.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteFtpAccount(r.Context(), f.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "ftp.delete", ac.User.ID, map[string]any{"username": f.Username}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "ftp.delete", "ftp", f.ID, "success", map[string]any{"username": f.Username})
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

// --- SSH Keys (7.8) ---

func (s *Server) syncUserSSHKeys(ctx context.Context, user *store.User) {
	if s.agent == nil || user == nil {
		return
	}
	keys, err := s.store.ListSSHKeys(ctx, &user.ID)
	if err != nil {
		return
	}
	pubKeys := make([]string, 0, len(keys))
	for _, k := range keys {
		pubKeys = append(pubKeys, k.PublicKey)
	}
	_, _ = s.agent.Call(ctx, agentproto.CmdSSHKeySync, map[string]any{
		"username": user.Username,
		"keys":     pubKeys,
	})
}

func (s *Server) handleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter *string
	if ac.User.Role != "admin" {
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListSSHKeys(r.Context(), userFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateSSHKey(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Label     string  `json:"label"`
		PublicKey string  `json:"public_key"`
		UserID    *string `json:"user_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	req.PublicKey = strings.TrimSpace(req.PublicKey)
	if req.Label == "" || req.PublicKey == "" || strings.ContainsAny(req.PublicKey, "\r\n") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "label and single-line public_key are required", "")
		return
	}
	parts := strings.Fields(req.PublicKey)
	if len(parts) < 2 || (!strings.HasPrefix(parts[0], "ssh-") && !strings.HasPrefix(parts[0], "ecdsa-")) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid OpenSSH public key format", "")
		return
	}
	rawKey, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid base64 in public key", "")
		return
	}
	sum := sha256.Sum256(rawKey)
	fp := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])

	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	id, _ := crypto.RandomID("ssh")
	k := &store.SSHKey{
		ID: id, UserID: owner.ID, Label: req.Label, PublicKey: req.PublicKey, Fingerprint: fp,
	}
	if err := s.store.CreateSSHKey(r.Context(), k); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncUserSSHKeys(r.Context(), owner)
	s.audit(r, ac.User, "ssh_key.create", "ssh_key", k.ID, "success", map[string]any{"label": k.Label, "fingerprint": k.Fingerprint})
	WriteJSON(w, http.StatusCreated, k)
}

func (s *Server) handleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	k, err := s.store.GetSSHKey(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, k.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteSSHKey(r.Context(), k.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	if owner, err := s.store.GetUserByID(r.Context(), k.UserID); err == nil {
		s.syncUserSSHKeys(r.Context(), owner)
	}
	s.audit(r, ac.User, "ssh_key.delete", "ssh_key", k.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

// --- Firewall & Bans (7.10) ---

func (s *Server) renderNftables(ctx context.Context, rules []store.FirewallRule) string {
	blockEgress25, _ := s.store.GetSetting(ctx, "firewall_block_egress_25")
	var b strings.Builder
	b.WriteString("table inet stako {\n  chain input {\n    type filter hook input priority 0; policy drop;\n    iif lo accept\n    ct state established,related accept\n    icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-advert, nd-redirect } accept\n    ip protocol icmp icmp type echo-request limit rate 10/second accept\n    ip6 nexthdr icmpv6 icmpv6 type echo-request limit rate 10/second accept\n")
	for _, r := range rules {
		act := "accept"
		if r.Action == "deny" {
			act = "drop"
		}
		addrKey := "ip saddr"
		if strings.Contains(r.SourceCIDR, ":") {
			addrKey = "ip6 saddr"
		}
		if r.Protocol == "any" {
			b.WriteString(fmt.Sprintf("    %s %s %s\n", addrKey, r.SourceCIDR, act))
		} else {
			b.WriteString(fmt.Sprintf("    %s dport %s %s %s %s\n", r.Protocol, r.Port, addrKey, r.SourceCIDR, act))
		}
	}
	b.WriteString("  }\n  chain output {\n    type filter hook output priority 0; policy accept;\n")
	if blockEgress25 == "true" {
		b.WriteString("    tcp dport 25 skuid != 0 skuid != postfix drop\n")
	}
	b.WriteString("  }\n}\n")
	return b.String()
}

func (s *Server) handleListFirewallRules(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	items, err := s.store.ListFirewallRules(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleCreateFirewallRule(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Action     string `json:"action"`
		Protocol   string `json:"protocol"`
		Port       string `json:"port"`
		SourceCIDR string `json:"source_cidr"`
		Comment    string `json:"comment"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Action != "allow" && req.Action != "deny" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Action must be allow or deny", "")
		return
	}
	if !slices.Contains([]string{"tcp", "udp", "any"}, req.Protocol) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Protocol must be tcp, udp, or any", "")
		return
	}
	if err := agentproto.ValidatePortSpec(req.Port); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid port", err.Error())
		return
	}
	if req.SourceCIDR == "" {
		req.SourceCIDR = "0.0.0.0/0"
	}
	if _, _, err := net.ParseCIDR(req.SourceCIDR); err != nil {
		if net.ParseIP(req.SourceCIDR) == nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid source_cidr", "")
			return
		}
	}
	id, _ := crypto.RandomID("fw")
	rule := &store.FirewallRule{
		ID: id, Action: req.Action, Protocol: req.Protocol,
		Port: req.Port, SourceCIDR: req.SourceCIDR, Comment: req.Comment,
	}
	if err := s.store.CreateFirewallRule(r.Context(), rule); err != nil {
		handleStoreErr(w, err)
		return
	}
	rules, _ := s.store.ListFirewallRules(r.Context())
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "firewall.apply", ac.User.ID, map[string]any{"ruleset": s.renderNftables(r.Context(), rules)}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "firewall.rule_create", "firewall", rule.ID, "success", map[string]any{"port": rule.Port, "action": rule.Action})
	WriteJSON(w, http.StatusCreated, map[string]any{"rule": rule, "job_id": jobID})
}

func (s *Server) handleDeleteFirewallRule(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	id := r.PathValue("id")
	if err := s.store.DeleteFirewallRule(r.Context(), id); err != nil {
		handleStoreErr(w, err)
		return
	}
	rules, _ := s.store.ListFirewallRules(r.Context())
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "firewall.apply", ac.User.ID, map[string]any{"ruleset": s.renderNftables(r.Context(), rules)}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "firewall.rule_delete", "firewall", id, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleGetFirewallEgress25(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	v, _ := s.store.GetSetting(r.Context(), "firewall_block_egress_25")
	WriteJSON(w, http.StatusOK, map[string]any{"block_egress_25": v == "true"})
}

func (s *Server) handleSetFirewallEgress25(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		BlockEgress25 bool `json:"block_egress_25"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	val := "false"
	if req.BlockEgress25 {
		val = "true"
	}
	_ = s.store.SetSetting(r.Context(), "firewall_block_egress_25", val)
	rules, _ := s.store.ListFirewallRules(r.Context())
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "firewall.apply", ac.User.ID, map[string]any{"ruleset": s.renderNftables(r.Context(), rules)}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "firewall.egress25_update", "firewall", "egress25", "success", map[string]any{"enabled": req.BlockEgress25})
	WriteJSON(w, http.StatusOK, map[string]any{"block_egress_25": req.BlockEgress25, "job_id": jobID})
}

func (s *Server) handleListFirewallBans(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFirewallBans, nil)
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	if bans, ok := resp.Data["bans"]; ok {
		WriteJSON(w, http.StatusOK, map[string]any{"items": bans})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}})
}

func (s *Server) handleUnbanIP(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		IP   string `json:"ip"`
		Jail string `json:"jail"`
	}
	if err := DecodeJSON(r, &req); err != nil || net.ParseIP(req.IP) == nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid IP", "")
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "firewall.unban", ac.User.ID, map[string]any{"ip": req.IP, "jail": req.Jail}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "firewall.unban", "ip", req.IP, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

// --- Security Check, Updates & CVE Scanner (7.18, 9.3) ---

func computeSecurityScore(items []store.SecurityFinding) int {
	score := 100
	for _, f := range items {
		if f.Status != "open" {
			continue
		}
		switch f.Severity {
		case "critical":
			score -= 25
		case "high":
			score -= 15
		case "medium":
			score -= 8
		case "low":
			score -= 3
		}
	}
	return max(score, 0)
}

func (s *Server) handleListSecurityFindings(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	items, err := s.store.ListSecurityFindings(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"score": computeSecurityScore(items),
		"items": items,
		"total": len(items),
	})
}

func (s *Server) handleRunSecurityScan(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	// Built-in panel posture checks
	users, _, _, _ := s.store.Counts(r.Context())
	if !ac.User.TOTPEnabled {
		_ = s.store.UpsertSecurityFinding(r.Context(), &store.SecurityFinding{
			ID: "sf_admin_2fa", Category: "hardening", Severity: "high",
			Title: "Admin 2FA is not enabled", Description: "Admin account lacks TOTP 2FA.",
			Remediation: "Enable 2FA in Account settings (SEC-A2).", Status: "open", Component: "stako-core",
		})
	} else {
		_ = s.store.UpsertSecurityFinding(r.Context(), &store.SecurityFinding{
			ID: "sf_admin_2fa", Category: "hardening", Severity: "high",
			Title: "Admin 2FA enabled", Description: "Admin account has TOTP 2FA active.",
			Remediation: "None", Status: "resolved", Component: "stako-core",
		})
	}
	if users > 0 && s.insecureHTTP {
		_ = s.store.UpsertSecurityFinding(r.Context(), &store.SecurityFinding{
			ID: "sf_panel_tls", Category: "tls", Severity: "high",
			Title: "Panel running without TLS", Description: "stako-core is running with --insecure-http.",
			Remediation: "Run stako-core with TLS enabled (SEC-A15).", Status: "open", Component: "stako-core",
		})
	} else {
		_ = s.store.UpsertSecurityFinding(r.Context(), &store.SecurityFinding{
			ID: "sf_panel_tls", Category: "tls", Severity: "high",
			Title: "Panel TLS enabled", Description: "stako-core is serving HTTPS with TLS.",
			Remediation: "None", Status: "resolved", Component: "stako-core",
		})
	}

	if resp, err := s.agent.Call(r.Context(), agentproto.CmdSecurityScan, nil); err == nil && resp.OK {
		if rawList, ok := resp.Data["findings"].([]any); ok {
			for _, item := range rawList {
				m, _ := item.(map[string]any)
				id, _ := m["id"].(string)
				if id == "" {
					continue
				}
				cat, _ := m["category"].(string)
				sev, _ := m["severity"].(string)
				title, _ := m["title"].(string)
				desc, _ := m["description"].(string)
				rem, _ := m["remediation"].(string)
				comp, _ := m["component"].(string)
				st, _ := m["status"].(string)
				if st == "" {
					st = "open"
				}
				_ = s.store.UpsertSecurityFinding(r.Context(), &store.SecurityFinding{
					ID: id, Category: cat, Severity: sev, Title: title,
					Description: desc, Remediation: rem, Status: st, Component: comp,
				})
			}
		}
	}

	items, _ := s.store.ListSecurityFindings(r.Context())
	s.audit(r, ac.User, "security.scan", "system", "server", "success", map[string]any{"findings": len(items)})
	WriteJSON(w, http.StatusOK, map[string]any{
		"score": computeSecurityScore(items),
		"items": items,
	})
}

func (s *Server) handleUpdateSecurityFinding(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := DecodeJSON(r, &req); err != nil || !slices.Contains([]string{"open", "resolved", "accepted"}, req.Status) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Status must be open, resolved, or accepted", "")
		return
	}
	f, err := s.store.UpdateSecurityFindingStatus(r.Context(), r.PathValue("id"), req.Status)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "security.finding_update", "security_finding", f.ID, "success", map[string]any{"status": f.Status})
	WriteJSON(w, http.StatusOK, f)
}

func (s *Server) handleListUpdates(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdUpdatesList, nil)
	if err != nil || !resp.OK {
		WriteJSON(w, http.StatusOK, map[string]any{"reboot_required": false, "items": []any{}})
		return
	}
	reboot, _ := resp.Data["reboot_required"].(bool)
	updates, ok := resp.Data["updates"]
	if !ok || updates == nil {
		updates = []any{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"reboot_required": reboot, "items": updates})
}

func (s *Server) handleApplyUpdates(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Mode   string `json:"mode"`
		DryRun bool   `json:"dry_run"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Mode == "" {
		req.Mode = "security"
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "updates.apply", ac.User.ID, map[string]any{
		"mode": req.Mode, "dry_run": req.DryRun,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "updates.apply", "system", "apt", "success", map[string]any{"mode": req.Mode, "dry_run": req.DryRun})
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}
