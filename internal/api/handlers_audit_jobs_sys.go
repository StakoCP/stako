// SPDX-License-Identifier: MIT
package api

import (
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/version"
)

var startTime = time.Now()

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	limit, offset := parsePagination(r)
	action := r.URL.Query().Get("action")
	actor := r.URL.Query().Get("actor")
	items, total, err := s.store.ListAudit(r.Context(), action, actor, limit, offset)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) handleVerifyAudit(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	valid, checked, brokenAt, err := s.store.VerifyAuditChain(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"valid":     valid,
		"checked":   checked,
		"broken_at": brokenAt,
	})
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	limit, offset := parsePagination(r)
	var createdBy *string
	if ac.User.Role != "admin" {
		createdBy = &ac.User.ID
	}
	items, total, err := s.store.ListJobs(r.Context(), createdBy, limit, offset)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	j, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if ac.User.Role != "admin" && j.CreatedBy != ac.User.ID {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	WriteJSON(w, http.StatusOK, j)
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	hostname, _ := os.Hostname()
	users, packages, jobsRunning, _ := s.store.Counts(r.Context())

	agentConnected := false
	agentDryRun := true
	agentVersion := ""
	var osName any = runtime.GOOS + "/" + runtime.GOARCH
	var kernel any = nil
	var loadAvg any = nil
	var memTotal any = nil
	var memAvail any = nil

	if resp, err := s.agent.Call(r.Context(), agentproto.CmdSystemInfo, nil); err == nil && resp.OK {
		agentConnected = true
		agentDryRun = resp.DryRun
		if v, ok := resp.Data["version"].(string); ok {
			agentVersion = v
		}
		if v, ok := resp.Data["os"]; ok && v != nil {
			osName = v
		}
		if v, ok := resp.Data["kernel"]; ok {
			kernel = v
		}
		if v, ok := resp.Data["load_avg"]; ok {
			loadAvg = v
		}
		if v, ok := resp.Data["memory_total_bytes"]; ok {
			memTotal = v
		}
		if v, ok := resp.Data["memory_available_bytes"]; ok {
			memAvail = v
		}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"hostname":               hostname,
		"os":                     osName,
		"kernel":                 kernel,
		"version":                version.Version,
		"uptime_seconds":         int64(time.Since(startTime).Seconds()),
		"cpu_cores":              runtime.NumCPU(),
		"load_avg":               loadAvg,
		"memory_total_bytes":     memTotal,
		"memory_available_bytes": memAvail,
		"agent": map[string]any{
			"connected": agentConnected,
			"dry_run":   agentDryRun,
			"version":   agentVersion,
		},
		"counts": map[string]any{
			"users":        users,
			"packages":     packages,
			"jobs_running": jobsRunning,
		},
	})
}
