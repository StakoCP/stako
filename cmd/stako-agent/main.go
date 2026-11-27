// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/StakoCP/stako/internal/agentproto"
	"github.com/StakoCP/stako/internal/catalog"
	"github.com/StakoCP/stako/internal/version"
)

func main() {
	sockPath := flag.String("sock", "/run/stako/agent.sock", "Unix socket path")
	sockGroup := flag.String("sock-group", "stako", "Unix socket group for stako-core access")
	dryRun := flag.Bool("dry-run", runtime.GOOS == "windows", "Log commands without executing")
	allowUID := flag.Int("allow-uid", -1, "Allowed peer UID (-1 = auto stako/root)")
	showVer := flag.Bool("version", false, "Print version")
	flag.Parse()

	if *showVer {
		fmt.Printf("stako-agent %s\n", version.Version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if runtime.GOOS == "windows" {
		logger.Info("stako-agent started in Windows dry-run mode (no unix socket server)")
		return
	}

	if err := os.MkdirAll(filepath.Dir(*sockPath), 0o750); err != nil {
		logger.Error("mkdir socket dir", "err", err)
		os.Exit(1)
	}
	_ = os.Remove(*sockPath)
	ln, err := net.Listen("unix", *sockPath)
	if err != nil {
		logger.Error("listen unix", "err", err)
		os.Exit(1)
	}
	defer ln.Close()

	if grp, err := user.LookupGroup(*sockGroup); err == nil {
		if gid, err := strconv.Atoi(grp.Gid); err == nil {
			_ = os.Chown(filepath.Dir(*sockPath), 0, gid)
			_ = os.Chmod(filepath.Dir(*sockPath), 0o750)
			_ = os.Chown(*sockPath, 0, gid)
			_ = os.Chmod(*sockPath, 0o660)
		}
	} else {
		_ = os.Chmod(*sockPath, 0o600)
	}
	if *allowUID < 0 {
		if u, err := user.Lookup(*sockGroup); err == nil {
			if uid, err := strconv.Atoi(u.Uid); err == nil {
				*allowUID = uid
			}
		}
	}

	logger.Info("stako-agent listening", "sock", *sockPath, "dry_run", *dryRun)

	if !*dryRun {
		if st, err := os.Stat("/etc/nginx/conf.d"); err == nil && st.IsDir() {
			changed := ensureNginxCloudflareRealIP()
			if ensureDBAdminToolsNginx() {
				changed = true
			}
			if changed && exec.Command("nginx", "-t").Run() == nil {
				_ = exec.Command("systemctl", "reload", "nginx").Run()
			}
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			break
		}
		go handleConn(conn, *dryRun, *allowUID, logger)
	}
}

func handleConn(conn net.Conn, dryRun bool, allowUID int, logger *slog.Logger) {
	defer conn.Close()
	if allowUID >= 0 {
		if err := verifyPeerUID(conn, allowUID); err != nil {
			logger.Warn("rejected unauthorized peer", "err", err)
			return
		}
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	if !sc.Scan() {
		return
	}
	var req agentproto.Request
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
		writeResp(conn, agentproto.Response{OK: false, Error: "invalid request json"})
		return
	}
	resp := dispatch(req, dryRun, logger)
	writeResp(conn, resp)
}

func writeResp(conn net.Conn, resp agentproto.Response) {
	b, _ := json.Marshal(resp)
	_, _ = conn.Write(append(b, '\n'))
}

func dispatch(req agentproto.Request, dryRun bool, logger *slog.Logger) agentproto.Response {
	resp := agentproto.Response{ID: req.ID, DryRun: dryRun}
	switch req.Command {
	case agentproto.CmdPing:
		resp.OK = true
		resp.Data = map[string]any{"version": version.Version, "dry_run": dryRun}
		return resp

	case agentproto.CmdSystemInfo:
		resp.OK = true
		resp.Data = collectSystemInfo()
		resp.Data["version"] = version.Version
		return resp

	case agentproto.CmdUserCreate:
		username, _ := req.Args["username"].(string)
		shell, _ := req.Args["shell"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if shell == "" {
			shell = "/usr/sbin/nologin"
		}
		r := runExec(resp, dryRun, logger, "useradd", "-m", "-s", shell, username)
		if r.OK && !dryRun {
			homeDir := "/home/" + username
			_ = exec.Command("chown", username+":www-data", homeDir).Run()
			_ = os.Chmod(homeDir, 0o710)
		}
		return r

	case agentproto.CmdUserDelete:
		username, _ := req.Args["username"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		return runExec(resp, dryRun, logger, "userdel", "-r", username)

	case agentproto.CmdUserSuspend:
		username, _ := req.Args["username"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		return runExec(resp, dryRun, logger, "usermod", "-L", username)

	case agentproto.CmdUserUnsuspend:
		username, _ := req.Args["username"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		return runExec(resp, dryRun, logger, "usermod", "-U", username)

	case agentproto.CmdUserSetShell:
		username, _ := req.Args["username"].(string)
		shell, _ := req.Args["shell"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		return runExec(resp, dryRun, logger, "usermod", "-s", shell, username)

	case agentproto.CmdDomainApply:
		domain, _ := req.Args["domain"].(string)
		username, _ := req.Args["username"].(string)
		docroot, _ := req.Args["docroot"].(string)
		stack, _ := req.Args["stack"].(string)
		tmpl, _ := req.Args["template"].(string)
		phpVer, _ := req.Args["php_version"].(string)
		conf, _ := req.Args["nginx_conf"].(string)
		apacheConf, _ := req.Args["apache_conf"].(string)
		if stack == "" {
			stack = "nginx"
		}
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if dryRun {
			return runExec(resp, dryRun, logger, "nginx", "-t", "# apply stack="+stack+" for "+domain)
		}
		if docroot != "" {
			_ = os.MkdirAll(docroot, 0o750)
			if username != "" && agentproto.ValidateLinuxUsername(username) == nil {
				ensureDocrootPermissions(username, docroot)
			}
		}
		if username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			logsDir := fmt.Sprintf("/home/%s/web/%s/logs", username, domain)
			_ = os.MkdirAll(logsDir, 0o750)
			ensureDocrootPermissions(username, logsDir)
			confDir := fmt.Sprintf("/home/%s/web/%s/conf/nginx", username, domain)
			_ = os.MkdirAll(confDir, 0o750)
			ensureDocrootPermissions(username, confDir)
		}
		if tmpl != "proxy" && tmpl != "spa" && tmpl != "static" && phpVer != "" && username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			ensurePHPFpmPool(username, phpVer, logger)
		}
		_ = os.MkdirAll("/var/www/acme", 0o755)
		nginxConfPath := "/etc/nginx/sites-enabled/" + domain + ".conf"
		apacheAvailPath := "/etc/apache2/sites-available/" + domain + ".conf"
		apacheEnabledPath := "/etc/apache2/sites-enabled/" + domain + ".conf"

		// Clean up vhost for inactive stack when switching stacks
		if stack == "apache" {
			if _, err := os.Stat(nginxConfPath); err == nil {
				_ = os.Remove(nginxConfPath)
				_ = runExec(resp, false, logger, "systemctl", "reload", "nginx")
			}
		} else if stack == "nginx" {
			if _, err := os.Stat(apacheEnabledPath); err == nil {
				_ = exec.Command("a2dissite", domain+".conf").Run()
				_ = os.Remove(apacheAvailPath)
				_ = runExec(resp, false, logger, "systemctl", "reload", "apache2")
			}
		}

		if apacheConf != "" {
			ensureApacheCloudflareRealIP()
			_ = exec.Command("a2enmod", "proxy", "proxy_http", "proxy_fcgi", "setenvif", "rewrite", "headers", "ssl", "remoteip").Run()
			if stack == "nginx_apache" {
				_ = os.WriteFile("/etc/apache2/conf-available/stako-backend.conf", []byte("Listen 127.0.0.1:8080\n"), 0o644)
				_ = exec.Command("a2enconf", "stako-backend").Run()
			}
			if err := os.WriteFile(apacheAvailPath, []byte(apacheConf), 0o644); err != nil {
				resp.Error = err.Error()
				return resp
			}
			_ = exec.Command("a2ensite", domain+".conf").Run()
			testResp := runExec(resp, false, logger, "apache2ctl", "-t")
			if !testResp.OK {
				_ = exec.Command("a2dissite", domain+".conf").Run()
				_ = os.Remove(apacheAvailPath)
				return testResp
			}
			if r := runExec(resp, false, logger, "systemctl", "reload", "apache2"); !r.OK {
				return r
			}
		}

		if conf != "" {
			ensureNginxCloudflareRealIP()
			if err := os.WriteFile(nginxConfPath, []byte(conf), 0o644); err != nil {
				resp.Error = err.Error()
				return resp
			}
			testResp := runExec(resp, false, logger, "nginx", "-t")
			if !testResp.OK {
				_ = os.Remove(nginxConfPath)
				return testResp
			}
			return runExec(resp, false, logger, "systemctl", "reload", "nginx")
		}
		resp.OK = true
		resp.Output = "applied " + stack + " vhost for " + domain
		return resp

	case agentproto.CmdDomainDelete:
		domain, _ := req.Args["domain"].(string)
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if dryRun {
			return runExec(resp, true, logger, "systemctl", "reload", "nginx")
		}
		if _, err := os.Stat("/etc/nginx/sites-enabled/" + domain + ".conf"); err == nil {
			_ = os.Remove("/etc/nginx/sites-enabled/" + domain + ".conf")
			_ = runExec(resp, false, logger, "systemctl", "reload", "nginx")
		}
		if _, err := os.Stat("/etc/apache2/sites-available/" + domain + ".conf"); err == nil {
			_ = exec.Command("a2dissite", domain+".conf").Run()
			_ = os.Remove("/etc/apache2/sites-available/" + domain + ".conf")
			_ = runExec(resp, false, logger, "systemctl", "reload", "apache2")
		}
		resp.OK = true
		resp.Output = "removed vhost for " + domain
		return resp

	case agentproto.CmdCronSync:
		username, _ := req.Args["username"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		cronPath := "/etc/cron.d/stako-" + username
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] sync " + cronPath
			return resp
		}
		rawJobs, _ := req.Args["jobs"].([]any)
		if len(rawJobs) == 0 {
			_ = os.Remove(cronPath)
			resp.OK = true
			resp.Output = "removed empty crontab " + cronPath
			return resp
		}
		var sb strings.Builder
		sb.WriteString("# Managed by Stako. Do not edit manually.\n")
		sb.WriteString("SHELL=/bin/bash\n")
		sb.WriteString("PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin\n")
		for _, item := range rawJobs {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			sched, _ := m["schedule"].(string)
			cmdStr, _ := m["command"].(string)
			if len(strings.Fields(sched)) != 5 || cmdStr == "" || strings.ContainsAny(cmdStr, "\n\r") {
				continue
			}
			sb.WriteString(fmt.Sprintf("%s %s %s\n", sched, username, cmdStr))
		}
		if err := os.WriteFile(cronPath, []byte(sb.String()), 0o644); err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.OK = true
		resp.Output = "synced " + cronPath
		return resp

	case agentproto.CmdDNSZoneSync:
		domain, _ := req.Args["domain"].(string)
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] pdnsutil load-zone " + domain
			return resp
		}
		if _, err := exec.LookPath("pdnsutil"); err != nil {
			resp.OK = true
			resp.Output = "pdnsutil not installed; zone stored in panel DB"
			return resp
		}
		var zb strings.Builder
		zb.WriteString(fmt.Sprintf("$ORIGIN %s.\n$TTL 3600\n", domain))
		zb.WriteString(fmt.Sprintf("@ IN SOA ns1.%s. hostmaster.%s. 1 10800 3600 604800 3600\n", domain, domain))
		zb.WriteString(fmt.Sprintf("@ IN NS ns1.%s.\n", domain))
		if rawRecs, ok := req.Args["records"].([]any); ok {
			for _, item := range rawRecs {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				rtype, _ := m["type"].(string)
				name, _ := m["name"].(string)
				content, _ := m["content"].(string)
				ttl := 3600
				if tf, ok := m["ttl"].(float64); ok && tf > 0 {
					ttl = int(tf)
				}
				prio := 0
				if pf, ok := m["priority"].(float64); ok && pf > 0 {
					prio = int(pf)
				}
				if rtype == "" || name == "" || content == "" || strings.ContainsAny(name+content, "\n\r") {
					continue
				}
				if rtype == "TXT" && !strings.HasPrefix(content, `"`) {
					content = `"` + strings.ReplaceAll(content, `"`, `\"`) + `"`
				}
				if rtype == "MX" || rtype == "SRV" {
					zb.WriteString(fmt.Sprintf("%s %d IN %s %d %s\n", name, ttl, rtype, prio, content))
				} else {
					zb.WriteString(fmt.Sprintf("%s %d IN %s %s\n", name, ttl, rtype, content))
				}
			}
		}
		tmpFile, err := os.CreateTemp("", "stako-zone-*.zone")
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)
		_, _ = tmpFile.WriteString(zb.String())
		_ = tmpFile.Close()
		if r := runExec(resp, false, logger, "pdnsutil", "load-zone", domain, tmpPath); !r.OK {
			return r
		}
		_ = exec.Command("pdnsutil", "rectify-zone", domain).Run()
		resp.OK = true
		resp.Output = "synced PowerDNS zone " + domain
		return resp

	case agentproto.CmdDNSZoneDelete:
		domain, _ := req.Args["domain"].(string)
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] pdnsutil delete-zone " + domain
			return resp
		}
		if _, err := exec.LookPath("pdnsutil"); err != nil {
			resp.OK = true
			return resp
		}
		return runExec(resp, false, logger, "pdnsutil", "delete-zone", domain)

	case agentproto.CmdDomainLogs:
		username, _ := req.Args["username"].(string)
		domain, _ := req.Args["domain"].(string)
		logType, _ := req.Args["log_type"].(string)
		if logType == "" {
			logType = "access"
		}
		if logType != "access" && logType != "error" && logType != "goaccess" {
			resp.Error = "invalid log_type"
			return resp
		}
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		lines := 200
		if lf, ok := req.Args["lines"].(float64); ok && lf > 0 {
			lines = int(lf)
			if lines > 1000 {
				lines = 1000
			}
		}
		fileType := logType
		if logType == "goaccess" {
			fileType = "access"
		}
		logPath := fmt.Sprintf("/home/%s/web/%s/logs/%s.log", username, domain, fileType)
		if dryRun {
			resp.OK = true
			if logType == "goaccess" {
				resp.Data = map[string]any{"path": logPath, "logs": fmt.Sprintf("[dry-run] goaccess %s --log-format=COMBINED -o json", logPath)}
			} else {
				resp.Data = map[string]any{"path": logPath, "logs": fmt.Sprintf("[dry-run] tail -n %d %s", lines, logPath)}
			}
			return resp
		}
		info, err := os.Stat(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				resp.OK = true
				resp.Data = map[string]any{"path": logPath, "logs": ""}
				return resp
			}
			resp.Error = err.Error()
			return resp
		}
		if logType == "goaccess" {
			if _, err := exec.LookPath("goaccess"); err != nil {
				resp.Error = "goaccess is not installed; install it from System Services"
				return resp
			}
			if info.Size() == 0 {
				resp.OK = true
				resp.Data = map[string]any{"path": logPath, "logs": ""}
				return resp
			}
			out, err := exec.Command("goaccess", logPath, "--log-format=COMBINED", "-o", "json").CombinedOutput()
			if err != nil {
				resp.Error = fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))
				return resp
			}
			resp.OK = true
			resp.Data = map[string]any{"path": logPath, "logs": string(out)}
			return resp
		}
		out, err := exec.Command("tail", "-n", fmt.Sprintf("%d", lines), logPath).CombinedOutput()
		if err != nil {
			resp.Error = fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))
			return resp
		}
		resp.OK = true
		resp.Data = map[string]any{"path": logPath, "logs": string(out)}
		return resp

	case agentproto.CmdDomainCustomConfRead:
		username, _ := req.Args["username"].(string)
		domain, _ := req.Args["domain"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		confPath := fmt.Sprintf("/home/%s/web/%s/conf/nginx/custom.conf", username, domain)
		if dryRun {
			resp.OK = true
			resp.Data = map[string]any{"path": confPath, "content": ""}
			return resp
		}
		b, err := os.ReadFile(confPath)
		if err != nil {
			if os.IsNotExist(err) {
				resp.OK = true
				resp.Data = map[string]any{"path": confPath, "content": ""}
				return resp
			}
			resp.Error = err.Error()
			return resp
		}
		resp.OK = true
		resp.Data = map[string]any{"path": confPath, "content": string(b)}
		return resp

	case agentproto.CmdDomainCustomConfWrite:
		username, _ := req.Args["username"].(string)
		domain, _ := req.Args["domain"].(string)
		content, _ := req.Args["content"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		confDir := fmt.Sprintf("/home/%s/web/%s/conf/nginx", username, domain)
		confPath := filepath.Join(confDir, "custom.conf")
		if dryRun {
			resp.OK = true
			resp.Data = map[string]any{"path": confPath}
			return resp
		}
		_ = os.MkdirAll(confDir, 0o750)
		ensureDocrootPermissions(username, confDir)
		prev, prevErr := os.ReadFile(confPath)
		if strings.TrimSpace(content) == "" {
			_ = os.Remove(confPath)
		} else {
			if err := os.WriteFile(confPath, []byte(content), 0o640); err != nil {
				resp.Error = err.Error()
				return resp
			}
			_ = exec.Command("chown", username+":www-data", confPath).Run()
		}
		testResp := runExec(resp, false, logger, "nginx", "-t")
		if !testResp.OK {
			if prevErr == nil {
				_ = os.WriteFile(confPath, prev, 0o640)
				_ = exec.Command("chown", username+":www-data", confPath).Run()
			} else {
				_ = os.Remove(confPath)
			}
			return testResp
		}
		r := runExec(resp, false, logger, "systemctl", "reload", "nginx")
		r.Data = map[string]any{"path": confPath}
		return r

	case agentproto.CmdSSLIssue:
		domain, _ := req.Args["domain"].(string)
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		return runExec(resp, dryRun, logger, "certbot", "certonly", "--webroot", "-w", "/var/www/acme", "-d", domain, "--non-interactive", "--agree-tos")

	case agentproto.CmdAppDeploy:
		appID, _ := req.Args["app_id"].(string)
		unitName, _ := req.Args["unit_name"].(string)
		unitBody, _ := req.Args["unit_body"].(string)
		if !strings.HasPrefix(unitName, "stako-app-") {
			resp.Error = "invalid unit name"
			return resp
		}
		if dryRun {
			return runExec(resp, dryRun, logger, "systemctl", "enable", "--now", unitName)
		}
		if appID == "" {
			appID = strings.TrimPrefix(unitName, "stako-app-")
		}
		if envMap, ok := req.Args["env"].(map[string]any); ok {
			_ = os.MkdirAll("/etc/stako/apps", 0o750)
			var lines []string
			for k, v := range envMap {
				if vs, ok := v.(string); ok && !strings.ContainsAny(k, "=\n\r") {
					lines = append(lines, k+"="+strings.ReplaceAll(vs, "\n", ""))
				}
			}
			_ = os.WriteFile("/etc/stako/apps/"+appID+".env", []byte(strings.Join(lines, "\n")+"\n"), 0o600)
		}
		if err := os.WriteFile("/etc/systemd/system/"+unitName+".service", []byte(unitBody), 0o644); err != nil {
			resp.Error = err.Error()
			return resp
		}
		_ = runExec(resp, false, logger, "systemctl", "daemon-reload")
		return runExec(resp, false, logger, "systemctl", "enable", "--now", unitName)

	case agentproto.CmdAppAction:
		unitName, _ := req.Args["unit_name"].(string)
		action, _ := req.Args["action"].(string)
		if !strings.HasPrefix(unitName, "stako-app-") {
			resp.Error = "invalid unit name"
			return resp
		}
		switch action {
		case "start", "stop", "restart":
			return runExec(resp, dryRun, logger, "systemctl", action, unitName)
		case "delete":
			r := runExec(resp, dryRun, logger, "systemctl", "disable", "--now", unitName)
			if !dryRun {
				_ = os.Remove("/etc/systemd/system/" + unitName + ".service")
				_ = os.Remove("/etc/stako/apps/" + strings.TrimPrefix(unitName, "stako-app-") + ".env")
			}
			return r
		default:
			resp.Error = "invalid app action"
			return resp
		}

	case agentproto.CmdAppLogs:
		unitName, _ := req.Args["unit_name"].(string)
		if !strings.HasPrefix(unitName, "stako-app-") {
			resp.Error = "invalid unit name"
			return resp
		}
		return runExec(resp, dryRun, logger, "journalctl", "-u", unitName, "-n", "100", "--no-pager")

	case agentproto.CmdIPList:
		if dryRun {
			resp.OK = true
			resp.Data = map[string]any{
				"ips": []map[string]any{
					{"ip": "127.0.0.1", "family": "ipv4", "interface": "lo"},
					{"ip": "::1", "family": "ipv6", "interface": "lo"},
				},
			}
			return resp
		}
		ifaces, err := net.Interfaces()
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		hasPublicV4 := false
		seen := map[string]bool{}
		ips := []map[string]any{}
		for _, iface := range ifaces {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ipNet, ok := addr.(*net.IPNet)
				if !ok || ipNet.IP == nil || ipNet.IP.IsLinkLocalUnicast() || ipNet.IP.IsLinkLocalMulticast() {
					continue
				}
				ipStr := ipNet.IP.String()
				seen[ipStr] = true
				fam := "ipv6"
				if ipNet.IP.To4() != nil {
					fam = "ipv4"
					if !ipNet.IP.IsLoopback() && !ipNet.IP.IsPrivate() {
						hasPublicV4 = true
					}
				}
				ips = append(ips, map[string]any{
					"ip":        ipStr,
					"family":    fam,
					"interface": iface.Name,
				})
			}
		}
		// On cloud VPS behind 1:1 NAT (AWS/GCP/Alibaba/etc.), detect public egress IP if not directly on NIC.
		if !hasPublicV4 {
			if pub := detectPublicIP("https://api.ipify.org"); pub != "" && !seen[pub] {
				ips = append(ips, map[string]any{
					"ip":        pub,
					"family":    "ipv4",
					"interface": "wan-nat",
				})
			}
		}
		if pub6 := detectPublicIP("https://api6.ipify.org"); pub6 != "" && !seen[pub6] {
			ips = append(ips, map[string]any{
				"ip":        pub6,
				"family":    "ipv6",
				"interface": "wan-nat",
			})
		}
		resp.OK = true
		resp.Data = map[string]any{"ips": ips}
		return resp

	case agentproto.CmdDBCreate:
		engine, _ := req.Args["engine"].(string)
		dbName, _ := req.Args["name"].(string)
		dbUser, _ := req.Args["db_user"].(string)
		dbPass, _ := req.Args["db_password"].(string)
		if err := agentproto.ValidateDBIdent(dbName); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if dbUser == "" {
			dbUser = dbName
		}
		if err := agentproto.ValidateDBIdent(dbUser); err != nil {
			resp.Error = err.Error()
			return resp
		}
		escPass := strings.ReplaceAll(dbPass, "'", "''")
		if engine == "postgres" {
			if dryRun {
				return runExec(resp, true, logger, "sudo", "-u", "postgres", "createdb", "-O", dbUser, dbName)
			}
			roleSQL := fmt.Sprintf("DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN CREATE ROLE %s WITH LOGIN PASSWORD '%s'; ELSE ALTER ROLE %s WITH PASSWORD '%s'; END IF; END $$;", dbUser, dbUser, escPass, dbUser, escPass)
			if r := runExec(resp, false, logger, "sudo", "-u", "postgres", "psql", "-v", "ON_ERROR_STOP=1", "-c", roleSQL); !r.OK {
				return r
			}
			return runExec(resp, false, logger, "sudo", "-u", "postgres", "createdb", "-O", dbUser, dbName)
		}
		if dryRun {
			return runExec(resp, true, logger, "mariadb", "-e", "CREATE DATABASE IF NOT EXISTS `"+dbName+"`;")
		}
		hosts := []string{"localhost", "127.0.0.1"}
		if rawHosts, ok := req.Args["remote_hosts"].([]any); ok && len(rawHosts) > 0 {
			hosts = []string{"localhost"}
			for _, h := range rawHosts {
				if hs, ok := h.(string); ok && hs != "" && !strings.ContainsAny(hs, "'`;\n\r ") {
					hosts = append(hosts, hs)
				}
			}
		}
		var stmts []string
		stmts = append(stmts, "CREATE DATABASE IF NOT EXISTS `"+dbName+"`;")
		for _, h := range hosts {
			stmts = append(stmts, fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s';", dbUser, h, escPass))
			stmts = append(stmts, fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY '%s';", dbUser, h, escPass))
			stmts = append(stmts, fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s';", dbName, dbUser, h))
		}
		stmts = append(stmts, "FLUSH PRIVILEGES;")
		return runExec(resp, false, logger, "mariadb", "-e", strings.Join(stmts, " "))

	case agentproto.CmdDBDelete:
		engine, _ := req.Args["engine"].(string)
		dbName, _ := req.Args["name"].(string)
		if err := agentproto.ValidateDBIdent(dbName); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if engine == "postgres" {
			return runExec(resp, dryRun, logger, "sudo", "-u", "postgres", "dropdb", "--if-exists", dbName)
		}
		return runExec(resp, dryRun, logger, "mariadb", "-e", "DROP DATABASE IF EXISTS `"+dbName+"`;")

	case agentproto.CmdDBBackup:
		engine, _ := req.Args["engine"].(string)
		dbName, _ := req.Args["name"].(string)
		if err := agentproto.ValidateDBIdent(dbName); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if engine == "postgres" {
			return runExec(resp, dryRun, logger, "sudo", "-u", "postgres", "pg_dump", "-Fc", dbName, "-f", "/var/backups/stako/"+dbName+".dump")
		}
		return runExec(resp, dryRun, logger, "mariadb-dump", "--single-transaction", dbName)

	case agentproto.CmdFTPCreate:
		username, _ := req.Args["username"].(string)
		homeDir, _ := req.Args["home_dir"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		return runExec(resp, dryRun, logger, "useradd", "-d", homeDir, "-s", "/usr/sbin/nologin", username)

	case agentproto.CmdFTPDelete:
		username, _ := req.Args["username"].(string)
		if err := agentproto.ValidateLinuxUsername(username); err != nil {
			resp.Error = err.Error()
			return resp
		}
		return runExec(resp, dryRun, logger, "userdel", username)

	case agentproto.CmdFirewallApply:
		ruleset, _ := req.Args["ruleset"].(string)
		if dryRun {
			return runExec(resp, dryRun, logger, "nft", "-c", "-f", "/etc/nftables.d/stako.nft")
		}
		_ = os.MkdirAll("/etc/nftables.d", 0o750)
		if err := os.WriteFile("/etc/nftables.d/stako.nft", []byte(ruleset), 0o640); err != nil {
			resp.Error = err.Error()
			return resp
		}
		return runExec(resp, false, logger, "nft", "-f", "/etc/nftables.d/stako.nft")

	case agentproto.CmdFirewallBans:
		if dryRun {
			resp.OK = true
			resp.Data = map[string]any{"bans": []any{}}
			return resp
		}
		return runExec(resp, false, logger, "fail2ban-client", "status")

	case agentproto.CmdFirewallUnban:
		ip, _ := req.Args["ip"].(string)
		jail, _ := req.Args["jail"].(string)
		if jail == "" {
			jail = "sshd"
		}
		if net.ParseIP(ip) == nil {
			resp.Error = "invalid ip"
			return resp
		}
		return runExec(resp, dryRun, logger, "fail2ban-client", "set", jail, "unbanip", ip)

	case agentproto.CmdSecurityScan:
		if dryRun {
			resp.OK = true
			resp.Data = map[string]any{
				"findings": []map[string]any{
					{
						"id":          "sf_ssh_root",
						"category":    "ssh",
						"severity":    "low",
						"title":       "Verify SSH PermitRootLogin is set to prohibit-password",
						"description": "Baseline CIS check for OpenSSH root login restriction.",
						"remediation": "Ensure PermitRootLogin prohibit-password in /etc/ssh/sshd_config",
						"component":   "openssh-server",
						"status":      "resolved",
					},
				},
			}
			return resp
		}
		resp.OK = true
		resp.Data = map[string]any{"findings": runSecurityScan()}
		return resp

	case agentproto.CmdUpdatesList:
		if dryRun {
			resp.OK = true
			resp.Data = map[string]any{
				"reboot_required": false,
				"updates":         []any{},
			}
			return resp
		}
		return runExec(resp, false, logger, "apt-get", "-s", "upgrade")

	case agentproto.CmdUpdatesApply:
		isDry, _ := req.Args["dry_run"].(bool)
		if isDry || dryRun {
			return runExec(resp, true, logger, "apt-get", "upgrade", "-s")
		}
		return runExec(resp, false, logger, "apt-get", "upgrade", "-y")

	case agentproto.CmdDockerRun:
		username, _ := req.Args["username"].(string)
		name, _ := req.Args["name"].(string)
		image, _ := req.Args["image"].(string)
		repoURL, _ := req.Args["repo_url"].(string)
		branch, _ := req.Args["repo_branch"].(string)
		dockerfile, _ := req.Args["dockerfile"].(string)
		localPath, _ := req.Args["local_path"].(string)
		cpuLimit, _ := req.Args["cpu_limit"].(string)
		if branch == "" {
			branch = "main"
		}
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}
		if localPath != "" {
			if dryRun {
				return runExec(resp, dryRun, logger, "docker", "build+run", localPath, image)
			}
			if _, err := os.Stat(localPath); err != nil {
				resp.Error = fmt.Sprintf("local_path not found: %v", err)
				return resp
			}
			dfPath := filepath.Join(localPath, dockerfile)
			if r := runExec(resp, false, logger, "sudo", "-u", username, "docker", "build", "-t", image, "-f", dfPath, localPath); !r.OK {
				return r
			}
			_ = exec.Command("sudo", "-u", username, "docker", "rm", "-f", name).Run()
		} else if repoURL != "" {
			buildDir := fmt.Sprintf("/home/%s/.stako-docker/%s", username, name)
			if dryRun {
				return runExec(resp, dryRun, logger, "docker", "build+run", repoURL+"@"+branch, image)
			}
			_ = os.MkdirAll(filepath.Dir(buildDir), 0o750)
			_ = exec.Command("chown", username+":"+username, filepath.Dir(buildDir)).Run()
			if _, err := os.Stat(filepath.Join(buildDir, ".git")); err == nil {
				if r := runExec(resp, false, logger, "sudo", "-u", username, "git", "-C", buildDir, "fetch", "origin", branch); !r.OK {
					return r
				}
				if r := runExec(resp, false, logger, "sudo", "-u", username, "git", "-C", buildDir, "checkout", "-f", branch); !r.OK {
					return r
				}
				if r := runExec(resp, false, logger, "sudo", "-u", username, "git", "-C", buildDir, "reset", "--hard", "origin/"+branch); !r.OK {
					return r
				}
			} else {
				if r := runExec(resp, false, logger, "sudo", "-u", username, "git", "clone", "--depth", "1", "-b", branch, repoURL, buildDir); !r.OK {
					return r
				}
			}
			dfPath := filepath.Join(buildDir, dockerfile)
			if r := runExec(resp, false, logger, "sudo", "-u", username, "docker", "build", "-t", image, "-f", dfPath, buildDir); !r.OK {
				return r
			}
			_ = exec.Command("sudo", "-u", username, "docker", "rm", "-f", name).Run()
		}
		args := []string{"-u", username, "docker", "run", "-d", "--name", name, "--restart", "unless-stopped", "--security-opt", "no-new-privileges", "--cap-drop=ALL"}
		if cpuLimit != "" {
			args = append(args, "--cpus", cpuLimit)
		}
		if memF, ok := req.Args["memory_mb"].(float64); ok && memF > 0 {
			args = append(args, "--memory", fmt.Sprintf("%dm", int(memF)))
		}
		if portsRaw, ok := req.Args["ports"].([]any); ok {
			for _, p := range portsRaw {
				if ps, ok := p.(string); ok && ps != "" {
					args = append(args, "-p", ps)
				}
			}
		}
		if envMap, ok := req.Args["env"].(map[string]any); ok {
			for k, v := range envMap {
				if vs, ok := v.(string); ok {
					args = append(args, "-e", k+"="+vs)
				}
			}
		}
		args = append(args, image)
		return runExec(resp, dryRun, logger, "sudo", args...)

	case agentproto.CmdDockerAction:
		username, _ := req.Args["username"].(string)
		name, _ := req.Args["name"].(string)
		action, _ := req.Args["action"].(string)
		switch action {
		case "start", "stop", "restart":
			return runExec(resp, dryRun, logger, "sudo", "-u", username, "docker", action, name)
		case "delete":
			return runExec(resp, dryRun, logger, "sudo", "-u", username, "docker", "rm", "-f", name)
		default:
			resp.Error = "invalid docker action"
			return resp
		}

	case agentproto.CmdDockerLogs:
		username, _ := req.Args["username"].(string)
		name, _ := req.Args["name"].(string)
		return runExec(resp, dryRun, logger, "sudo", "-u", username, "docker", "logs", "--tail", "100", name)

	case agentproto.CmdDockerCompose:
		username, _ := req.Args["username"].(string)
		name, _ := req.Args["name"].(string)
		action, _ := req.Args["action"].(string)
		if action == "down" {
			return runExec(resp, dryRun, logger, "sudo", "-u", username, "docker", "compose", "-p", name, "down")
		}
		return runExec(resp, dryRun, logger, "sudo", "-u", username, "docker", "compose", "-p", name, "up", "-d")

	case agentproto.CmdCatalogInstall:
		slug, _ := req.Args["app_slug"].(string)
		domain, _ := req.Args["domain"].(string)
		username, _ := req.Args["username"].(string)
		docroot, _ := req.Args["docroot"].(string)
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		appDef, ok := catalog.Find(slug)
		if !ok {
			resp.Error = "unknown app slug: " + slug
			return resp
		}
		if appDef.Type == "docker" {
			cname := "stako-" + slug + "-" + strings.ReplaceAll(domain, ".", "-")
			if dryRun {
				return runExec(resp, true, logger, "docker", "run", "-d", "--name", cname, appDef.Image)
			}
			if appDef.RepoURL != "" {
				buildDir := "/var/lib/stako/catalog-builds/" + slug
				_ = os.MkdirAll(filepath.Dir(buildDir), 0o750)
				if _, err := os.Stat(filepath.Join(buildDir, ".git")); err == nil {
					if r := runExec(resp, false, logger, "git", "-C", buildDir, "pull", "--ff-only"); !r.OK {
						return r
					}
				} else {
					if r := runExec(resp, false, logger, "git", "clone", "--depth", "1", appDef.RepoURL, buildDir); !r.OK {
						return r
					}
				}
				if r := runExec(resp, false, logger, "docker", "build", "-t", appDef.Image, buildDir); !r.OK {
					return r
				}
			}
			_ = runExec(resp, false, logger, "docker", "rm", "-f", cname)
			args := []string{
				"run", "-d", "--name", cname,
				"--restart", "unless-stopped", "--security-opt", "no-new-privileges", "--cap-drop=ALL",
				"-p", fmt.Sprintf("127.0.0.1:0:%d", appDef.DefaultPort),
				appDef.Image,
			}
			args = append(args, appDef.CmdArgs...)
			return runExec(resp, false, logger, "docker", args...)
		}
		if docroot == "" {
			docroot = "/var/www/" + domain + "/public_html"
		}
		if dryRun {
			return runExec(resp, true, logger, "install-php-app", slug, docroot)
		}
		if err := os.MkdirAll(docroot, 0o755); err != nil {
			resp.Error = err.Error()
			return resp
		}
		var r agentproto.Response
		switch slug {
		case "wordpress":
			r = runExec(resp, false, logger, "bash", "-c",
				fmt.Sprintf("curl -fsSL https://wordpress.org/latest.tar.gz | tar -xz --strip-components=1 -C %q", docroot))
		case "adminer":
			r = runExec(resp, false, logger, "curl", "-fsSL",
				"https://github.com/vrana/adminer/releases/download/v4.8.1/adminer-4.8.1.php",
				"-o", filepath.Join(docroot, "index.php"))
		default:
			if err := os.WriteFile(filepath.Join(docroot, "index.php"), []byte("<?php echo 'Stako "+appDef.Name+" ready';\n"), 0o644); err != nil {
				resp.Error = err.Error()
				return resp
			}
			r = agentproto.Response{ID: req.ID, OK: true, Output: "Initialized " + appDef.Name + " at " + docroot}
		}
		if username != "" {
			if err := agentproto.ValidateLinuxUsername(username); err == nil {
				_ = exec.Command("chown", "-R", username+":"+username, docroot).Run()
			}
		}
		return r

	case agentproto.CmdCatalogUninstall:
		slug, _ := req.Args["app_slug"].(string)
		domain, _ := req.Args["domain"].(string)
		appType, _ := req.Args["app_type"].(string)
		if appType == "docker" && domain != "" {
			cname := "stako-" + slug + "-" + strings.ReplaceAll(domain, ".", "-")
			return runExec(resp, dryRun, logger, "docker", "rm", "-f", cname)
		}
		resp.OK = true
		resp.Output = "uninstalled " + slug + " from " + domain
		return resp

	case agentproto.CmdRedisCreate:
		name, _ := req.Args["name"].(string)
		port := 6380
		if pf, ok := req.Args["port"].(float64); ok && pf > 0 {
			port = int(pf)
		}
		pass, _ := req.Args["password"].(string)
		memMB := 256
		if mf, ok := req.Args["memory_mb"].(float64); ok && mf > 0 {
			memMB = int(mf)
		}
		persistent, _ := req.Args["persistent"].(bool)
		if err := agentproto.ValidateLinuxUsername(name); err != nil {
			resp.Error = "invalid redis instance name: " + err.Error()
			return resp
		}
		if dryRun {
			return runExec(resp, true, logger, "systemctl", "enable", "--now", "redis-server@"+name)
		}
		dataDir := "/var/lib/redis-" + name
		_ = os.MkdirAll(dataDir, 0o750)
		_ = os.MkdirAll("/etc/redis", 0o755)
		_ = exec.Command("chown", "redis:redis", dataDir).Run()
		persistLine := "save \"\"\nappendonly no"
		if persistent {
			persistLine = "save 900 1\nsave 300 10\nappendonly yes"
		}
		redisConf := fmt.Sprintf(`# Managed by Stako. Do not edit manually.
bind 127.0.0.1 ::1
port %d
requirepass %s
maxmemory %dmb
maxmemory-policy allkeys-lru
dir %s
pidfile /run/redis/redis-server-%s.pid
logfile /var/log/redis/redis-server-%s.log
%s
`, port, pass, memMB, dataDir, name, name, persistLine)
		confPath := "/etc/redis/redis-" + name + ".conf"
		if err := os.WriteFile(confPath, []byte(redisConf), 0o640); err != nil {
			resp.Error = err.Error()
			return resp
		}
		_ = exec.Command("chown", "redis:redis", confPath).Run()
		return runExec(resp, false, logger, "systemctl", "enable", "--now", "redis-server@"+name)

	case agentproto.CmdRedisDelete:
		name, _ := req.Args["name"].(string)
		if err := agentproto.ValidateLinuxUsername(name); err != nil {
			resp.Error = "invalid redis instance name: " + err.Error()
			return resp
		}
		r := runExec(resp, dryRun, logger, "systemctl", "disable", "--now", "redis-server@"+name)
		if !dryRun {
			_ = os.Remove("/etc/redis/redis-" + name + ".conf")
		}
		return r

	case agentproto.CmdRabbitCreate:
		vhost, _ := req.Args["vhost"].(string)
		username, _ := req.Args["username"].(string)
		password, _ := req.Args["password"].(string)
		if dryRun {
			return runExec(resp, true, logger, "rabbitmqctl", "add_vhost", vhost)
		}
		if r := runExec(resp, false, logger, "rabbitmqctl", "add_vhost", vhost); !r.OK {
			return r
		}
		if username != "" && password != "" {
			if r := runExec(resp, false, logger, "rabbitmqctl", "add_user", username, password); !r.OK {
				_ = runExec(resp, false, logger, "rabbitmqctl", "change_password", username, password)
			}
			return runExec(resp, false, logger, "rabbitmqctl", "set_permissions", "-p", vhost, username, ".*", ".*", ".*")
		}
		resp.OK = true
		return resp

	case agentproto.CmdRabbitDelete:
		vhost, _ := req.Args["vhost"].(string)
		return runExec(resp, dryRun, logger, "rabbitmqctl", "delete_vhost", vhost)

	case agentproto.CmdMailDomainAdd:
		domain, _ := req.Args["domain"].(string)
		return runExec(resp, dryRun, logger, "stako-mail", "domain-add", domain)

	case agentproto.CmdMailDomainDel:
		domain, _ := req.Args["domain"].(string)
		return runExec(resp, dryRun, logger, "stako-mail", "domain-del", domain)

	case agentproto.CmdMailRelayApply:
		if dryRun {
			return runExec(resp, true, logger, "postmap", "/etc/postfix/sasl_passwd")
		}
		return applyPostfixRelays(resp, logger, req.Args)

	case agentproto.CmdBackupRun:
		id, _ := req.Args["id"].(string)
		kind, _ := req.Args["kind"].(string)
		username, _ := req.Args["username"].(string)
		var candidates []string
		if kind == "system_config" {
			candidates = []string{"/etc/stako", "/etc/nginx/sites-enabled", "/var/lib/stako", "/etc/php"}
		} else {
			if username != "" && agentproto.ValidateLinuxUsername(username) == nil {
				candidates = []string{"/home/" + username}
			} else {
				candidates = []string{"/home"}
			}
		}
		if dryRun {
			args := append([]string{"backup", "--tag", id}, candidates...)
			return runExec(resp, true, logger, "restic", args...)
		}
		var paths []string
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				paths = append(paths, p)
			}
		}
		if len(paths) == 0 {
			resp.Error = "no backup source directories exist for kind: " + kind
			return resp
		}
		resticArgs, err := ensureResticRepo(logger)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		args := append(resticArgs, "backup", "--tag", id, "--tag", kind)
		args = append(args, paths...)
		return runExec(resp, false, logger, "restic", args...)

	case agentproto.CmdBackupRestore:
		id, _ := req.Args["id"].(string)
		if dryRun {
			return runExec(resp, true, logger, "restic", "restore", id)
		}
		resticArgs, err := ensureResticRepo(logger)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		args := append(resticArgs, "restore", "latest", "--tag", id, "--target", "/")
		return runExec(resp, false, logger, "restic", args...)

	case agentproto.CmdFileList:
		p, _ := req.Args["path"].(string)
		if dryRun {
			resp.OK = true
			resp.Data = map[string]any{
				"items": []map[string]any{
					{"name": "public_html", "is_dir": true, "size": 4096, "mode": "drwxr-x---", "mod_time": "2026-11-25T10:00:00Z"},
					{"name": "index.html", "is_dir": false, "size": 128, "mode": "-rw-r-----", "mod_time": "2026-11-25T10:00:00Z"},
				},
			}
			return resp
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		items := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			info, _ := e.Info()
			var size int64
			var mode, octalMode, modTime string
			if info != nil {
				size = info.Size()
				mode = info.Mode().String()
				octalMode = fmt.Sprintf("%04o", info.Mode().Perm())
				modTime = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
			}
			items = append(items, map[string]any{
				"name": e.Name(), "is_dir": e.IsDir(), "size": size, "mode": mode, "octal_mode": octalMode, "mod_time": modTime,
			})
		}
		resp.OK = true
		resp.Data = map[string]any{"items": items}
		return resp

	case agentproto.CmdFileRead:
		p, _ := req.Args["path"].(string)
		if dryRun {
			resp.OK = true
			resp.Output = "<!-- dry-run file content: " + p + " -->\n"
			return resp
		}
		b, err := os.ReadFile(p)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.OK = true
		resp.Output = string(b)
		return resp

	case agentproto.CmdFileReadB64:
		p, _ := req.Args["path"].(string)
		if dryRun {
			resp.OK = true
			resp.Data = map[string]any{"b64": base64.StdEncoding.EncodeToString([]byte("dry-run:" + p))}
			return resp
		}
		info, err := os.Stat(p)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		if info.IsDir() {
			resp.Error = "path is a directory"
			return resp
		}
		if info.Size() > 32*1024*1024 {
			resp.Error = "file exceeds 32MB limit"
			return resp
		}
		b, err := os.ReadFile(p)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.OK = true
		resp.Data = map[string]any{"b64": base64.StdEncoding.EncodeToString(b)}
		return resp

	case agentproto.CmdFileWrite:
		p, _ := req.Args["path"].(string)
		content, _ := req.Args["content"].(string)
		username, _ := req.Args["username"].(string)
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] write " + p
			return resp
		}
		_ = os.MkdirAll(filepath.Dir(p), 0o750)
		if err := os.WriteFile(p, []byte(content), 0o640); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			_ = exec.Command("chown", username+":www-data", p).Run()
		}
		resp.OK = true
		return resp

	case agentproto.CmdFileWriteB64:
		p, _ := req.Args["path"].(string)
		b64, _ := req.Args["b64"].(string)
		username, _ := req.Args["username"].(string)
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] upload " + p
			return resp
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		_ = os.MkdirAll(filepath.Dir(p), 0o750)
		if err := os.WriteFile(p, raw, 0o640); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			_ = exec.Command("chown", username+":www-data", p).Run()
		}
		resp.OK = true
		return resp

	case agentproto.CmdFileDelete:
		p, _ := req.Args["path"].(string)
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] delete " + p
			return resp
		}
		if err := os.RemoveAll(p); err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.OK = true
		return resp

	case agentproto.CmdFileMkdir:
		p, _ := req.Args["path"].(string)
		username, _ := req.Args["username"].(string)
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] mkdir " + p
			return resp
		}
		if err := os.MkdirAll(p, 0o750); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			ensureDocrootPermissions(username, p)
		}
		resp.OK = true
		return resp

	case agentproto.CmdFileCopy:
		src, _ := req.Args["src"].(string)
		dst, _ := req.Args["dst"].(string)
		username, _ := req.Args["username"].(string)
		if dryRun {
			return runExec(resp, true, logger, "cp", "-a", src, dst)
		}
		_ = os.MkdirAll(filepath.Dir(dst), 0o750)
		r := runExec(resp, false, logger, "cp", "-a", src, dst)
		if r.OK && username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			_ = exec.Command("chown", "-R", username+":www-data", dst).Run()
		}
		return r

	case agentproto.CmdFileMove:
		src, _ := req.Args["src"].(string)
		dst, _ := req.Args["dst"].(string)
		username, _ := req.Args["username"].(string)
		if dryRun {
			return runExec(resp, true, logger, "mv", src, dst)
		}
		_ = os.MkdirAll(filepath.Dir(dst), 0o750)
		r := runExec(resp, false, logger, "mv", src, dst)
		if r.OK && username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			_ = exec.Command("chown", "-R", username+":www-data", dst).Run()
		}
		return r

	case agentproto.CmdFileChmod:
		p, _ := req.Args["path"].(string)
		modeStr, _ := req.Args["mode"].(string)
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] chmod " + modeStr + " " + p
			return resp
		}
		mVal, err := strconv.ParseUint(modeStr, 8, 32)
		if err != nil {
			resp.Error = "invalid octal mode: " + modeStr
			return resp
		}
		if err := os.Chmod(p, os.FileMode(mVal)); err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.OK = true
		return resp

	case agentproto.CmdFileExtract:
		src, _ := req.Args["src"].(string)
		dstDir, _ := req.Args["dst_dir"].(string)
		username, _ := req.Args["username"].(string)
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] extract " + src + " -> " + dstDir
			return resp
		}
		_ = os.MkdirAll(dstDir, 0o750)
		low := strings.ToLower(src)
		var r agentproto.Response
		switch {
		case strings.HasSuffix(low, ".zip"):
			r = runExec(resp, false, logger, "unzip", "-o", src, "-d", dstDir)
		case strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz"):
			r = runExec(resp, false, logger, "tar", "-xzf", src, "-C", dstDir)
		case strings.HasSuffix(low, ".tar.bz2") || strings.HasSuffix(low, ".tbz2"):
			r = runExec(resp, false, logger, "tar", "-xjf", src, "-C", dstDir)
		case strings.HasSuffix(low, ".tar"):
			r = runExec(resp, false, logger, "tar", "-xf", src, "-C", dstDir)
		default:
			resp.Error = "unsupported archive format (supported: .zip, .tar.gz, .tgz, .tar.bz2, .tar)"
			return resp
		}
		if r.OK && username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			_ = exec.Command("chown", "-R", username+":www-data", dstDir).Run()
		}
		return r

	case agentproto.CmdFileCompress:
		src, _ := req.Args["src"].(string)
		dst, _ := req.Args["dst"].(string)
		username, _ := req.Args["username"].(string)
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] compress " + src + " -> " + dst
			return resp
		}
		parent := filepath.Dir(src)
		base := filepath.Base(src)
		var r agentproto.Response
		if strings.HasSuffix(strings.ToLower(dst), ".zip") {
			r = runExec(resp, false, logger, "bash", "-c", fmt.Sprintf("cd %q && zip -r %q %q", parent, dst, base))
		} else {
			r = runExec(resp, false, logger, "tar", "-czf", dst, "-C", parent, base)
		}
		if r.OK && username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			_ = exec.Command("chown", username+":www-data", dst).Run()
		}
		return r

	case agentproto.CmdTerminalExec:
		username, _ := req.Args["username"].(string)
		cmdStr, _ := req.Args["command"].(string)
		return runExec(resp, dryRun, logger, "sudo", "-u", username, "bash", "-lc", cmdStr)

	case agentproto.CmdWAFApply:
		enabled, _ := req.Args["enabled"].(bool)
		engine, _ := req.Args["engine"].(string)
		mode, _ := req.Args["mode"].(string)
		paranoia := 1
		if pf, ok := req.Args["paranoia_level"].(float64); ok && pf >= 1 && pf <= 4 {
			paranoia = int(pf)
		}
		if dryRun {
			return runExec(resp, true, logger, "stako-waf", engine, mode)
		}
		return applyWAFConfig(resp, logger, enabled, engine, mode, paranoia)

	case agentproto.CmdGitDeploy:
		repo, _ := req.Args["repo_url"].(string)
		branch, _ := req.Args["branch"].(string)
		buildCmd, _ := req.Args["build_cmd"].(string)
		username, _ := req.Args["username"].(string)
		targetDir, _ := req.Args["target_dir"].(string)
		if branch == "" {
			branch = "main"
		}
		if dryRun || targetDir == "" || username == "" || agentproto.ValidateLinuxUsername(username) != nil {
			return runExec(resp, dryRun, logger, "git", "clone", "--depth", "1", "-b", branch, repo)
		}
		_ = os.MkdirAll(filepath.Dir(targetDir), 0o750)
		ensureDocrootPermissions(username, targetDir)
		if _, err := os.Stat(filepath.Join(targetDir, ".git")); err == nil {
			if r := runExec(resp, false, logger, "sudo", "-u", username, "git", "-C", targetDir, "fetch", "origin", branch); !r.OK {
				return r
			}
			if r := runExec(resp, false, logger, "sudo", "-u", username, "git", "-C", targetDir, "checkout", "-f", branch); !r.OK {
				return r
			}
			if r := runExec(resp, false, logger, "sudo", "-u", username, "git", "-C", targetDir, "reset", "--hard", "origin/"+branch); !r.OK {
				return r
			}
		} else {
			if r := runExec(resp, false, logger, "sudo", "-u", username, "git", "clone", "--depth", "1", "-b", branch, repo, targetDir); !r.OK {
				return r
			}
		}
		if strings.TrimSpace(buildCmd) != "" {
			return runExec(resp, false, logger, "sudo", "-u", username, "bash", "-lc", "cd "+targetDir+" && "+buildCmd)
		}
		resp.OK = true
		resp.Output = "deployed " + repo + "@" + branch + " to " + targetDir
		return resp

	case agentproto.CmdGitPreviewDeploy:
		previewID, _ := req.Args["preview_id"].(string)
		repo, _ := req.Args["repo_url"].(string)
		branch, _ := req.Args["branch"].(string)
		username, _ := req.Args["username"].(string)
		containerName, _ := req.Args["container_name"].(string)
		image, _ := req.Args["image"].(string)
		buildCmd, _ := req.Args["build_cmd"].(string)
		hostPort := 18001
		if hp, ok := req.Args["host_port"].(float64); ok && hp > 0 {
			hostPort = int(hp)
		}
		containerPort := 3000
		if cp, ok := req.Args["container_port"].(float64); ok && cp > 0 {
			containerPort = int(cp)
		}
		if branch == "" {
			branch = "main"
		}
		if image == "" {
			image = "node:20-alpine"
		}
		if dryRun || username == "" || agentproto.ValidateLinuxUsername(username) != nil {
			return runExec(resp, true, logger, "docker", "run", "-d", "--name", containerName, "-p", fmt.Sprintf("127.0.0.1:%d:%d", hostPort, containerPort), image)
		}
		previewDir := filepath.Join("/home", username, "previews", previewID)
		_ = os.MkdirAll(filepath.Dir(previewDir), 0o750)
		if _, err := os.Stat(filepath.Join(previewDir, ".git")); err == nil {
			_ = exec.Command("sudo", "-u", username, "git", "-C", previewDir, "fetch", "origin", branch).Run()
			_ = exec.Command("sudo", "-u", username, "git", "-C", previewDir, "checkout", "-f", branch).Run()
			_ = exec.Command("sudo", "-u", username, "git", "-C", previewDir, "reset", "--hard", "origin/"+branch).Run()
		} else {
			if r := runExec(resp, false, logger, "sudo", "-u", username, "git", "clone", "--depth", "1", "-b", branch, repo, previewDir); !r.OK {
				return r
			}
		}
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
		portMap := fmt.Sprintf("127.0.0.1:%d:%d", hostPort, containerPort)
		if _, err := os.Stat(filepath.Join(previewDir, "Dockerfile")); err == nil {
			imgTag := containerName + ":latest"
			if r := runExec(resp, false, logger, "docker", "build", "-t", imgTag, previewDir); !r.OK {
				return r
			}
			return runExec(resp, false, logger, "docker", "run", "-d", "--name", containerName, "--restart", "unless-stopped", "-p", portMap, imgTag)
		}
		if strings.TrimSpace(buildCmd) != "" {
			_ = exec.Command("sudo", "-u", username, "bash", "-lc", "cd "+previewDir+" && "+buildCmd).Run()
		}
		return runExec(resp, false, logger, "docker", "run", "-d", "--name", containerName, "--restart", "unless-stopped", "-p", portMap, "-v", previewDir+":/app", "-w", "/app", image)

	case agentproto.CmdGitPreviewDelete:
		previewID, _ := req.Args["preview_id"].(string)
		username, _ := req.Args["username"].(string)
		containerName, _ := req.Args["container_name"].(string)
		if dryRun {
			return runExec(resp, true, logger, "docker", "rm", "-f", containerName)
		}
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
		if username != "" && previewID != "" && agentproto.ValidateLinuxUsername(username) == nil && !strings.Contains(previewID, "..") {
			_ = os.RemoveAll(filepath.Join("/home", username, "previews", previewID))
		}
		resp.OK = true
		resp.Output = "removed preview container " + containerName
		return resp

	case agentproto.CmdWPScan:
		return wpScanSites(resp, dryRun, logger, req.Args)

	case agentproto.CmdWPUpdate:
		return wpUpdateSite(resp, dryRun, logger, req.Args)

	case agentproto.CmdWPStaging:
		return wpStagingSync(resp, dryRun, logger, req.Args)

	case agentproto.CmdWPSecurityHarden:
		return wpHardenSite(resp, dryRun, logger, req.Args)

	case agentproto.CmdWPAutoLogin:
		return wpAutoLogin(resp, dryRun, logger, req.Args)

	case agentproto.CmdPGPoolApply:
		return applyPgBouncer(resp, dryRun, logger, req.Args)

	case agentproto.CmdPGReplicationApply:
		return applyPGReplication(resp, dryRun, logger, req.Args)

	case agentproto.CmdPGReplicationStatus:
		return getPGReplicationStatus(resp, dryRun)

	case agentproto.CmdPGPITRApply:
		return applyPGPITR(resp, dryRun, logger, req.Args)

	case agentproto.CmdNginxMicrocacheApply:
		return applyNginxMicrocache(resp, dryRun, logger, req.Args)

	case agentproto.CmdRedisObjectCacheApply:
		return applyRedisObjectCache(resp, dryRun, logger, req.Args)

	case agentproto.CmdHardwareHealth:
		return collectHardwareHealth(resp, dryRun)

	case agentproto.CmdUserQuotaSet:
		return applyUserProjectQuota(resp, dryRun, logger, req.Args)

	case agentproto.CmdUserQuotaReport:
		return reportUserProjectQuotas(resp, dryRun, req.Args)

	case agentproto.CmdPanelImport:
		return runPanelImport(resp, dryRun, logger, req.Args)

	case agentproto.CmdMigrationInspect:
		return inspectMigrationArchive(resp, dryRun, logger, req.Args)

	case agentproto.CmdMigrationExport:
		return exportStakoAccount(resp, dryRun, logger, req.Args)

	case agentproto.CmdMigrationTransfer:
		return transferMigrationArchive(resp, dryRun, logger, req.Args)

	case agentproto.CmdMigrationListBundles:
		return listMigrationBundles(resp, dryRun)

	case agentproto.CmdSysConfigRead:
		p, _ := req.Args["path"].(string)
		if dryRun {
			resp.OK = true
			return resp
		}
		b, err := os.ReadFile(p)
		if err != nil {
			resp.Error = err.Error()
			return resp
		}
		resp.OK = true
		resp.Output = string(b)
		return resp

	case agentproto.CmdSysConfigApply:
		p, _ := req.Args["path"].(string)
		content, _ := req.Args["content"].(string)
		testCmd, _ := req.Args["test_cmd"].(string)
		reloadSvc, _ := req.Args["reload_svc"].(string)
		if dryRun {
			logger.Info("dry-run sysconfig apply", "path", p, "test", testCmd, "reload", reloadSvc)
			resp.OK = true
			resp.Output = "[dry-run] validated (" + testCmd + ") and reloaded " + reloadSvc
			return resp
		}
		bakPath := p + ".stako.bak"
		if orig, err := os.ReadFile(p); err == nil {
			_ = os.WriteFile(bakPath, orig, 0o600)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if testCmd != "" {
			parts := strings.Fields(testCmd)
			testResp := runExec(resp, false, logger, parts[0], parts[1:]...)
			if !testResp.OK {
				// Automatic rollback on validation failure
				if bak, err := os.ReadFile(bakPath); err == nil {
					_ = os.WriteFile(p, bak, 0o644)
				}
				return testResp
			}
		}
		if reloadSvc != "" {
			return runExec(resp, false, logger, "systemctl", "reload", reloadSvc)
		}
		resp.OK = true
		return resp

	case agentproto.CmdPanelUpdate:
		ver, _ := req.Args["version"].(string)
		if ver == "" {
			ver = "latest"
		}
		if dryRun {
			return runExec(resp, true, logger, "bash", "/usr/local/share/stako/install.sh", "--update", "--version", ver)
		}
		return runExec(resp, false, logger, "bash", "-c", "curl -fsSL https://raw.githubusercontent.com/StakoCP/stako/refs/heads/main/installer/install.sh | bash -s -- --update --version "+ver)

	case agentproto.CmdSysServiceList:
		outMap := map[string]any{}
		if rawList, ok := req.Args["services"].([]any); ok {
			for _, item := range rawList {
				m, _ := item.(map[string]any)
				id, _ := m["id"].(string)
				unit, _ := m["unit"].(string)
				pkg, _ := m["apt_pkg"].(string)
				if id == "" {
					continue
				}
				if dryRun {
					active := "inactive"
					installed := false
					ver := ""
					if id == "nginx" || id == "mariadb" || id == "php8.4-fpm" || id == "ssh" || id == "nftables" || id == "fail2ban" {
						installed = true
						active = "active"
						ver = "1.0.0-dryrun"
					}
					outMap[id] = map[string]any{"installed": installed, "version": ver, "active": active, "enabled": installed}
					continue
				}
				installed := false
				ver := ""
				pkgCandidates := []string{pkg}
				switch id {
				case "proftpd":
					pkgCandidates = []string{"proftpd-core", "proftpd-basic"}
				case "docker":
					pkgCandidates = []string{"docker-ce", "docker.io"}
				case "mariadb":
					pkgCandidates = []string{"mariadb-server", "mysql-server"}
				}
				for _, cand := range pkgCandidates {
					if cand == "" {
						continue
					}
					if stOut, err := exec.Command("dpkg-query", "-W", "-f=${Status}|${Version}", cand).Output(); err == nil {
						parts := strings.SplitN(strings.TrimSpace(string(stOut)), "|", 2)
						if len(parts) == 2 && parts[0] == "install ok installed" {
							installed = true
							ver = parts[1]
							break
						}
					}
				}
				if !installed && unit != "" {
					if loadOut, err := exec.Command("systemctl", "show", "-p", "LoadState", "--value", unit).Output(); err == nil {
						if strings.TrimSpace(string(loadOut)) == "loaded" {
							installed = true
						}
					}
				}
				active := "not-installed"
				enabled := false
				if installed {
					if unit == "" {
						active = "cli"
					} else {
						actOut, _ := exec.Command("systemctl", "is-active", unit).Output()
						active = strings.TrimSpace(string(actOut))
						if active == "" {
							active = "inactive"
						}
						enOut, _ := exec.Command("systemctl", "is-enabled", unit).Output()
						enabled = strings.TrimSpace(string(enOut)) == "enabled"
					}
				}
				outMap[id] = map[string]any{"installed": installed, "version": ver, "active": active, "enabled": enabled}
			}
		}
		resp.OK = true
		resp.Data = map[string]any{"services": outMap}
		return resp

	case agentproto.CmdSysServiceAct:
		unit, _ := req.Args["unit"].(string)
		action, _ := req.Args["action"].(string)
		return runExec(resp, dryRun, logger, "systemctl", action, unit)

	case agentproto.CmdSysServicePkg:
		action, _ := req.Args["action"].(string)
		pkg, _ := req.Args["apt_pkg"].(string)
		unit, _ := req.Args["unit"].(string)
		if action == "uninstall" {
			if unit != "" {
				_ = runExec(resp, dryRun, logger, "systemctl", "disable", "--now", unit)
			}
			r := runExec(resp, dryRun, logger, "apt-get", "remove", "--purge", "-y", pkg)
			_ = runExec(resp, dryRun, logger, "apt-get", "autoremove", "-y")
			return r
		}
		_ = runExec(resp, dryRun, logger, "apt-get", "update", "-y")
		r := runExec(resp, dryRun, logger, "apt-get", "install", "-y", pkg)
		if r.OK && unit != "" {
			_ = runExec(resp, dryRun, logger, "systemctl", "enable", "--now", unit)
		}
		return r

	case agentproto.CmdLogsQuery:
		return queryCentralLogs(resp, dryRun, logger, req.Args)

	case agentproto.CmdLogsRotate:
		return applyLogRotation(resp, dryRun, logger, req.Args)

	case agentproto.CmdLogsShippingApply:
		return applyLogShipping(resp, dryRun, logger, req.Args)

	default:
		logger.Warn("rejected unknown command", "command", req.Command)
		resp.Error = "command not in allowlist: " + req.Command
		return resp
	}
}

