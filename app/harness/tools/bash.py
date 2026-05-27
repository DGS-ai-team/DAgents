"""统一的 shell 执行工具（bash/cmd/powershell）。

说明：由 Agent 选择目标 shell，并由本模块做分 shell 解析、策略校验与执行。
"""

from __future__ import annotations

import asyncio
import os
import re
import shlex
import signal
import subprocess
from dataclasses import dataclass
from pathlib import Path
from time import time
from typing import Literal, Optional
from uuid import uuid4

from pydantic import BaseModel, ConfigDict, Field

from app.config.settings import get_settings
from app.config.host_snapshot import get_host_snapshot
from app.context.models import OpenAIConversationContext
from app.harness.tools.async_store import get_async_tool_result_store
from app.harness.tools.host_platform import HostOsKind, detect_host_os
from app.harness.tools.tool import tool

ShellType = Literal["bash", "cmd", "powershell"]


class BashRunArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    command: str = Field(min_length=1)
    timeout_seconds: Optional[int] = Field(default=None, ge=1, le=600)
    cwd: Optional[str] = None
    shell_type: Optional[ShellType] = None


class ShellJobIdArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    job_id: str = Field(min_length=1)


class BashJobTailArgs(ShellJobIdArgs):
    max_chars: Optional[int] = Field(default=None, ge=1, le=12000)


# 非 root 拦截：`su - <user> -c ...`（含 `-l` / `--login`）；按 bash 语句片段逐个匹配行首。
_SU_LOGIN_WITH_C_RE = re.compile(
    r"^\s*(?:/[\w./-]+/)?su\b\s+"
    r"(?:-|(?:-l\b)|(?:--login\b))\s+"
    r"(?:[^\s/-][^\s]*)"
    r"\s+(?:-c|--command)\b",
    re.IGNORECASE,
)

# 行首 `sudo` / `sudoedit`；若未带 `-n` / `--non-interactive`，可能在无 TTY 时阻塞读密码。
_SUDO_FAMILY_INVOCATION_RE = re.compile(
    r"^\s*(?:/[\w./-]+/)?(?:sudo|sudoedit)\b",
    re.IGNORECASE,
)
_SUDO_NONINTERACTIVE_FLAG_RE = re.compile(
    r"(?:^|\s)-n(?:\s|$)"
    r"|(?:^|\s)--non-interactive(?:\s|$)",
    re.IGNORECASE,
)

DEFAULT_BASH_TIMEOUT_SECONDS = 30
MAX_BASH_TIMEOUT_SECONDS = 600
MAX_BASH_OUTPUT_CHARS = 12000
SHELL_JOB_TAIL_CHARS = 8000


@dataclass(slots=True)
class ShellJob:
    """后台 shell job 快照。

    逻辑：
    1. 保存同一个已经启动的 `Popen` 进程，超时后不重启、不杀进程；
    2. 后台等待完成后写入 stdout/stderr、退出码与状态；
    3. status/tail/cancel 工具从本结构读取状态。

    关键边界：
    - 当前为进程内存储，服务重启后不可恢复；
    - stdout/stderr 暂存于内存，后续可替换为落盘 raw_ref。
    """

    job_id: str
    command: str
    cwd: str
    shell_type: ShellType
    timeout_seconds: int
    output_encoding: str
    process: subprocess.Popen[str]
    status: str = "running"
    async_job_id: str = ""
    stdout: str = ""
    stderr: str = ""
    exit_code: int | None = None
    started_at_unix_ms: int = 0
    finished_at_unix_ms: int = 0


_SHELL_JOBS: dict[str, ShellJob] = {}


def _now_ms() -> int:
    """返回当前 Unix 毫秒时间戳。"""
    return int(time() * 1000)


def _resolve_shell_output_encoding() -> str:
    """解析 shell 输出解码编码。

    逻辑：
    1. 读取配置 `BASH_OUTPUT_ENCODING`；
    2. 校验编码名是否可用；
    3. 不可用时回退 `utf-8`，避免命令执行阶段抛异常。
    """

    configured = (get_settings().bash_output_encoding or "").strip() or "utf-8"
    try:
        "".encode(configured)
        return configured
    except LookupError:
        return "utf-8"


