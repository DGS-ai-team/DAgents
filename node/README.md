# Agent Node（Go）

单进程 Agent 运行时：LLM turn loop、工具执行、会话与 SQLite 持久化（N5）。

配置模型见 [`shared/config/`](../shared/config/)。内部协作见 [`docs/architecture/go-node-internals.md`](../docs/architecture/go-node-internals.md)；联调见 [`docs/development.md`](../docs/development.md)。

## `internal/` 包历史评估（非当前契约）

> 本节保留架构演进记录；包规模和阶段建议可能过时，当前目录与职责以源码和下方目录说明为准。

| 包 | 规模 | 职责 | 评估 | 阶段 A 建议 |
|----|------|------|------|-------------|
| **`api`** | 10 文件 ~2.2k | HTTP/SSE、`NewServer` 装配 | `server.go` 偏大（~870 行），路由+装配耦合 | 后续拆 `server_routes.go` / `server_wire.go`；测试 workspace root 用 `t.TempDir()` |
| **`session`** | 15 文件 ~2.8k | Manager、runtime、队列消费、HITL 与 Workgroup Session | 边界清晰；`runtime.go` ~630 行可接受 | 保持 runtime/turn 边界；继续补恢复测试 |
| **`turn`** | 15 文件 ~2.6k | Orchestrator、tool 路由、HITL、prompt | `orchestrator.go` ~525 行、`tool_router.go` ~369 行 | 暂不动；单测已按 concern 拆分 |
| **`tools`** | 46 文件 ~5.7k | Registry、fs/bash/job/领域 tool | **已完成阶段 A**（见下） | 阶段 B：子 package + `Register` |
| **`triggers`** | 12 文件 ~2.1k | 存储、schedule、HTTP API | 与 `tools/tool_triggers` 分工明确 | 保持 |
| **`manage`** | 9 文件 ~1.3k | 注册、Release、Workgroup Dialer | 与运行时边界清晰 | 保持出站连接模型 |
| **`childagent`** | 10 文件 ~1.3k | 子 Agent 生命周期 | 与 `tools/tool_childagent` 分工明确 | 保持 |
| **`llm`** | 16 文件 ~1.8k | OpenAI/DeepSeek 适配 | provider 分文件合理 | 保持 |
| **`policy`** | 10 文件 ~1.3k | 审批策略引擎 | 独立专题包 | 保持 |
| **`compression`** | 4 文件 ~790 | 上下文压缩 | 小且聚焦 | 保持 |
| **`skills`** | 2 文件 ~500 | SKILL.md 扫描 | 小；catalog 注入 tools | 保持 |
| **`store`** / **`history`** / **`queue`** / **`stream`** | 各 2–3 文件 | 持久化、JSONL、队列、SSE | 粒度合适 | 保持 |
| **`hitl`** | 3 文件 ~290 | 共享 HITL 类型 | 与 turn 配合 | 保持 |
| **`promptcontext`** / **`hostsnapshot`** / **`logx`** / **`version`** | 1–2 文件 | 侧车、环境快照、日志、版本 | 辅助包 | 保持 |

**整体结论**：Node 按 **api → session → turn → tools/llm/policy** 分层合理；主要历史债务在 **`tools/` 根目录过平**（已阶段 A 整理）与 **`api/server.go` 体量**（待后续）。

---

## 目录

| 路径 | 说明 |
|------|------|
| `cmd/dagents-node/` | Node 进程入口 |
| `internal/api/` | HTTP/SSE 路由与运行时装配 |
| `internal/session/` | session 表、per-session 队列与 turn 消费 |
| `internal/turn/` | turn 编排 + 工具循环 + system prompt |
| `internal/tools/` | 本地工具 Registry（**阶段 A 已按域重命名**，见 [`internal/tools/README.md`](internal/tools/README.md)） |
| `internal/triggers/` | 触发器存储与调度 |
| `internal/manage/` | Manage 注册、Release 与 Workgroup 出站集成 |
| `internal/childagent/` | 临时子 Agent |
| `internal/llm/` | LLM 客户端与消息适配 |
| `internal/store/`、`history/` | SQLite 与 JSONL 审计 |
| `internal/version/` | **全项目唯一**构建版本号（`GET /health.version`） |
| `internal/webui/` | 内嵌浏览器 Web UI（`go:embed` 静态资源，`GET /ui/`） |
| `webui/` | Web UI 前端源码（Vue 3 + Vite）与 `build.sh` |

系统服务安装：Linux [`scripts/linux/install_node_service.sh`](../scripts/linux/install_node_service.sh)；Windows [`scripts/windows/install_node_service.cmd`](../scripts/windows/install_node_service.cmd)。

## Web UI（浏览器 Client）

Node 启动后可直接在本机浏览器（Chrome/Edge）打开：

```text
http://127.0.0.1:<listen.port>/ui/
```

- 配置：`ui.enabled`（默认 `true`；`false` 时不挂载 `/ui/`）
- 构建：`bash node/webui/build.sh`（产出 `node/internal/webui/static/`，不入库，嵌入二进制）
- 开发：`cd node/webui/frontend && npm run dev`（Vite 代理 `/v1`、`/health` 到 Node）
- 能力：对话、SSE 流式、HITL 审批/询问、斜杠命令、Sessions/Context/Skills/Children/Policy/Triggers 面板

## 本地运行

```bash
go run ./node/cmd/dagents-node -config packaging/agent-client/config.example.yaml
```

真实 LLM：在配置中设 `llm.mock: false` 并导出 `OPENAI_API_KEY`。

## 日志

```yaml
log:
  level: info   # debug | info | warn | error
```

或 `-log-level debug`。SSE **`turn_finished`** 语义见 [`docs/architecture/agent-node-api.md`](../docs/architecture/agent-node-api.md) §2.4.1。

## 测试

```bash
go test ./node/...
```
