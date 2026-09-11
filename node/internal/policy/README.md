# 本地策略（Agent Node N4）

| 文件 | 说明 |
|------|------|
| `engine.go` | `DecideTool`：工具 + bash shell 细粒度审批（含 `deny` 硬拒绝） |
| `store.go` | 策略快照、`ApplyToolUpdates` / `ApplyShellUpdates` 原子写盘 |
| `maps.go` | 当前版本内置策略种子与默认引擎构造 |
| `file_loader.go` | 读取策略目录，供测试和本地策略检查使用 |
| `entry_file.go` | 解析 `key=mode` txt 策略（always / never / rule / deny） |
| `shell_parse.go` | bash/cmd/powershell 命令拆分与 root command 提取 |
| `mode.go` | 审批模式常量 |

策略文件布局（与 Python v1 对齐）：

- `<runtime>/policy/tool.approval.txt` — 工具级：`tool_name=mode`
- `<runtime>/policy/shell/bash.approval.txt` — bash 首词：`command=mode`
- `<runtime>/policy/shell/cmd.approval.txt`
- `<runtime>/policy/shell/powershell.approval.txt`

**mode 语义**

| txt mode | 编排结果 | API decision |
|----------|----------|--------------|
| `never` | 免审批自动执行 | `allow_auto` |
| `always` / `rule` | 需审批（未命中 shell 条目默认 `rule`） | `require_approval` |
| `deny` | 硬拒绝（`policy_denied`） | `deny` |

`bash_run=rule` 时按 shell 策略逐段判定；任一段 `deny` 优先于审批。文件显式配置优先于 `engine.go` 内置 fallback（仅 `toolMode==rule` 时生效）。

**写盘信任链**：`write_file` / `search_replace` 为 **`rule`** 时，`node/internal/hooks` 的 `AgentOwnedFileHook` 可对 session 内 Agent 自建且 mtime 未变的 path 将 `require_approval` 降为 `auto`；**`always` 档位不经过信任链**。种子默认 `write_file=rule`（见 `packaging/runtime/policy/tool.approval.txt`）。设计：[ux-agent-owned-file-approval.md](../../../docs/design/ux-agent-owned-file-approval.md)。

**HTTP**：`GET/PUT /v1/agents/{agent_id}/policy*`（见 [`docs/architecture/agent-node-api.md`](../../../docs/architecture/agent-node-api.md) §2.3）。全局 `/v1/policy*` 已移除。写盘前滚动备份 `*.bak`；`ask_user_information` 不可设为 `deny`。

种子见 [`packaging/runtime/policy`](../../../packaging/runtime/policy)。
