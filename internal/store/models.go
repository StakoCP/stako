// SPDX-License-Identifier: MIT
package store

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	Status       string     `json:"status"`
	PackageID    *string    `json:"package_id"`
	OwnerID      *string    `json:"owner_id"`
	TOTPEnabled  bool       `json:"totp_enabled"`
	TOTPSecret   string     `json:"-"` // encrypted at rest
	Shell        string     `json:"shell"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLoginAt  *time.Time `json:"last_login_at"`
}

type Package struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	OwnerID      *string   `json:"owner_id"`
	DiskMB       int       `json:"disk_mb"`
	BandwidthMB  int       `json:"bandwidth_mb"`
	Domains      int       `json:"domains"`
	Databases    int       `json:"databases"`
	MailAccounts int       `json:"mail_accounts"`
	FTPAccounts  int       `json:"ftp_accounts"`
	Containers   int       `json:"containers"`
	CPUPercent   int       `json:"cpu_percent"`
	MemoryMB     int       `json:"memory_mb"`
	ShellAccess  bool      `json:"shell_access"`
	PHPVersions  []string  `json:"php_versions"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Session struct {
	ID              string     `json:"id"`
	UserID          string     `json:"-"`
	ActingAsUserID  *string    `json:"-"`
	ImpersonationID *string    `json:"-"`
	TokenHash       string     `json:"-"`
	CSRFToken       string     `json:"-"`
	IP              string     `json:"ip"`
	UserAgent       string     `json:"user_agent"`
	CreatedAt       time.Time  `json:"created_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	ExpiresAt       time.Time  `json:"-"`
	ReauthUntil     *time.Time `json:"-"`
	Current         bool       `json:"current,omitempty"`
}

type ImpersonationSession struct {
	ID        string     `json:"id"`
	ActorID   string     `json:"actor_id"`
	TargetID  string     `json:"target_id"`
	Reason    string     `json:"reason"`
	IP        string     `json:"ip"`
	CreatedAt time.Time  `json:"created_at"`
	EndedAt   *time.Time `json:"ended_at"`
}

type IPAddress struct {
	ID            string    `json:"id"`
	IP            string    `json:"ip"`
	Family        string    `json:"family"`
	Scope         string    `json:"scope"`
	Visibility    string    `json:"visibility"`
	InterfaceName string    `json:"interface_name"`
	OwnerID       *string   `json:"owner_id"`
	CreatedAt     time.Time `json:"created_at"`
}

type Domain struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Domain      string    `json:"domain"`
	Aliases     []string  `json:"aliases"`
	Stack       string    `json:"stack"`
	Template    string    `json:"template"`
	Docroot     string    `json:"docroot"`
	PHPVersion  string    `json:"php_version"`
	SSLEnabled  bool      `json:"ssl_enabled"`
	SSLMode     string    `json:"ssl_mode"`
	ForceHTTPS  bool      `json:"force_https"`
	HSTS        bool      `json:"hsts"`
	IPv4ID      *string   `json:"ipv4_id"`
	IPv6ID      *string   `json:"ipv6_id"`
	ProxyTarget string    `json:"proxy_target"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProxyRule struct {
	ID        string    `json:"id"`
	DomainID  string    `json:"domain_id"`
	Path      string    `json:"path"`
	Upstream  string    `json:"upstream"`
	WebSocket bool      `json:"websocket"`
	SSE       bool      `json:"sse"`
	GRPC      bool      `json:"grpc"`
	CreatedAt time.Time `json:"created_at"`
}

type App struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	DomainID       *string   `json:"domain_id"`
	Name           string    `json:"name"`
	Runtime        string    `json:"runtime"`
	RuntimeVersion string    `json:"runtime_version"`
	WorkDir        string    `json:"work_dir"`
	Command        string    `json:"command"`
	Port           int       `json:"port"`
	EnvKeys        []string  `json:"env_keys"`
	EnvEnc         string    `json:"-"`
	Status         string    `json:"status"`
	UnitName       string    `json:"unit_name"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Database struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Engine      string    `json:"engine"`
	DBUser      string    `json:"db_user"`
	RemoteHosts []string  `json:"remote_hosts"`
	SizeMB      int       `json:"size_mb"`
	CreatedAt   time.Time `json:"created_at"`
}

type FtpAccount struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	HomeDir   string    `json:"home_dir"`
	QuotaMB   int       `json:"quota_mb"`
	Protocol  string    `json:"protocol"`
	CreatedAt time.Time `json:"created_at"`
}

type FirewallRule struct {
	ID         string    `json:"id"`
	Action     string    `json:"action"`
	Protocol   string    `json:"protocol"`
	Port       string    `json:"port"`
	SourceCIDR string    `json:"source_cidr"`
	Comment    string    `json:"comment"`
	CreatedAt  time.Time `json:"created_at"`
}

type SecurityFinding struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"`
	Severity    string    `json:"severity"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Remediation string    `json:"remediation"`
	Status      string    `json:"status"`
	Component   string    `json:"component"`
	DetectedAt  time.Time `json:"detected_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Container struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	DomainID   *string   `json:"domain_id"`
	Name       string    `json:"name"`
	Image      string    `json:"image"`
	RepoURL    string    `json:"repo_url,omitempty"`
	RepoBranch string    `json:"repo_branch,omitempty"`
	Dockerfile string    `json:"dockerfile,omitempty"`
	Ports      []string  `json:"ports"`
	Volumes    []string  `json:"volumes"`
	EnvEnc     string    `json:"-"`
	CPULimit   string    `json:"cpu_limit"`
	MemoryMB   int       `json:"memory_mb"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type ComposeStack struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	ComposeYAML string    `json:"compose_yaml"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type AppInstall struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	DomainID  string    `json:"domain_id"`
	AppSlug   string    `json:"app_slug"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type RedisInstance struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Name       string    `json:"name"`
	Port       int       `json:"port"`
	MemoryMB   int       `json:"memory_mb"`
	Persistent bool      `json:"persistent"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type RabbitVhost struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Vhost     string    `json:"vhost"`
	Username  string    `json:"username"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type MailDomain struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	Domain        string    `json:"domain"`
	DKIMSelector  string    `json:"dkim_selector"`
	DKIMPublicKey string    `json:"dkim_public_key"`
	RelayProvider string    `json:"relay_provider,omitempty"`
	RelayHost     string    `json:"relay_host,omitempty"`
	RelayPort     int       `json:"relay_port,omitempty"`
	RelayUser     string    `json:"relay_user,omitempty"`
	RelayPassEnc  string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
}

type Mailbox struct {
	ID           string    `json:"id"`
	MailDomainID string    `json:"mail_domain_id"`
	Email        string    `json:"email"`
	QuotaMB      int       `json:"quota_mb"`
	CreatedAt    time.Time `json:"created_at"`
}

type Backup struct {
	ID        string    `json:"id"`
	UserID    *string   `json:"user_id"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target"`
	SizeBytes int64     `json:"size_bytes"`
	Checksum  string    `json:"checksum"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type Webhook struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	SecretEnc string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

type DNSZone struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Domain    string    `json:"domain"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
}

