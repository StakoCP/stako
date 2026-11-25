// SPDX-License-Identifier: MIT
package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

func (s *Server) verifyTOTPOrRecovery(r *http.Request, u *store.User, code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	if u.TOTPSecret != "" {
		if sec, err := s.enc.Decrypt(u.TOTPSecret); err == nil {
			if crypto.VerifyTOTP(sec, code, time.Now().UTC()) {
				return true
			}
		}
	}
	ok, err := s.store.ConsumeRecoveryCode(r.Context(), u.ID, crypto.SHA256Hex(strings.ToLower(code)))
	return err == nil && ok
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request body", err.Error())
		return
	}
	ip := ClientIP(r)
	ipKey := "ip:" + ip
	userKey := "user:" + strings.ToLower(req.Username)
	if !s.limiter.Allow(ipKey) || !s.limiter.Allow(userKey) {
		WriteProblem(w, http.StatusTooManyRequests, "rate-limited", "Too many login attempts", "Please wait a few minutes before trying again")
		return
	}

	u, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil || !crypto.VerifyPassword(req.Password, u.PasswordHash) {
		s.limiter.RecordFailure(ipKey)
		s.limiter.RecordFailure(userKey)
		s.audit(r, u, "auth.login", "user", req.Username, "failure", map[string]any{"reason": "invalid_credentials"})
		WriteProblem(w, http.StatusUnauthorized, "invalid-credentials", "Invalid credentials", "Username or password is incorrect")
		return
	}
	if u.Status != "active" {
		s.audit(r, u, "auth.login", "user", u.ID, "failure", map[string]any{"reason": "suspended"})
		WriteProblem(w, http.StatusForbidden, "forbidden", "Account suspended", "Your account is suspended")
		return
	}
	if s.isLockdown(r.Context()) && u.Role != "admin" {
		s.audit(r, u, "auth.login", "user", u.ID, "failure", map[string]any{"reason": "lockdown"})
		WriteProblem(w, http.StatusForbidden, "forbidden", "Lockdown active", "Panel is in emergency lockdown mode")
		return
	}
	if u.TOTPEnabled {
		if strings.TrimSpace(req.TOTPCode) == "" {
			WriteProblem(w, http.StatusUnauthorized, "totp-required", "Two-factor authentication required", "Enter your 6-digit TOTP code or a recovery code")
			return
		}
		if !s.verifyTOTPOrRecovery(r, u, req.TOTPCode) {
			s.limiter.RecordFailure(ipKey)
			s.limiter.RecordFailure(userKey)
			s.audit(r, u, "auth.login", "user", u.ID, "failure", map[string]any{"reason": "invalid_totp"})
			WriteProblem(w, http.StatusUnauthorized, "invalid-credentials", "Invalid authentication code", "The 2FA code is incorrect")
			return
		}
	}

	s.limiter.Reset(ipKey)
	s.limiter.Reset(userKey)

	rawToken, _ := crypto.RandomHex(32)
	csrfToken, _ := crypto.RandomHex(24)
	sessID, _ := crypto.RandomID("s")
	now := time.Now().UTC()
	sess := &store.Session{
		ID:         sessID,
		UserID:     u.ID,
		TokenHash:  crypto.SHA256Hex(rawToken),
		CSRFToken:  csrfToken,
		IP:         ip,
		UserAgent:  r.UserAgent(),
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(s.absTimeout),
	}
	if err := s.store.CreateSession(r.Context(), sess); err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to create session", "")
		return
	}
	u.LastLoginAt = &now
	_ = s.store.UpdateUser(r.Context(), u)
	s.audit(r, u, "auth.login", "session", sess.ID, "success", nil)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   !s.insecureHTTP,
		SameSite: http.SameSiteStrictMode,
		Expires:  sess.ExpiresAt,
	})
	WriteJSON(w, http.StatusOK, map[string]any{
		"user":       u,
		"csrf_token": csrfToken,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.Session != nil {
		_ = s.store.DeleteSession(r.Context(), ac.Session.ID, ac.User.ID)
		s.audit(r, ac.User, "auth.logout", "session", ac.Session.ID, "success", nil)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !s.insecureHTTP,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	csrf := ""
	if ac.Session != nil {
		csrf = ac.Session.CSRFToken
	}
	twoFactorRequired := (ac.User.Role == "admin" || ac.User.Role == "reseller") && !ac.User.TOTPEnabled && ac.Impersonator == nil
	WriteJSON(w, http.StatusOK, map[string]any{
		"user":                ac.User,
		"impersonator":        ac.Impersonator,
		"csrf_token":          csrf,
		"two_factor_required": twoFactorRequired,
	})
}

func (s *Server) handleReauth(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.Session == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req struct {
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if !crypto.VerifyPassword(req.Password, ac.User.PasswordHash) {
		s.audit(r, ac.User, "auth.reauth", "session", ac.Session.ID, "failure", nil)
		WriteProblem(w, http.StatusUnauthorized, "invalid-credentials", "Invalid password", "")
		return
	}
	if ac.User.TOTPEnabled && !s.verifyTOTPOrRecovery(r, ac.User, req.TOTPCode) {
		s.audit(r, ac.User, "auth.reauth", "session", ac.Session.ID, "failure", map[string]any{"reason": "totp"})
		WriteProblem(w, http.StatusUnauthorized, "invalid-credentials", "Invalid 2FA code", "")
		return
	}
	_ = s.store.SetSessionReauth(r.Context(), ac.Session.ID, time.Now().UTC().Add(5*time.Minute))
	s.audit(r, ac.User, "auth.reauth", "session", ac.Session.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if !crypto.VerifyPassword(req.CurrentPassword, ac.User.PasswordHash) {
		WriteProblem(w, http.StatusUnauthorized, "invalid-credentials", "Current password is incorrect", "")
		return
	}
	if err := crypto.ValidatePasswordStrength(req.NewPassword); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Weak password", err.Error(),
			FieldError{Field: "new_password", Message: err.Error()})
		return
	}
	hash, err := crypto.HashPassword(req.NewPassword)
	if err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to hash password", "")
		return
	}
	ac.User.PasswordHash = hash
	if err := s.store.UpdateUser(r.Context(), ac.User); err != nil {
		handleStoreErr(w, err)
		return
	}
	if ac.Session != nil {
		_ = s.store.DeleteOtherSessions(r.Context(), ac.User.ID, ac.Session.ID)
	} else {
		_ = s.store.DeleteAllSessions(r.Context(), ac.User.ID)
	}
	s.audit(r, ac.User, "auth.password_change", "user", ac.User.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	secret, err := crypto.GenerateTOTPSecret()
	if err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to generate secret", "")
		return
	}
	encSecret, err := s.enc.Encrypt(secret)
	if err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to encrypt secret", "")
		return
	}
	ac.User.TOTPSecret = encSecret
	if err := s.store.UpdateUser(r.Context(), ac.User); err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"secret":      secret,
		"otpauth_url": crypto.OTPAuthURL("Stako", ac.User.Username, secret),
	})
}

