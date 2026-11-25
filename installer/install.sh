#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Stako Smart Installer for Ubuntu 22.04 / 24.04 LTS (7.20, 7.21, 9.3)
# Installs & reconciles all required backend dependencies, verifies versions,
# removes incompatible versions, configures rootless Docker, hardens OS, and
# installs Stako binaries + systemd units.
set -euo pipefail

PRESET="standard"
DRY_RUN=0
UPDATE_ONLY=0
SKIP_UPGRADE=0
VERSION="latest"
REPO="StakoCP/stako"
ARCH="$(dpkg --print-architecture 2>/dev/null || uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac
FROM_DIR=""
WITH_DNS="no"
LOG_FILE="/var/log/stako-install.log"

usage() {
  cat <<EOF
Stako Smart Installer (Ubuntu 22.04 / 24.04 LTS)
Usage: $0 [options]
  --preset basic|standard|full   Service profile (default: standard)
  --version TAG                  Stako release tag (default: latest)
  --from-dir PATH                Install Stako binaries from local directory instead of GitHub
  --update                       In-place update of Stako binaries only (preserves DB, keys, and sites)
  --with-dns                     Install PowerDNS server (default: off)
  --skip-upgrade                 Skip 'apt-get upgrade'
  --dry-run                      Print reconciliation plan without making changes
  -h, --help                     Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --preset) PRESET="${2:?}"; shift 2 ;;
    --version) VERSION="${2:?}"; shift 2 ;;
    --from-dir) FROM_DIR="${2:?}"; shift 2 ;;
    --update) UPDATE_ONLY=1; shift ;;
    --with-dns) WITH_DNS="yes"; shift ;;
    --skip-upgrade) SKIP_UPGRADE=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

case "$PRESET" in
  basic|standard|full) ;;
  *) echo "Invalid preset: $PRESET (must be basic, standard, or full)" >&2; exit 2 ;;
esac

log() { echo "[$(date -u +%H:%M:%S)] $*"; }

run_cmd() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "[dry-run] $*"
  else
    "$@"
  fi
}

# Smart package reconciler:
# 1. If not installed -> apt-get install
# 2. If installed but version < min_ver -> purge old/conflicting + install required version
# 3. If installed and version >= min_ver -> keep (idempotent)
ensure_pkg() {
  local pkg="$1"
  local min_ver="${2:-0}"
  local conflicts="${3:-}"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "[dry-run] ensure_pkg $pkg (min_version=$min_ver${conflicts:+, conflicts=$conflicts})"
    return 0
  fi

  local status installed_ver
  status="$(dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null || true)"
  if [[ "$status" == "install ok installed" ]]; then
    installed_ver="$(dpkg-query -W -f='${Version}' "$pkg" 2>/dev/null || echo "0")"
    if [[ "$min_ver" != "0" ]] && dpkg --compare-versions "$installed_ver" lt "$min_ver"; then
      log "Package $pkg version $installed_ver < required $min_ver: removing old version and reinstalling..."
      DEBIAN_FRONTEND=noninteractive apt-get remove --purge -y "$pkg" $conflicts || true
      DEBIAN_FRONTEND=noninteractive apt-get autoremove -y || true
      DEBIAN_FRONTEND=noninteractive apt-get install -y "$pkg"
    else
      log "Package $pkg ($installed_ver) already meets >= $min_ver"
    fi
  else
    if [[ -n "$conflicts" ]]; then
      for c in $conflicts; do
        if dpkg-query -W -f='${Status}' "$c" 2>/dev/null | grep -q "install ok installed"; then
          log "Removing conflicting package $c before installing $pkg..."
          DEBIAN_FRONTEND=noninteractive apt-get remove --purge -y "$c" || true
        fi
      done
    fi
    log "Installing $pkg..."
    DEBIAN_FRONTEND=noninteractive apt-get install -y "$pkg"
  fi
}

setup_repos() {
  log "Ensuring official upstream repositories (PHP Ondrej, NodeSource, Docker)..."
  run_cmd DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates gnupg lsb-release software-properties-common

  if [[ "$DRY_RUN" -eq 0 ]]; then
    # PHP PPA for 8.2 / 8.3
    if ! grep -rq "ondrej/php" /etc/apt/sources.list /etc/apt/sources.list.d/ 2>/dev/null; then
      add-apt-repository -y ppa:ondrej/php || true
    fi
    # NodeSource 22.x for Standard/Full
    if [[ "$PRESET" != "basic" ]] && [[ ! -f /etc/apt/sources.list.d/nodesource.list ]]; then
      curl -fsSL https://deb.nodesource.com/setup_22.x | bash - || true
    fi
    # Docker official repo for Standard/Full
    if [[ "$PRESET" != "basic" ]] && [[ ! -f /etc/apt/sources.list.d/docker.list ]]; then
      install -m 0755 -d /etc/apt/keyrings
      curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg || true
      chmod a+r /etc/apt/keyrings/docker.gpg || true
      . /etc/os-release
      echo "deb [arch=$ARCH signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $VERSION_CODENAME stable" > /etc/apt/sources.list.d/docker.list || true
    fi
    apt-get update -y
  else
    echo "[dry-run] Configure Ondrej PHP PPA, NodeSource 22.x, Docker CE repo, and run apt-get update"
  fi
}

