# node/internal/tools

N3 在 Node 进程内本地执行；面向模型的 tool schema **均为同步调用**（仅 `call_purpose` 通用参数）。**`bash_run`** 在 `timeout_seconds` 内未完成时由 Node **自动降级**为后台 job；内部仍保留 `StartBackground` / `job_registry` 供降级与测试使用。

**配置**：`tools.enabled_groups`（7 组）见 [`docs/built-in-tools.md`](../../docs/built-in-tools.md) §0、[`shared/config/README.md`](../../shared/config/README.md)。  
**工具用法**：写在各 tool schema `description` 中（[`descriptions_shared.go`](./descriptions_shared.go) + 各 `tool_*` / `fs_*` / `bash_*` 文件）。

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
│   descriptions_shared.go    # 各工具 description 共用常量
│   executor.go               # Executor 接口
│   execution_mode.go         # call_purpose、StartBackground（内部）
│   tools_test.go             # registry 集成 smoke
├── 文件 fs_*
│   fs_read.go / fs_write.go / fs_search_replace.go
│   fs_glob_tool.go / fs_glob_internal.go
│   fs_grep_file.go / fs_grep_files.go / fs_grep_shared.go
│   fs_helpers.go / fs_encoding.go
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
│   tool_a2a.go / tool_childagent.go
│   tool_*_test.go
└── README.md / REFERENCE.md
```

---

## 执行边界

| 在 Registry 执行 | 在 turn 编排器执行 |
|------------------|-------------------|
| fs、bash、trigger CRUD、A2A HTTP、后台 job | `load_skills` / `unload_skills` / `clear_skills` |
| | `ask_user_information` |
| | 子 Agent 管理类（registry 为 stub） |

---

## 阶段 B（未做）

子 package + `Register(r *Registry)`，见上文「阶段 B」历史说明；待阶段 A 稳定后再开。

---

## 执行模式

- **同步（默认）**：orchestrator 调用 `Execute`；`read_file` / `write_file` / `trigger_create` 等始终同步完成。
- **`bash_run` 超时降级**：同步等待 `timeout_seconds`（默认 30）；超时后登记后台 job、返回 `RUNNING job_id=...`；完成后 **`async_tool_result` 自动回灌**。
- **内部 `StartBackground`**：不在 tool schema 暴露；`ParseToolCallArguments` 仍兼容剥离历史 `run_in_background` 字段。

触发器 condition 语义见 [`../triggers/README.md`](../triggers/README.md).
