"""`dagents serve` 后台进程与 startup/shutdown 钩子。"""

from __future__ import annotations

import argparse
import os
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from app.config.env import load_env

_SERVE_PID_FILE = "dagents-api.pid"
_SERVE_LOG_FILE = "logs/dagents-api.log"
_STARTUP_DIR = Path(".runtime") / "scripts" / "serve" / "startup.d"
_SHUTDOWN_DIR = Path(".runtime") / "scripts" / "serve" / "shutdown.d"


def add_serve_arguments(parser: argparse.ArgumentParser) -> None:
    """为 `serve` / `api` 子命令注册后台与钩子相关参数。"""
    parser.add_argument(
        "--foreground",
        "-f",
        action="store_true",
        help="前台运行（默认后台守护进程）",
    )
    parser.add_argument(
        "--stop",
        action="store_true",
        help="停止已后台运行的 Agent API",
    )
    parser.add_argument(
        "--status",
        action="store_true",
        help="查看后台 Agent API 是否在运行",
    )
    parser.add_argument(
        "--no-hooks",
        action="store_true",
        help="跳过 startup.d / shutdown.d 钩子脚本",
    )
    parser.add_argument(
        "--no-wait",
        action="store_true",
        help="后台启动后不等待 /health 就绪",
    )
    parser.add_argument(
        "extra",
        nargs=argparse.REMAINDER,
        help=argparse.SUPPRESS,
    )


def run_serve_command(
    home: Path,
    *,
    binary_stem: str,
    script_name: str,
    extra_args: list[str],
    foreground: bool,
    stop: bool,
    status: bool,
    no_hooks: bool,
    no_wait: bool,
) -> int:
    """执行 `dagents serve`：后台启动、停止、状态查询或前台运行。

    逻辑：
    1. `--stop` / `--status` 只操作 PID 文件与进程；
    2. 默认后台 `Popen` 启动 API，日志写入 `logs/dagents-api.log`；
    3. 启动前执行 `.runtime/scripts/serve/startup.d/*`，停止时执行 `shutdown.d/*`；
    4. `--foreground` 时仍前台运行（不写入 PID，不跑 shutdown 钩子）。

    关键边界：
    - 已有 PID 且进程存活时拒绝重复启动；
    - Windows 使用 `taskkill /PID` 停止；Unix 使用 `SIGTERM`。
    """
    home = home.resolve()
    load_env(home)
    pid_path = home / _SERVE_PID_FILE
    log_path = home / _SERVE_LOG_FILE

    if status:
        return _print_serve_status(home, pid_path)
    if stop:
        return _stop_serve_daemon(home, pid_path, run_hooks=not no_hooks)

    if _read_pid(pid_path) is not None and _pid_alive(_read_pid(pid_path)):
        print(f"[dagents] Agent API already running (pid={_read_pid(pid_path)}, pid_file={pid_path})", file=sys.stderr)
        return 1

    if foreground:
        if not no_hooks:
            hook_rc = _run_hook_dir(home, _STARTUP_DIR, "startup")
            if hook_rc != 0:
                return hook_rc
        return _run_api_foreground(home, binary_stem, script_name, extra_args)

    if not no_hooks:
        hook_rc = _run_hook_dir(home, _STARTUP_DIR, "startup")
        if hook_rc != 0:
            return hook_rc

    try:
        pid = _start_serve_daemon(home, binary_stem, script_name, extra_args, log_path)
    except OSError as exc:
        print(f"[dagents] failed to start Agent API: {exc}", file=sys.stderr)
        return 1

    pid_path.write_text(f"{pid}\n", encoding="utf-8")
    host = os.getenv("API_HOST", "127.0.0.1").strip() or "127.0.0.1"
    port = os.getenv("API_PORT", "8000").strip() or "8000"
    url = f"http://{host}:{port}"
    print(f"[dagents] Agent API started in background (pid={pid})")
    print(f"[dagents] log: {log_path}")
    print(f"[dagents] url: {url}")
    if no_wait:
        return 0
    if _wait_for_health(url, timeout_s=30.0):
        print("[dagents] health check ok")
        return 0
    print("[dagents] health check timed out; process may still be starting", file=sys.stderr)
    return 0


def _print_serve_status(home: Path, pid_path: Path) -> int:
    """打印后台 API 进程状态。"""
    pid = _read_pid(pid_path)
    if pid is None:
        print(f"[dagents] Agent API is not running (no pid file: {pid_path})")
        return 1
    if not _pid_alive(pid):
        print(f"[dagents] Agent API is not running (stale pid={pid}, pid_file={pid_path})")
        return 1
    print(f"[dagents] Agent API is running (pid={pid}, home={home})")
    return 0


def _stop_serve_daemon(home: Path, pid_path: Path, *, run_hooks: bool) -> int:
    """停止后台 API 并可选执行 shutdown 钩子。"""
    pid = _read_pid(pid_path)
    if pid is None:
        print("[dagents] Agent API is not running")
        return 0
    if not _pid_alive(pid):
        pid_path.unlink(missing_ok=True)
        print(f"[dagents] removed stale pid file (pid={pid})")
        return 0
    _terminate_pid(pid)
    if not _wait_pid_exit(pid, timeout_s=15.0):
        print(f"[dagents] process {pid} did not exit in time", file=sys.stderr)
        return 1
    pid_path.unlink(missing_ok=True)
    print(f"[dagents] Agent API stopped (pid={pid})")
    if run_hooks:
        return _run_hook_dir(home, _SHUTDOWN_DIR, "shutdown")
    return 0


