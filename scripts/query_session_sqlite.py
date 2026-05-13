#!/usr/bin/env python3
"""运维：读取 **`SqliteMessageStore`** 持久化的会话快照（按 **`session_id`**）。

用法（仓库根目录）：

    PYTHONPATH=. python scripts/query_session_sqlite.py list
    PYTHONPATH=. python scripts/query_session_sqlite.py show <session_id>

数据库路径：优先 **`--db`**，否则 **`AGENT_SESSION_STORE_PATH`**（会先 **`load_env`** 读取 `.env`），再否则 **`.runtime/memory/session.sqlite3`**（相对仓库根，与 AgentService 默认一致）。
"""

from __future__ import annotations

import argparse
import json
import sqlite3
import sys
from pathlib import Path

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))


def _resolve_db_path(argv_db: str) -> Path:
    """解析 sqlite 文件路径。

    逻辑：
    1. **`argv_db` 非空** → **`expanduser`** 后为唯一候选；
    2. 否则静默加载 **`resolve_runtime_root()/.env`**（不 **`print`**，便于 **`list`** 管道）后读 **`AGENT_SESSION_STORE_PATH`**；
    3. 再否则默认 **`.runtime/memory/session.sqlite3`**（相对仓库根，与 AgentService 一致）。

    关键边界：
    - 候选路径仍可能不存在（**`list`** / **`show`** 内再报错）。
    """
    raw = (argv_db or "").strip()
    if raw:
        return Path(raw).expanduser().resolve()

    from app.config.env import resolve_runtime_root

    try:
        from dotenv import load_dotenv

        env_file = resolve_runtime_root() / ".env"
        if env_file.is_file():
            # 与 **`load_env`** 一致：**`override=False`**；不写 stdout，避免破坏 **`list`** 的 TSV。
            load_dotenv(env_file, override=False)
    except ImportError:
        pass

    import os

    def _under_repo(rel_or_abs: str) -> Path:
        p = Path(rel_or_abs).expanduser()
        return p.resolve() if p.is_absolute() else (resolve_runtime_root() / p).resolve()

    env_path = (os.getenv("AGENT_SESSION_STORE_PATH") or "").strip()
    if env_path:
        return _under_repo(env_path)

    return _under_repo(".runtime/memory/session.sqlite3")


def _conversation_context_to_jsonable(store_path: Path, session_id: str) -> dict[str, object]:
    """加载会话并转为可 **`json.dumps`** 的字典。

    逻辑：
    1. **`SqliteMessageStore`** **`load_conversation_content`**；
    2. **`history`** 用 **`model_dump`**；
    3. **`run_turn_phase`** 写成字符串值。

    关键边界：
    - 库中无该行时上游应先 **`exists`**；此处仍可能得到「全默认」结构。
    """
    from app.harness.memory.store import SqliteMessageStore

    cc = SqliteMessageStore(store_path).load_conversation_content(session_id)
    return {
        "session_id": session_id,
        "run_turn_phase": cc.run_turn_phase.value,
        "messages_total_tokens": cc.messages_total_tokens,
        "tool_loop_count": cc.tool_loop_count,
        "loaded_skills": list(cc.loaded_skills),
        "pending_tool_calls": list(cc.pending_tool_calls),
        "openai_messages": list(cc.openai_messages),
        "history": [m.model_dump() for m in cc.history],
    }


def cmd_list(db_path: Path) -> None:
    """列出 **`session_history`** 中的会话 id、**`updated_at`**、**`content`** 字节长度。"""
    if not db_path.is_file():
        print(f"ERROR: 数据库文件不存在: {db_path}", file=sys.stderr)
        raise SystemExit(1)

    conn = sqlite3.connect(str(db_path))
    try:
        rows = conn.execute(
            "SELECT session_id, updated_at, length(content) FROM session_history ORDER BY updated_at DESC"
        ).fetchall()
    finally:
        conn.close()

    for sid, updated_at, blob_len in rows:
        print(f"{sid}\t{updated_at}\t{blob_len}")


def cmd_show(db_path: Path, session_id: str) -> None:
    """打印单会话完整 JSON（含 **`openai_messages`** / **`history`**）。"""
    if not db_path.is_file():
        print(f"ERROR: 数据库文件不存在: {db_path}", file=sys.stderr)
        raise SystemExit(1)

    sid = (session_id or "").strip()
    if not sid:
        print("ERROR: session_id 不能为空", file=sys.stderr)
        raise SystemExit(2)

    conn = sqlite3.connect(str(db_path))
    try:
        hit = conn.execute(
            "SELECT 1 FROM session_history WHERE session_id = ? LIMIT 1",
            (sid,),
        ).fetchone()
    finally:
        conn.close()

    if not hit:
        print(f"NOT_FOUND: session_id={sid!r}", file=sys.stderr)
        raise SystemExit(3)

    payload = _conversation_context_to_jsonable(db_path, sid)
    print(json.dumps(payload, ensure_ascii=False, indent=2))


def main() -> None:
    """CLI 入口：子命令 **`list`** / **`show`**。"""
    parser = argparse.ArgumentParser(description="按 session_id 查询 SqliteMessageStore 会话快照")
    parser.add_argument(
        "--db",
        default="",
        help="sqlite 路径；省略则从 AGENT_SESSION_STORE_PATH / 默认 .runtime/memory/session.sqlite3 推断",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("list", help="列出 session_id、updated_at、content 字节长度（TSV）")

    p_show = sub.add_parser("show", help="输出指定会话 JSON（openai_messages + history 等）")
    p_show.add_argument("session_id", help="会话 ID")

    args = parser.parse_args()
    db_path = _resolve_db_path(args.db)

    if args.command == "list":
        cmd_list(db_path)
    else:
        cmd_show(db_path, args.session_id)


if __name__ == "__main__":
    main()
