"""将 browser-use AgentHistory 压缩为主 Agent 可消费的结构化结果。"""

from __future__ import annotations

from typing import Any


def _safe_call(obj: Any, name: str, default: Any = None) -> Any:
    fn = getattr(obj, name, None)
    if fn is None:
        return default
    try:
        return fn() if callable(fn) else fn
    except Exception:
        return default


def _clean_str_list(values: Any, *, limit: int = 30) -> list[str]:
    if not values:
        return []
    out: list[str] = []
    for v in values:
        if v is None:
            continue
        s = str(v).strip()
        if s:
            out.append(s)
        if len(out) >= limit:
            break
    return out


def summarize_agent_history(history: Any) -> dict[str, Any]:
    """从 AgentHistoryList（或兼容 mock）提取稳定字段。"""
    if history is None:
        return {
            "summary": None,
            "success": None,
            "done": False,
            "steps": 0,
            "urls": [],
            "screenshot_paths": [],
            "action_names": [],
            "errors": [],
            "has_errors": False,
            "duration_seconds": None,
        }

    final = _safe_call(history, "final_result")
    if final is not None:
        final = str(final).strip() or None

    success = _safe_call(history, "is_successful")
    done = _safe_call(history, "is_done")
    if done is None:
        done = success is not None

    steps = _safe_call(history, "number_of_steps", 0) or 0
    try:
        steps = int(steps)
    except Exception:
        steps = 0

    urls = _clean_str_list(_safe_call(history, "urls", []) or [])
    screenshots = _clean_str_list(_safe_call(history, "screenshot_paths", []) or [], limit=20)
    actions = _clean_str_list(_safe_call(history, "action_names", []) or [], limit=50)

    raw_errors = _safe_call(history, "errors", []) or []
    errors = _clean_str_list(raw_errors, limit=20)
    has_errors = bool(_safe_call(history, "has_errors", False))
    if not has_errors and errors:
        has_errors = True

    duration = _safe_call(history, "total_duration_seconds")
    try:
        duration = float(duration) if duration is not None else None
    except Exception:
        duration = None

    # 主 Agent 友好摘要：优先 done.text，否则拼接末段抽取内容
    summary = final
    if not summary:
        extracted = _safe_call(history, "extracted_content", []) or []
        pieces = _clean_str_list(extracted, limit=5)
        if pieces:
            summary = "\n".join(pieces)

    return {
        "summary": summary,
        "final_result": final,
        "success": success,
        "done": bool(done),
        "steps": steps,
        "urls": urls,
        "last_url": urls[-1] if urls else None,
        "screenshot_paths": screenshots,
        "last_screenshot_path": screenshots[-1] if screenshots else None,
        "action_names": actions,
        "errors": errors,
        "has_errors": has_errors,
        "duration_seconds": duration,
    }


def task_status_response(entry: dict[str, Any]) -> dict[str, Any]:
    """组装 task_status / 完成后的 HTTP 响应（含顶层 url/screenshot 便于 Go ToolResult）。"""
    result = entry.get("result") or {}
    status = entry.get("status")
    detail: dict[str, Any] = {
        "task_id": entry.get("task_id"),
        "session_key": entry.get("session_key"),
        "task": entry.get("task"),
        "status": status,
        "max_steps": entry.get("max_steps"),
        "created_at": entry.get("created_at"),
        "updated_at": entry.get("updated_at"),
        "error": entry.get("error"),
    }
    # 展开结构化结果，避免主 Agent 再钻一层 result
    if isinstance(result, dict):
        for key in (
            "summary",
            "final_result",
            "success",
            "done",
            "steps",
            "urls",
            "last_url",
            "screenshot_paths",
            "last_screenshot_path",
            "action_names",
            "errors",
            "has_errors",
            "duration_seconds",
        ):
            if key in result:
                detail[key] = result[key]
        if result.get("summary"):
            detail["extracted_content"] = result["summary"]

    out: dict[str, Any] = {"ok": True, "detail": detail}
    if result.get("last_url"):
        out["url"] = result["last_url"]
    if result.get("last_screenshot_path"):
        out["screenshot_path"] = result["last_screenshot_path"]
    if status == "failed" and entry.get("error"):
        out["ok"] = True  # 查询成功；任务失败体现在 detail.status/error
    return out
