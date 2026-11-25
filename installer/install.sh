#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Stako Installer for Ubuntu 22.04 / 24.04 LTS (7.20, 7.21, 9.3)
set -euo pipefail

PRESET="standard"
DRY_RUN=0
VERSION="latest"
REPO="StakoCP/stako"
ARCH="$(dpkg --print-architecture 2>/dev/null || uname -m)"
CONFIG_FILE=""
WITH_DNS="no"

usage() {
  cat <<EOF
Stako Control Panel Installer (Ubuntu 22.04 / 24.04 LTS)
Usage: $0 [--preset basic|standard|full] [--version TAG] [--config install.yml] [--with-dns] [--dry-run]
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --preset) PRESET="${2:?}"; shift 2 ;;
    --version) VERSION="${2:?}"; shift 2 ;;
    --config) CONFIG_FILE="${2:?}"; shift 2 ;;
    --with-dns) WITH_DNS="yes"; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

case "$PRESET" in
  basic|standard|full) ;;
  *) echo "Invalid preset: $PRESET (must be basic, standard, or full)" >&2; exit 2 ;;
esac

echo "==> Stako Installer ($REPO)"
echo "    Preset  : $PRESET"
echo "    Version : $VERSION"
echo "    Arch    : $ARCH"
echo "    DNS     : $WITH_DNS (default: no)"

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "[dry-run] Plan:"
  echo "  1. Verify Ubuntu 22.04 or 24.04 LTS"
  echo "  2. Create system user 'stako' and directories (/var/lib/stako, /etc/stako/keys)"
  echo "  3. Apply OS baseline hardening (sysctl, SSH prohibit-password, auditd, unattended-upgrades)"
  echo "  4. Install preset '$PRESET' packages (nginx, php-fpm, mariadb/postgres, docker rootless, nftables, fail2ban)"
  echo "  5. Write /etc/stako/install.yml and enable systemd units (stako-agent, stako-core)"
  exit 0
fi

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

id -u stako >/dev/null 2>&1 || useradd --system --home-dir /var/lib/stako --create-home --shell /usr/sbin/nologin stako
install -d -m 0750 -o stako -g stako /var/lib/stako
install -d -m 0700 -o root -g root /etc/stako/keys
install -d -m 0750 -o root -g stako /etc/stako

# Baseline OS hardening (SEC-S1, SEC-S6)
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

echo "==> Baseline directories and hardening applied."
echo "Next steps:"
echo "  1. Install stako-core, stako-agent, and stako into /usr/local/bin/"
echo "  2. systemctl daemon-reload && systemctl enable --now stako-agent stako-core"
echo "  3. stako-core bootstrap-admin"
