"""
本地一键启动开发栈（API + Register Center + Frontend）。

用法（在 DAgents 根目录）:
  python run_dev_stack.py
  python run_dev_stack.py --no-register
  python run_dev_stack.py --frontend-cmd npm
"""

from __future__ import annotations

import argparse
import os
import shlex
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path

from app.config.env import load_env


@dataclass
class ManagedProcess:
    """封装受管子进程的元信息。

    逻辑：
    1. 保存进程名称、命令与 `Popen` 句柄；
    2. 供启动日志、健康检查、终止流程复用。

    关键分支/边界：
    - 名称仅用于日志显示，不参与业务判断；
    - 句柄为空的对象不应进入 stop 流程（由调用方保证）。

    与外部交互：
    - 无直接外部交互，仅保存外部进程句柄。

    异常说明：
    - 本结构体本身不抛业务异常。

    副作用说明：
    - 无。
    """

    name: str
    command: list[str]
    popen: subprocess.Popen[str]


def parse_args() -> argparse.Namespace:
    """解析命令行参数。

    逻辑：
    1. 定义需要启动的进程开关（api/register/frontend）；
    2. 定义前端包管理命令（pnpm/npm）；
    3. 返回解析后的命名空间供主流程使用。

    关键分支/边界：
    - 默认启动全部组件；
    - `--frontend-cmd` 仅允许 pnpm 或 npm，避免拼写导致运行失败。

    与外部交互：
    - 读取命令行参数。

    异常说明：
    - 参数非法时由 argparse 自动报错并退出。

    副作用说明：
    - 无。
    """

    parser = argparse.ArgumentParser(description="DAgents 本地开发栈启动器")
    parser.add_argument("--no-api", action="store_true", help="不启动 API 服务")
    parser.add_argument(
        "--no-register",
        action="store_true",
        help="不启动 Register Center",
    )
    parser.add_argument(
        "--no-frontend",
        action="store_true",
        help="不启动 Frontend dev server",
    )
    parser.add_argument(
        "--frontend-cmd",
        choices=["pnpm", "npm"],
        default="pnpm",
        help="前端包管理命令（默认 pnpm）",
    )
    return parser.parse_args()


def _resolve_vite_api_base_from_env() -> str:
    """解析前端注入用 `VITE_API_BASE_URL`。

    逻辑：
    1. 优先读取 `VITE_API_BASE_URL`；
    2. 其次读取 `AGENT_API_BASE`；
    3. 最后按 `API_PORT` 组装本地地址。

    关键分支/边界：
    - 地址统一去掉末尾 `/`；
    - 端口非法时回退 8000。

    与外部交互：
    - 读取环境变量（已由 `.env` 注入）。

    异常说明：
    - 无显式异常抛出。

    副作用说明：
    - 无。
    """

    explicit = (os.getenv("VITE_API_BASE_URL", "") or "").strip()
    if explicit:
        return explicit.rstrip("/")
    else:
        pass

    agent_api_base = (os.getenv("AGENT_API_BASE", "") or "").strip()
    if agent_api_base:
        return agent_api_base.rstrip("/")
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


def build_commands(
    root: Path,
    args: argparse.Namespace,
) -> list[tuple[str, list[str], Path, dict[str, str] | None]]:
    """按参数构造待启动命令列表。

    逻辑：
    1. 根据 `--no-*` 开关决定是否加入对应进程；
    2. 为每个进程指定命令与工作目录；
    3. 以稳定顺序返回命令列表（register -> api -> frontend）。

    关键分支/边界：
    - 若用户关闭全部进程，返回空列表，由主流程统一处理；
    - Frontend 命令使用 `--prefix frontend`，无需切换目录。

    与外部交互：
    - 无直接外部交互，仅生成命令数据。

    异常说明：
    - 无。

    副作用说明：
    - 无。
    """

    commands: list[tuple[str, list[str], Path, dict[str, str] | None]] = []
    if not args.no_register:
        commands.append(
            ("register-center", [sys.executable, "run_register_center.py"], root, None),
        )
    else:
        pass

    if not args.no_api:
        commands.append(("api", [sys.executable, "run_agent_api.py"], root, None))
    else:
        pass

    if not args.no_frontend:
        vite_api_base = _resolve_vite_api_base_from_env()
        commands.append(
            (
                "frontend",
                [args.frontend_cmd, "run", "--prefix", "frontend", "dev"],
                root,
                {"VITE_API_BASE_URL": vite_api_base},
            ),
        )
    else:
        pass
    return commands