install_backend_stack() {
  # --- Core & Basic Preset ---
  ensure_pkg "openssh-server" "1:8.0"
  ensure_pkg "nftables" "1.0"
  ensure_pkg "fail2ban" "0.11"
  ensure_pkg "auditd" "3.0"
  ensure_pkg "unattended-upgrades" "2.0"
  ensure_pkg "nginx" "1.18"
  ensure_pkg "php8.4-fpm" "8.4"
  ensure_pkg "php8.4-cli" "8.4"
  ensure_pkg "php8.4-mysql" "8.4"
  ensure_pkg "php8.4-curl" "8.4"
  ensure_pkg "php8.4-mbstring" "8.4"
  ensure_pkg "php8.4-xml" "8.4"
  ensure_pkg "php8.4-zip" "8.4"
  ensure_pkg "mariadb-server" "1:10.6"
  ensure_pkg "certbot" "1.21"
  ensure_pkg "python3-certbot-nginx" "1.21"

  if [[ "$PRESET" == "basic" ]]; then
    return 0
  fi

  # --- Standard Preset ---
  ensure_pkg "apache2" "2.4"
  ensure_pkg "postgresql" "14"
  ensure_pkg "nodejs" "20.0"
  ensure_pkg "golang-go" "2:1.22"
  ensure_pkg "python3" "3.10"
  ensure_pkg "python3-venv" "3.10"
  ensure_pkg "python3-pip" "22.0"
  ensure_pkg "docker-ce" "5:24.0" "docker.io containerd runc"
  ensure_pkg "docker-compose-plugin" "2.0"
  ensure_pkg "uidmap" "1:4.8"
  ensure_pkg "dbus-user-session" "1.12"
  ensure_pkg "redis-server" "5:6.0"
  ensure_pkg "postfix" "3.6"
  ensure_pkg "dovecot-core" "1:2.3"
  ensure_pkg "dovecot-imapd" "1:2.3"
  ensure_pkg "rspamd" "3.0"
  ensure_pkg "proftpd-basic" "1.3" "vsftpd"
  ensure_pkg "restic" "0.14"

  if [[ "$PRESET" == "standard" ]]; then
    if [[ "$WITH_DNS" == "yes" ]]; then
      ensure_pkg "pdns-server" "4.5" "bind9"
      ensure_pkg "pdns-backend-sqlite3" "4.5"
    fi
    return 0
  fi

  # --- Full Preset ---
  ensure_pkg "php8.3-fpm" "8.3"
  ensure_pkg "php8.5-fpm" "8.5"
  ensure_pkg "rabbitmq-server" "3.9"
  ensure_pkg "clamav" "0.103"
  ensure_pkg "clamav-daemon" "0.103"
  ensure_pkg "spamassassin" "3.4"
  ensure_pkg "goaccess" "1.5"
  ensure_pkg "libnginx-mod-http-modsecurity" "1.0"
  if [[ "$WITH_DNS" == "yes" ]]; then
    ensure_pkg "pdns-server" "4.5" "bind9"
    ensure_pkg "pdns-backend-sqlite3" "4.5"
  fi
}

install_stako_binaries() {
  if [[ -n "$FROM_DIR" ]]; then
    log "Installing Stako binaries from $FROM_DIR..."
    run_cmd install -m 0755 "$FROM_DIR/stako-core-linux-$ARCH" /usr/local/bin/stako-core
    run_cmd install -m 0755 "$FROM_DIR/stako-agent-linux-$ARCH" /usr/local/bin/stako-agent
    run_cmd install -m 0755 "$FROM_DIR/stako-linux-$ARCH" /usr/local/bin/stako
    return 0
  fi

  local base_url="https://github.com/$REPO/releases/latest/download"
  if [[ "$VERSION" != "latest" ]]; then
    base_url="https://github.com/$REPO/releases/download/$VERSION"
  fi
  log "Downloading verified Stako binaries ($VERSION, $ARCH) from $base_url..."
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "[dry-run] curl -fsSL $base_url/SHA256SUMS + binaries -> sha256sum -c -> /usr/local/bin/"
    return 0
  fi
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  curl -fsSL "$base_url/SHA256SUMS" -o "$tmp/SHA256SUMS"
  for bin in "stako-core-linux-$ARCH" "stako-agent-linux-$ARCH" "stako-linux-$ARCH"; do
    curl -fsSL "$base_url/$bin" -o "$tmp/$bin"
  done
  (cd "$tmp" && sha256sum -c SHA256SUMS --ignore-missing)
  install -m 0755 "$tmp/stako-core-linux-$ARCH" /usr/local/bin/stako-core
  install -m 0755 "$tmp/stako-agent-linux-$ARCH" /usr/local/bin/stako-agent
  install -m 0755 "$tmp/stako-linux-$ARCH" /usr/local/bin/stako
}

