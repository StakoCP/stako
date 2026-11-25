// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
	"github.com/StakoCP/stako/internal/version"
)

func (s *Server) handleCheckPanelUpdate(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	latest := "v" + version.Version
	releaseURL := "https://github.com/StakoCP/stako/releases"
	updateAvailable := false

	client := &http.Client{Timeout: 4 * time.Second}
	req, _ := http.NewRequestWithContext(r.Context(), "GET", "https://api.github.com/repos/StakoCP/stako/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	if resp, err := client.Do(req); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var rel struct {
				TagName string `json:"tag_name"`
				HTMLURL string `json:"html_url"`
			}
			if json.NewDecoder(resp.Body).Decode(&rel) == nil && rel.TagName != "" {
				latest = rel.TagName
				if rel.HTMLURL != "" {
					releaseURL = rel.HTMLURL
				}
				updateAvailable = strings.TrimPrefix(latest, "v") != strings.TrimPrefix(version.Version, "v")
			}
		}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"current_version":  version.Version,
		"latest_version":   latest,
		"update_available": updateAvailable,
		"release_url":      releaseURL,
	})
}

func (s *Server) handleApplyPanelUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Version string `json:"version"`
		Channel string `json:"channel"`
	}
	if r.ContentLength > 0 {
		if err := DecodeJSON(r, &req); err != nil {
			WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
			return
		}
	}
	if req.Version == "" {
		req.Version = "latest"
	}
	if req.Channel == "" {
		req.Channel = "stable"
	}

	// Automatic pre-update system_config backup snapshot (FR-UP6)
	bkID, _ := crypto.RandomID("bk")
	_ = s.store.CreateBackup(r.Context(), &store.Backup{
		ID: bkID, Kind: "system_config", Target: "local",
		Checksum: "sha256:" + crypto.SHA256Hex(bkID+"pre-update"), Status: "completed",
	})

	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "panel.update", ac.User.ID, map[string]any{
		"version": req.Version, "channel": req.Channel, "backup_id": bkID,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "panel.update", "system", "stako", "success", map[string]any{"version": req.Version, "backup_id": bkID})
	WriteJSON(w, http.StatusAccepted, map[string]any{
		"job_id":    jobID,
		"backup_id": bkID,
	})
}
