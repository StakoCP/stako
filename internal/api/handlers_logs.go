// SPDX-License-Identifier: MIT
package api

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/agentproto"
)

type logRotationConfig struct {
	Frequency string `json:"frequency"`
	Rotate    int    `json:"rotate"`
	MaxSize   string `json:"maxsize"`
	Compress  bool   `json:"compress"`
}

type logShippingStored struct {
	SyslogEnabled     bool   `json:"syslog_enabled"`
	SyslogHost        string `json:"syslog_host"`
	SyslogPort        int    `json:"syslog_port"`
	SyslogProtocol    string `json:"syslog_protocol"`
	SyslogFormat      string `json:"syslog_format"`
	LokiEnabled       bool   `json:"loki_enabled"`
	LokiURL           string `json:"loki_url"`
	LokiTenantID      string `json:"loki_tenant_id"`
	LokiUsername      string `json:"loki_username"`
	LokiPasswordEnc   string `json:"loki_password_enc,omitempty"`
	LokiLabels        string `json:"loki_labels"`
	LokiTLSSkipVerify bool   `json:"loki_tls_skip_verify"`
}

func defaultLogRotation() logRotationConfig {
	return logRotationConfig{
		Frequency: "daily",
		Rotate:    14,
		MaxSize:   "100M",
		Compress:  true,
	}
}

func (s *Server) loadLogRotation(r *http.Request) logRotationConfig {
	cfg := defaultLogRotation()
	if raw, err := s.store.GetSetting(r.Context(), "logs_rotation_policy"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	if cfg.Frequency != "daily" && cfg.Frequency != "weekly" {
		cfg.Frequency = "daily"
	}
	if cfg.Rotate < 1 || cfg.Rotate > 365 {
		cfg.Rotate = 14
	}
	if cfg.MaxSize == "" {
		cfg.MaxSize = "100M"
	}
	return cfg
}

func (s *Server) handleQueryCentralLogs(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	q := r.URL.Query()
	service := q.Get("service")
	if service == "" {
		service = "all"
	}
	if !slices.Contains([]string{"all", "web", "mail", "auth", "container", "panel"}, service) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "service must be all, web, mail, auth, container, or panel", "")
		return
	}
	level := q.Get("level")
	if level != "" && !slices.Contains([]string{"all", "error", "warn", "info"}, level) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "level must be all, error, warn, or info", "")
		return
	}
	lines := 200
	if ls := q.Get("lines"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 {
			lines = min(n, 1000)
		}
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdLogsQuery, map[string]any{
		"service": service,
		"source":  q.Get("source"),
		"query":   q.Get("q"),
		"level":   level,
		"lines":   lines,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to query centralized logs", msg)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"entries": resp.Data["entries"],
		"sources": resp.Data["sources"],
		"total":   resp.Data["total"],
	})
}

func (s *Server) handleGetLogRotation(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	WriteJSON(w, http.StatusOK, s.loadLogRotation(r))
}

func (s *Server) handleSetLogRotation(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req logRotationConfig
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Frequency != "daily" && req.Frequency != "weekly" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "frequency must be daily or weekly", "")
		return
	}
	if req.Rotate < 1 || req.Rotate > 365 {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "rotate must be between 1 and 365", "")
		return
	}
	req.MaxSize = strings.TrimSpace(req.MaxSize)
	if req.MaxSize == "" {
		req.MaxSize = "100M"
	}
	b, _ := json.Marshal(req)
	if err := s.store.SetSetting(r.Context(), "logs_rotation_policy", string(b)); err != nil {
		handleStoreErr(w, err)
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdLogsRotate, map[string]any{
		"frequency": req.Frequency,
		"rotate":    req.Rotate,
		"maxsize":   req.MaxSize,
		"compress":  req.Compress,
		"run_now":   false,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to apply log rotation policy", msg)
		return
	}
	s.audit(r, ac.User, "logs.rotation_update", "system", "logrotate", "success", map[string]any{"frequency": req.Frequency, "rotate": req.Rotate})
	WriteJSON(w, http.StatusOK, req)
}