def _start_serve_daemon(
    home: Path,
    binary_stem: str,
    script_name: str,
    extra_args: list[str],
    log_path: Path,
) -> int:
    """后台启动 API 进程并将 stdout/stderr 重定向到日志文件。"""
    log_path.parent.mkdir(parents=True, exist_ok=True)
    command = _api_command(home, binary_stem, script_name, extra_args)
    log_handle = log_path.open("ab")
    kwargs: dict[str, object] = {
        "cwd": str(home),
        "stdout": log_handle,
        "stderr": subprocess.STDOUT,
        "stdin": subprocess.DEVNULL,
    }
    if os.name == "nt":
        kwargs["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP | subprocess.DETACHED_PROCESS  # type: ignore[attr-defined]
    else:
        kwargs["start_new_session"] = True
    proc = subprocess.Popen(command, **kwargs)  # noqa: S603
    log_handle.close()
    return int(proc.pid)


def _run_api_foreground(home: Path, binary_stem: str, script_name: str, extra_args: list[str]) -> int:
    """前台运行 API（开发调试）。"""
    command = _api_command(home, binary_stem, script_name, extra_args)
    return subprocess.call(command, cwd=str(home))  # noqa: S603


def _api_command(home: Path, binary_stem: str, script_name: str, extra_args: list[str]) -> list[str]:
    """解析 API 可执行文件或源码启动命令。"""
    exe_name = f"{binary_stem}.exe" if os.name == "nt" else binary_stem
    binary = home / exe_name
    if binary.exists():
        return [str(binary), *extra_args]
    script = _repo_root() / script_name
    if script.exists():
        return [sys.executable, str(script), *extra_args]
    raise FileNotFoundError(f"missing {exe_name} and {script_name} under {home}")


def _run_hook_dir(home: Path, hook_dir: Path, phase: str) -> int:
    """按文件名顺序执行钩子目录下的脚本。

    逻辑：
    1. 目录不存在则跳过；
    2. 仅执行后缀为 `.sh` / `.bat` / `.cmd` 的文件；
    3. 任一脚本非零退出则中止并返回该退出码。

    关键边界：
    - 钩子以 `home` 为工作目录，便于访问 `.env` 与 `.runtime`。
    """
    path = (home / hook_dir).resolve()
    if not path.is_dir():
        return 0
    suffixes = {".sh", ".bat", ".cmd"} if os.name == "nt" else {".sh"}
    scripts = sorted(
        item for item in path.iterdir() if item.is_file() and item.suffix.lower() in suffixes
    )
    if not scripts:
        return 0
    for script in scripts:
        print(f"[dagents] {phase}: {script.name}")
        rc = _run_hook_script(home, script)
        if rc != 0:
            print(f"[dagents] {phase} hook failed: {script} (exit={rc})", file=sys.stderr)
            return rc
    return 0


def _run_hook_script(home: Path, script: Path) -> int:
    """执行单个 startup/shutdown 钩子。"""
    if script.suffix.lower() == ".sh":
        if os.name == "nt":
            return subprocess.call(["bash", str(script)], cwd=str(home))  # noqa: S603
        return subprocess.call([str(script)], cwd=str(home))  # noqa: S603
    return subprocess.call([str(script)], cwd=str(home), shell=True)  # noqa: S603


def _read_pid(pid_path: Path) -> int | None:
    """读取 PID 文件。"""
    if not pid_path.is_file():
        return None
    raw = pid_path.read_text(encoding="utf-8").strip()
    if not raw:
        return None
    try:
        return int(raw)
    except ValueError:
        return None


def _pid_alive(pid: int) -> bool:
    """检测进程是否仍存在。"""
    if pid <= 0:
        return False
    if os.name == "nt":
        completed = subprocess.run(  # noqa: S603
            ["tasklist", "/FI", f"PID eq {pid}"],
            capture_output=True,
            text=True,
            check=False,
        )
        return str(pid) in (completed.stdout or "")
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    else:
        return True


def _terminate_pid(pid: int) -> None:
    """向 API 进程发送终止信号。"""
    if os.name == "nt":
        subprocess.run(["taskkill", "/PID", str(pid), "/T", "/F"], check=False)  # noqa: S603
        return
    try:
        os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        pass


def _wait_pid_exit(pid: int, *, timeout_s: float) -> bool:
    """等待进程退出。"""
    deadline = time.monotonic() + max(0.0, timeout_s)
    while time.monotonic() < deadline:
        if not _pid_alive(pid):
            return True
        time.sleep(0.2)
    return not _pid_alive(pid)


def _wait_for_health(base_url: str, *, timeout_s: float) -> bool:
    """轮询 GET /health 直至成功或超时。"""
    url = base_url.rstrip("/") + "/health"
    deadline = time.monotonic() + max(0.0, timeout_s)
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=2.0) as resp:  # noqa: S310
                if resp.status == 200:
                    return True
        except (urllib.error.URLError, TimeoutError, OSError):
            pass
        time.sleep(0.3)
    return False


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[2]
