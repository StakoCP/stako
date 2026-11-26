// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

// --- WAF (7.10 FR-X5) ---

type wafConfig struct {
	Enabled       bool   `json:"enabled"`
	Engine        string `json:"engine"`
	Mode          string `json:"mode"`
	ParanoiaLevel int    `json:"paranoia_level"`
}

func (s *Server) handleGetWAF(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	cfg := wafConfig{Enabled: false, Engine: "coraza", Mode: "detection", ParanoiaLevel: 1}
	if raw, _ := s.store.GetSetting(r.Context(), "waf_config"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	WriteJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSetWAF(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var cfg wafConfig
	if err := DecodeJSON(r, &cfg); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if cfg.Engine == "" {
		cfg.Engine = "coraza"
	}
	if cfg.Engine != "coraza" && cfg.Engine != "modsecurity" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Engine must be coraza or modsecurity", "")
		return
	}
	if cfg.Mode == "" {
		cfg.Mode = "detection"
	}
	if cfg.Mode != "detection" && cfg.Mode != "blocking" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Mode must be detection or blocking", "")
		return
	}
	if cfg.ParanoiaLevel < 1 || cfg.ParanoiaLevel > 4 {
		cfg.ParanoiaLevel = 1
	}
	b, _ := json.Marshal(cfg)
	_ = s.store.SetSetting(r.Context(), "waf_config", string(b))

	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "waf.apply", ac.User.ID, map[string]any{
		"enabled": cfg.Enabled, "engine": cfg.Engine, "mode": cfg.Mode, "paranoia_level": cfg.ParanoiaLevel,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "waf.update", "waf", cfg.Engine, "success", map[string]any{"enabled": cfg.Enabled, "mode": cfg.Mode})
	WriteJSON(w, http.StatusOK, map[string]any{"waf": cfg, "job_id": jobID})
}

// --- Git Deploy (7.3 FR-P8) ---

func (s *Server) handleListGitDeploys(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListGitDeploys(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateGitDeploy(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		DomainID string  `json:"domain_id"`
		RepoURL  string  `json:"repo_url"`
		Branch   string  `json:"branch"`
		BuildCmd string  `json:"build_cmd"`
		UserID   *string `json:"user_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if err := validateWebhookURL(req.RepoURL); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Blocked or invalid repo_url", err.Error())
		return
	}
	dom, err := s.store.GetDomain(r.Context(), req.DomainID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, dom.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden domain", "")
		return
	}
	if req.Branch == "" {
		req.Branch = "main"
	}
	secret, _ := crypto.RandomHex(24)
	secEnc, _ := s.enc.Encrypt(secret)
	id, _ := crypto.RandomID("gd")
	gd := &store.GitDeploy{
		ID: id, UserID: dom.UserID, DomainID: dom.ID,
		RepoURL: req.RepoURL, Branch: req.Branch, BuildCmd: req.BuildCmd,
	}
	if err := s.store.CreateGitDeploy(r.Context(), gd, secEnc); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "git_deploy.create", "git_deploy", gd.ID, "success", map[string]any{"repo_url": gd.RepoURL})
	WriteJSON(w, http.StatusCreated, map[string]any{"deploy": gd, "webhook_secret": secret})
}

func (s *Server) handleTriggerGitDeploy(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	gd, err := s.store.GetGitDeploy(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, gd.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	s.store.TouchGitDeploy(r.Context(), gd.ID)
	owner, _ := s.store.GetUserByID(r.Context(), gd.UserID)
	targetDir := "/home/" + owner.Username + "/git/" + gd.ID
	if gd.DomainID != "" {
		if dom, err := s.store.GetDomain(r.Context(), gd.DomainID); err == nil && dom.Docroot != "" {
			targetDir = dom.Docroot
		}
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "git.deploy", ac.User.ID, map[string]any{
		"repo_url":   gd.RepoURL,
		"branch":     gd.Branch,
		"build_cmd":  gd.BuildCmd,
		"username":   owner.Username,
		"target_dir": targetDir,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "git_deploy.trigger", "git_deploy", gd.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleDeleteGitDeploy(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	gd, err := s.store.GetGitDeploy(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, gd.UserID) || ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteGitDeploy(r.Context(), gd.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Multi-Server Nodes & Panel Import (7.13) ---

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	items, err := s.store.ListServerNodes(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Name string `json:"name"`
		Host string `json:"host"`
		Port int    `json:"port"`
		Role string `json:"role"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Host) == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Name and host required", "")
		return
	}
	if req.Port <= 0 {
		req.Port = 8084
	}
	if req.Role == "" {
		req.Role = "worker"
	}
	if !slices.Contains([]string{"worker", "db", "mail"}, req.Role) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Role must be worker, db, or mail", "")
		return
	}
	rawTok, _ := crypto.RandomHex(24)
	agentToken := "stn_" + rawTok
	id, _ := crypto.RandomID("nd")
	n := &store.ServerNode{
		ID: id, Name: strings.TrimSpace(req.Name), Host: strings.TrimSpace(req.Host),
		Port: req.Port, Role: req.Role, Status: "online",
	}
	if err := s.store.CreateServerNode(r.Context(), n, crypto.SHA256Hex(agentToken)); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "node.create", "node", n.ID, "success", map[string]any{"name": n.Name, "host": n.Host})
	WriteJSON(w, http.StatusCreated, map[string]any{"node": n, "agent_token": agentToken})
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	if err := s.store.DeleteServerNode(r.Context(), r.PathValue("id")); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "node.delete", "node", r.PathValue("id"), "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePanelImport(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		SourcePanel string `json:"source_panel"`
		ArchivePath string `json:"archive_path"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if !slices.Contains([]string{"hestiacp", "cpanel", "vestacp"}, req.SourcePanel) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Source must be hestiacp, cpanel, or vestacp", "")
		return
	}
	if strings.TrimSpace(req.ArchivePath) == "" || strings.Contains(req.ArchivePath, "..") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid archive_path", "")
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "panel.import", ac.User.ID, map[string]any{
		"source_panel": req.SourcePanel, "archive_path": req.ArchivePath,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "panel.import", "system", req.SourcePanel, "success", map[string]any{"archive": req.ArchivePath})
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}
