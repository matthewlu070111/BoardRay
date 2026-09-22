#!/usr/bin/env bash
set -euo pipefail

REPOSITORY="matthewlu070111/BoardRay"
DEFAULT_AGENT_VERSION="v0.4.1"
XRAY_VERSION="v26.3.27"
XRAY_AMD64_SHA256="23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae"
XRAY_ARM64_SHA256="4d30283ae614e3057f730f67cd088a42be6fdf91f8639d82cb69e48cde80413c"
ACME_REVISION="3661fd86b6304115e42f43910e6dd452ab9866d6"
ACME_SHA256="fcabf274d4f96966ec933879ae0257266e8ef2f7d16161f14b84dd896c0cac32"

mode="boardless"
agent_version="$DEFAULT_AGENT_VERSION"
install_dir="/opt/boardray"
service_name="boardray-agent"
panel_url=""
install_token=""
node_token=""
preset=""
domain=""
acme_email=""
reality_target=""
reality_private=""
reality_public=""
reality_short_id=""
fallback_site=""
fallback_site_set=false
force_fallback=false
vps_panel_url=""
vps_enrollment_token=""
vps_agent_version="v0.25.0"
unattended=false
update_only=false

die() { printf 'boardray install: %s\n' "$*" >&2; exit 1; }
need_value() { [[ $# -ge 2 && -n "$2" ]] || die "$1 requires a value"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode) need_value "$@"; mode="$2"; shift 2 ;;
    --agent-version) need_value "$@"; agent_version="$2"; shift 2 ;;
    --install-dir) need_value "$@"; install_dir="$2"; shift 2 ;;
    --service-name) need_value "$@"; service_name="$2"; shift 2 ;;
    --panel-url) need_value "$@"; panel_url="${2%/}"; shift 2 ;;
    --install-token) need_value "$@"; install_token="$2"; shift 2 ;;
    --node-token) need_value "$@"; node_token="$2"; shift 2 ;;
    --preset) need_value "$@"; preset="$2"; shift 2 ;;
    --domain) need_value "$@"; domain="$2"; shift 2 ;;
    --acme-email) need_value "$@"; acme_email="$2"; shift 2 ;;
    --reality-target) need_value "$@"; reality_target="$2"; shift 2 ;;
    --reality-private-key) need_value "$@"; reality_private="$2"; shift 2 ;;
    --reality-public-key) need_value "$@"; reality_public="$2"; shift 2 ;;
    --reality-short-id) need_value "$@"; reality_short_id="$2"; shift 2 ;;
    --fallback-site) need_value "$@"; fallback_site="$2"; fallback_site_set=true; shift 2 ;;
    --force-fallback) force_fallback=true; shift ;;
    --vps-panel-url) need_value "$@"; vps_panel_url="${2%/}"; shift 2 ;;
    --vps-enrollment-token) need_value "$@"; vps_enrollment_token="$2"; shift 2 ;;
    --vps-agent-version) need_value "$@"; vps_agent_version="$2"; shift 2 ;;
    --update) update_only=true; shift ;;
    --unattended) unattended=true; shift ;;
    --help)
      sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# \?//'
      exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

# Usage: install.sh --mode boardless|vps-panel|both [options]
# BoardLess: --panel-url URL (--node-token TOKEN|--install-token TOKEN) --preset ID
# TLS: --domain HOST --acme-email EMAIL
# Nginx fallback: [--fallback-site HOST]
# Keep the TLS landing page online without an approved proxy inbound: [--force-fallback]
# REALITY: --reality-target HOST:PORT [--reality-private-key KEY --reality-public-key KEY --reality-short-id HEX]
# vps-panel: --vps-panel-url URL --vps-enrollment-token TOKEN
# Update an existing installation: --update

