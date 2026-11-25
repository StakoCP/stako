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
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	c := &client{
		base: strings.TrimRight(*url, "/"),
		key:  *apiKey,
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
		} else {
			usage()
		}
	case "updates":
		if len(args) >= 2 && args[1] == "list" {
			c.print("GET", "/api/v1/updates", nil)
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
	fmt.Fprintln(os.Stderr, "Usage: stako [--url URL] [--api-key KEY] [--insecure] <command>")
	fmt.Fprintln(os.Stderr, "Commands: version, doctor, health, system info, user, package, site, app, ip, db, ftp, firewall, security, updates, docker, catalog, job, audit")
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
	base string
	key  string
	http *http.Client
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