func runExec(resp agentproto.Response, dryRun bool, logger *slog.Logger, bin string, args ...string) agentproto.Response {
	cmdline := bin + " " + strings.Join(args, " ")
	if dryRun {
		logger.Info("dry-run exec", "cmd", cmdline)
		resp.OK = true
		resp.Output = "[dry-run] " + cmdline
		return resp
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	resp.Output = strings.TrimSpace(string(out))
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	resp.OK = true
	return resp
}

func detectPublicIP(endpoint string) string {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(endpoint)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if net.ParseIP(s) == nil {
		return ""
	}
	return s
}

func ensureDocrootPermissions(username, docroot string) {
	homeDir := "/home/" + username
	_ = exec.Command("chown", username+":www-data", homeDir).Run()
	_ = os.Chmod(homeDir, 0o710)

	clean := filepath.Clean(docroot)
	for cur := clean; cur != homeDir && strings.HasPrefix(cur, homeDir+"/"); cur = filepath.Dir(cur) {
		_ = exec.Command("chown", username+":www-data", cur).Run()
		_ = os.Chmod(cur, 0o750)
	}
	_ = exec.Command("chown", "-R", username+":www-data", clean).Run()
}

func isValidPHPVersion(v string) bool {
	if len(v) < 3 || len(v) > 5 {
		return false
	}
	dot := strings.IndexByte(v, '.')
	if dot <= 0 || dot == len(v)-1 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if i == dot {
			continue
		}
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}

func ensurePHPFpmPool(username, phpVer string, logger *slog.Logger) {
	if !isValidPHPVersion(phpVer) {
		return
	}
	poolDir := fmt.Sprintf("/etc/php/%s/fpm/pool.d", phpVer)
	st, err := os.Stat(poolDir)
	if err != nil || !st.IsDir() {
		return
	}
	poolPath := filepath.Join(poolDir, username+".conf")
	sockPath := fmt.Sprintf("/run/php/php%s-fpm-%s.sock", phpVer, username)
	if _, err := os.Stat(poolPath); err == nil {
		if _, err := os.Stat(sockPath); err == nil {
			return
		}
	}
	poolConf := fmt.Sprintf(`; Managed by Stako. Do not edit manually.
[%s]
user = %s
group = %s
listen = /run/php/php%s-fpm-%s.sock
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = dynamic
pm.max_children = 10
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 4
chdir = /home/%s
`, username, username, username, phpVer, username, username)
	if err := os.WriteFile(poolPath, []byte(poolConf), 0o644); err != nil {
		logger.Warn("failed to write php-fpm pool", "path", poolPath, "err", err)
		return
	}
	if out, err := exec.Command("systemctl", "reload-or-restart", "php"+phpVer+"-fpm").CombinedOutput(); err != nil {
		logger.Warn("failed to reload php-fpm", "unit", "php"+phpVer+"-fpm", "err", err, "output", strings.TrimSpace(string(out)))
	}
}

func ensureResticRepo(logger *slog.Logger) ([]string, error) {
	repo := os.Getenv("RESTIC_REPOSITORY")
	if repo == "" {
		repo = "/var/backups/stako/restic-repo"
	}
	pwFile := os.Getenv("RESTIC_PASSWORD_FILE")
	if pwFile == "" && os.Getenv("RESTIC_PASSWORD") == "" {
		pwFile = "/etc/stako/restic.key"
		if _, err := os.Stat(pwFile); os.IsNotExist(err) {
			_ = os.MkdirAll(filepath.Dir(pwFile), 0o750)
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				return nil, err
			}
			if err := os.WriteFile(pwFile, []byte(hex.EncodeToString(buf)+"\n"), 0o600); err != nil {
				return nil, fmt.Errorf("write restic key: %w", err)
			}
		}
	}
	baseArgs := []string{"-r", repo}
	if pwFile != "" {
		baseArgs = append(baseArgs, "--password-file", pwFile)
	}
	if _, err := os.Stat(filepath.Join(repo, "config")); os.IsNotExist(err) {
		_ = os.MkdirAll(repo, 0o750)
		initArgs := append(append([]string{}, baseArgs...), "init")
		out, err := exec.Command("restic", initArgs...).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("restic init failed: %v: %s", err, strings.TrimSpace(string(out)))
		}
		logger.Info("initialized restic backup repository", "repo", repo)
	}
	return baseArgs, nil
}

