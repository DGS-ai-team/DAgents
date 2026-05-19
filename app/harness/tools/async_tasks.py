"""异步工具任务查询与取消工具。"""

from __future__ import annotations

import json

from app.harness.tools.async_store import get_async_tool_result_store
from app.harness.tools.tool import tool


@tool("async_tool_status")
def async_tool_status(job_id: str) -> str:
    """使用场景：查询异步工具任务状态，包括后台化 shell 的 `async_job_id`。

    字段说明：
    - job_id: `AsyncToolResultStore` 返回的任务 ID。

    返回说明：
    - 成功：返回 JSON 字符串，含 job_id/tool_name/status/result/error/时间戳。
    - 失败：返回 `ERROR: ...`。

    调用范例：
    - `async_tool_status({"job_id":"..."})`
    """
    job = get_async_tool_result_store().get_job(str(job_id or "").strip())
    if job is None:
        return f"ERROR: 未找到异步工具任务：{job_id!r}"
    return json.dumps(
        {
            "job_id": job.job_id,
            "session_id": job.session_id,
            "client_id": job.client_id,
            "tool_name": job.tool_name,
            "status": job.status,
            "result_text": job.result_text,
            "error_text": job.error_text,
            "submitted_at_unix_ms": job.submitted_at_unix_ms,
            "started_at_unix_ms": job.started_at_unix_ms,
            "finished_at_unix_ms": job.finished_at_unix_ms,
        },
        ensure_ascii=False,
    )


@tool("async_tool_cancel")
def async_tool_cancel(job_id: str) -> str:
    """使用场景：取消仍在运行的异步工具任务。

    字段说明：
    - job_id: `AsyncToolResultStore` 返回的任务 ID。

    返回说明：
    - 成功：返回 JSON 字符串，含取消后的状态。
    - 失败：返回 `ERROR: ...`。

    调用范例：
    - `async_tool_cancel({"job_id":"..."})`
    """
    job = get_async_tool_result_store().cancel_job(str(job_id or "").strip())
    if job is None:
        return f"ERROR: 未找到异步工具任务：{job_id!r}"
    return json.dumps(
        {
            "job_id": job.job_id,
            "tool_name": job.tool_name,
            "status": job.status,
            "error_text": job.error_text,
        },
        ensure_ascii=False,
    )
