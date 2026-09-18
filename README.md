# BoardRay

BoardRay 是一个由 Xray 驱动的节点 Agent。它可以同时接入 BoardLess 和
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
  "version": "0.1.1",
  "panelApiVersion": "v1",
  "install": {
    "script": "scripts/install.sh",
    "sha256": "763fdeee84be15fb4590b7cc2eb59821f3fdaee9b0cf4393a4dc053b09686f85",
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
      "requiredInputs": ["server", "sni", "acmeEmail"],
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
      "requiredInputs": ["server", "sni", "realityTarget"],
      "generatedOutputs": ["realityPublicKey", "shortId"]
    }
  ]
}
```
<!-- boardless:backend:end -->

`install.sha256` 会固定到当前提交中的安装脚本。修改安装脚本后运行
`scripts/update-manifest-hash.sh`，并一并提交 README 变化。

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

BoardLess 文档中的一次性安装令牌也可使用 `--install-token`。当前控制面若尚未实现 `/api/node/v1/bootstrap`，安装程序会在收到 `404` 后停止，并提示改用 `--node-token`；不会自动降级或输出令牌。

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