def start_processes(
    commands: list[tuple[str, list[str], Path, dict[str, str] | None]],
) -> list[ManagedProcess]:
    """启动所有子进程并返回句柄列表。

    逻辑：
    1. 逐个执行 `subprocess.Popen`；
    2. 打印启动命令与 pid；
    3. 任一启动失败时抛异常，由上层统一清理。

    关键分支/边界：
    - 采用文本模式转发 stdout/stderr，便于同终端调试；
    - 不设置 `shell=True`，降低命令注入风险。

    与外部交互：
    - 创建系统级子进程。

    异常说明：
    - `Popen` 失败时直接向上抛出 `OSError` 等异常。

    副作用说明：
    - 启动多个长期运行进程并占用端口。
    """

    managed: list[ManagedProcess] = []
    for name, cmd, cwd, env_overrides in commands:
        print(f"[dev-stack] starting {name}: {shlex.join(cmd)}")
        child_env = os.environ.copy()
        if env_overrides:
            child_env.update(env_overrides)
        else:
            pass
        popen = subprocess.Popen(
            cmd,
            cwd=str(cwd),
            env=child_env,
            text=True,
            stdout=None,
            stderr=None,
        )
        managed.append(ManagedProcess(name=name, command=cmd, popen=popen))
        print(f"[dev-stack] {name} pid={popen.pid}")
    return managed


def stop_processes(processes: list[ManagedProcess]) -> None:
    """按“先温和后强制”策略关闭所有子进程。

    逻辑：
    1. 先对仍在运行的进程发送 terminate；
    2. 等待短超时；
    3. 仍未退出的进程执行 kill。

    关键分支/边界：
    - 已退出进程跳过 terminate/kill；
    - kill 仅用于兜底，避免僵尸进程遗留。

    与外部交互：
    - 向系统子进程发送终止信号。

    异常说明：
    - `wait/kill` 失败时不抛出，避免中断其余清理流程。

    副作用说明：
    - 终止所有受管进程。
    """

    for proc in processes:
        if proc.popen.poll() is None:
            proc.popen.terminate()
        else:
            pass

    for proc in processes:
        if proc.popen.poll() is None:
            try:
                proc.popen.wait(timeout=3)
            except subprocess.TimeoutExpired:
                proc.popen.kill()
            except Exception:
                pass
        else:
            pass


def watch_loop(processes: list[ManagedProcess]) -> int:
    """阻塞监控子进程，直到收到中断或任一进程异常退出。

    逻辑：
    1. 轮询检查是否有子进程退出；
    2. 若退出码非 0，打印告警并触发整体退出；
    3. 正常中断（Ctrl+C）时返回 0。

    关键分支/边界：
    - 任一进程异常退出时，开发栈整体退出并返回该退出码；
    - 进程正常退出（0）也触发整体退出，避免半残状态。

    与外部交互：
    - 读取系统进程状态。

    异常说明：
    - KeyboardInterrupt 被捕获并转为正常退出码 0。

    副作用说明：
    - 无。
    """

    try:
        while True:
            for proc in processes:
                code = proc.popen.poll()
                if code is None:
                    continue
                else:
                    print(f"[dev-stack] {proc.name} exited with code {code}")
                    return code
            time.sleep(0.8)
    except KeyboardInterrupt:
        print("[dev-stack] interrupted by user")
        return 0


def main() -> int:
    """程序入口：构建命令、启动进程、监控并清理。

    逻辑：
    1. 解析参数并构造命令；
    2. 启动子进程并输出提示；
    3. 进入监控循环；
    4. 退出前统一清理全部子进程。

    关键分支/边界：
    - 未选择任何进程时直接返回非 0；
    - 启动阶段异常时也会触发清理逻辑。

    与外部交互：
    - 创建并管理多个长期运行进程（API / Register / Frontend）。

    异常说明：
    - 不吞关键异常，统一转换为退出码并打印错误。

    副作用说明：
    - 管理多进程生命周期并输出运行日志。
    """

    root = Path(__file__).resolve().parent
    load_env(root)
    args = parse_args()
    commands = build_commands(root=root, args=args)
    if not commands:
        print("[dev-stack] no process selected, exit")
        return 2
    else:
        pass

    processes: list[ManagedProcess] = []
    try:
        processes = start_processes(commands)
        print("[dev-stack] all processes started. Press Ctrl+C to stop.")
        return watch_loop(processes)
    except Exception as exc:
        print(f"[dev-stack] failed: {exc}")
        return 1
    finally:
        stop_processes(processes)


if __name__ == "__main__":
    raise SystemExit(main())

