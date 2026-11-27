// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

func (s *Server) handleListMigrationBundles(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdMigrationListBundles, nil)
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to list migration bundles", msg)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"bundles": resp.Data["bundles"]})
}

func (s *Server) handleInspectMigration(w http.ResponseWriter, r *http.Request) {
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
	if req.SourcePanel == "" {
		req.SourcePanel = "auto"
	}
	if !slices.Contains([]string{"auto", "stako", "hestiacp", "cpanel", "vestacp"}, req.SourcePanel) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "source_panel must be auto, stako, hestiacp, cpanel, or vestacp", "")
		return
	}
	if strings.TrimSpace(req.ArchivePath) == "" || strings.Contains(req.ArchivePath, "..") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid archive_path", "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdMigrationInspect, map[string]any{
		"source_panel": req.SourcePanel,
		"archive_path": strings.TrimSpace(req.ArchivePath),
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusUnprocessableEntity, "inspect-failed", "Failed to inspect migration archive", msg)
		return
	}
	WriteJSON(w, http.StatusOK, resp.Data)
}

func (s *Server) handleExportAccount(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	u, err := s.store.GetUserByID(r.Context(), req.UserID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}

	doms, _, _ := s.store.ListDomains(r.Context(), &u.ID, nil, "", 200, 0)
	dbs, _, _ := s.store.ListDatabases(r.Context(), &u.ID, nil, 200, 0)
	mailDoms, _ := s.store.ListMailDomains(r.Context(), &u.ID, nil)
	crons, _ := s.store.ListCronJobs(r.Context(), &u.ID, nil)
	ftps, _ := s.store.ListFtpAccounts(r.Context(), &u.ID, nil)

	dbPayloads := make([]map[string]any, 0, len(dbs))
	for _, d := range dbs {
		dbPayloads = append(dbPayloads, map[string]any{"name": d.Name, "engine": d.Engine, "username": d.DBUser})
	}
	manifest := map[string]any{
		"format":       "stako-account-v1",
		"exported_at":  time.Now().UTC().Format(time.RFC3339),
		"user":         map[string]any{"username": u.Username, "email": u.Email, "role": u.Role},
		"domains":      doms,
		"databases":    dbs,
		"mail_domains": mailDoms,
		"cron_jobs":    crons,
		"ftp_accounts": ftps,
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")

	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "migration.export", ac.User.ID, map[string]any{
		"username":      u.Username,
		"databases":     dbPayloads,
		"manifest_json": string(manifestBytes),
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "migration.export", "user", u.ID, "success", map[string]any{
		"username": u.Username,
		"domains":  len(doms),
		"dbs":      len(dbs),
	})
	WriteJSON(w, http.StatusAccepted, map[string]any{
		"job_id":   jobID,
		"username": u.Username,
		"summary": map[string]int{
			"domains":      len(doms),
			"databases":    len(dbs),
			"mail_domains": len(mailDoms),
			"cron_jobs":    len(crons),
			"ftp_accounts": len(ftps),
		},
	})
}

func (s *Server) handleMigrationImport(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		SourcePanel      string   `json:"source_panel"`
		ArchivePath      string   `json:"archive_path"`
		TargetUsername   string   `json:"target_username"`
		TargetEmail      string   `json:"target_email"`
		Domains          []string `json:"domains"`
		Databases        []string `json:"databases"`
		MailDomains      []string `json:"mail_domains"`
		IncludeFiles     *bool    `json:"include_files"`
		IncludeDatabases *bool    `json:"include_databases"`
		IncludeMail      *bool    `json:"include_mail"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if !slices.Contains([]string{"stako", "hestiacp", "cpanel", "vestacp"}, req.SourcePanel) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "source_panel must be stako, hestiacp, cpanel, or vestacp", "")
		return
	}
	if strings.TrimSpace(req.ArchivePath) == "" || strings.Contains(req.ArchivePath, "..") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid archive_path", "")
		return
	}

	targetUser := strings.TrimSpace(req.TargetUsername)
	if targetUser != "" {
		if err := agentproto.ValidateLinuxUsername(targetUser); err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid target_username", err.Error())
			return
		}
	}

	includeFiles := req.IncludeFiles == nil || *req.IncludeFiles
	includeDB := req.IncludeDatabases == nil || *req.IncludeDatabases
	includeMail := req.IncludeMail == nil || *req.IncludeMail

	provisioned := map[string]int{"domains": 0, "databases": 0, "mail_domains": 0}
	if targetUser != "" {
		u, err := s.store.GetUserByUsername(r.Context(), targetUser)
		if err != nil {
			email := strings.TrimSpace(req.TargetEmail)
			if email == "" {
				email = targetUser + "@migrated.local"
			}
			pwSeed, _ := crypto.RandomID("migpw")
			hash, _ := crypto.HashPassword(pwSeed)
			uid, _ := crypto.RandomID("u")
			newU := &store.User{
				ID:           uid,
				Username:     targetUser,
				Email:        email,
				PasswordHash: hash,
				Role:         "user",
				Status:       "active",
			}
			if createErr := s.store.CreateUser(r.Context(), newU); createErr == nil {
				u = newU
				_, _ = s.agent.Call(r.Context(), agentproto.CmdUserCreate, map[string]any{
					"username": u.Username,
					"shell":    "nologin",
				})
			}
		}
		if u != nil {
			for _, dom := range req.Domains {
				dom = strings.ToLower(strings.TrimSpace(dom))
				if err := agentproto.ValidateDomain(dom); err != nil {
					continue
				}
				did, _ := crypto.RandomID("d")
				d := &store.Domain{
					ID:         did,
					UserID:     u.ID,
					Domain:     dom,
					Stack:      "nginx+php-fpm",
					Template:   "default",
					Docroot:    "/home/" + u.Username + "/web/" + dom + "/public_html",
					PHPVersion: "8.3",
					SSLMode:    "selfsigned",
					Status:     "active",
				}
				if err := s.store.CreateDomain(r.Context(), d); err == nil {
					provisioned["domains"]++
				}
			}
			if includeDB {
				for _, dbName := range req.Databases {
					dbName = strings.TrimSpace(dbName)
					if err := agentproto.ValidateDBIdent(dbName); err != nil {
						continue
					}
					dbid, _ := crypto.RandomID("db")
					db := &store.Database{
						ID:          dbid,
						UserID:      u.ID,
						Name:        dbName,
						Engine:      "mysql",
						DBUser:      dbName,
						RemoteHosts: []string{"127.0.0.1"},
					}
					if err := s.store.CreateDatabase(r.Context(), db); err == nil {
						provisioned["databases"]++
					}
				}
			}
			if includeMail {
				for _, md := range req.MailDomains {
					md = strings.ToLower(strings.TrimSpace(md))
					if err := agentproto.ValidateDomain(md); err != nil {
						continue
					}
					mdid, _ := crypto.RandomID("md")
					m := &store.MailDomain{
						ID:            mdid,
						UserID:        u.ID,
						Domain:        md,
						DKIMSelector:  "default",
						DKIMPublicKey: generateDKIMPublicRecord(),
					}
					if err := s.store.CreateMailDomain(r.Context(), m); err == nil {
						provisioned["mail_domains"]++
					}
				}
			}
		}
	}

	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "panel.import", ac.User.ID, map[string]any{
		"source_panel":      req.SourcePanel,
		"archive_path":      strings.TrimSpace(req.ArchivePath),
		"target_username":   targetUser,
		"include_files":     includeFiles,
		"include_databases": includeDB,
		"include_mail":      includeMail,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "migration.import", "system", req.SourcePanel, "success", map[string]any{
		"archive":     req.ArchivePath,
		"target_user": targetUser,
		"provisioned": provisioned,
	})
	WriteJSON(w, http.StatusAccepted, map[string]any{
		"job_id":      jobID,
		"provisioned": provisioned,
	})
}

func (s *Server) handleMigrationTransfer(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Direction  string `json:"direction"`
		RemoteHost string `json:"remote_host"`
		RemotePort int    `json:"remote_port"`
		RemoteUser string `json:"remote_user"`
		RemotePath string `json:"remote_path"`
		LocalPath  string `json:"local_path"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Direction != "pull" && req.Direction != "push" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "direction must be pull or push", "")
		return
	}
	if req.RemotePort <= 0 || req.RemotePort > 65535 {
		req.RemotePort = 22
	}
	if strings.TrimSpace(req.RemoteHost) == "" || strings.TrimSpace(req.RemoteUser) == "" || strings.TrimSpace(req.RemotePath) == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "remote_host, remote_user, and remote_path are required", "")
		return
	}
	if strings.Contains(req.RemotePath, "..") || strings.Contains(req.LocalPath, "..") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid path", "")
		return
	}

	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "migration.transfer", ac.User.ID, map[string]any{
		"direction":   req.Direction,
		"remote_host": strings.TrimSpace(req.RemoteHost),
		"remote_port": req.RemotePort,
		"remote_user": strings.TrimSpace(req.RemoteUser),
		"remote_path": strings.TrimSpace(req.RemotePath),
		"local_path":  strings.TrimSpace(req.LocalPath),
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "migration.transfer", "system", req.Direction, "success", map[string]any{
		"remote_host": req.RemoteHost,
	})
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}
