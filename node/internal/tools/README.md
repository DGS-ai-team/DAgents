# node/internal/tools

N3 在 Node 进程内本地执行；工具均为**同步实现**，通过通用参数 **`run_in_background`** 选择串行或后台并行。

| 文件 | 说明 |
|------|------|
| `registry.go` | Tool registry、FS_ROOT 沙箱、`Definitions` / `Execute` |
| `execution_mode.go` | `run_in_background` 解析与 schema 注入 |
| `background_jobs.go` | 后台任务注册表、`StartBackground` |
| `background_job_tools.go` | `background_job_status` / `background_job_cancel` |
| `fs.go` | `read_file`、`write_file`、`search_replace` |
| `bash.go` / `bash_runner.go` / `bash_shell.go` / `bash_policy.go` | `bash_run`（bash/cmd/powershell、cwd、sudo/su 拦截、超时降级） |
| `triggers.go` | `trigger_list/get/create/update/delete/fire`（见下节） |

## 执行模式

- **`run_in_background: false`（默认）**：orchestrator 同步调用 `Execute`；`bash_run` 在 `timeout_seconds` 内未结束则自动降级为后台 job（`status=RUNNING job_id=...`），进程继续运行并在完成后回灌。
- **`run_in_background: true`**：立即返回 `[TOOL_BACKGROUND] job_id=...`；完成后自动入队回灌 `[TOOL_BACKGROUND_DONE]...`。

## 触发器工具（`triggers.go`）

`SetTriggerRuntime` 注入 store / scheduler 后可用。`trigger_create` / `trigger_update` 的 `condition` 支持：

| 类型 | 示例 |
|------|------|
| 周期 | `{"interval_seconds": 60}` |
| 单次 | `{"fire_at": 1730000000}` |
| 日历 | `{"schedule": {"kind": "daily", "hour": 9, "minute": 0}}` |
| 日历 + 门控 | 同上并加 `"cmd": "test -f /tmp/ready"`（仅 schedule 自动触发时执行） |

详见 [`../triggers/README.md`](../triggers/README.md)。