class CommandAstNode(BaseModel):
    """命令 AST 节点（轻量版）。

    逻辑：
    1. 用于承载每个命令片段的原文和首命令词；
    2. `parser` 记录由哪种 shell 解析器得到；
    3. 上层基于 `root` 做审批策略判断。

    关键边界：
    - `root` 可能为空串（解析失败或片段无可执行词）；
    - 本类仅是数据载体，不做校验。
    """

    model_config = ConfigDict(frozen=True)

    parser: ShellType
    raw: str
    root: str


def _clip_text(text: str, max_chars: int) -> tuple[str, bool]:
    """按字符上限裁剪文本，返回裁剪结果和是否截断。

    逻辑：
    1. 未超过上限时直接返回原文；
    2. 超过时截断为前 `max_chars` 个字符；
    3. 返回 `(text, truncated)` 给上层拼接提示。

    关键边界：
    - `max_chars` 应由调用方保证 >=1；本函数不做纠正。
    """
    if len(text) <= max_chars:
        return text, False
    return text[:max_chars], True


def _split_bash_statements(command: str) -> list[str]:
    """按 bash 语义切分语句片段。

    逻辑：
    1. 跟踪单引号/双引号状态；
    2. 仅在引号外按 `&&`、`||`、`;`、`|`、换行切分；
    3. 返回去空白后的片段列表。

    关键边界：
    - 未闭合引号不会在此抛错，交给解释器执行时报错。
    """
    parts: list[str] = []
    buf: list[str] = []
    in_single = False
    in_double = False
    i = 0
    while i < len(command):
        ch = command[i]
        if ch == "'" and not in_double:
            in_single = not in_single
            buf.append(ch)
            i += 1
            continue
        if ch == '"' and not in_single:
            in_double = not in_double
            buf.append(ch)
            i += 1
            continue
        if not in_single and not in_double:
            two = command[i : i + 2]
            if two in ("&&", "||"):
                part = "".join(buf).strip()
                if part:
                    parts.append(part)
                buf = []
                i += 2
                continue
            if ch in (";", "|", "\n"):
                part = "".join(buf).strip()
                if part:
                    parts.append(part)
                buf = []
                i += 1
                continue
        buf.append(ch)
        i += 1
    tail = "".join(buf).strip()
    if tail:
        parts.append(tail)
    return parts


def _split_cmd_statements(command: str) -> list[str]:
    """按 cmd 语义切分语句片段。

    逻辑：
    1. 跟踪双引号状态与 `^` 转义；
    2. 在引号外按 `&&`、`||`、`&`、`|`、换行切分；
    3. 输出去空白片段。

    关键边界：
    - cmd 语法较复杂，此处实现为策略校验用的轻量 AST 切分。
    """
    parts: list[str] = []
    buf: list[str] = []
    in_double = False
    escaped = False
    i = 0
    while i < len(command):
        ch = command[i]
        if escaped:
            buf.append(ch)
            escaped = False
            i += 1
            continue
        if ch == "^":
            escaped = True
            buf.append(ch)
            i += 1
            continue
        if ch == '"':
            in_double = not in_double
            buf.append(ch)
            i += 1
            continue
        if not in_double:
            two = command[i : i + 2]
            if two in ("&&", "||"):
                part = "".join(buf).strip()
                if part:
                    parts.append(part)
                buf = []
                i += 2
                continue
            if ch in ("&", "|", "\n"):
                part = "".join(buf).strip()
                if part:
                    parts.append(part)
                buf = []
                i += 1
                continue
        buf.append(ch)
        i += 1
    tail = "".join(buf).strip()
    if tail:
        parts.append(tail)
    return parts


def _split_powershell_statements(command: str) -> list[str]:
    """按 powershell 语义切分语句片段。

    逻辑：
    1. 跟踪单/双引号状态和反引号转义；
    2. 在引号外按 `&&`、`||`、`;`、`|`、换行切分；
    3. 输出去空白片段。

    关键边界：
    - 复杂脚本块（如 here-string）不做完整语法建模，仅做策略前置拆分。
    """
    parts: list[str] = []
    buf: list[str] = []
    in_single = False
    in_double = False
    escaped = False
    i = 0
    while i < len(command):
        ch = command[i]
        if escaped:
            buf.append(ch)
            escaped = False
            i += 1
            continue
        if ch == "`":
            escaped = True
            buf.append(ch)
            i += 1
            continue
        if ch == "'" and not in_double:
            in_single = not in_single
            buf.append(ch)
            i += 1
            continue
        if ch == '"' and not in_single:
            in_double = not in_double
            buf.append(ch)
            i += 1
            continue
        if not in_single and not in_double:
            two = command[i : i + 2]
            if two in ("&&", "||"):
                part = "".join(buf).strip()
                if part:
                    parts.append(part)
                buf = []
                i += 2
                continue
            if ch in (";", "|", "\n"):
                part = "".join(buf).strip()
                if part:
                    parts.append(part)
                buf = []
                i += 1
                continue
        buf.append(ch)
        i += 1
    tail = "".join(buf).strip()
    if tail:
        parts.append(tail)
    return parts


