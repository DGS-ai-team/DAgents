# Issue 001：Python TUI — `search_replace` HITL 审批 UI 不出现

| 字段 | 值 |
|------|-----|
| **状态** | **Open**（排查暂停，后续继续） |
| **GitHub** | [#39](https://github.com/DGS-ai-team/DAgents/issues/39) |
| **组件** | Python Textual TUI（`app/cli/tui/`） |
| **影响** | 用户无法点击「同意/不同意」；工具块黄点 pending 计时直至 `wait_user_turn` timeout（默认 300s） |
| **Server 侧** | 行为符合预期（HITL 暂停，等 Client `POST resume`） |
| **可复现性** | **高** — 相同工具参数再调一次，现象一致（确定性 Client 路径，非偶发 SSE） |
| **记录日期** | 2026-06-21 |

---

## 1. 现象（现场）

- 工具：`search_replace`（实测 arguments ~3.2KB，含中文 path、SQL 片段；JSON 合法）
- TUI 展示：
  - 工具块 **黄点 pending** + 计时（如 76s、直至 timeout）
  - 代码框内为 **整段 growing raw JSON**（流式 partial 未闭合时）
  - **无**青色「同意 / 不同意」审批 UI
- 预期：走 **HITL 审批**，或命中 **AgentOwnedFile 信任链** 后免审批直接执行
- Server 诊断（`/status` 或 `GET /context`）：
  - `turn_state=idle`
  - `pending_tool_calls_count=1`
  - `queue_pending=0`
- 模型用 **相同参数再次调用** 同一工具 → 现象 **完全一致**

---

## 2. 已排除项

| 假设 | 结论 |
|------|------|
| 参数过长导致 SSE/JSON 损坏 | ❌ ~3KB，非 MB 级；`expand_hitl_required`、SSE 解析脚本均通过 |
| Go Client 1MB SSE 行限制 | ❌ 栈为 Python TUI |
| 特殊字符（中文、`\u003e`、SQL 引号）阻断 HITL 数据路径 | ❌ 脚本实测不阻断 |
| Node 未进入 HITL / 策略错误 | ❌ `pending=1` + `idle` = 正常 HITL 暂停 |
| 信用链应免审但未命中 | 与「卡住」无关；`pending=1` 说明 **未 auto 执行**，走审批路径 |

---

## 3. 根因假设（待最终确认）

**倾向：Python TUI 状态机 + Textual `call_later` 队列顺序，而非 Node。**

```text
SSE render loop 快速入队大量 tool_call partial → call_later(_apply_transcript)
hitl_required 入队 → _process_hitl_queue → _hitl_busy=True
旧路径：call_later(_begin_approval_ui) 排在 partial 更新之后 → await ready 长期挂起
→ 审批 UI 永不出现；_hitl_busy 可能永久为 True → 后续 HITL 被跳过
→ wait_user_turn 等 done，300s timeout
```

辅助因素：

- `search_replace` 流式 partial 在 JSON 未闭合时走 `streaming_tool_call_preview` → `{}, raw, "json"`，TUI 展示 **全文 raw JSON**，加重 UI 队列与 Rich 渲染负担（见 `app/cli/tool_calls_streaming.py`）。

相关代码：

| 文件 | 要点 |
|------|------|
| `app/cli/tui/app.py` | `_process_hitl_queue`、`_start_approval_hitl`、`_hitl_busy`、`_abort_local_hitl_for_user_message` |
| `app/cli/session_controller.py` | `_render_loop` 串行消费 SSE；`hitl_required` 排在 partial `tool_call` 之后 |
| `app/cli/tool_calls_streaming.py` | `search_replace` partial fallback |
| `node/internal/turn/tool_router.go` | 先 `publishToolCall`，再决策，再 `publishHITLRequired` |

---

## 4. 为何脚本「复现不出来」

`scripts/test_python_hitl_large_args.py` / `tests/test_cli_hitl_large_args.py` 覆盖：

- SSE 编解码、`expand_hitl_required`、Controller 入队、Rich 片段

**未覆盖**：

- 完整 Textual App + `call_later` FIFO
- `_hitl_busy` 与 `await ready` 挂起
- 大量 partial `tool_call` 与 HITL 启动 **抢同一 UI 队列**

用户侧 **100% 可复现** → 更像确定性 TUI bug，而非脚本所测的数据路径问题。

---

## 5. 拷贝 `sessions.db` 能否续聊复现

**仅能部分恢复，不能直接复现 TUI bug。**

- 路径：`<runtime>/memory/sessions.db`（`RuntimeState.pending` 含 HITL 快照）
- **能恢复**：服务端对话上下文、`pending_tool_calls_count=1`（若在 HITL 暂停时拷贝且未被新消息打断）
- **不能恢复**：SSE Hub 历史（Node 重启后为空）、TUI transcript / 本地 HITL 队列
- TUI 默认 `live=1` → **不重放** `hitl_required`；接回去 **不会** 自动再走一遍审批 UI 路径
- TUI **再发用户消息** → Server `InterruptPending`，**清掉** pending，开始新 turn

详见排查讨论记录；后续复现应靠 **新 turn 触发相同 `search_replace`**，而非仅恢复 DB。

---

## 6. 临时解卡（用户侧）

```bash
curl -X POST "$API/v1/messages" -H 'Content-Type: application/json' -d '{
  "session_id": "<session_id>",
  "request_type": "resume",
  "resume_value": {
    "type": "selection",
    "approved": ["<call_id>"],
    "rejected": []
  }
}'
```

---

## 7. 已尝试改动（**未验证 / 待回归**）

排查过程中在 working tree 中有 **未关闭 issue 前的局部修复**，方向包括：

1. `_process_hitl_queue` 在 UI 线程 **同步** `_begin_approval_ui`，去掉 `call_later(_begin)` + `await ready`
2. `_abort_local_hitl_for_user_message` / send timeout 时 **cancel `_hitl_task`**
3. `search_replace` 流式 partial **不展示 raw JSON 全文**（仅 path 摘要）

**Issue 仍保持 Open**：需在真实 Textual + 用户 payload 下回归确认；并补集成级测试。

---

## 8. 后续排查清单

- [ ] 在用户环境用 **修 fix 后** TUI 复测同一 `search_replace` payload
- [ ] 若仍失败：TUI 日志是否有 `approval ui failed` / `send failed`；Node 是否收到 `resume`
- [ ] Textual 集成测试：mock 大量 partial `tool_call` + `hitl_required`，断言审批块出现
- [ ] 评估 `hitl_required` 相对 partial 的 **优先级**（Controller 插队或 debounce FS 工具渲染）
- [ ] 评估 Node 侧 partial `tool_call` 是否应对 `search_replace` 省略完整 `arguments`（可选）
- [ ] 对比 Go TUI 同场景是否正常（隔离 Python 栈特有问题）
- [ ] 关闭 issue 后更新本文件状态，并同步 GitHub Issue（若有）

---

## 9. 相关测试与脚本

| 路径 | 用途 |
|------|------|
| `scripts/test_python_hitl_large_args.py` | 分层诊断（SSE / HITL 展开 / 入队） |
| `tests/test_cli_hitl_large_args.py` | 大参数 HITL 路径单测 |
| `tests/test_tool_call_streaming.py` | 流式 preview（含 `search_replace`） |

---

## 10. GitHub Issue 模板（可选粘贴）

**Title:** `Python TUI: search_replace HITL approval UI stuck (pending until timeout)`

**Labels:** `bug`, `cli`, `hitl`

**Body:** 见上文 §1–§8。
