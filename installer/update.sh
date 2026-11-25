#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

curl -fsSLO https://raw.githubusercontent.com/StakoCP/stako/refs/heads/main/installer/install.sh
bash install.sh --update "$@"
