// SPDX-License-Identifier: MIT
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/StakoCP/stako/internal/version"
)

func main() {
	url := flag.String("url", envOr("STAKO_URL", "https://127.0.0.1:8083"), "Panel base URL")
	apiKey := flag.String("api-key", os.Getenv("STAKO_API_KEY"), "API key (Bearer stk_...)")
	insecure := flag.Bool("insecure", false, "Skip TLS verification (dev self-signed)")
	actingAs := flag.String("as", "", "Act as subordinate username (impersonation)")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	c := &client{
		base:     strings.TrimRight(*url, "/"),
		key:      *apiKey,
		actingAs: strings.TrimSpace(*actingAs),
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: *insecure},
			},
		},
	}

	switch args[0] {
	case "version":
		fmt.Printf("stako CLI %s\n", version.Version)
	case "doctor":
		runDoctor(c)
	case "health":
		c.print("GET", "/api/v1/health", nil)
	case "system":
		if len(args) >= 2 && args[1] == "info" {
			c.print("GET", "/api/v1/system/info", nil)
		} else {
			usage()
		}
	case "user":
		handleUserCLI(c, args[1:])
	case "package":
		if len(args) >= 2 && args[1] == "list" {
			c.print("GET", "/api/v1/packages", nil)
		} else {
			usage()
		}
	case "job":
		if len(args) >= 2 && args[1] == "list" {
			c.print("GET", "/api/v1/jobs", nil)
		} else if len(args) >= 3 && args[1] == "get" {
			c.print("GET", "/api/v1/jobs/"+args[2], nil)
		} else {
			usage()
		}
	case "audit":
		if len(args) >= 2 && args[1] == "list" {
			c.print("GET", "/api/v1/audit", nil)
		} else if len(args) >= 2 && args[1] == "verify" {
			c.print("GET", "/api/v1/audit/verify", nil)
		} else {
			usage()
		}
	case "ip":
		if len(args) >= 2 && args[1] == "list" {
			c.print("GET", "/api/v1/ips", nil)
		} else if len(args) >= 2 && args[1] == "sync" {
			c.print("POST", "/api/v1/ips/sync", nil)
		} else {
			usage()
		}
	case "site":
		handleSiteCLI(c, args[1:])
	case "app":
		handleAppCLI(c, args[1:])
	case "db":
		handleDBCLI(c, args[1:])
	case "ftp":
		if len(args) >= 2 && args[1] == "list" {
			c.print("GET", "/api/v1/ftp", nil)
		} else {
			usage()
		}
	case "firewall":
		if len(args) >= 2 && args[1] == "rules" {
			c.print("GET", "/api/v1/firewall/rules", nil)
		} else if len(args) >= 2 && args[1] == "bans" {
			c.print("GET", "/api/v1/firewall/bans", nil)
		} else {
			usage()
		}
	case "security":
		if len(args) >= 2 && args[1] == "scan" {
			c.print("POST", "/api/v1/security/scan", nil)
		} else if len(args) >= 2 && args[1] == "findings" {
			c.print("GET", "/api/v1/security/findings", nil)
		} else if len(args) >= 3 && args[1] == "lockdown" {
			enabled := args[2] == "on" || args[2] == "true" || args[2] == "enable"
			c.print("POST", "/api/v1/security/lockdown", map[string]any{"enabled": enabled})
		} else if len(args) >= 2 && args[1] == "lockdown" {
			c.print("GET", "/api/v1/security/lockdown", nil)
		} else {
			usage()
		}
	case "service":
		handleServiceCLI(c, args[1:])
	case "mail":
		handleMailCLI(c, args[1:])
	case "dns":
		handleDNSCLI(c, args[1:])
	case "cron":
		handleCronCLI(c, args[1:])
	case "backup":
		handleBackupCLI(c, args[1:])
	case "updates":
		if len(args) >= 2 && args[1] == "list" {
			c.print("GET", "/api/v1/updates", nil)
		} else {
			usage()
		}
	case "update":
		if len(args) >= 2 && args[1] == "check" {
			c.print("GET", "/api/v1/system/update", nil)
		} else if len(args) >= 2 && args[1] == "apply" {
			c.print("POST", "/api/v1/system/update", map[string]any{"version": "latest"})
		} else {
			usage()
		}
	case "docker":
		if len(args) >= 2 && args[1] == "list" {
			c.print("GET", "/api/v1/docker/containers", nil)
		} else {
			usage()
		}
	case "catalog":
		if len(args) >= 2 && args[1] == "list" {
			c.print("GET", "/api/v1/catalog", nil)
		} else {
			usage()
		}
	default:
		usage()
		os.Exit(2)
	}
}

