"""命令行入口（方案二）：CLI 作为 HTTP 客户端对接 Agent Service。"""

from __future__ import annotations

import asyncio
import contextlib
import json
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Literal

from app.config.env import load_env
from app.config.settings import get_settings
from app.harness.service.http_client import HttpAgentServiceClient
from app.harness.service.interface import (
    AgentStreamEventData,
    AgentSubmitRequest,
)

# 斜杠指令（普通聊天内容不要以 `/` 开头）。
CLI_CMD_CANCEL = "/cancel"
CLI_CMD_YES = "/yes"
CLI_CMD_NO = "/no"


@dataclass(frozen=True)
class _CliUserMessage:
    """用户发给模型的正文（非指令）。"""

    text: str


@dataclass(frozen=True)
class _CliApprove:
    """同意执行当前待审批工具。"""


@dataclass(frozen=True)
class _CliReject:
    """拒绝执行当前待审批工具。"""


@dataclass(frozen=True)
class _CliCancelOnly:
    """仅取消当前 turn / 本地 SSE。"""


@dataclass(frozen=True)
class _CliCancelWithMessage:
    """**`submit_user_text(..., interrupt=True)`**：先入队再 **`cancel_current_turn`**。"""

    text: str


def _parse_cli_line(line: str) -> _CliUserMessage | _CliApprove | _CliReject | _CliCancelOnly | _CliCancelWithMessage:
    """将一行输入解析为用户消息或斜杠指令。

    逻辑：
    1. `rstrip('\\n')` 后 `strip()` 得 `s`；
    2. 若 `s` 为空，视为空行（调用方在 strip 前应已 `continue`）；
    3. 不以 `/` 开头 → **`_CliUserMessage(s)`**；
    4. 精确 **`/yes`** / **`/no`** → 审批语义；
    5. **`/cancel`**：其后无内容或仅空白 → **`_CliCancelOnly`**；否则去掉 **`/cancel`** 与紧随空白 → **`_CliCancelWithMessage`**；
    6. 其它以 `/` 开头 → **`ValueError`**。

    关键边界：
    - **`/cancelfoo`**（无空格）视为非法，避免与 **`/cancel`** 混淆；
    - 用户若以 `/` 开头聊天，需改写或转义（本 CLI 不另设转义符）。
    """
    s = line.rstrip("\n").strip()
    if not s.startswith("/"):
        return _CliUserMessage(s)
    if s == CLI_CMD_YES:
        return _CliApprove()
    if s == CLI_CMD_NO:
        return _CliReject()
    if s.startswith(CLI_CMD_CANCEL):
        if len(s) == len(CLI_CMD_CANCEL):
            return _CliCancelOnly()
        next_ch = s[len(CLI_CMD_CANCEL) : len(CLI_CMD_CANCEL) + 1]
        if next_ch not in (" ", "\t"):
            raise ValueError(
                f"未知指令 {s!r}。取消并带消息请写：{CLI_CMD_CANCEL} 你的消息（斜杠后须有空格再接正文）"
            )
        rest = s[len(CLI_CMD_CANCEL) :].lstrip()
        if not rest:
            return _CliCancelOnly()
        return _CliCancelWithMessage(rest)
    raise ValueError(f"未知指令 {s!r}。内置：{CLI_CMD_YES} {CLI_CMD_NO} {CLI_CMD_CANCEL} [{CLI_CMD_CANCEL} 消息…]")


def main(project_root: Path | None = None) -> None:
    """CLI 主入口：加载配置并进入 HTTP 交互模式。

    使用场景：本地终端通过 FastAPI 与独立 Agent Service 交互（方案二）。

    字段说明：
    - `project_root`：项目根目录（可选，默认按当前文件推导）。

    返回说明：
    - 成功：进入交互式 stdin 循环，直到 Ctrl+C/EOF 退出。
    - 失败：抛 `SystemExit` 或打印错误信息后退出。

    调用范例：
    - `main()`
    - `main(Path('/path/to/DAgents'))`
    """
    root = project_root or Path(__file__).resolve().parent.parent.parent.parent

    load_env(root)
    asyncio.run(_run_http_cli(root))


