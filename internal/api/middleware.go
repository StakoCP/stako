// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

type ctxKey string

const (
	ctxAuthKey   ctxKey = "auth"
	ctxCorrIDKey ctxKey = "corr_id"
	sessionCookieName   = "stako_session"
)

type AuthContext struct {
	User         *store.User
	Impersonator *store.User    // original admin/reseller when acting as another user
	Session      *store.Session // nil when authenticated via API key
	APIKey       *store.ApiKey  // nil when authenticated via session
}

func GetAuth(r *http.Request) *AuthContext {
	v, _ := r.Context().Value(ctxAuthKey).(*AuthContext)
	return v
}

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corrID, _ := crypto.RandomHex(8)
		ctx := context.WithValue(r.Context(), ctxCorrIDKey, corrID)
		w.Header().Set("X-Request-Id", corrID)
		if !strings.HasPrefix(r.URL.Path, "/phpmyadmin") && !strings.HasPrefix(r.URL.Path, "/phppgadmin") && !strings.HasPrefix(r.URL.Path, "/webmail") {
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if !s.insecureHTTP {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r.WithContext(ctx))
		s.logger.LogAttrs(ctx, slog.LevelInfo, "http_request",
			slog.String("corr_id", corrID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rw.status),
			slog.String("ip", ClientIP(r)),
			slog.Duration("dur", time.Since(start)),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			s.authWithAPIKey(w, r, token, next)
			return
		}
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "Authentication required", "Please sign in")
			return
		}
		sess, err := s.store.GetSessionByTokenHash(r.Context(), crypto.SHA256Hex(c.Value))
		if err != nil {
			WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "Session invalid", "Please sign in again")
			return
		}
		now := time.Now().UTC()
		if now.After(sess.ExpiresAt) || now.Sub(sess.LastSeenAt) > s.idleTimeout {
			_ = s.store.DeleteSession(r.Context(), sess.ID, sess.UserID)
			WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "Session expired", "Please sign in again")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			csrf := r.Header.Get("X-CSRF-Token")
			if csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(sess.CSRFToken)) != 1 {
				WriteProblem(w, http.StatusForbidden, "csrf", "CSRF validation failed", "Missing or invalid X-CSRF-Token header")
				return
			}
		}
		u, err := s.store.GetUserByID(r.Context(), sess.UserID)
		if err != nil || u.Status != "active" {
			WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "Account unavailable", "")
			return
		}
		if s.isLockdown(r.Context()) && u.Role != "admin" {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Lockdown active", "Panel is in emergency lockdown mode")
			return
		}
		ac := &AuthContext{User: u, Session: sess}
		if sess.ActingAsUserID != nil {
			target, err := s.store.GetUserByID(r.Context(), *sess.ActingAsUserID)
			if err != nil || target.Status != "active" {
				_ = s.store.EndImpersonation(r.Context(), sess.ID, sess.ImpersonationID)
			} else {
				ac.Impersonator = u
				ac.User = target
			}
		}
		_ = s.store.TouchSession(r.Context(), sess.ID, now)
		ctx := context.WithValue(r.Context(), ctxAuthKey, ac)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) authWithAPIKey(w http.ResponseWriter, r *http.Request, raw string, next http.HandlerFunc) {
	k, err := s.store.GetApiKeyByHash(r.Context(), crypto.SHA256Hex(raw))
	if err != nil {
		WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "Invalid API key", "")
		return
	}
	now := time.Now().UTC()
	if k.ExpiresAt != nil && now.After(*k.ExpiresAt) {
		WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "API key expired", "")
		return
	}
	if len(k.AllowedIPs) > 0 && !ipAllowed(ClientIP(r), k.AllowedIPs) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "IP not allowed", "Your IP is not in this API key allowlist")
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if !slices.Contains(k.Scopes, "read") && !slices.Contains(k.Scopes, "write") {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Insufficient API key scope", "")
			return
		}
	} else if !slices.Contains(k.Scopes, "write") {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Write scope required", "")
		return
	}
	u, err := s.store.GetUserByID(r.Context(), k.UserID)
	if err != nil || u.Status != "active" {
		WriteProblem(w, http.StatusUnauthorized, "unauthenticated", "Account unavailable", "")
		return
	}
	s.store.TouchApiKey(r.Context(), k.ID, now)
	ctx := context.WithValue(r.Context(), ctxAuthKey, &AuthContext{User: u, APIKey: k})
	next(w, r.WithContext(ctx))
}

func ipAllowed(ipStr string, cidrs []string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, entry := range cidrs {
		if strings.Contains(entry, "/") {
			if _, netw, err := net.ParseCIDR(entry); err == nil && netw.Contains(ip) {
				return true
			}
		} else if parsed := net.ParseIP(entry); parsed != nil && parsed.Equal(ip) {
			return true
		}
	}
	return false
}

func (s *Server) requireReauth(w http.ResponseWriter, r *http.Request) bool {
	ac := GetAuth(r)
	if ac == nil {
		return false
	}
	// API keys cannot perform step-up reauth; only interactive sessions or allow if write-scoped?
	// Per PRD SEC-A5, sensitive actions require step-up re-authentication on session.
	if ac.Session == nil {
		return true
	}
	if ac.Session.ReauthUntil == nil || time.Now().UTC().After(*ac.Session.ReauthUntil) {
		WriteProblem(w, http.StatusForbidden, "reauth-required", "Re-authentication required", "Please confirm your password/2FA to continue")
		return false
	}
	return true
}

func (s *Server) audit(r *http.Request, actor *store.User, action, objType, objID, result string, detail map[string]any) {
	var raw json.RawMessage
	if detail != nil {
		raw, _ = json.Marshal(detail)
	}
	actorID := ""
	actorName := "anonymous"
	var actingAs *string
	if ac := GetAuth(r); ac != nil && ac.Impersonator != nil {
		actorID = ac.Impersonator.ID
		actorName = ac.Impersonator.Username
		if actor != nil {
			actingAs = &actor.Username
		}
	} else if actor != nil {
		actorID = actor.ID
		actorName = actor.Username
	}
	_ = s.store.AppendAudit(r.Context(), &store.AuditEntry{
		ActorID:       actorID,
		ActorUsername: actorName,
		ActingAs:      actingAs,
		Action:        action,
		ObjectType:    objType,
		ObjectID:      objID,
		Result:        result,
		IP:            ClientIP(r),
		UserAgent:     r.UserAgent(),
		Detail:        raw,
	})
}

func handleStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		WriteProblem(w, http.StatusNotFound, "not-found", "Resource not found", "")
	case errors.Is(err, store.ErrConflict):
		WriteProblem(w, http.StatusConflict, "conflict", "Resource conflict", "Already exists or is in use")
	default:
		WriteProblem(w, http.StatusInternalServerError, "internal", "Internal server error", "")
	}
}