def _extract_root_for_shell(command_part: str, shell_type: ShellType) -> str:
    """按 shell 类型提取命令首词。

    逻辑：
    1. 对 bash 使用 `shlex.split(posix=True)`；
    2. 对 cmd/powershell 使用 `shlex.split(posix=False)`；
    3. 失败时回退空白切分；
    4. 返回首 token 小写形式。
    """
    try:
        tokens = shlex.split(command_part, posix=(shell_type == "bash"))
    except Exception:
        tokens = command_part.strip().split()
    if not tokens:
        return ""
    return tokens[0].strip().lower()


def _parse_command_ast(command: str, shell_type: ShellType) -> list[CommandAstNode]:
    """按指定 shell 解析命令，返回轻量 AST 节点列表。

    逻辑：
    1. 根据 `shell_type` 选择对应切分器；
    2. 对每个片段提取 root command；
    3. 组装 `CommandAstNode` 列表并返回。

    关键边界：
    - root 可能为空，调用方需自行决定如何处理。
    """
    if shell_type == "bash":
        parts = _split_bash_statements(command)
    elif shell_type == "cmd":
        parts = _split_cmd_statements(command)
    else:
        parts = _split_powershell_statements(command)
    return [
        CommandAstNode(parser=shell_type, raw=part, root=_extract_root_for_shell(part, shell_type))
        for part in parts
    ]


def _run_bash_command(
    command: str, cwd: str, timeout_seconds: int, output_encoding: str
) -> subprocess.CompletedProcess[str]:
    """执行 bash 命令。

    逻辑：
    1. 通过 `bash -lc` 运行命令字符串；
    2. 捕获 stdout/stderr 并按 UTF-8 解码；
    3. 保留非零退出码，由上层统一格式化。
    """
    return subprocess.run(
        ["bash", "-lc", command],
        cwd=cwd,
        capture_output=True,
        text=True,
        encoding=output_encoding,
        errors="replace",
        timeout=timeout_seconds,
        check=False,
    )


def _run_cmd_command(
    command: str, cwd: str, timeout_seconds: int, output_encoding: str
) -> subprocess.CompletedProcess[str]:
    """执行 cmd 命令。

    逻辑：
    1. 通过 `cmd /c` 运行命令字符串；
    2. 捕获 stdout/stderr 供上层统一处理。

    关键边界：
    - 非 Windows 环境可能不存在 `cmd`，异常由上层统一返回 `ERROR`。
    """
    return subprocess.run(
        ["cmd", "/c", command],
        cwd=cwd,
        capture_output=True,
        text=True,
        encoding=output_encoding,
        errors="replace",
        timeout=timeout_seconds,
        check=False,
    )


def _run_powershell_command(
    command: str, cwd: str, timeout_seconds: int, output_encoding: str
) -> subprocess.CompletedProcess[str]:
    """执行 powershell 命令。

    逻辑：
    1. 优先尝试 `pwsh`，失败再尝试 `powershell`；
    2. 使用 `-NoProfile -Command` 执行；
    3. 找不到可执行文件时抛 `RuntimeError`。
    """
    for exe in ("pwsh", "powershell"):
        try:
            return subprocess.run(
                [exe, "-NoProfile", "-Command", command],
                cwd=cwd,
                capture_output=True,
                text=True,
                encoding=output_encoding,
                errors="replace",
                timeout=timeout_seconds,
                check=False,
            )
        except FileNotFoundError:
            continue
    raise RuntimeError("未找到 powershell/pwsh 可执行文件。")


