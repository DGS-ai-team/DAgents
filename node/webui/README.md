# Node Web UI

浏览器 Client，挂载在 Agent Node `GET /ui/`，UI 对齐 [DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI) 工作台风格（双栏布局、浅色主题、气泡消息、内联工具审批）。

## 目录

| 路径 | 说明 |
|------|------|
| `frontend/` | Vue 3 + Vite 源码 |
| `build.sh` | 构建到 `../internal/webui/static/`（`go:embed`） |

## 构建与测试

```bash
bash node/webui/build.sh
cd node/webui/frontend && npm test
```

产出写入 `node/internal/webui/static/`（**不入库**，CI / Release 构建；本地 `go test` / `go build` 前须先执行本脚本），由 `dagents-node` 内嵌提供。

## 部署后带 UI 启动

Web UI **已打包进 `dagents-node` 二进制**，无需 Node.js 或单独前端服务。

```bash
# 1. 配置（与其它 Client 共用）
cp packaging/agent-client/config.example.yaml config.yaml
# 编辑 llm、listen.port 等；ui.enabled 省略时默认 true

# 2. 启动 Node（Release 包 / 本地编译均可）
./dagents node
# 或: ./bin/dagents-node -config config.yaml
# 或: ./scripts/startup/linux/start-node.sh

# 3. 浏览器打开（端口见 config listen.port，默认 18765）
http://127.0.0.1:18765/ui/
```

`dagents node` 就绪后会打印 `[dagents] Web UI: http://127.0.0.1:…/ui/`。

关闭 UI：在 `config.yaml` 中设置 `ui.enabled: false`（仅隐藏 `/ui/`，不影响 Node API）。

## 开发

与 Go 运维 Client **共用** `packaging/agent-client/config.yaml`（从 `config.example.yaml` 复制后按需编辑）。

### 方式 A：VS Code / Cursor 复合调试

| 配置 | 说明 |
|------|------|
| **Web UI: Node + Vite** | Node 终端 + Vite 热更新 |
| **Web UI: Chrome** | 打开 `http://localhost:5173/ui/`（需 Vite 已运行） |
| **Node: run (terminal)** | 仅 Go Node（内嵌 `/ui/`，无热更新） |
| **Web UI: Node + 内嵌 UI** | Node + Chrome 直连 `http://127.0.0.1:18765/ui/` |

开发时优先用 Vite（热更新）；验收 embed 时用 Node 内嵌 `/ui/`。

### 方式 B：手动双终端

```bash
# 终端 1：Node
go run ./node/cmd/dagents-node -config packaging/agent-client/config.example.yaml

# 终端 2：Vite（代理 /v1、/health → Node）
cd node/webui/frontend && npm run dev
```

浏览器打开 **`http://localhost:5173/ui/`**（勿用 Node 内嵌 `/ui/`，否则无热更新）。

环境变量（可选）：

| 变量 | 默认 | 说明 |
|------|------|------|
| `DAGENTS_NODE_URL` | `http://127.0.0.1:18765` | Vite 代理目标 |
| `WEBUI_DEV_PORT` | `5173` | Vite 开发端口 |

## 配置

`config.yaml` 中可选：

```yaml
ui:
  enabled: true   # false 时不挂载 /ui/
```

## 功能

- **双栏工作台**：主聊天 + Runtime（会话、审批、工具执行气泡）。
- **远程工作者条**：输入框上方显示工作中子 Agent / 对端 Agent 数量（SSE + `listChildAgents`）。
- **`read_file` 预览**：按扩展名渲染 Markdown、HTML、JSON、CSV、代码高亮或纯文本。
- **HITL**：内联工具审批与用户询问；订阅 **`hitl_required`**（`expandHitlRequired` 展开入队），兼容 A2A 的 `approval_required` / `user_information_required`。

## API

复用 Node 现有 `/v1` HTTP/SSE，封装见 `frontend/src/api/node.js`。
