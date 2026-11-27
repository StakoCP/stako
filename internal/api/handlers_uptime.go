// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

func (s *Server) handleListUptimeMonitors(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter *string
	if ac.User.Role != "admin" {
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListUptimeMonitors(r.Context(), userFilter, false)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateUptimeMonitor(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Domain         string `json:"domain"`
		URL            string `json:"url"`
		IntervalSec    int    `json:"interval_sec"`
		ExpectedStatus int    `json:"expected_status"`
		PublicStatus   *bool  `json:"public_status"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" && req.Domain != "" {
		req.URL = "https://" + req.Domain + "/"
	}
	u, err := url.ParseRequestURI(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Valid http/https url is required", "")
		return
	}
	if req.Domain == "" {
		req.Domain = u.Hostname()
	}
	if req.IntervalSec <= 0 {
		req.IntervalSec = 300
	}
	if req.ExpectedStatus <= 0 {
		req.ExpectedStatus = 200
	}
	pub := true
	if req.PublicStatus != nil {
		pub = *req.PublicStatus
	}
	id, _ := crypto.RandomID("up")
	m := &store.UptimeMonitor{
		ID:             id,
		UserID:         ac.User.ID,
		Domain:         req.Domain,
		URL:            req.URL,
		IntervalSec:    req.IntervalSec,
		ExpectedStatus: req.ExpectedStatus,
		Status:         "unknown",
		SSLDaysLeft:    -1,
		DomainDaysLeft: -1,
		PublicStatus:   pub,
	}
	if err := s.store.CreateUptimeMonitor(r.Context(), m); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "monitoring.uptime_create", "uptime_monitor", m.ID, "success", map[string]any{"domain": m.Domain, "url": m.URL})
	WriteJSON(w, http.StatusCreated, m)
}

func (s *Server) handleCheckUptimeMonitor(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	m, err := s.store.GetUptimeMonitor(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, m.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}

	runUptimeCheck(r.Context(), m)
	if err := s.store.UpdateUptimeMonitorResult(r.Context(), m); err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteUptimeMonitor(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	m, err := s.store.GetUptimeMonitor(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, m.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteUptimeMonitor(r.Context(), m.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "monitoring.uptime_delete", "uptime_monitor", m.ID, "success", map[string]any{"domain": m.Domain})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListUptimeMonitors(r.Context(), nil, true)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	overall := "operational"
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		if m.Status == "down" {
			overall = "outage"
		} else if m.Status == "degraded" && overall == "operational" {
			overall = "degraded"
		}
		uptimePct := 100.0
		if m.TotalChecks > 0 {
			uptimePct = float64(m.UpChecks) * 100.0 / float64(m.TotalChecks)
		}
		out = append(out, map[string]any{
			"id":                m.ID,
			"domain":            m.Domain,
			"url":               m.URL,
			"status":            m.Status,
			"last_http_status":  m.LastHTTPStatus,
			"last_latency_ms":   m.LastLatencyMs,
			"ssl_expires_at":    m.SSLExpiresAt,
			"ssl_days_left":     m.SSLDaysLeft,
			"domain_expires_at": m.DomainExpiresAt,
			"domain_days_left":  m.DomainDaysLeft,
			"uptime_pct":        uptimePct,
			"last_checked_at":   m.LastCheckedAt,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"overall_status": overall,
		"checked_at":     time.Now().UTC().Format(time.RFC3339),
		"monitors":       out,
	})
}

func runUptimeCheck(ctx context.Context, m *store.UptimeMonitor) {
	now := time.Now().UTC()
	m.LastCheckedAt = now.Format(time.RFC3339)
	m.TotalChecks++
	m.LastError = ""

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		m.Status = "down"
		m.LastError = err.Error()
		return
	}
	req.Header.Set("User-Agent", "Stako-Uptime-Monitor/1.0")

	start := time.Now()
	resp, err := client.Do(req)
	m.LastLatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		m.Status = "down"
		m.LastHTTPStatus = 0
		m.LastError = err.Error()
	} else {
		_ = resp.Body.Close()
		m.LastHTTPStatus = resp.StatusCode
		if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
			cert := resp.TLS.PeerCertificates[0]
			m.SSLExpiresAt = cert.NotAfter.UTC().Format(time.RFC3339)
			m.SSLDaysLeft = int(time.Until(cert.NotAfter).Hours() / 24)
			m.SSLIssuer = cert.Issuer.CommonName
		}
		if resp.StatusCode == m.ExpectedStatus || (m.ExpectedStatus == 200 && resp.StatusCode >= 200 && resp.StatusCode < 400) {
			m.Status = "up"
			m.UpChecks++
		} else {
			m.Status = "down"
			m.LastError = "unexpected HTTP status: " + resp.Status
		}
	}

	// Fallback direct TLS check if URL is https or domain has 443 open and peer cert wasn't captured
	if m.SSLExpiresAt == "" && strings.HasPrefix(m.URL, "https://") {
		if u, err := url.Parse(m.URL); err == nil {
			host := u.Hostname()
			port := u.Port()
			if port == "" {
				port = "443"
			}
			dialer := &net.Dialer{Timeout: 4 * time.Second}
			if conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, port), &tls.Config{ServerName: host}); err == nil {
				certs := conn.ConnectionState().PeerCertificates
				if len(certs) > 0 {
					m.SSLExpiresAt = certs[0].NotAfter.UTC().Format(time.RFC3339)
					m.SSLDaysLeft = int(time.Until(certs[0].NotAfter).Hours() / 24)
					m.SSLIssuer = certs[0].Issuer.CommonName
				}
				_ = conn.Close()
			}
		}
	}

	// Domain expiration check via RDAP (non-fatal if offline or unsupported TLD)
	if m.Domain != "" && !strings.HasSuffix(m.Domain, ".local") && !strings.HasSuffix(m.Domain, ".test") {
		rdapCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		rdapReq, err := http.NewRequestWithContext(rdapCtx, http.MethodGet, "https://rdap.org/domain/"+url.PathEscape(m.Domain), nil)
		if err == nil {
			if rResp, err := client.Do(rdapReq); err == nil {
				if rResp.StatusCode == http.StatusOK {
					var payload struct {
						Events []struct {
							EventAction string `json:"eventAction"`
							EventDate   string `json:"eventDate"`
						} `json:"events"`
					}
					if json.NewDecoder(rResp.Body).Decode(&payload) == nil {
						for _, ev := range payload.Events {
							if ev.EventAction == "expiration" && ev.EventDate != "" {
								if t, err := time.Parse(time.RFC3339, ev.EventDate); err == nil {
									m.DomainExpiresAt = t.UTC().Format(time.RFC3339)
									m.DomainDaysLeft = int(time.Until(t).Hours() / 24)
								}
								break
							}
						}
					}
				}
				_ = rResp.Body.Close()
			}
		}
	}

	if m.Status == "up" && ((m.SSLDaysLeft >= 0 && m.SSLDaysLeft < 14) || (m.DomainDaysLeft >= 0 && m.DomainDaysLeft < 30)) {
		m.Status = "degraded"
	}
}