def _popen_by_shell_type(
    shell_type: ShellType,
    command: str,
    cwd: str,
    output_encoding: str,
) -> subprocess.Popen[str]:
    """按 shell 类型启动可托管进程。

    逻辑：
    1. 根据 `shell_type` 组装与同步执行一致的命令参数；
    2. 使用 stdout/stderr pipe 捕获输出；
    3. POSIX 下启动独立进程组，便于后续 cancel 工具终止整组。

    关键边界：
    - powershell 仍按 `pwsh` → `powershell` 顺序探测；
    - 本函数只启动进程，不等待完成。
    """
    popen_kwargs: dict[str, object] = {
        "cwd": cwd,
        "stdout": subprocess.PIPE,
        "stderr": subprocess.PIPE,
        "text": True,
        "encoding": output_encoding,
        "errors": "replace",
    }
    if os.name != "nt":
        popen_kwargs["start_new_session"] = True
    if shell_type == "bash":
        return subprocess.Popen(["bash", "-lc", command], **popen_kwargs)
    if shell_type == "cmd":
        return subprocess.Popen(["cmd", "/C", command], **popen_kwargs)
    for exe in ("pwsh", "powershell"):
        try:
            return subprocess.Popen([exe, "-NoProfile", "-Command", command], **popen_kwargs)
        except FileNotFoundError:
            continue
    raise RuntimeError("未找到 powershell/pwsh 可执行文件。")


async def _wait_shell_job(job_id: str) -> str:
    """后台等待 shell job 完成并返回压缩摘要。

    逻辑：
    1. 在线程中等待原 `Popen.communicate()` 完成；
    2. 写入 job 终态、stdout/stderr 与退出码；
    3. 返回模型友好的完成摘要，供异步工具回灌。

    关键边界：
    - 取消等待时会尝试取消 shell job，避免孤儿进程继续跑；
    - 输出进入上下文前按 `MAX_BASH_OUTPUT_CHARS` 裁剪。
    """
    job = _SHELL_JOBS[job_id]
    try:
        stdout, stderr = await asyncio.to_thread(job.process.communicate)
        job.exit_code = job.process.returncode
        job.stdout = stdout or ""
        job.stderr = stderr or ""
        job.status = "succeeded" if job.exit_code == 0 else "failed"
    except asyncio.CancelledError:
        _terminate_shell_job_process(job)
        job.status = "cancelled"
        job.stderr = (job.stderr + "\nShellJob 等待任务被取消。").strip()
        raise
    finally:
        job.finished_at_unix_ms = _now_ms()

    stdout_text, out_truncated = _clip_text(job.stdout, MAX_BASH_OUTPUT_CHARS)
    stderr_text, err_truncated = _clip_text(job.stderr, MAX_BASH_OUTPUT_CHARS)
    parts = [
        f"[BASH_JOB_DONE] job_id={job.job_id} shell_type={job.shell_type} status={job.status} exit_code={job.exit_code}",
        f"cwd={job.cwd!r}",
        f"async_job_id={job.async_job_id}",
        "--- STDOUT ---",
        stdout_text,
        "--- STDERR ---",
        stderr_text,
    ]
    if out_truncated or err_truncated:
        parts.append(
            f"[TRUNCATED] 输出超过 {MAX_BASH_OUTPUT_CHARS} 字符，已对 stdout/stderr 分别截断；可用 bash_job_tail 查询尾部。"
        )
    return "\n".join(parts)


def _terminate_shell_job_process(job: ShellJob) -> None:
    """终止 shell job 对应进程。

    逻辑：
    1. 若进程已结束则直接返回；
    2. POSIX 下优先终止整个进程组；
    3. 失败时回退到单进程 `terminate()`。
    """
    if job.process.poll() is not None:
        return
    try:
        if os.name != "nt":
            os.killpg(job.process.pid, signal.SIGTERM)
        else:
            job.process.terminate()
    except Exception:
        job.process.terminate()


