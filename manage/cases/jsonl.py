"""Node history JSONL 解析与导出。"""

from __future__ import annotations

import json
import uuid
from typing import Any

from manage.cases.models import CaseMessage
from manage.cases.tool_resolve import filter_unlinked_tool_messages


def _content_to_str(content: Any) -> str:
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    return json.dumps(content, ensure_ascii=False)


def parse_jsonl_bytes(data: bytes) -> list[CaseMessage]:
    """解析 Node 原始 message journal JSONL。"""
    text = data.decode("utf-8-sig")
    out: list[CaseMessage] = []
    for line_no, line in enumerate(text.splitlines(), start=1):
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError as exc:
            raise ValueError(f"invalid JSON on line {line_no}") from exc
        if not isinstance(obj, dict):
            raise ValueError(f"line {line_no}: expected JSON object")
        recorded_at = str(obj.get("recorded_at") or "")
        message = obj.get("message")
        if message is None and "role" in obj:
            message = obj
        if not isinstance(message, dict):
            raise ValueError(f"line {line_no}: missing message object")
        role = str(message.get("role") or "user")
        content = _content_to_str(message.get("content"))
        out.append(
            CaseMessage(
                id=str(uuid.uuid4()),
                recorded_at=recorded_at,
                role=role,
                content=content,
                raw=message,
            )
        )
    return filter_unlinked_tool_messages(out)


def export_jsonl_bytes(messages: list[CaseMessage]) -> bytes:
    """将案例消息导出为 Node history JSONL。"""
    lines: list[str] = []
    for msg in messages:
        if msg.raw is not None:
            payload = {"recorded_at": msg.recorded_at, "message": msg.raw}
        else:
            payload = {
                "recorded_at": msg.recorded_at,
                "message": {"role": msg.role, "content": msg.content},
            }
        lines.append(json.dumps(payload, ensure_ascii=False))
    return ("\n".join(lines) + ("\n" if lines else "")).encode("utf-8")
