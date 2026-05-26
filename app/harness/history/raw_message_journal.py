"""原始 OpenAI 消息按条追加记录：按会话、按自然日写入 JSONL。"""

from __future__ import annotations

import copy
import json
import logging
import re
from datetime import datetime
from pathlib import Path
from typing import Any

from app.config.runtime_layout import raw_message_history_dir
from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext
from app.context.openai_messages import normalize_openai_message_for_context

logger = logging.getLogger(__name__)

_MAX_SESSION_FILENAME_PART_LEN = 200


def _sanitize_session_id_for_filename(session_id: str) -> str:
    """将会话 ID 转为可做文件名片段的安全字符串。

    逻辑：
    1. 去首尾空白；空则回退 `unknown_session`；
    2. 将非「字母数字下划线点横线」替换为 `_`，避免路径注入；
    3. 截断长度，降低极端长 ID 带来的路径问题。

    关键分支：
    - 替换后仍为空（例如全是非法字符）时回退为 `session`。

    副作用说明：
    - 无 IO。
    """
    raw = (session_id or "").strip()
    if not raw:
        return "unknown_session"
    safe = re.sub(r"[^\w.\-]+", "_", raw, flags=re.UNICODE)
    collapsed = safe.strip("._-")
    if not collapsed:
        return "session"
    return safe[:_MAX_SESSION_FILENAME_PART_LEN]


def _journal_file_path(session_id: str) -> Path:
    """解析当日 JSONL 记录文件路径（`{safe_sid}_{YYYYMMDD}.jsonl`）。

    逻辑：
    1. 基础目录固定为 **`<运行根>/.runtime/history`**（**`raw_message_history_dir()`**）；
    2. 文件名使用本地日期的 **`%Y%m%d`**（与「按日滚动」语义一致）。

    关键分支/边界：
    - 与 **`runtime_layout`** 常量一致；单测可 **`patch`** **`resolve_runtime_root`** 将目录隔离到临时根。
    """
    day = datetime.now().strftime("%Y%m%d")
    safe_sid = _sanitize_session_id_for_filename(session_id)
    base = raw_message_history_dir()
    return base / f"{safe_sid}_{day}.jsonl"


def record_raw_openai_message_append(session_id: str, message: dict[str, Any]) -> None:
    """将一条「插入瞬间」的消息快照追加写入 JSONL。

    逻辑：
    1. 配置关闭或 **`session_id`** 为空则直接返回；
    2. 构造 **`recorded_at`**（本地时区 ISO8601，毫秒精度）与 **`message`**；
    3. 确保父目录存在后 **`open(..., "a")`** 写一行 JSON。

    关键分支：
    - 写入与序列化失败仅 **`logging.warning`**，**不抛出**，避免影响对话主路径。

    与外部交互：
    - 本地文件系统追加写。

    Args:
        session_id: 与会话上下文绑定的 ID（与 sqlite / metrics 一致）。
        message: 已为 **deepcopy** 的快照（调用方负责），避免后续就地修改污染已落盘内容语义。
    """
    settings = get_settings()
    if not settings.agent_raw_message_history_enabled:
        return
    sid = (session_id or "").strip()
    if not sid:
        return
    path = _journal_file_path(sid)
    record = {
        "recorded_at": datetime.now().astimezone().isoformat(timespec="milliseconds"),
        "message": message,
    }
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        line = json.dumps(record, ensure_ascii=False, default=str) + "\n"
        with path.open("a", encoding="utf-8") as handle:
            handle.write(line)
    except OSError as exc:
        logger.warning("raw message journal write failed: %s path=%s", exc, path)
    except TypeError as exc:
        logger.warning("raw message journal serialize failed: %s path=%s", exc, path)


def append_openai_message_with_journal(ctx: OpenAIConversationContext, message: dict[str, Any]) -> None:
    """在 **`ctx.messages`** 末尾追加一条消息，并同步写入原始消息 JSONL 记录。

    逻辑：
    1. 先按 OpenAI/DeepSeek 协议规范化待写入消息；
    2. **`copy.deepcopy(normalized)`** 得到插入瞬间快照（与后续列表内字典原地改动解耦）；
    3. **`ctx.messages.append(normalized)`**；
    4. **`record_raw_openai_message_append`**。

    关键分支：
    - `assistant + tool_calls` 会统一保证 `reasoning_content` 字段存在；
    - JSONL 落盘失败在 **`record_*`** 内吞掉；列表已成功追加时不回滚。

    副作用说明：
    - 修改 **`ctx.messages`**；可能写本地 **`<运行根>/.runtime/history/`** 下 JSONL。
    """
    normalized = normalize_openai_message_for_context(existing_messages=ctx.messages, message=message)
    snapshot = copy.deepcopy(normalized)
    ctx.messages.append(normalized)
    record_raw_openai_message_append(ctx.session_id, snapshot)


def insert_openai_message_with_journal(ctx: OpenAIConversationContext, index: int, message: dict[str, Any]) -> None:
    """在 **`ctx.messages`** 指定下标插入一条消息，并同步写入原始消息 JSONL 记录。

    逻辑：
    1. 先按 OpenAI/DeepSeek 协议规范化待写入消息；
    2. **`deepcopy`** 快照；
    3. **`ctx.messages.insert(index, normalized)`**；
    4. **`record_raw_openai_message_append`**（按插入顺序追加到 JSONL，而非列表下标）。

    关键分支：
    - 异步工具回灌插入 `tool_callback` assistant 时，可从现有上下文继承最近 reasoning。

    副作用说明：
    - 修改 **`ctx.messages`**；可能写本地 JSONL 记录文件。
    """
    normalized = normalize_openai_message_for_context(existing_messages=ctx.messages, message=message)
    snapshot = copy.deepcopy(normalized)
    ctx.messages.insert(index, normalized)
    record_raw_openai_message_append(ctx.session_id, snapshot)
