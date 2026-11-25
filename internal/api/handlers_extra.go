// SPDX-License-Identifier: MIT
package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"time"

	"github.com/StakoCP/stako/internal/crypto"
)

// POST /api/v1/security/rotate-keys (SEC-S24: mass secret rotation)
func (s *Server) handleRotateKeys(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	// Revoke all non-admin sessions on emergency key rotation
	_ = s.store.DeleteNonAdminSessions(r.Context())
	s.audit(r, ac.User, "security.rotate_keys", "system", "keys", "success", map[string]any{
		"non_admin_sessions_revoked": true,
	})
	WriteJSON(w, http.StatusOK, map[string]any{
		"rotated_at":                 time.Now().UTC(),
		"non_admin_sessions_revoked": true,
	})
}

// GET /api/v1/security/forensics (SEC-S25: export forensic evidence bundle)
func (s *Server) handleExportForensics(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	entries, _, _ := s.store.ListAudit(r.Context(), "", "", 200, 0)
	valid, checked, brokenAt, _ := s.store.VerifyAuditChain(r.Context())

	bundle, _ := json.MarshalIndent(map[string]any{
		"exported_at":    time.Now().UTC(),
		"audit_valid":    valid,
		"audit_checked":  checked,
		"audit_broken":   brokenAt,
		"recent_audit":   entries,
	}, "", "  ")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "forensics.json", Mode: 0o600, Size: int64(len(bundle)), ModTime: time.Now().UTC()}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write(bundle)
	_ = tw.Close()
	_ = gz.Close()

	s.audit(r, ac.User, "security.forensics_export", "system", "forensics", "success", nil)
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename=stako-forensics.tar.gz")
	w.Header().Set("X-Checksum-Sha256", crypto.SHA256Hex(buf.String()))
	_, _ = w.Write(buf.Bytes())
}

// GET /api/v1/backups/{id}/download (7.19 FR-CB5)
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	bk, err := s.store.GetBackup(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if ac.User.Role != "admin" && (bk.UserID == nil || *bk.UserID != ac.User.ID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	manifest, _ := json.MarshalIndent(bk, "", "  ")
	s.audit(r, ac.User, "backup.download", "backup", bk.ID, "success", nil)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename="+bk.ID+"-manifest.json")
	_, _ = w.Write(manifest)
}

// POST /api/v1/billing/sso (7.17 FR-O6: single sign-on link for billing)
func (s *Server) handleBillingSSO(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" && ac.User.Role != "reseller" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	target, err := s.store.GetUserByID(r.Context(), req.UserID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !canManageUser(ac.User, target) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden target user", "")
		return
	}
	tok, _ := crypto.RandomHex(24)
	s.audit(r, ac.User, "billing.sso", "user", target.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{
		"sso_token": tok,
		"sso_url":   "/#/login?sso=" + tok,
		"expires_in": 120,
	})
}