def _format_event(ev: AgentStreamEventData) -> str:
    """将统一流事件格式化为 CLI 输出文本（每类事件一行，前缀为 `[事件类型]`）。

    说明：**`usage`** 在 **`_consume_stream_print`** 中直接跳过，不经本函数输出。
    """
    if ev.type == "tool_call":
        calls = ev.data.get("tool_calls", [])
        return f"[tool_call] {json.dumps(calls, ensure_ascii=False)}"
    if ev.type == "tool_result":
        tool_call_id = ev.data.get("tool_call_id", "")
        content = ev.data.get("content", "")
        return f"[tool_result] id={tool_call_id} content={content}"
    if ev.type == "reasoning":
        return f"[reasoning] {ev.data.get('content', '')}"
    if ev.type == "assistant":
        return f"[assistant] {ev.data.get('content', '')}"
    if ev.type == "approval_required":
        msg = str(ev.data.get("content", ""))
        atype = str(ev.data.get("approval_type", "approval_required"))
        return f"[approval_required] type={atype} message={msg}"

    if ev.type == "error":
        return f"[error] {json.dumps(ev.data, ensure_ascii=False)}"
    if ev.type == "done":
        return "[done]"
    return f"[{ev.type}] {json.dumps(ev.data, ensure_ascii=False)}"


def _extract_approval_payload(ev: AgentStreamEventData) -> dict | None:
    """从事件中提取审批 payload，非审批事件返回 `None`。"""
    if ev.type != "approval_required":
        return None
    return ev.data if isinstance(ev.data, dict) else None


def _ensure_stream_category_prefix(stream_kind: str, active: list[str | None]) -> None:
    """在 stdout 上保证当前流式块以 `[类别] ` 开头。

    逻辑：
    1. 若 `active[0]` 已与 `stream_kind` 相同，直接返回（继续拼接同一段流式正文）；
    2. 若此前在另一类流式块上，先换行结束上一段；
    3. 写入 `[stream_kind] ` 并令 `active[0] = stream_kind`。

    副作用：
    - 写入 `sys.stdout` 并 `flush`。

    关键边界：
    - `stream_kind` 一般为 SSE 事件名（如 `assistant` / `reasoning`），与 `_format_event` 前缀一致。
    """
    prev = active[0]
    if prev == stream_kind:
        return
    if prev is not None:
        print("", flush=True)
    sys.stdout.write(f"[{stream_kind}] ")
    sys.stdout.flush()
    active[0] = stream_kind


def _end_stream_if_any(active: list[str | None]) -> None:
    """若存在进行中的流式块，换行并清除状态。"""
    if active[0] is None:
        return
    print("", flush=True)
    active[0] = None


async def _stdin_pump(stdin_queue: asyncio.Queue[str]) -> None:
    """后台将 `sys.stdin.readline()` 结果放入队列，供与 SSE 消费并发。

    逻辑：
    1. 循环 `run_in_executor` 读一行；
    2. `put` 到 `stdin_queue`；`""` 表示 EOF，放入后结束循环。

    关键边界：
    - 与主协程解耦：主协程用 **`wait`** 在「下一行」与「流结束」间择一处理。
    """
    loop = asyncio.get_running_loop()
    while True:
        line = await loop.run_in_executor(None, sys.stdin.readline)
        await stdin_queue.put(line)
        if line == "":
            break


def _use_prompt_toolkit_layout() -> bool:
    """是否在 TTY 上使用 prompt_toolkit（输入固定在末行，上方 `print` 不打乱输入）。"""
    return bool(sys.stdin.isatty() and sys.stdout.isatty())


