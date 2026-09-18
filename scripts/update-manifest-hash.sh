#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
hash="$(sha256sum "$root/scripts/install.sh" | awk '{print $1}')"
BOARDRAY_INSTALL_HASH="$hash" perl -0pi -e 's/("sha256": ")[^"]+(".*)/$1$ENV{BOARDRAY_INSTALL_HASH}$2/' "$root/README.md"
printf '%s\n' "$hash"
