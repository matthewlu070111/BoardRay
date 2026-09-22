# BoardRay

BoardRay 是一个由 Xray 驱动的节点 Agent。它可以同时接入 [BoardLess](https://github.com/matthewlu070111/BoardLess) 和
[vps-panel](https://github.com/renaissance0721/vps-panel)，并把双方下发的 VLESS 入站合并到一个受管 Xray 进程。

首版有意只支持两种固定组合：

- VLESS + TCP + TLS + XTLS Vision
- VLESS + TCP/RAW + REALITY + XTLS Vision

不支持任意 Xray JSON、Shadowsocks、Realm、VMess 或 Trojan。vps-panel 下发这些配置时，Agent 会明确回报失败。

## BoardLess 后端识别

BoardLess 后端清单已独立存放在 [`boardless-backend.json`](boardless-backend.json)。导入仓库时将“识别文件路径”设置为
`boardless-backend.json`；清单中的 `recognitionCode` 用于确认仓库主动声明兼容 BoardLess。

`install.sha256` 会固定到当前提交中的安装脚本。修改安装脚本后运行
`scripts/update-manifest-hash.sh`，并一并提交 `boardless-backend.json` 与 README 变化。

每个预设的 `inputs` 同时是 BoardLess“新增节点”页面的表单定义。`key` 用于
`{{ input.<key> }}` 模板替换，`label`、`type`、`placeholder` 和 `help` 由
BoardLess 原样用于生成配置菜单；`installArg` 把对应值安全地附加到安装命令。因此新增或修改配置项时应同步更新独立清单，不能依赖
BoardLess 前端硬编码 BoardRay 参数。

## 一键安装

要求：Debian/Ubuntu、systemd、root、amd64 或 arm64。安装程序会通过 `apt-get` 确保 Nginx 已安装，但不会修改云防火墙；TLS 模式必须允许公网 TCP/80，代理端口也必须放行。

当前 BoardLess 已实现的 node-token 路径：

```bash
sudo bash scripts/install.sh \
  --mode boardless \
  --panel-url https://panel.example.com \
  --node-token 'NODE_TOKEN' \
  --preset vless-tcp-xtls-vision \
  --domain node.example.com \
  --acme-email ops@example.com \
  --unattended
```

REALITY：

```bash
sudo bash scripts/install.sh \
  --mode boardless \
  --panel-url https://panel.example.com \
  --node-token 'NODE_TOKEN' \
  --preset vless-tcp-xtls-vision-reality \
  --reality-target www.microsoft.com:443 \
  --reality-private-key 'PRIVATE_KEY_ALREADY_GENERATED' \
  --reality-public-key 'PUBLIC_KEY_CONFIGURED_IN_BOARDLESS' \
  --reality-short-id '0123456789abcdef' \
  --unattended
```

使用现有 `--node-token` 时，REALITY 公钥和 Short ID 已经存在于 BoardLess，因此安装时必须提供与之匹配的本机私钥。使用 `--install-token` 时，安装程序会在节点自动生成密钥，只把公钥和 Short ID 提交给 bootstrap 接口。

BoardLess 与 vps-panel 同时接入：

```bash
sudo bash scripts/install.sh \
  --mode both \
  --panel-url https://boardless.example.com \
  --node-token 'NODE_TOKEN' \
  --preset vless-tcp-xtls-vision-reality \
  --reality-target www.microsoft.com:443 \
  --reality-private-key 'PRIVATE_KEY_ALREADY_GENERATED' \
  --reality-public-key 'PUBLIC_KEY_CONFIGURED_IN_BOARDLESS' \
  --reality-short-id '0123456789abcdef' \
  --vps-panel-url https://vps-panel.example.com \
  --vps-enrollment-token 'ONE_TIME_TOKEN' \
  --vps-agent-version v0.25.0 \
  --unattended
```

## 更新已有安装

以下命令从 GitHub 的固定版本标签下载安装程序、校验 SHA256，然后更新已有安装。`--update` 会保留
`/etc/boardray/config.json` 中的面板地址、令牌、REALITY 密钥和回落站点配置，不需要再次提供安装令牌：

```bash
wget -O /tmp/boardray-install.sh \
  https://raw.githubusercontent.com/matthewlu070111/BoardRay/v0.4.1/scripts/install.sh
echo 'd0d27c3013a6a105cad670c7f1fe608417fb61e68dd48e1fd705b98d0acc7074  /tmp/boardray-install.sh' | sha256sum -c -
sudo bash /tmp/boardray-install.sh --update --unattended
```

### 强制补齐网页落地

已有节点缺少网页落地，或需要重新生成受管 Nginx 回落配置时，可使用固定版本的
[网页落地更新脚本](https://raw.githubusercontent.com/matthewlu070111/BoardRay/v0.4.1/scripts/install.sh)。下面的命令会安装
`v0.4.1`、重建 `/etc/nginx/conf.d/boardray.conf`，并把网页落地常驻开关写回
`/etc/boardray/config.json`：

```bash
wget -O /tmp/boardray-fallback-update.sh \
  https://raw.githubusercontent.com/matthewlu070111/BoardRay/v0.4.1/scripts/install.sh
echo 'd0d27c3013a6a105cad670c7f1fe608417fb61e68dd48e1fd705b98d0acc7074  /tmp/boardray-fallback-update.sh' | sha256sum -c -
sudo bash /tmp/boardray-fallback-update.sh \
  --update \
  --fallback-site www.lovelive-anime.jp \
  --force-fallback \
  --unattended
```

将 `www.lovelive-anime.jp` 替换为需要展示的 HTTPS 落地站点域名；只接受主机名，不要填写协议、路径或查询参数。
该命令会强制覆盖已有的 `runtime.fallback_site`，并设置 `runtime.fallback_always_on=true`。之后即使节点尚未审核、
BoardLess 暂时不可用或有效代理用户为零，TLS 端口仍只为网页落地保持监听；普通更新仍会保留原值和开关。
`--force-fallback` 仅支持 `vless-tcp-xtls-vision` TLS 预设，REALITY 预设会被安装器明确拒绝。

更新完成后确认版本和服务状态：

```bash
/opt/boardray/bin/boardray-agent version
systemctl status boardray-agent boardray-xray --no-pager
```

如果最初使用了自定义安装目录或服务名，请在更新命令中继续传入相同的 `--install-dir` 或
`--service-name`。如需安装指定旧版本，可以额外传入 `--agent-version vX.Y.Z`，并使用对应版本标签下的安装脚本。

### `mode=both` 运行逻辑

在 BoardLess 新增节点时勾选“同时同步到 VPS Panel”，面板会额外要求 VPS Panel 地址、一次性注册令牌和兼容版本，并生成带 `--mode both` 的安装命令。兼容版本默认 `v0.25.0`，也可用 `--vps-agent-version` 跟随 VPS Panel 的发布节奏自行调整。运行逻辑如下：

1. 安装程序先使用 BoardLess `installToken` 完成 bootstrap，取得正式 `nodeToken`；REALITY 私钥仍只在节点本机生成和保存。
2. BoardRay 启动后使用 VPS Panel `enrollment_token` 调用 `/api/agent/register`。注册成功后把返回的 `agent_token` 写入权限为 `0600` 的本机配置，并清空本机的一次性注册令牌。
3. BoardLess 每 30 秒拉取一次完整用户快照并发送心跳；VPS Panel 同时通过认证 WebSocket 触发变更，并由同一轮同步读取 desired state。任一控制面暂时不可用不会停止另一个控制面的同步。
4. 两边的配置先转换为统一的入站模型，再合并成一份 Xray 配置。BoardLess 管理的入站优先；如果 VPS Panel Proxy 使用了同一端口，该 Proxy 会被跳过，并向 VPS Panel 回报该版本应用失败。
5. 合并结果必须通过 `xray run -test` 才会原子替换并重启受管 Xray。验证失败不会写入；写入后的启动或探测失败会保留新配置和错误现场，同时把失败结果回报 VPS Panel。
6. 用户统计严格按命名空间分流：`br-user-*` 只汇总并上报 BoardLess，`vp-client-*` 只上报 VPS Panel。一个控制面的用户、凭据、到期时间和流量不会发送给另一个控制面。
7. BoardLess 鉴权收到 `401` 时立即撤销 BoardLess 入站；普通网络失败最多沿用最近快照 15 分钟。VPS Panel 暂时拉取失败时保留最近成功的 desired state，直到收到并成功应用更新版本。

`both` 只是让同一个 BoardRay 进程同时消费两个控制面的配置，不会把 BoardLess 节点注册成 VPS Panel 用户，也不会在两个面板之间复制账号或套餐。

BoardLess 生成的一次性安装命令使用 `--install-token`。安装程序会把令牌放在 `/api/node/v1/bootstrap` 的 JSON 请求体中；控制面返回的 `nodeToken` 只写入本机 `0600` 配置文件，不会输出到终端。若连接到尚未实现 bootstrap 的旧控制面，安装程序会在收到 `404` 后停止并提示改用 `--node-token`，不会自动降级或泄露令牌。

重复执行安装命令会更新已校验的 Agent、Xray 和 acme.sh，保留现有凭据和控制面配置，并把回落字段迁移到受管 Nginx 配置。

### Nginx 回落

安装和更新都会确保 Nginx 已安装，并生成独立的 `/etc/nginx/conf.d/boardray.conf`。现有 BoardRay 配置会自动迁移为：

```json
{
  "fallback_address": "127.0.0.1:8001",
  "fallback_h2_address": "127.0.0.1:8002",
  "fallback_proxy_protocol": true
}
```

生成的 TLS 入站会把普通回落流量发送到 8001，把协商为 HTTP/2 的流量发送到 8002，并用 PROXY protocol v1 将真实来源地址传给 Nginx。使用 `--fallback-site HOST` 可以修改反代上游；已有配置会保留之前的 `runtime.fallback_site`。

安装器只管理自己的 `boardray.conf`，Agent 另行管理仅用于 HTTP-01 的 `boardray-acme.conf`，不会覆盖其他 Nginx 站点。配置会先通过 `nginx -t`；失败时保留现场并停止更新，不写回旧配置。仓库中的 [`deploy/nginx-boardray.conf.example`](deploy/nginx-boardray.conf.example) 与安装器生成结构一致，可用于检查或手动定制。REALITY 入站不使用 TLS fallback。

## 运行行为

- BoardLess 每 30 秒拉取完整用户快照，失联最多沿用 15 分钟；令牌收到 `401` 时立即撤销其入站。
- vps-panel 使用原生注册、认证 WebSocket、配置版本、配置回执、主机指标和流量接口。
- 双控制面监听端口冲突时 BoardLess 优先；冲突的 vps-panel Proxy 被跳过并收到失败回执。
- 所有配置先经 `xray run -test`，再原子替换、重启并探测 API 与代理监听端口；失败时保留新配置和错误现场。
- TLS 使用固定并校验的 acme.sh，通过 Nginx 模式完成 HTTP-01 签发；Agent 会按实际域名更新独立的 `/etc/nginx/conf.d/boardray-acme.conf`，校验并热重载 Nginx，全程无需停止服务。证书提前 30 天续期，Xray 在证书更新后自动重启。
- Xray 的本地 API 入站仅启用 `StatsService`，只采集用户上下行流量；同时启用 Cloudflare DoH、广告/BT/中国及私网目标阻断，并为代理入站启用 HTTP/TLS/QUIC 嗅探。
- TLS 最低版本为 1.3，证书启用 OCSP stapling；Nginx 回落使用独立 HTTP/1.1 与 HTTP/2 端口及 PROXY protocol。
- REALITY 私钥只存放在节点的 `0600` 配置文件中，BoardLess bootstrap 只接收公钥和 Short ID。
- Xray 用户统计分别使用 `br-user-*` 和 `vp-client-*`，不会把流量上报给错误的控制面。

主要路径：

```text
/opt/boardray/bin/boardray-agent
/opt/boardray/xray/xray
/opt/boardray/xray/geoip.dat
/opt/boardray/xray/geosite.dat
/etc/boardray/config.json
/etc/boardray/xray/config.json
/etc/nginx/conf.d/boardray.conf
/etc/nginx/conf.d/boardray-acme.conf
/var/lib/boardray/state.json
```

查看状态：

```bash
systemctl status boardray-agent boardray-xray
journalctl -u boardray-agent -f
```

## 卸载教程

从固定版本标签下载卸载脚本并校验 SHA-256：

```bash
wget -O /tmp/boardray-uninstall.sh \
  https://raw.githubusercontent.com/matthewlu070111/BoardRay/v0.4.1/scripts/uninstall.sh
echo 'b77b709518d5c5017d4d27e8ef258f4299a8ce73e9f462ca71776a25057583f9  /tmp/boardray-uninstall.sh' | sha256sum -c -
```

普通卸载会停止并删除 BoardRay Agent、受管 Xray、systemd 服务及 BoardRay 的 Nginx 配置，但保留
`/etc/boardray` 和 `/var/lib/boardray`，方便以后恢复：

```bash
sudo bash /tmp/boardray-uninstall.sh
```

确认不再需要节点令牌、REALITY 私钥、证书、运行状态和流量计数后，可彻底删除所有本机数据。此操作不可恢复：

```bash
sudo bash /tmp/boardray-uninstall.sh --purge
```

如果安装时使用了自定义路径或服务名，卸载时必须传入相同参数：

```bash
sudo bash /tmp/boardray-uninstall.sh \
  --install-dir /custom/boardray \
  --service-name custom-boardray-agent
```

卸载完成后可以确认服务、监听端口和受管配置均已移除：

```bash
systemctl status boardray-agent boardray-xray --no-pager
ss -lntp | grep -E ':(443|10085)\\b' || true
test ! -e /etc/nginx/conf.d/boardray.conf
```

卸载器不会删除系统安装的 Nginx，也不会自动删除 BoardLess 或 VPS Panel 中的节点记录；不再使用该节点时，请在对应面板中另行删除或停用。

## 开发与验证

```bash
go test ./...
go vet ./...
bash -n scripts/*.sh
```

设置 `BOARDRAY_TEST_XRAY=/path/to/xray` 后，测试还会用真实的 Xray `run -test` 校验 TLS 与 REALITY 合并配置。Release CI 固定使用 Xray `v26.3.27` 执行该检查。

## 安全边界

- Xray 固定为 `v26.3.27`，amd64/arm64 下载均校验 SHA-256。
- vps-panel 兼容目标默认 `v0.25.0`，可通过 `--vps-agent-version` 自定义；BoardRay 不接受其自动升级指令。
- 配置、token、私钥和运行状态不会写入普通日志。
- Agent 不接管已有第三方 Xray，也不自动修改云防火墙或 DNS。
