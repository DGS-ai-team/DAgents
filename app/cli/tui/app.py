from __future__ import annotations

import asyncio
import re
import shlex
import time
from typing import Any

from rich.cells import cell_len, set_cell_size
from rich.console import Group, RenderableType
from rich.markup import escape
from rich.markdown import Markdown
from rich.panel import Panel
from rich.syntax import Syntax
from rich.table import Table
from rich.text import Text
from textual import events
from textual.app import App, ComposeResult
from textual.geometry import Size
from textual.widgets import RichLog, Static

from app.cli.approval import (
    ApprovalCancelled,
    ApprovalDecision,
    ToolApprovalRequest,
    build_all_rejected_decision,
    build_approval_decision_from_map,
    clamp_menu_selection_index,
    extract_tool_approval_requests,
)
from app.cli.child_agent import (
    approval_header,
    format_child_agents_list,
    format_temporary_agent_tool_title,
    parse_temporary_agent_tool_result,
)
from app.cli.render import TranscriptKind, TranscriptUpdate, format_inline_usage, parse_usage_round, sanitize_inline_tool_arg
from app.cli.session_controller import PendingHITL, SessionController
from app.cli.tool_calls import normalize_tool_call_item, tool_call_purpose, tool_display_name
from app.cli.user_information import (
    UserInformationAnswer,
    UserInformationCancelled,
    UserInformationRequest,
    build_answer_from_options,
    build_answer_from_text,
    extract_user_information_request,
)
from app.cli.tui.prompt_text_area import PromptTextArea
from app.cli.tui.welcome_panel import build_welcome_panel

_HELP_HINT = "Type /help for commands, /exit to quit.  Enter 发送，Shift+Enter 换行"
# bash command 在括号内可展示的 cell 上限；超出则在标题下方用代码框展示全文。
_BASH_INLINE_COMMAND_MAX_CELLS = 56

# Transcript 圆点颜色：按消息类型区分
_DOT_USER = "blue"
_DOT_ASSISTANT_STREAM = "yellow blink"
_DOT_ASSISTANT_DONE = "green"
_DOT_REASONING = "orange1"
_DOT_TOOL_PENDING = "yellow blink"
_DOT_TOOL_RESULT = "cyan"
_DOT_STATUS_ACTIVE = "yellow blink"
_DOT_STATUS_DONE = "green dim"
_DOT_USER_INFO = "magenta"
_USER_INFORMATION_TOOL_NAME = "ask_user_information"


