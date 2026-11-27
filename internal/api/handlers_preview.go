// SPDX-License-Identifier: MIT
package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

var previewSlugRe = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizePreviewLabel(branch string, prNumber int) string {
	if prNumber > 0 {
		return fmt.Sprintf("pr-%d", prNumber)
	}
	s := strings.ToLower(strings.TrimSpace(branch))
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = previewSlugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "preview"
	}
	if len(s) > 32 {
		s = strings.TrimRight(s[:32], "-")
	}
	return s
}

func (s *Server) handleListGitPreviews(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	gitDeployID := r.URL.Query().Get("git_deploy_id")
	items, err := s.store.ListGitPreviews(r.Context(), userFilter, resellerFilter, gitDeployID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateGitPreview(w http.ResponseWriter, r *http.Request) {
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
	parentDom, err := s.store.GetDomain(r.Context(), gd.DomainID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}

	var req struct {
		Branch        string `json:"branch"`
		PRNumber      int    `json:"pr_number"`
		Image         string `json:"image"`
		ContainerPort int    `json:"container_port"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Branch = strings.TrimSpace(req.Branch)
	if req.Branch == "" {
		req.Branch = gd.Branch
	}
	if req.ContainerPort <= 0 || req.ContainerPort > 65535 {
		req.ContainerPort = 3000
	}
	req.Image = strings.TrimSpace(req.Image)
	if req.Image == "" {
		req.Image = "node:20-alpine"
	}

	label := sanitizePreviewLabel(req.Branch, req.PRNumber)
	subdomain := label + "." + parentDom.Domain
	if err := agentproto.ValidateDomain(subdomain); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid preview subdomain", err.Error())
		return
	}

	hostPort, err := s.store.AllocatePreviewPort(r.Context())
	if err != nil {
		WriteProblem(w, http.StatusServiceUnavailable, "capacity", err.Error(), "")
		return
	}

	prevID, _ := crypto.RandomID("prev")
	domID, _ := crypto.RandomID("d")
	containerName := "stako-" + prevID
	proxyTarget := fmt.Sprintf("127.0.0.1:%d", hostPort)

	owner, _ := s.store.GetUserByID(r.Context(), gd.UserID)
	username := ""
	if owner != nil {
		username = owner.Username
	}

	// Provision reverse-proxy subdomain pointing to the preview container port
	prevDom := &store.Domain{
		ID:          domID,
		UserID:      gd.UserID,
		Domain:      subdomain,
		Stack:       "proxy",
		Template:    "proxy",
		Docroot:     "/home/" + username + "/previews/" + prevID,
		SSLMode:     "selfsigned",
		ProxyTarget: proxyTarget,
		Status:      "active",
	}
	if err := s.store.CreateDomain(r.Context(), prevDom); err != nil {
		handleStoreErr(w, err)
		return
	}

	p := &store.GitPreview{
		ID:              prevID,
		GitDeployID:     gd.ID,
		UserID:          gd.UserID,
		DomainID:        parentDom.ID,
		Subdomain:       subdomain,
		Branch:          req.Branch,
		PRNumber:        req.PRNumber,
		Image:           req.Image,
		ContainerName:   containerName,
		HostPort:        hostPort,
		ContainerPort:   req.ContainerPort,
		PreviewDomainID: prevDom.ID,
		Status:          "running",
	}
	if err := s.store.CreateGitPreview(r.Context(), p); err != nil {
		_ = s.store.DeleteDomain(r.Context(), prevDom.ID)
		handleStoreErr(w, err)
		return
	}

	if owner != nil {
		_ = s.enqueueDomainApply(r, ac.User.ID, prevDom, owner)
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "git.preview_deploy", ac.User.ID, map[string]any{
		"preview_id":     p.ID,
		"repo_url":       gd.RepoURL,
		"branch":         p.Branch,
		"username":       username,
		"container_name": p.ContainerName,
		"image":          p.Image,
		"build_cmd":      gd.BuildCmd,
		"host_port":      p.HostPort,
		"container_port": p.ContainerPort,
	}); err == nil {
		jobID = &j.ID
	}
	s.store.TouchGitPreview(r.Context(), p.ID, "running")
	s.audit(r, ac.User, "git_preview.create", "git_preview", p.ID, "success", map[string]any{
		"subdomain": p.Subdomain,
		"branch":    p.Branch,
		"pr_number": p.PRNumber,
	})
	WriteJSON(w, http.StatusCreated, map[string]any{"preview": p, "job_id": jobID})
}

func (s *Server) handleRedeployGitPreview(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	p, err := s.store.GetGitPreview(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, p.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	gd, err := s.store.GetGitDeploy(r.Context(), p.GitDeployID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), p.UserID)
	username := ""
	if owner != nil {
		username = owner.Username
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "git.preview_deploy", ac.User.ID, map[string]any{
		"preview_id":     p.ID,
		"repo_url":       gd.RepoURL,
		"branch":         p.Branch,
		"username":       username,
		"container_name": p.ContainerName,
		"image":          p.Image,
		"build_cmd":      gd.BuildCmd,
		"host_port":      p.HostPort,
		"container_port": p.ContainerPort,
	}); err == nil {
		jobID = &j.ID
	}
	s.store.TouchGitPreview(r.Context(), p.ID, "running")
	s.audit(r, ac.User, "git_preview.redeploy", "git_preview", p.ID, "success", map[string]any{"subdomain": p.Subdomain})
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}

func (s *Server) handleDeleteGitPreview(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	p, err := s.store.GetGitPreview(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, p.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), p.UserID)
	username := ""
	if owner != nil {
		username = owner.Username
	}
	_, _ = s.jobs.Enqueue(r.Context(), "git.preview_delete", ac.User.ID, map[string]any{
		"preview_id":     p.ID,
		"username":       username,
		"container_name": p.ContainerName,
	})
	if p.PreviewDomainID != "" {
		if dom, err := s.store.GetDomain(r.Context(), p.PreviewDomainID); err == nil && owner != nil {
			_, _ = s.jobs.Enqueue(r.Context(), "domain.delete", ac.User.ID, map[string]any{
				"username": owner.Username,
				"domain":   dom.Domain,
			})
			_ = s.store.DeleteDomain(r.Context(), dom.ID)
		}
	}
	if err := s.store.DeleteGitPreview(r.Context(), p.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "git_preview.delete", "git_preview", p.ID, "success", map[string]any{"subdomain": p.Subdomain})
	w.WriteHeader(http.StatusNoContent)
}
