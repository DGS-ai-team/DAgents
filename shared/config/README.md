# 共享配置（`shared/config`）

Agent Node 与 Client 共用的 YAML 配置加载与校验。

## 文件

| 文件 | 说明 |
|------|------|
| `config.go` | `Config` 结构体、`LoadFile`、`Validate`、`ApplyDefaults` |
| `agent_id.go` | `ResolveAgentID`、`AgentIDFilePath`：`.runtime/agent/agent_id` 持久化 |
| `resolve.go` | `ResolveConfigPath`：`-config` / `DAGENTS_CONFIG` / 默认候选路径 |
| `config_test.go` | 默认值、必填项、环境变量展开单测 |

示例配置见 [`packaging/agent-client/config.example.yaml`](../../packaging/agent-client/config.example.yaml)。

## 主要配置块

| 块 | 说明 |
|----|------|
| `listen` / `local` | Node 监听与 Client 连接 endpoint |
| `llm` | 模型、mock、tool loop 上限 |
| `fs_root` | 工作区根（缺省 `./.runtime`）；`data/`、`memory/`、`skills/`、`policy/` 等子路径硬编码相对此根 |
| `skills` | 技能开关与 prompt 上限（目录固定为 `{fs_root}/skills`） |
| `compression` | 上下文压缩 token 阈值 |
| `triggers` | 触发器调度（见下表） |
| `tools` | 内置工具行为与允许列表（见下表） |
| `log` | Node stderr 日志级别 |

### `tools`

| 键 | 默认 | 说明 |
|----|------|------|
| `enabled_groups` | （省略=全部） | 内置工具**组**允许列表；组内工具一并启用/禁用；未知名在 `LoadFile` 校验失败 |
| `bash_output_encoding` | 按平台 | `bash_run` 子进程 stdout/stderr 解码（如 `utf-8`、`gbk`） |
| `file_encoding` | 按平台 | FS 工具读写磁盘默认编码；单次调用可用 `encoding` 参数覆盖 |
| `bash_compress` | 见 example | `bash_run` 输出清洗与 rune 截断 |

**`tools.enabled_groups` 语义**

- **省略或留空**：启用全部已注册内置工具（向后兼容）。
- **非空列表**：仅列出组内全部工具对 LLM 可见/可调用；handler 仍注册（便于子 Agent 委托等内部调用）。
- **校验**：组名须与 `shared/config/builtin_tools.go` 中 `builtinToolGroups` 一致；`tools.enabled` 已废弃。

**可配置工具组（7 组）**

| 组名 | 包含工具 |
|------|----------|
| `fs` | `read_file`、`write_file`、`glob_files`、`grep_file`、`grep_files`、`search_replace` |
| `bash` | `bash_run`、`background_job_status`、`background_job_cancel` |
| `hitl` | `ask_user_information` |
| `skills` | `load_skills`、`unload_skills`、`clear_skills` |
| `triggers` | `trigger_list`、`trigger_get`、`trigger_create`、`trigger_update`、`trigger_delete`、`trigger_fire` |
| `a2a` | `agent_invoke`、`agent_discover`（须 `manage.enabled`） |
| `child_agents` | `create_temporary_agent`、`wait_temporary_agents`、`temporary_agent_status`、`cancel_temporary_agent` |

各工具作用见 [`docs/built-in-tools.md`](../../docs/built-in-tools.md) §0；示例见 [`packaging/agent-client/config.example.yaml`](../../packaging/agent-client/config.example.yaml)。

### `triggers`

| 键 | 默认 | 说明 |
|----|------|------|
| `enabled` | `true` | 是否启动后台调度轮询 |
| `poll_seconds` | `5` | 到期扫描间隔（秒，至少 1） |

持久化路径固定为 `{fs_root}/triggers/triggers.json`。

condition 语义（interval / fire_at / schedule / cmd）见 [`node/internal/triggers/README.md`](../../node/internal/triggers/README.md)。

## 测试

```bash
go test ./shared/config/...
```
