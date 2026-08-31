# 共享配置（`shared/config`）

Agent Node 与 Client 共用的 YAML 配置加载与校验。

## 文件

| 文件 | 说明 |
|------|------|
| `config.go` | `Config` 结构体、`LoadFile`、`Validate`、`ApplyDefaults` |
| `node_id.go` | `ResolveNodeID`、`NodeIDFilePath`：`.runtime/node/node_id` 持久化；旧 `.runtime/agent/agent_id` 仅用于升级迁移 |
| `resolve.go` | `ResolveConfigPath`：`-config` / `DAGENTS_CONFIG` / 默认候选路径 |
| `config_test.go` | 默认值、必填项、环境变量展开单测 |

示例配置见 [`packaging/agent-client/config.example.yaml`](../../packaging/agent-client/config.example.yaml)。

## 主要配置块

| 块 | 说明 |
|----|------|
| `listen` / `local` | Node 监听与 Client 连接 endpoint |
| `llm` | 模型连接（迁移种子）；工具轮次上限见 Agent snapshot |
| `runtime_root` | **不可配置**；固定 `./.runtime`。Node 的 `data/`、`memory/`、`skills/`、`policy/` 等管理目录相对此根 |
| `skills` | 技能开关与 prompt 上限（目录固定为 `{runtime_root}/skills`；不属于 Agent workspace） |
| `compression` | 上下文压缩 token 阈值 |
| `triggers` | 触发器调度（见下表） |
| `tools` | 内置工具编码与 bash 压缩（工具组见 Agent 快照） |
| `log` | Node stderr 日志级别 |
| `ui` | 内嵌浏览器 Web UI（`GET /ui/`）；`enabled` 省略时默认 `true` |

### `tools`

| 键 | 默认 | 说明 |
|----|------|------|
| `bash_output_encoding` | 按平台 | `bash_run` 子进程 stdout/stderr 解码（如 `utf-8`、`gbk`） |
| `file_encoding` | 按平台 | FS 工具读写磁盘默认编码；单次调用可用 `encoding` 参数覆盖 |
| `bash_compress` | 见 example | `bash_run` 输出清洗与 rune 截断；`output_mode: head_tail` 可额外保留尾部 |

Node 级 `tools.enabled_groups` 已移除；工具组由各 Agent / 模板的 `defaults.tools.enabled_groups` 决定（空=不启用任何组）。组名与成员见 `ExpandBuiltinToolGroups` / `builtinToolGroups`。`tools.enabled` 仍会在加载时拒绝。

**可配置工具组**

| 组名 | 包含工具 |
|------|----------|
| `fs` | `read_file`、`write_file`、`glob_files`、`grep_file`、`grep_files`、`search_replace` |
| `bash` | `bash_run` |
| `terminal` | `terminal_config_list`、`terminal_open`、`terminal_input`、`terminal_read`、`terminal_terminate`、`terminal_list` |
| `hitl` | `ask_user_information` |
| `memory` | `remember` |
| `skills` | `load_skills`、`unload_skills`、`clear_skills` |
| `triggers` | `trigger_list`、`trigger_get`、`trigger_create`、`trigger_update`、`trigger_delete` |
| `child_agents` | `create_temporary_agent`、`wait_temporary_agents`、`temporary_agent_status`、`cancel_temporary_agent` |
| `browser` | 任务级：`browser_run_task` / `browser_task_status` / `browser_task_cancel`（伴生 Chrome） |

各工具作用见 [handbook/04-能力与策略.md](../../docs/handbook/04-能力与策略.md) §1；示例见 [`packaging/agent-client/config.example.yaml`](../../packaging/agent-client/config.example.yaml)。

### `triggers`

| 键 | 默认 | 说明 |
|----|------|------|
| `enabled` | `true` | 是否启动后台调度轮询 |
| `poll_seconds` | `5` | 到期扫描间隔（秒，至少 1） |

持久化路径固定为 `{runtime_root}/triggers/triggers.json`。

condition 语义（interval / fire_at / schedule / cmd）见 [`node/internal/triggers/README.md`](../../node/internal/triggers/README.md)。

## 测试

```bash
go test ./shared/config/...
```
