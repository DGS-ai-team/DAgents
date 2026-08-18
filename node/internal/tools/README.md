# node/internal/tools

N3 在 Node 进程内本地执行；面向模型的 tool schema **均为同步调用**（仅 `call_purpose` 通用参数）。

**`bash_run` 超时语义**：
- **显式传入 `timeout_seconds`**：同步等待该秒数，超时**自动降级**为后台 job（返回 `job_id`），完成后 `async_tool_result` 回灌。
- **省略 `timeout_seconds`**：最长等待硬上限（默认 600 秒），超时**终止并报错**（不转后台）。
- **UI 控制**（仅 bash）：执行中可「终止」或「转后台」；HTTP `POST /v1/agents/{id}/tool-calls/{tool_call_id}/cancel|background`。

内部仍保留 `StartBackground` / `job_registry` 供降级、UI 转后台与测试使用。

**配置**：工具组由 Agent `defaults.tools.enabled_groups` 决定，见 [handbook/04-能力与策略.md](../../docs/handbook/04-能力与策略.md) §1、[handbook/附录/内置工具参考.md](../../docs/handbook/附录/内置工具参考.md)、[`shared/config/README.md`](../../shared/config/README.md)。  
**工具用法**：写在各 tool schema `description` 中（各 `tool_*` / `fs_*` / `bash_*` 文件）。

**`terminal_read` 延时读取**：可传入 `wait_seconds`，工具会严格等待指定秒数后再读取终端输出；默认 0，最大 60 秒。等待期间如果 turn 被取消，会立即结束等待并返回取消错误。输出仍使用 `after_seq` / `next_seq` 游标读取。

---

## 目录布局（阶段 A，2026-06）

同 package `tools`，按 **文件名前缀** 分组：

```text
tools/
├── 核心 registry
│   types.go                  # ToolDef、FunctionDef、handler
│   registry.go               # Registry、NewRegistry、Definitions、Execute
│   registry_path.go          # resolveFSRoot、resolvePath
│   registry_enabled.go       # SetBuiltinEnabled、filterToolDefs
│   registry_enrich.go        # SetSkillsCatalog、enrichDefinitions
│   executor.go               # Executor 接口
│   execution_mode.go         # call_purpose、StartBackground（内部）
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
├── 后台 job_*
│   job_registry.go           # backgroundJobRegistry
│   tool_job.go               # background_job_status/cancel
│   job_registry_test.go
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
| fs、bash、trigger CRUD、后台 job | `load_skills` / `unload_skills` / `clear_skills` |
| | `ask_user_information` |
| | 子 Agent 管理类（registry 为 stub） |

---

## 阶段 B（未做）

子 package + `Register(r *Registry)`；待阶段 A 稳定后再开。

---

## 执行模式

- **同步（默认）**：orchestrator 调用 `Execute`；`read_file` / `write_file` / `trigger_create` 等始终同步完成。
- **`bash_run` 显式 timeout**：同步等待 `timeout_seconds`；超时后登记后台 job、返回 `RUNNING job_id=...`；完成后 **`async_tool_result` 自动回灌**。
- **`bash_run` 省略 timeout**：硬上限（默认 600s）到期杀进程并返回 ERROR（不转后台）；UI 可提前终止或转后台。
- **写盘信任链**：`write_file` / `search_replace` 为 `rule` 时，同 session Agent 自建文件在 mtime 未变前提下后续写操作可免 HITL（`node/internal/hooks`，见 [ux-agent-owned-file-approval.md](../../docs/design/ux-agent-owned-file-approval.md)）。
- **内部 `StartBackground`**：不在 tool schema 暴露；`ParseToolCallArguments` 仍兼容剥离历史 `run_in_background` 字段。

触发器 condition 语义见 [`../triggers/README.md`](../triggers/README.md).