async def _wait_line_or_stream_end(
    stream_task: asyncio.Task | None,
    *,
    turn_done_event: asyncio.Event | None = None,
    prompt_session: object | None,
    stdin_queue: asyncio.Queue[str] | None,
    prompt_text: str = "> ",
) -> tuple[Literal["line", "stream_end", "turn_done"], str]:
    """阻塞直到有一行输入、当前回合 done，或流任务结束（三者先发生者）。

    逻辑：
    1. **TTY**：**`prompt_session.prompt_async(prompt_text)`** 任务（与 **`patch_stdout`** 配合，输出在输入行之上）；
    2. **非 TTY**：**`stdin_queue.get()`**；
    3. 若传入 **`turn_done_event`**，并发等待回合结束信号；
    4. 若 **`stream_task`** 非空，并发等待任务结束；
    4. line 先完成：返回 **`("line", raw)`**（统一为含 `\\n` 的串，便于与 `readline` 语义对齐；**`prompt` 返回 `None` 视为 EOF，raw=`""`**）；
    5. done 信号先到：取消 line 任务，返回 **`("turn_done", "")`**；
    6. stream 先完成：取消 line 任务，返回 **`("stream_end", "")`**。

    关键边界：
    - **`prompt_session` 与 `stdin_queue` 二选一**；
    - 取消 **`prompt_async`** 时可能收到 **`CancelledError`**，已吞掉。
    """
    if prompt_session is not None:
        line_task = asyncio.create_task(prompt_session.prompt_async(prompt_text))
    else:
        assert stdin_queue is not None
        line_task = asyncio.create_task(stdin_queue.get())

    wait_tasks: set[asyncio.Task] = {line_task}
    done_task: asyncio.Task | None = None
    if turn_done_event is not None:
        done_task = asyncio.create_task(turn_done_event.wait())
        wait_tasks.add(done_task)
    if stream_task is not None:
        wait_tasks.add(stream_task)
    if len(wait_tasks) == 1:
        try:
            got = await line_task
        except asyncio.CancelledError:
            return ("line", "")
        return ("line", _normalize_cli_line_result(got, prompt_session is not None))

    done, _pending = await asyncio.wait(wait_tasks, return_when=asyncio.FIRST_COMPLETED)
    if line_task in done:
        try:
            got = line_task.result()
        except asyncio.CancelledError:
            return ("line", "")
        if done_task is not None and not done_task.done():
            done_task.cancel()
        return ("line", _normalize_cli_line_result(got, prompt_session is not None))
    if done_task is not None and done_task in done:
        line_task.cancel()
        try:
            await line_task
        except asyncio.CancelledError:
            pass
        return ("turn_done", "")
    line_task.cancel()
    try:
        await line_task
    except asyncio.CancelledError:
        pass
    return ("stream_end", "")


def _normalize_cli_line_result(got: object, from_prompt_toolkit: bool) -> str:
    """将 prompt 或 readline 的结果规范为「一行」字符串（readline 风格带换行）。"""
    if from_prompt_toolkit:
        if got is None:
            return ""
        return str(got) + "\n"
    return str(got) if isinstance(got, str) else str(got) + "\n"


async def _cancel_stream_task(task: asyncio.Task | None) -> None:
    """取消本地 SSE 消费任务并吞掉 `CancelledError`。"""
    if task is None or task.done():
        return
    task.cancel()
    try:
        await task
    except asyncio.CancelledError:
        pass


async def _consume_stream_print(
    client: HttpAgentServiceClient,
    session_id: str,
    *,
    approval_seen_flag: list[bool] | None = None,
    approvals_buffer: list[dict] | None = None,
    turn_done_event: asyncio.Event | None = None,
) -> None:
    """常驻消费 SSE：打印事件并缓存 `approval_required` 载荷。

    逻辑：
    1. 若传入 **`approval_seen_flag`**，先置 **`[0]=False`**，在本流中首次解析到 **`approval_required`** 时置 **`True`**（供 CLI 识别「用户可能在 `done` 前输入 `/yes`/`/no`」的窗口）；
    2. `async for` **`client.stream`**；
    3. `assistant`/`reasoning` 行内拼接；**`usage`** 忽略；其余事件换行打印；
    4. 遇 **`approval_required`** 时写入 `approvals_buffer`；
    5. 遇 **`done`** 时触发 `turn_done_event`，但不退出消费循环（保持 SSE 常驻）。

    与外部交互：
    - HTTP SSE 长连接；取消任务会中断读取。
    """
    if approval_seen_flag is not None:
        approval_seen_flag[0] = False
    stream_active: list[str | None] = [None]
    try:
        async for ev in client.stream(session_id):
            if ev.type == "usage":
                continue
            if ev.type in ("assistant", "reasoning"):
                text = str(ev.data.get("content", ""))
                if text:
                    _ensure_stream_category_prefix(ev.type, stream_active)
                    sys.stdout.write(text)
                    sys.stdout.flush()
            else:
                _end_stream_if_any(stream_active)
                print(_format_event(ev), flush=True)
            payload = _extract_approval_payload(ev)
            if payload is not None:
                if approvals_buffer is not None:
                    approvals_buffer.append(payload)
                if approval_seen_flag is not None:
                    approval_seen_flag[0] = True
            if ev.type == "done":
                if turn_done_event is not None:
                    turn_done_event.set()
                else:
                    # 无回合事件时仅继续常驻消费。
                    pass
    finally:
        _end_stream_if_any(stream_active)
    return None


