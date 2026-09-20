#!/usr/bin/env bash
set -euo pipefail

purge=false
service_name="boardray-agent"
install_dir="/opt/boardray"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --purge) purge=true; shift ;;
    --service-name) [[ $# -ge 2 ]] || { printf '%s\n' '--service-name requires a value' >&2; exit 1; }; service_name="$2"; shift 2 ;;
    --install-dir) [[ $# -ge 2 ]] || { printf '%s\n' '--install-dir requires a value' >&2; exit 1; }; install_dir="$2"; shift 2 ;;
    *) printf 'unknown argument: %s\n' "$1" >&2; exit 1 ;;
  esac
done
[[ $EUID -eq 0 ]] || { printf 'run as root\n' >&2; exit 1; }
[[ "$service_name" =~ ^[A-Za-z0-9_.@-]+$ ]] || { printf 'invalid service name\n' >&2; exit 1; }
[[ "$install_dir" == /* && "$install_dir" != *[[:space:]]* ]] || { printf 'invalid install directory\n' >&2; exit 1; }
case "$install_dir" in /|/opt|/usr|/etc|/var|/bin|/sbin) printf 'install directory is too broad\n' >&2; exit 1 ;; esac

systemctl disable --now "$service_name.service" boardray-xray.service 2>/dev/null || true
rm -f "/etc/systemd/system/$service_name.service" /etc/systemd/system/boardray-xray.service
rm -f /etc/nginx/conf.d/boardray.conf
if command -v nginx >/dev/null && nginx -t >/dev/null 2>&1; then
  systemctl reload nginx.service 2>/dev/null || true
fi
systemctl daemon-reload
rm -rf "$install_dir"
if $purge; then
  rm -rf /etc/boardray /var/lib/boardray
  printf 'BoardRay removed, including configuration and state.\n'
else
  printf 'BoardRay removed; /etc/boardray and /var/lib/boardray were preserved.\n'
fi
