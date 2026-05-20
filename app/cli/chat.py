from __future__ import annotations

import argparse
import asyncio
from contextlib import suppress
from typing import Any
from uuid import uuid4

from prompt_toolkit import PromptSession
from prompt_toolkit.patch_stdout import patch_stdout

from app.cli.api_client import DAgentsApiClient, StreamEvent
from app.cli.approval import (
    ToolApprovalRequest,
    build_all_approved_decision,
    build_all_rejected_decision,
    build_selection_decision,
    extract_tool_approval_requests,
    parse_selection_tokens,
)
from app.cli.render import tool_summary, write, write_error, write_tool_call, write_tool_result


class DAgentsChatApp:
    def __init__(self, *, api_base: str, session_id: str | None, client_id: str | None, show_reasoning: bool) -> None:
        self.api_base = api_base
        self.initial_session_id = session_id
        self.client_id = client_id or f"cli-{uuid4().hex[:12]}"
        self.show_reasoning = show_reasoning
        self.session_id = ""
        self._client: DAgentsApiClient | None = None
        self._events: asyncio.Queue[StreamEvent] = asyncio.Queue()
        self._stream_task: asyncio.Task[None] | None = None
        self._prompt = PromptSession()
        self._line_open = False

    async def run(self) -> int:
        self._client = DAgentsApiClient(self.api_base)
        try:
            if not await self._client.health():
                raise RuntimeError(f"backend health check failed: {self.api_base}/health")
            self.session_id = await self._client.create_session(self.initial_session_id)
            self._stream_task = asyncio.create_task(self._pump_stream())
            write(f"Connected to {self.api_base}")
            write(f"Session: {self.session_id}")
            write("Type /help for commands, /exit to quit.")
            await self._input_loop()
            return 0
        except (KeyboardInterrupt, EOFError):
            write("\nbye")
            return 0
        except Exception as exc:
            write_error(f"dagents chat failed: {exc}")
            return 1
        finally:
            if self._stream_task is not None:
                self._stream_task.cancel()
                with suppress(asyncio.CancelledError):
                    await self._stream_task
            if self._client is not None:
                await self._client.close()

    async def _input_loop(self) -> None:
        while True:
            with patch_stdout():
                text = await self._prompt.prompt_async("dagents> ")
            value = text.strip()
            if not value:
                continue
            if value in {"/exit", "/quit", "exit", "quit"}:
                write("bye")
                return
            if value == "/help":
                self._show_help()
                continue
            if value == "/status":
                write(f"api={self.api_base} session={self.session_id} client={self.client_id}")
                continue
            if value.startswith("/"):
                write(f"Unknown command: {value}")
                continue
            assert self._client is not None
            await self._client.submit_message(session_id=self.session_id, client_id=self.client_id, content=text)
            await self._drain_turn()

    async def _pump_stream(self) -> None:
        assert self._client is not None
        try:
            async for event in self._client.stream_events(client_id=self.client_id):
                if event.session_id and event.session_id != self.session_id:
                    continue
                await self._events.put(event)
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            await self._events.put(
                StreamEvent(
                    event_type="error",
                    event_id=None,
                    payload={"session_id": self.session_id, "data": {"message": f"SSE stream failed: {exc}"}},
                )
            )
            await self._events.put(
                StreamEvent(event_type="done", event_id=None, payload={"session_id": self.session_id, "data": {}})
            )

    async def _drain_turn(self) -> None:
        skip_next_done = False
        while True:
            event = await self._events.get()
            data = event.data
            if event.event_type == "assistant":
                content = str(data.get("content") or "")
                if content:
                    write(content, end="")
                    self._line_open = True
            elif event.event_type == "reasoning" and self.show_reasoning:
                content = str(data.get("content") or "")
                if content:
                    self._ensure_newline()
                    write(f"[reasoning] {content}")
            elif event.event_type == "tool_call":
                self._ensure_newline()
                write_tool_call(data)
            elif event.event_type == "approval_required":
                self._ensure_newline()
                requests = extract_tool_approval_requests(data)
                decision = await self._prompt_for_approval(requests)
                assert self._client is not None
                await self._client.submit_resume(
                    session_id=self.session_id,
                    client_id=self.client_id,
                    resume_value=decision.to_resume_value(),
                )
                skip_next_done = True
            elif event.event_type == "tool_result":
                self._ensure_newline()
                write_tool_result(data)
            elif event.event_type == "error":
                self._ensure_newline()
                write_error(f"[error] {data.get('message') or 'unknown error'}")
            elif event.event_type == "done":
                if skip_next_done:
                    skip_next_done = False
                    continue
                self._ensure_newline()
                return

    async def _prompt_for_approval(self, requests: list[ToolApprovalRequest]):
        if not requests:
            write("Tool approval requested, but no tool calls were provided. Rejecting by default.")
            return build_all_rejected_decision([])
        write("Tool approval required:")
        for index, item in enumerate(requests, start=1):
            write(tool_summary(item, index))
        while True:
            with patch_stdout():
                value = await self._prompt.prompt_async("Approve tools? [a]ll/[r]eject/[s]elect/[d]etails: ")
            choice = value.strip()
            lowered = choice.lower()
            if lowered in {"a", "all", "approve", "yes", "y"}:
                return build_all_approved_decision(requests)
            if lowered in {"r", "reject", "no", "n"}:
                return build_all_rejected_decision(requests)
            if lowered in {"d", "details"}:
                self._show_approval_details(requests)
                continue
            if lowered.startswith("s"):
                selected_text = choice[1:].strip()
                if not selected_text:
                    with patch_stdout():
                        selected_text = await self._prompt.prompt_async(
                            "Approve which tools? Enter numbers or call IDs separated by spaces: "
                        )
                try:
                    approved_ids = parse_selection_tokens(selected_text, requests)
                    return build_selection_decision(requests, approved_ids)
                except ValueError as exc:
                    write(str(exc))
                    continue
            write("Please choose a, r, s, or d.")

    def _show_approval_details(self, requests: list[ToolApprovalRequest]) -> None:
        for index, item in enumerate(requests, start=1):
            write(tool_summary(item, index))
            if item.raw_arguments and item.raw_arguments != "{}":
                write(f"    raw: {item.raw_arguments}")

    def _show_help(self) -> None:
        write("Commands:")
        write("  /help      Show this help")
        write("  /status    Show API/session/client IDs")
        write("  /exit      Quit chat")
        write("Approval:")
        write("  a          Approve all pending tool calls")
        write("  r          Reject all pending tool calls")
        write("  s          Select approved tools by number or call ID; the rest are rejected")
        write("  d          Show tool details again")

    def _ensure_newline(self) -> None:
        if self._line_open:
            write()
            self._line_open = False


def run_chat(args: argparse.Namespace) -> int:
    app = DAgentsChatApp(
        api_base=args.api,
        session_id=args.session,
        client_id=args.client_id,
        show_reasoning=args.show_reasoning,
    )
    return asyncio.run(app.run())
