"""
本地一键启动 Register Center（开发联调）。

用法（在 DAgents 根目录）:
  python run_dev_stack.py
"""

from __future__ import annotations

import argparse
import shlex
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path

from app.config.env import load_env


@dataclass
class ManagedProcess:
    """受管子进程元信息。"""

    name: str
    command: list[str]
    popen: subprocess.Popen[str]


def parse_args() -> argparse.Namespace:
    """解析命令行；当前仅启动 Register Center。"""
    return argparse.ArgumentParser(description="DAgents Register Center 开发启动器").parse_args()


def start_processes(root: Path) -> list[ManagedProcess]:
    """启动 Register Center 子进程。"""
    cmd = [sys.executable, "run_register_center.py"]
    print(f"[dev-stack] starting register_center: {shlex.join(cmd)}")
    popen = subprocess.Popen(cmd, cwd=str(root), text=True, stdout=None, stderr=None)
    print(f"[dev-stack] register_center pid={popen.pid}")
    return [ManagedProcess(name="register_center", command=cmd, popen=popen)]


def stop_processes(processes: list[ManagedProcess]) -> None:
    """按 terminate → kill 顺序关闭子进程。"""
    for proc in processes:
        if proc.popen.poll() is None:
            proc.popen.terminate()
    for proc in processes:
        if proc.popen.poll() is None:
            try:
                proc.popen.wait(timeout=3)
            except subprocess.TimeoutExpired:
                proc.popen.kill()
            except Exception:
                pass


def watch_loop(processes: list[ManagedProcess]) -> int:
    """阻塞监控直至子进程退出或 Ctrl+C。"""
    try:
        while True:
            for proc in processes:
                code = proc.popen.poll()
                if code is None:
                    continue
                print(f"[dev-stack] {proc.name} exited with code {code}")
                return code
            time.sleep(0.8)
    except KeyboardInterrupt:
        print("[dev-stack] interrupted by user")
        return 0


def main() -> int:
    """加载 .env、启动 Register Center 并监控。"""
    root = Path(__file__).resolve().parent
    load_env(root)
    processes: list[ManagedProcess] = []
    try:
        processes = start_processes(root)
        print("[dev-stack] Register Center started. Press Ctrl+C to stop.")
        return watch_loop(processes)
    except Exception as exc:
        print(f"[dev-stack] failed: {exc}")
        return 1
    finally:
        stop_processes(processes)


if __name__ == "__main__":
    raise SystemExit(main())
