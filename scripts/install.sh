#!/usr/bin/env bash
set -euo pipefail

REPOSITORY="matthewlu070111/BoardRay"
DEFAULT_AGENT_VERSION="v0.1.1"
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
vps_panel_url=""
vps_enrollment_token=""
vps_agent_version="v0.20.2"
unattended=false

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
    --vps-panel-url) need_value "$@"; vps_panel_url="${2%/}"; shift 2 ;;
    --vps-enrollment-token) need_value "$@"; vps_enrollment_token="$2"; shift 2 ;;
    --vps-agent-version) need_value "$@"; vps_agent_version="$2"; shift 2 ;;
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
# REALITY: --reality-target HOST:PORT [--reality-private-key KEY --reality-public-key KEY --reality-short-id HEX]
# vps-panel: --vps-panel-url URL --vps-enrollment-token TOKEN

[[ $EUID -eq 0 ]] || die "run as root"
[[ "$mode" == "boardless" || "$mode" == "vps-panel" || "$mode" == "both" ]] || die "invalid --mode"
[[ "$service_name" =~ ^[A-Za-z0-9_.@-]+$ ]] || die "invalid --service-name"
[[ "$install_dir" == /* && "$install_dir" != *[[:space:]]* ]] || die "--install-dir must be an absolute path without spaces"
case "$install_dir" in /|/opt|/usr|/etc|/var|/bin|/sbin) die "--install-dir is too broad" ;; esac
[[ "$agent_version" =~ ^v[0-9][A-Za-z0-9._-]*$ ]] || die "invalid --agent-version"
[[ -d /run/systemd/system ]] || die "systemd is required"
case "$(uname -m)" in
  x86_64|amd64) arch="amd64"; xray_asset="Xray-linux-64.zip"; xray_sha="$XRAY_AMD64_SHA256" ;;
  aarch64|arm64) arch="arm64"; xray_asset="Xray-linux-arm64-v8a.zip"; xray_sha="$XRAY_ARM64_SHA256" ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

for command in curl jq unzip sha256sum; do
  command -v "$command" >/dev/null || die "$command is required"
done

if [[ "$mode" != "vps-panel" ]]; then
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
if [[ "$mode" != "boardless" ]]; then
  [[ -n "$vps_panel_url" && -n "$vps_enrollment_token" ]] || die "vps-panel mode requires its URL and enrollment token"
fi

config_dir="/etc/boardray"
state_dir="/var/lib/boardray"
config_path="$config_dir/config.json"
mkdir -p "$install_dir/bin" "$install_dir/xray" "$install_dir/acme" "$config_dir/xray" "$state_dir"
chmod 700 "$config_dir" "$config_dir/xray" "$state_dir"

tmp_dir="$(mktemp -d /tmp/boardray-install.XXXXXX)"
trap 'rm -rf "$tmp_dir"' EXIT

release_base="https://github.com/$REPOSITORY/releases/download/$agent_version"
curl -fsSL "$release_base/SHA256SUMS" -o "$tmp_dir/SHA256SUMS"
agent_asset="boardray-linux-$arch"
curl -fsSL "$release_base/$agent_asset" -o "$tmp_dir/$agent_asset"
(cd "$tmp_dir" && grep "  $agent_asset\$" SHA256SUMS | sha256sum -c -) || die "Agent checksum verification failed"
install -m 0755 "$tmp_dir/$agent_asset" "$install_dir/bin/boardray-agent"

xray_url="https://github.com/XTLS/Xray-core/releases/download/$XRAY_VERSION/$xray_asset"
curl -fsSL "$xray_url" -o "$tmp_dir/xray.zip"
printf '%s  %s\n' "$xray_sha" "$tmp_dir/xray.zip" | sha256sum -c - || die "Xray checksum verification failed"
unzip -p "$tmp_dir/xray.zip" xray > "$tmp_dir/xray"
install -m 0755 "$tmp_dir/xray" "$install_dir/xray/xray"

acme_url="https://raw.githubusercontent.com/acmesh-official/acme.sh/$ACME_REVISION/acme.sh"
curl -fsSL "$acme_url" -o "$tmp_dir/acme.sh"
printf '%s  %s\n' "$ACME_SHA256" "$tmp_dir/acme.sh" | sha256sum -c - || die "acme.sh checksum verification failed"
install -m 0755 "$tmp_dir/acme.sh" "$install_dir/acme/acme.sh"

if [[ -f "$config_path" ]]; then
  printf 'Existing %s preserved; binaries and services will be updated.\n' "$config_path"
else
  if [[ "$mode" != "vps-panel" && "$preset" == "vless-tcp-xtls-vision-reality" && -n "$install_token" ]]; then
    IFS=$'\t' read -r reality_private reality_public reality_short_id < <("$install_dir/bin/boardray-agent" keygen)
  fi
  if [[ -n "$install_token" ]]; then
    bootstrap_body="$(jq -n --arg preset "$preset" --arg version "$agent_version" --arg public "$reality_public" --arg short "$reality_short_id" \
      '{preset:$preset,agentVersion:$version,generatedOutputs:({} + (if $public == "" then {} else {realityPublicKey:$public,shortId:$short} end))}')"
    status="$(curl -sS -o "$tmp_dir/bootstrap.json" -w '%{http_code}' -X POST "$panel_url/api/node/v1/bootstrap" \
      -H "Authorization: Bearer $install_token" -H 'Content-Type: application/json' --data "$bootstrap_body")"
    if [[ "$status" == "404" ]]; then
      die "BoardLess does not implement /api/node/v1/bootstrap yet; create a node and rerun with --node-token"
    fi
    [[ "$status" == "200" || "$status" == "201" ]] || die "BoardLess bootstrap failed with HTTP $status"
    node_token="$(jq -er '.token' "$tmp_dir/bootstrap.json")" || die "BoardLess bootstrap response has no node token"
  fi
  jq -n \
    --arg mode "$mode" --arg panel "$panel_url" --arg node_token "$node_token" --arg preset "$preset" \
    --arg domain "$domain" --arg email "$acme_email" --arg target "$reality_target" \
    --arg private "$reality_private" --arg public "$reality_public" --arg short "$reality_short_id" \
    --arg vps_panel "$vps_panel_url" --arg vps_enrollment "$vps_enrollment_token" --arg vps_version "$vps_agent_version" \
    --arg install_dir "$install_dir" \
    '{mode:$mode,runtime:{state_path:"/var/lib/boardray/state.json",xray_binary:($install_dir+"/xray/xray"),xray_config:"/etc/boardray/xray/config.json",xray_previous:"/etc/boardray/xray/config.previous.json",xray_service:"boardray-xray.service",cert_dir:"/etc/boardray/xray/certs",acme_script:($install_dir+"/acme/acme.sh"),acme_home:"/var/lib/boardray/acme",fallback_address:"127.0.0.1:18080",stats_address:"127.0.0.1:10085",stale_grace_seconds:900}}
      + (if $mode == "vps-panel" then {} else {boardless:{panel_url:$panel,node_token:$node_token,preset:$preset,domain:$domain,acme_email:$email,reality_target:$target,reality_private_key:$private,reality_public_key:$public,reality_short_id:$short}} end)
      + (if $mode == "boardless" then {} else {vps_panel:{panel_url:$vps_panel,enrollment_token:$vps_enrollment,announced_version:$vps_version}} end)' \
    > "$tmp_dir/config.json"
  install -m 0600 "$tmp_dir/config.json" "$config_path"
fi

cat > /etc/systemd/system/boardray-xray.service <<EOF
[Unit]
Description=BoardRay managed Xray
After=network-online.target
Wants=network-online.target

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
After=network-online.target
Wants=network-online.target

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

printf 'BoardRay installed. Ensure public TCP/80 and configured proxy ports are allowed.\n'
printf 'Status: systemctl status %s.service\n' "$service_name"
