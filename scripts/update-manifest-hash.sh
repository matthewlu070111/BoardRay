#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
hash="$(sha256sum "$root/scripts/install.sh" | awk '{print $1}')"
perl -0pi -e 's/("sha256": ")[^"]+(".*)/$1'"$hash"'$2/' "$root/README.md"
printf '%s\n' "$hash"
