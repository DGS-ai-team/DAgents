"""工作组 @成员 提及：剥离喂模型文案、校验 direct_member_id。"""

from __future__ import annotations

import re
from typing import Any

from manage.workgroup.errors import WorkgroupError


def strip_member_mention(text: str, *, display_name: str) -> str:
    """从用户可见文案中去掉 @{display_name}，供 Member instruction 使用。"""
    name = (display_name or "").strip()
    raw = str(text or "")
    if not name:
        return raw.strip()
    token = f"@{name}"
    # 仅去掉精确匹配的 token（选择器写入，避免误伤正文 @）
    out = re.sub(rf"(?<!\S){re.escape(token)}(?!\S)", " ", raw)
    return re.sub(r"[ \t]+", " ", out).strip()


def resolve_direct_member(
    store: Any,
    workgroup_id: str,
    *,
    direct_member_id: str | None,
    timeline_text: str,
) -> tuple[Any | None, str]:
    """若 direct_member_id 非空则校验并返回 (member, instruction)；否则 (None, text)。

    instruction 已剥离 @显示名。有 @ 意图但 id 无效 → 409/404。
    """
    mid = (direct_member_id or "").strip()
    text = str(timeline_text or "").strip()
    if not mid:
        return None, text

    member = store.get_member(mid)
    if member is None or member.workgroup_id != workgroup_id:
        raise WorkgroupError("not_found", "direct member not found", http_status=404)
    if member.status != "ready":
        raise WorkgroupError(
            "conflict",
            "direct member not ready",
            http_status=409,
            details={"member_id": mid, "status": member.status},
        )
    display = (member.display_name or "").strip() or mid
    # Timeline 文案应含 @显示名（UI 保证）；若缺失仍允许，instruction=全文
    instruction = strip_member_mention(text, display_name=display)
    if not instruction:
        raise WorkgroupError(
            "invalid_request",
            "direct message has empty instruction after stripping @mention",
            http_status=400,
        )
    return member, instruction