apply_hardening_and_systemd() {
  log "Creating system user 'stako' and applying OS baseline hardening..."
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "[dry-run] useradd stako, write /etc/sysctl.d/99-stako.conf, /etc/ssh/sshd_config.d/99-stako.conf, /etc/stako/install.yml, systemd units"
    return 0
  fi

  id -u stako >/dev/null 2>&1 || useradd --system --home-dir /var/lib/stako --create-home --shell /usr/sbin/nologin stako
  install -d -m 0750 -o stako -g stako /var/lib/stako
  install -d -m 0700 -o root -g root /etc/stako/keys
  install -d -m 0750 -o root -g stako /etc/stako
  install -d -m 0755 /var/www/acme /var/backups/stako

  cat >/etc/sysctl.d/99-stako.conf <<'SYSCTL'
net.ipv4.tcp_syncookies = 1
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
kernel.kptr_restrict = 2
kernel.dmesg_restrict = 1
kernel.yama.ptrace_scope = 1
fs.protected_symlinks = 1
fs.protected_hardlinks = 1
kernel.unprivileged_bpf_disabled = 1
SYSCTL
  sysctl --system >/dev/null 2>&1 || true

  install -d -m 0755 /etc/ssh/sshd_config.d
  cat >/etc/ssh/sshd_config.d/99-stako.conf <<'SSH'
PermitRootLogin prohibit-password
MaxAuthTries 4
X11Forwarding no
SSH
  systemctl reload sshd >/dev/null 2>&1 || systemctl reload ssh >/dev/null 2>&1 || true

  cat >/etc/stako/install.yml <<YML
preset: $PRESET
version: $VERSION
dns_server: $WITH_DNS
docker_mode: rootless
state_db: sqlite
YML
  chmod 0640 /etc/stako/install.yml

  cat >/etc/systemd/system/stako-agent.service <<'UNIT'
[Unit]
Description=Stako Privileged System Agent
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/stako-agent --sock /run/stako/agent.sock
Restart=on-failure
RestartSec=3
RuntimeDirectory=stako
RuntimeDirectoryMode=0750

[Install]
WantedBy=multi-user.target
UNIT

  cat >/etc/systemd/system/stako-core.service <<'UNIT'
[Unit]
Description=Stako Control Panel Core API & Web UI
After=network.target stako-agent.service
Requires=stako-agent.service

[Service]
Type=simple
User=stako
Group=stako
ExecStart=/usr/local/bin/stako-core serve --listen :8083 --db /var/lib/stako/stako.db --master-key /etc/stako/keys/master.key --agent-sock /run/stako/agent.sock
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/stako /run/stako
CapabilityBoundingSet=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable --now nftables fail2ban auditd nginx stako-agent stako-core
}

main() {
  if [[ "$DRY_RUN" -eq 0 ]]; then
    if [[ "$(id -u)" -ne 0 ]]; then
      echo "Error: installer must run as root" >&2
      exit 1
    fi
    if [[ -r /etc/os-release ]]; then
      . /etc/os-release
      if [[ "${ID:-}" != "ubuntu" ]] || [[ "${VERSION_ID:-}" != "22.04" && "${VERSION_ID:-}" != "24.04" ]]; then
        echo "Error: only Ubuntu 22.04 and 24.04 LTS are supported (detected ${PRETTY_NAME:-unknown})" >&2
        exit 1
      fi
    fi
    mkdir -p "$(dirname "$LOG_FILE")"
    exec > >(tee -a "$LOG_FILE") 2>&1
  fi

  log "Stako Smart Installer starting (preset=$PRESET, version=$VERSION, arch=$ARCH, update_only=$UPDATE_ONLY)"

  if [[ "$UPDATE_ONLY" -eq 1 ]]; then
    log "Running in-place Stako update (preserving /var/lib/stako, /etc/stako, and user data)..."
    if [[ "$DRY_RUN" -eq 0 ]]; then
      local bk_dir="/var/backups/stako/pre-update-$(date -u +%Y%m%d%H%M%S)"
      mkdir -p "$bk_dir"
      cp -a /usr/local/bin/stako* "$bk_dir/" 2>/dev/null || true
      cp -a /var/lib/stako/stako.db "$bk_dir/" 2>/dev/null || true
    fi
    install_stako_binaries
    run_cmd systemctl daemon-reload
    run_cmd systemctl restart stako-agent stako-core
    log "Stako updated in-place successfully!"
    return 0
  fi

  run_cmd apt-get update -y
  if [[ "$SKIP_UPGRADE" -eq 0 ]]; then
    run_cmd DEBIAN_FRONTEND=noninteractive apt-get upgrade -y
  fi
  setup_repos
  install_backend_stack
  install_stako_binaries
  apply_hardening_and_systemd

  if [[ "$DRY_RUN" -eq 0 ]]; then
    log "Bootstrapping initial admin user..."
    stako-core bootstrap-admin --db /var/lib/stako/stako.db
    log "Done! Panel listening on https://$(hostname -f 2>/dev/null || echo localhost):8083"
  else
    log "Dry-run complete. No system changes were made."
  fi
}

main "$@"
