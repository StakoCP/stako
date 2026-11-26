// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

// --- DNS (PowerDNS / Cloudflare) ---

func (s *Server) handleListDNSZones(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListDNSZones(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateDNSZone(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Domain   string  `json:"domain"`
		Provider string  `json:"provider"`
		UserID   *string `json:"user_id"`
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
	if req.Provider == "" {
		req.Provider = "powerdns"
	}
	if req.Provider != "powerdns" && req.Provider != "cloudflare" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Provider must be powerdns or cloudflare", "")
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	id, _ := crypto.RandomID("dz")
	z := &store.DNSZone{ID: id, UserID: owner.ID, Domain: req.Domain, Provider: req.Provider}
	if err := s.store.CreateDNSZone(r.Context(), z); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncDNSZone(r.Context(), z)
	s.audit(r, ac.User, "dns.zone_create", "dns_zone", z.ID, "success", map[string]any{"domain": z.Domain, "provider": z.Provider})
	WriteJSON(w, http.StatusCreated, z)
}

func (s *Server) handleDeleteDNSZone(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	z, err := s.store.GetDNSZone(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, z.UserID) || ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteDNSZone(r.Context(), z.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	if z.Provider == "powerdns" {
		_, _ = s.agent.Call(r.Context(), agentproto.CmdDNSZoneDelete, map[string]any{"domain": z.Domain})
	}
	s.audit(r, ac.User, "dns.zone_delete", "dns_zone", z.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListDNSRecords(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	z, err := s.store.GetDNSZone(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, z.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	items, err := s.store.ListDNSRecords(r.Context(), z.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateDNSRecord(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	z, err := s.store.GetDNSZone(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, z.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Content  string `json:"content"`
		TTL      int    `json:"ttl"`
		Priority int    `json:"priority"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Type = strings.ToUpper(strings.TrimSpace(req.Type))
	if !slices.Contains([]string{"A", "AAAA", "CNAME", "MX", "TXT", "SRV", "NS"}, req.Type) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid DNS record type", "")
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Content) == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Name and content required", "")
		return
	}
	if req.TTL <= 0 {
		req.TTL = 3600
	}
	id, _ := crypto.RandomID("dr")
	rec := &store.DNSRecord{
		ID: id, ZoneID: z.ID, Type: req.Type, Name: strings.TrimSpace(req.Name),
		Content: strings.TrimSpace(req.Content), TTL: req.TTL, Priority: req.Priority,
	}
	if err := s.store.CreateDNSRecord(r.Context(), rec); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncDNSZone(r.Context(), z)
	s.audit(r, ac.User, "dns.record_create", "dns_record", rec.ID, "success", map[string]any{"type": rec.Type, "name": rec.Name})
	WriteJSON(w, http.StatusCreated, rec)
}

func (s *Server) handleDeleteDNSRecord(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	z, _ := s.store.GetDNSZone(r.Context(), r.PathValue("id"))
	if err := s.store.DeleteDNSRecord(r.Context(), r.PathValue("recordId")); err != nil {
		handleStoreErr(w, err)
		return
	}
	if z != nil {
		s.syncDNSZone(r.Context(), z)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) syncDNSZone(ctx context.Context, z *store.DNSZone) {
	if z == nil || z.Provider != "powerdns" {
		return
	}
	recs, err := s.store.ListDNSRecords(ctx, z.ID)
	if err != nil {
		return
	}
	recMaps := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		recMaps = append(recMaps, map[string]any{
			"type":     rec.Type,
			"name":     rec.Name,
			"content":  rec.Content,
			"ttl":      rec.TTL,
			"priority": rec.Priority,
		})
	}
	_, _ = s.agent.Call(ctx, agentproto.CmdDNSZoneSync, map[string]any{
		"domain":  z.Domain,
		"records": recMaps,
	})
}

// --- Cron Jobs ---

func (s *Server) handleListCron(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListCronJobs(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateCron(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Schedule string  `json:"schedule"`
		Command  string  `json:"command"`
		UserID   *string `json:"user_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Schedule = strings.TrimSpace(req.Schedule)
	req.Command = strings.TrimSpace(req.Command)
	if len(strings.Fields(req.Schedule)) != 5 {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid cron schedule (must have 5 fields)", "")
		return
	}
	if req.Command == "" || strings.ContainsAny(req.Command, "\n\r") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid command", "")
		return
	}
	owner, ok := s.resolveTargetUser(r, ac, req.UserID)
	if !ok {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	id, _ := crypto.RandomID("cj")
	cj := &store.CronJob{ID: id, UserID: owner.ID, Schedule: req.Schedule, Command: req.Command}
	if err := s.store.CreateCronJob(r.Context(), cj); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncUserCron(r.Context(), owner.ID)
	s.audit(r, ac.User, "cron.create", "cron", cj.ID, "success", map[string]any{"schedule": cj.Schedule})
	WriteJSON(w, http.StatusCreated, cj)
}

func (s *Server) handleDeleteCron(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	cj, err := s.store.GetCronJob(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, cj.UserID) || ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteCronJob(r.Context(), cj.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncUserCron(r.Context(), cj.UserID)
	s.audit(r, ac.User, "cron.delete", "cron", cj.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) syncUserCron(ctx context.Context, userID string) {
	owner, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return
	}
	jobs, err := s.store.ListCronJobs(ctx, &userID, nil)
	if err != nil {
		return
	}
	jobMaps := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		jobMaps = append(jobMaps, map[string]any{
			"id":       j.ID,
			"schedule": j.Schedule,
			"command":  j.Command,
		})
	}
	_, _ = s.agent.Call(ctx, agentproto.CmdCronSync, map[string]any{
		"username": owner.Username,
		"jobs":     jobMaps,
	})
}

// --- File Manager (SEC-A12 safe path resolution) ---

func resolveUserFilePath(username, reqPath string) (string, error) {
	base := "/home/" + username
	clean := path.Clean("/" + reqPath)
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("path traversal rejected")
	}
	return base + clean, nil
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	reqPath := r.URL.Query().Get("path")
	fullPath, err := resolveUserFilePath(ac.User.Username, reqPath)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFileList, map[string]any{"path": fullPath})
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to list files", "")
		return
	}
	items, _ := resp.Data["items"]
	if items == nil {
		items = []any{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"path": path.Clean("/" + reqPath), "items": items})
}

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	reqPath := r.URL.Query().Get("path")
	fullPath, err := resolveUserFilePath(ac.User.Username, reqPath)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFileRead, map[string]any{"path": fullPath})
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusNotFound, "not-found", "Cannot read file", "")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"path": path.Clean("/" + reqPath), "content": resp.Output})
}

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	fullPath, err := resolveUserFilePath(ac.User.Username, req.Path)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFileWrite, map[string]any{
		"path": fullPath, "content": req.Content, "username": ac.User.Username,
	})
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to write file", "")
		return
	}
	s.audit(r, ac.User, "file.write", "file", req.Path, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if path.Clean("/"+req.Path) == "/" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Cannot delete home root", "")
		return
	}
	fullPath, err := resolveUserFilePath(ac.User.Username, req.Path)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	_, _ = s.agent.Call(r.Context(), agentproto.CmdFileDelete, map[string]any{"path": fullPath})
	s.audit(r, ac.User, "file.delete", "file", req.Path, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) serveAgentFile(w http.ResponseWriter, r *http.Request, attachment bool) {
	ac := GetAuth(r)
	reqPath := r.URL.Query().Get("path")
	fullPath, err := resolveUserFilePath(ac.User.Username, reqPath)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFileReadB64, map[string]any{"path": fullPath})
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusNotFound, "not-found", "Cannot read file", "")
		return
	}
	b64, _ := resp.Data["b64"].(string)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Invalid file encoding", "")
		return
	}
	name := filepath.Base(fullPath)
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if ct == "" {
		ct = http.DetectContentType(raw)
	}
	disp := "inline"
	if attachment {
		disp = "attachment"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disp, strings.ReplaceAll(name, `"`, "")))
	_, _ = w.Write(raw)
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	s.serveAgentFile(w, r, true)
}

