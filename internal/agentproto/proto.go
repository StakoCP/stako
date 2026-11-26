// SPDX-License-Identifier: MIT
package agentproto

import (
	"errors"
	"regexp"
)

// Command names allowed on stako-agent (strict allowlist).
const (
	CmdPing            = "ping"
	CmdSystemInfo      = "system.info"
	CmdUserCreate      = "user.create"
	CmdUserDelete      = "user.delete"
	CmdUserSuspend     = "user.suspend"
	CmdUserUnsuspend   = "user.unsuspend"
	CmdUserSetShell    = "user.set_shell"
	CmdUserSetPassword = "user.set_password"
	CmdDomainApply           = "domain.apply"
	CmdDomainDelete          = "domain.delete"
	CmdDomainLogs            = "domain.logs"
	CmdDomainCustomConfRead  = "domain.custom_conf_read"
	CmdDomainCustomConfWrite = "domain.custom_conf_write"
	CmdSSLIssue        = "ssl.issue"
	CmdAppDeploy       = "app.deploy"
	CmdAppAction       = "app.action"
	CmdAppLogs         = "app.logs"
	CmdIPList          = "ip.list"
	CmdDBCreate        = "db.create"
	CmdDBDelete        = "db.delete"
	CmdDBBackup        = "db.backup"
	CmdFTPCreate       = "ftp.create"
	CmdFTPDelete       = "ftp.delete"
	CmdFirewallApply   = "firewall.apply"
	CmdFirewallBans    = "firewall.bans"
	CmdFirewallUnban   = "firewall.unban"
	CmdSecurityScan    = "security.scan"
	CmdUpdatesList     = "updates.list"
	CmdUpdatesApply    = "updates.apply"
	CmdDockerRun       = "docker.run"
	CmdDockerAction    = "docker.action"
	CmdDockerLogs      = "docker.logs"
	CmdDockerCompose   = "docker.compose"
	CmdCatalogInstall  = "catalog.install"
	CmdRedisCreate     = "redis.create"
	CmdRedisDelete     = "redis.delete"
	CmdRabbitCreate    = "rabbitmq.create"
	CmdRabbitDelete    = "rabbitmq.delete"
	CmdMailDomainAdd   = "mail.domain_add"
	CmdMailDomainDel   = "mail.domain_del"
	CmdMailboxAdd      = "mail.mailbox_add"
	CmdMailboxDel      = "mail.mailbox_del"
	CmdBackupRun       = "backup.run"
	CmdBackupRestore   = "backup.restore"
	CmdFileList        = "file.list"
	CmdFileRead        = "file.read"
	CmdFileWrite       = "file.write"
	CmdFileDelete      = "file.delete"
	CmdTerminalExec    = "terminal.exec"
	CmdWAFApply        = "waf.apply"
	CmdGitDeploy       = "git.deploy"
	CmdPanelImport     = "panel.import"
	CmdSysConfigRead   = "sysconfig.read"
	CmdSysConfigApply  = "sysconfig.apply"
	CmdPanelUpdate     = "panel.update"
	CmdSysServiceList  = "sysservice.list"
	CmdSysServiceAct   = "sysservice.action"
	CmdSysServicePkg   = "sysservice.pkg"
)

type Request struct {
	ID      string         `json:"id"`
	Command string         `json:"command"`
	Args    map[string]any `json:"args,omitempty"`
}

type Response struct {
	ID     string         `json:"id"`
	OK     bool           `json:"ok"`
	DryRun bool           `json:"dry_run,omitempty"`
	Error  string         `json:"error,omitempty"`
	Output string         `json:"output,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
}

var (
	usernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	reserved   = map[string]bool{
		"root": true, "daemon": true, "bin": true, "sys": true, "sync": true,
		"games": true, "man": true, "lp": true, "mail": true, "news": true,
		"uucp": true, "proxy": true, "www-data": true, "backup": true,
		"list": true, "irc": true, "gnats": true, "nobody": true,
		"systemd-network": true, "systemd-resolve": true, "syslog": true,
		"messagebus": true, "sshd": true, "postfix": true, "dovecot": true,
		"mysql": true, "postgres": true, "redis": true, "rabbitmq": true,
		"stako": true, "stako-core": true, "stako-agent": true,
	}
	ValidShells = map[string]string{
		"nologin": "/usr/sbin/nologin",
		"sftp":    "/usr/sbin/nologin",
		"rbash":   "/bin/rbash",
		"bash":    "/bin/bash",
	}
	domainRe   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	upstreamRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+:[0-9]{1,5}$`)
	urlPathRe  = regexp.MustCompile(`^/[a-zA-Z0-9._~/@-]*$`)
	dbIdentRe  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)
	portSpecRe = regexp.MustCompile(`^[0-9]{1,5}(-[0-9]{1,5})?$`)
)

func ValidateDBIdent(s string) error {
	if !dbIdentRe.MatchString(s) {
		return errors.New("invalid identifier: must match ^[a-zA-Z_][a-zA-Z0-9_]{0,62}$")
	}
	return nil
}

func ValidatePortSpec(p string) error {
	if !portSpecRe.MatchString(p) {
		return errors.New("invalid port spec")
	}
	return nil
}

func ValidateDomain(d string) error {
	if len(d) > 253 || !domainRe.MatchString(d) {
		return errors.New("invalid domain name")
	}
	return nil
}

func ValidateUpstream(u string) error {
	if !upstreamRe.MatchString(u) {
		return errors.New("invalid upstream: must be host:port")
	}
	return nil
}

func ValidateURLPath(p string) error {
	if !urlPathRe.MatchString(p) {
		return errors.New("invalid path: must start with / and contain no spaces or control characters")
	}
	return nil
}

func ValidateLinuxUsername(u string) error {
	if !usernameRe.MatchString(u) {
		return errors.New("invalid username: must match ^[a-z_][a-z0-9_-]{0,31}$")
	}
	if reserved[u] {
		return errors.New("username is reserved by the system")
	}
	return nil
}

func ValidateShell(s string) (string, error) {
	path, ok := ValidShells[s]
	if !ok {
		return "", errors.New("invalid shell: must be one of nologin, sftp, rbash, bash")
	}
	return path, nil
}
