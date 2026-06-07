# shell 策略目录

每个 shell 使用一份审批策略文件：

- `bash.approval.txt` — Linux/macOS/WSL 下 `bash_run`（默认 bash）
- `cmd.approval.txt` — Windows `cmd.exe`
- `powershell.approval.txt` — Windows PowerShell

## 规则格式

每行一条 **`命令首词=审批模式`**（首词小写匹配；管道/分号拆成的**每一段**各自匹配）：

| 模式 | 含义 |
|------|------|
| **`never`** | 免审批（适合只读：`ls`、`cat`、`get-childitem` 等） |
| **`always`** | 必须审批（写删、网络、提权、解释器、`git`、`curl` 等） |
| **`rule`** | 未细分时与 **`always` 相同，仍需审批** |

空行与 **`#`** 注释行忽略；未列出的首词默认为 **`rule`**。

## 与工具级策略的关系

- **`../tool.approval.txt`**：`bash_run=rule` 时才会读本目录；其它工具不受此处约束。
- 全局 **`AGENT_TOOL_APPROVAL_MODE=always/never`** 会覆盖文件策略。

## 维护注意

- 仅匹配**第一个 token**（`sudo rm` 命中 `sudo`，不会单独匹配 `rm`）。
- `git status` 与 `git push` 均命中 `git`；已将 **`git=always`**，避免误放行推送。
- `find` 在 bash 中设为 **`rule`**（含 `-delete`/`-exec` 等危险用法）。