func handleDBCLI(c *client, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		c.print("GET", "/api/v1/databases", nil)
	case "create":
		fs := flag.NewFlagSet("db create", flag.ExitOnError)
		engine := fs.String("engine", "mariadb", "Database engine (mariadb|postgres)")
		pass := fs.String("password", "", "Database password")
		_ = fs.Parse(args[2:])
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("POST", "/api/v1/databases", map[string]any{
			"name": args[1], "engine": *engine, "db_password": *pass,
		})
	case "delete":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/databases/"+args[1], nil)
	default:
		usage()
		os.Exit(2)
	}
}

func handleSiteCLI(c *client, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		c.print("GET", "/api/v1/domains", nil)
	case "add":
		fs := flag.NewFlagSet("site add", flag.ExitOnError)
		proxy := fs.String("proxy", "", "Proxy target host:port")
		ssl := fs.Bool("ssl", false, "Enable Let's Encrypt SSL")
		stack := fs.String("stack", "nginx", "Web stack")
		_ = fs.Parse(args[2:])
		if len(args) < 2 {
			os.Exit(2)
		}
		tmpl := "php"
		if *proxy != "" {
			tmpl = "proxy"
		}
		sslMode := "none"
		if *ssl {
			sslMode = "letsencrypt"
		}
		c.print("POST", "/api/v1/domains", map[string]any{
			"domain": args[1], "stack": *stack, "template": tmpl,
			"proxy_target": *proxy, "ssl_mode": sslMode,
		})
	case "delete":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/domains/"+args[1], nil)
	default:
		usage()
		os.Exit(2)
	}
}

func handleAppCLI(c *client, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		c.print("GET", "/api/v1/apps", nil)
	case "start", "stop", "restart":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("POST", "/api/v1/apps/"+args[1]+"/"+args[0], nil)
	case "logs":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("GET", "/api/v1/apps/"+args[1]+"/logs", nil)
	default:
		usage()
		os.Exit(2)
	}
}

func handleUserCLI(c *client, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		c.print("GET", "/api/v1/users", nil)
	case "create":
		fs := flag.NewFlagSet("user create", flag.ExitOnError)
		username := fs.String("username", "", "Username")
		email := fs.String("email", "", "Email")
		password := fs.String("password", "", "Password")
		role := fs.String("role", "user", "Role")
		shell := fs.String("shell", "nologin", "Shell")
		_ = fs.Parse(args[1:])
		c.print("POST", "/api/v1/users", map[string]any{
			"username": *username, "email": *email, "password": *password,
			"role": *role, "shell": *shell,
		})
	case "suspend":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("POST", "/api/v1/users/"+args[1]+"/suspend", nil)
	case "unsuspend":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("POST", "/api/v1/users/"+args[1]+"/unsuspend", nil)
	case "delete":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/users/"+args[1], nil)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: stako [--url URL] [--api-key KEY] [--insecure] [--as USER] <command>")
	fmt.Fprintln(os.Stderr, "Commands: version, doctor, health, system info, service, user, package, site, app, ip, db, ftp, mail, dns, cron, backup, firewall, security, updates, update, docker, catalog, job, audit")
}

func handleServiceCLI(c *client, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		c.print("GET", "/api/v1/system/services", nil)
	case "start", "stop", "restart", "enable", "disable":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("POST", "/api/v1/system/services/"+args[1]+"/action", map[string]any{"action": args[0]})
	case "install":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("POST", "/api/v1/system/services/"+args[1]+"/install", nil)
	case "remove", "uninstall":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/system/services/"+args[1], nil)
	default:
		usage()
		os.Exit(2)
	}
}

func handleMailCLI(c *client, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "domains", "list":
		c.print("GET", "/api/v1/mail/domains", nil)
	case "domain-create":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("POST", "/api/v1/mail/domains", map[string]any{"domain": args[1]})
	case "domain-delete":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/mail/domains/"+args[1], nil)
	case "mailboxes":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("GET", "/api/v1/mail/domains/"+args[1]+"/mailboxes", nil)
	case "mailbox-add":
		if len(args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: stako mail mailbox-add <domain_id> <local_part> <password>")
			os.Exit(2)
		}
		c.print("POST", "/api/v1/mail/domains/"+args[1]+"/mailboxes", map[string]any{
			"local_part": args[2], "password": args[3],
		})
	case "mailbox-del":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/mail/mailboxes/"+args[1], nil)
	default:
		usage()
		os.Exit(2)
	}
}