func (s *Server) handleRawFile(w http.ResponseWriter, r *http.Request) {
	s.serveAgentFile(w, r, false)
}

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024*1024)
	if err := r.ParseMultipartForm(32 * 1024 * 1024); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid multipart upload", err.Error())
		return
	}
	dirPath := r.FormValue("path")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Missing file field", err.Error())
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 32*1024*1024))
	if err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Failed to read upload", err.Error())
		return
	}
	baseName := filepath.Base(hdr.Filename)
	if baseName == "" || baseName == "." || baseName == ".." {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid filename", "")
		return
	}
	relTarget := path.Join("/", dirPath, baseName)
	fullPath, err := resolveUserFilePath(ac.User.Username, relTarget)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFileWriteB64, map[string]any{
		"path":     fullPath,
		"b64":      base64.StdEncoding.EncodeToString(raw),
		"username": ac.User.Username,
	})
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to save uploaded file", "")
		return
	}
	s.audit(r, ac.User, "file.upload", "file", relTarget, "success", map[string]any{"size": len(raw)})
	WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "path": relTarget})
}

func (s *Server) handleMkdirFile(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	fullPath, err := resolveUserFilePath(ac.User.Username, req.Path)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFileMkdir, map[string]any{
		"path": fullPath, "username": ac.User.Username,
	})
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to create directory", "")
		return
	}
	s.audit(r, ac.User, "file.mkdir", "file", req.Path, "success", nil)
	WriteJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleCopyFile(w http.ResponseWriter, r *http.Request) {
	s.handleCopyOrMoveFile(w, r, false)
}

