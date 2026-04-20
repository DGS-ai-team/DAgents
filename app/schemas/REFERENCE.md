# `app/schemas/` REFERENCE

## `approval.py`

- **`ToolCallApprovalItem`**：审批条目中单条工具（`id` / `name` / `arguments` / `raw_arguments`）
- **`ApprovalToolCallsArgs`**：`payload.args`，含 **`tool_calls`**
- **`ApprovalRequiredEnvelopePayload`**：与 `AgentEventEnvelope(event_type=approval_required)` 的 `payload` 一致（`approval_type` / `message` / `args` / `description`）
- **`ApprovalRequiredSseData`**：经 `AgentService._map_event_envelope_to_stream` 映射后的 SSE `data` 内嵌结构
- **`ResumeToolApprove`**、**`ResumeToolReject`**、**`ResumeToolUnion`**：`resume_value` 判别式联合类型
- **`parse_resume_tool_decision`**、**`is_tool_execution_approved`**：将任意 `resume_value` 规范为 approve/reject（失败视为 reject）

## `agent_peer.py`

- **`AgentPeerCaller`**：调用方字段（`agent_id/session_id/discovery_groups`）
- **`AgentPeerTarget`**：目标字段（`agent_id` 与 `discovery_groups` 互斥二选一）
- **`AgentPeerPayload`**：业务载荷（`content_type/content`）
- **`AgentPeerTask`**：任务信息（`task_id/state/artifact_refs`）
- **`AgentPeerError`**：错误结构（`code/message/retryable`）
- **`AgentPeerEnvelope`**：统一协议信封（`protocol_version/trace_id/...`）
- **`build_agent_peer_envelope`**：构建标准信封并自动补全追踪字段
- **`parse_agent_peer_envelope_from_text`**：从文本解析 AgentPeer JSON 信封

## `__init__.py`

- 聚合导出上述公共符号
