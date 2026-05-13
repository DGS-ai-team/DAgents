"""会话消息等进程内/本地持久化能力（如 `SqliteMessageStore`）。"""

from __future__ import annotations

import json
import sqlite3
from pathlib import Path
from typing import Any

from app.context.models import (
    ConversationContext,
    MessageRecord,
    RunTurnPhase,
)


class SqliteMessageStore:
    """按 `session_id` 维护会话内容的 SQLite 存储（每会话一行 `content` BLOB）。

    逻辑：
    1. 初始化时确保数据库与 `session_history` 表存在（不做旧版列/格式迁移）；
    2. `content` 为 UTF-8 JSON 对象：`{"history","openai_messages","pending_tool_calls","run_turn_phase","messages_total_tokens","tool_loop_count"}`；
    3. `append_message` 事务内读改写：仅追加 `history`，其余上下文字段保持原值；
    4. `save_conversation_content` 整包覆盖写入（含主流程与 summary 压缩常驻字段）；
    5. `list_messages` / `load_conversation_content` 单次读取并解析；
    6. `clear_session` 删除该行；
    7. 可选 `max_messages_per_session` 在写回前截断 `history`。

    关键边界：
    - BLOB 非法或根非 JSON 对象时，回落为默认上下文字段（读路径容错）；
    - 空 `session_id`/`role` 在写入接口中会抛 `ValueError`（`clear_session`/`list_messages`/`load` 对空 sid 宽容）。

    与外部交互：
    - 文件系统：创建 sqlite 文件父目录；
    - 数据库：读写本地 sqlite（追加为读-改-写单行更新）。
    """

    def __init__(self, db_path: str | Path, max_messages_per_session: int = 0) -> None:
        """初始化消息存储。

        逻辑：
        1. 规范化 sqlite 文件路径并确保父目录存在；
        2. 规范化会话消息上限；
        3. 初始化数据库表结构（不存在则创建）。

        关键边界：
        - `max_messages_per_session <= 0` 视为不限制；
        - 构造阶段会创建数据库文件（若不存在）。

        副作用说明：
        - 可能创建 sqlite 文件与 `session_history` 表（列 `content`，无自动迁移）。
        """
        self._db_path = Path(db_path).expanduser()
        self._db_path.parent.mkdir(parents=True, exist_ok=True)
        self._max_messages_per_session = max(0, int(max_messages_per_session))
        self._init_schema()

    def append_message(self, session_id: str, *, role: str, content: str, meta: dict[str, Any] | None = None) -> None:
        """向指定会话追加一条历史消息。

        逻辑：
        1. 开启 IMMEDIATE 事务；
        2. 读取 `content` BLOB，解析出 `history` 与其余常驻字段；
        3. 向 `history` 追加一条，按上限截断后写回整包 JSON。

        关键边界：
        - `session_id`/`role` 非法时抛 `ValueError`；
        - `meta` 须可被 `json` 序列化；
        - 不修改调用方未显式更新的其余常驻字段。

        副作用说明：
        - 更新单行 `content` BLOB。
        """
        sid = (session_id or "").strip()
        if not sid:
            raise ValueError("session_id 不能为空。")
        role_text = (role or "").strip()
        if not role_text:
            raise ValueError("role 不能为空。")

        new_item = {"role": role_text, "content": content, "meta": meta or {}}
        json.dumps(new_item, ensure_ascii=False)

        with self._connect() as conn:
            conn.execute("BEGIN IMMEDIATE")
            row = conn.execute(
                "SELECT content FROM session_history WHERE session_id = ?",
                (sid,),
            ).fetchone()
            decoded = self._decode_content_blob(row[0] if row else None)
            items = list(decoded["history"])
            items.append(new_item)
            if self._max_messages_per_session > 0 and len(items) > self._max_messages_per_session:
                items = items[-self._max_messages_per_session :]
            blob = self._encode_content_payload(
                history_items=items,
                openai_messages=list(decoded["openai_messages"]),
                pending_tool_calls=list(decoded["pending_tool_calls"]),
                run_turn_phase=str(decoded["run_turn_phase"]),
                messages_total_tokens=int(decoded["messages_total_tokens"]),
                tool_loop_count=int(decoded["tool_loop_count"]),
            )
            conn.execute(
                """
                INSERT INTO session_history(session_id, content)
                VALUES(?, ?)
                ON CONFLICT(session_id) DO UPDATE SET
                    content = excluded.content,
                    updated_at = CURRENT_TIMESTAMP
                """,
                (sid, blob),
            )
            conn.commit()

    def save_conversation_content(self, session_id: str, payload: ConversationContext) -> None:
        """用内存中的 `ConversationContext` 覆盖写入该会话的 `content` 行。

        逻辑：
        1. 校验 `session_id`；
        2. 将 `history` 转为与持久化一致的字典列表，并按上限截断；
        3. 序列化主流程与 summary 压缩常驻字段后 UPSERT。

        关键边界：
        - 每条 `meta` 须可被 `json` 序列化；
        - 空 `session_id` 抛 `ValueError`。

        副作用说明：
        - 覆盖该行 `content`；若不存在则插入新行。
        """
        sid = (session_id or "").strip()
        if not sid:
            raise ValueError("session_id 不能为空。")
        items = self._records_to_dicts(payload.history)
        if self._max_messages_per_session > 0 and len(items) > self._max_messages_per_session:
            items = items[-self._max_messages_per_session :]
        blob = self._encode_content_payload(
            history_items=items,
            openai_messages=list(payload.openai_messages),
            pending_tool_calls=list(payload.pending_tool_calls),
            run_turn_phase=payload.run_turn_phase.value,
            messages_total_tokens=max(0, int(payload.messages_total_tokens)),
            tool_loop_count=max(0, int(payload.tool_loop_count)),
        )

        with self._connect() as conn:
            conn.execute(
                """
                INSERT INTO session_history(session_id, content)
                VALUES(?, ?)
                ON CONFLICT(session_id) DO UPDATE SET
                    content = excluded.content,
                    updated_at = CURRENT_TIMESTAMP
                """,
                (sid, blob),
            )
            conn.commit()

    def list_messages(self, session_id: str) -> list[MessageRecord]:
        """返回指定会话的历史消息列表副本。

        逻辑：
        1. 读取 `content`；
        2. 解析 `history` 并转为 `MessageRecord`。

        关键边界：
        - 会话不存在或内容损坏时返回空列表。

        Returns:
            list[MessageRecord]: 历史消息副本。
        """
        sid = (session_id or "").strip()
        if not sid:
            return []
        with self._connect() as conn:
            row = conn.execute(
                "SELECT content FROM session_history WHERE session_id = ?",
                (sid,),
            ).fetchone()
        decoded = self._decode_content_blob(row[0] if row else None)
        items = decoded["history"]
        return self._items_to_records(items)

    def load_conversation_content(self, session_id: str) -> ConversationContext:
        """从库中恢复某会话的 `ConversationContext`（含 summary 压缩常驻字段）。

        逻辑：
        1. 读取 `content` BLOB；
        2. 解析 `history` 与其余常驻字段并构造 `ConversationContext`。

        关键边界：
        - 空 `session_id` 时返回空的 `ConversationContext`。

        Returns:
            ConversationContext: 含 `history` 与其余常驻字段的新实例。
        """
        sid = (session_id or "").strip()
        if not sid:
            return ConversationContext()
        with self._connect() as conn:
            row = conn.execute(
                "SELECT content FROM session_history WHERE session_id = ?",
                (sid,),
            ).fetchone()
        decoded = self._decode_content_blob(row[0] if row else None)
        return ConversationContext(
            history=self._items_to_records(decoded["history"]),
            openai_messages=list(decoded["openai_messages"]),
            pending_tool_calls=list(decoded["pending_tool_calls"]),
            run_turn_phase=RunTurnPhase(str(decoded["run_turn_phase"])),
            messages_total_tokens=max(0, int(decoded["messages_total_tokens"])),
            tool_loop_count=max(0, int(decoded["tool_loop_count"])),
        )

    def clear_session(self, session_id: str) -> None:
        """清空指定会话历史消息。

        逻辑：
        1. 规范化 `session_id`；
        2. 非空则删除对应行。

        关键边界：
        - 空 `session_id` 或未命中会话时不抛错。

        副作用说明：
        - 删除 `session_history` 中该会话行。
        """
        sid = (session_id or "").strip()
        if not sid:
            return
        with self._connect() as conn:
            conn.execute("DELETE FROM session_history WHERE session_id = ?", (sid,))
            conn.commit()

    def _init_schema(self) -> None:
        """初始化 SQLite 表结构。

        逻辑：
        1. 建立连接；
        2. 创建 `session_history`（`session_id` + `content` BLOB）；
        3. 提交。

        关键边界：
        - 不做旧表结构或历史格式的自动迁移。

        与外部交互：
        - 数据库：执行 DDL。
        """
        with self._connect() as conn:
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS session_history (
                    session_id TEXT PRIMARY KEY,
                    content BLOB NOT NULL,
                    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
                )
                """
            )
            conn.commit()

    @staticmethod
    def _decode_content_blob(blob: bytes | None) -> dict[str, Any]:
        """将 `content` BLOB 解析为结构化会话上下文字段。

        逻辑：
        1. 空 BLOB 返回默认上下文字段；
        2. UTF-8 + `json.loads`，根须为 object；
        3. 读取 `history` 与其余常驻字段（包含 summary 压缩字段）。

        关键边界：
        - 解析失败或非 object 根时返回默认值，不抛异常；
        - 固定字段解析失败时回落为安全默认值（空列表、`idle`、`0`）。
        """
        empty_payload = {
            "history": [],
            "openai_messages": [],
            "pending_tool_calls": [],
            "run_turn_phase": RunTurnPhase.IDLE.value,
            "messages_total_tokens": 0,
            "tool_loop_count": 0,
        }
        if not blob:
            return empty_payload
        try:
            data = json.loads(blob.decode("utf-8"))
        except Exception:
            return empty_payload
        if not isinstance(data, dict):
            return empty_payload
        raw_hist = data.get("history", [])
        if not isinstance(raw_hist, list):
            raw_hist = []
        raw_openai_messages = data.get("openai_messages", [])
        raw_pending_tool_calls = data.get("pending_tool_calls", [])
        raw_phase = data.get("run_turn_phase", RunTurnPhase.IDLE.value)
        raw_messages_total_tokens = data.get("messages_total_tokens", 0)
        raw_tool_loop_count = data.get("tool_loop_count", 0)

        openai_messages: list[dict[str, Any]] = []
        if isinstance(raw_openai_messages, list):
            for item in raw_openai_messages:
                if isinstance(item, dict):
                    openai_messages.append(dict(item))

        pending_tool_calls: list[dict[str, Any]] = []
        if isinstance(raw_pending_tool_calls, list):
            for item in raw_pending_tool_calls:
                if not isinstance(item, dict):
                    continue
                call_id = str(item.get("call_id", "") or "").strip()
                if not call_id:
                    continue
                args = item.get("arguments")
                if not isinstance(args, dict):
                    args = {}
                pending_tool_calls.append(
                    {
                        "call_id": call_id,
                        "name": str(item.get("name", "") or ""),
                        "arguments": dict(args),
                    }
                )

        try:
            run_turn_phase = RunTurnPhase(str(raw_phase)).value
        except Exception:
            run_turn_phase = RunTurnPhase.IDLE.value
        try:
            messages_total_tokens = max(0, int(raw_messages_total_tokens))
        except Exception:
            messages_total_tokens = 0
        try:
            tool_loop_count = max(0, int(raw_tool_loop_count))
        except Exception:
            tool_loop_count = 0

        return {
            "history": SqliteMessageStore._normalize_message_dicts(raw_hist),
            "openai_messages": openai_messages,
            "pending_tool_calls": pending_tool_calls,
            "run_turn_phase": run_turn_phase,
            "messages_total_tokens": messages_total_tokens,
            "tool_loop_count": tool_loop_count,
        }

    @staticmethod
    def _normalize_message_dicts(raw_list: list[Any]) -> list[dict[str, Any]]:
        """将 JSON 数组中的元素归一化为消息字典列表。"""
        out: list[dict[str, Any]] = []
        for el in raw_list:
            if not isinstance(el, dict):
                continue
            role = el.get("role", "")
            content = el.get("content", "")
            meta = el.get("meta", {})
            if not isinstance(meta, dict):
                meta = {}
            out.append({"role": str(role), "content": str(content), "meta": meta})
        return out

    @staticmethod
    def _encode_content_payload(
        *,
        history_items: list[dict[str, Any]],
        openai_messages: list[dict[str, Any]],
        pending_tool_calls: list[dict[str, Any]],
        run_turn_phase: str,
        messages_total_tokens: int,
        tool_loop_count: int,
    ) -> bytes:
        """将会话上下文序列化为 UTF-8 JSON 字节。

        逻辑：
        1. 组装 `{"history","openai_messages","pending_tool_calls","run_turn_phase","messages_total_tokens","tool_loop_count"}`；
        2. `json.dumps` 后编码为 UTF-8。

        异常说明：
        - 不可序列化时向上抛出 `TypeError`/`ValueError`。
        """
        payload = {
            "history": history_items,
            "openai_messages": openai_messages,
            "pending_tool_calls": pending_tool_calls,
            "run_turn_phase": run_turn_phase,
            "messages_total_tokens": max(0, int(messages_total_tokens)),
            "tool_loop_count": max(0, int(tool_loop_count)),
        }
        return json.dumps(payload, ensure_ascii=False).encode("utf-8")

    @staticmethod
    def _records_to_dicts(history: list[MessageRecord]) -> list[dict[str, Any]]:
        """将 `MessageRecord` 列表转为持久化用的字典列表。"""
        return [r.model_dump() for r in history]

    @staticmethod
    def _items_to_records(items: list[dict[str, Any]]) -> list[MessageRecord]:
        """将反序列化后的字典列表转为不可变 `MessageRecord` 列表。

        逻辑：
        1. 遍历字典；
        2. 对 `role`/`content` 做 `str` 归一化，`meta` 做浅拷贝为 dict。

        关键边界：
        - 假定键已存在（由 `_normalize_message_dicts` 保证）；否则可能 `KeyError`。
        """
        return [MessageRecord.model_validate(it) for it in items]

    def _connect(self) -> sqlite3.Connection:
        """创建 sqlite 连接。

        逻辑：
        1. 连接到目标 sqlite 文件；
        2. 设置 `busy_timeout` 降低并发写冲突概率；
        3. 返回连接对象。

        Returns:
            sqlite3.Connection: 当前数据库连接。
        """
        conn = sqlite3.connect(self._db_path)
        conn.execute("PRAGMA busy_timeout=5000")
        return conn