func (s *Server) handleMoveFile(w http.ResponseWriter, r *http.Request) {
	s.handleCopyOrMoveFile(w, r, true)
}

func (s *Server) handleCopyOrMoveFile(w http.ResponseWriter, r *http.Request, move bool) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	fullSrc, err := resolveUserFilePath(ac.User.Username, req.Src)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	fullDst, err := resolveUserFilePath(ac.User.Username, req.Dst)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	if path.Clean("/"+req.Src) == "/" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Cannot move/copy home root", "")
		return
	}
	cmd := agentproto.CmdFileCopy
	auditAct := "file.copy"
	if move {
		cmd = agentproto.CmdFileMove
		auditAct = "file.move"
	}
	resp, err := s.agent.Call(r.Context(), cmd, map[string]any{
		"src": fullSrc, "dst": fullDst, "username": ac.User.Username,
	})
	if err != nil || !resp.OK {
		errMsg := ""
		if resp.Error != "" {
			errMsg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "File operation failed", errMsg)
		return
	}
	s.audit(r, ac.User, auditAct, "file", req.Src, "success", map[string]any{"dst": req.Dst})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleChmodFile(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Mode = strings.TrimSpace(req.Mode)
	if len(req.Mode) < 3 || len(req.Mode) > 4 {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Mode must be 3 or 4 octal digits (e.g. 0644, 755)", "")
		return
	}
	for _, ch := range req.Mode {
		if ch < '0' || ch > '7' {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Mode must contain only octal digits 0-7", "")
			return
		}
	}
	fullPath, err := resolveUserFilePath(ac.User.Username, req.Path)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFileChmod, map[string]any{
		"path": fullPath, "mode": req.Mode,
	})
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to change permissions", "")
		return
	}
	s.audit(r, ac.User, "file.chmod", "file", req.Path, "success", map[string]any{"mode": req.Mode})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleExtractFile(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Src    string `json:"src"`
		DstDir string `json:"dst_dir"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	fullSrc, err := resolveUserFilePath(ac.User.Username, req.Src)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	fullDst, err := resolveUserFilePath(ac.User.Username, req.DstDir)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFileExtract, map[string]any{
		"src": fullSrc, "dst_dir": fullDst, "username": ac.User.Username,
	})
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Failed to extract archive", resp.Error+" "+resp.Output)
		return
	}
	s.audit(r, ac.User, "file.extract", "file", req.Src, "success", map[string]any{"dst_dir": req.DstDir})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCompressFile(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	fullSrc, err := resolveUserFilePath(ac.User.Username, req.Src)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	fullDst, err := resolveUserFilePath(ac.User.Username, req.Dst)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", err.Error(), "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdFileCompress, map[string]any{
		"src": fullSrc, "dst": fullDst, "username": ac.User.Username,
	})
	if err != nil || !resp.OK {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to compress file", resp.Error+" "+resp.Output)
		return
	}
	s.audit(r, ac.User, "file.compress", "file", req.Src, "success", map[string]any{"dst": req.Dst})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Monitoring & Alerts ---

func (s *Server) handleMonitoringMetrics(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var cpuPct, memPct, diskPct float64
	agentStatus := "inactive"
	if resp, err := s.agent.Call(r.Context(), agentproto.CmdSystemInfo, nil); err == nil && resp.OK {
		agentStatus = "active"
		if v, ok := resp.Data["cpu_percent"].(float64); ok {
			cpuPct = v
		}
		if v, ok := resp.Data["memory_percent"].(float64); ok {
			memPct = v
		}
		if v, ok := resp.Data["disk_percent"].(float64); ok {
			diskPct = v
		}
	}
	services := []map[string]string{
		{"name": "stako-core", "status": "active"},
		{"name": "stako-agent", "status": agentStatus},
	}
	specs := make([]map[string]string, 0, len(managedServices))
	for _, m := range managedServices {
		specs = append(specs, map[string]string{"id": m.ID, "unit": m.Unit, "apt_pkg": m.AptPkg})
	}
	if resp, err := s.agent.Call(r.Context(), agentproto.CmdSysServiceList, map[string]any{"services": specs}); err == nil && resp.OK {
		if rawMap, ok := resp.Data["services"].(map[string]any); ok {
			for _, m := range managedServices {
				if info, ok := rawMap[m.ID].(map[string]any); ok {
					if inst, _ := info["installed"].(bool); inst {
						act, _ := info["active"].(string)
						if act == "" {
							act = "inactive"
						}
						services = append(services, map[string]string{"name": m.ID, "status": act})
					}
				}
			}
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"cpu_percent":    cpuPct,
		"memory_percent": memPct,
		"disk_percent":   diskPct,
		"services":       services,
		"timestamp":      time.Now().UTC(),
	})
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	items, err := s.store.ListAlertRules(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateAlert(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Name      string `json:"name"`
		Channel   string `json:"channel"`
		Target    string `json:"target"`
		Threshold string `json:"threshold"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if !slices.Contains([]string{"email", "telegram", "slack", "webhook"}, req.Channel) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid channel", "")
		return
	}
	id, _ := crypto.RandomID("al")
	a := &store.AlertRule{ID: id, Name: req.Name, Channel: req.Channel, Target: req.Target, Threshold: req.Threshold}
	if err := s.store.CreateAlertRule(r.Context(), a); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "monitoring.alert_create", "alert", a.ID, "success", nil)
	WriteJSON(w, http.StatusCreated, a)
}

func (s *Server) handleDeleteAlert(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	if err := s.store.DeleteAlertRule(r.Context(), r.PathValue("id")); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Web Terminal (7.8 FR-S5, SEC-A18) ---

func (s *Server) handleTerminalExec(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if ac.User.Role != "admin" && ac.User.Shell == "nologin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Shell access disabled for this user", "Enable bash or rbash shell first")
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if err := DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Command) == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Command required", "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdTerminalExec, map[string]any{
		"username": ac.User.Username, "command": req.Command,
	})
	s.audit(r, ac.User, "terminal.exec", "terminal", ac.User.ID, "success", map[string]any{"command": req.Command})
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{"output": err.Error(), "exit_code": 1})
		return
	}
	code := 0
	if !resp.OK {
		code = 1
	}
	WriteJSON(w, http.StatusOK, map[string]any{"output": resp.Output, "exit_code": code})
}