class DAgentsTuiApp(App[None]):
    """DAgents Textual 聊天 TUI。"""

    # 固定暗色主题；不跟随终端、不做动态切换。
    theme = "textual-dark"

    CSS = """
    Screen {
        layout: vertical;
    }
    #top-status-bar {
        dock: top;
        height: 1;
        width: 100%;
        padding: 0 1;
        content-align: right middle;
        color: $text-muted;
        background: transparent;
        border: none;
    }
    #transcript {
        height: 1fr;
        margin: 0 1 1 1;
        background: transparent;
        border: none;
    }
    #context-view {
        height: 1fr;
        margin: 0 1 1 1;
        background: transparent;
        border: none;
    }
    RichLog {
        background: transparent;
        border: none;
    }
    #input-strip {
        height: 1;
        margin: 0 1 0 1;
        padding: 0 1;
        color: $text-muted;
        content-align: left middle;
        background: transparent;
        border: none;
    }
    #prompt {
        height: 4;
        min-height: 4;
        max-height: 4;
        margin: 0 1;
        padding: 0 1;
        width: 100%;
        border: tall $border-blurred;
        background: $surface;
        color: $foreground;
        overflow-y: auto;
    }
    #prompt:focus {
        border: tall $border;
        background-tint: $foreground 5%;
    }
    #prompt .text-area--gutter {
        display: none;
    }
    #help-hint {
        dock: bottom;
        height: 1;
        margin: 0 1 1 1;
        padding: 0 2;
        color: $foreground 40%;
        content-align: left middle;
        background: transparent;
        border: none;
    }
    Static {
        background: transparent;
        border: none;
    }
    """

    def __init__(
        self,
        *,
        controller: SessionController,
    ) -> None:
        super().__init__()
        self._controller = controller
        self._assistant_buffer = ""
        self._assistant_stream_start: int | None = None
        self._pending_round_usage_suffix: str | None = None
        # 欢迎 Panel 写入后 RichLog 行数，流式 assistant 回退不得早于该位置。
        self._transcript_base_lines = 0
        self._approval_future: asyncio.Future[ApprovalDecision] | None = None
        self._approval_requests: list[ToolApprovalRequest] = []
        self._approval_selected_index = 0
        self._approval_decisions: dict[str, bool] = {}
        self._approval_block: dict[str, int] | None = None
        self._approval_raw_data: dict[str, Any] | None = None
        self._hitl_busy = False
        self._hitl_task: asyncio.Task[None] | None = None
        self._user_info_future: asyncio.Future[UserInformationAnswer] | None = None
        self._user_info_request: UserInformationRequest | None = None
        self._user_info_selected_index = 0
        self._user_info_selected_ids: set[str] = set()
        self._user_info_block: dict[str, int] | None = None
        self._pending_tools: dict[str, dict[str, Any]] = {}
        self._tool_results: dict[str, dict[str, Any]] = {}
        self._tool_result_counter = 0
        self._status_lines: dict[str, dict[str, Any]] = {}
        self._submit_content_seen = False
        self._cancel_task: asyncio.Task[None] | None = None
        self._context_mode = False
        # assistant 段结束后待插入的空行（tool_call 紧随其后会取消）。
        self._pending_segment_gap = False
        # 工具批次结束后，下一段 prefilling/thinking/assistant 前插入一行。
        self._needs_gap_after_tools = False

    def compose(self) -> ComposeResult:
        """创建 TUI 组件层次结构。"""
        yield Static(id="top-status-bar", markup=True)
        yield RichLog(id="transcript", highlight=True, markup=True, wrap=True)
        yield RichLog(id="context-view", highlight=True, markup=True, wrap=True)
        yield Static("", id="input-strip", markup=True)
        yield PromptTextArea(
            id="prompt",
            placeholder="",
            show_line_numbers=False,
            soft_wrap=True,
        )
        yield Static(_HELP_HINT, id="help-hint")

    async def on_mount(self) -> None:
        """挂载后注册 controller 回调并聚焦输入框。"""
        self._controller.on_transcript(self._on_transcript)
        self._controller.on_status(self._on_status)
        self._controller.on_hitl_pending(self._on_hitl_pending)
        self._controller.on_child_strip(self._on_child_strip)
        self._refresh_input_strip()
        self._apply_top_status(connected=False)
        try:
            await self._controller.start()
        except Exception as exc:
            log = self._transcript_log()
            log.write(f"[red]Failed to connect: {exc}[/red]")
            self._apply_top_status(connected=False)
            return
        self._write_welcome_panel()
        self.query_one("#context-view", RichLog).display = False
        self._apply_top_status()
        self.query_one("#prompt", PromptTextArea).focus()

    async def on_unmount(self) -> None:
        """退出时停止 controller 后台任务。"""
        self._cancel_status_lines()
        await self._controller.stop()

    def _transcript_log(self) -> RichLog:
        return self.query_one("#transcript", RichLog)

    def _context_log(self) -> RichLog:
        """获取 context 专用 RichLog。

        逻辑：
        1. 通过 Textual id 查询 `#context-view`；
        2. 交给 `/context` 进入/退出逻辑控制显示状态。

        关键边界：
        - 不创建新 widget，只复用 compose 中已挂载的视图。
        """
        return self.query_one("#context-view", RichLog)

    def _write_welcome_panel(self) -> None:
        """连接成功后向 RichLog 写入一次性欢迎 Panel。

        副作用：更新 ``_transcript_base_lines`` 供流式回退边界使用。
        """
        log = self._transcript_log()
        panel_width = int(getattr(log.size, "width", 0) or 0)
        log.write(
            build_welcome_panel(
                api_base=self._controller.api_base,
                session_id=self._controller.session_id,
                width=panel_width if panel_width > 0 else None,
            ),
            expand=True,
        )
        self._transcript_base_lines = len(log.lines)

    def _on_transcript(self, update: TranscriptUpdate) -> None:
        """将 controller transcript 更新调度到 UI 线程。"""
        self.call_later(self._apply_transcript, update)

    def _on_status(self, _text: str) -> None:
        """SSE/连接变化时刷新顶栏。"""
        self.call_later(self._apply_top_status)

    def _on_hitl_pending(self) -> None:
        """HITL 入队后触发非阻塞处理。"""
        self.call_later(self._process_hitl_queue)

    def _on_child_strip(self) -> None:
        """子 Agent 状态变更时刷新输入框上方状态条。"""
        self.call_later(self._refresh_input_strip)

    def _refresh_input_strip(self) -> None:
        """刷新 #input-strip 文案（子 Agent / HITL 队列 + 右侧 token 统计）。"""
        strip = self.query_one("#input-strip", Static)
        left = self._controller.child_tracker.input_strip_text(self._controller.hitl_queue_len())
        right = self._controller.input_strip_token_text()
        if right:
            width = int(getattr(strip.size, "width", 0) or 0)
            if width > len(left) + len(right) + 1:
                gap = width - len(left) - len(right)
                line = f"{left}{' ' * gap}{right}"
            else:
                line = f"{left}  ·  {right}"
        else:
            line = left
        strip.update(f"[dim]{escape(line)}[/dim]")

    def _process_hitl_queue(self) -> None:
        """空闲时展示并处理队首 HITL（不阻塞 SSE render loop）。"""
        if self._hitl_busy:
            return
        if self._approval_future is not None and not self._approval_future.done():
            return
        if self._user_info_future is not None and not self._user_info_future.done():
            return
        item = self._controller.peek_hitl()
        if item is None:
            return
        self._hitl_busy = True
        if item.kind == "approval":
            self._hitl_task = asyncio.create_task(self._run_approval_hitl(item))
        else:
            self._hitl_task = asyncio.create_task(self._run_user_info_hitl(item))

    async def _run_approval_hitl(self, item: PendingHITL) -> None:
        """展示审批 UI，完成后异步 submit resume。"""
        requests = extract_tool_approval_requests(item.data)
        if not requests:
            try:
                await self._controller.complete_hitl_approval(build_all_rejected_decision([]))
            except Exception as exc:  # noqa: BLE001
                self._transcript_log().write(f"[red]approval submit failed: {exc}[/red]")
            finally:
                self._hitl_busy = False
                self._refresh_input_strip()
                self.call_later(self._process_hitl_queue)
            return
        loop = asyncio.get_running_loop()
        ready: asyncio.Future[asyncio.Future[ApprovalDecision]] = loop.create_future()
        self._approval_raw_data = item.data

        def _begin() -> None:
            try:
                future = self._begin_approval_ui(list(requests))
            except Exception as exc:  # noqa: BLE001
                if not ready.done():
                    ready.set_exception(exc)
            else:
                if not ready.done():
                    ready.set_result(future)

        self.call_later(_begin)
        try:
            approval_future = await ready
            decision = await approval_future
        except ApprovalCancelled:
            try:
                await self._controller.complete_hitl_approval(
                    build_all_rejected_decision(list(requests))
                )
            except Exception as exc:  # noqa: BLE001
                self._transcript_log().write(f"[red]approval reject failed: {exc}[/red]")
            self.call_later(self._end_approval_ui)
            self._hitl_busy = False
            self._refresh_input_strip()
            self.call_later(self._process_hitl_queue)
            return
        except Exception as exc:  # noqa: BLE001
            self._controller.discard_hitl_head()
            self.call_later(self._end_approval_ui)
            self._transcript_log().write(f"[red]approval ui failed: {exc}[/red]")
            self._hitl_busy = False
            self._refresh_input_strip()
            self.call_later(self._process_hitl_queue)
            return
        finally:
            self._approval_raw_data = None
        cleanup_done: asyncio.Future[None] = loop.create_future()

        def _cleanup() -> None:
            try:
                self._end_approval_ui()
            except Exception as exc:  # noqa: BLE001
                if not cleanup_done.done():
                    cleanup_done.set_exception(exc)
            else:
                if not cleanup_done.done():
                    cleanup_done.set_result(None)

        self.call_later(_cleanup)
        await cleanup_done
        try:
            await self._controller.complete_hitl_approval(decision)
        except Exception as exc:  # noqa: BLE001
            self._transcript_log().write(f"[red]approval submit failed: {exc}[/red]")
        finally:
            self._hitl_busy = False
            self._refresh_input_strip()
            self.call_later(self._process_hitl_queue)

    async def _run_user_info_hitl(self, item: PendingHITL) -> None:
        """展示用户询问 UI，完成后异步 submit resume。"""
        request = extract_user_information_request(item.data)
        if request is None:
            self._controller.discard_hitl_head()
            self._hitl_busy = False
            self.call_later(self._process_hitl_queue)
            return
        loop = asyncio.get_running_loop()
        ready: asyncio.Future[asyncio.Future[UserInformationAnswer]] = loop.create_future()

        def _begin() -> None:
            try:
                future = self._begin_user_info_ui(request)
            except Exception as exc:  # noqa: BLE001
                if not ready.done():
                    ready.set_exception(exc)
            else:
                if not ready.done():
                    ready.set_result(future)

        self.call_later(_begin)
        try:
            answer_future = await ready
            answer = await answer_future
        except UserInformationCancelled:
            request = self._user_info_request
            if request is not None:
                try:
                    await self._controller.complete_hitl_user_info(
                        UserInformationAnswer(
                            tool_call_id=request.tool_call_id,
                            answer="",
                            selected_options=[],
                            cancelled=True,
                        )
                    )
                except Exception as exc:  # noqa: BLE001
                    self._transcript_log().write(f"[red]user info cancel failed: {exc}[/red]")
            else:
                self._controller.discard_hitl_head()
            self.call_later(self._end_user_info_ui)
            self._hitl_busy = False
            self.call_later(self._process_hitl_queue)
            return
        cleanup_done: asyncio.Future[None] = loop.create_future()

        def _cleanup() -> None:
            try:
                self._end_user_info_ui()
            except Exception as exc:  # noqa: BLE001
                if not cleanup_done.done():
                    cleanup_done.set_exception(exc)
            else:
                if not cleanup_done.done():
                    cleanup_done.set_result(None)

        self.call_later(_cleanup)
        await cleanup_done
        try:
            await self._controller.complete_hitl_user_info(answer)
        except Exception as exc:  # noqa: BLE001
            self._transcript_log().write(f"[red]user info submit failed: {exc}[/red]")
        finally:
            self._hitl_busy = False
            self.call_later(self._process_hitl_queue)

    def _begin_approval_ui(self, requests: list[ToolApprovalRequest]) -> asyncio.Future[ApprovalDecision]:
        """在 UI 队列中初始化审批状态并立即刷新 RichLog。

        逻辑：
        1. 创建审批 future 与本轮请求快照；
        2. 隐藏输入框，让 App 捕获审批快捷键；
        3. 写入审批选项并刷新布局/滚动到底部。

        副作用说明：
        - 修改 `_approval_*` 状态、TextArea display 与 RichLog 内容。
        """
        prompt = self._prompt_area()
        self._approval_requests = list(requests)
        self._approval_selected_index = 0
        self._approval_decisions = {}
        self._approval_future = asyncio.get_running_loop().create_future()

        # 审批期间隐藏并只读输入框，避免 Enter 误触 submit_prompt；快捷键由 App 捕获。
        prompt.display = False
        prompt.read_only = True
        self._refresh_approval_tool_blocks()
        self._write_approval_block()
        self._refresh_approval_layout()
        self._transcript_log().focus()
        return self._approval_future

    def _end_approval_ui(self) -> None:
        """在 UI 队列中清理审批状态，并恢复输入框。"""
        prompt = self._prompt_area()
        self._approval_future = None
        self._approval_requests = []
        self._approval_selected_index = 0
        self._approval_decisions = {}
        self._delete_approval_block()
        self._reset_pending_tools_execution_clock()
        self._refresh_all_pending_tool_blocks()
        prompt.read_only = False
        prompt.display = True
        self._refresh_approval_layout()
        prompt.focus()

    def _begin_user_info_ui(self, request: UserInformationRequest) -> asyncio.Future[UserInformationAnswer]:
        """初始化用户询问 UI 并返回等待 future。"""
        prompt = self._prompt_area()
        self._user_info_request = request
        self._user_info_selected_index = 0
        self._user_info_selected_ids = set()
        self._user_info_future = asyncio.get_running_loop().create_future()
        self._stop_user_info_pending_animation(request.tool_call_id)
        self._write_user_info_merged_block()
        if request.options:
            prompt.display = False
            prompt.read_only = True
            self._transcript_log().focus()
        else:
            prompt.display = True
            if request.placeholder:
                prompt.placeholder = request.placeholder
            prompt.focus()
        self._refresh_user_info_layout()
        return self._user_info_future

    def _end_user_info_ui(self) -> None:
        """清理用户询问 UI 状态。"""
        prompt = self._prompt_area()
        self._user_info_future = None
        self._user_info_request = None
        self._user_info_selected_index = 0
        self._user_info_selected_ids = set()
        self._delete_user_info_block()
        prompt.placeholder = ""
        prompt.read_only = False
        prompt.display = True
        self._refresh_user_info_layout()
        prompt.focus()

    def _refresh_user_info_layout(self) -> None:
        """用户询问块变更后刷新布局。"""
        log = self._transcript_log()
        log.refresh(layout=True)
        self._prompt_area().refresh(layout=True)
        self.refresh(layout=True)
        scroll_end = getattr(log, "scroll_end", None)
        if callable(scroll_end):
            try:
                scroll_end(animate=False)
            except TypeError:
                scroll_end()

    def _refresh_approval_layout(self) -> None:
        """审批块增删后强制刷新布局并滚动到底部。"""
        log = self._transcript_log()
        log.refresh(layout=True)
        self._prompt_area().refresh(layout=True)
        self.refresh(layout=True)
        scroll_end = getattr(log, "scroll_end", None)
        if callable(scroll_end):
            try:
                scroll_end(animate=False)
            except TypeError:
                scroll_end()

    async def on_key(self, event: events.Key) -> None:
        """拦截全局快捷键：Esc 取消当前 turn，审批期间处理 ↑/↓/Enter。

        逻辑：
        1. Esc 在普通输出与审批等待中都触发服务端 cancel；
        2. 审批等待中 Esc 还会让 approval future 抛出 `ApprovalCancelled`，避免继续 submit resume；
        3. 其它按键仅在审批期间被拦截。
        """
        if event.key == "escape":
            event.stop()
            event.prevent_default()
            if self._context_mode:
                self._exit_context_view()
                return
            self._cancel_current_turn()
            return
        if self._user_info_future is not None and not self._user_info_future.done():
            request = self._user_info_request
            if request is not None and request.options:
                if event.key == "up":
                    event.stop()
                    event.prevent_default()
                    self._move_user_info_selection(-1)
                elif event.key == "down":
                    event.stop()
                    event.prevent_default()
                    self._move_user_info_selection(1)
                elif event.key == "space" and request.allow_multiple:
                    event.stop()
                    event.prevent_default()
                    self._toggle_user_info_selection()
                elif event.key == "enter":
                    event.stop()
                    event.prevent_default()
                    self.confirm_user_info_choice()
            return
        if self._approval_future is None or self._approval_future.done():
            return
        if event.key in {"up", "down"}:
            event.stop()
            event.prevent_default()
            delta = -1 if event.key == "up" else 1
            self._move_approval_selection(delta)
        elif event.key in {"y", "Y"}:
            event.stop()
            event.prevent_default()
            self._set_approval_choice_for_current(approved=True)
        elif event.key in {"n", "N"}:
            event.stop()
            event.prevent_default()
            self._set_approval_choice_for_current(approved=False)
        elif event.key == "enter":
            event.stop()
            event.prevent_default()
            self.confirm_approval_choice()

    def _cancel_current_turn(self) -> None:
        """启动当前 turn 取消任务，并同步结束本地审批等待。

        关键边界：
        - 重复 Esc 只保留一个在途 cancel 请求；
        - 审批 future 使用异常结束，controller 捕获后不会 submit resume。
        """
        if self._cancel_task is not None and not self._cancel_task.done():
            return
        self._abort_local_hitl_for_user_message()
        self._controller.clear_hitl_queue()
        self._hitl_busy = False
        self._cancel_tool_pending_tasks()
        self._cancel_status_lines()
        self._finish_assistant_stream(self._transcript_log())
        self._cancel_task = asyncio.create_task(self._cancel_current_turn_request())

    def _abort_local_hitl_for_user_message(self) -> None:
        """关闭本地 HITL UI；新用户消息或 Esc 会打断 server pending，勿再 submit 旧 call_id。"""
        if self._approval_future is not None and not self._approval_future.done():
            self._approval_future.set_exception(ApprovalCancelled())
            self.call_later(self._end_approval_ui)
        if self._user_info_future is not None and not self._user_info_future.done():
            self._user_info_future.set_exception(UserInformationCancelled())
            self.call_later(self._end_user_info_ui)
        self._hitl_busy = False

    async def _cancel_current_turn_request(self) -> None:
        """调用服务端 cancel 接口；失败时在 transcript 中提示。"""
        try:
            await self._controller.cancel_current_turn()
        except Exception as exc:  # noqa: BLE001
            self._transcript_log().write(f"[red]cancel failed: {exc}[/red]")

    def confirm_approval_choice(self) -> None:
        """确认当前工具的审批选择，必要时切换到下一个工具。

        逻辑：
        1. 读取当前工具的两个选项（同意/不同意）；
        2. 保存本工具决策；
        3. 若还有未决工具，则删除旧审批块并写到下一个工具下方；否则唤醒 future。

        关键边界：
        - 每次 Enter 只确认一个工具，避免多工具审批选项同时出现在同一块里。
        """
        if self._approval_future is None or self._approval_future.done():
            return
        options = self._approval_options()
        if not options:
            self._finish_approval()
            return
        call_id, approved = options[self._approval_selected_index]
        self._approval_decisions[call_id] = approved
        if len(self._approval_decisions) >= len(self._approval_requests):
            self._finish_approval()
            return
        self._approval_selected_index = 0
        # 当前工具确认后，折叠其详情并移动到下一个工具调用块下方。
        self._delete_approval_block()
        self._refresh_approval_tool_blocks()
        self._write_approval_block()
        self._refresh_approval_layout()

    def _finish_approval(self) -> None:
        """所有工具均完成单独审批后，构造最终决策。"""
        decision = build_approval_decision_from_map(self._approval_requests, self._approval_decisions)
        if self._approval_future is not None and not self._approval_future.done():
            self._approval_future.set_result(decision)

    def _set_approval_choice_for_current(self, *, approved: bool) -> None:
        """为当前待审批工具写入决策并推进到下一项或结束。"""
        item = self._current_approval_request()
        if item is None:
            self._finish_approval()
            return
        self._approval_decisions[item.call_id] = approved
        if len(self._approval_decisions) >= len(self._approval_requests):
            self._finish_approval()
            return
        self._approval_selected_index = 0
        self._delete_approval_block()
        self._refresh_approval_tool_blocks()
        self._write_approval_block()
        self._refresh_approval_layout()

    def _stop_user_info_pending_animation(self, tool_call_id: str) -> None:
        """停止 ask_user_information 占位行的耗时动画，避免与合并块重复刷新。"""
        pending = self._pending_tools.get(tool_call_id)
        if pending is None:
            return
        task = pending.get("status_task")
        if isinstance(task, asyncio.Task):
            task.cancel()
        pending.pop("status_task", None)

    def _user_info_block_text(self) -> str:
        request = self._user_info_request
        if request is None:
            return ""
        lines: list[str] = [
            "[bold cyan]Agent 询问[/bold cyan]",
            escape(request.question),
        ]
        if not request.options:
            return "\n".join(lines)
        lines.append("")
        for index, option in enumerate(request.options):
            selected = option.id in self._user_info_selected_ids
            cursor = "[cyan]●[/cyan]" if index == self._user_info_selected_index else " "
            marker = "[x]" if selected else "[ ]"
            style = "bold" if index == self._user_info_selected_index else "dim"
            lines.append(f"   {cursor} [{style}]{marker} {escape(option.label)}[/{style}]")
        if request.allow_multiple:
            lines.append("   [dim]↑/↓ 选择，Space 切换，Enter 提交[/dim]")
        else:
            lines.append("   [dim]↑/↓ 选择，Enter 确认[/dim]")
        return "\n".join(lines)

    def _write_user_info_merged_block(self) -> None:
        """将 tool_call 占位行重写为「Agent 询问 + 问题 + 选项」单块。"""
        request = self._user_info_request
        if request is None:
            return
        content = self._user_info_block_text()
        block = self._message_block(_DOT_USER_INFO, content, escape_text=False)
        pending = self._pending_tools.get(request.tool_call_id)
        if pending is not None:
            start, end = self._replace_log_block(int(pending["start"]), int(pending["end"]), block)
            pending["start"] = start
            pending["end"] = end
            pending["summary"] = "Agent 询问"
        else:
            log = self._transcript_log()
            start = len(log.lines)
            self._log_write_block(log, block)
            end = len(log.lines)
        self._user_info_block = {"start": start, "end": end}

    def _render_user_info_block(self) -> None:
        if self._user_info_block is None:
            return
        start, end = self._replace_log_block(
            self._user_info_block["start"],
            self._user_info_block["end"],
            self._message_block(_DOT_USER_INFO, self._user_info_block_text(), escape_text=False),
        )
        self._user_info_block = {"start": start, "end": end}
        request = self._user_info_request
        if request is not None:
            pending = self._pending_tools.get(request.tool_call_id)
            if pending is not None:
                pending["start"] = start
                pending["end"] = end

    def _delete_user_info_block(self) -> None:
        if self._user_info_block is None:
            return
        self._delete_log_block(self._user_info_block["start"], self._user_info_block["end"])
        self._user_info_block = None

    def _move_user_info_selection(self, delta: int) -> None:
        request = self._user_info_request
        if request is None or not request.options:
            return
        count = len(request.options)
        if count <= 0:
            return
        self._user_info_selected_index = clamp_menu_selection_index(
            self._user_info_selected_index,
            delta,
            count,
        )
        self._render_user_info_block()

    def _toggle_user_info_selection(self) -> None:
        request = self._user_info_request
        if request is None or not request.options:
            return
        option = request.options[self._user_info_selected_index]
        if option.id in self._user_info_selected_ids:
            self._user_info_selected_ids.remove(option.id)
        else:
            self._user_info_selected_ids.add(option.id)
        self._render_user_info_block()

    def confirm_user_info_choice(self) -> None:
        """提交选项式用户回答。"""
        if self._user_info_future is None or self._user_info_future.done():
            return
        request = self._user_info_request
        if request is None:
            self._finish_user_info(
                UserInformationAnswer(
                    tool_call_id="",
                    answer="",
                    selected_options=[],
                    cancelled=True,
                )
            )
            return
        if not request.options:
            return
        if request.allow_multiple:
            selected_ids = sorted(self._user_info_selected_ids)
        else:
            option = request.options[self._user_info_selected_index]
            selected_ids = [option.id]
        if request.required and not selected_ids:
            return
        answer = build_answer_from_options(request, selected_ids)
        self._finish_user_info(answer)

    def _submit_user_info_text_answer(self, text: str) -> None:
        """提交自由文本用户回答。"""
        if self._user_info_future is None or self._user_info_future.done():
            return
        request = self._user_info_request
        if request is None or request.options:
            return
        value = str(text or "").strip()
        if request.required and not value:
            return
        self._finish_user_info(build_answer_from_text(request, value))

    def _finish_user_info(self, answer: UserInformationAnswer) -> None:
        """唤醒用户询问 future。"""
        if self._user_info_future is not None and not self._user_info_future.done():
            self._user_info_future.set_result(answer)

    def _finish_assistant_stream(self, log: RichLog) -> None:
        """结束当前 assistant 流式段并重置缓冲。"""
        self._assistant_buffer = ""
        self._assistant_stream_start = None

    def _rewind_assistant_stream_lines(self, log: RichLog) -> None:
        """回退 RichLog 中当前流式 assistant 已写入的行，便于整段重写。

        不得截断欢迎 Panel 所在行（``_transcript_base_lines`` 之前）。
        """
        if self._assistant_stream_start is None:
            return
        floor = self._transcript_base_lines
        start = max(self._assistant_stream_start, floor)
        if len(log.lines) <= start:
            return
        log.lines = log.lines[:start]
        log._line_cache.clear()
        log._widest_line_width = max(
            (sum(segment.cell_length for segment in strip) for strip in log.lines),
            default=0,
        )
        log.virtual_size = Size(log._widest_line_width, len(log.lines))
        log.refresh()

    def _dot_column_block(self, dot_style: str, body: RenderableType) -> Table:
        """圆点列 + 正文列的统一布局（assistant / 状态 / 工具共用）。"""
        grid = Table.grid(expand=True, padding=(0, 1))
        grid.add_column(width=1, no_wrap=True)
        grid.add_column(ratio=1, overflow="fold")
        grid.add_row(Text("●", style=dot_style), body)
        return grid

    def _text_block_body(self, text: str, *, escape_text: bool) -> RenderableType:
        lines = text.splitlines() or [""]
        if escape_text:
            lines = [escape(line) for line in lines]
            if len(lines) == 1:
                return Text(lines[0])
            return Group(*[Text(line) for line in lines])
        if len(lines) == 1:
            return Text.from_markup(lines[0])
        return Group(*[Text.from_markup(line) for line in lines])

    def _message_block(self, dot_style: str, text: str, *, escape_text: bool = True) -> Table:
        """按固定圆点列格式化 transcript 消息，保证与工具块横向对齐。"""
        return self._dot_column_block(dot_style, self._text_block_body(text, escape_text=escape_text))

    def _content_column_width(self) -> int:
        """正文列可用 cell 宽度（扣除圆点列与间距）。"""
        return max(20, self._transcript_content_width() - 2)

    def _right_aligned_usage_row(self, suffix: str) -> Text:
        """usage 独占一行并右对齐。"""
        width = self._content_column_width()
        pad = max(0, width - cell_len(suffix))
        return Text.assemble((" " * pad, ""), (suffix, "bright_black"))

    def _merge_last_line_with_usage(
        self, last_line: str, suffix: str
    ) -> tuple[RenderableType, RenderableType | None]:
        """末行与 usage 同行（放得下）或拆成两行（末行 + 右对齐 usage）。"""
        width = self._content_column_width()
        if cell_len(last_line) + cell_len(suffix) <= width:
            pad = max(1, width - cell_len(last_line) - cell_len(suffix))
            return (
                Text.assemble(
                    (last_line, ""),
                    (" " * pad, ""),
                    (suffix, "bright_black"),
                ),
                None,
            )
        return Text(last_line), self._right_aligned_usage_row(suffix)

    def _assistant_body_with_usage(self, text: str, suffix: str) -> RenderableType:
        """assistant 完成态正文 + 右对齐 usage（末行放得下则同行）。"""
        stripped = text.rstrip("\n")
        parts = stripped.split("\n")
        if len(parts) == 1:
            line_render, extra = self._merge_last_line_with_usage(parts[0], suffix)
            if extra is None:
                return line_render
            return Group(line_render, extra)
        head = "\n".join(parts[:-1])
        last_render, extra = self._merge_last_line_with_usage(parts[-1], suffix)
        body: RenderableType = Group(Markdown(head), last_render)
        if extra is not None:
            body = Group(body, extra)
        return body

    def _log_write_block(self, log: RichLog, content: str | RenderableType) -> None:
        if isinstance(content, str):
            log.write(content)
        else:
            log.write(content, expand=True)

    def _assistant_block(self, text: str, *, complete: bool, usage_suffix: str | None = None) -> RenderableType:
        """格式化 assistant 消息。

        流式中使用普通文本，完成后用 Markdown 渲染正文；左侧圆点单独占一列以保持对齐。
        完成态正文列使用 overflow=fold，避免 Rich Table 默认 ellipsis 截断长行。
        """
        suffix = usage_suffix
        if complete and suffix is None:
            suffix = self._pending_round_usage_suffix
        if not complete:
            return self._message_block(_DOT_ASSISTANT_STREAM, text)
        if suffix:
            stripped = text.rstrip("\n")
            self._pending_round_usage_suffix = None
            body = self._assistant_body_with_usage(stripped, suffix)
            return self._dot_column_block(_DOT_ASSISTANT_DONE, body)
        return self._dot_column_block(_DOT_ASSISTANT_DONE, Markdown(text))

    def _write_assistant_block(
        self,
        log: RichLog,
        text: str,
        *,
        complete: bool,
        usage_suffix: str | None = None,
    ) -> None:
        """写入 assistant 块；Renderables 需 expand 才能按 RichLog 宽度折行。"""
        log.write(self._assistant_block(text, complete=complete, usage_suffix=usage_suffix), expand=True)

    def _event_block(self, text: str) -> str | Table:
        """格式化非流式事件：工具消息使用圆点，普通系统行保持原样。"""
        if text.startswith("[reasoning]"):
            return self._message_block(_DOT_REASONING, text)
        return text

    def _append_message_gap(self) -> None:
        """在消息块之间恢复一行间隔；不用于输入框间距。"""
        log = self._transcript_log()
        if log.lines:
            log.write("")

    def _last_log_line_is_blank(self, log: RichLog) -> bool:
        if not log.lines:
            return True
        text = "".join(str(segment) for segment in log.lines[-1])
        return text.strip() == ""

    def _append_message_gap_if_needed(self, log: RichLog | None = None) -> None:
        """上一条非空行后插入空行，避免重复空行。"""
        if log is None:
            log = self._transcript_log()
        if log.lines and not self._last_log_line_is_blank(log):
            log.write("")

    def _status_text(self, name: str, *, done: bool = False) -> Table:
        """生成 prefilling/thinking/compression_blocking 状态行文本。"""
        state = self._status_lines.get(name)
        started_at = float(state.get("started_at", time.monotonic())) if state else time.monotonic()
        raw_elapsed = max(0.0, time.monotonic() - started_at)
        elapsed = int(raw_elapsed)
        label = name
        if name == "compression_blocking":
            label = "compressing context (blocking)"
        if done:
            return self._message_block(_DOT_STATUS_DONE, f"{label}... {elapsed}s done")
        frame = int(raw_elapsed * 2) % 3
        # 省略号槽位固定 3 个字符，避免动画刷新时秒数左右抖动。
        dots = ("." * (frame + 1)).ljust(3)
        return self._message_block(_DOT_STATUS_ACTIVE, f"{label}{dots} {elapsed}s")

    def _compression_detail_line(self, mode: str, status: str, count: Any) -> str:
        """压缩结束时的摘要行（blocking/silent 文案区分）。"""
        prefix = "blocking" if mode == "blocking" else "silent"
        if status == "applied":
            return f"[dim]Context compression ({prefix}) applied — replaced {count} messages[/dim]"
        if status == "failed":
            return f"[yellow]Context compression ({prefix}) failed — keeping original context[/yellow]"
        if status == "stale":
            return f"[yellow]Context compression ({prefix}) result stale — discarded[/yellow]"
        if status == "invalid":
            return f"[yellow]Context compression ({prefix}) result invalid — discarded[/yellow]"
        return f"[dim]Context compression ({prefix}) finished (status={status})[/dim]"

    def _replace_status_line(self, name: str, content: str) -> None:
        state = self._status_lines.get(name)
        if state is None:
            return
        start, end = self._replace_log_block(int(state["start"]), int(state["end"]), content)
        state["start"] = start
        state["end"] = end

    async def _animate_status_line(self, name: str) -> None:
        """定时刷新状态行省略号与等待秒数。"""
        try:
            while name in self._status_lines:
                self._replace_status_line(name, self._status_text(name))
                await asyncio.sleep(0.5)
        except asyncio.CancelledError:
            raise

    def _start_status_line(self, name: str) -> None:
        """在 transcript 末尾启动一个可重写状态行。"""
        self._finish_status_line(name, add_gap=False)
        log = self._transcript_log()
        started_at = time.monotonic()
        start = len(log.lines)
        self._status_lines[name] = {
            "start": start,
            "end": start,
            "started_at": started_at,
            "task": None,
        }
        self._log_write_block(log, self._status_text(name))
        self._status_lines[name]["end"] = len(log.lines)
        self._status_lines[name]["task"] = asyncio.create_task(self._animate_status_line(name))

    def _finish_status_line(self, name: str, *, add_gap: bool = True) -> None:
        """将等待状态行冻结为 done，并停止动画。"""
        state = self._status_lines.get(name)
        if state is None:
            return
        task = state.get("task")
        if isinstance(task, asyncio.Task):
            task.cancel()
        self._replace_status_line(name, self._status_text(name, done=True))
        self._status_lines.pop(name, None)
        if add_gap:
            self._append_message_gap()

    def _finish_waiting_statuses(self, *, before_reasoning: bool = False) -> None:
        """内容到达前结束等待状态。

        reasoning 到达时只结束 prefilling，并新建 thinking；普通内容到达时结束所有等待态。
        """
        self._finish_status_line("prefilling", add_gap=False)
        if not before_reasoning:
            self._finish_status_line("thinking")

    def _mark_pending_segment_gap(self) -> None:
        self._pending_segment_gap = True

    def _flush_pending_segment_gap(self) -> None:
        if self._pending_segment_gap:
            self._append_message_gap()
            self._pending_segment_gap = False

    def _begin_turn_segment_after_tools(self) -> None:
        if self._needs_gap_after_tools:
            self._append_message_gap()
            self._needs_gap_after_tools = False

    def _cancel_status_lines(self) -> None:
        """取消所有状态行动画任务，用于退出或清屏。"""
        for state in self._status_lines.values():
            task = state.get("task")
            if isinstance(task, asyncio.Task):
                task.cancel()
        self._status_lines.clear()

    def _rich_code_box(self, content: str, *, lexer: str = "bash") -> Panel:
        """将文本渲染为带边框的 Rich 代码框（Syntax + Panel）。"""
        text = content if content.strip() else "<empty>"
        return Panel(
            Syntax(text, lexer, theme="monokai", word_wrap=True, background_color="default"),
            border_style="dim",
            padding=(0, 1),
        )

    def _tool_dot_block(self, *, dot_style: str, body: RenderableType) -> Table:
        """圆点 + 正文（可为 Group / Panel 等 Rich 组件）的 transcript 行。"""
        return self._dot_column_block(dot_style, body)

    def _bash_command_parts(self, command: str) -> tuple[str, str | None]:
        """生成 bash 工具标题行与可选的 command 代码框。

        逻辑：
        1. 参数换行压成空格，避免 `bash(...)` 标题折行；
        2. 按 cell 宽度判断 command 是否可放在括号内；
        3. 过长时标题只保留截断预览，全文放入下方代码框；
        4. 返回 `(title, None)` 或 `(title, full_command)`。

        关键边界：
        - 空 command 显示 `bash(—)`，不附加代码框。
        """
        raw = str(command or "").strip()
        if not raw:
            return "bash(—)", None
        cmd = sanitize_inline_tool_arg(raw)
        if cell_len(cmd) <= _BASH_INLINE_COMMAND_MAX_CELLS:
            return f"bash({cmd})", None
        preview = self._truncate_cells(cmd, max(12, _BASH_INLINE_COMMAND_MAX_CELLS - 1))
        return f"bash({preview})", raw

    def _tool_display_name(self, name: str, arguments: dict[str, Any]) -> str:
        """生成工具调用在 transcript 中的短标题（优先 call_purpose）。"""
        return tool_display_name(name, arguments)

    def _tool_call_parts_from_call(self, item: dict[str, Any]) -> tuple[str, str | None, str]:
        """从 tool_call SSE payload 解析短标题与可选代码框内容。

        逻辑：
        1. 规范化 OpenAI / Node 扁平格式；
        2. `bash_run` / `write_file` 在需要时返回全文供代码框展示；
        3. 返回 `(summary, code_content, code_lexer)`。

        关键边界：
        - 短 bash command 不附加代码框（`code_content=None`）。
        """
        normalized = normalize_tool_call_item(item)
        name = normalized["name"]
        arguments = normalized["arguments"]
        if name == "bash_run":
            purpose = tool_call_purpose(arguments)
            if purpose:
                title = tool_display_name(name, arguments)
                command = str(arguments.get("command") or "")
                return title, command if command else None, "bash"
            title, command = self._bash_command_parts(str(arguments.get("command") or ""))
            return title, command, "bash"
        summary = self._tool_display_name(name, arguments)
        if name == "write_file":
            content = str(arguments.get("content") or "")
            return summary, content if content else None, "text"
        return summary, None, "bash"

    def _tool_summary_from_call(self, item: dict[str, Any]) -> str:
        """从 tool_call SSE payload 生成后续结果重写使用的短标题。

        逻辑：
        1. 读取工具名与结构化 arguments；
        2. 委托 `_tool_display_name` 做工具特化展示；
        3. 返回单行标题，供 pending tool 与 tool_result 共用。
        """
        normalized = normalize_tool_call_item(item)
        return self._tool_display_name(normalized["name"], normalized["arguments"])

    def _tool_pending_renderable(
        self,
        summary: str,
        *,
        code_content: str | None = None,
        code_lexer: str = "bash",
        elapsed_s: float = 0.0,
        dot_blink: bool = True,
        show_code: bool = True,
    ) -> Table:
        """生成执行中工具占位块（黄点 + 可选代码框 + 动态耗时）。"""
        frame = int(max(0.0, elapsed_s) * 2) % 3
        dots = ("." * (frame + 1)).ljust(3)
        head = f"{summary}{dots} {self._format_tool_pending_elapsed(elapsed_s)}"
        if show_code and code_content is not None:
            body: RenderableType = Group(Text(head), self._rich_code_box(code_content, lexer=code_lexer))
        else:
            body = Text(head)
        dot_style = _DOT_TOOL_PENDING if dot_blink else "yellow"
        return self._tool_dot_block(dot_style=dot_style, body=body)

    def _approval_active(self) -> bool:
        return self._approval_future is not None and not self._approval_future.done()

    def _should_show_tool_detail(self, call_id: str) -> bool:
        """审批期间仅当前待审工具展示代码框等详情。"""
        if not self._approval_active():
            return True
        current = self._current_approval_request()
        if current is None:
            return True
        return current.call_id == call_id

    def _ensure_tool_pending_animation(self, call_id: str) -> None:
        pending = self._pending_tools.get(call_id)
        if pending is None:
            return
        task = pending.get("status_task")
        if isinstance(task, asyncio.Task) and not task.done():
            return
        pending["status_task"] = asyncio.create_task(self._animate_tool_pending(call_id))

    def _reset_pending_tools_execution_clock(self) -> None:
        """审批结束后重置计时，使耗时只统计实际执行等待（不含审批耗时）。"""
        now = time.monotonic()
        for pending in self._pending_tools.values():
            pending["started_at"] = now

    def _refresh_tool_pending_block(self, call_id: str) -> None:
        pending = self._pending_tools.get(call_id)
        if pending is None:
            return
        show_code = self._should_show_tool_detail(call_id)
        code_content = pending.get("code_content") if show_code else None
        started_at = float(pending.get("started_at", time.monotonic()))
        elapsed_s = max(0.0, time.monotonic() - started_at)
        status_task = pending.get("status_task")
        block = self._tool_pending_renderable(
            str(pending.get("summary") or "tool"),
            code_content=code_content,
            code_lexer=str(pending.get("code_lexer") or "bash"),
            elapsed_s=elapsed_s,
            dot_blink=not isinstance(status_task, asyncio.Task) or not status_task.done(),
            show_code=show_code,
        )
        start, end = self._replace_log_block(int(pending["start"]), int(pending["end"]), block)
        pending["start"] = start
        pending["end"] = end
        self._ensure_tool_pending_animation(call_id)

    def _refresh_approval_tool_blocks(self) -> None:
        """审批期间折叠非当前工具详情，仅展开当前待审工具。"""
        if not self._approval_active():
            return
        items = [item for item in self._approval_requests if item.call_id in self._pending_tools]
        for item in reversed(items):
            self._refresh_tool_pending_block(item.call_id)

    def _refresh_all_pending_tool_blocks(self) -> None:
        """重写所有 pending 工具块（审批结束后恢复详情展示）。"""
        for call_id in list(self._pending_tools.keys()):
            self._refresh_tool_pending_block(call_id)

    @staticmethod
    def _format_tool_pending_elapsed(elapsed_s: float) -> str:
        """与 prefilling/thinking 一致：整数秒计数。"""
        return f"{int(max(0.0, elapsed_s))}s"

    @staticmethod
    def _format_tool_elapsed(elapsed_s: float) -> str:
        """将工具执行秒数格式化为可读耗时（毫秒 / 秒 / 分秒）。"""
        safe = max(0.0, float(elapsed_s))
        if safe < 1.0:
            return f"{safe * 1000:.0f}ms"
        if safe < 60.0:
            return f"{safe:.1f}s"
        minutes = int(safe // 60)
        seconds = safe % 60.0
        return f"{minutes}m{seconds:.0f}s"

    def _tool_result_title_markup(
        self,
        summary: str,
        *,
        elapsed_s: float | None,
        rejected: bool,
        compress_saved_pct: int | None = None,
    ) -> str:
        """组装工具结果标题行 Rich markup（含拒绝态、耗时与输出压缩率）。"""
        parts = [escape(summary)]
        if rejected:
            parts.append("[red]已拒绝[/red]")
        if elapsed_s is not None:
            parts.append(f"[dim]· {escape(self._format_tool_elapsed(elapsed_s))}[/dim]")
        if compress_saved_pct is not None and compress_saved_pct > 0:
            parts.append(f"[dim]· -{compress_saved_pct}%[/dim]")
        return " ".join(parts)

    def _cancel_tool_pending_tasks(self) -> None:
        """取消所有工具执行中的耗时刷新任务（Esc 取消 turn 时调用）。"""
        for item in self._pending_tools.values():
            task = item.get("status_task")
            if isinstance(task, asyncio.Task):
                task.cancel()

    async def _animate_tool_pending(self, call_id: str) -> None:
        """定时重写工具占位行，展示执行中耗时（与 prefilling/thinking 一致）。"""
        try:
            while call_id in self._pending_tools:
                pending = self._pending_tools.get(call_id)
                if pending is None:
                    return
                started_at = float(pending.get("started_at", time.monotonic()))
                elapsed_s = max(0.0, time.monotonic() - started_at)
                block = self._tool_pending_renderable(
                    str(pending.get("summary") or "tool"),
                    code_content=pending.get("code_content") if self._should_show_tool_detail(call_id) else None,
                    code_lexer=str(pending.get("code_lexer") or "bash"),
                    elapsed_s=elapsed_s,
                    show_code=self._should_show_tool_detail(call_id),
                )
                start, end = self._replace_log_block(
                    int(pending["start"]),
                    int(pending["end"]),
                    block,
                )
                pending["start"] = start
                pending["end"] = end
                await asyncio.sleep(0.5)
        except asyncio.CancelledError:
            raise

    def _write_tool_call(self, data: dict[str, Any]) -> None:
        """写入工具调用占位行，并记录行范围以便 tool_result 到达后重写。

        逻辑：
        1. 写入黄点占位并记录行号；
        2. 记录 started_at 并启动耗时动画任务；
        3. tool_result 到达后取消动画并展示最终耗时。
        """
        tool_calls = data.get("tool_calls")
        if not isinstance(tool_calls, list) or not tool_calls:
            return
        log = self._transcript_log()
        for item in tool_calls:
            if not isinstance(item, dict):
                continue
            call_id = str(item.get("id") or "").strip()
            summary, code_content, code_lexer = self._tool_call_parts_from_call(item)
            started_at = time.monotonic()
            start = len(log.lines)
            self._log_write_block(
                log,
                self._tool_pending_renderable(
                    summary,
                    code_content=code_content,
                    code_lexer=code_lexer,
                    elapsed_s=0.0,
                ),
            )
            end = len(log.lines)
            if call_id:
                self._pending_tools[call_id] = {
                    "start": start,
                    "end": end,
                    "summary": summary,
                    "code_content": code_content,
                    "code_lexer": code_lexer,
                    "started_at": started_at,
                    "status_task": asyncio.create_task(self._animate_tool_pending(call_id)),
                }

    def _extract_bash_sections(self, content: str) -> tuple[str, str]:
        stdout_match = re.search(
            r"--- STDOUT ---\n(?P<stdout>.*?)(?:\n--- STDERR ---\n(?P<stderr>.*))?$",
            content,
            flags=re.DOTALL,
        )
        if stdout_match is None:
            return "", ""
        stdout = (stdout_match.group("stdout") or "").strip()
        stderr = (stdout_match.group("stderr") or "").strip()
        return stdout, stderr

    def _parse_search_replace_result(self, content: str) -> tuple[str, str]:
        """解析 search_replace 结构化输出为 (折叠摘要, 展开 diff)。

        逻辑：
        1. 按 `---` 分隔元数据与 diff；
        2. 摘要仅保留成功/失败、路径、替换次数；
        3. 展开区优先展示 diff，无 diff 时回退元数据全文。
        """
        meta, _, diff = content.partition("\n---\n")
        meta = meta.strip()
        diff = diff.strip()
        fields: dict[str, str] = {}
        for line in meta.splitlines():
            key, sep, value = line.partition(":")
            if sep:
                fields[key.strip()] = value.strip()
        path = fields.get("路径", "")
        if fields.get("成功") == "否":
            err = fields.get("错误", "失败")
            summary = f"失败 · {path}" if path else f"失败 · {err}"
            detail = meta if not diff else f"{meta}\n---\n{diff}"
            return summary, detail
        count = fields.get("替换次数", "?")
        summary = f"成功 · 替换 {count} 处"
        if path:
            summary += f" · {path}"
        if diff:
            return summary, diff
        return summary, ""

    def _tool_result_text(self, data: dict[str, Any]) -> tuple[str, str]:
        """提取工具结果摘要与详情；bash 优先展示 stdout，空则 stderr。"""
        content = str(data.get("content") or "").strip()
        tool_name = str(data.get("tool_name") or "")
        parsed = parse_temporary_agent_tool_result(tool_name, content)
        if parsed is not None:
            return parsed
        if tool_name == "bash_run":
            stdout, stderr = self._extract_bash_sections(content)
            detail = stdout or stderr or content
        elif tool_name == "search_replace":
            return self._parse_search_replace_result(content)
        else:
            detail = content
        lines = [line for line in detail.splitlines() if line.strip()]
        summary = lines[0] if lines else "无输出"
        return summary, detail

    def _transcript_content_width(self) -> int:
        """返回 transcript（RichLog）可用列宽，供 Panel/折行布局使用。"""
        log_width = int(getattr(self._transcript_log().size, "width", 0) or 0)
        screen_width = int(getattr(self.size, "width", 0) or 0)
        return max(20, log_width or screen_width or 80)

    def _tool_result_summary_width(self, *, has_toggle: bool) -> int:
        """估算工具结果折叠摘要可用宽度，避免 bash 单行超过屏幕。

        逻辑：
        1. 优先读取 RichLog 当前宽度；
        2. 预留圆点、缩进、`└─` 与展开按钮空间；
        3. 返回至少 20 列，避免极窄窗口下摘要完全不可读。
        """
        reserved = 11 + (5 if has_toggle else 0)
        return max(20, self._transcript_content_width() - reserved)

    def _truncate_cells(self, text: str, max_width: int) -> str:
        """按终端 cell 宽度截断文本，并用省略号标记。"""
        if cell_len(text) <= max_width:
            return text
        return f"{set_cell_size(text, max(0, max_width - 1)).rstrip()}…"

    def _render_bash_result_block(self, result_id: str) -> Table:
        """渲染 bash 工具结果：折叠单行，展开为 Syntax 代码框。"""
        result = self._tool_results[result_id]
        title = self._tool_result_title_markup(
            str(result["title"]),
            elapsed_s=result.get("elapsed_s"),
            rejected=bool(result.get("rejected")),
            compress_saved_pct=result.get("compress_saved_pct"),
        )
        detail = str(result["detail"])
        expanded = bool(result["expanded"])
        has_detail = bool(detail.strip())
        toggle = ""
        if has_detail:
            action = "收起" if expanded else "展开"
            toggle = f" [@click=app.toggle_tool_result('{result_id}')][dim underline]{action}[/dim underline][/]"
        if not expanded:
            summary = str(result["summary"]).replace("\n", " ").strip() or "无输出"
            summary = self._truncate_cells(summary, self._tool_result_summary_width(has_toggle=has_detail))
            body: RenderableType = Group(
                Text.from_markup(f"{title}{toggle}"),
                Text.from_markup(f"[dim]└─ {escape(summary)}[/dim]"),
            )
        else:
            body = Group(
                Text.from_markup(f"{title}{toggle}"),
                self._rich_code_box(detail, lexer="bash"),
            )
        return self._tool_dot_block(dot_style=_DOT_TOOL_RESULT, body=body)

    def _render_search_replace_result_block(self, result_id: str) -> Table:
        """渲染 search_replace 结果：折叠摘要，展开为 diff 代码框。"""
        result = self._tool_results[result_id]
        title = self._tool_result_title_markup(
            str(result["title"]),
            elapsed_s=result.get("elapsed_s"),
            rejected=bool(result.get("rejected")),
            compress_saved_pct=result.get("compress_saved_pct"),
        )
        detail = str(result["detail"])
        expanded = bool(result["expanded"])
        has_detail = bool(detail.strip())
        toggle = ""
        if has_detail:
            action = "收起" if expanded else "展开"
            toggle = f" [@click=app.toggle_tool_result('{result_id}')][dim underline]{action}[/dim underline][/]"
        if not expanded:
            summary = self._truncate_cells(
                str(result["summary"]).replace("\n", " ").strip() or "无输出",
                self._tool_result_summary_width(has_toggle=has_detail),
            )
            body: RenderableType = Group(
                Text.from_markup(f"{title}{toggle}"),
                Text.from_markup(f"[dim]└─ {escape(summary)}[/dim]"),
            )
        else:
            body = Group(
                Text.from_markup(f"{title}{toggle}"),
                self._rich_code_box(detail, lexer="diff"),
            )
        return self._tool_dot_block(dot_style=_DOT_TOOL_RESULT, body=body)

    def _render_tool_result_block(self, result_id: str) -> str | Table:
        result = self._tool_results[result_id]
        tool_name = str(result.get("tool_name") or "")
        if tool_name == "bash_run":
            return self._render_bash_result_block(result_id)
        if tool_name == "search_replace":
            return self._render_search_replace_result_block(result_id)
        summary = str(result["summary"])
        detail = str(result["detail"])
        expanded = bool(result["expanded"])
        body = detail if expanded else summary
        body_lines = escape(body).splitlines() or [""]
        if not expanded and len(detail.splitlines()) > 1:
            toggle = f" [@click=app.toggle_tool_result('{result_id}')][dim underline]展开[/dim underline][/]"
        elif expanded:
            toggle = f" [@click=app.toggle_tool_result('{result_id}')][dim underline]收起[/dim underline][/]"
        else:
            toggle = ""
        lines = [
            self._tool_result_title_markup(
                str(result["title"]),
                elapsed_s=result.get("elapsed_s"),
                rejected=bool(result.get("rejected")),
                compress_saved_pct=result.get("compress_saved_pct"),
            )
        ]
        for index, line in enumerate(body_lines):
            suffix = toggle if index == 0 else ""
            lines.append(f"[dim]└─ {line}[/dim]{suffix}" if index == 0 else f"[dim]   {line}[/dim]")
        return self._message_block(_DOT_TOOL_RESULT, "\n".join(lines), escape_text=False)

    def _replace_log_block(self, start: int, end: int, content: str | RenderableType) -> tuple[int, int]:
        """替换 RichLog 指定行范围，用于 tool_result 点击展开/收起。"""
        log = self._transcript_log()
        suffix = log.lines[end:]
        log.lines = log.lines[:start]
        log._line_cache.clear()
        self._log_write_block(log, content)
        new_end = len(log.lines)
        log.lines.extend(suffix)
        log._widest_line_width = max(
            (sum(segment.cell_length for segment in strip) for strip in log.lines),
            default=0,
        )
        log.virtual_size = Size(log._widest_line_width, len(log.lines))
        log.refresh()
        self._shift_tracked_ranges(end, new_end - end)
        return start, new_end

    def _delete_log_block(self, start: int, end: int) -> None:
        """删除 RichLog 指定行范围，用于审批完成后移除选项块。"""
        log = self._transcript_log()
        log.lines = log.lines[:start] + log.lines[end:]
        log._line_cache.clear()
        log._widest_line_width = max(
            (sum(segment.cell_length for segment in strip) for strip in log.lines),
            default=0,
        )
        log.virtual_size = Size(log._widest_line_width, len(log.lines))
        log.refresh()
        self._shift_tracked_ranges(end, start - end)

    def _shift_tracked_ranges(self, anchor: int, delta: int) -> None:
        """RichLog 行数变化后，平移变化点之后的追踪范围。"""
        if delta == 0:
            return
        for item in self._pending_tools.values():
            if int(item["start"]) >= anchor:
                item["start"] = int(item["start"]) + delta
                item["end"] = int(item["end"]) + delta
        for item in self._tool_results.values():
            if int(item["start"]) >= anchor:
                item["start"] = int(item["start"]) + delta
                item["end"] = int(item["end"]) + delta
        for item in self._status_lines.values():
            if int(item["start"]) >= anchor:
                item["start"] = int(item["start"]) + delta
                item["end"] = int(item["end"]) + delta
        if self._approval_block is not None and self._approval_block["start"] >= anchor:
            self._approval_block["start"] += delta
            self._approval_block["end"] += delta
        if self._user_info_block is not None and self._user_info_block["start"] >= anchor:
            self._user_info_block["start"] += delta
            self._user_info_block["end"] += delta

    def _approval_option_count(self) -> int:
        # 当前只展示一个工具的“同意/不同意”，确认后再切到下一个工具。
        return len(self._approval_options())

    def _current_approval_request(self) -> ToolApprovalRequest | None:
        """返回当前待审批的第一个工具请求。

        逻辑：
        1. 按服务端给出的工具顺序遍历 `_approval_requests`；
        2. 跳过已写入 `_approval_decisions` 的工具；
        3. 返回第一个未决请求。

        关键边界：
        - 全部确认后返回 `None`，调用方据此完成整批审批。
        """
        for item in self._approval_requests:
            if item.call_id not in self._approval_decisions:
                return item
        return None

    def _approval_options(self) -> list[tuple[str, bool]]:
        """生成当前工具的审批选项。

        逻辑：
        1. 定位第一个未决工具；
        2. 只返回该工具的同意/不同意两个选项。

        关键边界：
        - 不为后续工具提前生成选项，保证 UI 按工具逐个确认。
        """
        item = self._current_approval_request()
        if item is None:
            return []
        return [(item.call_id, True), (item.call_id, False)]

    def _approval_anchor_line(self) -> int:
        """计算审批块应插入的位置。

        逻辑：
        1. 优先把审批块插到当前工具调用块下方；
        2. 若没有记录到 pending tool 行范围，则退回 transcript 末尾。

        关键边界：
        - 行号会随 RichLog 重写平移，读取 `_pending_tools` 中最新范围。
        """
        current = self._current_approval_request()
        if current is None:
            return len(self._transcript_log().lines)
        pending = self._pending_tools.get(current.call_id)
        if pending is None:
            return len(self._transcript_log().lines)
        return max(0, min(int(pending.get("end") or 0), len(self._transcript_log().lines)))

    def _approval_request_label(self, item: ToolApprovalRequest) -> str:
        return self._tool_display_name(item.name, item.arguments)

    def _approval_block_text(self) -> str:
        lines: list[str] = []
        if self._approval_raw_data:
            header = approval_header(self._approval_raw_data)
            lines.append(f"[bold cyan]{escape(header)}[/bold cyan]")
        for option_index, (_call_id, approved) in enumerate(self._approval_options()):
            action = "同意" if approved else "不同意"
            cursor = "[cyan]●[/cyan]" if option_index == self._approval_selected_index else " "
            style = "bold" if option_index == self._approval_selected_index else "dim"
            color = "green" if approved else "red"
            lines.append(f"   {cursor} [{style}][{color}]{action}[/{color}][/{style}]")
        lines.append("   [dim]↑/↓ 选择，Enter 确认当前项，Y 同意 / N 拒绝[/dim]")
        return "\n".join(lines)

    def _write_approval_block(self) -> None:
        """在当前工具调用下方写入审批选项，并记录可重写行范围。

        逻辑：
        1. 根据当前未决工具定位插入点；
        2. 以空范围替换的方式插入审批块；
        3. 记录新行范围，供上下键刷新与确认后删除。

        关键边界：
        - 没有 pending 行范围时退回末尾，避免审批 UI 丢失。
        """
        start = self._approval_anchor_line()
        start, end = self._replace_log_block(start, start, self._approval_block_text())
        self._approval_block = {"start": start, "end": end}

    def _render_approval_block(self) -> None:
        if self._approval_block is None:
            return
        start, end = self._replace_log_block(
            self._approval_block["start"],
            self._approval_block["end"],
            self._approval_block_text(),
        )
        self._approval_block = {"start": start, "end": end}

    def _delete_approval_block(self) -> None:
        if self._approval_block is None:
            return
        self._delete_log_block(self._approval_block["start"], self._approval_block["end"])
        self._approval_block = None

    def _move_approval_selection(self, delta: int) -> None:
        count = self._approval_option_count()
        if count <= 0:
            return
        self._approval_selected_index = clamp_menu_selection_index(
            self._approval_selected_index,
            delta,
            count,
        )
        self._render_approval_block()

    def _write_tool_result(self, data: dict[str, Any]) -> None:
        """tool_result 到达时，将原 tool_call 黄点块重写为绿点结果块。

        逻辑：
        1. 停止该 call_id 的执行中耗时动画；
        2. 用 started_at 计算耗时（优先 SSE 下发的 duration_seconds）；
        3. 在结果标题行展示耗时。
        """
        call_id = str(data.get("tool_call_id") or "").strip()
        pending = self._pending_tools.pop(call_id, None)
        if pending is not None:
            task = pending.get("status_task")
            if isinstance(task, asyncio.Task):
                task.cancel()
        tool_name = str(data.get("tool_name") or "tool")
        title = pending["summary"] if pending is not None else self._tool_display_name(tool_name, {})
        summary, detail = self._tool_result_text(data)
        elapsed_s: float | None = None
        raw_duration = data.get("duration_seconds")
        if raw_duration is not None:
            try:
                elapsed_s = max(0.0, float(raw_duration))
            except (TypeError, ValueError):
                elapsed_s = None
        if elapsed_s is None and pending is not None:
            elapsed_s = max(0.0, time.monotonic() - float(pending.get("started_at", time.monotonic())))
        compress_saved_pct: int | None = None
        raw_pct = data.get("output_compress_saved_pct")
        if raw_pct is not None:
            try:
                pct = int(raw_pct)
                if pct > 0:
                    compress_saved_pct = pct
            except (TypeError, ValueError):
                compress_saved_pct = None
        self._tool_result_counter += 1
        result_id = f"tool-{self._tool_result_counter}"
        self._tool_results[result_id] = {
            "tool_name": tool_name,
            "title": title,
            "summary": summary,
            "detail": detail,
            "expanded": False,
            "elapsed_s": elapsed_s,
            "rejected": bool(data.get("rejected")),
            "compress_saved_pct": compress_saved_pct,
            "start": 0,
            "end": 0,
        }
        block = self._render_tool_result_block(result_id)
        if pending is not None:
            start, end = self._replace_log_block(int(pending["start"]), int(pending["end"]), block)
        else:
            log = self._transcript_log()
            start = len(log.lines)
            self._log_write_block(log, block)
            end = len(log.lines)
        self._tool_results[result_id]["start"] = start
        self._tool_results[result_id]["end"] = end

    async def action_toggle_tool_result(self, result_id: str) -> None:
        """点击工具结果摘要时展开/收起详情。"""
        result = self._tool_results.get(result_id)
        if result is None:
            return
        result["expanded"] = not bool(result["expanded"])
        start, end = self._replace_log_block(
            int(result["start"]),
            int(result["end"]),
            self._render_tool_result_block(result_id),
        )
        result["start"] = start
        result["end"] = end

    def _write_user_message(self, value: str) -> None:
        """写入用户消息：与上方内容、下方 Agent 回复各留一行空行。"""
        self._pending_round_usage_suffix = None
        log = self._transcript_log()
        self._flush_pending_segment_gap()
        self._append_message_gap_if_needed(log)
        self._log_write_block(log, self._message_block(_DOT_USER, value))
        self._append_message_gap()

    def _apply_transcript(self, update: TranscriptUpdate) -> None:
        log = self._transcript_log()
        if update.kind == TranscriptKind.ASSISTANT_DELTA:
            self._submit_content_seen = True
            self._begin_turn_segment_after_tools()
            self._finish_waiting_statuses()
            if self._assistant_stream_start is None:
                self._assistant_stream_start = max(len(log.lines), self._transcript_base_lines)
            self._assistant_buffer += update.text
            self._rewind_assistant_stream_lines(log)
            if self._assistant_buffer:
                self._write_assistant_block(log, self._assistant_buffer, complete=False)
        elif update.kind == TranscriptKind.ASSISTANT_END:
            had_assistant = bool(self._assistant_buffer)
            if had_assistant:
                self._rewind_assistant_stream_lines(log)
                self._write_assistant_block(log, self._assistant_buffer, complete=True)
            self._finish_assistant_stream(log)
            if had_assistant:
                self._mark_pending_segment_gap()
        elif update.kind == TranscriptKind.USAGE:
            suffix = format_inline_usage(parse_usage_round(update.data))
            if suffix:
                self._pending_round_usage_suffix = suffix
        elif update.kind == TranscriptKind.TOOL_CALL:
            self._submit_content_seen = True
            self._flush_pending_segment_gap()
            self._finish_waiting_statuses()
            self._finish_assistant_stream(log)
            self._write_tool_call(update.data)
        elif update.kind == TranscriptKind.TOOL_RESULT:
            self._submit_content_seen = True
            self._finish_waiting_statuses()
            self._finish_assistant_stream(log)
            self._write_tool_result(update.data)
            self._needs_gap_after_tools = True
        elif update.kind == TranscriptKind.ERROR:
            self._submit_content_seen = True
            self._finish_waiting_statuses()
            self._finish_assistant_stream(log)
            log.write(f"[red]{update.text}[/red]")
            self._append_message_gap()
        elif update.kind == TranscriptKind.COMPRESSION:
            mode = str(update.data.get("mode") or "blocking")
            phase = str(update.data.get("phase") or "")
            status = str(update.data.get("status") or "")
            count = update.data.get("compressed_message_count")
            if mode == "blocking":
                if phase == "start":
                    self._start_status_line("compression_blocking")
                elif phase == "end":
                    self._finish_status_line("compression_blocking", add_gap=False)
                    detail = self._compression_detail_line(mode, status, count)
                    log.write(detail)
                    self._append_message_gap()
            else:
                if phase == "start":
                    log.write(f"[dim]Silent context compression started (target {count} messages)[/dim]")
                elif phase == "end":
                    log.write(self._compression_detail_line(mode, status, count))
                    if status in {"applied", "failed", "stale", "invalid"}:
                        self._append_message_gap()
        elif update.kind == TranscriptKind.LINE and update.text.startswith("[reasoning]"):
            self._submit_content_seen = True
            self._begin_turn_segment_after_tools()
            self._finish_waiting_statuses(before_reasoning=True)
            self._finish_assistant_stream(log)
            if "thinking" not in self._status_lines:
                self._start_status_line("thinking")
        else:
            self._submit_content_seen = True
            self._finish_waiting_statuses()
            self._finish_assistant_stream(log)
            self._log_write_block(log, self._event_block(update.text))
            self._append_message_gap()

    def _apply_top_status(self, *, connected: bool | None = None) -> None:
        """更新顶栏右侧：SSE 状态文案 + 圆点。

        逻辑：
        1. 根据 connected（默认取 controller.sse_connected）生成「已连接/未连接」文案与圆点颜色；
        2. 写入 #top-status-bar（Rich markup）。
        """
        if connected is None:
            connected = self._controller.sse_connected
        sse_text = "SSE 已连接" if connected else "SSE 未连接"
        dot = "[green]●[/green]" if connected else "[red]●[/red]"
        bar = self.query_one("#top-status-bar", Static)
        bar.update(f"{sse_text} {dot}")

    def _reset_transcript_after_clear(self, log: RichLog) -> None:
        """清屏后重置流式状态与欢迎区行边界。"""
        self._pending_round_usage_suffix = None
        self._cancel_tool_pending_tasks()
        self._pending_tools.clear()
        self._cancel_status_lines()
        self._finish_assistant_stream(log)
        self._pending_segment_gap = False
        self._needs_gap_after_tools = False
        self._transcript_base_lines = 0

    def _prompt_area(self) -> PromptTextArea:
        return self.query_one("#prompt", PromptTextArea)

    def _exit_with_resume_hint(self) -> None:
        """退出 TUI，并在终端恢复后打印当前会话恢复命令。

        逻辑：
        1. 读取当前 session_id；
        2. 使用 CLI 参数 `--session` 生成恢复命令；
        3. 通过 Textual `exit(message=...)` 在退出后输出提示。

        关键边界：
        - session_id 为空时仅提示无法生成恢复命令；
        - 使用 `shlex.quote` 保护 session 中的特殊字符。
        """
        session_id = self._controller.session_id.strip()
        if not session_id:
            self.exit(message="已退出。当前 session_id 为空，无法生成恢复命令。")
            return
        command = f"dagents chat --session {shlex.quote(session_id)}"
        self.exit(message=f"已退出。恢复当前会话：{command}")

    async def submit_prompt(self) -> None:
        """处理 TextArea 提交：命令或发消息（Enter）。

        逻辑：
        1. 读取并清空 #prompt 文本（strip 后为空则返回）；
        2. 按命令分流；普通消息提交后等待 turn 结束。
        """
        prompt = self._prompt_area()
        value = prompt.text.strip()
        prompt.text = ""
        if not value:
            return
        if self._user_info_future is not None and not self._user_info_future.done():
            request = self._user_info_request
            if request is not None and not request.options:
                self._submit_user_info_text_answer(value)
            return
        if value in {"/exit", "/quit", "exit", "quit"}:
            self._exit_with_resume_hint()
            return
        if value == "/help":
            self._show_help()
            return
        if value == "/status":
            log = self._transcript_log()
            log.write(
                f"api={self._controller.api_base} session={self._controller.session_id} "
                f"sse={'connected' if self._controller.sse_connected else 'disconnected'}"
            )
            self._apply_top_status()
            return
        if value == "/session":
            await self._show_sessions()
            return
        if value == "/context":
            await self._show_context_view()
            return
        if value == "/compress":
            await self._compress_context()
            return
        if value == "/skill" or value.startswith("/skill "):
            await self._handle_skill_command(value)
            return
        if value == "/clear":
            await self._clear_context()
            return
        if value == "/children":
            await self._show_children()
            return
        if value.startswith("/"):
            log = self._transcript_log()
            log.write(f"[yellow]Unknown command: {value}[/yellow]")
            return
        self._abort_local_hitl_for_user_message()
        self._write_user_message(value)
        self._submit_content_seen = False
        try:
            await self._controller.submit_message(value)
            # 如果 SSE 内容已经抢先到达，就不再追加 prefilling，避免状态行倒挂在内容之后。
            if not self._submit_content_seen:
                self._start_status_line("prefilling")
            await self._controller.wait_user_turn()
        except Exception as exc:
            self._finish_status_line("prefilling")
            log = self._transcript_log()
            log.write(f"[red]send failed: {exc}[/red]")
        finally:
            prompt.focus()

    async def _show_sessions(self) -> None:
        """查询并展示 Node session 列表（GET /v1/sessions → sessions）。"""
        log = self._transcript_log()
        try:
            data = await self._controller.list_sessions()
            sessions = data.get("sessions")
            rows = sessions if isinstance(sessions, list) else []
            active_rows = [item for item in rows if isinstance(item, dict) and item.get("active")]
            persisted_rows = [item for item in rows if isinstance(item, dict) and not item.get("active")]
            lines = ["Active sessions (in memory):"]
            if not active_rows:
                lines.append("  (none)")
            for item in active_rows:
                sid = str(item.get("session_id") or "-")
                pending = str(item.get("queue_pending") or 0)
                processing = "yes" if item.get("has_active_turn") else "no"
                phase = str(item.get("run_turn_phase") or "idle")
                msgs = str(item.get("message_count") or 0)
                lines.append(
                    f"  {sid}  msgs={msgs}  pending={pending}  processing={processing}  phase={phase}"
                )
            lines.append("")
            lines.append("Persisted sessions (sqlite, not in memory):")
            if not persisted_rows:
                lines.append("  (none)")
            for item in persisted_rows:
                sid = str(item.get("session_id") or "-")
                preview = str(item.get("first_user_message") or "")
                if len(preview) > 40:
                    preview = preview[:40] + "..."
                updated = str(item.get("updated_at") or "-")
                lines.append(f"  {sid}  updated={updated}  {preview}")
            log.write("\n".join(lines))
        except Exception as exc:
            log.write(f"[red]session failed: {exc}[/red]")

    async def _show_context_view(self) -> None:
        """进入 context 视图，隐藏聊天 RichLog 并展示当前 context 摘要。

        逻辑：
        1. 调后端读取当前 session context 摘要；
        2. 写入独立 `#context-view` RichLog；
        3. 隐藏原 transcript，Esc 返回聊天 RichLog。

        关键边界：
        - 只隐藏聊天输出区，不清空 transcript；
        - 查询失败时仍进入 context 视图显示错误，便于 Esc 返回。
        """
        context_log = self._context_log()
        context_log.clear()
        try:
            data = await self._controller.get_context()
            context_log.write(self._format_context_state(data))
        except Exception as exc:
            context_log.write(f"[red]context failed: {escape(str(exc))}[/red]")
        self._enter_context_view()

    def _enter_context_view(self) -> None:
        """切换到 context 视图。

        逻辑：
        1. 标记 `_context_mode`，让 Esc 优先执行返回而不是取消 turn；
        2. 隐藏聊天 transcript，显示 `#context-view`；
        3. 刷新布局并把焦点移到 context 视图。

        副作用说明：
        - 只改变 widget 显隐与焦点，不修改 transcript 内容。
        """
        self._context_mode = True
        self._transcript_log().display = False
        context_log = self._context_log()
        context_log.display = True
        context_log.focus()
        self.refresh(layout=True)

    def _exit_context_view(self) -> None:
        """从 context 视图返回聊天 RichLog。

        逻辑：
        1. 清除 `_context_mode`；
        2. 隐藏 context 视图并恢复聊天 transcript；
        3. 焦点交还输入框，保持聊天输入体验。

        关键边界：
        - 不清空 context 视图内容，下一次 `/context` 会重新查询并覆盖。
        """
        self._context_mode = False
        self._context_log().display = False
        self._transcript_log().display = True
        self._prompt_area().focus()
        self.refresh(layout=True)

    def _format_context_state(self, data: dict[str, Any]) -> str:
        """将 context 摘要 API 响应格式化为 context 视图文本。

        逻辑：
        1. 输出 session、phase、队列和计数字段；
        2. 展示 loaded skills；
        3. 展示最近 messages 的角色、截断内容与 tool/reasoning 标记。

        关键边界：
        - RichLog 开启 markup，所有 API 字符串字段先 escape，避免用户内容被当作 Rich 标记。
        """
        lines = [
            "Context",
            "",
            f"session_id: {escape(str(data.get('session_id') or '-'))}",
            f"turn_state: {escape(str(data.get('turn_state') or '-'))}",
            f"run_turn_phase: {escape(str(data.get('run_turn_phase') or '-'))}",
            f"messages_count: {data.get('messages_count') or 0}",
            f"pending_tool_calls_count: {data.get('pending_tool_calls_count') or 0}",
            f"messages_total_tokens: {data.get('messages_total_tokens') or 0}",
            f"tool_loop_count: {data.get('tool_loop_count') or 0}",
            f"queue_pending: {data.get('queue_pending') or 0}",
            f"has_active_turn: {'yes' if data.get('has_active_turn') else 'no'}",
            "",
            "system_prompt:",
        ]
        system_prompt = str(data.get("system_prompt") or "").strip()
        if not system_prompt:
            lines.append("  (none)")
        else:
            for content_line in system_prompt.splitlines():
                safe_line = escape(content_line) if content_line else "[dim](空行)[/dim]"
                lines.append(f"  {safe_line}")
        lines.extend(["", "loaded_skills:"])
        loaded = data.get("loaded_skills")
        loaded_rows = loaded if isinstance(loaded, list) else []
        if not loaded_rows:
            lines.append("  (none)")
        for item in loaded_rows:
            if not isinstance(item, dict):
                continue
            name = escape(str(item.get("skill_name") or "-"))
            desc = escape(str(item.get("description") or ""))
            lines.append(f"  - {name}{f' · {desc}' if desc else ''}")
        lines.extend(["", "recent_messages:"])
        recent = data.get("recent_messages")
        rows = recent if isinstance(recent, list) else []
        if not rows:
            lines.append("  (none)")
        for idx, item in enumerate(rows, start=1):
            if not isinstance(item, dict):
                continue
            lines.extend(self._format_context_recent_message(idx, item))
        lines.extend(["", "[dim]Esc 返回聊天记录[/dim]"])
        return "\n".join(lines)

    async def _compress_context(self) -> None:
        """手动触发阻塞压缩并在 transcript 输出结果。"""
        log = self._transcript_log()
        if self._controller.awaiting_user_turn:
            log.write("[yellow]当前 turn 进行中，请稍后再试[/yellow]")
            return
        try:
            result = await self._controller.compress_context()
        except Exception as exc:
            log.write(f"[red]compress failed: {exc}[/red]")
            return
        status = str(result.get("status") or "")
        if status == "in_progress":
            mode = str(result.get("trigger_level") or "unknown")
            count = result.get("compressed_message_count")
            if count:
                log.write(
                    f"[dim]Context compression already in progress ({mode}, target {count} messages)[/dim]"
                )
            else:
                log.write(f"[dim]Context compression already in progress ({mode})[/dim]")
            return
        self._start_status_line("compression_blocking")
        self._finish_status_line("compression_blocking", add_gap=False)
        count = result.get("compressed_message_count")
        detail = self._compression_detail_line("blocking", status, count)
        if detail:
            log.write(detail)
        self._apply_top_status()

    def _format_context_recent_message(self, index: int, item: dict[str, Any]) -> list[str]:
        """格式化 `/context` 最近消息的一条记录。

        逻辑：
        1. 将 OpenAI role 映射为中文标题；
        2. 将 tool_call/reasoning 等协议信息放到独立 meta 行；
        3. 将内容按缩进行展示，避免长行与元信息混在一起。

        关键边界：
        - 后端预览会把换行转成 `\\n`，这里还原为多行展示；
        - RichLog 开启 markup，来自 API 的字段必须 escape。
        """
        role = str(item.get("role") or "unknown")
        lines = [f"  {index}. {self._context_role_label(role)}"]
        meta = self._context_recent_message_meta(item)
        if meta:
            lines.append(f"     [dim]{meta}[/dim]")
        content = str(item.get("content") or "")
        content_lines = content.replace("\\n", "\n").splitlines()
        if not content_lines:
            lines.append("     [dim](无文本内容)[/dim]")
            return lines
        for content_line in content_lines:
            safe_line = escape(content_line) if content_line else "[dim](空行)[/dim]"
            lines.append(f"     {safe_line}")
        return lines

    @staticmethod
    def _context_role_label(role: str) -> str:
        """将 OpenAI role 转成 context 视图里的友好标签。

        逻辑：
        1. 识别 user/assistant/tool/system 常见 role；
        2. 为未知 role 保留原值，方便排查协议异常。

        关键边界：
        - 返回值包含 Rich markup，但 role 原值会先 escape。
        """
        labels = {
            "user": "[cyan]用户[/cyan]",
            "assistant": "[green]助手[/green]",
            "tool": "[magenta]工具结果[/magenta]",
            "system": "[yellow]系统[/yellow]",
        }
        return labels.get(role, f"[yellow]{escape(role)}[/yellow]")

    @staticmethod
    def _context_recent_message_meta(item: dict[str, Any]) -> str:
        """格式化最近消息的协议元信息。

        逻辑：
        1. 收集 tool_call_id、tool_calls_count 与 reasoning 标记；
        2. 只输出存在的字段，减少空信息噪音。

        关键边界：
        - 所有字符串字段 escape 后再拼接到 Rich markup。
        """
        parts: list[str] = []
        tool_call_id = str(item.get("tool_call_id") or "").strip()
        if tool_call_id:
            parts.append(f"tool_call_id={escape(tool_call_id)}")
        tool_calls_count = int(item.get("tool_calls_count") or 0)
        if tool_calls_count > 0:
            parts.append(f"tool_calls={tool_calls_count}")
        if item.get("has_reasoning_content"):
            parts.append("reasoning_content=yes")
        return "  ".join(parts)

    async def _handle_skill_command(self, value: str) -> None:
        """处理 `/skill` 命令：列表、加载与卸载当前会话技能。

        逻辑：
        1. `/skill` 查询当前 loaded 与 available skills；
        2. `/skill load <name>` 调后端加载单个 skill；
        3. `/skill unload <name>` 调后端卸载单个 skill；
        4. 操作后统一展示最新 skill 状态。

        关键边界：
        - 使用 `shlex.split` 支持带引号的名称；
        - 只支持 load/unload 两个动作，缺少名称时给出用法。
        """
        log = self._transcript_log()
        try:
            parts = shlex.split(value)
        except ValueError as exc:
            log.write(f"[red]skill command parse failed: {exc}[/red]")
            return
        if len(parts) == 1:
            try:
                data = await self._controller.list_skills()
                log.write(self._skill_state_block(data), expand=True)
            except Exception as exc:
                log.write(f"[red]skill failed: {exc}[/red]")
            return
        if len(parts) != 3 or parts[1] not in {"load", "unload"}:
            log.write("[yellow]Usage: /skill | /skill load <skill_name> | /skill unload <skill_name>[/yellow]")
            return
        action = parts[1]
        skill_name = parts[2].strip()
        try:
            if action == "load":
                data = await self._controller.load_skill(skill_name)
            else:
                data = await self._controller.unload_skill(skill_name)
            log.write(self._skill_state_block(data), expand=True)
        except Exception as exc:
            log.write(f"[red]skill {action} failed: {exc}[/red]")

    def _format_skill_state(self, data: dict[str, Any]) -> str:
        """将 skill API 响应格式化为纯文本（供 `_skill_state_block` 折行渲染）。"""
        loaded = data.get("loaded_skills")
        available = data.get("available_skills")
        loaded_rows = loaded if isinstance(loaded, list) else []
        available_rows = available if isinstance(available, list) else []
        lines = ["Skills:"]
        lines.append("  loaded:")
        if not loaded_rows:
            lines.append("    (none)")
        for item in loaded_rows:
            if not isinstance(item, dict):
                continue
            name = escape(str(item.get("skill_name") or "-"))
            desc = escape(str(item.get("description") or ""))
            lines.append(f"    - {name}{f' · {desc}' if desc else ''}")
        lines.append("  available:")
        if not available_rows:
            lines.append("    (none)")
        loaded_names = {
            str(item.get("skill_name") or "")
            for item in loaded_rows
            if isinstance(item, dict)
        }
        for item in available_rows:
            if not isinstance(item, dict):
                continue
            name = escape(str(item.get("skill_name") or "-"))
            desc = escape(str(item.get("description") or ""))
            marker = " [loaded]" if str(item.get("skill_name") or "") in loaded_names else ""
            lines.append(f"    - {name}{marker}{f' · {desc}' if desc else ''}")
        return "\n".join(lines)

    def _skill_state_block(self, data: dict[str, Any]) -> RenderableType:
        """构造 skill 列表块；按 transcript 宽度折行，避免描述超出聊天区域。"""
        width = self._transcript_content_width()
        grid = Table.grid(expand=True, padding=(0, 0))
        grid.add_column(ratio=1, overflow="fold", max_width=width if width > 0 else None)
        grid.add_row(Text.from_markup(self._format_skill_state(data)))
        return grid

    async def _show_children(self) -> None:
        """查询并展示当前 session 下活跃子 Agent。"""
        log = self._transcript_log()
        try:
            items = await self._controller.list_child_agents()
            self._controller.child_tracker.replace_from_api(items)
            self._refresh_input_strip()
            text = format_child_agents_list(items, self._controller.child_tracker.awaiting_map())
            log.write(text)
        except Exception as exc:
            log.write(f"[red]children failed: {exc}[/red]")

    async def _clear_context(self) -> None:
        log = self._transcript_log()
        try:
            await self._controller.clear_context()
            self._controller.reset_child_state()
            log.clear()
            self._reset_transcript_after_clear(log)
        except Exception as exc:
            log.write(f"[red]clear failed: {exc}[/red]")

    def _show_help(self) -> None:
        log = self._transcript_log()
        for line in (
            "Commands:",
            "  /help            Show this help",
            "  /status          Show API/session/SSE status",
            "  /context         Show current context view (Esc to return)",
            "  /compress        Run blocking context compression once",
            "  /session         Show Agent Node sessions",
            "  /skill           Show loaded and available skills",
            "  /skill load NAME Load one skill into current session",
            "  /skill unload NAME Unload one skill from current session",
            "  /children        List active child agents",
            "  /clear           Clear server context and transcript",
            "  /exit            Quit chat",
        ):
            log.write(line)
