#!/usr/bin/env python3
"""诊断 Python TUI：超大文件工具参数是否导致 HITL 丢失或 UI 路径失败。

用法（仓库根目录）::

    PYTHONPATH=. python scripts/test_python_hitl_large_args.py
    PYTHONPATH=. python scripts/test_python_hitl_large_args.py --sizes 1000,500000,1048576
    PYTHONPATH=. python scripts/test_python_hitl_large_args.py --node-url http://127.0.0.1:8765

阶段说明：
1. sse_encode / sse_parse — 模拟 Node FormatSSE 单行 JSON
2. hitl_expand / approval_extract — expand_hitl_required + extract_tool_approval_requests
3. session_controller — SessionController._handle_stream_event 入队
4. aiohttp_stream — DAgentsApiClient.stream_events 经假 SSE 服务器读完整帧
5. rich_syntax — write_file 审批块 Rich Syntax 渲染耗时（TUI 瓶颈探测）
6. node_e2e（可选）— 对已运行 Node 发 mock 不可控，仅 health + 提示
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
import time
from dataclasses import dataclass, field
from typing import Any

from aiohttp import web

from app.cli.api_client import DAgentsApiClient, StreamEvent, _parse_sse_block
from app.cli.approval import ToolApprovalRequest, extract_tool_approval_requests
from app.cli.hitl_batch import expand_hitl_required
from app.cli.render import format_tool_call
from app.cli.session_controller import SessionController
from app.cli.tool_calls import normalize_tool_call_item, tool_display_name


GO_CLIENT_SSE_LINE_LIMIT = 1024 * 1024


@dataclass
class StageResult:
    name: str
    ok: bool
    elapsed_ms: float
    detail: str = ""


@dataclass
class SizeReport:
    content_bytes: int
    raw_args_bytes: int
    sse_line_bytes: int
    stages: list[StageResult] = field(default_factory=list)

    @property
    def all_ok(self) -> bool:
        return all(stage.ok for stage in self.stages)


def _ms(start: float) -> float:
    return (time.perf_counter() - start) * 1000.0


def build_write_file_args(content_bytes: int) -> tuple[dict[str, Any], str]:
    content = "X" * content_bytes
    args = {"path": "large-test.txt", "content": content}
    raw = json.dumps(args, ensure_ascii=False)
    return args, raw


def build_search_replace_args(content_bytes: int) -> tuple[dict[str, Any], str]:
    old = "O" * content_bytes
    new = "N" * content_bytes
    args = {"path": "large-test.txt", "old_string": old, "new_string": new}
    raw = json.dumps(args, ensure_ascii=False)
    return args, raw


def build_tool_args(tool: str, content_bytes: int) -> tuple[dict[str, Any], str, str]:
    if tool == "search_replace":
        args, raw = build_search_replace_args(content_bytes)
        return args, raw, "search_replace"
    args, raw = build_write_file_args(content_bytes)
    return args, raw, "write_file"


def build_hitl_required_data(args: dict[str, Any], raw: str, tool_name: str) -> dict[str, Any]:
    path = str(args.get("path") or "large-test.txt")
    return {
        "hitl_id": "hitl-large-test",
        "message": "检测到工具调用，等待用户确认后继续执行。",
        "items": [
            {
                "hitl_type": "execute_tool",
                "id": "call-write-large",
                "name": tool_name,
                "arguments": args,
                "raw_arguments": raw,
                "approval_reason": f"将修改本地文件: {path}",
                "risk_level": "medium",
            }
        ],
    }


def build_tool_call_data(raw: str, tool_name: str) -> dict[str, Any]:
    return {
        "partial": False,
        "tool_calls": [
            {
                "id": "call-write-large",
                "type": "function",
                "function": {
                    "name": tool_name,
                    "arguments": raw,
                },
            }
        ],
    }


def format_node_sse_line(
    *,
    event_type: str,
    seq: int,
    session_id: str,
    data: dict[str, Any],
) -> str:
    envelope = {
        "session_id": session_id,
        "agent_id": "agent-test",
        "type": event_type,
        "seq": seq,
        "data": data,
    }
    return f"id: {seq}\nevent: {event_type}\ndata: {json.dumps(envelope, ensure_ascii=False)}\n\n"


def stage_sse_encode(data: dict[str, Any]) -> tuple[StageResult, str]:
    start = time.perf_counter()
    try:
        line = format_node_sse_line(
            event_type="hitl_required",
            seq=42,
            session_id="s-large",
            data=data,
        )
        detail = f"sse_line={len(line)} bytes"
        if len(line) > GO_CLIENT_SSE_LINE_LIMIT:
            detail += f" (超过 Go client 1MiB 单行上限 {GO_CLIENT_SSE_LINE_LIMIT})"
        return StageResult("sse_encode", True, _ms(start), detail), line
    except Exception as exc:  # noqa: BLE001
        return StageResult("sse_encode", False, _ms(start), str(exc)), ""


def stage_sse_parse(sse_block: str) -> tuple[StageResult, StreamEvent | None]:
    start = time.perf_counter()
    try:
        event = _parse_sse_block(sse_block.strip())
        if event is None:
            return StageResult("sse_parse", False, _ms(start), "parse returned None"), None
        if event.event_type != "hitl_required":
            return (
                StageResult(
                    "sse_parse",
                    False,
                    _ms(start),
                    f"unexpected event_type={event.event_type}",
                ),
                None,
            )
        return StageResult("sse_parse", True, _ms(start), f"seq_payload_keys={list(event.payload.keys())}"), event
    except Exception as exc:  # noqa: BLE001
        return StageResult("sse_parse", False, _ms(start), str(exc)), None


def stage_hitl_expand(data: dict[str, Any]) -> tuple[StageResult, dict[str, Any] | None]:
    start = time.perf_counter()
    try:
        _user_infos, approval = expand_hitl_required(data)
        if approval is None:
            return StageResult("hitl_expand", False, _ms(start), "approval is None"), None
        calls = approval.get("approval_args", {}).get("tool_calls", [])
        if not calls:
            return StageResult("hitl_expand", False, _ms(start), "tool_calls empty"), None
        return StageResult("hitl_expand", True, _ms(start), f"tool_calls={len(calls)}"), approval
    except Exception as exc:  # noqa: BLE001
        return StageResult("hitl_expand", False, _ms(start), str(exc)), None


def stage_approval_extract(approval: dict[str, Any]) -> StageResult:
    start = time.perf_counter()
    try:
        requests = extract_tool_approval_requests(approval)
        if not requests:
            return StageResult("approval_extract", False, _ms(start), "no requests")
        req = requests[0]
        detail = (
            f"call_id={req.call_id} name={req.name} "
            f"arg_keys={list(req.arguments.keys())} raw_len={len(req.raw_arguments)}"
        )
        return StageResult("approval_extract", True, _ms(start), detail)
    except Exception as exc:  # noqa: BLE001
        return StageResult("approval_extract", False, _ms(start), str(exc))


def stage_format_tool_call(tool_call_data: dict[str, Any]) -> StageResult:
    start = time.perf_counter()
    try:
        update = format_tool_call(tool_call_data)
        if update is None:
            return StageResult("format_tool_call", False, _ms(start), "returned None")
        return StageResult(
            "format_tool_call",
            True,
            _ms(start),
            f"text_len={len(update.text)} (args 展示截断到 500 字符)",
        )
    except Exception as exc:  # noqa: BLE001
        return StageResult("format_tool_call", False, _ms(start), str(exc))


def stage_tui_tool_parts(tool_call_data: dict[str, Any]) -> StageResult:
    """模拟 TUI `_tool_call_parts_from_call`：write_file 会把完整 content 放进代码框。"""
    start = time.perf_counter()
    try:
        tool_calls = tool_call_data.get("tool_calls")
        if not isinstance(tool_calls, list) or not tool_calls:
            return StageResult("tui_tool_parts", False, _ms(start), "no tool_calls")
        item = tool_calls[0]
        normalized = normalize_tool_call_item(item)
        name = str(normalized.get("name") or "")
        arguments = normalized.get("arguments")
        if not isinstance(arguments, dict):
            arguments = {}
        summary = tool_display_name(name, arguments)
        code_content: str | None = None
        if name == "write_file":
            content = str(arguments.get("content") or "")
            code_content = content if content else None
        detail = f"summary_len={len(summary)} code_content_len={len(code_content or '')}"
        return StageResult("tui_tool_parts", True, _ms(start), detail)
    except Exception as exc:  # noqa: BLE001
        return StageResult("tui_tool_parts", False, _ms(start), str(exc))


def stage_tui_rich_export(content_bytes: int, *, slow_ms: float) -> StageResult:
    """模拟 TUI `_rich_code_box` + word_wrap 导出；Textual RichLog 写入前的 Rich 渲染成本。"""
    start = time.perf_counter()
    try:
        from io import StringIO

        from rich.console import Console
        from rich.panel import Panel
        from rich.syntax import Syntax

        content = "X" * content_bytes
        panel = Panel(
            Syntax(content, "text", theme="monokai", word_wrap=True, background_color="default"),
            border_style="dim",
            padding=(0, 1),
        )
        buf = StringIO()
        Console(file=buf, width=120, force_terminal=True).print(panel)
        exported = buf.getvalue()
        elapsed = _ms(start)
        detail = f"export_len={len(exported)} lines≈{exported.count(chr(10))}"
        ok = True
        if elapsed > slow_ms:
            detail += f" (超过 slow 阈值 {slow_ms:.0f}ms，Textual 可能长时间卡住)"
            ok = False
        return StageResult("tui_rich_export", ok, elapsed, detail)
    except Exception as exc:  # noqa: BLE001
        return StageResult("tui_rich_export", False, _ms(start), str(exc))


def stage_tui_approval_synthetic(content_bytes: int) -> StageResult:
    """模拟 `_ensure_approval_pending_tool_blocks` 对 write_file 使用 raw_arguments 作 code_content。"""
    start = time.perf_counter()
    try:
        args, raw = build_write_file_args(content_bytes)
        item = ToolApprovalRequest(
            call_id="call-write-large",
            name="write_file",
            arguments=args,
            raw_arguments=raw,
        )
        code_content = raw if raw.strip() and raw.strip() != "{}" else None
        detail = f"code_content_len={len(code_content or '')}"
        return StageResult("tui_approval_synthetic", True, _ms(start), detail)
    except Exception as exc:  # noqa: BLE001
        return StageResult("tui_approval_synthetic", False, _ms(start), str(exc))


async def stage_event_sequence(content_bytes: int, tool: str) -> StageResult:
    """tool_call → hitl_required → done 顺序入队（对齐真实 Node 事件顺序）。"""
    args, raw, tool_name = build_tool_args(tool, content_bytes)
    hitl_data = build_hitl_required_data(args, raw, tool_name)
    tool_call_data = build_tool_call_data(raw, tool_name)
    start = time.perf_counter()
    controller = SessionController(api_base="http://test", session_id="s-large", show_reasoning=False)
    controller.session_id = "s-large"
    controller._reset_user_turn_wait()
    try:
        await controller._handle_stream_event(
            StreamEvent(
                event_type="tool_call",
                event_id="1",
                payload={"session_id": "s-large", "seq": 1, "data": tool_call_data},
            )
        )
        await controller._handle_stream_event(
            StreamEvent(
                event_type="hitl_required",
                event_id="2",
                payload={"session_id": "s-large", "seq": 2, "data": hitl_data},
            )
        )
        await controller._handle_stream_event(
            StreamEvent(
                event_type="done",
                event_id="3",
                payload={
                    "session_id": "s-large",
                    "seq": 3,
                    "data": {
                        "finish_reason": "awaiting_hitl",
                        "turn_complete": False,
                        "awaiting": "hitl",
                    },
                },
            )
        )
        item = controller.peek_hitl()
        if item is None:
            return StageResult("event_sequence", False, _ms(start), "hitl queue empty after sequence")
        return StageResult(
            "event_sequence",
            True,
            _ms(start),
            f"queue_len={controller.hitl_queue_len()} kind={item.kind}",
        )
    except Exception as exc:  # noqa: BLE001
        return StageResult("event_sequence", False, _ms(start), str(exc))


async def stage_session_controller(data: dict[str, Any]) -> StageResult:
    start = time.perf_counter()
    controller = SessionController(api_base="http://test", session_id="s-large", show_reasoning=False)
    controller.session_id = "s-large"
    event = StreamEvent(
        event_type="hitl_required",
        event_id="99",
        payload={"session_id": "s-large", "seq": 99, "data": data},
    )
    try:
        await controller._handle_stream_event(event)
        item = controller.peek_hitl()
        if item is None or item.kind != "approval":
            return StageResult(
                "session_controller",
                False,
                _ms(start),
                f"queue_len={controller.hitl_queue_len()} head={item}",
            )
        requests = extract_tool_approval_requests(item.data)
        if not requests:
            return StageResult("session_controller", False, _ms(start), "head approval has no requests")
        return StageResult(
            "session_controller",
            True,
            _ms(start),
            f"queue_len={controller.hitl_queue_len()} call_id={requests[0].call_id}",
        )
    except Exception as exc:  # noqa: BLE001
        return StageResult("session_controller", False, _ms(start), str(exc))


def stage_rich_syntax(content_bytes: int, *, slow_ms: float) -> StageResult:
    start = time.perf_counter()
    try:
        from rich.syntax import Syntax

        content = "X" * content_bytes
        Syntax(content, "text", word_wrap=True)
        elapsed = _ms(start)
        detail = f"render_ms={elapsed:.1f}"
        ok = True
        if elapsed > slow_ms:
            detail += f" (超过 slow 阈值 {slow_ms:.0f}ms，TUI 可能卡顿)"
            ok = False
        return StageResult("rich_syntax", ok, elapsed, detail)
    except Exception as exc:  # noqa: BLE001
        return StageResult("rich_syntax", False, _ms(start), str(exc))


async def stage_aiohttp_stream(content_bytes: int, tool: str) -> StageResult:
    args, raw, tool_name = build_tool_args(tool, content_bytes)
    hitl_data = build_hitl_required_data(args, raw, tool_name)
    tool_call_data = build_tool_call_data(raw, tool_name)
    session_id = "s-stream-test"
    frames = [
        format_node_sse_line(event_type="tool_call", seq=1, session_id=session_id, data=tool_call_data),
        format_node_sse_line(event_type="hitl_required", seq=2, session_id=session_id, data=hitl_data),
        format_node_sse_line(
            event_type="done",
            seq=3,
            session_id=session_id,
            data={
                "finish_reason": "awaiting_hitl",
                "turn_complete": False,
                "awaiting": "hitl",
            },
        ),
    ]
    payload = "".join(frames).encode("utf-8")

    app = web.Application()

    async def handle_streams(_request: web.Request) -> web.StreamResponse:
        resp = web.StreamResponse(
            status=200,
            headers={
                "Content-Type": "text/event-stream; charset=utf-8",
                "Cache-Control": "no-cache",
            },
        )
        await resp.prepare(_request)
        chunk_size = 4096
        for offset in range(0, len(payload), chunk_size):
            await resp.write(payload[offset : offset + chunk_size])
            await asyncio.sleep(0)
        await resp.write(b": eof\n\n")
        return resp

    app.router.add_get("/v1/streams", handle_streams)
    runner = web.AppRunner(app)
    await runner.setup()
    site = web.TCPSite(runner, "127.0.0.1", 0)
    await site.start()
    port = site._server.sockets[0].getsockname()[1]  # type: ignore[union-attr]
    base = f"http://127.0.0.1:{port}"

    start = time.perf_counter()
    client = DAgentsApiClient(base)
    seen: list[str] = []
    hitl_payload: dict[str, Any] | None = None
    try:
        async for event in client.stream_events(session_id=session_id):
            if event.is_stream_ready:
                continue
            seen.append(event.event_type)
            if event.event_type == "hitl_required":
                hitl_payload = dict(event.data)
        elapsed = _ms(start)
        if hitl_payload is None:
            return StageResult(
                "aiohttp_stream",
                False,
                elapsed,
                f"no hitl_required; seen={seen}",
            )
        _ui, approval = expand_hitl_required(hitl_payload)
        if approval is None:
            return StageResult("aiohttp_stream", False, elapsed, f"expand failed; seen={seen}")
        return StageResult(
            "aiohttp_stream",
            True,
            elapsed,
            f"seen={seen} approval_calls={len(approval.get('approval_args', {}).get('tool_calls', []))}",
        )
    except Exception as exc:  # noqa: BLE001
        return StageResult("aiohttp_stream", False, _ms(start), str(exc))
    finally:
        await client.close()
        await runner.cleanup()


async def run_size(content_bytes: int, *, slow_ms: float, tool: str) -> SizeReport:
    args, raw, tool_name = build_tool_args(tool, content_bytes)
    hitl_data = build_hitl_required_data(args, raw, tool_name)
    tool_call_data = build_tool_call_data(raw, tool_name)
    report = SizeReport(
        content_bytes=content_bytes,
        raw_args_bytes=len(raw),
        sse_line_bytes=0,
    )

    report.stages.append(stage_format_tool_call(tool_call_data))
    report.stages.append(stage_tui_tool_parts(tool_call_data))
    report.stages.append(stage_tui_approval_synthetic(content_bytes))
    report.stages.append(await stage_event_sequence(content_bytes, tool))

    encode_stage, sse_block = stage_sse_encode(hitl_data)
    report.stages.append(encode_stage)
    if encode_stage.ok:
        report.sse_line_bytes = len(sse_block)

    parse_stage, event = stage_sse_parse(sse_block)
    report.stages.append(parse_stage)

    expand_stage, approval = stage_hitl_expand(hitl_data)
    report.stages.append(expand_stage)

    if approval is not None:
        report.stages.append(stage_approval_extract(approval))

    report.stages.append(await stage_session_controller(hitl_data))
    report.stages.append(await stage_aiohttp_stream(content_bytes, tool))
    report.stages.append(stage_rich_syntax(content_bytes, slow_ms=slow_ms))
    report.stages.append(stage_tui_rich_export(content_bytes, slow_ms=slow_ms))

    if event is not None and event.data:
        # 二次校验：SSE 解析后的 data 与直接 expand 一致
        _ui, approval_from_event = expand_hitl_required(event.data)
        ok = approval_from_event is not None
        report.stages.append(
            StageResult(
                "sse_roundtrip_expand",
                ok,
                0.0,
                "ok" if ok else "expand from parsed event failed",
            )
        )

    return report


def parse_sizes(raw: str) -> list[int]:
    out: list[int] = []
    for part in raw.split(","):
        part = part.strip().lower()
        if not part:
            continue
        if part.endswith("k"):
            out.append(int(float(part[:-1]) * 1024))
        elif part.endswith("m"):
            out.append(int(float(part[:-1]) * 1024 * 1024))
        else:
            out.append(int(part))
    return out


def print_report(report: SizeReport) -> None:
    mib = report.content_bytes / (1024 * 1024)
    print(f"\n{'=' * 72}")
    print(
        f"content={report.content_bytes} B ({mib:.3f} MiB)  "
        f"raw_args={report.raw_args_bytes} B  sse_line≈{report.sse_line_bytes} B"
    )
    if report.sse_line_bytes > GO_CLIENT_SSE_LINE_LIMIT:
        print(
            f"  ⚠ SSE 单行超过 Go client 上限 ({GO_CLIENT_SSE_LINE_LIMIT} B)；"
            "Go TUI 会丢事件，Python 仍可能收到"
        )
    for stage in report.stages:
        mark = "OK" if stage.ok else "FAIL"
        print(f"  [{mark:4}] {stage.name:22} {stage.elapsed_ms:8.1f} ms  {stage.detail}")


async def optional_node_probe(node_url: str) -> None:
    base = node_url.rstrip("/")
    client = DAgentsApiClient(base)
    try:
        ok = await client.health()
        print(f"\nNode health ({base}): {'OK' if ok else 'FAIL'}")
        if ok:
            info = await client.get_agent_info()
            llm = info.get("llm") or {}
            print(
                "  agent_id=%s mock=%s model=%s"
                % (info.get("agent_id"), llm.get("mock"), llm.get("model"))
            )
            print(
                "  提示：要对真实 Node 复现，需 mock LLM 返回超大 write_file 且 policy 为 require_approval；"
                "本脚本主要覆盖 Python client 路径。"
            )
    except Exception as exc:  # noqa: BLE001
        print(f"\nNode probe failed: {exc}")
    finally:
        await client.close()


async def async_main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--sizes",
        default="1000,100000,500000,1048576,2097152",
        help="content 字节数，逗号分隔；支持 k/m 后缀（默认 1K,100K,500K,1M,2M）",
    )
    parser.add_argument(
        "--tool",
        choices=("write_file", "search_replace"),
        default="write_file",
        help="模拟的文件工具（search_replace 的 old+new 约为 2× content）",
    )
    parser.add_argument(
        "--slow-ms",
        type=float,
        default=3000.0,
        help="rich_syntax / tui_rich_export 超过此毫秒数判为 FAIL（模拟 TUI 卡顿）",
    )
    parser.add_argument(
        "--node-url",
        default="",
        help="可选：探测已运行 Node 的 /health",
    )
    args = parser.parse_args(argv)
    sizes = parse_sizes(args.sizes)
    if not sizes:
        print("no sizes", file=sys.stderr)
        return 2

    print("Python TUI HITL 大参数诊断")
    print(f"tool={args.tool} sizes={sizes} slow_ms={args.slow_ms}")

    reports: list[SizeReport] = []
    for size in sizes:
        reports.append(await run_size(size, slow_ms=args.slow_ms, tool=args.tool))

    for report in reports:
        print_report(report)

    failed = [r for r in reports if not r.all_ok]
    print(f"\n{'=' * 72}")
    print(f"总计 {len(reports)} 档，失败 {len(failed)} 档")
    if failed:
        print("失败档位 content 字节:", [r.content_bytes for r in failed])

    if args.node_url.strip():
        await optional_node_probe(args.node_url.strip())

    return 1 if failed else 0


def main() -> None:
    raise SystemExit(asyncio.run(async_main(sys.argv[1:])))


if __name__ == "__main__":
    main()
