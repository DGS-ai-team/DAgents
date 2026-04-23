from __future__ import annotations

import asyncio
import json
import re
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import httpx

BASE_URL = "http://127.0.0.1:8000"
SESSION_ID = "demo-session"
CLIENT_ID = "demo-client"
AUTO_RESUME_ON_INTERRUPT = True
RAW_STREAM_LOG = Path(__file__).resolve().parent / "call_agent_api_raw_stream.jsonl"
TYPEWRITER_CPS = 10


def _append_raw_log(record: dict[str, Any]) -> None:
    """将原始流记录按 JSONL 追加到文件，便于离线排查。"""
    with RAW_STREAM_LOG.open("a", encoding="utf-8") as f:
        f.write(json.dumps(record, ensure_ascii=False, default=str) + "\n")


def _print_tag(tag: str, message: str = "", *, end: str = "\n") -> None:
    """统一控制台输出前缀，避免出现无标识文本。

    逻辑：
    1. 使用 `[tag]` 作为统一前缀；
    2. 支持自定义行尾，便于流式输出与普通日志共用；
    3. 全量 `flush=True`，保证流式可见性。
    """
    print(f"[{tag}] {message}", end=end, flush=True)


def _print_allowed(tag: str, message: str = "", *, end: str = "\n") -> None:
    """仅打印允许展示的类型：tool_call/tool/ai/updates。"""
    if tag not in {"tool_call", "tool", "ai", "updates"}:
        return
    _print_tag(tag, message, end=end)


def _normalize_ai_text(text: str) -> str:
    """规范化 AI 可见文本的空格与换行，降低 token 粒度导致的观感抖动。

    逻辑：
    1. 统一换行符为 `\\n`；
    2. 行内把连续空白折叠为单空格；
    3. 连续空行折叠为最多一个空行；
    4. 保留每行有效内容顺序，不改写语义词序。
    """
    unified = text.replace("\r\n", "\n").replace("\r", "\n")
    lines = [re.sub(r"[ \t]+", " ", line).strip() for line in unified.split("\n")]
    compact = "\n".join(lines)
    compact = re.sub(r"\n{3,}", "\n\n", compact)
    return compact


async def _run_typewriter(
    queue: "asyncio.Queue[str | None]",
    *,
    cps: int,
) -> None:
    """从缓存队列匀速打印 AI 字符流。

    逻辑：
    1. 消费者从 `queue` 读取字符片段（生产者在网络事件循环中持续入队）；
    2. 首次输出时仅打印一次 `[ai] ` 前缀；
    3. 按固定 `cps` 速率逐字符输出，削峰填谷，降低突发 token 抖动；
    4. 收到 `None` 作为结束信号后收尾换行并退出。

    边界：
    - `cps <= 0` 时回退为 32；
    - 仅处理字符串片段，其他类型由上游过滤。
    """
    rate = cps if cps > 0 else 32
    delay = 1.0 / rate
    has_prefix = False
    while True:
        piece = await queue.get()
        if piece is None:
            if has_prefix:
                print("", flush=True)
            break
        if not piece:
            continue
        normalized = _normalize_ai_text(piece)
        if not normalized:
            continue
        if not has_prefix:
            _print_allowed("ai", "", end="")
            has_prefix = True
        for ch in normalized:
            print(ch, end="", flush=True)
            await asyncio.sleep(delay)


def _tool_chunk_key(chunk: dict[str, Any]) -> int:
    """从单条 tool_call_chunk 解析 index。

    逻辑：读取 `index` 键，缺省为 0；转为 int。
    """
    return int(chunk.get("index", 0))


def _apply_tool_call_chunks(
    buffers: dict[int, dict[str, str]],
    chunks: list[Any],
) -> list[tuple[int, str, str]]:
    """把本帧 `tool_call_chunks` 累加到 `buffers`，并产出增量片段供打印。

    逻辑：
    1. 遍历 chunks，跳过非 dict；
    2. 按 index 分桶，合并 `name`（覆盖）与 `args`（字符串拼接）；
    3. 返回本次相对上一帧新增的 (index, field, fragment)，field 为 name/args。
    """
    deltas: list[tuple[int, str, str]] = []
    for raw in chunks or []:
        if not isinstance(raw, dict):
            continue
        idx = _tool_chunk_key(raw)
        state = buffers.setdefault(idx, {"name": "", "args": ""})
        name = raw.get("name")
        if name:
            prev = state["name"]
            if name != prev:
                state["name"] = str(name)
                deltas.append((idx, "name", str(name)))
        args_piece = raw.get("args")
        if args_piece is not None and args_piece != "":
            fragment = str(args_piece)
            state["args"] += fragment
            deltas.append((idx, "args", fragment))
    return deltas


def _format_tool_buffers_line(buffers: dict[int, dict[str, str]]) -> str:
    """将各 index 上已拼接的 name/args 格式化为可读单行。

    逻辑：按 index 排序后输出 `[idx] name(args拼接串)`，多路工具用 ` | ` 连接。
    """
    parts: list[str] = []
    for idx in sorted(buffers.keys()):
        st = buffers[idx]
        name = st["name"] or "(tool)"
        parts.append(f"[{idx}] {name}({st['args']})")
    return " | ".join(parts)


