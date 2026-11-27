// SPDX-License-Identifier: MIT
package api

import (
	"hash/fnv"
	"net/http"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/store"
)

func projectIDForUser(username string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(username))
	return 10000 + int(h.Sum32()%50000)
}

func (s *Server) handleHardwareHealth(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdHardwareHealth, nil)
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to collect hardware health", msg)
		return
	}
	if resp.DryRun && resp.Data["disks"] == nil {
		WriteJSON(w, http.StatusOK, map[string]any{
			"disks": []map[string]any{
				{"device": "/dev/sda", "model": "QEMU HARDDISK", "size": "80G", "health_status": "PASSED", "temperature_c": 34, "power_on_hours": 1420},
			},
			"inodes": []map[string]any{
				{"filesystem": "/dev/sda1", "mount": "/", "inodes_total": 5242880, "inodes_used": 215400, "inodes_free": 5027480, "inodes_pct": 4.1},
			},
			"pressure": map[string]any{
				"mem_total_mb": 4096, "mem_used_mb": 1640, "mem_pct": 40.0,
				"swap_total_mb": 2048, "swap_used_mb": 128, "swap_pct": 6.2,
				"psi_mem_avg10": 0.12, "psi_io_avg10": 0.05,
			},
		})
		return
	}
	WriteJSON(w, http.StatusOK, resp.Data)
}

func (s *Server) handleListUserQuotas(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	users, _, err := s.store.ListUsers(r.Context(), nil, "", 200, 0)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	existing, _ := s.store.ListUserDiskQuotas(r.Context())
	byID := map[string]store.UserDiskQuota{}
	for _, q := range existing {
		byID[q.UserID] = q
	}
	usernames := make([]string, 0, len(users))
	for _, u := range users {
		usernames = append(usernames, u.Username)
	}
	liveUsage := map[string]map[string]int{}
	if resp, err := s.agent.Call(r.Context(), agentproto.CmdUserQuotaReport, map[string]any{"usernames": usernames}); err == nil && resp.OK {
		if rawUsage, ok := resp.Data["usage"].(map[string]any); ok {
			for k, v := range rawUsage {
				if vm, ok := v.(map[string]any); ok {
					liveUsage[k] = map[string]int{
						"disk_used_mb": intVal(vm["disk_used_mb"]),
						"inode_used":   intVal(vm["inode_used"]),
					}
				}
			}
		}
	}

	items := make([]store.UserDiskQuota, 0, len(users))
	for _, u := range users {
		q, ok := byID[u.ID]
		if !ok {
			q = store.UserDiskQuota{
				UserID:    u.ID,
				Username:  u.Username,
				ProjectID: projectIDForUser(u.Username),
			}
		}
		if lu, ok := liveUsage[u.Username]; ok {
			q.DiskUsedMB = lu["disk_used_mb"]
			q.InodeUsed = lu["inode_used"]
		}
		items = append(items, q)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleSetUserQuota(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	u, err := s.store.GetUserByID(r.Context(), r.PathValue("userId"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	var req struct {
		DiskLimitMB int `json:"disk_limit_mb"`
		InodeLimit  int `json:"inode_limit"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.DiskLimitMB < 0 || req.InodeLimit < 0 {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Limits must be >= 0", "")
		return
	}
	projID := projectIDForUser(u.Username)
	if prev, err := s.store.GetUserDiskQuota(r.Context(), u.ID); err == nil && prev.ProjectID > 0 {
		projID = prev.ProjectID
	}

	resp, err := s.agent.Call(r.Context(), agentproto.CmdUserQuotaSet, map[string]any{
		"username":      u.Username,
		"project_id":    projID,
		"disk_limit_mb": req.DiskLimitMB,
		"inode_limit":   req.InodeLimit,
	})
	if err != nil || !resp.OK {
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = resp.Error
		}
		WriteProblem(w, http.StatusInternalServerError, "internal", "Failed to apply project quota", msg)
		return
	}

	q := &store.UserDiskQuota{
		UserID:      u.ID,
		Username:    u.Username,
		ProjectID:   projID,
		DiskLimitMB: req.DiskLimitMB,
		InodeLimit:  req.InodeLimit,
	}
	if err := s.store.UpsertUserDiskQuota(r.Context(), q); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "quota.project_update", "user", u.ID, "success", map[string]any{
		"username":      u.Username,
		"disk_limit_mb": req.DiskLimitMB,
		"inode_limit":   req.InodeLimit,
	})
	WriteJSON(w, http.StatusOK, q)
}
