// SPDX-License-Identifier: MIT
package catalog

import "sync"

type AppManifest struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Type        string   `json:"type"` // php | docker
	Category    string   `json:"category"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Image       string   `json:"image,omitempty"`
	RepoURL     string   `json:"repo_url,omitempty"`
	CmdArgs     []string `json:"cmd_args,omitempty"`
	DefaultPort int      `json:"default_port,omitempty"`
}

var Apps = []AppManifest{
	{Slug: "wordpress", Name: "WordPress", Type: "php", Category: "CMS", Version: "6.7", Description: "CMS dan blog populer berbasis PHP + MySQL/MariaDB"},
	{Slug: "laravel", Name: "Laravel", Type: "php", Category: "Framework", Version: "11.x", Description: "Skeleton aplikasi web PHP modern"},
	{Slug: "nextcloud", Name: "Nextcloud", Type: "docker", Category: "Storage", Version: "30", Description: "Cloud penyimpanan file dan kolaborasi privat", Image: "nextcloud:30-apache", DefaultPort: 80},
	{Slug: "n8n", Name: "n8n", Type: "docker", Category: "Automation", Version: "1.70", Description: "Otomasi workflow self-hosted", Image: "n8nio/n8n:latest", DefaultPort: 5678},
	{Slug: "gitea", Name: "Gitea", Type: "docker", Category: "DevTools", Version: "1.22", Description: "Git server ringan self-hosted", Image: "gitea/gitea:1.22", DefaultPort: 3000},
	{Slug: "uptime-kuma", Name: "Uptime Kuma", Type: "docker", Category: "Monitoring", Version: "1.23", Description: "Monitoring uptime dan status page", Image: "louislam/uptime-kuma:1", DefaultPort: 3001},
	{Slug: "ghost", Name: "Ghost", Type: "docker", Category: "CMS", Version: "5", Description: "Platform publikasi dan newsletter modern", Image: "ghost:5-alpine", DefaultPort: 2368},
	{Slug: "grafana", Name: "Grafana", Type: "docker", Category: "Analytics", Version: "11", Description: "Dashboard metrik dan observabilitas", Image: "grafana/grafana:11.3.0", DefaultPort: 3000},
	{Slug: "vaultwarden", Name: "Vaultwarden", Type: "docker", Category: "Security", Version: "1.32", Description: "Password manager kompatibel Bitwarden", Image: "vaultwarden/server:1.32.5", DefaultPort: 80},
	{Slug: "adminer", Name: "Adminer", Type: "php", Category: "Database", Version: "4.8.1", Description: "Manajemen database web ringan"},
	{Slug: "hermes", Name: "Hermes Agent", Type: "docker", Category: "AI Agent", Version: "latest", Description: "Agen AI otonom dari Nous Research dengan gateway persisten", Image: "nousresearch/hermes-agent:latest", CmdArgs: []string{"gateway", "run"}, DefaultPort: 8642},
	{Slug: "muse", Name: "Muse (OpenMuse)", Type: "docker", Category: "AI Agent", Version: "latest", Description: "Personal AI agent dengan browser persisten dan workspace terisolasi", Image: "stako-local/openmuse:latest", RepoURL: "https://github.com/CopilotKit/openmuse", DefaultPort: 3000},
	{Slug: "9router", Name: "9Router", Type: "docker", Category: "AI Router", Version: "latest", Description: "AI API gateway dan router multi-provider untuk model LLM", Image: "decolua/9router:latest", DefaultPort: 20128},
}

var (
	extMu   sync.RWMutex
	extApps []AppManifest
)

func SetExternalApps(apps []AppManifest) {
	extMu.Lock()
	defer extMu.Unlock()
	extApps = append([]AppManifest(nil), apps...)
}

func ListAll() []AppManifest {
	extMu.RLock()
	defer extMu.RUnlock()
	out := make([]AppManifest, 0, len(Apps)+len(extApps))
	out = append(out, Apps...)
	out = append(out, extApps...)
	return out
}

func Find(slug string) (AppManifest, bool) {
	for _, a := range Apps {
		if a.Slug == slug {
			return a, true
		}
	}
	extMu.RLock()
	defer extMu.RUnlock()
	for _, a := range extApps {
		if a.Slug == slug {
			return a, true
		}
	}
	return AppManifest{}, false
}
