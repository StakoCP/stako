// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/StakoCP/stako/internal/catalog"
	"github.com/StakoCP/stako/internal/store"
)

var pluginIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,48}$`)

func (s *Server) syncPluginCatalog(ctx context.Context) {
	// Also load any drop-in community manifests from /etc/stako/plugins/*.json (or STAKO_PLUGINS_DIR)
	pluginsDir := os.Getenv("STAKO_PLUGINS_DIR")
	if pluginsDir == "" {
		pluginsDir = "/etc/stako/plugins"
	}
	if entries, err := os.ReadDir(pluginsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(pluginsDir, e.Name())); err == nil {
				var p store.Plugin
				if json.Unmarshal(b, &p) == nil && pluginIDRe.MatchString(p.ID) && p.Name != "" {
					if _, err := s.store.GetPlugin(ctx, p.ID); err != nil {
						p.Enabled = true
						_ = s.store.UpsertPlugin(ctx, &p)
					}
				}
			}
		}
	}

	plugins, err := s.store.ListPlugins(ctx)
	if err != nil {
		return
	}
	var apps []catalog.AppManifest
	for _, p := range plugins {
		if !p.Enabled {
			continue
		}
		apps = append(apps, p.Apps...)
	}
	catalog.SetExternalApps(apps)
}

func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	s.syncPluginCatalog(r.Context())
	items, err := s.store.ListPlugins(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleListPluginPages(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	s.syncPluginCatalog(r.Context())
	plugins, err := s.store.ListPlugins(r.Context())
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	var pages []map[string]any
	for _, p := range plugins {
		if !p.Enabled {
			continue
		}
		for _, pg := range p.Pages {
			if len(pg.Roles) > 0 && !slices.Contains(pg.Roles, ac.User.Role) {
				continue
			}
			pages = append(pages, map[string]any{
				"plugin_id":   p.ID,
				"plugin_name": p.Name,
				"slug":        pg.Slug,
				"title":       pg.Title,
				"url":         pg.URL,
			})
		}
	}
	if pages == nil {
		pages = []map[string]any{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": pages})
}

func (s *Server) handleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var p store.Plugin
	if err := DecodeJSON(r, &p); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid plugin manifest JSON", err.Error())
		return
	}
	p.ID = strings.ToLower(strings.TrimSpace(p.ID))
	p.Name = strings.TrimSpace(p.Name)
	p.Version = strings.TrimSpace(p.Version)
	if !pluginIDRe.MatchString(p.ID) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid plugin id (lowercase alphanumeric & hyphens, 2..49 chars)", "")
		return
	}
	if p.Name == "" || p.Version == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Plugin name and version are required", "")
		return
	}
	for _, h := range p.Hooks {
		if err := validateWebhookURL(h.URL); err != nil {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid hook URL: "+h.URL, err.Error())
			return
		}
	}
	for _, pg := range p.Pages {
		u, err := url.ParseRequestURI(pg.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid page URL: "+pg.URL, "")
			return
		}
	}
	p.Enabled = true
	if err := s.store.UpsertPlugin(r.Context(), &p); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncPluginCatalog(r.Context())
	s.audit(r, ac.User, "plugin.install", "plugin", p.ID, "success", map[string]any{"version": p.Version})
	WriteJSON(w, http.StatusCreated, p)
}

func (s *Server) handleTogglePlugin(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if err := s.store.SetPluginEnabled(r.Context(), r.PathValue("id"), req.Enabled); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncPluginCatalog(r.Context())
	s.audit(r, ac.User, "plugin.toggle", "plugin", r.PathValue("id"), "success", map[string]any{"enabled": req.Enabled})
	WriteJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "enabled": req.Enabled})
}

func (s *Server) handleDeletePlugin(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	if err := s.store.DeletePlugin(r.Context(), r.PathValue("id")); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.syncPluginCatalog(r.Context())
	s.audit(r, ac.User, "plugin.delete", "plugin", r.PathValue("id"), "success", nil)
	w.WriteHeader(http.StatusNoContent)
}