func isPkgInstalled(pkg string) bool {
	out, err := exec.Command("dpkg-query", "-W", "-f=${Status}", pkg).Output()
	return err == nil && strings.TrimSpace(string(out)) == "install ok installed"
}

func applyWAFConfig(resp agentproto.Response, logger *slog.Logger, enabled bool, engine, mode string, paranoia int) agentproto.Response {
	_ = os.MkdirAll("/etc/stako", 0o750)
	stateConf := fmt.Sprintf("enabled=%v\nengine=%s\nmode=%s\nparanoia_level=%d\n", enabled, engine, mode, paranoia)
	if err := os.WriteFile("/etc/stako/waf.conf", []byte(stateConf), 0o640); err != nil {
		resp.Error = err.Error()
		return resp
	}

	hasNginx := isPkgInstalled("nginx")
	hasApache := isPkgInstalled("apache2")
	nginxWAFConf := "/etc/nginx/conf.d/stako-waf.conf"
	apacheAvail := "/etc/apache2/conf-available/stako-waf.conf"
	apacheEnabled := "/etc/apache2/conf-enabled/stako-waf.conf"

	if !enabled {
		if hasNginx {
			if _, err := os.Stat(nginxWAFConf); err == nil {
				_ = os.Remove(nginxWAFConf)
				if t := runExec(resp, false, logger, "nginx", "-t"); t.OK {
					_ = runExec(resp, false, logger, "systemctl", "reload", "nginx")
				}
			}
		}
		if hasApache {
			if _, err := os.Stat(apacheEnabled); err == nil {
				_ = os.Remove(apacheEnabled)
			}
			if _, err := os.Stat(apacheAvail); err == nil {
				_ = os.Remove(apacheAvail)
				if t := runExec(resp, false, logger, "apache2ctl", "-t"); t.OK {
					_ = runExec(resp, false, logger, "systemctl", "reload", "apache2")
				}
			}
		}
		resp.OK = true
		resp.Output = "WAF disabled; configuration saved to /etc/stako/waf.conf"
		return resp
	}

	secRuleEngine := "DetectionOnly"
	if mode == "blocking" {
		secRuleEngine = "On"
	}
	var applied []string

	if hasNginx {
		if !checkNginxModSecurity() {
			logger.Info("nginx modsecurity module not found, installing libnginx-mod-http-modsecurity")
			_ = exec.Command("apt-get", "update", "-y").Run()
			cmd := exec.Command("apt-get", "install", "-y", "libnginx-mod-http-modsecurity")
			cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
			if out, err := cmd.CombinedOutput(); err != nil {
				resp.Output = strings.TrimSpace(string(out))
				resp.Error = fmt.Sprintf("failed to install libnginx-mod-http-modsecurity: %v", err)
				return resp
			}
			if !checkNginxModSecurity() {
				resp.Error = "libnginx-mod-http-modsecurity installed, but nginx modsecurity module is still not detected"
				return resp
			}
		}
		_ = os.MkdirAll("/etc/nginx/modsec", 0o755)
		modsecMain := fmt.Sprintf("# Managed by Stako\nSecRuleEngine %s\nSecAction \"id:900000,phase:1,nolog,pass,t:none,setvar:tx.paranoia_level=%d\"\n", secRuleEngine, paranoia)
		if err := os.WriteFile("/etc/nginx/modsec/main.conf", []byte(modsecMain), 0o644); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if err := os.WriteFile(nginxWAFConf, []byte("modsecurity on;\nmodsecurity_rules_file /etc/nginx/modsec/main.conf;\n"), 0o644); err != nil {
			resp.Error = err.Error()
			return resp
		}
		testResp := runExec(resp, false, logger, "nginx", "-t")
		if !testResp.OK {
			_ = os.Remove(nginxWAFConf)
			return testResp
		}
		if r := runExec(resp, false, logger, "systemctl", "reload", "nginx"); !r.OK {
			return r
		}
		applied = append(applied, "nginx")
	}

	if hasApache {
		if !checkApacheModSecurity() {
			if _, err := os.Stat("/etc/apache2/mods-available/security2.load"); os.IsNotExist(err) {
				logger.Info("apache2 security2 module not found, installing libapache2-mod-security2")
				_ = exec.Command("apt-get", "update", "-y").Run()
				cmd := exec.Command("apt-get", "install", "-y", "libapache2-mod-security2")
				cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
				if out, err := cmd.CombinedOutput(); err != nil {
					resp.Output = strings.TrimSpace(string(out))
					resp.Error = fmt.Sprintf("failed to install libapache2-mod-security2: %v", err)
					return resp
				}
			}
			if out, err := exec.Command("a2enmod", "security2").CombinedOutput(); err != nil {
				resp.Output = strings.TrimSpace(string(out))
				resp.Error = fmt.Sprintf("failed to enable apache2 security2 module: %v", err)
				return resp
			}
			if !checkApacheModSecurity() {
				resp.Error = "libapache2-mod-security2 installed, but apache2 security2 module is still not active"
				return resp
			}
		}
		apacheConf := fmt.Sprintf("# Managed by Stako\n<IfModule security2_module>\n    SecRuleEngine %s\n    SecAction \"id:900000,phase:1,nolog,pass,t:none,setvar:tx.paranoia_level=%d\"\n</IfModule>\n", secRuleEngine, paranoia)
		if err := os.WriteFile(apacheAvail, []byte(apacheConf), 0o644); err != nil {
			resp.Error = err.Error()
			return resp
		}
		_ = exec.Command("a2enconf", "stako-waf").Run()
		testResp := runExec(resp, false, logger, "apache2ctl", "-t")
		if !testResp.OK {
			_ = os.Remove(apacheEnabled)
			_ = os.Remove(apacheAvail)
			return testResp
		}
		if r := runExec(resp, false, logger, "systemctl", "reload", "apache2"); !r.OK {
			return r
		}
		applied = append(applied, "apache2")
	}

	resp.OK = true
	if len(applied) == 0 {
		resp.Output = "WAF configuration saved to /etc/stako/waf.conf (no nginx or apache2 installed)"
	} else {
		resp.Output = fmt.Sprintf("WAF applied (%s, mode=%s, paranoia=%d) for: %s", engine, mode, paranoia, strings.Join(applied, ", "))
	}
	return resp
}

