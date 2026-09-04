# node/internal/tools

N3 在 Node 进程内本地执行；面向模型的普通 tool schema **均为同步调用**（仅 `call_purpose` 通用参数）。`browser_run_task(wait=false)` 是独立的显式异步任务接口，使用自己的 task_id 和状态查询，不属于通用后台 job。

**`bash_run` 超时语义**：
- **显式传入 `timeout_seconds`**：同步等待该秒数，超时**终止并返回 `timed_out` 失败结果**，不会创建后台 job。
- **省略 `timeout_seconds`**：最长等待硬上限（默认 600 秒），超时同样**终止并报错**。
- **UI 控制**（仅 bash）：执行中可「终止」；不支持把 bash_run 转为后台。需要长期运行状态时使用 `terminal_open`。

`bash_run` 只走同步执行路径；`browser_run_task(wait=false)` 是独立的显式异步任务接口，使用自己的 task_id 和状态查询，不属于通用后台任务协议。

**配置**：工具组由 Agent `defaults.tools.enabled_groups` 决定，见 [handbook/04-能力与策略.md](../../../docs/handbook/04-能力与策略.md) §1、[handbook/附录/内置工具参考.md](../../../docs/handbook/附录/内置工具参考.md)、[`shared/config/README.md`](../../../shared/config/README.md)。  
**工具用法**：写在各 tool schema `description` 中（各 `tool_*` / `fs_*` / `bash_*` 文件）。

## 工具结果契约

工具 handler 保持各自正文格式；统一状态在
`result_contract.go` 中分类，并由 turn 层附加到 `tool_result` SSE：

| 字段 | 语义 |
|------|------|
| `status` | `succeeded`、`failed`、`denied`、`running`、`queued`、`cancelled`、`timed_out`、`awaiting_user`、`unknown` |
| `error` | 失败状态的 `{code, message, retryable}`，策略拒绝使用 `policy_denied` |
| `rejected` | 仅表示策略拒绝；执行失败不再误报为“已拒绝” |

状态优先于正文是否为空、`ERROR:` 前缀或本地化文案。正文中的既有字段（例如
`exit_code`、`stdout_bytes`、`output_empty`、`job_id`）继续作为工具证据保留。
执行器若同时返回部分正文和 Go `error`，编排器会保留正文并追加错误诊断，不再丢失
远端/MCP/SSH/SFTP 提供的证据。

**`terminal_read` 延时读取**：可传入 `wait_seconds`，工具会严格等待指定秒数后再读取终端输出；默认 0，最大 60 秒。等待期间如果 turn 被取消，会立即结束等待并返回取消错误。输出仍使用 `after_seq` / `next_seq` 游标读取。

---

## 目录布局（阶段 A，2026-06）

同 package `tools`，按 **文件名前缀** 分组：

```text
tools/
├── 核心 registry
│   types.go                  # ToolDef、FunctionDef、handler
│   result_contract.go        # 统一 tool_result status/error 分类
│   registry.go               # Registry、NewRegistry、Definitions、Execute
│   registry_path.go          # resolveWorkspaceRoot、resolvePath（Agent workspace）
│   registry_enabled.go       # SetBuiltinEnabled、filterToolDefs
│   executor.go               # Executor 接口
│   execution_mode.go         # call_purpose 清洗与上下文标记
│   tools_test.go             # registry 集成 smoke
├── 文件 fs_*
│   fs_read.go / fs_write.go / fs_search_replace.go
│   fs_glob_tool.go / fs_glob_internal.go
│   fs_grep_file.go / fs_grep_files.go / fs_grep_shared.go
│   fs_helpers.go / fs_encoding.go / fs_path_encoding.go
│   fs_stat.go                # StatRelPath（写盘信任链 Hook）
│   fs_*_test.go
├── Shell bash_*
│   bash_run_tool.go          # bashRunToolDef
│   bash_runner.go / bash_shell.go / bash_policy.go
│   bash_compress.go          # 配置 + 清洗 + 截断 + SSE 统计（原 6 文件合并）
│   shell_output_encoding.go / shell_platform_*.go
│   bash_*_test.go
├── Shell 执行状态
│   shell_execution.go        # 单次同步 bash 的取消/终态状态
├── 领域 tool_*
│   tool_skills.go / tool_hitl.go / tool_triggers.go
│   tool_childagent.go
│   tool_*_test.go
└── README.md / REFERENCE.md
```

---

## 执行边界

| 在 Registry 执行 | 在 turn 编排器执行 |
|------------------|-------------------|
| fs、bash、trigger CRUD | `load_skills` / `unload_skills` / `clear_skills` |
| | `ask_user_information` |
| | 子 Agent 管理类（registry 为 stub） |

---

## 阶段 B（未做）

子 package + `Register(r *Registry)`；待阶段 A 稳定后再开。

---

## 执行模式

- **同步（默认）**：orchestrator 调用 `Execute`；`read_file` / `write_file` / `trigger_create` 等始终同步完成。
- **`bash_run`**：始终同步调用；到达显式 timeout 或默认硬上限后杀进程并返回 `TIMED_OUT`，不登记后台 job，也不产生异步回灌。
- **`terminal_open`**：用于需要保持目录、环境或进程状态的长期交互任务。
- **写盘信任链**：`write_file` / `search_replace` 为 `rule` 时，同 session Agent 自建文件在 mtime 未变前提下后续写操作可免 HITL（`node/internal/hooks`，见 [ux-agent-owned-file-approval.md](../../../docs/design/ux-agent-owned-file-approval.md)）。

触发器 condition 语义见 [`../triggers/README.md`](../triggers/README.md).
