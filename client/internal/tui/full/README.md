# full — bubbletea 全屏 TUI

| 文件 | 用途 |
|------|------|
| [`model.go`](model.go) | bubbletea Model：viewport、输入框、SSE/HITL、布局 |
| [`commands.go`](commands.go) | 斜杠命令、SSE 重连循环 |
| [`policy_view.go`](policy_view.go) | `/policy` 全屏策略管理（工具 / shell 三档） |

## 交互

- **上**：viewport 滚动 transcript（SSE 流式 assistant、tool、系统行）；**贴底时**自动跟随新输出，上滚后不会被流式刷新拽回底部（鼠标滚轮可滚动；发消息后仍会滚到底）
- **下**：textarea 输入；Enter 发送，Shift+Enter 换行
- **Esc**：取消在途 turn（chat 模式）；context / policy 视图返回
- **`/policy`**：全屏策略管理 · Tab 切工具/Shell · `1/2/3` 改档位 · Enter 提交 · `[`/`]` 切换 shell 类型
- **审批**：↑/↓ 移动 · Space 勾选 · Enter 确认 · Y 全批准 · N/Esc 全拒绝
- **询问（有选项）**：↑/↓ · Space · Enter；无选项时在输入框文本回答

## 依赖

- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`（viewport、textarea）
