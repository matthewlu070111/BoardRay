# BoardRay

BoardRay 是一个由 Xray 驱动的节点 Agent。它可以同时接入 [BoardLess](https://github.com/matthewlu070111/BoardLess) 和
[vps-panel](https://github.com/renaissance0721/vps-panel)，并把双方下发的 VLESS 入站合并到一个受管 Xray 进程。

首版有意只支持两种固定组合：

- VLESS + TCP + TLS + XTLS Vision
- VLESS + TCP/RAW + REALITY + XTLS Vision

不支持任意 Xray JSON、Shadowsocks、Realm、VMess 或 Trojan。vps-panel 下发这些配置时，Agent 会明确回报失败。

## BoardLess 后端识别

<!-- BOARDLESS_BACKEND_REPOSITORY_V1 -->

<!-- boardless:backend:start -->
```json boardless-backend
{
  "recognitionCode": "BOARDLESS_BACKEND_REPOSITORY_V1",
  "schemaVersion": 1,
  "backendId": "io.github.matthewlu070111.boardray",
  "name": "BoardRay Xray Agent",
  "version": "v0.2.1",
  "panelApiVersion": "v1",
  "install": {
    "script": "scripts/install.sh",
    "sha256": "2369f9c894da30b4365d5e64390f3b7b46bf6aaca13bf1d88b65d551029e1949",
    "uninstallScript": "scripts/uninstall.sh"
  },
  "presets": [
    {
      "id": "vless-tcp-xtls-vision",
      "name": "VLESS TCP XTLS Vision",
      "protocol": "vless",
      "description": "自动 ACME 证书与内置网页回落",
      "config": {
        "server": "{{ input.server }}",
        "port": 443,
        "transport": "tcp",
        "tls": true,
        "sni": "{{ input.sni }}",
        "flow": "xtls-rprx-vision",
        "acmeEmail": "{{ input.acmeEmail }}"
      },
      "inputs": [
        {
          "key": "server",
          "label": "节点域名",
          "type": "hostname",
          "installArg": "--domain",
          "placeholder": "node.example.com",
          "help": "请先将域名解析到这台节点服务器的公网 IP"
        },
        {
          "key": "sni",
          "label": "TLS SNI",
          "type": "hostname",
          "placeholder": "node.example.com",
          "help": "通常与节点域名相同"
        },
        {
          "key": "acmeEmail",
          "label": "ACME 邮箱",
          "type": "email",
          "installArg": "--acme-email",
          "placeholder": "ops@example.com",
          "help": "用于申请和续期 TLS 证书"
        },
        {
          "key": "enableVpsPanel",
          "label": "同时同步到 VPS Panel",
          "type": "checkbox",
          "required": false,
          "default": "false",
          "installArg": "--mode",
          "checkedValue": "both",
          "uncheckedValue": "boardless",
          "help": "启用后，BoardRay 会把 BoardLess 与 VPS Panel 的入站合并到同一个 Xray 进程"
        },
        {
          "key": "vpsPanelUrl",
          "label": "VPS Panel 地址",
          "type": "url",
          "placeholder": "https://vps-panel.example.com",
          "installArg": "--vps-panel-url",
          "when": { "key": "enableVpsPanel", "equals": "true" }
        },
        {
          "key": "vpsEnrollmentToken",
          "label": "VPS Panel 注册令牌",
          "type": "password",
          "placeholder": "一次性 Enrollment Token",
          "installArg": "--vps-enrollment-token",
          "sensitive": true,
          "when": { "key": "enableVpsPanel", "equals": "true" },
          "help": "仅用于本次安装，不会保存在 BoardLess 节点配置或安装令牌记录中"
        },
        {
          "key": "vpsAgentVersion",
          "label": "VPS Panel Agent 兼容版本",
          "type": "text",
          "default": "v0.20.2",
          "installArg": "--vps-agent-version",
          "when": { "key": "enableVpsPanel", "equals": "true" }
        }
      ],
      "generatedOutputs": []
    },
    {
      "id": "vless-tcp-xtls-vision-reality",
      "name": "VLESS TCP XTLS Vision REALITY",
      "protocol": "vless",
      "description": "本机生成 REALITY 密钥与 Short ID",
      "config": {
        "server": "{{ input.server }}",
        "port": 443,
        "transport": "tcp",
        "tls": true,
        "sni": "{{ input.sni }}",
        "flow": "xtls-rprx-vision",
        "realityTarget": "{{ input.realityTarget }}",
        "realityPublicKey": "{{ generated.realityPublicKey }}",
        "shortId": "{{ generated.shortId }}"
      },
      "inputs": [
        {
          "key": "server",
          "label": "节点地址",
          "type": "hostname",
          "placeholder": "node.example.com",
          "help": "填写客户端可以访问的域名或公网 IP"
        },
        {
          "key": "sni",
          "label": "REALITY SNI",
          "type": "hostname",
          "placeholder": "www.microsoft.com"
        },
        {
          "key": "realityTarget",
          "label": "REALITY 目标",
          "type": "text",
          "installArg": "--reality-target",
          "placeholder": "www.microsoft.com:443",
          "help": "目标必须支持 TLS 1.3，并包含端口"
        },
        {
          "key": "enableVpsPanel",
          "label": "同时同步到 VPS Panel",
          "type": "checkbox",
          "required": false,
          "default": "false",
          "installArg": "--mode",
          "checkedValue": "both",
          "uncheckedValue": "boardless",
          "help": "启用后，BoardRay 会把 BoardLess 与 VPS Panel 的入站合并到同一个 Xray 进程"
        },
        {
          "key": "vpsPanelUrl",
          "label": "VPS Panel 地址",
          "type": "url",
          "placeholder": "https://vps-panel.example.com",
          "installArg": "--vps-panel-url",
          "when": { "key": "enableVpsPanel", "equals": "true" }
        },
        {
          "key": "vpsEnrollmentToken",
          "label": "VPS Panel 注册令牌",
          "type": "password",
          "placeholder": "一次性 Enrollment Token",
          "installArg": "--vps-enrollment-token",
          "sensitive": true,
          "when": { "key": "enableVpsPanel", "equals": "true" },
          "help": "仅用于本次安装，不会保存在 BoardLess 节点配置或安装令牌记录中"
        },
        {
          "key": "vpsAgentVersion",
          "label": "VPS Panel Agent 兼容版本",
          "type": "text",
          "default": "v0.20.2",
          "installArg": "--vps-agent-version",
          "when": { "key": "enableVpsPanel", "equals": "true" }
        }
      ],
      "generatedOutputs": ["realityPublicKey", "shortId"]
    }
  ]
}
```
<!-- boardless:backend:end -->

`install.sha256` 会固定到当前提交中的安装脚本。修改安装脚本后运行
`scripts/update-manifest-hash.sh`，并一并提交 README 变化。

每个预设的 `inputs` 同时是 BoardLess“新增节点”页面的表单定义。`key` 用于
`{{ input.<key> }}` 模板替换，`label`、`type`、`placeholder` 和 `help` 由
BoardLess 原样用于生成配置菜单；`installArg` 把对应值安全地附加到安装命令。因此新增或修改配置项时应同步更新这里，不能依赖
BoardLess 前端硬编码 BoardRay 参数。

## 一键安装

要求：Debian/Ubuntu、systemd、root、amd64 或 arm64。安装程序不会修改云防火墙；TLS 模式必须允许公网 TCP/80，代理端口也必须放行。

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
  --vps-agent-version v0.20.2 \
  --unattended
```

### `mode=both` 运行逻辑

在 BoardLess 新增节点时勾选“同时同步到 VPS Panel”，面板会额外要求 VPS Panel 地址、一次性注册令牌和兼容版本，并生成带 `--mode both` 的安装命令。运行逻辑如下：

1. 安装程序先使用 BoardLess `installToken` 完成 bootstrap，取得正式 `nodeToken`；REALITY 私钥仍只在节点本机生成和保存。
2. BoardRay 启动后使用 VPS Panel `enrollment_token` 调用 `/api/agent/register`。注册成功后把返回的 `agent_token` 写入权限为 `0600` 的本机配置，并清空本机的一次性注册令牌。
3. BoardLess 每 30 秒拉取一次完整用户快照并发送心跳；VPS Panel 同时通过认证 WebSocket 触发变更，并由同一轮同步读取 desired state。任一控制面暂时不可用不会停止另一个控制面的同步。
4. 两边的配置先转换为统一的入站模型，再合并成一份 Xray 配置。BoardLess 管理的入站优先；如果 VPS Panel Proxy 使用了同一端口，该 Proxy 会被跳过，并向 VPS Panel 回报该版本应用失败。
5. 合并结果必须通过 `xray run -test` 才会原子替换并重启受管 Xray。失败时恢复上一份可用配置，同时把失败结果回报 VPS Panel；不会启动两套互相争抢端口的 Xray。
6. 用户统计严格按命名空间分流：`br-user-*` 只汇总并上报 BoardLess，`vp-client-*` 只上报 VPS Panel。一个控制面的用户、凭据、到期时间和流量不会发送给另一个控制面。
7. BoardLess 鉴权收到 `401` 时立即撤销 BoardLess 入站；普通网络失败最多沿用最近快照 15 分钟。VPS Panel 暂时拉取失败时保留最近成功的 desired state，直到收到并成功应用更新版本。

`both` 只是让同一个 BoardRay 进程同时消费两个控制面的配置，不会把 BoardLess 节点注册成 VPS Panel 用户，也不会在两个面板之间复制账号或套餐。

BoardLess 生成的一次性安装命令使用 `--install-token`。安装程序会把令牌放在 `/api/node/v1/bootstrap` 的 JSON 请求体中；控制面返回的 `nodeToken` 只写入本机 `0600` 配置文件，不会输出到终端。若连接到尚未实现 bootstrap 的旧控制面，安装程序会在收到 `404` 后停止并提示改用 `--node-token`，不会自动降级或泄露令牌。

重复执行安装命令会更新已校验的 Agent、Xray 和 acme.sh，并保留现有 `/etc/boardray/config.json`。

## 运行行为

- BoardLess 每 30 秒拉取完整用户快照，失联最多沿用 15 分钟；令牌收到 `401` 时立即撤销其入站。
- vps-panel 使用原生注册、认证 WebSocket、配置版本、配置回执、主机指标和流量接口。
- 双控制面监听端口冲突时 BoardLess 优先；冲突的 vps-panel Proxy 被跳过并收到失败回执。
- 所有配置先经 `xray run -test`，再原子替换、重启并探测监听端口；失败会恢复上一份配置。
- TLS 使用固定并校验的 acme.sh，通过 HTTP-01 签发，提前 30 天续期；Xray 证书续期后自动重启。
- REALITY 私钥只存放在节点的 `0600` 配置文件中，BoardLess bootstrap 只接收公钥和 Short ID。
- Xray 用户统计分别使用 `br-user-*` 和 `vp-client-*`，不会把流量上报给错误的控制面。

主要路径：

```text
/opt/boardray/bin/boardray-agent
/opt/boardray/xray/xray
/etc/boardray/config.json
/etc/boardray/xray/config.json
/var/lib/boardray/state.json
```

查看状态：

```bash
systemctl status boardray-agent boardray-xray
journalctl -u boardray-agent -f
```

卸载默认保留配置和状态；彻底删除必须显式指定 `--purge`：

```bash
sudo bash scripts/uninstall.sh
sudo bash scripts/uninstall.sh --purge
```

## 开发与验证

```bash
go test ./...
go vet ./...
bash -n scripts/*.sh
```

设置 `BOARDRAY_TEST_XRAY=/path/to/xray` 后，测试还会用真实的 Xray `run -test` 校验 TLS 与 REALITY 合并配置。Release CI 固定使用 Xray `v26.3.27` 执行该检查。

## 安全边界

- Xray 固定为 `v26.3.27`，amd64/arm64 下载均校验 SHA-256。
- vps-panel 兼容目标固定为 `v0.20.2`；BoardRay 不接受其自动升级指令。
- 配置、token、私钥和运行状态不会写入普通日志。
- Agent 不接管已有第三方 Xray，也不自动修改云防火墙或 DNS。
