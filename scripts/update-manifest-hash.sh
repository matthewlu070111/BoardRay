#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
hash="$(sha256sum "$root/scripts/install.sh" | awk '{print $1}')"
uninstall_hash="$(sha256sum "$root/scripts/uninstall.sh" | awk '{print $1}')"
BOARDRAY_INSTALL_HASH="$hash" perl -0pi -e '
  s/("sha256": ")[^"]+(")/$1$ENV{BOARDRAY_INSTALL_HASH}$2/;
' "$root/boardless-backend.json"
BOARDRAY_INSTALL_HASH="$hash" perl -0pi -e '
  s/(echo '\''\K)[0-9a-f]{64}(  \/tmp\/boardray-(?:install|fallback-update)\.sh'\'')/$ENV{BOARDRAY_INSTALL_HASH}$2/g;
' "$root/README.md"
BOARDRAY_UNINSTALL_HASH="$uninstall_hash" perl -0pi -e '
  s/(echo '\''\K)[0-9a-f]{64}(  \/tmp\/boardray-uninstall\.sh'\'')/$ENV{BOARDRAY_UNINSTALL_HASH}$2/g;
' "$root/README.md"
printf '%s\n' "$hash"
