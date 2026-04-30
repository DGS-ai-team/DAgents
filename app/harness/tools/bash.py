"""统一的 shell 执行工具（bash/cmd/powershell）。

说明：由 Agent 选择目标 shell，并由本模块做分 shell 解析、策略校验与执行。
"""

from __future__ import annotations

import shlex
import subprocess
from pathlib import Path
from typing import Literal, Optional

from pydantic import BaseModel, ConfigDict

from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext
from app.harness.tools.host_platform import HostOsKind, detect_host_os
from app.harness.tools.tool import tool

ShellType = Literal["bash", "cmd", "powershell"]

DEFAULT_BASH_TIMEOUT_SECONDS = 30
MAX_BASH_TIMEOUT_SECONDS = 600
MAX_BASH_OUTPUT_CHARS = 12000


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
    - 是否审批：由 `tool.should_require_tool_approval` 统一判断，本工具只负责命令执行。

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

        try:
            completed = _run_by_shell_type(
                resolved_shell_type,
                cmd,
                str(run_cwd),
                timeout,
                output_encoding,
            )
            stdout = completed.stdout or ""
            stderr = completed.stderr or ""
            status = "OK" if completed.returncode == 0 else "NON_ZERO_EXIT"
            exit_code = completed.returncode
        except subprocess.TimeoutExpired as exc:
            stdout = (exc.stdout or "") if isinstance(exc.stdout, str) else ""
            stderr = (exc.stderr or "") if isinstance(exc.stderr, str) else ""
            status = "TIMEOUT"
            exit_code = -1
            stderr = (stderr + f"\n命令执行超时：{timeout}s").strip()

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
