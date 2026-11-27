// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/agentproto"
)

type pgBouncerConfig struct {
	Enabled         bool   `json:"enabled"`
	ListenPort      int    `json:"listen_port"`
	PoolMode        string `json:"pool_mode"`
	MaxClientConn   int    `json:"max_client_conn"`
	DefaultPoolSize int    `json:"default_pool_size"`
	ReservePoolSize int    `json:"reserve_pool_size"`
}

type pgReplicationStored struct {
	Role               string   `json:"role"`
	WALLevel           string   `json:"wal_level"`
	MaxWALSenders      int      `json:"max_wal_senders"`
	WALKeepSizeMB      int      `json:"wal_keep_size_mb"`
	ReplicationUser    string   `json:"replication_user"`
	ReplicationPassEnc string   `json:"replication_pass_enc,omitempty"`
	StandbyHosts       []string `json:"standby_hosts"`
	PrimaryHost        string   `json:"primary_host"`
	PrimaryPort        int      `json:"primary_port"`
}

type pgPITRConfig struct {
	Enabled       bool   `json:"enabled"`
	ArchiveDir    string `json:"archive_dir"`
	RetentionDays int    `json:"retention_days"`
}

func (s *Server) handleGetPGAdvanced(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	pgb := pgBouncerConfig{ListenPort: 6432, PoolMode: "transaction", MaxClientConn: 200, DefaultPoolSize: 20, ReservePoolSize: 5}
	if raw, err := s.store.GetSetting(r.Context(), "pg_pgbouncer_config"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &pgb)
	}
	rep := pgReplicationStored{Role: "disabled", WALLevel: "replica", MaxWALSenders: 10, WALKeepSizeMB: 512, ReplicationUser: "replicator", PrimaryPort: 5432, StandbyHosts: []string{}}
	if raw, err := s.store.GetSetting(r.Context(), "pg_replication_config"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &rep)
	}
	if rep.StandbyHosts == nil {
		rep.StandbyHosts = []string{}
	}
	pitr := pgPITRConfig{ArchiveDir: "/var/lib/postgresql/wal_archive", RetentionDays: 7}
	if raw, err := s.store.GetSetting(r.Context(), "pg_pitr_config"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &pitr)
	}

	status := map[string]any{"pgbouncer_active": false, "in_recovery": false, "replication_peers": []any{}}
	if resp, err := s.agent.Call(r.Context(), agentproto.CmdPGReplicationStatus, nil); err == nil && resp.OK && resp.Data != nil {
		if _, ok := resp.Data["pgbouncer_active"]; ok {
			status = resp.Data
		}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"pgbouncer": pgb,
		"replication": map[string]any{
			"role":                     rep.Role,
			"wal_level":                rep.WALLevel,
			"max_wal_senders":          rep.MaxWALSenders,
			"wal_keep_size_mb":         rep.WALKeepSizeMB,
			"replication_user":         rep.ReplicationUser,
			"has_replication_password": rep.ReplicationPassEnc != "",
			"standby_hosts":            rep.StandbyHosts,
			"primary_host":             rep.PrimaryHost,
			"primary_port":             rep.PrimaryPort,
		},
		"pitr":   pitr,
		"status": status,
	})
}

func (s *Server) handleSetPgBouncer(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req pgBouncerConfig
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.ListenPort <= 0 || req.ListenPort > 65535 {
		req.ListenPort = 6432
	}
	if req.PoolMode == "" {
		req.PoolMode = "transaction"
	}
	if !slices.Contains([]string{"transaction", "session", "statement"}, req.PoolMode) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "pool_mode must be transaction, session, or statement", "")
		return
	}
	if req.MaxClientConn <= 0 {
		req.MaxClientConn = 200
	}
	if req.DefaultPoolSize <= 0 {
		req.DefaultPoolSize = 20
	}
	if req.ReservePoolSize < 0 {
		req.ReservePoolSize = 5
	}
	b, _ := json.Marshal(req)
	if err := s.store.SetSetting(r.Context(), "pg_pgbouncer_config", string(b)); err != nil {
		handleStoreErr(w, err)
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdPGPoolApply, map[string]any{
		"enabled":           req.Enabled,
		"listen_port":       req.ListenPort,
		"pool_mode":         req.PoolMode,
		"max_client_conn":   req.MaxClientConn,
		"default_pool_size": req.DefaultPoolSize,
		"reserve_pool_size": req.ReservePoolSize,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to apply PgBouncer config", msg)
		return
	}
	s.audit(r, ac.User, "db.pgbouncer_update", "system", "pgbouncer", "success", map[string]any{"enabled": req.Enabled, "pool_mode": req.PoolMode})
	WriteJSON(w, http.StatusOK, req)
}