def _blocked_non_root_password_prompting_shell(command: str, shell_type: ShellType) -> str | None:
    """判定非 root 下是否应拦截「可能读 TTY 密码」的 bash 片段。

    逻辑：
    1. 仅 **`shell_type == bash`** 参与判定；
    2. 使用 **`get_host_snapshot()`** 的 **os_kind / effective_uid**，避免重复 **`os.geteuid`**；
    3. **Windows**（含 cygwin/msys 归类）、**effective_uid 不可用** 或 **root** 时放行；
    4. **`su - <user> -c`**（含 `-l` / `--login`）任一片段行首命中 → 返回 **`ERROR`**（跨用户 su）；
    5. 行首 **`sudo` / `sudoedit`** 且片段内 **无** **`-n`** / **`--non-interactive`** → 返回 **`ERROR`**（避免无 TTY 阻塞；免密场景请显式 **`sudo -n`**）。

    关键分支或边界：
    - **NOPASSWD** 但未写 **`-n`** 仍会被拦，迫使用户使用非交互语义（失败快于挂死）；
    - 不做完整 bash 赋值/`env` 前缀建模，复杂绕过不在此覆盖。

    Args:
        command: 原始命令串。
        shell_type: 已解析的执行 shell。

    Returns:
        需要拦截时返回错误字符串；否则返回 `None`。
    """

    if shell_type != "bash":
        return None
    snap = get_host_snapshot()
    if snap.os_kind == "windows":
        return None
    euid = snap.effective_uid
    if euid is None:
        return None
    if euid == 0:
        return None
    for segment in _split_bash_statements(command):
        s = segment.strip()
        if not s:
            continue
        if _SU_LOGIN_WITH_C_RE.match(s):
            return (
                "ERROR: 当前进程非 root，不允许执行 `su - <user> -c ...` 形式的跨用户登录 shell。"
            )
        if _SUDO_FAMILY_INVOCATION_RE.match(s):
            if _SUDO_NONINTERACTIVE_FLAG_RE.search(s):
                continue
            return (
                "ERROR: 当前进程非 root，不允许执行可能提示输入密码的 `sudo`/`sudoedit`（片段中缺少 `-n` 或 `--non-interactive`）。"
            )
    return None


def _run_by_shell_type(
    shell_type: ShellType,
    command: str,
    cwd: str,
    timeout_seconds: int,
    output_encoding: str,
) -> subprocess.CompletedProcess[str]:
    """根据 shell 类型分发到对应执行方法。

    逻辑：
    1. `bash` 调用 `_run_bash_command`；
    2. `cmd` 调用 `_run_cmd_command`；
    3. `powershell` 调用 `_run_powershell_command`。
    """
    if shell_type == "bash":
        return _run_bash_command(command, cwd, timeout_seconds, output_encoding)
    if shell_type == "cmd":
        return _run_cmd_command(command, cwd, timeout_seconds, output_encoding)
    return _run_powershell_command(command, cwd, timeout_seconds, output_encoding)


def _resolve_shell_type(shell_type: Optional[ShellType]) -> ShellType:
    """解析最终执行 shell：显式优先，未指定时按宿主机系统自动选择。

    逻辑：
    1. 若调用方显式传入 `shell_type`，直接返回该值；
    2. 若未传（`None`），读取 `detect_host_os()`；
    3. Windows 默认 `powershell`，其余系统默认 `bash`。

    关键边界：
    - 自动模式不会返回 `cmd`；`cmd` 仅在显式传参时使用；
    - WSL 识别为 Linux，因此默认 `bash`。

    Args:
        shell_type: 调用方传入的 shell 类型或 `None`。

    Returns:
        最终用于解析、策略校验与执行的 shell 类型。
    """
    if shell_type is not None:
        return shell_type
    host_os = detect_host_os()
    if host_os == HostOsKind.WINDOWS:
        return "powershell"
    return "bash"


