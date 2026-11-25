// SPDX-License-Identifier: MIT
package api

import (
	"net/http"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

type serviceConfigSpec struct {
	Service   string `json:"service"`
	Label     string `json:"label"`
	Path      string `json:"path"`
	TestCmd   string `json:"test_cmd"`
	ReloadSvc string `json:"-"`
	Sample    string `json:"-"`
}

var managedServiceConfigs = []serviceConfigSpec{
	{Service: "nginx", Label: "Nginx", Path: "/etc/nginx/nginx.conf", TestCmd: "nginx -t", ReloadSvc: "nginx", Sample: "user www-data;\nworker_processes auto;\npid /run/nginx.pid;\n"},
	{Service: "apache2", Label: "Apache2", Path: "/etc/apache2/apache2.conf", TestCmd: "apache2ctl configtest", ReloadSvc: "apache2", Sample: "ServerRoot \"/etc/apache2\"\nTimeout 300\nKeepAlive On\n"},
	{Service: "mysql", Label: "MariaDB / MySQL", Path: "/etc/mysql/mariadb.conf.d/50-server.cnf", TestCmd: "mariadbd --validate-config", ReloadSvc: "mariadb", Sample: "[mysqld]\nbind-address = 127.0.0.1\nmax_connections = 150\n"},
	{Service: "postgresql", Label: "PostgreSQL", Path: "/etc/postgresql/16/main/postgresql.conf", TestCmd: "", ReloadSvc: "postgresql", Sample: "listen_addresses = 'localhost'\nmax_connections = 100\nshared_buffers = 256MB\n"},
	{Service: "php", Label: "PHP-FPM 8.4", Path: "/etc/php/8.4/fpm/php.ini", TestCmd: "php-fpm8.4 -t", ReloadSvc: "php8.4-fpm", Sample: "[PHP]\nmemory_limit = 256M\nupload_max_filesize = 64M\npost_max_size = 64M\n"},
	{Service: "powerdns", Label: "PowerDNS", Path: "/etc/powerdns/pdns.conf", TestCmd: "", ReloadSvc: "pdns", Sample: "launch=gsqlite3\ngsqlite3-database=/var/lib/powerdns/pdns.sqlite3\n"},
	{Service: "bind9", Label: "BIND9", Path: "/etc/bind/named.conf.options", TestCmd: "named-checkconf", ReloadSvc: "bind9", Sample: "options {\n  directory \"/var/cache/bind\";\n  recursion no;\n};\n"},
	{Service: "redis", Label: "Redis", Path: "/etc/redis/redis.conf", TestCmd: "", ReloadSvc: "redis-server", Sample: "bind 127.0.0.1 ::1\nprotected-mode yes\nmaxmemory 256mb\n"},
	{Service: "postfix", Label: "Postfix", Path: "/etc/postfix/main.cf", TestCmd: "postfix check", ReloadSvc: "postfix", Sample: "smtpd_tls_security_level = may\ninet_interfaces = all\n"},
	{Service: "dovecot", Label: "Dovecot", Path: "/etc/dovecot/dovecot.conf", TestCmd: "doveconf -n", ReloadSvc: "dovecot", Sample: "protocols = imap pop3 lmtp\nssl = required\n"},
	{Service: "ssh", Label: "OpenSSH", Path: "/etc/ssh/sshd_config.d/99-stako.conf", TestCmd: "sshd -t", ReloadSvc: "ssh", Sample: "PermitRootLogin prohibit-password\nMaxAuthTries 4\nX11Forwarding no\n"},
}

func findServiceConfig(svc string) (serviceConfigSpec, bool) {
	for _, s := range managedServiceConfigs {
		if s.Service == svc {
			return s, true
		}
	}
	return serviceConfigSpec{}, false
}

func (s *Server) handleListSysConfigs(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	specs := make([]map[string]string, 0, len(managedServices))
	for _, m := range managedServices {
		specs = append(specs, map[string]string{"id": m.ID, "unit": m.Unit, "apt_pkg": m.AptPkg})
	}
	resp, err := s.agent.Call(r.Context(), agentproto.CmdSysServiceList, map[string]any{"services": specs})
	if err != nil || !resp.OK {
		WriteJSON(w, http.StatusOK, map[string]any{"items": managedServiceConfigs})
		return
	}
	rawMap, ok := resp.Data["services"].(map[string]any)
	if !ok {
		WriteJSON(w, http.StatusOK, map[string]any{"items": managedServiceConfigs})
		return
	}
	installedCfg := map[string]bool{}
	for _, m := range managedServices {
		if m.ConfigService == "" {
			continue
		}
		if d, ok := rawMap[m.ID].(map[string]any); ok {
			if inst, _ := d["installed"].(bool); inst {
				installedCfg[m.ConfigService] = true
			}
		}
	}
	out := []serviceConfigSpec{}
	for _, c := range managedServiceConfigs {
		if installedCfg[c.Service] {
			out = append(out, c)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleGetSysConfig(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	spec, ok := findServiceConfig(r.PathValue("service"))
	if !ok {
		WriteProblem(w, http.StatusNotFound, "not-found", "Unknown service", "")
		return
	}
	content := spec.Sample
	if resp, err := s.agent.Call(r.Context(), agentproto.CmdSysConfigRead, map[string]any{
		"service": spec.Service, "path": spec.Path,
	}); err == nil && resp.OK && !resp.DryRun {
		content = resp.Output
	}
	history, _ := s.store.ListServiceConfigRevisions(r.Context(), spec.Service)
	if len(history) > 0 && content == spec.Sample {
		// In dry-run mode, reflect the latest saved revision if any.
		content = history[0].Content
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"service": spec.Service,
		"label":   spec.Label,
		"path":    spec.Path,
		"content": content,
		"history": history,
	})
}

func (s *Server) handleUpdateSysConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireReauth(w, r) {
		return
	}
	ac := GetAuth(r)
	if ac.User.Role != "admin" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Admin only", "")
		return
	}
	spec, ok := findServiceConfig(r.PathValue("service"))
	if !ok {
		WriteProblem(w, http.StatusNotFound, "not-found", "Unknown service", "")
		return
	}
	var req struct {
		Content string `json:"content"`
		Comment string `json:"comment"`
	}
	if err := DecodeJSON(r, &req); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	if req.Content == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Content cannot be empty", "")
		return
	}

	resp, err := s.agent.Call(r.Context(), agentproto.CmdSysConfigApply, map[string]any{
		"service":    spec.Service,
		"path":       spec.Path,
		"content":    req.Content,
		"test_cmd":   spec.TestCmd,
		"reload_svc": spec.ReloadSvc,
	})
	if err != nil {
		WriteProblem(w, http.StatusInternalServerError, "internal", "Agent error", err.Error())
		return
	}
	if !resp.OK {
		s.audit(r, ac.User, "sysconfig.update", "sysconfig", spec.Service, "failure", map[string]any{"error": resp.Error})
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Configuration validation failed (rolled back)", resp.Error+"\n"+resp.Output)
		return
	}

	revID, _ := crypto.RandomID("sc")
	_ = s.store.SaveServiceConfigRevision(r.Context(), &store.ServiceConfigRevision{
		ID: revID, Service: spec.Service, Content: req.Content, Comment: req.Comment, CreatedBy: ac.User.Username,
	})
	s.audit(r, ac.User, "sysconfig.update", "sysconfig", spec.Service, "success", map[string]any{"path": spec.Path, "comment": req.Comment})
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"output": resp.Output,
	})
}