async def _async_resume_decision(
    approval_payload: dict,
    *,
    prompt_session: object | None,
    stdin_queue: asyncio.Queue[str] | None,
) -> dict | str:
    """等待审批输入： **`/yes`** / **`/no`**，或直接输入用户消息（改发新 **`message`**）。

    逻辑：
    1. 打印 **`[approval]`** 说明（含可直接输入正文打断审批的提示）；
    2. 循环 **`_wait_line_or_stream_end(None, ...)`** 读行；
    3. EOF（空串）→ 返回 **`{"type":"reject"}`**；
    4. **`_parse_cli_line`**：**`/yes`** / **`/no`** → 对应 **`resume`** 字典；**普通行（非 `/` 开头）** → **返回 `str`**，由调用方 **`submit_user_text`**；
    5. **`/cancel`**、**`/cancel …`** 等其它指令 → 提示在审批提示下应改用 **`/yes`**/**`/no`** 或发正文，然后重读。

    关键边界：
    - 返回 **`str`** 时调用方**不得**再发 **`request_type=resume`**；
    - 此阶段允许常驻 `stream_task` 继续运行，输入读取与流输出并发进行。
    """
    atype = str(approval_payload.get("approval_type", "approval_required"))
    print(
        f"[approval] type={atype} 待确认工具执行。"
        f" {CLI_CMD_YES} 同意，{CLI_CMD_NO} 拒绝；或直接输入一行正文改发新消息（不打断当前队列时仅入队）。",
        flush=True,
    )
    wait_kw = {"prompt_session": prompt_session, "stdin_queue": stdin_queue}
    while True:
        _kind, raw = await _wait_line_or_stream_end(
            None,
            prompt_text="> ",
            **wait_kw,
        )
        if raw == "":
            return {"type": "reject"}
        if not raw.rstrip("\n").strip():
            continue
        try:
            parsed = _parse_cli_line(raw)
        except ValueError as exc:
            print(f"[cli] {exc}", flush=True)
            continue
        if isinstance(parsed, _CliApprove):
            return {"type": "approve"}
        if isinstance(parsed, _CliReject):
            return {"type": "reject"}
        if isinstance(parsed, _CliUserMessage):
            return parsed.text
        print(
            f"[cli] 此处请用 {CLI_CMD_YES}/{CLI_CMD_NO}，或直接输入不含 `/` 的用户消息；"
            f"其它斜杠指令请在本轮审批结束后再用。",
            flush=True,
        )