func (s *Server) handleSetPGReplication(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Role            string   `json:"role"`
		WALLevel        string   `json:"wal_level"`
		MaxWALSenders   int      `json:"max_wal_senders"`
		WALKeepSizeMB   int      `json:"wal_keep_size_mb"`
		ReplicationUser string   `json:"replication_user"`
		ReplicationPass string   `json:"replication_pass"`
		StandbyHosts    []string `json:"standby_hosts"`
		PrimaryHost     string   `json:"primary_host"`
		PrimaryPort     int      `json:"primary_port"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Role == "" {
		req.Role = "disabled"
	}
	if !slices.Contains([]string{"disabled", "primary", "standby"}, req.Role) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "role must be disabled, primary, or standby", "")
		return
	}
	if req.WALLevel == "" {
		req.WALLevel = "replica"
	}
	if !slices.Contains([]string{"replica", "logical"}, req.WALLevel) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "wal_level must be replica or logical", "")
		return
	}
	if req.MaxWALSenders <= 0 {
		req.MaxWALSenders = 10
	}
	if req.WALKeepSizeMB < 0 {
		req.WALKeepSizeMB = 512
	}
	if req.ReplicationUser == "" {
		req.ReplicationUser = "replicator"
	}
	if req.PrimaryPort <= 0 || req.PrimaryPort > 65535 {
		req.PrimaryPort = 5432
	}
	if req.Role == "standby" && strings.TrimSpace(req.PrimaryHost) == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "primary_host is required for standby role", "")
		return
	}

	var prev pgReplicationStored
	if raw, err := s.store.GetSetting(r.Context(), "pg_replication_config"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &prev)
	}
	passEnc := prev.ReplicationPassEnc
	plainPass := ""
	if req.ReplicationPass != "" {
		enc, err := s.enc.Encrypt(req.ReplicationPass)
		if err != nil {
			WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to encrypt replication password", err.Error())
			return
		}
		passEnc = enc
		plainPass = req.ReplicationPass
	} else if passEnc != "" {
		if dec, err := s.enc.Decrypt(passEnc); err == nil {
			plainPass = dec
		}
	}
	if req.StandbyHosts == nil {
		req.StandbyHosts = []string{}
	}
	stored := pgReplicationStored{
		Role:               req.Role,
		WALLevel:           req.WALLevel,
		MaxWALSenders:      req.MaxWALSenders,
		WALKeepSizeMB:      req.WALKeepSizeMB,
		ReplicationUser:    strings.TrimSpace(req.ReplicationUser),
		ReplicationPassEnc: passEnc,
		StandbyHosts:       req.StandbyHosts,
		PrimaryHost:        strings.TrimSpace(req.PrimaryHost),
		PrimaryPort:        req.PrimaryPort,
	}
	b, _ := json.Marshal(stored)
	if err := s.store.SetSetting(r.Context(), "pg_replication_config", string(b)); err != nil {
		handleStoreErr(w, err)
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdPGReplicationApply, map[string]any{
		"role":             stored.Role,
		"wal_level":        stored.WALLevel,
		"max_wal_senders":  stored.MaxWALSenders,
		"wal_keep_size_mb": stored.WALKeepSizeMB,
		"replication_user": stored.ReplicationUser,
		"replication_pass": plainPass,
		"standby_hosts":    stored.StandbyHosts,
		"primary_host":     stored.PrimaryHost,
		"primary_port":     stored.PrimaryPort,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to apply PostgreSQL replication", msg)
		return
	}
	s.audit(r, ac.User, "db.pg_replication_update", "system", "postgresql", "success", map[string]any{"role": stored.Role})
	WriteJSON(w, http.StatusOK, map[string]any{
		"role":                     stored.Role,
		"wal_level":                stored.WALLevel,
		"max_wal_senders":          stored.MaxWALSenders,
		"wal_keep_size_mb":         stored.WALKeepSizeMB,
		"replication_user":         stored.ReplicationUser,
		"has_replication_password": stored.ReplicationPassEnc != "",
		"standby_hosts":            stored.StandbyHosts,
		"primary_host":             stored.PrimaryHost,
		"primary_port":             stored.PrimaryPort,
	})
}

func (s *Server) handleSetPGPITR(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req pgPITRConfig
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.ArchiveDir = strings.TrimSpace(req.ArchiveDir)
	if req.ArchiveDir == "" {
		req.ArchiveDir = "/var/lib/postgresql/wal_archive"
	}
	if strings.Contains(req.ArchiveDir, "..") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid archive_dir", "")
		return
	}
	if req.RetentionDays <= 0 {
		req.RetentionDays = 7
	}
	b, _ := json.Marshal(req)
	if err := s.store.SetSetting(r.Context(), "pg_pitr_config", string(b)); err != nil {
		handleStoreErr(w, err)
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdPGPITRApply, map[string]any{
		"action":      "config",
		"enabled":     req.Enabled,
		"archive_dir": req.ArchiveDir,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to apply PITR config", msg)
		return
	}
	s.audit(r, ac.User, "db.pg_pitr_update", "system", "postgresql", "success", map[string]any{"enabled": req.Enabled})
	WriteJSON(w, http.StatusOK, req)
}

func (s *Server) handlePGPITRBaseBackup(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "pg.pitr_basebackup", ac.User.ID, map[string]any{
		"action": "basebackup",
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "db.pg_pitr_basebackup", "system", "postgresql", "success", nil)
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}

func (s *Server) handlePGPITRRestore(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		TargetTime string `json:"target_time"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(req.TargetTime)); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "target_time must be RFC3339 (e.g. 2026-11-27T08:00:00Z)", "")
		return
	}
	pitr := pgPITRConfig{ArchiveDir: "/var/lib/postgresql/wal_archive"}
	if raw, err := s.store.GetSetting(r.Context(), "pg_pitr_config"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &pitr)
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "pg.pitr_restore", ac.User.ID, map[string]any{
		"action":      "restore",
		"archive_dir": pitr.ArchiveDir,
		"target_time": strings.TrimSpace(req.TargetTime),
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "db.pg_pitr_restore", "system", "postgresql", "success", map[string]any{"target_time": req.TargetTime})
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}