func checkNginxModSecurity() bool {
	if modMatches, _ := filepath.Glob("/etc/nginx/modules-enabled/*modsecurity*"); len(modMatches) > 0 {
		return true
	}
	if out, err := exec.Command("nginx", "-V").CombinedOutput(); err == nil {
		return strings.Contains(strings.ToLower(string(out)), "modsecurity")
	}
	return false
}

func checkApacheModSecurity() bool {
	if _, err := os.Stat("/etc/apache2/mods-enabled/security2.load"); err == nil {
		return true
	}
	if out, err := exec.Command("apache2ctl", "-M").CombinedOutput(); err == nil {
		return strings.Contains(strings.ToLower(string(out)), "security2_module")
	}
	return false
}

const cloudflareRealIPNginx = `# Managed by Stako: restore real visitor IP when behind Cloudflare proxy
set_real_ip_from 173.245.48.0/20;
set_real_ip_from 103.21.244.0/22;
set_real_ip_from 103.22.200.0/22;
set_real_ip_from 103.31.4.0/22;
set_real_ip_from 141.101.64.0/18;
set_real_ip_from 108.162.192.0/18;
set_real_ip_from 190.93.240.0/20;
set_real_ip_from 188.114.96.0/20;
set_real_ip_from 197.234.240.0/22;
set_real_ip_from 198.41.128.0/17;
set_real_ip_from 162.158.0.0/15;
set_real_ip_from 104.16.0.0/13;
set_real_ip_from 104.24.0.0/14;
set_real_ip_from 172.64.0.0/13;
set_real_ip_from 131.0.72.0/22;
set_real_ip_from 2400:cb00::/32;
set_real_ip_from 2606:4700::/32;
set_real_ip_from 2803:f800::/32;
set_real_ip_from 2405:b500::/32;
set_real_ip_from 2405:8100::/32;
set_real_ip_from 2a06:98c0::/29;
set_real_ip_from 2c0f:f248::/32;
real_ip_header CF-Connecting-IP;
real_ip_recursive on;
`

