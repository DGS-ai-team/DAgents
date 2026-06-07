# Agent Node（Go）

单进程 Agent 运行时：LLM turn loop、工具执行、会话与 SQLite 持久化（N5）。

## 目录

| 路径 | 说明 |
|------|------|
| `cmd/dagents-node/` | Node 进程入口 |
| `internal/api/` | HTTP/SSE 路由（sessions/messages/streams、持久化 API）、access log 中间件 |
| `internal/logx/` | 日志级别解析与 slog 辅助 |
| `internal/store/` | SQLite session 持久化 |
| `internal/triggers/` | 触发器 JSON 存储、日历 schedule + cmd 门控、调度器、`/v1/triggers` API |
| `internal/tools/` | 本地工具：`read_file`、`write_file`、`bash_run`、`trigger_*`（含 schedule 条件） |
| `internal/turn/` | turn 编排 + 工具循环 |
| `internal/version/` | 构建版本号 |

配置模型见 [`shared/config/`](../shared/config/)。架构与联调见 [`docs/architecture/local-assistant.md`](../docs/architecture/local-assistant.md)。

系统服务安装：Linux [`scripts/linux/install_node_service.sh`](../scripts/linux/install_node_service.sh)；Windows [`scripts/windows/install_node_service.cmd`](../scripts/windows/install_node_service.cmd)。

## 本地运行

```bash
# 仓库根目录（config.example.yaml 默认 llm.mock=true，无需 API Key）
go run ./node/cmd/dagents-node -config packaging/agent-client/config.example.yaml
```

真实 LLM：在配置中设 `llm.mock: false` 并导出 `OPENAI_API_KEY`。

## 日志

结构化日志输出到 **stderr**（`log/slog` text 格式）。

```yaml
# config.yaml
log:
  level: info   # debug | info | warn | error
```

或启动参数覆盖：

```bash
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml -log-level debug
```

**Info 级别**可见：HTTP 请求、session 创建/恢复/删除、turn 开始/结束、tool 调用、SSE 连接、resume 入队/处理。  
**Debug 级别**额外可见：Hub 事件 publish、message 入队、SSE 断开。

SSE **`done`** 语义（`turn_complete` / `awaiting`）：见 [`docs/architecture/agent-node-api.md`](../docs/architecture/agent-node-api.md) §2.4.1。

另开终端：

```bash
go run ./client/cmd/dagents-client -config packaging/agent-client/config.example.yaml probe
go run ./client/cmd/dagents-client -config packaging/agent-client/config.example.yaml chat "你好"
```

## 测试

```bash
go test ./node/...
```
