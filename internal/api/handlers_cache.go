// SPDX-License-Identifier: MIT
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

func (s *Server) handleGetDomainCache(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	dom, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, dom.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	cfg, err := s.store.GetDomainCacheConfig(r.Context(), dom.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	cfg.UserID = dom.UserID
	WriteJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSetNginxMicrocache(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	dom, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, dom.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
		TTLSec  int  `json:"ttl_sec"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.TTLSec <= 0 {
		req.TTLSec = 10
	}
	if req.TTLSec > 3600 {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "ttl_sec must be 1..3600", "")
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), dom.UserID)
	uname := ""
	if owner != nil {
		uname = owner.Username
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdNginxMicrocacheApply, map[string]any{
		"username": uname,
		"domain":   dom.Domain,
		"enabled":  req.Enabled,
		"ttl_sec":  req.TTLSec,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to apply Nginx microcache", msg)
		return
	}

	cfg, _ := s.store.GetDomainCacheConfig(r.Context(), dom.ID)
	cfg.UserID = dom.UserID
	cfg.MicrocacheEnabled = req.Enabled
	cfg.MicrocacheTTLSec = req.TTLSec
	if err := s.store.UpsertDomainCacheConfig(r.Context(), cfg); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "cache.microcache_update", "domain", dom.ID, "success", map[string]any{"enabled": req.Enabled, "ttl_sec": req.TTLSec})
	WriteJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleToggleRedisObjectCache(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	dom, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, dom.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	owner, _ := s.store.GetUserByID(r.Context(), dom.UserID)
	uname := ""
	if owner != nil {
		uname = owner.Username
	}
	cfg, _ := s.store.GetDomainCacheConfig(r.Context(), dom.ID)
	cfg.UserID = dom.UserID

	if req.Enabled && cfg.RedisPort == 0 {
		// Reuse existing Redis instance owned by user, or auto-provision one in one click
		existing, _ := s.store.ListRedis(r.Context(), &dom.UserID, nil)
		if len(existing) > 0 {
			cfg.RedisInstanceID = existing[0].ID
			cfg.RedisPort = existing[0].Port
		} else {
			port, err := s.store.NextFreeRedisPort(r.Context())
			if err != nil {
				WriteProblem(w, http.StatusConflict, "conflict", "No free Redis port", "")
				return
			}
			rid, _ := crypto.RandomID("rd")
			pass, _ := crypto.RandomHex(16)
			rname := strings.ReplaceAll(uname, "-", "_") + "_cache"
			inst := &store.RedisInstance{
				ID: rid, UserID: dom.UserID, Name: rname,
				Port: port, MemoryMB: 128, Persistent: false, Status: "active",
			}
			if err := s.store.CreateRedis(r.Context(), inst); err != nil {
				handleStoreErr(w, err)
				return
			}
			_, _ = s.jobs.Enqueue(r.Context(), "redis.create", ac.User.ID, map[string]any{
				"name": inst.Name, "port": inst.Port, "memory_mb": inst.MemoryMB, "persistent": false, "password": pass,
			})
			cfg.RedisInstanceID = inst.ID
			cfg.RedisPort = inst.Port
		}
	}

	resp, err := s.agent.Call(r.Context(), agentproto.CmdRedisObjectCacheApply, map[string]any{
		"username":   uname,
		"domain":     dom.Domain,
		"docroot":    dom.Docroot,
		"enabled":    req.Enabled,
		"redis_port": cfg.RedisPort,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to configure Redis object cache", msg)
		return
	}

	cfg.RedisObjectCache = req.Enabled
	if err := s.store.UpsertDomainCacheConfig(r.Context(), cfg); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "cache.redis_object_update", "domain", dom.ID, "success", map[string]any{"enabled": req.Enabled, "port": cfg.RedisPort})
	WriteJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSetCloudflareCDN(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	dom, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, dom.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var req struct {
		CFZoneID      string `json:"cf_zone_id"`
		CFAPIToken    string `json:"cf_api_token"`
		CFUnderAttack bool   `json:"cf_under_attack"`
		CFDevMode     bool   `json:"cf_dev_mode"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	cfg, _ := s.store.GetDomainCacheConfig(r.Context(), dom.ID)
	cfg.UserID = dom.UserID
	cfg.CFZoneID = strings.TrimSpace(req.CFZoneID)
	plainToken := ""
	if req.CFAPIToken != "" {
		enc, err := s.enc.Encrypt(req.CFAPIToken)
		if err != nil {
			WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to encrypt Cloudflare token", err.Error())
			return
		}
		cfg.CFAPITokenEnc = enc
		cfg.HasCFToken = true
		plainToken = req.CFAPIToken
	} else if cfg.CFAPITokenEnc != "" {
		if dec, err := s.enc.Decrypt(cfg.CFAPITokenEnc); err == nil {
			plainToken = dec
		}
	}

	// Apply security_level & development_mode via Cloudflare API when real token & zone ID are configured
	if cfg.CFZoneID != "" && plainToken != "" && !strings.HasPrefix(plainToken, "test-") {
		secLevel := "medium"
		if req.CFUnderAttack {
			secLevel = "under_attack"
		}
		if err := callCloudflarePatch(r.Context(), cfg.CFZoneID, plainToken, "settings/security_level", map[string]any{"value": secLevel}); err != nil {
			WriteProblem(w, http.StatusBadGateway, "upstream", "Cloudflare security_level update failed", err.Error())
			return
		}
		devVal := "off"
		if req.CFDevMode {
			devVal = "on"
		}
		if err := callCloudflarePatch(r.Context(), cfg.CFZoneID, plainToken, "settings/development_mode", map[string]any{"value": devVal}); err != nil {
			WriteProblem(w, http.StatusBadGateway, "upstream", "Cloudflare development_mode update failed", err.Error())
			return
		}
	}

	cfg.CFUnderAttack = req.CFUnderAttack
	cfg.CFDevMode = req.CFDevMode
	if err := s.store.UpsertDomainCacheConfig(r.Context(), cfg); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "cache.cloudflare_update", "domain", dom.ID, "success", map[string]any{
		"under_attack": cfg.CFUnderAttack,
		"dev_mode":     cfg.CFDevMode,
	})
	WriteJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePurgeCloudflareCache(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role == "readonly" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	dom, err := s.store.GetDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !s.canAccessOwned(r, ac, dom.UserID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	cfg, _ := s.store.GetDomainCacheConfig(r.Context(), dom.ID)
	if cfg.CFZoneID == "" || cfg.CFAPITokenEnc == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Cloudflare Zone ID and API Token must be configured first", "")
		return
	}
	plainToken, err := s.enc.Decrypt(cfg.CFAPITokenEnc)
	if err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to decrypt Cloudflare token", err.Error())
		return
	}
	if !strings.HasPrefix(plainToken, "test-") {
		if err := callCloudflarePost(r.Context(), cfg.CFZoneID, plainToken, "purge_cache", map[string]any{"purge_everything": true}); err != nil {
			WriteProblem(w, http.StatusBadGateway, "upstream", "Cloudflare purge_cache failed", err.Error())
			return
		}
	}
	s.audit(r, ac.User, "cache.cloudflare_purge", "domain", dom.ID, "success", map[string]any{"zone_id": cfg.CFZoneID})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "purged": true})
}

func callCloudflarePatch(ctx context.Context, zoneID, token, endpoint string, body map[string]any) error {
	return callCloudflareAPI(ctx, http.MethodPatch, zoneID, token, endpoint, body)
}

func callCloudflarePost(ctx context.Context, zoneID, token, endpoint string, body map[string]any) error {
	return callCloudflareAPI(ctx, http.MethodPost, zoneID, token, endpoint, body)
}

func callCloudflareAPI(ctx context.Context, method, zoneID, token, endpoint string, body map[string]any) error {
	b, _ := json.Marshal(body)
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/%s", zoneID, endpoint)
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("cloudflare API returned %s", resp.Status)
	}
	return nil
}