func handleDNSCLI(c *client, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "zones", "list":
		c.print("GET", "/api/v1/dns/zones", nil)
	case "zone-create":
		if len(args) < 2 {
			os.Exit(2)
		}
		prov := "powerdns"
		if len(args) >= 3 {
			prov = args[2]
		}
		c.print("POST", "/api/v1/dns/zones", map[string]any{"domain": args[1], "provider": prov})
	case "zone-delete":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/dns/zones/"+args[1], nil)
	case "records":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("GET", "/api/v1/dns/zones/"+args[1]+"/records", nil)
	case "record-create":
		if len(args) < 5 {
			fmt.Fprintln(os.Stderr, "Usage: stako dns record-create <zone_id> <type> <name> <content>")
			os.Exit(2)
		}
		c.print("POST", "/api/v1/dns/zones/"+args[1]+"/records", map[string]any{
			"type": args[2], "name": args[3], "content": args[4],
		})
	case "record-delete":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/dns/records/"+args[1], nil)
	default:
		usage()
		os.Exit(2)
	}
}

func handleCronCLI(c *client, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		c.print("GET", "/api/v1/cron", nil)
	case "create":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: stako cron create \"<schedule>\" \"<command>\"")
			os.Exit(2)
		}
		c.print("POST", "/api/v1/cron", map[string]any{"schedule": args[1], "command": args[2]})
	case "delete":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/cron/"+args[1], nil)
	default:
		usage()
		os.Exit(2)
	}
}

func handleBackupCLI(c *client, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		c.print("GET", "/api/v1/backups", nil)
	case "create":
		kind := "user_data"
		if len(args) >= 2 {
			kind = args[1]
		}
		c.print("POST", "/api/v1/backups", map[string]any{"kind": kind, "target": "local"})
	case "restore":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("POST", "/api/v1/backups/"+args[1]+"/restore", nil)
	case "delete":
		if len(args) < 2 {
			os.Exit(2)
		}
		c.print("DELETE", "/api/v1/backups/"+args[1], nil)
	case "config":
		if len(args) < 2 {
			usage()
			os.Exit(2)
		}
		switch args[1] {
		case "list":
			c.print("GET", "/api/v1/backups?kind=system_config", nil)
		case "create":
			c.print("POST", "/api/v1/backups", map[string]any{"kind": "system_config", "target": "local"})
		case "download":
			if len(args) < 3 {
				os.Exit(2)
			}
			c.print("GET", "/api/v1/backups/"+args[2]+"/download", nil)
		case "restore":
			if len(args) < 3 {
				os.Exit(2)
			}
			c.print("POST", "/api/v1/backups/"+args[2]+"/restore", nil)
		default:
			usage()
			os.Exit(2)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func runDoctor(c *client) {
	fmt.Println("Stako Doctor Diagnostics")
	fmt.Println("========================")
	resp, err := c.http.Get(c.base + "/api/v1/health")
	if err != nil {
		fmt.Printf("[FAIL] Panel API unreachable at %s: %v\n", c.base, err)
		os.Exit(1)
	}
	_ = resp.Body.Close()
	fmt.Printf("[OK]   Panel API reachable (%s, HTTP %d)\n", c.base, resp.StatusCode)
	if c.key != "" {
		req, _ := http.NewRequest("GET", c.base+"/api/v1/system/info", nil)
		req.Header.Set("Authorization", "Bearer "+c.key)
		if r2, err := c.http.Do(req); err == nil {
			defer r2.Body.Close()
			var info struct {
				OS    string `json:"os"`
				Agent struct {
					Connected bool `json:"connected"`
					DryRun    bool `json:"dry_run"`
				} `json:"agent"`
			}
			if json.NewDecoder(r2.Body).Decode(&info) == nil && r2.StatusCode == 200 {
				fmt.Printf("[OK]   OS: %s\n", info.OS)
				fmt.Printf("[OK]   Agent connected: %v (dry_run: %v)\n", info.Agent.Connected, info.Agent.DryRun)
			}
		}
	} else {
		fmt.Println("[INFO] Pass --api-key to inspect agent, system, and audit chain status")
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type client struct {
	base     string
	key      string
	actingAs string
	http     *http.Client
}

func (c *client) print(method, path string, body any) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if c.actingAs != "" {
		req.Header.Set("X-Stako-As", c.actingAs)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var pretty bytes.Buffer
	if json.Indent(&pretty, raw, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(raw))
	}
	if resp.StatusCode >= 400 {
		os.Exit(1)
	}
}