const cloudflareRealIPApache = `# Managed by Stako: restore real visitor IP behind Cloudflare or local Nginx
<IfModule remoteip_module>
    RemoteIPHeader CF-Connecting-IP
    RemoteIPTrustedProxy 127.0.0.1 ::1
    RemoteIPTrustedProxy 173.245.48.0/20 103.21.244.0/22 103.22.200.0/22 103.31.4.0/22
    RemoteIPTrustedProxy 141.101.64.0/18 108.162.192.0/18 190.93.240.0/20 188.114.96.0/20
    RemoteIPTrustedProxy 197.234.240.0/22 198.41.128.0/17 162.158.0.0/15 104.16.0.0/13
    RemoteIPTrustedProxy 104.24.0.0/14 172.64.0.0/13 131.0.72.0/22
    RemoteIPTrustedProxy 2400:cb00::/32 2606:4700::/32 2803:f800::/32 2405:b500::/32
    RemoteIPTrustedProxy 2405:8100::/32 2a06:98c0::/29 2c0f:f248::/32
</IfModule>
`

func ensureNginxCloudflareRealIP() bool {
	p := "/etc/nginx/conf.d/cloudflare-realip.conf"
	if _, err := os.Stat(p); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(cloudflareRealIPNginx), 0o644); err == nil {
			return true
		}
	}
	return false
}

func ensureApacheCloudflareRealIP() {
	p := "/etc/apache2/conf-available/cloudflare-realip.conf"
	if _, err := os.Stat(p); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(cloudflareRealIPApache), 0o644)
		_ = exec.Command("a2enconf", "cloudflare-realip").Run()
	}
}

func ensureDBAdminToolsNginx() bool {
	p := "/etc/nginx/conf.d/stako-dbtools.conf"
	if _, err := os.Stat(p); err == nil {
		return false
	}
	phpSock := "/run/php/php8.4-fpm.sock"
	for _, cand := range []string{"/run/php/php8.4-fpm.sock", "/run/php/php8.3-fpm.sock", "/run/php/php8.5-fpm.sock", "/run/php/php-fpm.sock"} {
		if _, err := os.Stat(cand); err == nil {
			phpSock = cand
			break
		}
	}
	roundcubeRoot := "/var/lib/roundcube/public_html/"
	if _, err := os.Stat(roundcubeRoot); err != nil {
		roundcubeRoot = "/usr/share/roundcube/"
	}
	conf := fmt.Sprintf(`# Managed by Stako: local phpMyAdmin, phpPgAdmin & Roundcube backend for :8083 proxy
server {
    listen 127.0.0.1:8084;
    server_name localhost;
    client_max_body_size 128M;

    location ^~ /phpmyadmin/ {
        alias /usr/share/phpmyadmin/;
        index index.php index.html;
        location ~ ^/phpmyadmin/(.+\.php)$ {
            alias /usr/share/phpmyadmin/$1;
            include fastcgi_params;
            fastcgi_param SCRIPT_FILENAME $request_filename;
            fastcgi_pass unix:%s;
        }
    }

    location ^~ /phppgadmin/ {
        alias /usr/share/phppgadmin/;
        index index.php index.html;
        location ~ ^/phppgadmin/(.+\.php)$ {
            alias /usr/share/phppgadmin/$1;
            include fastcgi_params;
            fastcgi_param SCRIPT_FILENAME $request_filename;
            fastcgi_pass unix:%s;
        }
    }

    location ^~ /webmail/ {
        alias %s;
        index index.php index.html;
        location ~ ^/webmail/(.+\.php)$ {
            alias %s$1;
            include fastcgi_params;
            fastcgi_param SCRIPT_FILENAME $request_filename;
            fastcgi_pass unix:%s;
        }
    }
}
`, phpSock, phpSock, roundcubeRoot, roundcubeRoot, phpSock)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	return os.WriteFile(p, []byte(conf), 0o644) == nil
}

func applyPostfixRelays(resp agentproto.Response, logger *slog.Logger, args map[string]any) agentproto.Response {
	if _, err := os.Stat("/etc/postfix"); os.IsNotExist(err) {
		resp.OK = true
		resp.Output = "postfix not installed; relay settings saved in panel DB"
		return resp
	}

	var saslLines []string
	var senderLines []string
	relayHost := ""
	tlsLevel := "may"

	if srv, ok := args["server_relay"].(map[string]any); ok {
		enabled, _ := srv["enabled"].(bool)
		host, _ := srv["host"].(string)
		user, _ := srv["username"].(string)
		pass, _ := srv["password"].(string)
		tlsMode, _ := srv["tls_mode"].(string)
		port := 587
		if pf, ok := srv["port"].(float64); ok && pf > 0 {
			port = int(pf)
		}
		if enabled && host != "" {
			relayHost = fmt.Sprintf("[%s]:%d", host, port)
			if tlsMode == "tls" || tlsMode == "starttls" {
				tlsLevel = "encrypt"
			}
			if user != "" {
				saslLines = append(saslLines, fmt.Sprintf("[%s]:%d %s:%s", host, port, user, pass))
			}
		}
	}

	if doms, ok := args["domain_relays"].([]any); ok {
		for _, raw := range doms {
			d, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			domain, _ := d["domain"].(string)
			host, _ := d["host"].(string)
			user, _ := d["username"].(string)
			pass, _ := d["password"].(string)
			port := 587
			if pf, ok := d["port"].(float64); ok && pf > 0 {
				port = int(pf)
			}
			if domain == "" || host == "" {
				continue
			}
			senderLines = append(senderLines, fmt.Sprintf("@%s [%s]:%d", domain, host, port))
			if user != "" {
				saslLines = append(saslLines, fmt.Sprintf("@%s %s:%s", domain, user, pass))
			}
		}
	}

	saslPath := "/etc/postfix/sasl_passwd"
	if err := os.WriteFile(saslPath, []byte(strings.Join(saslLines, "\n")+"\n"), 0o600); err != nil {
		resp.Error = err.Error()
		return resp
	}
	_ = exec.Command("postmap", saslPath).Run()

	senderPath := "/etc/postfix/sender_relay"
	if err := os.WriteFile(senderPath, []byte(strings.Join(senderLines, "\n")+"\n"), 0o644); err != nil {
		resp.Error = err.Error()
		return resp
	}
	_ = exec.Command("postmap", senderPath).Run()

	postconfPairs := [][2]string{
		{"relayhost", relayHost},
		{"smtp_sasl_auth_enable", "yes"},
		{"smtp_sasl_password_maps", "hash:/etc/postfix/sasl_passwd"},
		{"smtp_sasl_security_options", "noanonymous"},
		{"smtp_tls_security_level", tlsLevel},
		{"smtp_sender_dependent_authentication", "yes"},
		{"sender_dependent_relayhost_maps", "hash:/etc/postfix/sender_relay"},
	}
	if relayHost == "" && len(senderLines) == 0 {
		postconfPairs = [][2]string{
			{"relayhost", ""},
			{"sender_dependent_relayhost_maps", ""},
		}
	}
	for _, kv := range postconfPairs {
		_ = exec.Command("postconf", "-e", kv[0]+"="+kv[1]).Run()
	}

	if r := runExec(resp, false, logger, "postfix", "check"); !r.OK {
		return r
	}
	return runExec(resp, false, logger, "systemctl", "reload", "postfix")
}

func runSecurityScan() []map[string]any {
	findings := []map[string]any{}

	// 1. SSH PermitRootLogin & PasswordAuthentication
	sshRootStatus := "resolved"
	sshPassStatus := "open"
	sshFiles, _ := filepath.Glob("/etc/ssh/sshd_config.d/*.conf")
	sshFiles = append([]string{"/etc/ssh/sshd_config"}, sshFiles...)
	for _, f := range sshFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			low := strings.ToLower(t)
			if strings.HasPrefix(low, "permitrootlogin") && strings.Contains(low, "yes") {
				sshRootStatus = "open"
			}
			if strings.HasPrefix(low, "passwordauthentication") && strings.Contains(low, "no") {
				sshPassStatus = "resolved"
			}
		}
	}
	findings = append(findings,
		map[string]any{
			"id":          "sf_ssh_root",
			"category":    "ssh",
			"severity":    "high",
			"title":       "SSH root login should not allow password authentication",
			"description": "Checks /etc/ssh/sshd_config and sshd_config.d for PermitRootLogin yes.",
			"remediation": "Set PermitRootLogin prohibit-password or no in /etc/ssh/sshd_config",
			"component":   "openssh-server",
			"status":      sshRootStatus,
		},
		map[string]any{
			"id":          "sf_ssh_password",
			"category":    "ssh",
			"severity":    "medium",
			"title":       "Prefer key-based SSH authentication over passwords",
			"description": "Checks whether PasswordAuthentication no is configured for OpenSSH.",
			"remediation": "Set PasswordAuthentication no in /etc/ssh/sshd_config after configuring SSH keys",
			"component":   "openssh-server",
			"status":      sshPassStatus,
		},
	)

	// 2. Firewall (nftables) & Fail2Ban
	fwStatus := "open"
	if out, err := exec.Command("systemctl", "is-active", "nftables").Output(); err == nil && strings.TrimSpace(string(out)) == "active" {
		fwStatus = "resolved"
	}
	findings = append(findings, map[string]any{
		"id":          "sf_firewall_inactive",
		"category":    "firewall",
		"severity":    "high",
		"title":       "Host firewall (nftables) must be active",
		"description": "Verifies that nftables systemd service is running.",
		"remediation": "Run: systemctl enable --now nftables",
		"component":   "nftables",
		"status":      fwStatus,
	})

	f2bStatus := "open"
	if out, err := exec.Command("systemctl", "is-active", "fail2ban").Output(); err == nil && strings.TrimSpace(string(out)) == "active" {
		f2bStatus = "resolved"
	}
	findings = append(findings, map[string]any{
		"id":          "sf_fail2ban_inactive",
		"category":    "intrusion",
		"severity":    "medium",
		"title":       "Fail2Ban brute-force protection must be active",
		"description": "Verifies that fail2ban systemd service is running.",
		"remediation": "Run: systemctl enable --now fail2ban",
		"component":   "fail2ban",
		"status":      f2bStatus,
	})

	// 3. Pending security updates via apt-get -s upgrade
	secUpdates := 0
	if out, err := exec.Command("apt-get", "-s", "upgrade").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Inst ") && strings.Contains(strings.ToLower(line), "security") {
				secUpdates++
			}
		}
	}
	updStatus := "resolved"
	updSev := "low"
	if secUpdates > 0 {
		updStatus = "open"
		updSev = "high"
	}
	findings = append(findings, map[string]any{
		"id":          "sf_apt_security_updates",
		"category":    "packages",
		"severity":    updSev,
		"title":       fmt.Sprintf("Pending Ubuntu security updates (%d)", secUpdates),
		"description": "Checks apt-get -s upgrade for unapplied security patches.",
		"remediation": "Apply security updates from Security -> Updates or run apt-get upgrade -y",
		"component":   "apt",
		"status":      updStatus,
	})

	// 4. Master key file permissions
	keyStatus := "resolved"
	if info, err := os.Stat("/etc/stako/keys/master.key"); err == nil {
		if info.Mode().Perm()&0o077 != 0 {
			keyStatus = "open"
		}
	}
	findings = append(findings, map[string]any{
		"id":          "sf_master_key_perms",
		"category":    "secrets",
		"severity":    "critical",
		"title":       "Stako master key permissions must be 0600",
		"description": "Verifies /etc/stako/keys/master.key is not readable by group or others.",
		"remediation": "Run: chmod 0600 /etc/stako/keys/master.key",
		"component":   "stako-core",
		"status":      keyStatus,
	})

	return findings
}

type centralLogEntry struct {
	Timestamp string `json:"timestamp"`
	Service   string `json:"service"`
	Source    string `json:"source"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

func inferLogLevel(line string) string {
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "error") || strings.Contains(low, "crit") || strings.Contains(low, "fatal") || strings.Contains(low, "panic") || strings.Contains(low, "emerg") || strings.Contains(low, "failed"):
		return "error"
	case strings.Contains(low, "warn"):
		return "warn"
	default:
		return "info"
	}
}

func tailLogLines(path string, maxLines int) []string {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return nil
	}
	out, err := exec.Command("tail", "-n", strconv.Itoa(maxLines), path).Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(out), "\r\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func queryCentralLogs(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	service, _ := args["service"].(string)
	if service == "" {
		service = "all"
	}
	sourceFilter, _ := args["source"].(string)
	queryStr, _ := args["query"].(string)
	queryLow := strings.ToLower(strings.TrimSpace(queryStr))
	levelFilter, _ := args["level"].(string)
	lines := 200
	if lf, ok := args["lines"].(float64); ok && lf > 0 {
		lines = min(int(lf), 1000)
	}

	var raw []centralLogEntry
	sourceSet := map[string]bool{}
	addEntry := func(svc, src, ts, msg string) {
		sourceSet[src] = true
		lvl := inferLogLevel(msg)
		if service != "all" && svc != service {
			return
		}
		if sourceFilter != "" && src != sourceFilter {
			return
		}
		if levelFilter != "" && levelFilter != "all" && lvl != levelFilter {
			return
		}
		if queryLow != "" && !strings.Contains(strings.ToLower(msg), queryLow) && !strings.Contains(strings.ToLower(src), queryLow) {
			return
		}
		raw = append(raw, centralLogEntry{
			Timestamp: ts,
			Service:   svc,
			Source:    src,
			Level:     lvl,
			Message:   msg,
		})
	}

	if dryRun {
		now := time.Now().UTC()
		samples := []centralLogEntry{
			{Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339), Service: "web", Source: "nginx/access.log", Message: `203.0.113.10 - - "GET /api/v1/health HTTP/1.1" 200 42 "-" "curl/8.5.0"`},
			{Timestamp: now.Add(-4 * time.Minute).Format(time.RFC3339), Service: "web", Source: "nginx/error.log", Message: `[warn] 1244#1244: *18 upstream server temporarily disabled while connecting to upstream`},
			{Timestamp: now.Add(-3 * time.Minute).Format(time.RFC3339), Service: "mail", Source: "postfix/mail.log", Message: `postfix/smtp[2104]: 9A2F140B1: to=<user@example.com>, relay=email-smtp.us-east-1.amazonaws.com[54.243.1.1]:587, status=sent (250 Ok)`},
			{Timestamp: now.Add(-2 * time.Minute).Format(time.RFC3339), Service: "auth", Source: "sshd/auth.log", Message: `sshd[3312]: Failed password for invalid user admin from 198.51.100.44 port 51822 ssh2`},
			{Timestamp: now.Add(-90 * time.Second).Format(time.RFC3339), Service: "container", Source: "docker/stako-app", Message: `INFO server listening on :3000`},
			{Timestamp: now.Add(-30 * time.Second).Format(time.RFC3339), Service: "panel", Source: "stako-core", Message: `level=INFO msg="centralized log query executed"`},
		}
		for _, s := range samples {
			addEntry(s.Service, s.Source, s.Timestamp, s.Message)
		}
	} else {
		collectFiles := func(svc string, paths []string) {
			for _, p := range paths {
				src := filepath.Base(p)
				if strings.Contains(p, "/home/") {
					parts := strings.Split(filepath.ToSlash(p), "/")
					if len(parts) >= 5 {
						src = parts[4] + "/" + filepath.Base(p)
					}
				}
				for _, line := range tailLogLines(p, lines) {
					addEntry(svc, src, "", line)
				}
			}
		}

		if service == "all" || service == "web" {
			webPaths := []string{"/var/log/nginx/access.log", "/var/log/nginx/error.log", "/var/log/apache2/access.log", "/var/log/apache2/error.log"}
			if domLogs, err := filepath.Glob("/home/*/web/*/logs/*.log"); err == nil {
				webPaths = append(webPaths, domLogs...)
			}
			collectFiles("web", webPaths)
		}
		if service == "all" || service == "mail" {
			collectFiles("mail", []string{"/var/log/mail.log", "/var/log/mail.err"})
			if out, err := exec.Command("journalctl", "-u", "postfix", "-u", "dovecot", "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso").Output(); err == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "--") {
						addEntry("mail", "journal/mail", "", line)
					}
				}
			}
		}
		if service == "all" || service == "auth" {
			collectFiles("auth", []string{"/var/log/auth.log", "/var/log/fail2ban.log"})
			if out, err := exec.Command("journalctl", "-u", "ssh", "-u", "sshd", "-u", "fail2ban", "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso").Output(); err == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "--") {
						addEntry("auth", "journal/auth", "", line)
					}
				}
			}
		}
		if service == "all" || service == "container" {
			if out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output(); err == nil {
				names := strings.Fields(string(out))
				if len(names) > 10 {
					names = names[:10]
				}
				per := max(20, lines/max(1, len(names)))
				for _, name := range names {
					if cout, err := exec.Command("docker", "logs", "--tail", strconv.Itoa(per), "--timestamps", name).CombinedOutput(); err == nil {
						for _, line := range strings.Split(strings.TrimSpace(string(cout)), "\n") {
							if strings.TrimSpace(line) != "" {
								addEntry("container", "docker/"+name, "", line)
							}
						}
					}
				}
			}
		}
		if service == "all" || service == "panel" {
			if stakoLogs, err := filepath.Glob("/var/log/stako/*.log"); err == nil {
				collectFiles("panel", stakoLogs)
			}
			if out, err := exec.Command("journalctl", "-u", "stako-core", "-u", "stako-agent", "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso").Output(); err == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "--") {
						addEntry("panel", "journal/stako", "", line)
					}
				}
			}
		}
	}

	if len(raw) > lines {
		raw = raw[len(raw)-lines:]
	}
	sources := make([]string, 0, len(sourceSet))
	for s := range sourceSet {
		sources = append(sources, s)
	}
	logger.Info("centralized logs queried", "service", service, "count", len(raw))
	resp.OK = true
	resp.Data = map[string]any{
		"entries": raw,
		"sources": sources,
		"total":   len(raw),
	}
	return resp
}