func (s *Server) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var req struct {
		Code string `json:"code"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if ac.User.TOTPSecret == "" {
		WriteProblem(w, http.StatusBadRequest, "validation", "2FA not initialized", "Call /auth/2fa/setup first")
		return
	}
	sec, err := s.enc.Decrypt(ac.User.TOTPSecret)
	if err != nil || !crypto.VerifyTOTP(sec, req.Code, time.Now().UTC()) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid TOTP code", "Code did not match",
			FieldError{Field: "code", Message: "Invalid 6-digit code"})
		return
	}
	codes, err := crypto.GenerateRecoveryCodes(8)
	if err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to generate recovery codes", "")
		return
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = crypto.SHA256Hex(strings.ToLower(c))
	}
	if err := s.store.ReplaceRecoveryCodes(r.Context(), ac.User.ID, hashes); err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to save recovery codes", "")
		return
	}
	ac.User.TOTPEnabled = true
	_ = s.store.UpdateUser(r.Context(), ac.User)
	s.audit(r, ac.User, "auth.2fa_enable", "user", ac.User.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (s *Server) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	var req struct {
		Code string `json:"code"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if !s.verifyTOTPOrRecovery(r, ac.User, req.Code) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid 2FA code", "")
		return
	}
	ac.User.TOTPEnabled = false
	ac.User.TOTPSecret = ""
	_ = s.store.UpdateUser(r.Context(), ac.User)
	_ = s.store.ReplaceRecoveryCodes(r.Context(), ac.User.ID, nil)
	s.audit(r, ac.User, "auth.2fa_disable", "user", ac.User.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	curr := ""
	if ac.Session != nil {
		curr = ac.Session.ID
	}
	items, err := s.store.ListSessionsByUser(r.Context(), ac.User.ID, curr)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	id := r.PathValue("id")
	if err := s.store.DeleteSession(r.Context(), id, ac.User.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "auth.session_revoke", "session", id, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	keep := ""
	if ac.Session != nil {
		keep = ac.Session.ID
	}
	if err := s.store.DeleteOtherSessions(r.Context(), ac.User.ID, keep); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "auth.sessions_revoke_others", "user", ac.User.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}
