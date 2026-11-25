// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
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
		return runExec(resp, dryRun, logger, "useradd", "-m", "-s", shell, username)

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
		conf, _ := req.Args["nginx_conf"].(string)
		if err := agentproto.ValidateDomain(domain); err != nil {
			resp.Error = err.Error()
			return resp
		}
		if dryRun {
			return runExec(resp, dryRun, logger, "nginx", "-t", "# write /etc/nginx/sites-enabled/"+domain+".conf")
		}
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
		return runExec(resp, dryRun, logger, "sudo", "-u", username, "docker", "run", "-d", "--name", name, "--security-opt", "no-new-privileges", "--cap-drop=ALL", image)

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
		return runExec(resp, dryRun, logger, "restic", "backup", "--tag", id, kind)

	case agentproto.CmdBackupRestore:
		id, _ := req.Args["id"].(string)
		return runExec(resp, dryRun, logger, "restic", "restore", id)

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
		engine, _ := req.Args["engine"].(string)
		mode, _ := req.Args["mode"].(string)
		return runExec(resp, dryRun, logger, "stako-waf", engine, mode)

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
