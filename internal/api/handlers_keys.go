// SPDX-License-Identifier: MIT
package api

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	items, err := s.store.ListApiKeysByUser(r.Context(), ac.User.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	var req struct {
		Name          string   `json:"name"`
		Scopes        []string `json:"scopes"`
		AllowedIPs    []string `json:"allowed_ips"`
		ExpiresInDays *int     `json:"expires_in_days"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Name required", "",
			FieldError{Field: "name", Message: "API key name is required"})
		return
	}
	if len(req.Scopes) == 0 {
		req.Scopes = []string{"read"}
	}
	for _, sc := range req.Scopes {
		if sc != "read" && sc != "write" {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid scope", "",
				FieldError{Field: "scopes", Message: "Scopes must be read and/or write"})
			return
		}
	}
	for _, entry := range req.AllowedIPs {
		if strings.Contains(entry, "/") {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid CIDR", "",
					FieldError{Field: "allowed_ips", Message: "Invalid CIDR: " + entry})
				return
			}
		} else if net.ParseIP(entry) == nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid IP", "",
				FieldError{Field: "allowed_ips", Message: "Invalid IP: " + entry})
			return
		}
	}

	randHex, _ := crypto.RandomHex(24)
	rawToken := "stk_" + randHex
	prefix := rawToken[:10]
	id, _ := crypto.RandomID("k")
	now := time.Now().UTC()
	var exp *time.Time
	if req.ExpiresInDays != nil && *req.ExpiresInDays > 0 {
		t := now.Add(time.Duration(*req.ExpiresInDays) * 24 * time.Hour)
		exp = &t
	}
	if req.AllowedIPs == nil {
		req.AllowedIPs = []string{}
	}
	k := &store.ApiKey{
		ID:         id,
		UserID:     ac.User.ID,
		Name:       req.Name,
		Prefix:     prefix,
		KeyHash:    crypto.SHA256Hex(rawToken),
		Scopes:     req.Scopes,
		AllowedIPs: req.AllowedIPs,
		ExpiresAt:  exp,
		CreatedAt:  now,
	}
	if err := s.store.CreateApiKey(r.Context(), k); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "api_key.create", "api_key", k.ID, "success", map[string]any{"name": k.Name, "scopes": k.Scopes})
	WriteJSON(w, http.StatusCreated, map[string]any{
		"api_key": k,
		"token":   rawToken,
	})
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	id := r.PathValue("id")
	if err := s.store.DeleteApiKey(r.Context(), id, ac.User.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "api_key.delete", "api_key", id, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}