type DNSRecord struct {
	ID       string `json:"id"`
	ZoneID   string `json:"zone_id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
}

type CronJob struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Schedule  string    `json:"schedule"`
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"created_at"`
}

type AlertRule struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Channel   string    `json:"channel"`
	Target    string    `json:"target"`
	Threshold string    `json:"threshold"`
	CreatedAt time.Time `json:"created_at"`
}

type GitDeploy struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	DomainID       string     `json:"domain_id"`
	RepoURL        string     `json:"repo_url"`
	Branch         string     `json:"branch"`
	BuildCmd       string     `json:"build_cmd"`
	LastDeployedAt *time.Time `json:"last_deployed_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

type ServerNode struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type ApiKey struct {
	ID         string     `json:"id"`
	UserID     string     `json:"-"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	KeyHash    string     `json:"-"`
	Scopes     []string   `json:"scopes"`
	AllowedIPs []string   `json:"allowed_ips"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type AuditEntry struct {
	ID            int64           `json:"id"`
	At            time.Time       `json:"at"`
	ActorID       string          `json:"actor_id"`
	ActorUsername string          `json:"actor_username"`
	ActingAs      *string         `json:"acting_as"`
	Action        string          `json:"action"`
	ObjectType    string          `json:"object_type"`
	ObjectID      string          `json:"object_id"`
	Result        string          `json:"result"`
	IP            string          `json:"ip"`
	UserAgent     string          `json:"user_agent"`
	Detail        json.RawMessage `json:"detail"`
	PrevHash      string          `json:"-"`
	Hash          string          `json:"hash"`
}

type Job struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Status     string          `json:"status"`
	CreatedBy  string          `json:"created_by"`
	Payload    json.RawMessage `json:"payload"`
	Log        string          `json:"log"`
	Error      *string         `json:"error"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
}
