"""统一的 shell 执行工具（bash/cmd/powershell）。

说明：由 Agent 选择目标 shell，并由本模块做分 shell 解析、策略校验与执行。
"""

from __future__ import annotations

import os
import shlex
import subprocess
from pathlib import Path
from typing import Literal, Optional

from pydantic import BaseModel, ConfigDict

from app.context.models import OpenAIConversationContext
from app.harness.tools.host_platform import HostOsKind, detect_host_os
from app.harness.tools.tooling import tool

ShellType = Literal["bash", "cmd", "powershell"]

DEFAULT_BASH_TIMEOUT_SECONDS = 30
MAX_BASH_TIMEOUT_SECONDS = 600
MAX_BASH_OUTPUT_CHARS = 12000
DEFAULT_SHELL_POLICY_DIR = ".agent/policy/shell"


class CommandAstNode(BaseModel):
    """命令 AST 节点（轻量版）。

    逻辑：
    1. 用于承载每个命令片段的原文和首命令词；
    2. `parser` 记录由哪种 shell 解析器得到；
    3. 上层基于 `root` 做黑白名单判断与审批提示。

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


def _policy_dir() -> Path:
    """返回策略目录路径。

    逻辑：
    1. 优先读取 `SHELL_POLICY_DIR`；
    2. 未设置时使用默认 `.agent/policy/shell`；
    3. 统一展开为绝对路径。
    """
    raw = os.environ.get("SHELL_POLICY_DIR", "").strip()
    chosen = raw or DEFAULT_SHELL_POLICY_DIR
    return Path(chosen).expanduser().resolve()


def _ensure_policy_files() -> Path:
    """确保策略目录与 3 套黑白名单文件存在。

    逻辑：
    1. 创建策略目录；
    2. 为 `bash/cmd/powershell` 各自创建 `*.allow.txt` 与 `*.deny.txt`（若不存在）；
    3. 返回策略目录路径。

    副作用：
    - 可能在文件系统中创建目录与空文件。
    """
    d = _policy_dir()
    d.mkdir(parents=True, exist_ok=True)
    for shell in ("bash", "cmd", "powershell"):
        for suffix in ("allow", "deny"):
            fp = d / f"{shell}.{suffix}.txt"
            if not fp.exists():
                fp.write_text("", encoding="utf-8")
    return d


def _read_roots_file(path: Path) -> set[str]:
    """读取命令首词名单文件并返回集合。

    逻辑：
    1. 文件不存在时返回空集合；
    2. 按行读取 UTF-8 内容；
    3. 忽略空行和 `#` 注释行；
    4. 标准化为小写集合。
    """
    if not path.is_file():
        return set()
    roots: set[str] = set()
    text = path.read_text(encoding="utf-8", errors="replace")
    for line in text.splitlines():
        s = line.strip()
        if not s or s.startswith("#"):
            continue
        roots.add(s.lower())
    return roots


def _load_policy_sets(shell_type: ShellType) -> tuple[set[str], set[str]]:
    """按 shell 类型加载 allow/deny 集合。

    逻辑：
    1. 确保策略目录和对应文件存在；
    2. 读取 `<shell>.allow.txt` 与 `<shell>.deny.txt`；
    3. 返回 `(allow_set, deny_set)`。

    Args:
        shell_type: 目标 shell（bash/cmd/powershell）。
    """
    d = _ensure_policy_files()
    allow_set = _read_roots_file(d / f"{shell_type}.allow.txt")
    deny_set = _read_roots_file(d / f"{shell_type}.deny.txt")
    return allow_set, deny_set


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
    - root 可能为空，后续策略校验会拒绝。
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


def _validate_command_policy(command: str, shell_type: ShellType) -> tuple[Optional[str], list[str]]:
    """执行策略校验，返回阻断错误和待确认命令。

    逻辑：
    1. 解析指定 shell 的 AST 节点；
    2. 加载该 shell 的 allow/deny 集合；
    3. 命中 deny 立即拒绝；
    4. 不在 allow 的命令加入待确认列表。

    Returns:
        `(error, need_confirm_roots)`。
    """
    nodes = _parse_command_ast(command, shell_type)
    if not nodes:
        return "命令为空或仅包含分隔符。", []
    allow_set, deny_set = _load_policy_sets(shell_type)
    need_confirm: list[str] = []
    for node in nodes:
        if not node.root:
            return f"无法识别命令片段：{node.raw!r}", []
        if node.root in deny_set:
            return f"命令被黑名单拦截：{node.root!r}", []
        if node.root not in allow_set and node.root not in need_confirm:
            need_confirm.append(node.root)
    return None, need_confirm


def _confirm_non_whitelist_commands(
    command: str,
    cwd: str,
    timeout_seconds: int,
    shell_type: ShellType,
    roots: list[str],
) -> bool:
    """对白名单外命令做放行判断（审批由 runtime 统一处理）。

    逻辑：
    1. roots 为空则直接放行；
    2. roots 非空时也返回 True，由上层 runtime 的统一审批流程负责拦截与确认。

    关键边界：
    - 在 OpenAI 直连运行时中，工具层不再直接触发 `interrupt`，避免多处审批分支。
    """
    del command, cwd, timeout_seconds, shell_type
    return True


def _run_bash_command(command: str, cwd: str, timeout_seconds: int) -> subprocess.CompletedProcess[str]:
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
        encoding="utf-8",
        errors="replace",
        timeout=timeout_seconds,
        check=False,
    )


def _run_cmd_command(command: str, cwd: str, timeout_seconds: int) -> subprocess.CompletedProcess[str]:
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
        encoding="utf-8",
        errors="replace",
        timeout=timeout_seconds,
        check=False,
    )


def _run_powershell_command(command: str, cwd: str, timeout_seconds: int) -> subprocess.CompletedProcess[str]:
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
                encoding="utf-8",
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
) -> subprocess.CompletedProcess[str]:
    """根据 shell 类型分发到对应执行方法。

    逻辑：
    1. `bash` 调用 `_run_bash_command`；
    2. `cmd` 调用 `_run_cmd_command`；
    3. `powershell` 调用 `_run_powershell_command`。
    """
    if shell_type == "bash":
        return _run_bash_command(command, cwd, timeout_seconds)
    if shell_type == "cmd":
        return _run_cmd_command(command, cwd, timeout_seconds)
    return _run_powershell_command(command, cwd, timeout_seconds)


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
    - 白名单外命令：由 runtime 上层统一审批，本工具只执行策略解析与命令执行。

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

        policy_error, need_confirm_roots = _validate_command_policy(cmd, resolved_shell_type)
        if policy_error:
            return f"ERROR: {policy_error}"
        if not _confirm_non_whitelist_commands(
            command=cmd,
            cwd=str(run_cwd),
            timeout_seconds=timeout,
            shell_type=resolved_shell_type,
            roots=need_confirm_roots,
        ):
            return "用户拒绝执行工具调用"

        try:
            completed = _run_by_shell_type(resolved_shell_type, cmd, str(run_cwd), timeout)
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
