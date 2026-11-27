// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

func (s *Server) handleListWPSites(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	items, err := s.store.ListWPSites(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleScanWPSites(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var userFilter, resellerFilter *string
	switch ac.User.Role {
	case "admin":
	case "reseller":
		resellerFilter = &ac.User.ID
	default:
		userFilter = &ac.User.ID
	}
	doms, _, err := s.store.ListDomains(r.Context(), userFilter, resellerFilter, "", 200, 0)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	domInputs := make([]map[string]any, 0, len(doms))
	userCache := map[string]string{}
	for _, d := range doms {
		uname := userCache[d.UserID]
		if uname == "" {
			if u, err := s.store.GetUserByID(r.Context(), d.UserID); err == nil {
				uname = u.Username
				userCache[d.UserID] = uname
			}
		}
		domInputs = append(domInputs, map[string]any{
			"domain_id":   d.ID,
			"user_id":     d.UserID,
			"username":    uname,
			"domain":      d.Domain,
			"docroot":     d.Docroot,
			"template":    d.Template,
			"php_version": d.PHPVersion,
		})
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdWPScan, map[string]any{
		"domains": domInputs,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to scan WordPress sites", msg)
		return
	}

	if rawSites, ok := resp.Data["sites"].([]any); ok {
		for _, item := range rawSites {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			domID, _ := m["domain_id"].(string)
			userID, _ := m["user_id"].(string)
			dom, _ := m["domain"].(string)
			path, _ := m["path"].(string)
			wpVer, _ := m["wp_version"].(string)
			phpVer, _ := m["php_version"].(string)
			adminURL, _ := m["admin_url"].(string)
			uCore := intVal(m["updates_core"])
			uPlug := intVal(m["updates_plugins"])
			uTheme := intVal(m["updates_themes"])
			secScore := intVal(m["security_score"])
			issuesB, _ := json.Marshal(m["security_issues"])

			id, _ := crypto.RandomID("wp")
			_ = s.store.UpsertWPSite(r.Context(), &store.WPSite{
				ID:             id,
				UserID:         userID,
				DomainID:       domID,
				Domain:         dom,
				Path:           path,
				WPVersion:      wpVer,
				PHPVersion:     phpVer,
				AdminURL:       adminURL,
				UpdatesCore:    uCore,
				UpdatesPlugins: uPlug,
				UpdatesThemes:  uTheme,
				SecurityScore:  secScore,
				SecurityIssues: string(issuesB),
			})
		}
	} else if resp.DryRun {
		for _, d := range doms {
			if d.Template == "wordpress" || strings.Contains(strings.ToLower(d.Domain), "wp") || strings.Contains(strings.ToLower(d.Domain), "blog") {
				id, _ := crypto.RandomID("wp")
				_ = s.store.UpsertWPSite(r.Context(), &store.WPSite{
					ID:             id,
					UserID:         d.UserID,
					DomainID:       d.ID,
					Domain:         d.Domain,
					Path:           d.Docroot,
					WPVersion:      "6.7.1",
					PHPVersion:     d.PHPVersion,
					AdminURL:       "https://" + d.Domain + "/wp-admin/",
					UpdatesPlugins: 2,
					UpdatesThemes:  1,
					SecurityScore:  75,
					SecurityIssues: `[{"id":"wp_file_edit","severity":"medium","title":"DISALLOW_FILE_EDIT is not enabled","fixed":false}]`,
				})
			}
		}
	}

	items, err := s.store.ListWPSites(r.Context(), userFilter, resellerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "wordpress.scan", "wordpress", "scan", "success", map[string]any{"count": len(items)})
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func intVal(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	default:
		return 0
	}
}

func (s *Server) handleBulkUpdateWP(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		SiteIDs []string `json:"site_ids"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if len(req.SiteIDs) == 0 {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "site_ids is required", "")
		return
	}
	var jobIDs []string
	for _, id := range req.SiteIDs {
		site, err := s.store.GetWPSite(r.Context(), id)
		if err != nil || !s.canAccessOwned(r, ac, site.UserID) {
			continue
		}
		uname := ""
		if u, err := s.store.GetUserByID(r.Context(), site.UserID); err == nil {
			uname = u.Username
		}
		if j, err := s.jobs.Enqueue(r.Context(), "wp.update", ac.User.ID, map[string]any{
			"site_id":  site.ID,
			"path":     site.Path,
			"username": uname,
		}); err == nil {
			jobIDs = append(jobIDs, j.ID)
		}
		s.store.UpdateWPSiteUpdates(r.Context(), site.ID, 0, 0, 0)
	}
	s.audit(r, ac.User, "wordpress.bulk_update", "wordpress", "bulk", "success", map[string]any{"sites": len(jobIDs)})
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_ids": jobIDs, "updated_count": len(jobIDs)})
}

func (s *Server) handleCreateWPStaging(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	prod, err := s.store.GetWPSite(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, prod.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		StagingDomain string `json:"staging_domain"`
	}
	_ = DecodeJSON(r, &req)
	stgDomain := strings.ToLower(strings.TrimSpace(req.StagingDomain))
	if stgDomain == "" {
		stgDomain = "staging." + prod.Domain
	}
	if err := agentproto.ValidateDomain(stgDomain); err != nil {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Invalid staging_domain", err.Error())
		return
	}

	owner, err := s.store.GetUserByID(r.Context(), prod.UserID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	stgPath := "/home/" + owner.Username + "/web/" + stgDomain + "/public_html"
	domID, _ := crypto.RandomID("d")
	stgDomObj := &store.Domain{
		ID:         domID,
		UserID:     owner.ID,
		Domain:     stgDomain,
		Stack:      "nginx_apache",
		Template:   "wordpress",
		Docroot:    stgPath,
		PHPVersion: prod.PHPVersion,
		SSLMode:    "selfsigned",
		Status:     "active",
	}
	if err := s.store.CreateDomain(r.Context(), stgDomObj); err != nil {
		handleStoreErr(w, err)
		return
	}
	_ = s.enqueueDomainApply(r, ac.User.ID, stgDomObj, owner)

	stgID, _ := crypto.RandomID("wp")
	stgSite := &store.WPSite{
		ID:             stgID,
		UserID:         owner.ID,
		DomainID:       stgDomObj.ID,
		Domain:         stgDomain,
		Path:           stgPath,
		WPVersion:      prod.WPVersion,
		PHPVersion:     prod.PHPVersion,
		AdminURL:       "https://" + stgDomain + "/wp-admin/",
		IsStaging:      true,
		ParentID:       prod.ID,
		SecurityScore:  prod.SecurityScore,
		SecurityIssues: prod.SecurityIssues,
	}
	if err := s.store.UpsertWPSite(r.Context(), stgSite); err != nil {
		handleStoreErr(w, err)
		return
	}

	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "wp.staging", ac.User.ID, map[string]any{
		"action":     "create",
		"src_path":   prod.Path,
		"dst_path":   stgPath,
		"src_domain": prod.Domain,
		"dst_domain": stgDomain,
		"username":   owner.Username,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "wordpress.staging_create", "wordpress", stgSite.ID, "success", map[string]any{"domain": stgDomain})
	WriteJSON(w, http.StatusCreated, map[string]any{"site": stgSite, "job_id": jobID})
}

func (s *Server) handlePushWPStaging(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	stg, err := s.store.GetWPSite(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, stg.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if !stg.IsStaging || stg.ParentID == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Site is not a staging installation", "")
		return
	}
	prod, err := s.store.GetWPSite(r.Context(), stg.ParentID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), stg.UserID)
	uname := ""
	if owner != nil {
		uname = owner.Username
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "wp.staging", ac.User.ID, map[string]any{
		"action":     "push",
		"src_path":   stg.Path,
		"dst_path":   prod.Path,
		"src_domain": stg.Domain,
		"dst_domain": prod.Domain,
		"username":   uname,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "wordpress.staging_push", "wordpress", stg.ID, "success", map[string]any{"target": prod.Domain})
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}

func (s *Server) handleHardenWPSite(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	site, err := s.store.GetWPSite(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, site.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdWPSecurityHarden, map[string]any{
		"path": site.Path,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to harden WordPress site", msg)
		return
	}
	s.store.UpdateWPSiteSecurity(r.Context(), site.ID, 100, "[]")
	s.audit(r, ac.User, "wordpress.harden", "wordpress", site.ID, "success", map[string]any{"domain": site.Domain})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "security_score": 100})
}

func (s *Server) handleAutoLoginWPSite(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	site, err := s.store.GetWPSite(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, site.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	token, _ := crypto.RandomHex(16)
	resp, err := s.agent.Call(r.Context(), agentproto.CmdWPAutoLogin, map[string]any{
		"path":   site.Path,
		"domain": site.Domain,
		"token":  token,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to generate WordPress auto-login", msg)
		return
	}
	s.audit(r, ac.User, "wordpress.autologin", "wordpress", site.ID, "success", map[string]any{"domain": site.Domain})
	data := resp.Data
	if _, ok := data["login_url"]; !ok && resp.DryRun {
		data = map[string]any{
			"login_url":  "https://" + site.Domain + "/_stako_login_" + token[:12] + ".php?token=" + token,
			"expires_in": 60,
		}
	}
	WriteJSON(w, http.StatusOK, data)
}

func (s *Server) handleDeleteWPSite(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	site, err := s.store.GetWPSite(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, site.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeleteWPSite(r.Context(), site.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "wordpress.delete", "wordpress", site.ID, "success", map[string]any{"domain": site.Domain})
	w.WriteHeader(http.StatusNoContent)
}