@tool("bash_run")
def bash_run(
    command: str,
    timeout_seconds: Optional[int] = None,
    cwd: Optional[str] = None,
    shell_type: Optional[ShellType] = None,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：执行 bash/cmd/powershell 命令并返回标准输出。

    字段说明：
    - `command`：命令字符串（必填）。
    - `timeout_seconds`：超时秒数（可选，默认 30，范围 1-600）。
    - `cwd`：执行目录（可选，默认当前目录；需存在且为目录）。
    - `shell_type`：目标 shell（可选，默认 `None`；`None` 时按系统自动选择：Windows=`powershell`，其它=`bash`；也可显式传 `bash/cmd/powershell`）。

    返回说明：
    - 成功或非零退出：返回结构化文本，含 `status`、`exit_code`、`stdout`、`stderr`。
    - 失败：返回 `ERROR: ...`；超时返回 `status=TIMEOUT`。
    - Linux/macOS 等非 Windows：**非 root** 若以 **bash** 执行：语句片段行首匹配 **`su - <user> -c ...`**（含 `-l` / `--login`），或行首 **`sudo`/`sudoedit`** 且未含 **`-n`/`--non-interactive`**，直接返回错误（不创建子进程）。

    调用范例：
    - `bash_run({"command":"git status"})`
    - `bash_run({"command":"dir","shell_type":"cmd","timeout_seconds":10})`
    """
    try:
        cmd = command.strip()
        if not cmd:
            return "ERROR: command 不能为空。"

        timeout = DEFAULT_BASH_TIMEOUT_SECONDS if timeout_seconds is None else int(timeout_seconds)
        timeout = max(1, min(timeout, MAX_BASH_TIMEOUT_SECONDS))

        run_cwd = Path(cwd).expanduser().resolve() if cwd else Path.cwd().resolve()
        if not run_cwd.exists():
            return f"ERROR: cwd 不存在：{str(run_cwd)!r}"
        if not run_cwd.is_dir():
            return f"ERROR: cwd 不是目录：{str(run_cwd)!r}"

        resolved_shell_type = _resolve_shell_type(shell_type)
        if resolved_shell_type not in ("bash", "cmd", "powershell"):
            return f"ERROR: 不支持的 shell_type：{shell_type!r}"
        output_encoding = _resolve_shell_output_encoding()

        # 非 root 下禁止可能读 TTY 密码的 su/sudo（无 sudo -n）；避免 subprocess 挂死。
        blocked = _blocked_non_root_password_prompting_shell(cmd, resolved_shell_type)
        if blocked is not None:
            return blocked

        process = _popen_by_shell_type(
            resolved_shell_type,
            cmd,
            str(run_cwd),
            output_encoding,
        )
        try:
            stdout, stderr = process.communicate(timeout=timeout)
            stdout = stdout or ""
            stderr = stderr or ""
            status = "OK" if process.returncode == 0 else "NON_ZERO_EXIT"
            exit_code = process.returncode
        except subprocess.TimeoutExpired:
            # 超时不再杀进程：登记同一个正在运行的 Popen，交给后台协程等待终态并异步回灌摘要。
            job_id = str(uuid4())
            job = ShellJob(
                job_id=job_id,
                command=cmd,
                cwd=str(run_cwd),
                shell_type=resolved_shell_type,
                timeout_seconds=timeout,
                output_encoding=output_encoding,
                process=process,
                started_at_unix_ms=_now_ms(),
            )
            _SHELL_JOBS[job_id] = job
            async_job_id = ""
            async_error = ""
            if isinstance(context, OpenAIConversationContext):
                try:
                    async_store = get_async_tool_result_store()
                    async_job = async_store.submit_coroutine(
                        session_id=context.session_id,
                        client_id=(context.sse_client_id or "").strip(),
                        tool_name="bash_run",
                        coroutine_obj=_wait_shell_job(job_id),
                    )
                    async_job_id = async_job.job_id
                    job.async_job_id = async_job_id
                except Exception as exc:  # noqa: BLE001
                    async_error = str(exc)
            parts = [
                f"[BASH_RESULT] shell_type={resolved_shell_type} status=RUNNING job_id={job_id}",
                f"cwd={str(run_cwd)!r}",
                f"timeout_seconds={timeout}",
                f"output_encoding={output_encoding}",
                f"async_job_id={async_job_id}",
                "命令超过同步等待时间，已降级为后台 ShellJob；完成后会自动返回结果",
                "可用 bash_job_status / bash_job_tail / bash_job_cancel 查询或取消。",
            ]
            if async_error:
                parts.append(f"[ASYNC_CALLBACK_WARNING] 异步回灌未注册成功：{async_error}")
            return "\n".join(parts)

        stdout, out_truncated = _clip_text(stdout, MAX_BASH_OUTPUT_CHARS)
        stderr, err_truncated = _clip_text(stderr, MAX_BASH_OUTPUT_CHARS)

        parts = [
            f"[BASH_RESULT] shell_type={resolved_shell_type} status={status} exit_code={exit_code}",
            f"cwd={str(run_cwd)!r}",
            f"timeout_seconds={timeout}",
            f"output_encoding={output_encoding}",
            "--- STDOUT ---",
            stdout,
            "--- STDERR ---",
            stderr,
        ]
        if out_truncated or err_truncated:
            parts.append(
                f"[TRUNCATED] 输出超过 {MAX_BASH_OUTPUT_CHARS} 字符，已对 stdout/stderr 分别截断。"
            )
        return "\n".join(parts)
    except Exception as exc:
        return f"ERROR: bash_run 失败: {exc}"


bash_run.args_schema = BashRunArgs  # type: ignore[attr-defined]


@tool("bash_job_status")
def bash_job_status(job_id: str) -> str:
    """使用场景：查询 `bash_run` 超时后台化后返回的 ShellJob 状态。

    字段说明：
    - job_id: `bash_run` 返回的 ShellJob ID。

    返回说明：
    - 成功：返回结构化文本，含 status、exit_code、cwd、时间戳与 async_job_id。
    - 失败：返回 `ERROR: ...`。

    调用范例：
    - `bash_job_status({"job_id":"..."})`
    """
    job = _SHELL_JOBS.get(str(job_id or "").strip())
    if job is None:
        return f"ERROR: 未找到 ShellJob：{job_id!r}"
    if job.process.poll() is not None and job.status == "running":
        job.exit_code = job.process.returncode
        job.status = "succeeded" if job.exit_code == 0 else "failed"
        job.finished_at_unix_ms = _now_ms()
    return "\n".join(
        [
            f"[BASH_JOB_STATUS] job_id={job.job_id} status={job.status} exit_code={job.exit_code}",
            f"shell_type={job.shell_type}",
            f"cwd={job.cwd!r}",
            f"timeout_seconds={job.timeout_seconds}",
            f"async_job_id={job.async_job_id}",
            f"started_at_unix_ms={job.started_at_unix_ms}",
            f"finished_at_unix_ms={job.finished_at_unix_ms}",
        ]
    )


bash_job_status.args_schema = ShellJobIdArgs  # type: ignore[attr-defined]


@tool("bash_job_tail")
def bash_job_tail(job_id: str, max_chars: Optional[int] = None) -> str:
    """使用场景：读取 ShellJob 已完成输出的尾部；运行中 job 仅返回当前可用状态。

    字段说明：
    - job_id: `bash_run` 返回的 ShellJob ID。
    - max_chars: stdout/stderr 各自最多返回字符数（可选，默认 8000）。

    返回说明：
    - 成功：返回 stdout/stderr 尾部文本。
    - 失败：返回 `ERROR: ...`。

    调用范例：
    - `bash_job_tail({"job_id":"..."})`
    - `bash_job_tail({"job_id":"...","max_chars":2000})`
    """
    job = _SHELL_JOBS.get(str(job_id or "").strip())
    if job is None:
        return f"ERROR: 未找到 ShellJob：{job_id!r}"
    limit = SHELL_JOB_TAIL_CHARS if max_chars is None else max(1, min(int(max_chars), MAX_BASH_OUTPUT_CHARS))
    if job.status == "running":
        return (
            f"[BASH_JOB_TAIL] job_id={job.job_id} status=running\n"
            "进程仍在运行；stdout/stderr 将在完成后由后台等待协程写入。"
        )
    stdout = (job.stdout or "")[-limit:]
    stderr = (job.stderr or "")[-limit:]
    return "\n".join(
        [
            f"[BASH_JOB_TAIL] job_id={job.job_id} status={job.status} exit_code={job.exit_code}",
            "--- STDOUT_TAIL ---",
            stdout,
            "--- STDERR_TAIL ---",
            stderr,
        ]
    )


bash_job_tail.args_schema = BashJobTailArgs  # type: ignore[attr-defined]


@tool("bash_job_cancel")
def bash_job_cancel(job_id: str) -> str:
    """使用场景：取消仍在运行的 ShellJob。

    字段说明：
    - job_id: `bash_run` 返回的 ShellJob ID。

    返回说明：
    - 成功：返回取消后的状态；已结束 job 返回当前终态。
    - 失败：返回 `ERROR: ...`。

    调用范例：
    - `bash_job_cancel({"job_id":"..."})`
    """
    job = _SHELL_JOBS.get(str(job_id or "").strip())
    if job is None:
        return f"ERROR: 未找到 ShellJob：{job_id!r}"
    if job.process.poll() is None:
        _terminate_shell_job_process(job)
        job.status = "cancelled"
        job.finished_at_unix_ms = _now_ms()
        return f"[BASH_JOB_CANCELLED] job_id={job.job_id} status=cancelled"
    if job.status == "running":
        job.exit_code = job.process.returncode
        job.status = "succeeded" if job.exit_code == 0 else "failed"
        job.finished_at_unix_ms = _now_ms()
    return f"[BASH_JOB_CANCELLED] job_id={job.job_id} status={job.status} exit_code={job.exit_code}"


bash_job_cancel.args_schema = ShellJobIdArgs  # type: ignore[attr-defined]
