// SPDX-License-Identifier: MIT
package api

import (
	"context"
	"net/http"
	"slices"

	"github.com/StakoCP/stako/internal/agentproto"
)

type managedServiceSpec struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Category      string `json:"category"`
	Unit          string `json:"unit"`
	AptPkg        string `json:"apt_pkg"`
	ConfigService string `json:"config_service,omitempty"`
	Protected     bool   `json:"protected"`
}

type systemServiceStatus struct {
	managedServiceSpec
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Active    string `json:"active"` // active | inactive | failed | not-installed
	Enabled   bool   `json:"enabled"`
}

var managedServices = []managedServiceSpec{
	{ID: "nginx", Label: "Nginx Web Server", Category: "web", Unit: "nginx", AptPkg: "nginx", ConfigService: "nginx"},
	{ID: "apache2", Label: "Apache2 HTTP Server", Category: "web", Unit: "apache2", AptPkg: "apache2", ConfigService: "apache2"},
	{ID: "php8.4-fpm", Label: "PHP-FPM 8.4", Category: "web", Unit: "php8.4-fpm", AptPkg: "php8.4-fpm", ConfigService: "php"},
	{ID: "php8.3-fpm", Label: "PHP-FPM 8.3", Category: "web", Unit: "php8.3-fpm", AptPkg: "php8.3-fpm"},
	{ID: "php8.5-fpm", Label: "PHP-FPM 8.5", Category: "web", Unit: "php8.5-fpm", AptPkg: "php8.5-fpm"},
	{ID: "mariadb", Label: "MariaDB / MySQL", Category: "database", Unit: "mariadb", AptPkg: "mariadb-server", ConfigService: "mysql"},
	{ID: "postgresql", Label: "PostgreSQL", Category: "database", Unit: "postgresql", AptPkg: "postgresql", ConfigService: "postgresql"},
	{ID: "redis", Label: "Redis Server", Category: "database", Unit: "redis-server", AptPkg: "redis-server", ConfigService: "redis"},
	{ID: "rabbitmq", Label: "RabbitMQ", Category: "queue", Unit: "rabbitmq-server", AptPkg: "rabbitmq-server"},
	{ID: "powerdns", Label: "PowerDNS Server", Category: "dns", Unit: "pdns", AptPkg: "pdns-server", ConfigService: "powerdns"},
	{ID: "bind9", Label: "BIND9 DNS Server", Category: "dns", Unit: "bind9", AptPkg: "bind9", ConfigService: "bind9"},
	{ID: "postfix", Label: "Postfix MTA", Category: "mail", Unit: "postfix", AptPkg: "postfix", ConfigService: "postfix"},
	{ID: "dovecot", Label: "Dovecot IMAP/POP3", Category: "mail", Unit: "dovecot", AptPkg: "dovecot-core", ConfigService: "dovecot"},
	{ID: "rspamd", Label: "Rspamd Antispam", Category: "mail", Unit: "rspamd", AptPkg: "rspamd"},
	{ID: "proftpd", Label: "ProFTPD (FTPS)", Category: "ftp", Unit: "proftpd", AptPkg: "proftpd-basic"},
	{ID: "docker", Label: "Docker Engine", Category: "container", Unit: "docker", AptPkg: "docker-ce"},
	{ID: "fail2ban", Label: "Fail2Ban", Category: "security", Unit: "fail2ban", AptPkg: "fail2ban"},
	{ID: "nftables", Label: "nftables Firewall", Category: "security", Unit: "nftables", AptPkg: "nftables", Protected: true},
	{ID: "clamav", Label: "ClamAV Antivirus", Category: "security", Unit: "clamav-daemon", AptPkg: "clamav-daemon"},
	{ID: "ssh", Label: "OpenSSH Server", Category: "security", Unit: "ssh", AptPkg: "openssh-server", ConfigService: "ssh", Protected: true},
}

func findManagedService(id string) (managedServiceSpec, bool) {
	for _, m := range managedServices {
		if m.ID == id {
			return m, true
		}
	}
	return managedServiceSpec{}, false
}

