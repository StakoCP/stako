// SPDX-License-Identifier: MIT
package api

import (
	"net/http"
	"strings"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

func canManageUser(actor, target *store.User) bool {
	if actor.Role == "admin" {
		return true
	}
	if actor.Role == "reseller" {
		if target.ID == actor.ID {
			return true
		}
		return target.OwnerID != nil && *target.OwnerID == actor.ID && target.Role == "user"
	}
	return target.ID == actor.ID
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	limit, offset := parsePagination(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	if ac.User.Role == "user" || ac.User.Role == "readonly" {
		WriteJSON(w, http.StatusOK, map[string]any{"items": []store.User{*ac.User}, "total": 1})
		return
	}
	var ownerFilter *string
	if ac.User.Role == "reseller" {
		ownerFilter = &ac.User.ID
	}
	items, total, err := s.store.ListUsers(r.Context(), ownerFilter, q, limit, offset)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" && ac.User.Role != "reseller" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "Only admin or reseller can create users")
		return
	}
	var req struct {
		Username  string  `json:"username"`
		Email     string  `json:"email"`
		Password  string  `json:"password"`
		Role      string  `json:"role"`
		PackageID *string `json:"package_id"`
		Shell     string  `json:"shell"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if err := agentproto.ValidateLinuxUsername(req.Username); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid username", err.Error(),
			FieldError{Field: "username", Message: err.Error()})
		return
	}
	if !strings.Contains(req.Email, "@") {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid email", "Must be a valid email",
			FieldError{Field: "email", Message: "Invalid email address"})
		return
	}
	if err := crypto.ValidatePasswordStrength(req.Password); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Weak password", err.Error(),
			FieldError{Field: "password", Message: err.Error()})
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	if ac.User.Role == "reseller" && req.Role != "user" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden role", "Resellers can only create 'user' accounts")
		return
	}
	switch req.Role {
	case "admin", "reseller", "user", "readonly":
	default:
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid role", "",
			FieldError{Field: "role", Message: "Must be admin, reseller, user, or readonly"})
		return
	}
	if req.Shell == "" {
		req.Shell = "nologin"
	}
	shellPath, err := agentproto.ValidateShell(req.Shell)
	if err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid shell", err.Error(),
			FieldError{Field: "shell", Message: err.Error()})
		return
	}
	if req.PackageID != nil && *req.PackageID != "" {
		if _, err := s.store.GetPackageByID(r.Context(), *req.PackageID); err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Package not found", "",
				FieldError{Field: "package_id", Message: "Package does not exist"})
			return
		}
	}

	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to hash password", "")
		return
	}
	id, _ := crypto.RandomID("u")
	var ownerID *string
	if ac.User.Role == "reseller" {
		ownerID = &ac.User.ID
	}
	u := &store.User{
		ID:           id,
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hash,
		Role:         req.Role,
		Status:       "active",
		PackageID:    req.PackageID,
		OwnerID:      ownerID,
		Shell:        req.Shell,
	}
	if err := s.store.CreateUser(r.Context(), u); err != nil {
		handleStoreErr(w, err)
		return
	}

	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "user.provision", ac.User.ID, map[string]any{
		"username": u.Username,
		"shell":    shellPath,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "user.create", "user", u.ID, "success", map[string]any{"username": u.Username, "role": u.Role})
	WriteJSON(w, http.StatusCreated, map[string]any{"user": u, "job_id": jobID})
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	u, err := s.store.GetUserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !canManageUser(ac.User, u) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	WriteJSON(w, http.StatusOK, u)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	u, err := s.store.GetUserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !canManageUser(ac.User, u) || ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Email     *string `json:"email"`
		Role      *string `json:"role"`
		PackageID *string `json:"package_id"`
		Shell     *string `json:"shell"`
		Password  *string `json:"password"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Password != nil {
		if !s.requireReauth(w, r) {
			return
		}
		if err := crypto.ValidatePasswordStrength(*req.Password); err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Weak password", err.Error(),
				FieldError{Field: "password", Message: err.Error()})
			return
		}
		h, err := crypto.HashPassword(*req.Password)
		if err != nil {
			WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to hash password", "")
			return
		}
		u.PasswordHash = h
	}
	if req.Email != nil {
		if !strings.Contains(*req.Email, "@") {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid email", "",
				FieldError{Field: "email", Message: "Invalid email"})
			return
		}
		u.Email = *req.Email
	}
	if req.Role != nil {
		if ac.User.Role != "admin" {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Only admin can change roles", "")
			return
		}
		u.Role = *req.Role
	}
	if req.PackageID != nil {
		if ac.User.Role != "admin" && ac.User.Role != "reseller" {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Cannot change package", "")
			return
		}
		if *req.PackageID == "" {
			u.PackageID = nil
		} else {
			u.PackageID = req.PackageID
		}
	}
	shellChanged := false
	var shellPath string
	if req.Shell != nil {
		if ac.User.Role != "admin" && ac.User.Role != "reseller" {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Cannot change shell", "")
			return
		}
		sp, err := agentproto.ValidateShell(*req.Shell)
		if err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid shell", err.Error(),
				FieldError{Field: "shell", Message: err.Error()})
			return
		}
		if *req.Shell != u.Shell {
			u.Shell = *req.Shell
			shellChanged = true
			shellPath = sp
		}
	}
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		handleStoreErr(w, err)
		return
	}
	if shellChanged {
		_, _ = s.jobs.Enqueue(r.Context(), "user.set_shell", ac.User.ID, map[string]any{
			"username": u.Username,
			"shell":    shellPath,
		})
	}
	s.audit(r, ac.User, "user.update", "user", u.ID, "success", nil)
	WriteJSON(w, http.StatusOK, u)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	u, err := s.store.GetUserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !canManageUser(ac.User, u) || (ac.User.Role != "admin" && ac.User.Role != "reseller") {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if u.ID == ac.User.ID {
		WriteProblem(w, http.StatusConflict, "conflict", "Cannot delete yourself", "")
		return
	}
	if err := s.store.DeleteUser(r.Context(), u.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "user.delete", ac.User.ID, map[string]any{"username": u.Username}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "user.delete", "user", u.ID, "success", map[string]any{"username": u.Username})
	WriteJSON(w, http.StatusOK, map[string]any{"job_id": jobID})
}

func (s *Server) handleSuspendUser(w http.ResponseWriter, r *http.Request) {
	s.setUserStatus(w, r, "suspended", "user.suspend")
}

func (s *Server) handleUnsuspendUser(w http.ResponseWriter, r *http.Request) {
	s.setUserStatus(w, r, "active", "user.unsuspend")
}

func (s *Server) setUserStatus(w http.ResponseWriter, r *http.Request, status, jobType string) {
	ac := GetAuth(r)
	u, err := s.store.GetUserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !canManageUser(ac.User, u) || (ac.User.Role != "admin" && ac.User.Role != "reseller") || u.ID == ac.User.ID {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	u.Status = status
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		handleStoreErr(w, err)
		return
	}
	if status == "suspended" {
		_ = s.store.DeleteAllSessions(r.Context(), u.ID)
	}
	_, _ = s.jobs.Enqueue(r.Context(), jobType, ac.User.ID, map[string]any{"username": u.Username})
	s.audit(r, ac.User, jobType, "user", u.ID, "success", nil)
	WriteJSON(w, http.StatusOK, u)
}