async def _run_http_cli(project_root: Path) -> None:
    """交互式 HTTP CLI：stdin 与 SSE 并发；TTY 下用 **`patch_stdout`** 固定输入在末行。"""
    del project_root
    s = get_settings()
    client = HttpAgentServiceClient(base_url=s.agent_api_base)
    session = await client.create_session()
    session_id = session.session_id

    use_pt = _use_prompt_toolkit_layout()
    pt_session: object | None = None
    stdin_queue: asyncio.Queue[str] | None = None
    pump: asyncio.Task | None = None
    if use_pt:
        from prompt_toolkit import PromptSession

        pt_session = PromptSession()
    else:
        stdin_queue = asyncio.Queue()
        pump = asyncio.create_task(_stdin_pump(stdin_queue))

    out_cm = contextlib.nullcontext()
    if use_pt:
        from prompt_toolkit.patch_stdout import patch_stdout

        out_cm = patch_stdout()

    stream_task: asyncio.Task | None = None
    # 已在服务端入队、但尚未在本机收到 done 的请求数量（同一 session 按 FIFO 处理）。
    pending_stream_count = 0
    # 常驻流消费在收到 done 时置位，主循环据此触发审批与回合切换。
    turn_done_event = asyncio.Event()
    # 常驻流消费收集到的审批载荷，由主循环在 turn_done 后处理。
    approvals_buffer: list[dict] = []
    # 当前 SSE 中是否已收到过 `approval_required`（用于「打印了审批提示但 `done` 尚未到达」时的 /yes /no 预填）。
    approval_seen_in_stream: list[bool] = [False]
    # 用户在流未结束时输入的 /yes /no，在 turn_done 后用于首条审批，避免与 **`_wait_line_or_stream_end`** 竞态。
    pre_decided_resume: list[dict | None] = [None]

    def ensure_stream_consumer_running() -> None:
        """确保当前 session 常驻 SSE 消费已启动。

        副作用：赋值 **`stream_task`**。
        """
        nonlocal stream_task
        if stream_task is not None and not stream_task.done():
            return
        stream_task = asyncio.create_task(
            _consume_stream_print(
                client,
                session_id,
                approval_seen_flag=approval_seen_in_stream,
                approvals_buffer=approvals_buffer,
                turn_done_event=turn_done_event,
            )
        )

    async def handle_cancel() -> None:
        """仅取消：请求服务端打断当前 turn，本地 SSE 常驻不关闭。"""
        try:
            cr = await client.cancel_current_turn(session_id)
            print(f"[cli] cancel_turn cancelled={cr.cancelled}", flush=True)
        except Exception as exc:
            print(f"[cli] cancel_turn failed: {exc}", flush=True)

    async def submit_user_text(text: str, *, interrupt: bool = False) -> None:
        """提交用户主消息；是否打断在途 turn 由 **`interrupt`** 决定。

        逻辑：
        1. **`client.submit`** 入队（`priority=human`）；
        2. pending 计数 +1，并确保常驻 SSE 已启动；
        3. **`interrupt=True`**（**`/cancel …`**）时额外调用 **`cancel_current_turn`**，但不关闭本地 SSE。

        边界：**`submit` 失败则整段返回**；无在途 turn 时 cancel 为 no-op。

        说明：普通消息不打断时，多条入队会按提交顺序在服务端队列排队；本地通过 pending 计数在每次 `done` 后推进状态。
        """
        nonlocal pending_stream_count
        try:
            await client.submit(
                AgentSubmitRequest(
                    session_id=session_id,
                    client_id=client.client_id,
                    request_type="message",
                    content=text,
                    source="cli",
                    priority="human",
                )
            )
        except Exception as exc:
            print(f"[submit] failed: {exc}", flush=True)
            return
        pending_stream_count += 1
        ensure_stream_consumer_running()
        if interrupt:
            try:
                await client.cancel_current_turn(session_id)
            except Exception as exc:
                print(f"[cli] cancel_turn after submit failed: {exc}", flush=True)

    wait_kw = {"prompt_session": pt_session, "stdin_queue": stdin_queue}

    async def dispatch_line(raw_line: str) -> None:
        """解析并执行一行输入（主循环与流式进行中的内层循环共用）。

        逻辑：
        1. 空白行直接返回；
        2. **`_parse_cli_line`** 区分用户正文与 **`/yes`**、**`/no`**、**`/cancel`**、**`/cancel …`**；
        3. 用户正文 → **`submit_user_text(..., interrupt=False)`**；**`/cancel …`** → **`interrupt=True`**（先入队再 cancel）；仅 **`/cancel`** → **`handle_cancel`**；
        4. **`/yes`** / **`/no`**：若当前 SSE 已出现过 **`approval_required`** 但流未结束，写入 **`pre_decided_resume`**；否则打印「无待审批」提示。

        副作用：
        - 可能发起 HTTP **`submit`** / **`cancel`** 并创建新的 **`stream_task`**。
        """
        if not raw_line.rstrip("\n").strip():
            return
        try:
            parsed = _parse_cli_line(raw_line)
        except ValueError as exc:
            print(f"[cli] {exc}", flush=True)
            return
        if isinstance(parsed, _CliUserMessage):
            await submit_user_text(parsed.text, interrupt=False)
        elif isinstance(parsed, _CliCancelOnly):
            await handle_cancel()
        elif isinstance(parsed, _CliCancelWithMessage):
            await submit_user_text(parsed.text, interrupt=True)
        elif isinstance(parsed, (_CliApprove, _CliReject)):
            if stream_task is not None and not stream_task.done() and approval_seen_in_stream[0]:
                pre_decided_resume[0] = (
                    {"type": "approve"} if isinstance(parsed, _CliApprove) else {"type": "reject"}
                )
                return
            print(
                "[cli] 当前没有待审批的工具调用；出现 [approval_required] 后请使用 "
                f"{CLI_CMD_YES} / {CLI_CMD_NO}。",
                flush=True,
            )

    try:
        with out_cm:
            layout_hint = (
                "（输入固定在屏幕末行，模型输出在上方滚动）"
                if use_pt
                else "（管道/非 TTY：输出可能与输入交错）"
            )
            print(
                "HTTP CLI：普通行=用户消息；指令均以 / 开头。"
                f" {CLI_CMD_YES}=同意执行工具 {CLI_CMD_NO}=拒绝；"
                f" {CLI_CMD_CANCEL}=仅取消当前输出；"
                f" `{CLI_CMD_CANCEL} 消息` = 先入队再 cancel，打断当前输出后按队列处理（含此前排队的消息）。"
                " 普通用户消息在模型输出中也可输入，仅入队、**不**取消当前 turn；当前条 **`done`** 后再依次展示后续回复。"
                " 工具审批提示下也可**直接输入一行正文**，将改发新用户消息（不打断时与排队规则一致）。"
                f" Ctrl+C 退出；EOF 结束。{layout_hint}"
                f" api_base={s.agent_api_base.rstrip('/')} session_id={session_id}",
                flush=True,
            )
            ensure_stream_consumer_running()
            while True:
                kind, raw_line = await _wait_line_or_stream_end(
                    stream_task,
                    turn_done_event=turn_done_event,
                    prompt_text="> ",
                    **wait_kw,
                )

                if kind == "stream_end":
                    # 常驻流异常结束时重启；若重启失败由下一轮输入/提交再触发。
                    stream_task = None
                    ensure_stream_consumer_running()
                    continue

                if kind == "turn_done":
                    turn_done_event.clear()
                    if pending_stream_count > 0:
                        pending_stream_count -= 1
                    else:
                        # 兜底：避免计数被减成负值。
                        pending_stream_count = 0
                    if not approvals_buffer:
                        pre_decided_resume[0] = None
                    while approvals_buffer:
                        approval_payload = approvals_buffer.pop(0)
                        buf = pre_decided_resume[0]
                        if buf is not None:
                            chosen: dict | str = buf
                            pre_decided_resume[0] = None
                        else:
                            chosen = await _async_resume_decision(
                                approval_payload,
                                prompt_session=pt_session,
                                stdin_queue=stdin_queue,
                            )
                        if isinstance(chosen, str):
                            await submit_user_text(chosen, interrupt=False)
                            approvals_buffer.clear()
                            break
                        try:
                            await client.submit(
                                AgentSubmitRequest(
                                    session_id=session_id,
                                    client_id=client.client_id,
                                    request_type="resume",
                                    resume_value=chosen,
                                    source="cli",
                                    priority="resume",
                                )
                            )
                        except Exception as exc:
                            print(f"[resume] failed: {exc}", flush=True)
                            break
                        pending_stream_count += 1
                        # resume 提交后等待下一次 done，再决定是否继续处理后续审批。
                        break
                    continue

                if raw_line == "":
                    await _cancel_stream_task(stream_task)
                    stream_task = None
                    break
                await dispatch_line(raw_line)

    except KeyboardInterrupt:
        pass
    finally:
        await _cancel_stream_task(stream_task)
        if pump is not None:
            pump.cancel()
            try:
                await pump
            except asyncio.CancelledError:
                pass


if __name__ == "__main__":
    main()
