# Issue 003：终端 TUI — 工具参数过长时 HITL 审批卡片/面板不出现（Web UI 正常）

| 字段 | 值 |
|------|-----|
| **状态** | **Open**（现象已确认，根因排查中） |
| **GitHub** | [#40](https://github.com/DGS-ai-team/DAgents/issues/40) |
| **组件** | **终端 Client**（Python Textual TUI `dagents chat`、Go bubbletea TUI `dagents-client tui` / `--plain`） |
| **对比** | **Node Web UI**（`/ui/`）同场景 **可正常** 弹出内联审批 |
| **影响** | 用户无法点击批准/拒绝；工具块可能长时间 **pending**（黄点 + 计时），直至 `wait_user_turn` timeout 或 Esc 取消 |
| **Server 侧** | 通常行为符合预期（`turn_state=idle`、`pending_tool_calls_count≥1`，等待 Client `POST resume`） |
| **记录日期** | 2026-06-25 |

---

## 1. 现象（用户报告）

- **触发条件**：模型为需审批工具（如 `write_file`、`search_replace`、`bash_run` 等）生成 **较长 arguments**（常见 KB 级 JSON；具体阈值待测）。
- **终端 TUI**：
  - SSE 流中可见 **tool_call**（常为 growing raw JSON 或 pending 工具块）。
  - **不出现** HITL 审批交互：
    - Python：RichLog 内无青色「同意 / 不同意」审批块，或审批 TextArea 未切换。
    - Go full TUI：viewport 未进入 `modeApproval`，无审批面板。
  - 工具块保持 **pending**，计时递增，直至超时或用户 Esc。
- **Web UI**（同 Node、同 session、同类工具）：**正常** 展示 HITL 气泡/内联审批，可批准并继续 turn。
- **预期**：终端与 Web UI 均应在 `hitl_required` / `approval_required` 后展示审批 UI，并 `POST resume`。

---

## 2. 与 Issue 001 的关系

| 项 | Issue 001 ([#39](https://github.com/DGS-ai-team/DAgents/issues/39)) | 本 Issue ([#40](https://github.com/DGS-ai-team/DAgents/issues/40)) |
|----|-----------|----------|
| 范围 | Python TUI + **`search_replace`** 为主 | **终端 TUI 通用**（参数过长） |
| Web UI | 未强调对比 | **明确 Web UI 正常** |
| Go TUI | 清单项「待对比」 | 纳入同一现象 |
| 根因假设 | Textual `call_later` 队列 + partial 洪水 | 可能 **共享**（流式 partial 与 HITL 启动竞态）；亦可能有 **Client 栈差异** |

若最终证实仅为 Python 栈问题，可将本 Issue **合并入 001** 并关闭；当前先独立记录「**TUI vs Web UI**」对比与 **参数长度** 触发条件。

---

## 3. 已排除 / 部分排除（脚本层）

`scripts/test_python_hitl_large_args.py` / `tests/test_cli_hitl_large_args.py` 在 **非 UI** 路径下通过：

| 阶段 | 说明 |
|------|------|
| SSE 编解码 | 大 JSON 单行（含 MB 级探测） |
| `expand_hitl_required` / `extract_tool_approval_requests` | HITL 载荷解析 |
| `SessionController._handle_stream_event` | 入队 `PendingHITL` |
| Rich Syntax 渲染耗时 | 单独阶段可测 |

**未覆盖**（与 Issue 001 相同缺口）：

- 完整 Textual / bubbletea App 事件循环
- 大量 **partial `tool_call`** 与 **HITL 展示** 同一 UI 队列的先后顺序
- Go TUI `showNextHITLIfIdle` 在 partial 渲染阻塞主线程时的行为

→ 用户现场 **100% 可复现**、脚本 **复现不了** → 倾向 **Client UI 层**，而非 Node SSE 或 HITL 数据损坏。

---

## 4. 根因假设（待验证）

### 4.1 Python Textual TUI

```text
SSE render loop 快速入队大量 tool_call partial
  → call_later(_apply_transcript) 占满 UI 队列
hitl_required 入队 → _process_hitl_queue → _hitl_busy=True
审批 UI 启动 (_begin_approval_ui / RichLog 审批块) 被推迟或 await 挂起
  → 审批卡片永不出现；_hitl_busy 长期 True → 后续 HITL 被跳过
```

相关代码：

| 文件 | 要点 |
|------|------|
| `app/cli/tui/app.py` | `_process_hitl_queue`、`_begin_approval_ui`、`_hitl_busy` |
| `app/cli/session_controller.py` | `_render_loop` 串行消费 SSE |
| `app/cli/tool_calls_streaming.py` | partial 时 raw JSON 全文展示，加重渲染 |
| `node/internal/turn/tool_router.go` | 先 `publishToolCall`，再策略/HITL |

### 4.2 Go bubbletea TUI

```text
partial tool_call → UpsertToolCallLines / viewport 刷新
hitl_required → enqueueHITLQueue → pendingHITLChangedMsg → showNextHITLIfIdle
若 Update 循环被大量 transcript 刷新占用，审批 mode 切换延迟或丢失（待测）
```

相关代码：

| 文件 | 要点 |
|------|------|
| `client/internal/tui/full/stream_events.go` | `tool_call` 与 `hitl_required` 分支 |
| `client/internal/tui/full/hitl_queue.go` | `showNextHITLIfIdle`、`modeApproval` |
| `client/internal/tui/shared/tool_call_stream.go` | partial upsert |

### 4.3 为何 Web UI 正常

| 维度 | Web UI | 终端 TUI |
|------|--------|----------|
| HITL 展示 | Vue 组件内联，`hitlStore` 独立状态 | RichLog / bubbletea viewport，与 transcript 共享 UI 线程 |
| partial 工具 | `transcript.js` 可跳过/摘要化 | 常展示 growing JSON 全文 |
| 队列 | 浏览器事件循环 + 响应式更新 | Textual `call_later` FIFO / bubbletea `Update` |

---

## 5. 复现建议

1. 启动 Node（`policy` 对目标工具为 `rule`/`always`）。
2. 触发需审批且 **arguments 较大** 的工具调用（如大段 `write_file` / `search_replace`）。
3. 分别用 **`dagents chat`**、**`dagents-client tui`**、浏览器 **`/ui/`** 观察。
4. 记录：`/status` 或 `GET /v1/sessions/{id}/context` 中 `pending_tool_calls_count`、`turn_state`。
5. 可选：对照 `scripts/test_python_hitl_large_args.py --sizes ...` 确认数据路径无损。

---

## 6. 临时解卡（与 Issue 001 相同）

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

## 7. 排查清单

- [ ] 确认触发工具类型与 **arguments 字节数** 阈值（Python / Go 分别记录）
- [ ] Go TUI 同 payload 是否复现（隔离 Python 特有问题）
- [ ] TUI 日志：Python `approval ui failed`；Go 是否有 `hitlSubmitResultMsg` 错误
- [ ] Node 是否在 HITL 暂停时仍持续发送 **partial tool_call**（加重 Client 队列）
- [ ] 评估 partial `tool_call` **debounce** 或 HITL **插队**（优先于 FS 工具全文渲染）
- [ ] Textual / bubbletea **集成测试**：mock 大量 partial + `hitl_required`，断言审批 UI 出现
- [ ] 与 Web UI `hitl.js` / `transcript.js` 行为对齐（参数摘要而非全文）

---

## 8. 相关文档与测试

| 路径 | 用途 |
|------|------|
| [001](./001-python-tui-hitl-approval-ui-stuck.md) | Python `search_replace` 专项，排查细节 |
| [002](./002-webui-tool-display-and-approval.md) | Web UI HITL（不同 bug，作对比参考） |
| `scripts/test_python_hitl_large_args.py` | 大参数分层诊断 |
| `tests/test_cli_hitl_large_args.py` | 单测 |
| `CHANGELOG.md` [0.5.1] | Issue 001 已列入 Open Issue |

---

## 9. GitHub Issue 模板（可选粘贴）

**Title:** `TUI: HITL approval UI missing when tool arguments are large (Web UI OK)`

**Labels:** `bug`, `cli`, `hitl`

**Body:** 见上文 §1–§7。