func applyLogRotation(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	freq, _ := args["frequency"].(string)
	if freq != "daily" && freq != "weekly" {
		freq = "daily"
	}
	rotateCount := 14
	if rf, ok := args["rotate"].(float64); ok && rf >= 1 && rf <= 365 {
		rotateCount = int(rf)
	}
	maxsize, _ := args["maxsize"].(string)
	if maxsize == "" {
		maxsize = "100M"
	}
	compress := true
	if c, ok := args["compress"].(bool); ok {
		compress = c
	}
	runNow, _ := args["run_now"].(bool)

	compressLine := "nocompress"
	if compress {
		compressLine = "compress\n    delaycompress"
	}
	cfg := fmt.Sprintf(`# Managed by Stako Panel
/home/*/web/*/logs/*.log /var/log/nginx/*.log /var/log/apache2/*.log /var/log/stako/*.log {
    %s
    rotate %d
    maxsize %s
    missingok
    notifempty
    %s
    copytruncate
}
`, freq, rotateCount, maxsize, compressLine)

	const confPath = "/etc/logrotate.d/stako-centralized"
	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] wrote %s (freq=%s rotate=%d maxsize=%s compress=%v run_now=%v)", confPath, freq, rotateCount, maxsize, compress, runNow)
		return resp
	}
	if err := os.WriteFile(confPath, []byte(cfg), 0o644); err != nil {
		resp.Error = fmt.Sprintf("write %s: %v", confPath, err)
		return resp
	}
	if runNow {
		return runExec(resp, false, logger, "logrotate", "-f", confPath)
	}
	logger.Info("applied log rotation policy", "path", confPath)
	resp.OK = true
	resp.Output = "Log rotation policy applied to " + confPath
	return resp
}

func applyLogShipping(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	syslogEnabled, _ := args["syslog_enabled"].(bool)
	syslogHost, _ := args["syslog_host"].(string)
	syslogPort := 514
	if sp, ok := args["syslog_port"].(float64); ok && sp > 0 && sp <= 65535 {
		syslogPort = int(sp)
	}
	syslogProto, _ := args["syslog_protocol"].(string)
	if syslogProto != "udp" && syslogProto != "tcp" && syslogProto != "tls" {
		syslogProto = "tcp"
	}
	syslogFormat, _ := args["syslog_format"].(string)
	if syslogFormat != "rfc5424" && syslogFormat != "rfc3164" {
		syslogFormat = "rfc5424"
	}

	lokiEnabled, _ := args["loki_enabled"].(bool)
	lokiURL, _ := args["loki_url"].(string)
	lokiTenant, _ := args["loki_tenant_id"].(string)
	lokiUser, _ := args["loki_username"].(string)
	lokiPass, _ := args["loki_password"].(string)
	lokiLabels, _ := args["loki_labels"].(string)
	lokiSkipTLS, _ := args["loki_tls_skip_verify"].(bool)

	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] log shipping applied (syslog=%v %s:%d/%s, loki=%v %s)", syslogEnabled, syslogHost, syslogPort, syslogProto, lokiEnabled, lokiURL)
		return resp
	}

	const rsyslogConf = "/etc/rsyslog.d/90-stako-forward.conf"
	if syslogEnabled && syslogHost != "" {
		protoPrefix := "@@"
		if syslogProto == "udp" {
			protoPrefix = "@"
		}
		tmpl := "RSYSLOG_SyslogProtocol23Format"
		if syslogFormat == "rfc3164" {
			tmpl = "RSYSLOG_TraditionalForwardFormat"
		}
		target := net.JoinHostPort(syslogHost, strconv.Itoa(syslogPort))
		content := fmt.Sprintf("# Managed by Stako Panel\n*.* %s%s;%s\n", protoPrefix, target, tmpl)
		if err := os.WriteFile(rsyslogConf, []byte(content), 0o644); err != nil {
			resp.Error = fmt.Sprintf("write %s: %v", rsyslogConf, err)
			return resp
		}
	} else {
		_ = os.Remove(rsyslogConf)
	}
	if _, err := exec.LookPath("rsyslogd"); err == nil {
		_ = exec.Command("systemctl", "reload-or-restart", "rsyslog").Run()
	}

	_ = os.MkdirAll("/etc/stako", 0o750)
	const promtailConf = "/etc/stako/promtail.yml"
	if lokiEnabled && lokiURL != "" {
		var b strings.Builder
		b.WriteString("server:\n  http_listen_port: 9080\n  grpc_listen_port: 0\n")
		b.WriteString("positions:\n  filename: /var/lib/stako/promtail-positions.yaml\n")
		b.WriteString("clients:\n  - url: " + lokiURL + "\n")
		if lokiTenant != "" {
			b.WriteString("    tenant_id: " + lokiTenant + "\n")
		}
		if lokiUser != "" {
			b.WriteString("    basic_auth:\n      username: " + lokiUser + "\n      password: " + lokiPass + "\n")
		}
		if lokiSkipTLS {
			b.WriteString("    tls_config:\n      insecure_skip_verify: true\n")
		}
		extraLabels := ""
		for _, pair := range strings.Split(lokiLabels, ",") {
			kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(kv) == 2 && kv[0] != "" && kv[1] != "" {
				extraLabels += fmt.Sprintf("          %s: %q\n", strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]))
			}
		}
		b.WriteString("scrape_configs:\n")
		jobs := []struct {
			name string
			path string
		}{
			{"web", "/home/*/web/*/logs/*.log"},
			{"nginx", "/var/log/nginx/*.log"},
			{"mail", "/var/log/mail.log"},
			{"auth", "/var/log/auth.log"},
			{"panel", "/var/log/stako/*.log"},
		}
		for _, j := range jobs {
			b.WriteString(fmt.Sprintf("  - job_name: stako-%s\n    static_configs:\n      - targets: [localhost]\n        labels:\n          job: stako-%s\n%s          __path__: %s\n", j.name, j.name, extraLabels, j.path))
		}
		if err := os.WriteFile(promtailConf, []byte(b.String()), 0o600); err != nil {
			resp.Error = fmt.Sprintf("write %s: %v", promtailConf, err)
			return resp
		}
		if _, err := os.Stat("/etc/promtail"); err == nil {
			_ = os.WriteFile("/etc/promtail/config.yml", []byte(b.String()), 0o600)
			_ = exec.Command("systemctl", "reload-or-restart", "promtail").Run()
		}
	} else {
		_ = os.Remove(promtailConf)
	}

	logger.Info("applied log shipping config", "syslog_enabled", syslogEnabled, "loki_enabled", lokiEnabled)
	resp.OK = true
	resp.Output = "Log shipping configuration applied"
	return resp
}

const migrationBundlesDir = "/var/lib/stako/migrations"

func listMigrationBundles(resp agentproto.Response, dryRun bool) agentproto.Response {
	if dryRun {
		resp.OK = true
		resp.Data = map[string]any{
			"bundles": []map[string]any{
				{
					"name":       "stako-demo-20261127.tar.gz",
					"path":       migrationBundlesDir + "/stako-demo-20261127.tar.gz",
					"size_bytes": 1048576,
					"created_at": time.Now().UTC().Format(time.RFC3339),
				},
			},
		}
		return resp
	}
	_ = os.MkdirAll(migrationBundlesDir, 0o750)
	entries, err := os.ReadDir(migrationBundlesDir)
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	var bundles []map[string]any
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		bundles = append(bundles, map[string]any{
			"name":       e.Name(),
			"path":       filepath.Join(migrationBundlesDir, e.Name()),
			"size_bytes": info.Size(),
			"created_at": info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	if bundles == nil {
		bundles = []map[string]any{}
	}
	resp.OK = true
	resp.Data = map[string]any{"bundles": bundles}
	return resp
}

func inspectMigrationArchive(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	srcPanel, _ := args["source_panel"].(string)
	archPath, _ := args["archive_path"].(string)
	if archPath == "" || strings.Contains(archPath, "..") {
		resp.Error = "invalid archive_path"
		return resp
	}

	if dryRun {
		if srcPanel == "" || srcPanel == "auto" {
			srcPanel = "hestiacp"
		}
		resp.OK = true
		resp.Data = map[string]any{
			"source_panel": srcPanel,
			"archive_path": archPath,
			"username":     "migrated_user",
			"domains":      []string{"example.com", "shop.example.com"},
			"databases":    []string{"migrated_user_wp", "migrated_user_shop"},
			"mail_domains": []string{"example.com"},
			"mailboxes":    []string{"admin@example.com", "info@example.com"},
			"cron_jobs":    []string{"0 2 * * * /usr/bin/php /home/migrated_user/web/example.com/public_html/cron.php"},
			"size_bytes":   int64(52428800),
			"notes":        []string{"[dry-run] Archive inspection simulated for " + srcPanel},
		}
		return resp
	}

	info, err := os.Stat(archPath)
	if err != nil {
		resp.Error = fmt.Sprintf("stat archive: %v", err)
		return resp
	}

	tarArgs := []string{"-tf", archPath}
	if strings.HasSuffix(archPath, ".zst") {
		tarArgs = []string{"--zstd", "-tf", archPath}
	}
	out, err := exec.Command("tar", tarArgs...).Output()
	if err != nil {
		resp.Error = fmt.Sprintf("list archive contents: %v", err)
		return resp
	}

	lines := strings.Split(string(out), "\n")
	domSet := map[string]bool{}
	dbSet := map[string]bool{}
	mailDomSet := map[string]bool{}
	mailboxSet := map[string]bool{}
	var username string
	detected := srcPanel

	for _, raw := range lines {
		p := strings.TrimPrefix(strings.TrimSpace(raw), "./")
		if p == "" {
			continue
		}
		if p == "manifest.json" {
			detected = "stako"
		}
		if strings.HasPrefix(p, "cp/") || strings.HasPrefix(p, "homedir/") {
			if detected == "" || detected == "auto" {
				detected = "cpanel"
			}
			if strings.HasPrefix(p, "cp/") {
				name := strings.TrimPrefix(p, "cp/")
				if name != "" && !strings.Contains(name, "/") {
					username = name
				}
			}
			if strings.HasPrefix(p, "mysql/") && strings.HasSuffix(p, ".sql") {
				dbName := strings.TrimSuffix(filepath.Base(p), ".sql")
				dbSet[dbName] = true
			}
			if strings.HasPrefix(p, "va/") && !strings.Contains(strings.TrimPrefix(p, "va/"), "/") {
				d := strings.TrimPrefix(p, "va/")
				if d != "" {
					mailDomSet[d] = true
				}
			}
		}
		// HestiaCP / VestaCP layout: web/<domain>/..., db/<db>.sql, mail/<domain>/<localpart>/
		if strings.HasPrefix(p, "web/") {
			parts := strings.Split(p, "/")
			if len(parts) >= 2 && parts[1] != "" {
				domSet[parts[1]] = true
			}
			if detected == "" || detected == "auto" {
				detected = "hestiacp"
			}
		}
		if strings.HasPrefix(p, "db/") && (strings.HasSuffix(p, ".sql") || strings.HasSuffix(p, ".sql.gz")) {
			base := filepath.Base(p)
			base = strings.TrimSuffix(strings.TrimSuffix(base, ".gz"), ".sql")
			if base != "" {
				dbSet[base] = true
			}
		}
		if strings.HasPrefix(p, "mail/") {
			parts := strings.Split(p, "/")
			if len(parts) >= 2 && parts[1] != "" {
				mailDomSet[parts[1]] = true
			}
			if len(parts) >= 3 && parts[2] != "" && !strings.HasPrefix(parts[2], ".") {
				mailboxSet[parts[2]+"@"+parts[1]] = true
			}
		}
	}

	if detected == "" || detected == "auto" {
		detected = "hestiacp"
	}
	if username == "" {
		base := filepath.Base(archPath)
		username = strings.SplitN(base, ".", 2)[0]
		username = strings.TrimPrefix(username, "cpmove-")
	}

	mapKeys := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		return out
	}

	logger.Info("inspected migration archive", "panel", detected, "path", archPath)
	resp.OK = true
	resp.Data = map[string]any{
		"source_panel": detected,
		"archive_path": archPath,
		"username":     username,
		"domains":      mapKeys(domSet),
		"databases":    mapKeys(dbSet),
		"mail_domains": mapKeys(mailDomSet),
		"mailboxes":    mapKeys(mailboxSet),
		"cron_jobs":    []string{},
		"size_bytes":   info.Size(),
		"notes":        []string{},
	}
	return resp
}

func exportStakoAccount(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	username, _ := args["username"].(string)
	if err := agentproto.ValidateLinuxUsername(username); err != nil {
		resp.Error = err.Error()
		return resp
	}
	manifestJSON, _ := args["manifest_json"].(string)
	bundleName := fmt.Sprintf("stako-%s-%s.tar.gz", username, time.Now().UTC().Format("20060102-150405"))
	bundlePath := filepath.Join(migrationBundlesDir, bundleName)

	if dryRun {
		resp.OK = true
		resp.Output = "[dry-run] exported account " + username + " to " + bundlePath
		resp.Data = map[string]any{"bundle_name": bundleName, "bundle_path": bundlePath}
		return resp
	}

	_ = os.MkdirAll(migrationBundlesDir, 0o750)
	stageDir, err := os.MkdirTemp("", "stako-export-*")
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	defer os.RemoveAll(stageDir)

	if manifestJSON != "" {
		_ = os.WriteFile(filepath.Join(stageDir, "manifest.json"), []byte(manifestJSON), 0o600)
	}

	// Copy /home/<username> if present
	homeDir := filepath.Join("/home", username)
	if _, err := os.Stat(homeDir); err == nil {
		_ = exec.Command("cp", "-a", homeDir, filepath.Join(stageDir, "home")).Run()
	}

	// Dump databases listed in args["databases"]
	if dbsRaw, ok := args["databases"].([]any); ok && len(dbsRaw) > 0 {
		dbStage := filepath.Join(stageDir, "db")
		_ = os.MkdirAll(dbStage, 0o750)
		for _, item := range dbsRaw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			dbName, _ := m["name"].(string)
			engine, _ := m["engine"].(string)
			if err := agentproto.ValidateDBIdent(dbName); err != nil {
				continue
			}
			outPath := filepath.Join(dbStage, dbName+".sql")
			if engine == "postgres" {
				if out, err := exec.Command("sudo", "-u", "postgres", "pg_dump", dbName).Output(); err == nil {
					_ = os.WriteFile(outPath, out, 0o600)
				}
			} else {
				if out, err := exec.Command("mysqldump", "--single-transaction", dbName).Output(); err == nil {
					_ = os.WriteFile(outPath, out, 0o600)
				}
			}
		}
	}

	if out, err := exec.Command("tar", "-czf", bundlePath, "-C", stageDir, ".").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("tar export: %v: %s", err, strings.TrimSpace(string(out)))
		return resp
	}
	logger.Info("exported stako account", "username", username, "bundle", bundlePath)
	resp.OK = true
	resp.Output = "Exported " + username + " to " + bundlePath
	resp.Data = map[string]any{"bundle_name": bundleName, "bundle_path": bundlePath}
	return resp
}

func transferMigrationArchive(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	direction, _ := args["direction"].(string) // "pull" or "push"
	host, _ := args["remote_host"].(string)
	port := 22
	if pf, ok := args["remote_port"].(float64); ok && pf > 0 && pf <= 65535 {
		port = int(pf)
	}
	user, _ := args["remote_user"].(string)
	remotePath, _ := args["remote_path"].(string)
	localPath, _ := args["local_path"].(string)
	if host == "" || user == "" || remotePath == "" {
		resp.Error = "remote_host, remote_user, and remote_path are required"
		return resp
	}
	if localPath == "" {
		localPath = filepath.Join(migrationBundlesDir, fmt.Sprintf("incoming-%s.tar.gz", time.Now().UTC().Format("20060102-150405")))
	}

	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] scp -P %d %s %s@%s:%s -> %s", port, direction, user, host, remotePath, localPath)
		resp.Data = map[string]any{"local_path": localPath}
		return resp
	}

	_ = os.MkdirAll(migrationBundlesDir, 0o750)
	remoteSpec := fmt.Sprintf("%s@%s:%s", user, host, remotePath)
	scpArgs := []string{"-o", "StrictHostKeyChecking=accept-new", "-P", strconv.Itoa(port)}
	if direction == "push" {
		scpArgs = append(scpArgs, localPath, remoteSpec)
	} else {
		scpArgs = append(scpArgs, remoteSpec, localPath)
	}
	out, err := exec.Command("scp", scpArgs...).CombinedOutput()
	if err != nil {
		resp.Error = fmt.Sprintf("scp %s: %v: %s", direction, err, strings.TrimSpace(string(out)))
		return resp
	}
	logger.Info("transferred migration archive", "direction", direction, "host", host, "local", localPath)
	resp.OK = true
	resp.Output = strings.TrimSpace(string(out))
	resp.Data = map[string]any{"local_path": localPath}
	return resp
}

func runPanelImport(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	srcPanel, _ := args["source_panel"].(string)
	archPath, _ := args["archive_path"].(string)
	targetUser, _ := args["target_username"].(string)
	includeFiles := true
	if v, ok := args["include_files"].(bool); ok {
		includeFiles = v
	}
	includeDB := true
	if v, ok := args["include_databases"].(bool); ok {
		includeDB = v
	}
	includeMail := true
	if v, ok := args["include_mail"].(bool); ok {
		includeMail = v
	}

	if archPath == "" || strings.Contains(archPath, "..") {
		resp.Error = "invalid archive_path"
		return resp
	}

	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] imported %s archive %s (user=%s files=%v db=%v mail=%v)", srcPanel, archPath, targetUser, includeFiles, includeDB, includeMail)
		return resp
	}

	if _, err := os.Stat(archPath); err != nil {
		resp.Error = fmt.Sprintf("archive not found: %v", err)
		return resp
	}

	workDir, err := os.MkdirTemp("", "stako-import-*")
	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	defer os.RemoveAll(workDir)

	tarArgs := []string{"-xf", archPath, "-C", workDir}
	if strings.HasSuffix(archPath, ".zst") {
		tarArgs = []string{"--zstd", "-xf", archPath, "-C", workDir}
	}
	if out, err := exec.Command("tar", tarArgs...).CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("extract archive: %v: %s", err, strings.TrimSpace(string(out)))
		return resp
	}

	var notes []string
	if targetUser != "" && agentproto.ValidateLinuxUsername(targetUser) == nil {
		homeDst := filepath.Join("/home", targetUser)
		_ = os.MkdirAll(homeDst, 0o750)
		if includeFiles {
			// Stako bundle: home/ -> /home/<targetUser>
			if _, err := os.Stat(filepath.Join(workDir, "home")); err == nil {
				_ = exec.Command("cp", "-a", filepath.Join(workDir, "home")+"/.", homeDst+"/").Run()
				notes = append(notes, "restored Stako home directory")
			}
			// HestiaCP / VestaCP: web/<domain>/public_html -> /home/<targetUser>/web/<domain>/public_html
			if webEntries, err := os.ReadDir(filepath.Join(workDir, "web")); err == nil {
				for _, de := range webEntries {
					if !de.IsDir() {
						continue
					}
					srcPub := filepath.Join(workDir, "web", de.Name(), "public_html")
					dstPub := filepath.Join(homeDst, "web", de.Name(), "public_html")
					if _, err := os.Stat(srcPub); err == nil {
						_ = os.MkdirAll(dstPub, 0o750)
						_ = exec.Command("cp", "-a", srcPub+"/.", dstPub+"/").Run()
						notes = append(notes, "restored web root for "+de.Name())
					}
				}
			}
			// cPanel: homedir/public_html -> /home/<targetUser>/public_html
			if _, err := os.Stat(filepath.Join(workDir, "homedir")); err == nil {
				_ = exec.Command("cp", "-a", filepath.Join(workDir, "homedir")+"/.", homeDst+"/").Run()
				notes = append(notes, "restored cPanel homedir")
			}
			_ = exec.Command("chown", "-R", targetUser+":"+targetUser, homeDst).Run()
		}
	}

	if includeDB {
		for _, dbDir := range []string{filepath.Join(workDir, "db"), filepath.Join(workDir, "mysql")} {
			if entries, err := os.ReadDir(dbDir); err == nil {
				for _, e := range entries {
					if e.IsDir() || (!strings.HasSuffix(e.Name(), ".sql") && !strings.HasSuffix(e.Name(), ".sql.gz")) {
						continue
					}
					dbName := strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".gz"), ".sql")
					if err := agentproto.ValidateDBIdent(dbName); err != nil {
						continue
					}
					_ = exec.Command("mysql", "-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", dbName)).Run()
					sqlPath := filepath.Join(dbDir, e.Name())
					if strings.HasSuffix(e.Name(), ".gz") {
						_ = exec.Command("sh", "-c", fmt.Sprintf("zcat %q | mysql %q", sqlPath, dbName)).Run()
					} else {
						_ = exec.Command("sh", "-c", fmt.Sprintf("mysql %q < %q", dbName, sqlPath)).Run()
					}
					notes = append(notes, "imported database "+dbName)
				}
			}
		}
	}

	if includeMail {
		if mailEntries, err := os.ReadDir(filepath.Join(workDir, "mail")); err == nil {
			for _, me := range mailEntries {
				if !me.IsDir() {
					continue
				}
				dstMail := filepath.Join("/var/mail/vhosts", me.Name())
				_ = os.MkdirAll(dstMail, 0o750)
				_ = exec.Command("cp", "-a", filepath.Join(workDir, "mail", me.Name())+"/.", dstMail+"/").Run()
				notes = append(notes, "restored mail domain "+me.Name())
			}
		}
	}

	logger.Info("panel import completed", "source_panel", srcPanel, "archive", archPath, "notes", len(notes))
	resp.OK = true
	resp.Output = strings.Join(notes, "; ")
	if resp.Output == "" {
		resp.Output = "Import completed for " + archPath
	}
	return resp
}

