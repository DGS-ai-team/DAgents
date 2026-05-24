from __future__ import annotations

import asyncio

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.widgets import Footer, Header, Input, RichLog, Static

from app.cli.approval import ApprovalDecision, ToolApprovalRequest, build_all_rejected_decision
from app.cli.render import TranscriptKind, TranscriptUpdate
from app.cli.session_controller import SessionController
from app.cli.tui.approval_screen import ApprovalScreen


class DAgentsTuiApp(App[None]):
    """DAgents Textual 聊天 TUI。"""

    CSS = """
    Screen {
        layout: vertical;
    }
    #transcript {
        height: 1fr;
        border: solid $primary;
        margin: 0 1;
    }
    #status-bar {
        height: 1;
        margin: 0 1;
        color: $text-muted;
    }
    #prompt {
        margin: 0 1 1 1;
        border: tall $accent;
    }
    """

    BINDINGS = [
        Binding("ctrl+l", "clear_transcript", "Clear", show=True),
        Binding("ctrl+c", "quit", "Quit", show=True),
    ]

    def __init__(
        self,
        *,
        controller: SessionController,
    ) -> None:
        super().__init__()
        self._controller = controller
        self._assistant_buffer = ""

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        yield RichLog(id="transcript", highlight=True, markup=True, wrap=True)
        yield Static("", id="status-bar")
        yield Input(placeholder="Type a message or /help …", id="prompt")
        yield Footer()

    async def on_mount(self) -> None:
        """挂载后注册 controller 回调并聚焦输入框。"""
        self._controller.on_transcript(self._on_transcript)
        self._controller.on_status(self._on_status)
        self._controller.on_approval(self._on_approval)
        try:
            await self._controller.start()
        except Exception as exc:
            log = self.query_one("#transcript", RichLog)
            log.write(f"[red]Failed to connect: {exc}[/red]")
            return
        self.query_one("#prompt", Input).focus()

    async def on_unmount(self) -> None:
        """退出时停止 controller 后台任务。"""
        await self._controller.stop()

    def _on_transcript(self, update: TranscriptUpdate) -> None:
        """将 controller transcript 更新调度到 UI 线程。"""
        self.call_later(self._apply_transcript, update)

    def _on_status(self, text: str) -> None:
        self.call_later(self._apply_status, text)

    async def _on_approval(self, requests: list[ToolApprovalRequest]) -> ApprovalDecision:
        """弹出审批 Modal 并返回决策。"""
        if not requests:
            return build_all_rejected_decision([])
        return await self.push_screen_wait(ApprovalScreen(requests))

    def _apply_transcript(self, update: TranscriptUpdate) -> None:
        log = self.query_one("#transcript", RichLog)
        if update.kind == TranscriptKind.ASSISTANT_DELTA:
            log.write(update.text, end="")
            self._assistant_buffer += update.text
        elif update.kind == TranscriptKind.ASSISTANT_END:
            if self._assistant_buffer:
                log.write("")
                self._assistant_buffer = ""
        elif update.kind == TranscriptKind.ERROR:
            log.write(f"[red]{update.text}[/red]")
        else:
            log.write(update.text)

    def _apply_status(self, text: str) -> None:
        self.query_one("#status-bar", Static).update(text)

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """处理用户输入：命令或发消息。"""
        if event.input.id != "prompt":
            return
        value = event.value.strip()
        event.input.value = ""
        if not value:
            return
        if value in {"/exit", "/quit", "exit", "quit"}:
            self.exit()
            return
        if value == "/help":
            self._show_help()
            return
        if value == "/status":
            self._apply_status(
                f"api={self._controller.api_base} session={self._controller.session_id} "
                f"client={self._controller.client_id} sse="
                f"{'connected' if self._controller.sse_connected else 'disconnected'}"
            )
            return
        if value == "/bind-triggers":
            await self._bind_triggers()
            return
        if value.startswith("/"):
            log = self.query_one("#transcript", RichLog)
            log.write(f"[yellow]Unknown command: {value}[/yellow]")
            return
        log = self.query_one("#transcript", RichLog)
        log.write(f"[bold cyan]you>[/bold cyan] {value}")
        try:
            await self._controller.submit_message(value)
            await self._controller.wait_user_turn()
        except Exception as exc:
            log.write(f"[red]send failed: {exc}[/red]")

    async def _bind_triggers(self) -> None:
        log = self.query_one("#transcript", RichLog)
        try:
            bound = await self._controller.bind_triggers_to_client()
            log.write(f"Bound {bound} trigger(s) to client {self._controller.client_id}")
        except Exception as exc:
            log.write(f"[red]bind-triggers failed: {exc}[/red]")

    def _show_help(self) -> None:
        log = self.query_one("#transcript", RichLog)
        for line in (
            "Commands:",
            "  /help            Show this help",
            "  /status          Show API/session/client IDs",
            "  /bind-triggers   Bind session triggers to this client_id",
            "  /exit            Quit chat",
            "Keys: Ctrl+L clear transcript, Ctrl+C quit",
        ):
            log.write(line)

    def action_clear_transcript(self) -> None:
        self.query_one("#transcript", RichLog).clear()