[[ $EUID -eq 0 ]] || die "run as root"
[[ "$mode" == "boardless" || "$mode" == "vps-panel" || "$mode" == "both" ]] || die "invalid --mode"
[[ "$service_name" =~ ^[A-Za-z0-9_.@-]+$ ]] || die "invalid --service-name"
[[ "$install_dir" == /* && "$install_dir" != *[[:space:]]* ]] || die "--install-dir must be an absolute path without spaces"
case "$install_dir" in /|/opt|/usr|/etc|/var|/bin|/sbin) die "--install-dir is too broad" ;; esac
[[ "$agent_version" =~ ^v[0-9][A-Za-z0-9._-]*$ ]] || die "invalid --agent-version"
if $fallback_site_set; then
  [[ "$fallback_site" =~ ^[A-Za-z0-9.-]+$ && "$fallback_site" != .* && "$fallback_site" != *. ]] || die "invalid --fallback-site"
fi
[[ -d /run/systemd/system ]] || die "systemd is required"
case "$(uname -m)" in
  x86_64|amd64) arch="amd64"; xray_asset="Xray-linux-64.zip"; xray_sha="$XRAY_AMD64_SHA256" ;;
  aarch64|arm64) arch="arm64"; xray_asset="Xray-linux-arm64-v8a.zip"; xray_sha="$XRAY_ARM64_SHA256" ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

for command in curl jq unzip sha256sum; do
  command -v "$command" >/dev/null || die "$command is required"
done

config_dir="/etc/boardray"
state_dir="/var/lib/boardray"
config_path="$config_dir/config.json"

if $update_only; then
  [[ -f "$config_path" ]] || die "--update requires an existing $config_path"
elif [[ "$mode" != "vps-panel" ]]; then
  [[ -n "$panel_url" && -n "$preset" ]] || die "BoardLess mode requires --panel-url and --preset"
  [[ -n "$node_token" || -n "$install_token" ]] || die "BoardLess mode requires --node-token or --install-token"
  [[ -z "$node_token" || -z "$install_token" ]] || die "use only one BoardLess token type"
  case "$preset" in
    vless-tcp-xtls-vision) [[ -n "$domain" && -n "$acme_email" ]] || die "TLS preset requires --domain and --acme-email" ;;
    vless-tcp-xtls-vision-reality)
      [[ -n "$reality_target" ]] || die "REALITY preset requires --reality-target"
      if [[ -n "$node_token" ]]; then
        [[ -n "$reality_private" && -n "$reality_public" && -n "$reality_short_id" ]] ||
          die "REALITY with --node-token requires the private/public key pair and short ID already configured in BoardLess"
      fi
      ;;
    *) die "unsupported preset: $preset" ;;
  esac
fi
if $force_fallback; then
  fallback_preset="$preset"
  if $update_only; then
    fallback_preset="$(jq -r '.boardless.preset // empty' "$config_path")"
  fi
  [[ "$fallback_preset" == "vless-tcp-xtls-vision" ]] || die "--force-fallback requires the BoardLess TLS preset"
fi
if ! $update_only && [[ "$mode" != "boardless" ]]; then
  [[ -n "$vps_panel_url" && -n "$vps_enrollment_token" ]] || die "vps-panel mode requires its URL and enrollment token"
fi

mkdir -p "$install_dir/bin" "$install_dir/xray" "$install_dir/acme" "$config_dir/xray" "$state_dir"
rm -f "$config_dir/xray/config.previous.json"
chmod 700 "$config_dir" "$config_dir/xray" "$state_dir"

tmp_dir="$(mktemp -d /tmp/boardray-install.XXXXXX)"
trap 'rm -rf "$tmp_dir"' EXIT

release_base="https://github.com/$REPOSITORY/releases/download/$agent_version"
curl -fsSL "$release_base/SHA256SUMS" -o "$tmp_dir/SHA256SUMS"
agent_asset="boardray-linux-$arch"
curl -fsSL "$release_base/$agent_asset" -o "$tmp_dir/$agent_asset"
(cd "$tmp_dir" && grep "  $agent_asset\$" SHA256SUMS | sha256sum -c -) || die "Agent checksum verification failed"
install -m 0755 "$tmp_dir/$agent_asset" "$install_dir/bin/boardray-agent"

if [[ -f "$config_path" ]]; then
  printf 'Existing %s preserved; BoardRay will be updated to %s.\n' "$config_path" "$agent_version"
  if ! $fallback_site_set; then
    fallback_site="$(jq -r '.runtime.fallback_site // empty' "$config_path")"
  fi
else
  if [[ "$mode" != "vps-panel" && "$preset" == "vless-tcp-xtls-vision-reality" && -n "$install_token" ]]; then
    IFS=$'\t' read -r reality_private reality_public reality_short_id < <("$install_dir/bin/boardray-agent" keygen)
  fi
  if [[ -n "$install_token" ]]; then
    bootstrap_body="$(jq -n --arg token "$install_token" --arg version "$agent_version" --arg public "$reality_public" --arg short "$reality_short_id" \
      '{installToken:$token,agentVersion:$version,generatedOutputs:({} + (if $public == "" then {} else {realityPublicKey:$public,shortId:$short} end))}')"
    status="$(curl -sS -o "$tmp_dir/bootstrap.json" -w '%{http_code}' -X POST "$panel_url/api/node/v1/bootstrap" \
      -H 'Content-Type: application/json' --data "$bootstrap_body")"
    if [[ "$status" == "404" ]]; then
      die "BoardLess does not implement /api/node/v1/bootstrap yet; create a node and rerun with --node-token"
    fi
    if [[ "$status" != "200" && "$status" != "201" ]]; then
      bootstrap_error="$(jq -r '.error // empty' "$tmp_dir/bootstrap.json" 2>/dev/null || true)"
      if [[ "$status" == "401" ]]; then
        die "BoardLess bootstrap rejected the one-time install token (invalid, expired, or already used); generate a fresh install command in BoardLess and run it promptly"
      fi
      [[ -z "$bootstrap_error" ]] || die "BoardLess bootstrap failed with HTTP $status: $bootstrap_error"
      die "BoardLess bootstrap failed with HTTP $status"
    fi
    node_token="$(jq -er '.nodeToken' "$tmp_dir/bootstrap.json")" || die "BoardLess bootstrap response has no node token"
  fi
  jq -n \
    --arg mode "$mode" --arg panel "$panel_url" --arg node_token "$node_token" --arg preset "$preset" \
    --arg domain "$domain" --arg email "$acme_email" --arg target "$reality_target" \
    --arg private "$reality_private" --arg public "$reality_public" --arg short "$reality_short_id" \
    --arg vps_panel "$vps_panel_url" --arg vps_enrollment "$vps_enrollment_token" --arg vps_version "$vps_agent_version" \
    --arg install_dir "$install_dir" \
    '{mode:$mode,runtime:{state_path:"/var/lib/boardray/state.json",xray_binary:($install_dir+"/xray/xray"),xray_config:"/etc/boardray/xray/config.json",xray_service:"boardray-xray.service",cert_dir:"/etc/boardray/xray/certs",acme_script:($install_dir+"/acme/acme.sh"),acme_home:"/var/lib/boardray/acme",fallback_address:"127.0.0.1:18080",stats_address:"127.0.0.1:10085",stale_grace_seconds:900}}
      + (if $mode == "vps-panel" then {} else {boardless:{panel_url:$panel,node_token:$node_token,preset:$preset,domain:$domain,acme_email:$email,reality_target:$target,reality_private_key:$private,reality_public_key:$public,reality_short_id:$short}} end)
      + (if $mode == "boardless" then {} else {vps_panel:{panel_url:$vps_panel,enrollment_token:$vps_enrollment,announced_version:$vps_version}} end)' \
    > "$tmp_dir/config.json"
  install -m 0600 "$tmp_dir/config.json" "$config_path"
fi

[[ -n "$fallback_site" ]] || fallback_site="www.lovelive-anime.jp"
[[ "$fallback_site" =~ ^[A-Za-z0-9.-]+$ && "$fallback_site" != .* && "$fallback_site" != *. ]] || die "invalid fallback site in existing configuration"

xray_url="https://github.com/XTLS/Xray-core/releases/download/$XRAY_VERSION/$xray_asset"
curl -fsSL "$xray_url" -o "$tmp_dir/xray.zip"
printf '%s  %s\n' "$xray_sha" "$tmp_dir/xray.zip" | sha256sum -c - || die "Xray checksum verification failed"
unzip -p "$tmp_dir/xray.zip" xray > "$tmp_dir/xray"
unzip -p "$tmp_dir/xray.zip" geoip.dat > "$tmp_dir/geoip.dat"
unzip -p "$tmp_dir/xray.zip" geosite.dat > "$tmp_dir/geosite.dat"
install -m 0755 "$tmp_dir/xray" "$install_dir/xray/xray"
install -m 0644 "$tmp_dir/geoip.dat" "$install_dir/xray/geoip.dat"
install -m 0644 "$tmp_dir/geosite.dat" "$install_dir/xray/geosite.dat"

acme_url="https://raw.githubusercontent.com/acmesh-official/acme.sh/$ACME_REVISION/acme.sh"
curl -fsSL "$acme_url" -o "$tmp_dir/acme.sh"
printf '%s  %s\n' "$ACME_SHA256" "$tmp_dir/acme.sh" | sha256sum -c - || die "acme.sh checksum verification failed"
install -m 0755 "$tmp_dir/acme.sh" "$install_dir/acme/acme.sh"

if ! command -v nginx >/dev/null; then
  command -v apt-get >/dev/null || die "nginx is required and apt-get is unavailable"
  DEBIAN_FRONTEND=noninteractive apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install --no-install-recommends -y nginx
fi

cat > "$tmp_dir/boardray-nginx.conf" <<EOF
map \$http_upgrade \$boardray_connection_upgrade {
    default upgrade;
    ""      close;
}

server {
    listen 127.0.0.1:8001 proxy_protocol;
    listen 127.0.0.1:8002 http2 proxy_protocol;

    set_real_ip_from 127.0.0.1;
    real_ip_header proxy_protocol;
    set \$boardray_fallback_host $fallback_site;

    location / {
        resolver 1.1.1.1 ipv6=off;
        proxy_pass https://\$boardray_fallback_host;
        proxy_http_version 1.1;
        proxy_ssl_server_name on;
        proxy_ssl_name \$boardray_fallback_host;
        proxy_set_header Host \$boardray_fallback_host;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection \$boardray_connection_upgrade;
        proxy_set_header X-Real-IP \$proxy_protocol_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
}
EOF

nginx_config="/etc/nginx/conf.d/boardray.conf"
install -m 0644 "$tmp_dir/boardray-nginx.conf" "$nginx_config"
nginx -t || die "generated nginx configuration is invalid"
systemctl enable --now nginx.service || die "nginx could not start with the BoardRay configuration"
systemctl reload nginx.service || die "nginx could not reload the BoardRay configuration"

jq --arg fallback_site "$fallback_site" --argjson force_fallback "$force_fallback" \
  '.runtime.fallback_address = "127.0.0.1:8001"
   | .runtime.fallback_h2_address = "127.0.0.1:8002"
   | .runtime.fallback_proxy_protocol = true
   | .runtime.fallback_site = $fallback_site
   | if $force_fallback then .runtime.fallback_always_on = true else . end' \
  "$config_path" > "$tmp_dir/config-with-nginx.json"
install -m 0600 "$tmp_dir/config-with-nginx.json" "$config_path"

cat > /etc/systemd/system/boardray-xray.service <<EOF
[Unit]
Description=BoardRay managed Xray
After=network-online.target nginx.service
Wants=network-online.target nginx.service

[Service]
Type=simple
ExecStart=$install_dir/xray/xray run -config /etc/boardray/xray/config.json
Restart=on-failure
RestartSec=2
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

cat > "/etc/systemd/system/$service_name.service" <<EOF
[Unit]
Description=BoardRay dual-control-plane agent
After=network-online.target nginx.service
Wants=network-online.target nginx.service

[Service]
Type=simple
ExecStart=$install_dir/bin/boardray-agent -config $config_path
Restart=always
RestartSec=3
UMask=0077

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$service_name.service"
systemctl restart "$service_name.service"

printf 'BoardRay installed with managed Nginx fallback to %s. Ensure public TCP/80 and configured proxy ports are allowed.\n' "$fallback_site"
printf 'Status: systemctl status %s.service\n' "$service_name"
