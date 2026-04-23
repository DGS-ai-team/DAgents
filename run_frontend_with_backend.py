"""
前端开发入口：先检查后端，未启动则先拉起后端，再启动前端。

用法（在 DAgents 根目录）:
  python run_frontend_with_backend.py
  python run_frontend_with_backend.py --frontend-cmd npm
  python run_frontend_with_backend.py --check-only
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
import time
from pathlib import Path
from urllib.error import URLError
from urllib.parse import urlparse
from urllib.request import urlopen

from app.config.env import load_env


def parse_args() -> argparse.Namespace:
    """解析启动参数。

    逻辑：
    1. 提供后端入口、前端命令、等待超时等可选项；
    2. 默认值不直接写死地址，而是交由 `.env` 解析函数决定；
    3. 支持 `--check-only` 仅做健康检查，不启动进程。

    关键分支/边界：
    - `frontend-cmd` 仅允许 pnpm/npm；
    - 若同时使用参数与 `.env`，参数优先。

    与外部交互：
    - 读取命令行参数。

    异常说明：
    - 参数错误由 argparse 自动处理并退出。

    副作用说明：
    - 无。
    """

    parser = argparse.ArgumentParser(description="前端启动前自动检查并拉起后端")
    parser.add_argument("--backend-url", default="", help="后端基础地址（为空时从 .env 读取）")
    parser.add_argument("--backend-entry", default="run_agent_api.py", help="后端启动脚本（相对仓库根目录）")
    parser.add_argument("--frontend-cmd", choices=["pnpm", "npm"], default="pnpm", help="前端包管理命令")
    parser.add_argument(
        "--backend-wait-seconds",
        type=float,
        default=30.0,
        help="拉起后端后的健康检查最长等待秒数（默认 30）",
    )
    parser.add_argument(
        "--health-timeout-seconds",
        type=float,
        default=1.5,
        help="单次健康检查超时秒数（默认 1.5）",
    )
    parser.add_argument("--check-only", action="store_true", help="只检查后端是否在线，不启动进程")
    return parser.parse_args()


def resolve_backend_base_url(cli_value: str) -> str:
    """解析后端基础 URL（参数优先，其次 `.env`）。

    逻辑：
    1. 若命令行显式传入则直接使用；
    2. 读取 `VITE_API_BASE_URL`（前端连接地址）；
    3. 若未配置，再读取 `AGENT_API_BASE`；
    4. 若仍为空，用 `API_PORT` 组装 `http://127.0.0.1:<port>`。

    关键分支/边界：
    - 地址统一去掉尾部 `/`；
    - 所有候选为空时回退 `http://127.0.0.1:8000`。

    与外部交互：
    - 读取进程环境变量。

    异常说明：
    - 不抛异常，返回可用默认值。

    副作用说明：
    - 无。
    """

    if cli_value.strip():
        return cli_value.strip().rstrip("/")
    else:
        pass

    from_vite = (os.getenv("VITE_API_BASE_URL", "") or "").strip()
    if from_vite:
        return from_vite.rstrip("/")
    else:
        pass

    from_agent_api_base = (os.getenv("AGENT_API_BASE", "") or "").strip()
    if from_agent_api_base:
        return from_agent_api_base.rstrip("/")
    else:
        pass

    raw_port = (os.getenv("API_PORT", "8000") or "8000").strip()
    try:
        port = int(raw_port)
        if not (1 <= port <= 65535):
            port = 8000
        else:
            pass
    except ValueError:
        port = 8000
    return f"http://127.0.0.1:{port}"


def resolve_frontend_api_base_for_child(backend_url: str) -> str:
    """解析传给前端进程的 `VITE_API_BASE_URL` 值。

    逻辑：
    1. 优先使用环境变量已配置值；
    2. 未配置时使用已解析的 backend_url；
    3. 输出统一去掉末尾 `/`。

    关键分支/边界：
    - 若 backend_url 为空，最终回退 `http://127.0.0.1:8000`。

    与外部交互：
    - 读取环境变量。

    异常说明：
    - 无。

    副作用说明：
    - 无。
    """

    explicit = (os.getenv("VITE_API_BASE_URL", "") or "").strip()
    if explicit:
        return explicit.rstrip("/")
    else:
        pass
    if backend_url:
        return backend_url.rstrip("/")
    else:
        return "http://127.0.0.1:8000"


def is_backend_ready(backend_url: str, timeout_seconds: float) -> bool:
    """检查后端 `/health` 是否可访问。

    逻辑：
    1. 构造 `<backend_url>/health`；
    2. 发起 GET 请求；
    3. 返回状态码是否在 2xx。

    关键分支/边界：
    - 网络不可达/拒绝连接/超时均判定为 False。

    与外部交互：
    - 本地或远程 HTTP 请求。

    异常说明：
    - 网络类异常内部吞掉并返回 False。

    副作用说明：
    - 无。
    """

    health_url = f"{backend_url.rstrip('/')}/health"
    try:
        with urlopen(health_url, timeout=timeout_seconds) as resp:  # noqa: S310
            return 200 <= int(getattr(resp, "status", 0)) < 300
    except (URLError, TimeoutError, OSError, ValueError):
        return False


def wait_backend_ready(backend_url: str, timeout_seconds: float, check_timeout_seconds: float) -> bool:
    """轮询等待后端健康检查通过。

    逻辑：
    1. 在总超时时间内循环调用健康检查；
    2. 任一轮通过即返回 True；
    3. 超时后返回 False。

    关键分支/边界：
    - 轮询间隔固定 0.5 秒；
    - 总超时到达即停止等待。

    与外部交互：
    - 多次 HTTP 健康检查。

    异常说明：
    - 内部不抛异常，统一返回布尔值。

    副作用说明：
    - 无。
    """

    deadline = time.time() + timeout_seconds
    while time.time() <= deadline:
        if is_backend_ready(backend_url=backend_url, timeout_seconds=check_timeout_seconds):
            return True
        else:
            time.sleep(0.5)
    return False


def should_try_local_backend_start(backend_url: str) -> bool:
    """判断是否应尝试在本机拉起后端。

    逻辑：
    1. 解析 backend_url 的 hostname；
    2. 若是 localhost/127.0.0.1/0.0.0.0 视为本机；
    3. 非本机地址默认不自动拉起，避免误起本地服务覆盖远端预期。

    关键分支/边界：
    - URL 无 hostname 时视为本机（保守策略）。

    与外部交互：
    - 无。

    异常说明：
    - 解析失败时回退本机策略。

    副作用说明：
    - 无。
    """

    try:
        hostname = (urlparse(backend_url).hostname or "").strip().lower()
    except Exception:
        hostname = ""

    if not hostname:
        return True
    else:
        return hostname in {"127.0.0.1", "localhost", "0.0.0.0"}


def start_backend(root: Path, backend_entry: str) -> subprocess.Popen[str]:
    """启动本地后端进程。

    逻辑：
    1. 使用当前 Python 解释器执行后端入口脚本；
    2. 继承当前环境变量（含 `.env` 结果）；
    3. 返回子进程句柄供后续监控与清理。

    关键分支/边界：
    - 不使用 shell，降低命令注入风险。

    与外部交互：
    - 创建系统子进程。

    异常说明：
    - 启动失败向上抛出，由上层处理。

    副作用说明：
    - 拉起后端长期运行进程。
    """

    return subprocess.Popen(  # noqa: S603
        [sys.executable, backend_entry],
        cwd=str(root),
        text=True,
        stdout=None,
        stderr=None,
    )


def start_frontend(root: Path, frontend_cmd: str, frontend_api_base: str) -> subprocess.Popen[str]:
    """启动前端 dev server，并注入统一 API 地址。

    逻辑：
    1. 复制当前环境变量；
    2. 写入 `VITE_API_BASE_URL`（若未配置则用解析后的 backend_url）；
    3. 启动 `pnpm/npm run --prefix frontend dev`。

    关键分支/边界：
    - 若 `VITE_API_BASE_URL` 已配置，优先保持原值。

    与外部交互：
    - 创建系统子进程。

    异常说明：
    - 启动失败向上抛出。

    副作用说明：
    - 拉起前端长期运行进程。
    """

    child_env = os.environ.copy()
    if not child_env.get("VITE_API_BASE_URL"):
        child_env["VITE_API_BASE_URL"] = frontend_api_base
    else:
        pass

    return subprocess.Popen(  # noqa: S603
        [frontend_cmd, "run", "--prefix", "frontend", "dev"],
        cwd=str(root),
        env=child_env,
        text=True,
        stdout=None,
        stderr=None,
    )


def terminate_process(proc: subprocess.Popen[str], name: str) -> None:
    """优雅终止子进程，必要时强制 kill。

    逻辑：
    1. 进程存活时先 terminate；
    2. 等待 3 秒；
    3. 超时后 kill。

    关键分支/边界：
    - 已退出进程直接跳过；
    - 异常仅记录，不影响其它进程清理。

    与外部交互：
    - 发送进程终止信号。

    异常说明：
    - 内部吞掉清理异常。

    副作用说明：
    - 终止目标进程。
    """

    if proc.poll() is not None:
        return
    else:
        pass

    proc.terminate()
    try:
        proc.wait(timeout=3)
    except subprocess.TimeoutExpired:
        proc.kill()
    except Exception as exc:  # pragma: no cover
        print(f"[startup] terminate {name} failed: {exc}")


def main() -> int:
    """程序入口：按“检查后端 -> 启动后端(可选) -> 启动前端”执行。

    逻辑：
    1. 加载仓库根 `.env`；
    2. 解析 backend_url 与 frontend API 基地址；
    3. 检查后端健康状态；
    4. 未就绪且为本机地址时，尝试启动后端并等待；
    5. 启动前端并持续监控子进程状态；
    6. 退出时仅清理“由本脚本启动”的进程。

    关键分支/边界：
    - `--check-only` 只做检查并返回状态码；
    - backend_url 指向远端时，默认不尝试本地拉起后端；
    - 后端等待超时则终止流程，不启动前端。

    与外部交互：
    - 读取 `.env`；
    - HTTP 健康检查；
    - 启动/停止系统子进程。

    异常说明：
    - 启动失败返回非 0，并打印错误摘要。

    副作用说明：
    - 可能拉起本地后端与前端长期运行进程。
    """

    root = Path(__file__).resolve().parent
    load_env(root)
    args = parse_args()

    backend_url = resolve_backend_base_url(args.backend_url)
    frontend_api_base = resolve_frontend_api_base_for_child(backend_url)

    backend_started_by_me = False
    backend_proc: subprocess.Popen[str] | None = None
    frontend_proc: subprocess.Popen[str] | None = None

    backend_ready = is_backend_ready(
        backend_url=backend_url,
        timeout_seconds=args.health_timeout_seconds,
    )
    if backend_ready:
        print(f"[startup] backend ready: {backend_url}")
    else:
        print(f"[startup] backend not ready: {backend_url}")

    if args.check_only:
        return 0 if backend_ready else 1
    else:
        pass

    try:
        if not backend_ready:
            if should_try_local_backend_start(backend_url):
                print(f"[startup] starting backend: {args.backend_entry}")
                backend_proc = start_backend(root=root, backend_entry=args.backend_entry)
                backend_started_by_me = True
                ok = wait_backend_ready(
                    backend_url=backend_url,
                    timeout_seconds=args.backend_wait_seconds,
                    check_timeout_seconds=args.health_timeout_seconds,
                )
                if not ok:
                    print("[startup] backend health check timeout, abort frontend startup")
                    return 1
                else:
                    print("[startup] backend is ready")
            else:
                print("[startup] backend URL is not local, skip auto-start and abort")
                return 1
        else:
            pass

        print(f"[startup] starting frontend with {args.frontend_cmd}")
        print(f"[startup] VITE_API_BASE_URL={frontend_api_base}")
        frontend_proc = start_frontend(
            root=root,
            frontend_cmd=args.frontend_cmd,
            frontend_api_base=frontend_api_base,
        )

        while True:
            if frontend_proc.poll() is not None:
                print(f"[startup] frontend exited: {frontend_proc.returncode}")
                return int(frontend_proc.returncode or 0)
            else:
                pass

            if backend_started_by_me and backend_proc is not None and backend_proc.poll() is not None:
                print(f"[startup] backend exited unexpectedly: {backend_proc.returncode}")
                return int(backend_proc.returncode or 1)
            else:
                pass

            time.sleep(0.8)
    except KeyboardInterrupt:
        print("[startup] interrupted by user")
        return 0
    except Exception as exc:
        print(f"[startup] failed: {exc}")
        return 1
    finally:
        if frontend_proc is not None:
            terminate_process(frontend_proc, "frontend")
        else:
            pass

        if backend_started_by_me and backend_proc is not None:
            terminate_process(backend_proc, "backend")
        else:
            pass


if __name__ == "__main__":
    raise SystemExit(main())

