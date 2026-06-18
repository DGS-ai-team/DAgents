# full — bubbletea 全屏 TUI

| 文件 | 用途 |
|------|------|
| [`model.go`](model.go) | bubbletea Model：viewport、输入框、SSE/HITL、布局 |
| [`commands.go`](commands.go) | 斜杠命令、SSE 重连循环 |
| [`policy_view.go`](policy_view.go) | `/policy` 全屏策略管理（工具 / shell 三档） |
| [`status_lines.go`](status_lines.go) | prefilling/thinking/compression 等待态展示 |
| [`context_view_test.go`](context_view_test.go) | `/context` viewport 滚动单测 |

## 交互（键盘优先，Linux/SSH）

- **上**：viewport 滚动 transcript；贴底时自动跟随，上滚后不拽回底部
- **下**：textarea；Enter 发送，Shift+Enter 换行
- **PgUp/PgDn**：滚动 transcript 或 `/context` 视图
- **↑/↓**（输入框为空）：滚动 transcript；`/context` 下始终滚动
- **o / c**：展开/收起最近一条 tool 结果（或 `/tools expand` / `/tools collapse`）
- tool 执行中：pending 行每秒刷新耗时（如 `▶ 调用 bash(…) … 3s`）
- **Esc**：取消 turn；context / policy 返回聊天
- **`/context`**：全屏可滚动 context 摘要（隐藏输入区）
- **`/policy`**：Tab 切页 · `1/2/3` 改档位 · Enter 应用 · `[`/`]` 切换 shell
- **审批**：↑/↓ · Space · Enter · Y/N/Esc
- **`/quit`**：退出并打印 `--session` 恢复命令

## 依赖

- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`（viewport、textarea）
