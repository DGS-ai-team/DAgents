# `app/schemas/`

跨模块 **Pydantic** 契约（与 `app.context.models` 会话上下文实体区分）：审批事件、`resume` 决策等。

| 文件 | 说明 |
|------|------|
| **`approval.py`** | 工具审批出站载荷、SSE 扁平 `data`、`ResumeToolApprove` / `ResumeToolReject` 及解析辅助函数 |
| **`agent_peer.py`** | Agent 间交互统一信封（caller/target/payload/task/error）与构建/解析辅助函数 |
| **`__init__.py`** | 对外 re-export |
