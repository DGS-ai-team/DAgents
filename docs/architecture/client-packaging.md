# Client + Agent Node 同包发布

与 [agent-client-refactor-plan.md](../design/agent-client-refactor-plan.md) 配套；描述本地助手同包发布与安装布局。

## 1. 配置（单文件示例）

```yaml
# config.yaml — Node 与 Client 共用
agent_id: ops-linux-01

listen:
  host: 127.0.0.1
  port: 18765

local:
  endpoint: http://127.0.0.1:18765

fs_root: ./.runtime

llm:
  provider: openai
  base_url: https://api.openai.com/v1
  model: gpt-4.1
  api_key_env: OPENAI_API_KEY
  mock: true

manage:
  enabled: false

triggers:
  enabled: true
  poll_seconds: 5
```

- **Client** 只读 `local.endpoint` 与可选 `agent_id`（校验一致性）。
- **Node** 读完整配置；`listen` 决定 bind 地址。
- **工具审批** 不在 YAML 中配置；首次启动 Node 时从 `packaging/runtime/policy` 复制到 `.runtime/policy/*.approval.txt`。

## 2. 目录布局（安装后）

```text
/opt/dagents/
  bin/dagents-node
  bin/dagents-client
  etc/config.yaml
  etc/policy.yaml
  var/workspace/          # fs_root
    memory/sessions.db    # SQLite 会话库
    data/                 # 临时工作区
```

## 3. 启动顺序

```text
1. dagents-node -config /opt/dagents/etc/config.yaml
2. dagents-client -config /opt/dagents/etc/config.yaml chat
   # 或 dagents-client -config ... tui [session_id]
```

交互式 TUI 斜杠命令：`/status`、`/sessions`、`/switch`、`/new`、`/clear`、`/history`、`/tools`、`/quit`。SSE 断线后自动按 `Last-Event-ID` 重连。

systemd：Node 为 `Type=simple` 服务；Client 为用户会话或 SSH 登录后启动。

## 4. 同机多 Agent

每个 `agent_id` 独立目录、独立 `port`、独立 systemd unit；Client 配置指向对应 `local.endpoint`。

## 5. 构建产物

Phase AC Release：`linux-amd64` 与 `windows-amd64` 压缩包（Go Node + PyInstaller Textual TUI）；glibc 目标见 [go-node-compatibility.md](./go-node-compatibility.md)。

```bash
scripts/package_local_assistant.sh
# 产物: dist/dagents-local-assistant-linux-amd64-<version>.tar.gz（或 windows zip）
```
