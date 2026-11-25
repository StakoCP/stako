// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"net/http"

	"github.com/StakoCP/stako/internal/version"
	"gopkg.in/yaml.v3"
)

func buildOpenAPISpec() map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Stako Control Panel API",
			"version": version.Version,
			"license": map[string]any{"name": "MIT"},
		},
		"servers": []map[string]any{{"url": "/api/v1"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"},
				"cookieAuth": map[string]any{"type": "apiKey", "in": "cookie", "name": "stako_session"},
			},
		},
		"paths": map[string]any{
			"/health":           map[string]any{"get": map[string]any{"summary": "Health check"}},
			"/auth/login":       map[string]any{"post": map[string]any{"summary": "Authenticate user"}},
			"/auth/logout":      map[string]any{"post": map[string]any{"summary": "Sign out"}},
			"/auth/me":          map[string]any{"get": map[string]any{"summary": "Current user & CSRF token"}},
			"/auth/reauth":      map[string]any{"post": map[string]any{"summary": "Step-up re-authentication"}},
			"/auth/password":    map[string]any{"post": map[string]any{"summary": "Change own password"}},
			"/auth/2fa/setup":   map[string]any{"post": map[string]any{"summary": "Initialize TOTP 2FA"}},
			"/auth/2fa/enable":  map[string]any{"post": map[string]any{"summary": "Enable TOTP 2FA"}},
			"/auth/2fa/disable": map[string]any{"post": map[string]any{"summary": "Disable TOTP 2FA"}},
			"/auth/sessions":    map[string]any{"get": map[string]any{"summary": "List active sessions"}},
			"/users": map[string]any{
				"get":  map[string]any{"summary": "List users"},
				"post": map[string]any{"summary": "Create user"},
			},
			"/users/{id}": map[string]any{
				"get":    map[string]any{"summary": "Get user"},
				"patch":  map[string]any{"summary": "Update user"},
				"delete": map[string]any{"summary": "Delete user"},
			},
			"/users/{id}/suspend":   map[string]any{"post": map[string]any{"summary": "Suspend user"}},
			"/users/{id}/unsuspend": map[string]any{"post": map[string]any{"summary": "Unsuspend user"}},
			"/packages": map[string]any{
				"get":  map[string]any{"summary": "List packages"},
				"post": map[string]any{"summary": "Create package"},
			},
			"/packages/{id}": map[string]any{
				"get":    map[string]any{"summary": "Get package"},
				"patch":  map[string]any{"summary": "Update package"},
				"delete": map[string]any{"summary": "Delete package"},
			},
			"/api-keys": map[string]any{
				"get":  map[string]any{"summary": "List API keys"},
				"post": map[string]any{"summary": "Create API key"},
			},
			"/api-keys/{id}": map[string]any{"delete": map[string]any{"summary": "Revoke API key"}},
			"/audit":         map[string]any{"get": map[string]any{"summary": "List audit log entries"}},
			"/audit/verify":  map[string]any{"get": map[string]any{"summary": "Verify audit log hash chain"}},
			"/jobs":          map[string]any{"get": map[string]any{"summary": "List background jobs"}},
			"/jobs/{id}":     map[string]any{"get": map[string]any{"summary": "Get job status and log"}},
			"/system/info":   map[string]any{"get": map[string]any{"summary": "System status and metrics"}},
		},
	}
}

func (s *Server) handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(buildOpenAPISpec())
}

func (s *Server) handleOpenAPIYAML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	out, _ := yaml.Marshal(buildOpenAPISpec())
	_, _ = w.Write(out)
}
