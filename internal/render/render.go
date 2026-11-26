// SPDX-License-Identifier: MIT
package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

//go:embed vhost_nginx.tmpl
var nginxVhostTmplRaw string

//go:embed vhost_apache.tmpl
var apacheVhostTmplRaw string

var nginxVhostTmpl = template.Must(template.New("nginx_vhost").Parse(nginxVhostTmplRaw))
var apacheVhostTmpl = template.Must(template.New("apache_vhost").Parse(apacheVhostTmplRaw))

type ProxyRuleData struct {
	Path      string
	Upstream  string
	WebSocket bool
	SSE       bool
	GRPC      bool
}

type DomainVhostData struct {
	Username    string
	Domain      string
	Aliases     []string
	Stack       string
	Template    string
	Docroot     string
	PHPVersion  string
	SSLEnabled  bool
	SSLCertPath string
	SSLKeyPath  string
	ForceHTTPS  bool
	HSTS        bool
	ListenIPv4  string
	ListenIPv6  string
	ProxyTarget string
	ProxyRules  []ProxyRuleData
}

func RenderNginxVhost(d DomainVhostData) (string, error) {
	if d.SSLCertPath == "" {
		d.SSLCertPath = fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", d.Domain)
	}
	if d.SSLKeyPath == "" {
		d.SSLKeyPath = fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", d.Domain)
	}
	var buf bytes.Buffer
	if err := nginxVhostTmpl.Execute(&buf, d); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func RenderApacheVhost(d DomainVhostData) (string, error) {
	if d.SSLCertPath == "" {
		d.SSLCertPath = fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", d.Domain)
	}
	if d.SSLKeyPath == "" {
		d.SSLKeyPath = fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", d.Domain)
	}
	var buf bytes.Buffer
	if err := apacheVhostTmpl.Execute(&buf, d); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const systemdAppTmplRaw = `# Managed by Stako. Do not edit manually.
[Unit]
Description=Stako App {{ .Name }} ({{ .Username }})
After=network.target

[Service]
Type=simple
User={{ .Username }}
Group={{ .Username }}
WorkingDirectory={{ .WorkDir }}
Environment=PORT={{ .Port }}
EnvironmentFile=-/etc/stako/apps/{{ .ID }}.env
ExecStart={{ .Command }}
Restart=on-failure
RestartSec=3

NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ReadWritePaths={{ .WorkDir }}

[Install]
WantedBy=multi-user.target
`

var systemdAppTmpl = template.Must(template.New("systemd_app").Parse(systemdAppTmplRaw))

type AppUnitData struct {
	ID       string
	Name     string
	Username string
	WorkDir  string
	Port     int
	Command  string
}

func RenderAppSystemdUnit(d AppUnitData) (string, error) {
	var buf bytes.Buffer
	if err := systemdAppTmpl.Execute(&buf, d); err != nil {
		return "", err
	}
	return buf.String(), nil
}