async def _submit_message(client: httpx.AsyncClient, *, content: str) -> None:
    resp = await client.post(
        f"{BASE_URL}/v1/messages",
        json={
            "session_id": SESSION_ID,
            "client_id": CLIENT_ID,
            "request_type": "message",
            "content": content,
            "source": "script",
            "priority": "other",
        },
    )
    resp.raise_for_status()
    _ = resp.json()


async def _submit_resume(client: httpx.AsyncClient, *, resume_value: Any) -> None:
    resp = await client.post(
        f"{BASE_URL}/v1/messages",
        json={
            "session_id": SESSION_ID,
            "client_id": CLIENT_ID,
            "request_type": "resume",
            "resume_value": resume_value,
            "source": "script",
            "priority": "resume",
        },
    )
    resp.raise_for_status()
    _ = resp.json()


async def _stream_request(client: httpx.AsyncClient) -> dict[str, Any]:
    assembled = ""
    tool_call_buffers: dict[int, dict[str, str]] = {}
    tool_call_header_printed: set[int] = set()
    tool_call_line_open = False
    interrupt_payload: dict[str, Any] | None = None
    current_event = ""
    ai_queue: asyncio.Queue[str | None] = asyncio.Queue()
    ai_task = asyncio.create_task(_run_typewriter(ai_queue, cps=TYPEWRITER_CPS))
    async with client.stream("GET", f"{BASE_URL}/v1/streams?client_id={CLIENT_ID}") as sse_resp:
        sse_resp.raise_for_status()
        async for line in sse_resp.aiter_lines():
            if not line:
                continue

            if line.startswith("event: "):
                current_event = line.split("event: ", 1)[1].strip()
                if current_event == "done":
                    break
                continue

            if not line.startswith("data: "):
                continue

            raw_json = line.split("data: ", 1)[1]
            payload = json.loads(raw_json)
            if str(payload.get("session_id", "")).strip() != SESSION_ID:
                continue
            _append_raw_log(
                {
                    "ts": datetime.now(timezone.utc).isoformat(),
                    "client_id": CLIENT_ID,
                    "event": current_event,
                    "raw_payload": payload,
                }
            )
            event_data = payload.get("data", {})
            if current_event == "messages":
                msg_type = event_data.get("message_type", "ai")
                if msg_type == "tool_call":
                    chunks = event_data.get("tool_call_chunks") or []
                    deltas = _apply_tool_call_chunks(tool_call_buffers, chunks)
                    for idx, field, fragment in deltas:
                        if field == "name" and idx not in tool_call_header_printed:
                            _print_allowed("tool_call", f"[idx={idx}] name={fragment} args=", end="")
                            tool_call_header_printed.add(idx)
                            tool_call_line_open = True
                        elif field == "args":
                            if idx not in tool_call_header_printed:
                                _print_allowed("tool_call", f"[idx={idx}] args=", end="")
                                tool_call_header_printed.add(idx)
                                tool_call_line_open = True
                            print(fragment, end="", flush=True)
                elif msg_type == "tool":
                    if tool_call_line_open:
                        print("", flush=True)
                        tool_call_line_open = False
                    tc = event_data.get("content", "")
                    if tc:
                        for line_text in str(tc).splitlines() or [""]:
                            _print_allowed("tool", line_text)
                else:
                    if tool_call_line_open:
                        print("", flush=True)
                        tool_call_line_open = False
                    piece = event_data.get("content", "")
                    if isinstance(piece, str):
                        if piece.strip():
                            assembled += piece
                            await ai_queue.put(piece)
                    elif piece:
                        piece_text = str(piece)
                        assembled += piece_text
                        await ai_queue.put(piece_text)
            elif current_event == "updates":
                if tool_call_line_open:
                    print("", flush=True)
                    tool_call_line_open = False
                interrupt_payload = event_data.get("interrupt")
                if isinstance(interrupt_payload, dict):
                    _print_allowed("updates", json.dumps(interrupt_payload, ensure_ascii=False))
            elif current_event == "error":
                if tool_call_line_open:
                    print("", flush=True)
                    tool_call_line_open = False
                # 仅保留 tool_call/tool/ai 的可见输出；error 仍记录在 raw log。
                pass

    if tool_call_line_open:
        print("", flush=True)
    await ai_queue.put(None)
    await ai_task

    return {
        "assembled": _normalize_ai_text(assembled),
        "interrupt": interrupt_payload,
        "tool_call_merged": _format_tool_buffers_line(tool_call_buffers),
    }


async def main() -> None:
    async with httpx.AsyncClient(timeout=60) as client:
        await _submit_message(client, content="计算以下两个数字之和：1 + 2")
        first_round = await _stream_request(client)

        if AUTO_RESUME_ON_INTERRUPT and first_round.get("interrupt"):
            # 示例：收到 interrupt 后自动同意继续，resume_value 可按业务改成 reject/自定义参数。
            await _submit_resume(
                client,
                resume_value={"type": "approve"},
            )
            await _stream_request(client)


if __name__ == "__main__":
    asyncio.run(main())