func wpScanSites(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	if dryRun {
		var sites []map[string]any
		if domsRaw, ok := args["domains"].([]any); ok {
			for _, item := range domsRaw {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				dom, _ := m["domain"].(string)
				docroot, _ := m["docroot"].(string)
				tpl, _ := m["template"].(string)
				if tpl == "wordpress" || strings.Contains(strings.ToLower(dom), "wp") || strings.Contains(strings.ToLower(dom), "blog") {
					sites = append(sites, map[string]any{
						"domain_id":       m["domain_id"],
						"user_id":         m["user_id"],
						"domain":          dom,
						"path":            docroot,
						"wp_version":      "6.7.1",
						"php_version":     m["php_version"],
						"admin_url":       "https://" + dom + "/wp-admin/",
						"updates_core":    0,
						"updates_plugins": 2,
						"updates_themes":  1,
						"security_score":  75,
						"security_issues": []map[string]any{
							{"id": "wp_file_edit", "severity": "medium", "title": "DISALLOW_FILE_EDIT is not enabled", "fixed": false},
							{"id": "wp_xmlrpc", "severity": "low", "title": "xmlrpc.php is accessible", "fixed": false},
						},
					})
				}
			}
		}
		resp.OK = true
		resp.Data = map[string]any{"sites": sites}
		return resp
	}

	var sites []map[string]any
	hasWPCLI := false
	if _, err := exec.LookPath("wp"); err == nil {
		hasWPCLI = true
	}
	if domsRaw, ok := args["domains"].([]any); ok {
		for _, item := range domsRaw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			dom, _ := m["domain"].(string)
			docroot, _ := m["docroot"].(string)
			username, _ := m["username"].(string)
			if docroot == "" {
				continue
			}
			verFile := filepath.Join(docroot, "wp-includes", "version.php")
			cfgFile := filepath.Join(docroot, "wp-config.php")
			verBytes, err := os.ReadFile(verFile)
			if err != nil {
				continue
			}
			wpVer := ""
			for _, line := range strings.Split(string(verBytes), "\n") {
				if strings.Contains(line, "$wp_version") && strings.Contains(line, "'") {
					parts := strings.Split(line, "'")
					if len(parts) >= 2 {
						wpVer = parts[1]
						break
					}
				}
			}
			updatesCore, updatesPlugins, updatesThemes := 0, 0, 0
			if hasWPCLI && agentproto.ValidateLinuxUsername(username) == nil {
				if out, err := exec.Command("sudo", "-u", username, "wp", "core", "check-update", "--path="+docroot, "--format=count").Output(); err == nil {
					if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
						updatesCore = n
					}
				}
				if out, err := exec.Command("sudo", "-u", username, "wp", "plugin", "list", "--update=available", "--path="+docroot, "--format=count").Output(); err == nil {
					if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
						updatesPlugins = n
					}
				}
				if out, err := exec.Command("sudo", "-u", username, "wp", "theme", "list", "--update=available", "--path="+docroot, "--format=count").Output(); err == nil {
					if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
						updatesThemes = n
					}
				}
			}
			score := 100
			var issues []map[string]any
			if cfgBytes, err := os.ReadFile(cfgFile); err == nil {
				cfgStr := string(cfgBytes)
				if !strings.Contains(cfgStr, "DISALLOW_FILE_EDIT") {
					score -= 15
					issues = append(issues, map[string]any{"id": "wp_file_edit", "severity": "medium", "title": "DISALLOW_FILE_EDIT is not enabled", "fixed": false})
				}
				if strings.Contains(cfgStr, "'WP_DEBUG', true") || strings.Contains(cfgStr, `"WP_DEBUG", true`) {
					score -= 15
					issues = append(issues, map[string]any{"id": "wp_debug", "severity": "high", "title": "WP_DEBUG is enabled in production", "fixed": false})
				}
			}
			if info, err := os.Stat(cfgFile); err == nil && info.Mode().Perm()&0o077 != 0 {
				score -= 20
				issues = append(issues, map[string]any{"id": "wp_config_perms", "severity": "high", "title": "wp-config.php permissions should be 0600", "fixed": false})
			}
			if _, err := os.Stat(filepath.Join(docroot, "xmlrpc.php")); err == nil {
				xmlrpcBlocked := false
				if hb, err := os.ReadFile(filepath.Join(docroot, ".htaccess")); err == nil && strings.Contains(string(hb), "xmlrpc.php") {
					xmlrpcBlocked = true
				}
				if agentproto.ValidateLinuxUsername(username) == nil && dom != "" {
					secConf := fmt.Sprintf("/home/%s/web/%s/conf/nginx/stako-wp-security.conf", username, dom)
					if sb, err := os.ReadFile(secConf); err == nil && strings.Contains(string(sb), "xmlrpc.php") {
						xmlrpcBlocked = true
					}
				}
				if !xmlrpcBlocked {
					score -= 10
					issues = append(issues, map[string]any{"id": "wp_xmlrpc", "severity": "low", "title": "xmlrpc.php exists", "fixed": false})
				}
			}
			if score < 0 {
				score = 0
			}
			if issues == nil {
				issues = []map[string]any{}
			}
			sites = append(sites, map[string]any{
				"domain_id":       m["domain_id"],
				"user_id":         m["user_id"],
				"domain":          dom,
				"path":            docroot,
				"wp_version":      wpVer,
				"php_version":     m["php_version"],
				"admin_url":       "https://" + dom + "/wp-admin/",
				"updates_core":    updatesCore,
				"updates_plugins": updatesPlugins,
				"updates_themes":  updatesThemes,
				"security_score":  score,
				"security_issues": issues,
			})
		}
	}
	logger.Info("scanned wordpress installations", "count", len(sites))
	resp.OK = true
	resp.Data = map[string]any{"sites": sites}
	return resp
}

func wpUpdateSite(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	docroot, _ := args["path"].(string)
	username, _ := args["username"].(string)
	if dryRun || docroot == "" {
		return runExec(resp, true, logger, "wp", "core", "update", "--path="+docroot)
	}
	if _, err := exec.LookPath("wp"); err != nil {
		resp.Error = "WP-CLI (wp) is not installed on server"
		return resp
	}
	if err := agentproto.ValidateLinuxUsername(username); err != nil {
		resp.Error = err.Error()
		return resp
	}
	var outputs []string
	for _, sub := range [][]string{
		{"core", "update"},
		{"plugin", "update", "--all"},
		{"theme", "update", "--all"},
	} {
		cmdArgs := append([]string{"-u", username, "wp"}, append(sub, "--path="+docroot)...)
		out, err := exec.Command("sudo", cmdArgs...).CombinedOutput()
		outputs = append(outputs, strings.TrimSpace(string(out)))
		if err != nil {
			resp.Error = fmt.Sprintf("wp %s: %v: %s", sub[0], err, strings.TrimSpace(string(out)))
			return resp
		}
	}
	resp.OK = true
	resp.Output = strings.Join(outputs, "\n")
	return resp
}

func wpStagingSync(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	srcPath, _ := args["src_path"].(string)
	dstPath, _ := args["dst_path"].(string)
	srcDomain, _ := args["src_domain"].(string)
	dstDomain, _ := args["dst_domain"].(string)
	username, _ := args["username"].(string)
	action, _ := args["action"].(string) // "create" or "push"
	if action == "" {
		action = "create"
	}
	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] wp staging %s: %s (%s) -> %s (%s)", action, srcDomain, srcPath, dstDomain, dstPath)
		return resp
	}
	if err := agentproto.ValidateLinuxUsername(username); err != nil {
		resp.Error = err.Error()
		return resp
	}
	_ = os.MkdirAll(dstPath, 0o750)
	if out, err := exec.Command("cp", "-a", srcPath+"/.", dstPath+"/").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("copy files: %v: %s", err, strings.TrimSpace(string(out)))
		return resp
	}
	ensureDocrootPermissions(username, dstPath)
	if _, err := exec.LookPath("wp"); err == nil && srcDomain != "" && dstDomain != "" {
		_ = exec.Command("sudo", "-u", username, "wp", "search-replace", srcDomain, dstDomain, "--path="+dstPath).Run()
	}
	resp.OK = true
	resp.Output = fmt.Sprintf("WordPress staging %s completed (%s -> %s)", action, srcDomain, dstDomain)
	return resp
}

func wpHardenSite(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	docroot, _ := args["path"].(string)
	username, _ := args["username"].(string)
	domain, _ := args["domain"].(string)
	if dryRun || docroot == "" {
		return runExec(resp, true, logger, "chmod", "0600", filepath.Join(docroot, "wp-config.php"))
	}
	cfgFile := filepath.Join(docroot, "wp-config.php")
	if b, err := os.ReadFile(cfgFile); err == nil {
		content := string(b)
		if !strings.Contains(content, "DISALLOW_FILE_EDIT") {
			needle := "/* That's all, stop editing!"
			add := "define('DISALLOW_FILE_EDIT', true);\n"
			if strings.Contains(content, needle) {
				content = strings.Replace(content, needle, add+needle, 1)
			} else {
				content += "\n" + add
			}
		}
		content = strings.ReplaceAll(content, "'WP_DEBUG', true", "'WP_DEBUG', false")
		content = strings.ReplaceAll(content, `"WP_DEBUG", true`, `"WP_DEBUG", false`)
		_ = os.WriteFile(cfgFile, []byte(content), 0o600)
		_ = os.Chmod(cfgFile, 0o600)
	}
	htaccess := filepath.Join(docroot, ".htaccess")
	existingHT, _ := os.ReadFile(htaccess)
	if !strings.Contains(string(existingHT), "xmlrpc.php") {
		block := "\n# Stako WP Security\n<Files xmlrpc.php>\n  Require all denied\n</Files>\n"
		_ = os.WriteFile(htaccess, append(existingHT, []byte(block)...), 0o644)
	}
	if agentproto.ValidateLinuxUsername(username) == nil && agentproto.ValidateDomain(domain) == nil {
		nginxDir := fmt.Sprintf("/home/%s/web/%s/conf/nginx", username, domain)
		_ = os.MkdirAll(nginxDir, 0o750)
		secConf := filepath.Join(nginxDir, "stako-wp-security.conf")
		_ = os.WriteFile(secConf, []byte("# Managed by Stako WP Security\nlocation = /xmlrpc.php {\n    deny all;\n    return 403;\n}\n"), 0o644)
		_ = exec.Command("systemctl", "reload", "nginx").Run()
	}
	resp.OK = true
	resp.Output = "WordPress security hardening applied"
	return resp
}

func wpAutoLogin(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	docroot, _ := args["path"].(string)
	domain, _ := args["domain"].(string)
	token, _ := args["token"].(string)
	if token == "" {
		var buf [16]byte
		_, _ = rand.Read(buf[:])
		token = hex.EncodeToString(buf[:])
	}
	fileName := "_stako_login_" + token[:12] + ".php"
	loginURL := fmt.Sprintf("https://%s/%s?token=%s", domain, fileName, token)
	if dryRun || docroot == "" {
		resp.OK = true
		resp.Data = map[string]any{"login_url": loginURL, "expires_in": 60}
		return resp
	}
	phpScript := fmt.Sprintf(`<?php
@unlink(__FILE__);
if (!isset($_GET['token']) || !hash_equals(%q, (string)$_GET['token']) || time() > %d) {
    http_response_code(403);
    exit('Expired or invalid token');
}
require_once __DIR__ . '/wp-load.php';
$admins = get_users(['role' => 'administrator', 'number' => 1, 'orderby' => 'ID', 'order' => 'ASC']);
if (empty($admins)) { exit('No administrator found'); }
wp_set_current_user($admins[0]->ID);
wp_set_auth_cookie($admins[0]->ID, false);
wp_safe_redirect(admin_url());
exit;
`, token, time.Now().UTC().Add(60*time.Second).Unix())
	if err := os.WriteFile(filepath.Join(docroot, fileName), []byte(phpScript), 0o644); err != nil {
		resp.Error = err.Error()
		return resp
	}
	resp.OK = true
	resp.Data = map[string]any{"login_url": loginURL, "expires_in": 60}
	return resp
}

func pgConfDirs() []string {
	matches, _ := filepath.Glob("/etc/postgresql/*/main/conf.d")
	return matches
}

func pgMainDirs() []string {
	matches, _ := filepath.Glob("/etc/postgresql/*/main")
	return matches
}

func applyPgBouncer(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	enabled, _ := args["enabled"].(bool)
	listenPort := 6432
	if p, ok := args["listen_port"].(float64); ok && p > 0 && p <= 65535 {
		listenPort = int(p)
	}
	poolMode, _ := args["pool_mode"].(string)
	if poolMode != "transaction" && poolMode != "session" && poolMode != "statement" {
		poolMode = "transaction"
	}
	maxClient := 200
	if m, ok := args["max_client_conn"].(float64); ok && m > 0 {
		maxClient = int(m)
	}
	defaultPool := 20
	if d, ok := args["default_pool_size"].(float64); ok && d > 0 {
		defaultPool = int(d)
	}
	reservePool := 5
	if r, ok := args["reserve_pool_size"].(float64); ok && r >= 0 {
		reservePool = int(r)
	}

	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] pgbouncer enabled=%v port=%d mode=%s max_client=%d pool=%d", enabled, listenPort, poolMode, maxClient, defaultPool)
		return resp
	}

	if !enabled {
		_ = exec.Command("systemctl", "disable", "--now", "pgbouncer").Run()
		resp.OK = true
		resp.Output = "PgBouncer disabled"
		return resp
	}

	_ = os.MkdirAll("/etc/pgbouncer", 0o750)
	ini := fmt.Sprintf(`; Managed by Stako Panel
[databases]
* = host=127.0.0.1 port=5432

[pgbouncer]
listen_addr = 127.0.0.1
listen_port = %d
auth_type = scram-sha-256
auth_file = /etc/pgbouncer/userlist.txt
pool_mode = %s
max_client_conn = %d
default_pool_size = %d
reserve_pool_size = %d
admin_users = postgres
stats_users = postgres
ignore_startup_parameters = extra_float_digits
`, listenPort, poolMode, maxClient, defaultPool, reservePool)
	if err := os.WriteFile("/etc/pgbouncer/pgbouncer.ini", []byte(ini), 0o640); err != nil {
		resp.Error = err.Error()
		return resp
	}
	if _, err := os.Stat("/etc/pgbouncer/userlist.txt"); os.IsNotExist(err) {
		_ = os.WriteFile("/etc/pgbouncer/userlist.txt", []byte(""), 0o640)
	}
	logger.Info("applied pgbouncer config", "port", listenPort, "mode", poolMode)
	return runExec(resp, false, logger, "systemctl", "enable", "--now", "pgbouncer")
}

func applyPGReplication(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	role, _ := args["role"].(string) // "disabled", "primary", "standby"
	walLevel, _ := args["wal_level"].(string)
	if walLevel != "replica" && walLevel != "logical" {
		walLevel = "replica"
	}
	maxSenders := 10
	if ms, ok := args["max_wal_senders"].(float64); ok && ms > 0 {
		maxSenders = int(ms)
	}
	walKeepMB := 512
	if wk, ok := args["wal_keep_size_mb"].(float64); ok && wk >= 0 {
		walKeepMB = int(wk)
	}
	repUser, _ := args["replication_user"].(string)
	if repUser == "" {
		repUser = "replicator"
	}
	repPass, _ := args["replication_pass"].(string)
	primaryHost, _ := args["primary_host"].(string)
	primaryPort := 5432
	if pp, ok := args["primary_port"].(float64); ok && pp > 0 && pp <= 65535 {
		primaryPort = int(pp)
	}

	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] postgresql replication role=%s wal_level=%s max_wal_senders=%d", role, walLevel, maxSenders)
		return resp
	}

	confDirs := pgConfDirs()
	for _, d := range confDirs {
		confFile := filepath.Join(d, "90-stako-replication.conf")
		if role == "disabled" || role == "" {
			_ = os.Remove(confFile)
			continue
		}
		var b strings.Builder
		b.WriteString("# Managed by Stako Panel\n")
		b.WriteString("listen_addresses = '*'\n")
		b.WriteString("wal_level = " + walLevel + "\n")
		b.WriteString(fmt.Sprintf("max_wal_senders = %d\n", maxSenders))
		b.WriteString(fmt.Sprintf("wal_keep_size = %dMB\n", walKeepMB))
		if role == "standby" && primaryHost != "" {
			b.WriteString("hot_standby = on\n")
			b.WriteString(fmt.Sprintf("primary_conninfo = 'host=%s port=%d user=%s password=%s'\n", primaryHost, primaryPort, repUser, repPass))
		}
		if err := os.WriteFile(confFile, []byte(b.String()), 0o644); err != nil {
			resp.Error = err.Error()
			return resp
		}
	}

	if role == "primary" && agentproto.ValidateDBIdent(repUser) == nil && repPass != "" {
		sql := fmt.Sprintf("DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN CREATE ROLE %s WITH REPLICATION LOGIN PASSWORD '%s'; ELSE ALTER ROLE %s WITH REPLICATION LOGIN PASSWORD '%s'; END IF; END $$;",
			repUser, repUser, strings.ReplaceAll(repPass, "'", "''"), repUser, strings.ReplaceAll(repPass, "'", "''"))
		_ = exec.Command("sudo", "-u", "postgres", "psql", "-c", sql).Run()
	}

	if hostsRaw, ok := args["standby_hosts"].([]any); ok && role == "primary" {
		for _, md := range pgMainDirs() {
			hbaPath := filepath.Join(md, "pg_hba.conf")
			if b, err := os.ReadFile(hbaPath); err == nil {
				lines := strings.Split(string(b), "\n")
				var filtered []string
				for _, l := range lines {
					if !strings.Contains(l, "# stako-repl") {
						filtered = append(filtered, l)
					}
				}
				for _, hr := range hostsRaw {
					if h, ok := hr.(string); ok && strings.TrimSpace(h) != "" {
						filtered = append(filtered, fmt.Sprintf("host replication %s %s scram-sha-256 # stako-repl", repUser, strings.TrimSpace(h)))
					}
				}
				_ = os.WriteFile(hbaPath, []byte(strings.Join(filtered, "\n")), 0o640)
			}
		}
	}

	logger.Info("applied postgresql replication config", "role", role)
	return runExec(resp, false, logger, "systemctl", "reload-or-restart", "postgresql")
}

