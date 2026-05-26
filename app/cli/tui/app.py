from __future__ import annotations

import asyncio
import re
import shlex
import time
from typing import Any

from rich.cells import cell_len, set_cell_size
from rich.markup import escape
from rich.markdown import Markdown
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
)
from app.cli.render import TranscriptKind, TranscriptUpdate
from app.cli.session_controller import SessionController
from app.cli.tui.prompt_text_area import PromptTextArea
from app.cli.tui.welcome_panel import build_welcome_panel

_HELP_HINT = "Type /help for commands, /exit to quit.  Enter 发送，Shift+Enter 换行"


class DAgentsTuiApp(App[None]):
    """DAgents Textual 聊天 TUI。"""

    # 固定亮色主题（Screen 背景约 #E0E0E0）；不跟随终端、不做动态切换。
    theme = "textual-light"

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
        # 欢迎 Panel 写入后 RichLog 行数，流式 assistant 回退不得早于该位置。
        self._transcript_base_lines = 0
        self._approval_future: asyncio.Future[ApprovalDecision] | None = None
        self._approval_requests: list[ToolApprovalRequest] = []
        self._approval_selected_index = 0
        self._approval_decisions: dict[str, bool] = {}
        self._approval_block: dict[str, int] | None = None
        self._pending_tools: dict[str, dict[str, Any]] = {}
        self._tool_results: dict[str, dict[str, Any]] = {}
        self._tool_result_counter = 0
        self._status_lines: dict[str, dict[str, Any]] = {}
        self._submit_content_seen = False
        self._cancel_task: asyncio.Task[None] | None = None
        self._context_mode = False

    def compose(self) -> ComposeResult:
        """创建 TUI 组件层次结构。"""
        yield Static(id="top-status-bar", markup=True)
        yield RichLog(id="transcript", highlight=True, markup=True, wrap=True)
        yield RichLog(id="context-view", highlight=True, markup=True, wrap=True)
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
        self._controller.on_approval(self._on_approval)
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
        log.write(
            build_welcome_panel(
                api_base=self._controller.api_base,
                session_id=self._controller.session_id,
            )
        )
        self._transcript_base_lines = len(log.lines)

    def _on_transcript(self, update: TranscriptUpdate) -> None:
        """将 controller transcript 更新调度到 UI 线程。"""
        self.call_later(self._apply_transcript, update)

    def _on_status(self, _text: str) -> None:
        """SSE/连接变化时仅刷新顶栏（不再使用输入框上方状态行）。"""
        self.call_later(self._apply_top_status)

    async def _on_approval(self, requests: list[ToolApprovalRequest]) -> ApprovalDecision:
        """隐藏输入框，并在 RichLog 中当前工具下方展示审批选项。

        逻辑：
        1. controller 的 SSE render task 只负责等待结果；
        2. widget 状态变更通过 `call_later` 回到 Textual UI 队列执行；
        3. 用户逐项确认后，由 `_approval_future` 将决策交还 controller。

        关键边界：
        - 不能在 controller 后台 task 里直接改 RichLog/TextArea，否则需要键盘等下一次 UI 事件才会重绘；
        - cleanup 同样回到 UI 队列执行，确保输入框恢复与审批块删除同步刷新。
        """
        if not requests:
            return build_all_rejected_decision([])
        loop = asyncio.get_running_loop()
        ready: asyncio.Future[asyncio.Future[ApprovalDecision]] = loop.create_future()

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
        approval_future = await ready
        try:
            return await approval_future
        finally:
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

        # 审批期间隐藏输入框，由 App 捕获上下键与 Enter；选项直接插在工具调用下方。
        prompt.display = False
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
        prompt.display = True
        self._refresh_approval_layout()
        prompt.focus()

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
        if self._approval_future is None or self._approval_future.done():
            return
        if event.key == "up":
            event.stop()
            event.prevent_default()
            self._move_approval_selection(-1)
        elif event.key == "down":
            event.stop()
            event.prevent_default()
            self._move_approval_selection(1)
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
        if self._approval_future is not None and not self._approval_future.done():
            self._approval_future.set_exception(ApprovalCancelled())
        self._cancel_status_lines()
        self._finish_assistant_stream(self._transcript_log())
        self._cancel_task = asyncio.create_task(self._cancel_current_turn_request())

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
        # 当前工具确认后，审批块要移动到下一个工具调用块下方。
        self._delete_approval_block()
        self._write_approval_block()
        self._refresh_approval_layout()

    def _finish_approval(self) -> None:
        """所有工具均完成单独审批后，构造最终决策。"""
        approved = [item.call_id for item in self._approval_requests if self._approval_decisions.get(item.call_id)]
        rejected = [item.call_id for item in self._approval_requests if not self._approval_decisions.get(item.call_id)]
        if self._approval_future is not None and not self._approval_future.done():
            self._approval_future.set_result(ApprovalDecision(approved=approved, rejected=rejected))

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

    def _message_block(self, dot: str, text: str, *, escape_text: bool = True) -> str:
        """按固定前缀列格式化 transcript 消息，保证圆点与正文对齐。"""
        body = escape(text) if escape_text else text
        lines = body.splitlines() or [""]
        first_prefix = f"{dot}  "
        next_prefix = "   "
        return "\n".join(
            f"{first_prefix if index == 0 else next_prefix}{line}"
            for index, line in enumerate(lines)
        )

    def _assistant_block(self, text: str, *, complete: bool) -> object:
        """格式化 assistant 消息。

        流式中使用普通文本，完成后用 Markdown 渲染正文；左侧圆点单独占一列以保持对齐。
        """
        if not complete:
            return self._message_block("[yellow blink]●[/yellow blink]", text)
        grid = Table.grid(expand=True, padding=(0, 1))
        grid.add_column(width=1)
        grid.add_column(ratio=1)
        grid.add_row(Text("●", style="green"), Markdown(text))
        return grid

    def _event_block(self, text: str) -> str:
        """格式化非流式事件：工具消息使用圆点，普通系统行保持原样。"""
        if text.startswith("[reasoning]"):
            return self._message_block("[yellow blink]●[/yellow blink]", text)
        return text

    def _append_message_gap(self) -> None:
        """在消息块之间恢复一行间隔；不用于输入框间距。"""
        log = self._transcript_log()
        if log.lines:
            log.write("")

    def _status_text(self, name: str, *, done: bool = False) -> str:
        """生成 prefilling/thinking 状态行文本。"""
        state = self._status_lines.get(name)
        started_at = float(state.get("started_at", time.monotonic())) if state else time.monotonic()
        raw_elapsed = max(0.0, time.monotonic() - started_at)
        elapsed = int(raw_elapsed)
        if done:
            return self._message_block("[green]●[/green]", f"{name}... {elapsed}s done")
        frame = int(raw_elapsed * 2) % 3
        # 省略号槽位固定 3 个字符，避免动画刷新时秒数左右抖动。
        dots = ("." * (frame + 1)).ljust(3)
        return self._message_block("[yellow blink]●[/yellow blink]", f"{name}{dots} {elapsed}s")

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
        log.write(self._status_text(name))
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
        self._finish_status_line("prefilling")
        if not before_reasoning:
            self._finish_status_line("thinking")

    def _cancel_status_lines(self) -> None:
        """取消所有状态行动画任务，用于退出或清屏。"""
        for state in self._status_lines.values():
            task = state.get("task")
            if isinstance(task, asyncio.Task):
                task.cancel()
        self._status_lines.clear()

    def _tool_display_name(self, name: str, arguments: dict[str, Any]) -> str:
        """生成工具调用在 transcript 中的短标题。

        逻辑：
        1. `bash_run` 只展示命令，压缩长参数结构；
        2. `trigger_create` 只展示触发器名称，避免 cron/描述等参数挤占消息行；
        3. `write_file` 只展示 path，content 交给下方文本框；
        4. 其它工具按 `key=value` 摘要展示参数。

        关键边界：
        - 目标字段为空时使用 `—` 占位；
        - 不修改传入 arguments，仅用于 UI 文案。
        """
        if name == "bash_run":
            command = str(arguments.get("command") or "").strip()
            return f"bash({command or '—'})"
        if name == "trigger_create":
            # trigger 创建参数较多，列表中只暴露 name 便于快速识别本次审批对象。
            trigger_name = str(arguments.get("name") or "").strip()
            return f"trigger_create({trigger_name or '—'})"
        if name == "write_file":
            path = str(arguments.get("path") or "").strip()
            return f"write_file({path or '—'})"
        if arguments:
            args = ", ".join(f"{key}={value!r}" for key, value in arguments.items())
            return f"{name}({args})"
        return f"{name}()"

    def _write_file_content_box(self, content: str) -> str:
        """将 `write_file.content` 渲染为工具标题下方的代码框。

        逻辑：
        1. 将 content 按原始换行拆分；
        2. 使用类似 Markdown fenced code block 的等宽区域标出内容；
        3. 空内容以占位文案展示，避免只看到空框。

        关键边界：
        - 不裁剪、不解析 content；Rich markup 由外层 `_message_block` 统一 escape；
        - 只负责生成文本，不写入 RichLog。
        """
        content_lines = content.splitlines()
        if not content_lines:
            content_lines = ["<empty>"]
        lines = ["```content"]
        lines.extend(content_lines)
        lines.append("```")
        return "\n".join(lines)

    def _tool_summary_from_call(self, item: dict[str, Any]) -> str:
        """从 tool_call SSE payload 生成后续结果重写使用的短标题。

        逻辑：
        1. 读取工具名与结构化 arguments；
        2. 委托 `_tool_display_name` 做工具特化展示；
        3. 返回单行标题，供 pending tool 与 tool_result 共用。
        """
        name = str(item.get("name") or "unknown")
        arguments = item.get("arguments") if isinstance(item.get("arguments"), dict) else {}
        return self._tool_display_name(name, arguments)

    def _tool_call_text_from_call(self, item: dict[str, Any]) -> str:
        """从 tool_call SSE payload 生成 RichLog 中的完整工具调用块。

        逻辑：
        1. 先生成工具短标题；
        2. `write_file` 追加 content 文本框；
        3. 其它工具只展示短标题。

        关键边界：
        - `arguments` 非 dict 时退化为空参数；
        - pending summary 仍使用短标题，避免工具结果重写时携带大段 content。
        """
        name = str(item.get("name") or "unknown")
        arguments = item.get("arguments") if isinstance(item.get("arguments"), dict) else {}
        summary = self._tool_display_name(name, arguments)
        if name != "write_file":
            return summary
        content = str(arguments.get("content") or "")
        return f"{summary}\n{self._write_file_content_box(content)}"

    def _write_tool_call(self, data: dict[str, Any]) -> None:
        """写入工具调用占位行，并记录行范围以便 tool_result 到达后重写。"""
        tool_calls = data.get("tool_calls")
        if not isinstance(tool_calls, list) or not tool_calls:
            return
        log = self._transcript_log()
        for item in tool_calls:
            if not isinstance(item, dict):
                continue
            call_id = str(item.get("id") or "").strip()
            summary = self._tool_summary_from_call(item)
            block = self._tool_call_text_from_call(item)
            start = len(log.lines)
            log.write(self._message_block("[yellow blink]●[/yellow blink]", block))
            end = len(log.lines)
            if call_id:
                self._pending_tools[call_id] = {"start": start, "end": end, "summary": summary}

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

    def _tool_result_text(self, data: dict[str, Any]) -> tuple[str, str]:
        """提取工具结果摘要与详情；bash 优先展示 stdout，空则 stderr。"""
        content = str(data.get("content") or "").strip()
        tool_name = str(data.get("tool_name") or "")
        if tool_name == "bash_run":
            stdout, stderr = self._extract_bash_sections(content)
            detail = stdout or stderr or content
        else:
            detail = content
        lines = [line for line in detail.splitlines() if line.strip()]
        summary = lines[0] if lines else "无输出"
        return summary, detail

    def _tool_result_summary_width(self, *, has_toggle: bool) -> int:
        """估算工具结果折叠摘要可用宽度，避免 bash 单行超过屏幕。

        逻辑：
        1. 优先读取 RichLog 当前宽度；
        2. 预留圆点、缩进、`└─` 与展开按钮空间；
        3. 返回至少 20 列，避免极窄窗口下摘要完全不可读。
        """
        log_width = int(getattr(self._transcript_log().size, "width", 0) or 0)
        screen_width = int(getattr(self.size, "width", 0) or 0)
        width = log_width or screen_width or 80
        reserved = 11 + (5 if has_toggle else 0)
        return max(20, width - reserved)

    def _truncate_cells(self, text: str, max_width: int) -> str:
        """按终端 cell 宽度截断文本，并用省略号标记。"""
        if cell_len(text) <= max_width:
            return text
        return f"{set_cell_size(text, max(0, max_width - 1)).rstrip()}…"

    def _render_bash_result_block(self, result_id: str) -> str:
        """渲染 bash 工具结果：折叠单行，展开为代码框。"""
        result = self._tool_results[result_id]
        title = escape(str(result["title"]))
        detail = str(result["detail"])
        expanded = bool(result["expanded"])
        has_detail = bool(detail.strip())
        toggle = ""
        if has_detail:
            action = "收起" if expanded else "展开"
            toggle = f" [@click=app.toggle_tool_result('{result_id}')][dim underline]{action}[/dim underline][/]"
        lines = [title]
        if not expanded:
            summary = str(result["summary"]).replace("\n", " ").strip() or "无输出"
            summary = self._truncate_cells(summary, self._tool_result_summary_width(has_toggle=has_detail))
            lines.append(f"[dim]└─ {escape(summary)}[/dim]{toggle}")
            return self._message_block("[green]●[/green]", "\n".join(lines), escape_text=False)

        code_lines = detail.splitlines() or ["<empty>"]
        lines.append(f"[dim]└─ ```bash[/dim]{toggle}")
        lines.extend(f"[dim]   {escape(line)}[/dim]" for line in code_lines)
        lines.append("[dim]   ```[/dim]")
        return self._message_block("[green]●[/green]", "\n".join(lines), escape_text=False)

    def _render_tool_result_block(self, result_id: str) -> str:
        result = self._tool_results[result_id]
        if str(result.get("tool_name") or "") == "bash_run":
            return self._render_bash_result_block(result_id)
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
        lines = [escape(str(result["title"]))]
        for index, line in enumerate(body_lines):
            suffix = toggle if index == 0 else ""
            lines.append(f"[dim]└─ {line}[/dim]{suffix}" if index == 0 else f"[dim]   {line}[/dim]")
        return self._message_block("[green]●[/green]", "\n".join(lines), escape_text=False)

    def _replace_log_block(self, start: int, end: int, content: str) -> tuple[int, int]:
        """替换 RichLog 指定行范围，用于 tool_result 点击展开/收起。"""
        log = self._transcript_log()
        suffix = log.lines[end:]
        log.lines = log.lines[:start]
        log._line_cache.clear()
        log.write(content)
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
        for option_index, (_call_id, approved) in enumerate(self._approval_options()):
            action = "同意" if approved else "不同意"
            cursor = "[cyan]●[/cyan]" if option_index == self._approval_selected_index else " "
            style = "bold" if option_index == self._approval_selected_index else "dim"
            color = "green" if approved else "red"
            lines.append(f"   {cursor} [{style}][{color}]{action}[/{color}][/{style}]")
        lines.append("   [dim]↑/↓ 选择，Enter 确认当前项[/dim]")
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
        self._approval_selected_index = (self._approval_selected_index + delta) % count
        self._render_approval_block()

    def _write_tool_result(self, data: dict[str, Any]) -> None:
        """tool_result 到达时，将原 tool_call 黄点块重写为绿点结果块。"""
        call_id = str(data.get("tool_call_id") or "").strip()
        pending = self._pending_tools.pop(call_id, None)
        tool_name = str(data.get("tool_name") or "tool")
        title = pending["summary"] if pending is not None else self._tool_display_name(tool_name, {})
        summary, detail = self._tool_result_text(data)
        self._tool_result_counter += 1
        result_id = f"tool-{self._tool_result_counter}"
        self._tool_results[result_id] = {
            "tool_name": tool_name,
            "title": title,
            "summary": summary,
            "detail": detail,
            "expanded": False,
            "start": 0,
            "end": 0,
        }
        block = self._render_tool_result_block(result_id)
        if pending is not None:
            start, end = self._replace_log_block(int(pending["start"]), int(pending["end"]), block)
        else:
            log = self._transcript_log()
            start = len(log.lines)
            log.write(block)
            end = len(log.lines)
        self._tool_results[result_id]["start"] = start
        self._tool_results[result_id]["end"] = end
        self._append_message_gap()

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
        """写入用户消息：用户与上一条消息之间留一行，使用蓝点标识。"""
        log = self._transcript_log()
        log.write(self._message_block("[blue]●[/blue]", value))
        self._append_message_gap()

    def _apply_transcript(self, update: TranscriptUpdate) -> None:
        log = self._transcript_log()
        if update.kind == TranscriptKind.ASSISTANT_DELTA:
            self._submit_content_seen = True
            self._finish_waiting_statuses()
            if self._assistant_stream_start is None:
                self._assistant_stream_start = max(len(log.lines), self._transcript_base_lines)
            self._assistant_buffer += update.text
            self._rewind_assistant_stream_lines(log)
            if self._assistant_buffer:
                log.write(self._assistant_block(self._assistant_buffer, complete=False))
        elif update.kind == TranscriptKind.ASSISTANT_END:
            if self._assistant_buffer:
                self._rewind_assistant_stream_lines(log)
                log.write(self._assistant_block(self._assistant_buffer, complete=True))
                self._append_message_gap()
            self._finish_assistant_stream(log)
        elif update.kind == TranscriptKind.TOOL_CALL:
            self._submit_content_seen = True
            self._finish_waiting_statuses()
            self._finish_assistant_stream(log)
            self._write_tool_call(update.data)
        elif update.kind == TranscriptKind.TOOL_RESULT:
            self._submit_content_seen = True
            self._finish_waiting_statuses()
            self._finish_assistant_stream(log)
            self._write_tool_result(update.data)
        elif update.kind == TranscriptKind.ERROR:
            self._submit_content_seen = True
            self._finish_waiting_statuses()
            self._finish_assistant_stream(log)
            log.write(f"[red]{update.text}[/red]")
            self._append_message_gap()
        elif update.kind == TranscriptKind.LINE and update.text.startswith("[reasoning]"):
            self._submit_content_seen = True
            self._finish_waiting_statuses(before_reasoning=True)
            self._finish_assistant_stream(log)
            if "thinking" not in self._status_lines:
                self._start_status_line("thinking")
        else:
            self._submit_content_seen = True
            self._finish_waiting_statuses()
            self._finish_assistant_stream(log)
            log.write(self._event_block(update.text))
            self._append_message_gap()

    def _apply_top_status(self, *, connected: bool | None = None) -> None:
        """更新顶栏右侧：SSE 状态文案 + 圆点 + 当前 session_id。

        逻辑：
        1. 根据 connected（默认取 controller.sse_connected）生成「已连接/未连接」文案与圆点颜色；
        2. session_id 为空时显示「—」；
        3. 写入 #top-status-bar（Rich markup）。
        """
        if connected is None:
            connected = self._controller.sse_connected
        sse_text = "SSE 已连接" if connected else "SSE 未连接"
        dot = "[green]●[/green]" if connected else "[red]●[/red]"
        session_id = self._controller.session_id.strip() or "—"
        bar = self.query_one("#top-status-bar", Static)
        bar.update(f"{sse_text} {dot}  session {session_id}")

    def _reset_transcript_after_clear(self, log: RichLog) -> None:
        """清屏后重置流式状态与欢迎区行边界。"""
        self._cancel_status_lines()
        self._finish_assistant_stream(log)
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
                f"client={self._controller.client_id} sse="
                f"{'connected' if self._controller.sse_connected else 'disconnected'}"
            )
            self._apply_top_status()
            return
        if value == "/session":
            await self._show_sessions()
            return
        if value == "/context":
            await self._show_context_view()
            return
        if value == "/skill" or value.startswith("/skill "):
            await self._handle_skill_command(value)
            return
        if value == "/bind-triggers":
            await self._bind_triggers()
            return
        if value == "/clear":
            await self._clear_context()
            return
        if value.startswith("/"):
            log = self._transcript_log()
            log.write(f"[yellow]Unknown command: {value}[/yellow]")
            return
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

    async def _bind_triggers(self) -> None:
        log = self._transcript_log()
        try:
            bound = await self._controller.bind_triggers_to_client()
            log.write(f"Bound {bound} trigger(s) to client {self._controller.client_id}")
        except Exception as exc:
            log.write(f"[red]bind-triggers failed: {exc}[/red]")

    async def _show_sessions(self) -> None:
        """查询并展示当前队列中的 active sessions。

        逻辑：
        1. 调用 controller 查询 `/v1/sessions`；
        2. 只读取 `active` 列表，符合 `/session` 查看当前队列的语义；
        3. 以紧凑文本输出 session_id、client、pending、processing 与 phase。

        关键边界：
        - active 为空时显示 `(none)`；
        - API 字段异常时按空列表处理，避免 TUI 命令崩溃。
        """
        log = self._transcript_log()
        try:
            data = await self._controller.list_sessions()
            active = data.get("active")
            rows = active if isinstance(active, list) else []
            lines = ["Active sessions (queue):"]
            if not rows:
                lines.append("  (none)")
            for item in rows:
                if not isinstance(item, dict):
                    continue
                sid = str(item.get("session_id") or "-")
                client_id = str(item.get("client_id") or "-")
                pending = str(item.get("queue_pending") or 0)
                processing = "yes" if item.get("has_active_turn") else "no"
                phase = str(item.get("run_turn_phase") or "-")
                lines.append(
                    f"  {sid}  client={client_id}  pending={pending}  processing={processing}  phase={phase}"
                )
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
            f"sse_client_id: {escape(str(data.get('sse_client_id') or '-'))}",
            f"active_client_id: {escape(str(data.get('active_client_id') or '-'))}",
            f"run_turn_phase: {escape(str(data.get('run_turn_phase') or '-'))}",
            f"messages_count: {data.get('messages_count') or 0}",
            f"pending_tool_calls_count: {data.get('pending_tool_calls_count') or 0}",
            f"messages_total_tokens: {data.get('messages_total_tokens') or 0}",
            f"tool_loop_count: {data.get('tool_loop_count') or 0}",
            f"queue_pending: {data.get('queue_pending') or 0}",
            f"has_active_turn: {'yes' if data.get('has_active_turn') else 'no'}",
            "",
            "loaded_skills:",
        ]
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
                log.write(self._format_skill_state(data))
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
            log.write(self._format_skill_state(data))
        except Exception as exc:
            log.write(f"[red]skill {action} failed: {exc}[/red]")

    def _format_skill_state(self, data: dict[str, Any]) -> str:
        """将 skill API 响应格式化为 RichLog 文本。"""
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
            name = str(item.get("skill_name") or "-")
            desc = str(item.get("description") or "")
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
            name = str(item.get("skill_name") or "-")
            desc = str(item.get("description") or "")
            marker = " [loaded]" if name in loaded_names else ""
            lines.append(f"    - {name}{marker}{f' · {desc}' if desc else ''}")
        return "\n".join(lines)

    async def _clear_context(self) -> None:
        log = self._transcript_log()
        try:
            await self._controller.clear_context()
            log.clear()
            self._reset_transcript_after_clear(log)
        except Exception as exc:
            log.write(f"[red]clear failed: {exc}[/red]")

    def _show_help(self) -> None:
        log = self._transcript_log()
        for line in (
            "Commands:",
            "  /help            Show this help",
            "  /status          Show API/session/client IDs",
            "  /context         Show current context view (Esc to return)",
            "  /session         Show active queued sessions",
            "  /skill           Show loaded and available skills",
            "  /skill load NAME Load one skill into current session",
            "  /skill unload NAME Unload one skill from current session",
            "  /bind-triggers   Bind session triggers to this client_id",
            "  /clear           Clear server context and transcript",
            "  /exit            Quit chat",
        ):
            log.write(line)
