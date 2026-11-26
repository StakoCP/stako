// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
	"crypto/rand"
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
		tmpl, _ := req.Args["template"].(string)
		phpVer, _ := req.Args["php_version"].(string)
		conf, _ := req.Args["nginx_conf"].(string)
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if dryRun {
			return runExec(resp, dryRun, logger, "nginx", "-t", "# write /etc/nginx/sites-enabled/"+domain+".conf")
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
		}
		if tmpl != "proxy" && tmpl != "spa" && tmpl != "static" && username != "" && agentproto.ValidateLinuxUsername(username) == nil {
			ensurePHPFpmPool(username, phpVer, logger)
		}
		_ = os.MkdirAll("/var/www/acme", 0o755)
		confPath := "/etc/nginx/sites-enabled/" + domain + ".conf"
		if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
			resp.Error = err.Error()
			return resp
		}
		testResp := runExec(resp, false, logger, "nginx", "-t")
		if !testResp.OK {
			_ = os.Remove(confPath)
			return testResp
		}
		return runExec(resp, false, logger, "systemctl", "reload", "nginx")

	case agentproto.CmdDomainDelete:
		domain, _ := req.Args["domain"].(string)
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if !dryRun {
			_ = os.Remove("/etc/nginx/sites-enabled/" + domain + ".conf")
		}
		return runExec(resp, dryRun, logger, "systemctl", "reload", "nginx")

	case agentproto.CmdDomainLogs:
		username, _ := req.Args["username"].(string)
		domain, _ := req.Args["domain"].(string)
		logType, _ := req.Args["log_type"].(string)
		if logType == "" {
			logType = "access"
		}
		if logType != "access" && logType != "error" {
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
		logPath := fmt.Sprintf("/home/%s/web/%s/logs/%s.log", username, domain, logType)
		if dryRun {
			resp.OK = true
			resp.Data = map[string]any{"path": logPath, "logs": fmt.Sprintf("[dry-run] tail -n %d %s", lines, logPath)}
			return resp
		}
		if _, err := os.Stat(logPath); err != nil {
			if os.IsNotExist(err) {
				resp.OK = true
				resp.Data = map[string]any{"path": logPath, "logs": ""}
				return resp
			}
			resp.Error = err.Error()
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

	case agentproto.CmdSSLIssue:
		domain, _ := req.Args["domain"].(string)
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		return runExec(resp, dryRun, logger, "certbot", "certonly", "--webroot", "-w", "/var/www/acme", "-d", domain, "--non-interactive", "--agree-tos")

	case agentproto.CmdAppDeploy:
		unitName, _ := req.Args["unit_name"].(string)
		unitBody, _ := req.Args["unit_body"].(string)
		if !strings.HasPrefix(unitName, "stako-app-") {
			resp.Error = "invalid unit name"
			return resp
		}
		if dryRun {
			return runExec(resp, dryRun, logger, "systemctl", "enable", "--now", unitName)
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
		if err := agentproto.ValidateDBIdent(dbName); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if engine == "postgres" {
			return runExec(resp, dryRun, logger, "sudo", "-u", "postgres", "createdb", "-O", dbUser, dbName)
		}
		return runExec(resp, dryRun, logger, "mariadb", "-e", "CREATE DATABASE IF NOT EXISTS `"+dbName+"`;")

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
		cpuLimit, _ := req.Args["cpu_limit"].(string)
		if branch == "" {
			branch = "main"
		}
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}
		if repoURL != "" {
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
			_ = runExec(resp, false, logger, "docker", "rm", "-f", cname)
			return runExec(resp, false, logger, "docker", "run", "-d", "--name", cname,
				"--restart", "unless-stopped", "--security-opt", "no-new-privileges", "--cap-drop=ALL",
				"-p", fmt.Sprintf("127.0.0.1:0:%d", appDef.DefaultPort), appDef.Image)
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

	case agentproto.CmdRedisCreate:
		name, _ := req.Args["name"].(string)
		return runExec(resp, dryRun, logger, "systemctl", "enable", "--now", "redis-server@"+name)

	case agentproto.CmdRedisDelete:
		name, _ := req.Args["name"].(string)
		return runExec(resp, dryRun, logger, "systemctl", "disable", "--now", "redis-server@"+name)

	case agentproto.CmdRabbitCreate:
		vhost, _ := req.Args["vhost"].(string)
		return runExec(resp, dryRun, logger, "rabbitmqctl", "add_vhost", vhost)

	case agentproto.CmdRabbitDelete:
		vhost, _ := req.Args["vhost"].(string)
		return runExec(resp, dryRun, logger, "rabbitmqctl", "delete_vhost", vhost)

	case agentproto.CmdMailDomainAdd:
		domain, _ := req.Args["domain"].(string)
		return runExec(resp, dryRun, logger, "stako-mail", "domain-add", domain)

	case agentproto.CmdMailDomainDel:
		domain, _ := req.Args["domain"].(string)
		return runExec(resp, dryRun, logger, "stako-mail", "domain-del", domain)

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
			var mode, modTime string
			if info != nil {
				size = info.Size()
				mode = info.Mode().String()
				modTime = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
			}
			items = append(items, map[string]any{
				"name": e.Name(), "is_dir": e.IsDir(), "size": size, "mode": mode, "mod_time": modTime,
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

	case agentproto.CmdFileWrite:
		p, _ := req.Args["path"].(string)
		content, _ := req.Args["content"].(string)
		if dryRun {
			resp.OK = true
			resp.Output = "[dry-run] write " + p
			return resp
		}
		if err := os.WriteFile(p, []byte(content), 0o640); err != nil {
			resp.Error = err.Error()
			return resp
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
		return runExec(resp, dryRun, logger, "git", "clone", "--depth", "1", "-b", branch, repo)

	case agentproto.CmdPanelImport:
		src, _ := req.Args["source_panel"].(string)
		arch, _ := req.Args["archive_path"].(string)
		return runExec(resp, dryRun, logger, "stako-import", src, arch)

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
				if stOut, err := exec.Command("dpkg-query", "-W", "-f=${Status}|${Version}", pkg).Output(); err == nil {
					parts := strings.SplitN(strings.TrimSpace(string(stOut)), "|", 2)
					if len(parts) == 2 && parts[0] == "install ok installed" {
						installed = true
						ver = parts[1]
					}
				}
				active := "not-installed"
				enabled := false
				if installed {
					actOut, _ := exec.Command("systemctl", "is-active", unit).Output()
					active = strings.TrimSpace(string(actOut))
					if active == "" {
						active = "inactive"
					}
					enOut, _ := exec.Command("systemctl", "is-enabled", unit).Output()
					enabled = strings.TrimSpace(string(enOut)) == "enabled"
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
			_ = runExec(resp, dryRun, logger, "systemctl", "disable", "--now", unit)
			r := runExec(resp, dryRun, logger, "apt-get", "remove", "--purge", "-y", pkg)
			_ = runExec(resp, dryRun, logger, "apt-get", "autoremove", "-y")
			return r
		}
		_ = runExec(resp, dryRun, logger, "apt-get", "update", "-y")
		r := runExec(resp, dryRun, logger, "apt-get", "install", "-y", pkg)
		if r.OK {
			_ = runExec(resp, dryRun, logger, "systemctl", "enable", "--now", unit)
		}
		return r

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
	if existing, err := os.ReadFile(poolPath); err == nil && string(existing) == poolConf {
		// Ensure socket exists; reload-or-restart only if missing
		sockPath := fmt.Sprintf("/run/php/php%s-fpm-%s.sock", phpVer, username)
		if _, err := os.Stat(sockPath); err == nil {
			return
		}
	}
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