func getPGReplicationStatus(resp agentproto.Response, dryRun bool) agentproto.Response {
	if dryRun {
		resp.OK = true
		resp.Data = map[string]any{
			"pgbouncer_active":  false,
			"in_recovery":       false,
			"replication_peers": []any{},
		}
		return resp
	}
	pgbActive := false
	if out, err := exec.Command("systemctl", "is-active", "pgbouncer").Output(); err == nil && strings.TrimSpace(string(out)) == "active" {
		pgbActive = true
	}
	inRecovery := false
	if out, err := exec.Command("sudo", "-u", "postgres", "psql", "-tAc", "SELECT pg_is_in_recovery();").Output(); err == nil {
		inRecovery = strings.TrimSpace(string(out)) == "t"
	}
	peers := []string{}
	if out, err := exec.Command("sudo", "-u", "postgres", "psql", "-tAc", "SELECT client_addr || ' (' || state || ', lag=' || COALESCE(pg_wal_lsn_diff(sent_lsn, replay_lsn)::text, '0') || 'B)' FROM pg_stat_replication;").Output(); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.TrimSpace(l) != "" {
				peers = append(peers, l)
			}
		}
	}
	resp.OK = true
	resp.Data = map[string]any{
		"pgbouncer_active":  pgbActive,
		"in_recovery":       inRecovery,
		"replication_peers": peers,
	}
	return resp
}

func applyPGPITR(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	action, _ := args["action"].(string) // "config", "basebackup", "restore"
	if action == "" {
		action = "config"
	}
	enabled, _ := args["enabled"].(bool)
	archiveDir, _ := args["archive_dir"].(string)
	if archiveDir == "" {
		archiveDir = "/var/lib/postgresql/wal_archive"
	}
	targetTime, _ := args["target_time"].(string)

	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] postgresql PITR action=%s enabled=%v archive_dir=%s target_time=%s", action, enabled, archiveDir, targetTime)
		return resp
	}

	switch action {
	case "basebackup":
		backupDir := filepath.Join("/var/lib/postgresql/base_backups", time.Now().UTC().Format("20060102-150405"))
		_ = os.MkdirAll(filepath.Dir(backupDir), 0o700)
		return runExec(resp, false, logger, "sudo", "-u", "postgres", "pg_basebackup", "-D", backupDir, "-Ft", "-z", "-X", "fetch")

	case "restore":
		if targetTime == "" {
			resp.Error = "target_time is required for PITR restore"
			return resp
		}
		for _, d := range pgConfDirs() {
			recConf := filepath.Join(d, "92-stako-pitr-restore.conf")
			cfg := fmt.Sprintf("# Managed by Stako Panel\nrestore_command = 'cp %s/%%f %%p'\nrecovery_target_time = '%s'\nrecovery_target_action = 'promote'\n", archiveDir, targetTime)
			_ = os.WriteFile(recConf, []byte(cfg), 0o644)
		}
		if dataDirs, err := filepath.Glob("/var/lib/postgresql/*/main"); err == nil {
			for _, dd := range dataDirs {
				_ = os.WriteFile(filepath.Join(dd, "recovery.signal"), []byte(""), 0o644)
			}
		}
		return runExec(resp, false, logger, "systemctl", "restart", "postgresql")

	default:
		_ = os.MkdirAll(archiveDir, 0o700)
		for _, d := range pgConfDirs() {
			confFile := filepath.Join(d, "91-stako-pitr.conf")
			if !enabled {
				_ = os.Remove(confFile)
				continue
			}
			cfg := fmt.Sprintf("# Managed by Stako Panel\narchive_mode = on\narchive_command = 'test ! -f %s/%%f && cp %%p %s/%%f'\n", archiveDir, archiveDir)
			if err := os.WriteFile(confFile, []byte(cfg), 0o644); err != nil {
				resp.Error = err.Error()
				return resp
			}
		}
		logger.Info("applied postgresql PITR config", "enabled", enabled, "archive_dir", archiveDir)
		return runExec(resp, false, logger, "systemctl", "reload-or-restart", "postgresql")
	}
}

func applyNginxMicrocache(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	username, _ := args["username"].(string)
	domain, _ := args["domain"].(string)
	enabled, _ := args["enabled"].(bool)
	ttl := 10
	if tf, ok := args["ttl_sec"].(float64); ok && tf > 0 && tf <= 3600 {
		ttl = int(tf)
	}
	if err := agentproto.ValidateLinuxUsername(username); err != nil {
		resp.Error = err.Error()
		return resp
	}
	if err := agentproto.ValidateDomain(domain); err != nil {
		resp.Error = err.Error()
		return resp
	}
	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] nginx microcache domain=%s enabled=%v ttl=%ds", domain, enabled, ttl)
		return resp
	}

	const globalCacheConf = "/etc/nginx/conf.d/stako-microcache.conf"
	_ = os.MkdirAll("/var/cache/nginx/stako", 0o750)
	_ = os.MkdirAll("/var/cache/nginx/stako_proxy", 0o750)
	globalCfg := `# Managed by Stako Panel
fastcgi_cache_path /var/cache/nginx/stako levels=1:2 keys_zone=STAKO_MICRO:32m inactive=10m max_size=512m;
proxy_cache_path /var/cache/nginx/stako_proxy levels=1:2 keys_zone=STAKO_PROXY:32m inactive=10m max_size=512m;
`
	_ = os.WriteFile(globalCacheConf, []byte(globalCfg), 0o644)

	confDir := fmt.Sprintf("/home/%s/web/%s/conf/nginx", username, domain)
	_ = os.MkdirAll(confDir, 0o750)
	domConf := filepath.Join(confDir, "stako-microcache.conf")
	if !enabled {
		_ = os.Remove(domConf)
	} else {
		snippet := fmt.Sprintf(`# Managed by Stako Microcache
set $skip_cache 0;
if ($request_method = POST) { set $skip_cache 1; }
if ($query_string != "") { set $skip_cache 1; }
if ($http_cookie ~* "comment_author|wordpress_[a-f0-9]+|wp-postpass|wordpress_logged_in") { set $skip_cache 1; }
fastcgi_cache STAKO_MICRO;
fastcgi_cache_valid 200 %ds;
fastcgi_cache_bypass $skip_cache;
fastcgi_no_cache $skip_cache;
add_header X-Stako-Cache $upstream_cache_status;
`, ttl)
		if err := os.WriteFile(domConf, []byte(snippet), 0o644); err != nil {
			resp.Error = err.Error()
			return resp
		}
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		_ = os.Remove(domConf)
		resp.Error = fmt.Sprintf("nginx -t: %v: %s", err, strings.TrimSpace(string(out)))
		return resp
	}
	logger.Info("applied nginx microcache", "domain", domain, "enabled", enabled, "ttl", ttl)
	return runExec(resp, false, logger, "systemctl", "reload", "nginx")
}

func applyRedisObjectCache(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	username, _ := args["username"].(string)
	domain, _ := args["domain"].(string)
	docroot, _ := args["docroot"].(string)
	enabled, _ := args["enabled"].(bool)
	port := 6379
	if pf, ok := args["redis_port"].(float64); ok && pf > 0 {
		port = int(pf)
	}
	if err := agentproto.ValidateLinuxUsername(username); err != nil {
		resp.Error = err.Error()
		return resp
	}
	if err := agentproto.ValidateDomain(domain); err != nil {
		resp.Error = err.Error()
		return resp
	}
	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] redis object cache domain=%s enabled=%v port=%d", domain, enabled, port)
		return resp
	}

	envPath := fmt.Sprintf("/home/%s/web/%s/conf/redis-cache.env", username, domain)
	if !enabled {
		_ = os.Remove(envPath)
		if _, err := exec.LookPath("wp"); err == nil && docroot != "" {
			if _, err := os.Stat(filepath.Join(docroot, "wp-config.php")); err == nil {
				_ = exec.Command("sudo", "-u", username, "wp", "redis", "disable", "--path="+docroot).Run()
			}
		}
		resp.OK = true
		resp.Output = "Redis object cache disabled for " + domain
		return resp
	}

	_ = os.MkdirAll(filepath.Dir(envPath), 0o750)
	_ = os.WriteFile(envPath, []byte(fmt.Sprintf("REDIS_HOST=127.0.0.1\nREDIS_PORT=%d\n", port)), 0o600)

	if docroot != "" {
		wpCfg := filepath.Join(docroot, "wp-config.php")
		if b, err := os.ReadFile(wpCfg); err == nil {
			content := string(b)
			if !strings.Contains(content, "WP_REDIS_PORT") {
				add := fmt.Sprintf("define('WP_REDIS_HOST', '127.0.0.1');\ndefine('WP_REDIS_PORT', %d);\n", port)
				needle := "/* That's all, stop editing!"
				if strings.Contains(content, needle) {
					content = strings.Replace(content, needle, add+needle, 1)
				} else {
					content += "\n" + add
				}
				_ = os.WriteFile(wpCfg, []byte(content), 0o600)
			}
			if _, err := exec.LookPath("wp"); err == nil {
				_ = exec.Command("sudo", "-u", username, "wp", "plugin", "install", "redis-cache", "--activate", "--path="+docroot).Run()
				_ = exec.Command("sudo", "-u", username, "wp", "redis", "enable", "--path="+docroot).Run()
			}
		}
	}
	logger.Info("applied redis object cache", "domain", domain, "port", port)
	resp.OK = true
	resp.Output = fmt.Sprintf("Redis object cache enabled on 127.0.0.1:%d", port)
	return resp
}

func collectHardwareHealth(resp agentproto.Response, dryRun bool) agentproto.Response {
	if dryRun {
		resp.OK = true
		resp.Data = map[string]any{
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
		}
		return resp
	}

	// 1. SMART disk health
	disks := []map[string]any{}
	if out, err := exec.Command("lsblk", "-dn", "-o", "NAME,TYPE,SIZE,MODEL").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || fields[1] != "disk" {
				continue
			}
			dev := "/dev/" + fields[0]
			size := fields[2]
			model := strings.Join(fields[3:], " ")
			health := "UNKNOWN"
			temp := 0
			poh := 0
			if sout, err := exec.Command("smartctl", "-H", "-A", "-j", dev).Output(); err == nil {
				var sj struct {
					SmartStatus struct {
						Passed bool `json:"passed"`
					} `json:"smart_status"`
					Temperature struct {
						Current int `json:"current"`
					} `json:"temperature"`
					PowerOnTime struct {
						Hours int `json:"hours"`
					} `json:"power_on_time"`
				}
				if json.Unmarshal(sout, &sj) == nil {
					if sj.SmartStatus.Passed {
						health = "PASSED"
					} else {
						health = "FAILED"
					}
					temp = sj.Temperature.Current
					poh = sj.PowerOnTime.Hours
				}
			}
			disks = append(disks, map[string]any{
				"device":         dev,
				"model":          model,
				"size":           size,
				"health_status":  health,
				"temperature_c":  temp,
				"power_on_hours": poh,
			})
		}
	}

	// 2. Filesystem inode usage (df -i -P)
	inodes := []map[string]any{}
	if out, err := exec.Command("df", "-i", "-P").Output(); err == nil {
		for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if i == 0 {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 6 {
				continue
			}
			fs := fields[0]
			if fs == "tmpfs" || fs == "devtmpfs" || strings.HasPrefix(fs, "udev") || strings.Contains(fields[5], "/snap") {
				continue
			}
			tot, _ := strconv.Atoi(fields[1])
			used, _ := strconv.Atoi(fields[2])
			free, _ := strconv.Atoi(fields[3])
			pctStr := strings.TrimSuffix(fields[4], "%")
			pct, _ := strconv.ParseFloat(pctStr, 64)
			inodes = append(inodes, map[string]any{
				"filesystem":   fs,
				"mount":        fields[5],
				"inodes_total": tot,
				"inodes_used":  used,
				"inodes_free":  free,
				"inodes_pct":   pct,
			})
		}
	}

	// 3. Memory, Swap & PSI pressure
	var memTotalKB, memAvailKB, swapTotalKB, swapFreeKB int64
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			v, _ := strconv.ParseInt(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				memTotalKB = v
			case "MemAvailable:":
				memAvailKB = v
			case "SwapTotal:":
				swapTotalKB = v
			case "SwapFree:":
				swapFreeKB = v
			}
		}
	}
	memTotalMB := memTotalKB / 1024
	memUsedMB := (memTotalKB - memAvailKB) / 1024
	memPct := 0.0
	if memTotalKB > 0 {
		memPct = float64(memTotalKB-memAvailKB) * 100.0 / float64(memTotalKB)
	}
	swapTotalMB := swapTotalKB / 1024
	swapUsedMB := (swapTotalKB - swapFreeKB) / 1024
	swapPct := 0.0
	if swapTotalKB > 0 {
		swapPct = float64(swapTotalKB-swapFreeKB) * 100.0 / float64(swapTotalKB)
	}
	parsePSI := func(p string) float64 {
		b, err := os.ReadFile(p)
		if err != nil {
			return 0
		}
		for _, f := range strings.Fields(string(b)) {
			if strings.HasPrefix(f, "avg10=") {
				v, _ := strconv.ParseFloat(strings.TrimPrefix(f, "avg10="), 64)
				return v
			}
		}
		return 0
	}

	resp.OK = true
	resp.Data = map[string]any{
		"disks":  disks,
		"inodes": inodes,
		"pressure": map[string]any{
			"mem_total_mb":  memTotalMB,
			"mem_used_mb":   memUsedMB,
			"mem_pct":       memPct,
			"swap_total_mb": swapTotalMB,
			"swap_used_mb":  swapUsedMB,
			"swap_pct":      swapPct,
			"psi_mem_avg10": parsePSI("/proc/pressure/memory"),
			"psi_io_avg10":  parsePSI("/proc/pressure/io"),
		},
	}
	return resp
}

func applyUserProjectQuota(resp agentproto.Response, dryRun bool, logger *slog.Logger, args map[string]any) agentproto.Response {
	username, _ := args["username"].(string)
	if err := agentproto.ValidateLinuxUsername(username); err != nil {
		resp.Error = err.Error()
		return resp
	}
	projectID := 1000
	if pf, ok := args["project_id"].(float64); ok && pf > 0 {
		projectID = int(pf)
	}
	diskMB := 0
	if df, ok := args["disk_limit_mb"].(float64); ok && df >= 0 {
		diskMB = int(df)
	}
	inodeLimit := 0
	if inf, ok := args["inode_limit"].(float64); ok && inf >= 0 {
		inodeLimit = int(inf)
	}
	if dryRun {
		resp.OK = true
		resp.Output = fmt.Sprintf("[dry-run] applied project quota for %s (proj=%d disk=%dMB inodes=%d)", username, projectID, diskMB, inodeLimit)
		return resp
	}

	homeDir := "/home/" + username
	_ = os.MkdirAll(homeDir, 0o750)
	// Register project in /etc/projects and /etc/projid
	projLine := fmt.Sprintf("%d:%s\n", projectID, homeDir)
	projIDLine := fmt.Sprintf("%s:%d\n", username, projectID)
	upsertLine := func(path, prefix, line string) {
		b, _ := os.ReadFile(path)
		var out []string
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if l != "" && !strings.HasPrefix(l, prefix) {
				out = append(out, l)
			}
		}
		out = append(out, strings.TrimSpace(line))
		_ = os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644)
	}
	upsertLine("/etc/projects", fmt.Sprintf("%d:", projectID), projLine)
	upsertLine("/etc/projid", username+":", projIDLine)

	if _, err := exec.LookPath("xfs_quota"); err == nil {
		_ = exec.Command("xfs_quota", "-x", "-c", fmt.Sprintf("project -s %s", username), "/home").Run()
		_ = exec.Command("xfs_quota", "-x", "-c", fmt.Sprintf("limit -p bsoft=%dm bhard=%dm isoft=%d ihard=%d %s", diskMB, diskMB, inodeLimit, inodeLimit, username), "/home").Run()
	} else if _, err := exec.LookPath("setquota"); err == nil {
		_ = exec.Command("chattr", "-R", "+P", "-p", strconv.Itoa(projectID), homeDir).Run()
		blocks := diskMB * 1024
		_ = exec.Command("setquota", "-P", strconv.Itoa(projectID), strconv.Itoa(blocks), strconv.Itoa(blocks), strconv.Itoa(inodeLimit), strconv.Itoa(inodeLimit), "/").Run()
	}

	logger.Info("applied user project quota", "username", username, "project_id", projectID, "disk_mb", diskMB, "inodes", inodeLimit)
	resp.OK = true
	resp.Output = fmt.Sprintf("Applied project quota for %s (%d MB, %d inodes)", username, diskMB, inodeLimit)
	return resp
}

func reportUserProjectQuotas(resp agentproto.Response, dryRun bool, args map[string]any) agentproto.Response {
	usage := map[string]map[string]int{}
	if usersRaw, ok := args["usernames"].([]any); ok {
		for _, u := range usersRaw {
			uname, _ := u.(string)
			if uname == "" {
				continue
			}
			if dryRun {
				usage[uname] = map[string]int{"disk_used_mb": 42, "inode_used": 1280}
				continue
			}
			homeDir := "/home/" + uname
			diskUsed := 0
			inodeUsed := 0
			if out, err := exec.Command("du", "-sm", homeDir).Output(); err == nil {
				f := strings.Fields(string(out))
				if len(f) > 0 {
					diskUsed, _ = strconv.Atoi(f[0])
				}
			}
			if out, err := exec.Command("sh", "-c", fmt.Sprintf("find %q 2>/dev/null | wc -l", homeDir)).Output(); err == nil {
				inodeUsed, _ = strconv.Atoi(strings.TrimSpace(string(out)))
			}
			usage[uname] = map[string]int{"disk_used_mb": diskUsed, "inode_used": inodeUsed}
		}
	}
	resp.OK = true
	resp.Data = map[string]any{"usage": usage}
	return resp
}