func (s *Server) handleRunLogRotation(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	cfg := s.loadLogRotation(r)
	resp, err := s.agent.Call(r.Context(), agentproto.CmdLogsRotate, map[string]any{
		"frequency": cfg.Frequency,
		"rotate":    cfg.Rotate,
		"maxsize":   cfg.MaxSize,
		"compress":  cfg.Compress,
		"run_now":   true,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to rotate logs", msg)
		return
	}
	s.audit(r, ac.User, "logs.rotate_now", "system", "logrotate", "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "output": resp.Output})
}

func (s *Server) handleGetLogShipping(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	cfg := logShippingStored{SyslogPort: 514, SyslogProtocol: "tcp", SyslogFormat: "rfc5424"}
	if raw, err := s.store.GetSetting(r.Context(), "logs_shipping_config"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"syslog_enabled":       cfg.SyslogEnabled,
		"syslog_host":          cfg.SyslogHost,
		"syslog_port":            cfg.SyslogPort,
		"syslog_protocol":      cfg.SyslogProtocol,
		"syslog_format":        cfg.SyslogFormat,
		"loki_enabled":         cfg.LokiEnabled,
		"loki_url":             cfg.LokiURL,
		"loki_tenant_id":       cfg.LokiTenantID,
		"loki_username":        cfg.LokiUsername,
		"has_loki_password":    cfg.LokiPasswordEnc != "",
		"loki_labels":          cfg.LokiLabels,
		"loki_tls_skip_verify": cfg.LokiTLSSkipVerify,
	})
}

func (s *Server) handleSetLogShipping(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		SyslogEnabled     bool   `json:"syslog_enabled"`
		SyslogHost        string `json:"syslog_host"`
		SyslogPort        int    `json:"syslog_port"`
		SyslogProtocol    string `json:"syslog_protocol"`
		SyslogFormat      string `json:"syslog_format"`
		LokiEnabled       bool   `json:"loki_enabled"`
		LokiURL           string `json:"loki_url"`
		LokiTenantID      string `json:"loki_tenant_id"`
		LokiUsername      string `json:"loki_username"`
		LokiPassword      string `json:"loki_password"`
		LokiLabels        string `json:"loki_labels"`
		LokiTLSSkipVerify bool   `json:"loki_tls_skip_verify"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.SyslogHost = strings.TrimSpace(req.SyslogHost)
	req.LokiURL = strings.TrimSpace(req.LokiURL)
	if req.SyslogPort == 0 {
		req.SyslogPort = 514
	}
	if req.SyslogPort < 1 || req.SyslogPort > 65535 {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "syslog_port must be 1..65535", "")
		return
	}
	if req.SyslogProtocol == "" {
		req.SyslogProtocol = "tcp"
	}
	if !slices.Contains([]string{"udp", "tcp", "tls"}, req.SyslogProtocol) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "syslog_protocol must be udp, tcp, or tls", "")
		return
	}
	if req.SyslogFormat == "" {
		req.SyslogFormat = "rfc5424"
	}
	if !slices.Contains([]string{"rfc5424", "rfc3164"}, req.SyslogFormat) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "syslog_format must be rfc5424 or rfc3164", "")
		return
	}
	if req.SyslogEnabled && req.SyslogHost == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "syslog_host is required when syslog is enabled", "")
		return
	}
	if req.LokiEnabled {
		u, err := url.ParseRequestURI(req.LokiURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "loki_url must be a valid http/https URL", "")
			return
		}
	}

	var prev logShippingStored
	if raw, err := s.store.GetSetting(r.Context(), "logs_shipping_config"); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &prev)
	}
	passEnc := prev.LokiPasswordEnc
	plainPass := ""
	if req.LokiPassword != "" {
		enc, err := s.enc.Encrypt(req.LokiPassword)
		if err != nil {
			WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to encrypt Loki password", err.Error())
			return
		}
		passEnc = enc
		plainPass = req.LokiPassword
	} else if passEnc != "" {
		if dec, err := s.enc.Decrypt(passEnc); err == nil {
			plainPass = dec
		}
	}

	stored := logShippingStored{
		SyslogEnabled:     req.SyslogEnabled,
		SyslogHost:        req.SyslogHost,
		SyslogPort:        req.SyslogPort,
		SyslogProtocol:    req.SyslogProtocol,
		SyslogFormat:      req.SyslogFormat,
		LokiEnabled:       req.LokiEnabled,
		LokiURL:           req.LokiURL,
		LokiTenantID:      strings.TrimSpace(req.LokiTenantID),
		LokiUsername:      strings.TrimSpace(req.LokiUsername),
		LokiPasswordEnc:   passEnc,
		LokiLabels:        strings.TrimSpace(req.LokiLabels),
		LokiTLSSkipVerify: req.LokiTLSSkipVerify,
	}
	b, _ := json.Marshal(stored)
	if err := s.store.SetSetting(r.Context(), "logs_shipping_config", string(b)); err != nil {
		handleStoreErr(w, err)
		return
	}

	resp, err := s.agent.Call(r.Context(), agentproto.CmdLogsShippingApply, map[string]any{
		"syslog_enabled":       stored.SyslogEnabled,
		"syslog_host":          stored.SyslogHost,
		"syslog_port":          stored.SyslogPort,
		"syslog_protocol":      stored.SyslogProtocol,
		"syslog_format":        stored.SyslogFormat,
		"loki_enabled":         stored.LokiEnabled,
		"loki_url":             stored.LokiURL,
		"loki_tenant_id":       stored.LokiTenantID,
		"loki_username":        stored.LokiUsername,
		"loki_password":        plainPass,
		"loki_labels":          stored.LokiLabels,
		"loki_tls_skip_verify": stored.LokiTLSSkipVerify,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to apply log shipping configuration", msg)
		return
	}

	s.audit(r, ac.User, "logs.shipping_update", "system", "log_shipping", "success", map[string]any{
		"syslog_enabled": stored.SyslogEnabled,
		"loki_enabled":   stored.LokiEnabled,
	})
	WriteJSON(w, http.StatusOK, map[string]any{
		"syslog_enabled":       stored.SyslogEnabled,
		"syslog_host":          stored.SyslogHost,
		"syslog_port":          stored.SyslogPort,
		"syslog_protocol":      stored.SyslogProtocol,
		"syslog_format":        stored.SyslogFormat,
		"loki_enabled":         stored.LokiEnabled,
		"loki_url":             stored.LokiURL,
		"loki_tenant_id":       stored.LokiTenantID,
		"loki_username":        stored.LokiUsername,
		"has_loki_password":    stored.LokiPasswordEnc != "",
		"loki_labels":          stored.LokiLabels,
		"loki_tls_skip_verify": stored.LokiTLSSkipVerify,
	})
}

func (s *Server) handleTestLogShipping(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Target            string `json:"target"` // "loki" or "syslog"
		SyslogHost        string `json:"syslog_host"`
		SyslogPort        int    `json:"syslog_port"`
		SyslogProtocol    string `json:"syslog_protocol"`
		LokiURL           string `json:"loki_url"`
		LokiTenantID      string `json:"loki_tenant_id"`
		LokiUsername      string `json:"loki_username"`
		LokiPassword      string `json:"loki_password"`
		LokiTLSSkipVerify bool   `json:"loki_tls_skip_verify"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	switch req.Target {
	case "syslog":
		if req.SyslogHost == "" || req.SyslogPort < 1 || req.SyslogPort > 65535 {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Valid syslog_host and syslog_port required", "")
			return
		}
		network := "tcp"
		if req.SyslogProtocol == "udp" {
			network = "udp"
		}
		addr := net.JoinHostPort(req.SyslogHost, strconv.Itoa(req.SyslogPort))
		conn, err := net.DialTimeout(network, addr, 5*time.Second)
		if err != nil {
			WriteProblem(w, http.StatusBadGateway, "upstream", "Syslog connection test failed", err.Error())
			return
		}
		_ = conn.Close()
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "target": "syslog", "address": addr})

	case "loki":
		u, err := url.ParseRequestURI(strings.TrimSpace(req.LokiURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Valid loki_url required", "")
			return
		}
		readyURL := u.Scheme + "://" + u.Host + "/ready"
		client := &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: req.LokiTLSSkipVerify}, //nolint:gosec
			},
		}
		httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, readyURL, nil)
		if err != nil {
			WriteProblem(w, http.StatusBadRequest, "validation", "Invalid Loki URL", err.Error())
			return
		}
		if req.LokiTenantID != "" {
			httpReq.Header.Set("X-Scope-OrgID", req.LokiTenantID)
		}
		if req.LokiUsername != "" {
			pass := req.LokiPassword
			if pass == "" {
				if raw, err := s.store.GetSetting(r.Context(), "logs_shipping_config"); err == nil && raw != "" {
					var stored logShippingStored
					if json.Unmarshal([]byte(raw), &stored) == nil && stored.LokiPasswordEnc != "" {
						if dec, err := s.enc.Decrypt(stored.LokiPasswordEnc); err == nil {
							pass = dec
						}
					}
				}
			}
			httpReq.SetBasicAuth(req.LokiUsername, pass)
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			WriteProblem(w, http.StatusBadGateway, "upstream", "Loki connection test failed", err.Error())
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			WriteProblem(w, http.StatusBadGateway, "upstream", "Loki returned error status", resp.Status)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "target": "loki", "status": resp.StatusCode})

	default:
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "target must be loki or syslog", "")
	}
}