func (s *Server) getCapabilities(ctx context.Context) map[string]bool {
	caps := map[string]bool{
		"web": true, "dns": true, "docker": true, "databases": true,
		"redis_rabbitmq": true, "ftp": true, "mail": true, "firewall": true,
	}
	specs := make([]map[string]string, 0, len(managedServices))
	for _, m := range managedServices {
		specs = append(specs, map[string]string{"id": m.ID, "unit": m.Unit, "apt_pkg": m.AptPkg})
	}
	resp, err := s.agent.Call(ctx, agentproto.CmdSysServiceList, map[string]any{"services": specs})
	if err != nil || !resp.OK {
		return caps
	}
	rawMap, ok := resp.Data["services"].(map[string]any)
	if !ok {
		return caps
	}
	has := func(ids ...string) bool {
		for _, id := range ids {
			if m, ok := rawMap[id].(map[string]any); ok {
				if inst, _ := m["installed"].(bool); inst {
					return true
				}
			}
		}
		return false
	}
	caps["web"] = has("nginx", "apache2")
	caps["dns"] = has("powerdns", "bind9")
	caps["docker"] = has("docker")
	caps["databases"] = has("mariadb", "postgresql")
	caps["redis_rabbitmq"] = has("redis", "rabbitmq")
	caps["ftp"] = has("proftpd")
	caps["mail"] = has("postfix", "dovecot")
	caps["firewall"] = has("nftables", "fail2ban")
	return caps
}

func (s *Server) handleListSysServices(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}

	specs := make([]map[string]string, 0, len(managedServices))
	for _, m := range managedServices {
		specs = append(specs, map[string]string{
			"id": m.ID, "unit": m.Unit, "apt_pkg": m.AptPkg,
		})
	}
	detected := map[string]map[string]any{}
	if resp, err := s.agent.Call(r.Context(), agentproto.CmdSysServiceList, map[string]any{"services": specs}); err == nil && resp.OK {
		if rawMap, ok := resp.Data["services"].(map[string]any); ok {
			for k, v := range rawMap {
				if m, ok := v.(map[string]any); ok {
					detected[k] = m
				}
			}
		}
	}

	out := make([]systemServiceStatus, 0, len(managedServices))
	for _, m := range managedServices {
		st := systemServiceStatus{
			managedServiceSpec: m,
			Active:             "not-installed",
		}
		if d, ok := detected[m.ID]; ok {
			st.Installed, _ = d["installed"].(bool)
			st.Version, _ = d["version"].(string)
			st.Active, _ = d["active"].(string)
			st.Enabled, _ = d["enabled"].(bool)
		}
		out = append(out, st)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

func (s *Server) handleSysServiceAction(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	spec, ok := findManagedService(r.PathValue("id"))
	if !ok {
		WriteProblem(w, http.StatusNotFound, "not-found", "Unknown service", "")
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := DecodeJSON(r, &req); err != nil || !slices.Contains([]string{"start", "stop", "restart", "enable", "disable"}, req.Action) {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Action must be start, stop, restart, enable, or disable", "")
		return
	}
	if spec.Protected && (req.Action == "stop" || req.Action == "disable") {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Cannot stop or disable protected system service", spec.ID)
		return
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdSysServiceAct, map[string]any{
		"unit": spec.Unit, "action": req.Action,
	})
	if err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Agent error", err.Error())
		return
	}
	if !resp.OK {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Service action failed", resp.Error+" "+resp.Output)
		return
	}
	s.audit(r, ac.User, "sysservice."+req.Action, "service", spec.ID, "success", nil)
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "output": resp.Output})
}

func (s *Server) handleInstallSysService(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	spec, ok := findManagedService(r.PathValue("id"))
	if !ok {
		WriteProblem(w, http.StatusNotFound, "not-found", "Unknown service", "")
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "sysservice.install", ac.User.ID, map[string]any{
		"action": "install", "apt_pkg": spec.AptPkg, "unit": spec.Unit,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "sysservice.install", "service", spec.ID, "success", map[string]any{"pkg": spec.AptPkg})
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}

func (s *Server) handleUninstallSysService(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	spec, ok := findManagedService(r.PathValue("id"))
	if !ok {
		WriteProblem(w, http.StatusNotFound, "not-found", "Unknown service", "")
		return
	}
	if spec.Protected {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Protected service cannot be uninstalled", spec.ID)
		return
	}
	var jobID *string
	if j, err := s.jobs.Enqueue(r.Context(), "sysservice.uninstall", ac.User.ID, map[string]any{
		"action": "uninstall", "apt_pkg": spec.AptPkg, "unit": spec.Unit,
	}); err == nil {
		jobID = &j.ID
	}
	s.audit(r, ac.User, "sysservice.uninstall", "service", spec.ID, "success", map[string]any{"pkg": spec.AptPkg})
	WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}
