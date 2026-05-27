from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path
from typing import Sequence

from app.config.env import load_env
from app.cli.chat import run_chat
from app.cli.daemon import add_serve_arguments, run_serve_command
from app.cli.session_commands import run_delete_session, run_show_session


def _default_api_base() -> str:
    """从 .env 读取 API_HOST / API_PORT 构造默认地址，兜底 127.0.0.1:8000。"""
    home = _runtime_home()
    env_path = home / ".env"
    if env_path.is_file():
        load_env(home)
    host = os.getenv("API_HOST", "127.0.0.1").strip() or "127.0.0.1"
    port = os.getenv("API_PORT", "8000").strip() or "8000"
    return f"http://{host}:{port}"


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if args.command == "chat":
        return run_chat(args)
    if args.command == "show":
        if args.show_command == "session":
            return run_show_session(args)
        parser.parse_args(["show", "--help"])
        return 1
    if args.command == "delete":
        if args.delete_command == "session":
            return run_delete_session(args)
        parser.parse_args(["delete", "--help"])
        return 1
    if args.command in {"serve", "api"}:
        return run_serve_command(
            _runtime_home(),
            binary_stem="dagents-api",
            script_name="run_agent_api.py",
            extra_args=_normalize_serve_extra(args.extra),
            foreground=bool(args.foreground),
            stop=bool(args.stop),
            status=bool(args.status),
            no_hooks=bool(args.no_hooks),
            no_wait=bool(args.no_wait),
        )
    if args.command == "register-center":
        return _run_installed_or_python("dagents_register_center", "run_register_center.py", args.extra)
    if args.command == "doctor":
        return _doctor()
    if args.command == "version":
        print("DAgents")
        return 0
    parser.print_help()
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="dagents", description="DAgents command line interface")
    subparsers = parser.add_subparsers(dest="command")

    chat = subparsers.add_parser("chat", help="Start an interactive Textual TUI chat")
    chat.add_argument("--api", default=os.getenv("DAGENTS_API_BASE") or _default_api_base(), help="DAgents API base URL")
    chat.add_argument("--session", default=None, help="Session ID to create or reuse")
    chat.add_argument("--client-id", default=None, help="SSE client ID")
    chat.add_argument("--show-reasoning", action="store_true", help="Print reasoning stream events")

    show = subparsers.add_parser("show", help="Show runtime resources")
    show_sub = show.add_subparsers(dest="show_command")
    show_session = show_sub.add_parser("session", help="List active queue sessions and persisted sqlite sessions")
    show_session.add_argument(
        "--api",
        default=os.getenv("DAGENTS_API_BASE") or _default_api_base(),
        help="DAgents API base URL",
    )

    delete = subparsers.add_parser("delete", help="Delete runtime resources")
    delete_sub = delete.add_subparsers(dest="delete_command")
    delete_session = delete_sub.add_parser(
        "session",
        help="Delete a persisted sqlite session (only when not in queue)",
    )
    delete_session.add_argument("session_id", help="Session ID to delete from sqlite")
    delete_session.add_argument(
        "--api",
        default=os.getenv("DAGENTS_API_BASE") or _default_api_base(),
        help="DAgents API base URL",
    )

    serve = subparsers.add_parser(
        "serve",
        help="Start the Agent API backend in background (use --foreground for blocking)",
    )
    add_serve_arguments(serve)

    api = subparsers.add_parser("api", help="Alias for serve")
    add_serve_arguments(api)

    register_center = subparsers.add_parser("register-center", help="Start the Register Center")
    register_center.add_argument("extra", nargs=argparse.REMAINDER)

    subparsers.add_parser("doctor", help="Check installed files")
    subparsers.add_parser("version", help="Print version information")
    return parser


def _normalize_serve_extra(extra: list[str] | None) -> list[str]:
    """剥离 REMAINDER 可能带的 `--` 前缀，保留传给 API 进程的参数。"""
    rows = list(extra or [])
    if rows and rows[0] == "--":
        return rows[1:]
    return rows


def _run_installed_or_python(binary_stem: str, script_name: str, extra_args: list[str]) -> int:
    home = _runtime_home()
    exe_name = f"{binary_stem}.exe" if os.name == "nt" else binary_stem
    binary = home / exe_name
    if binary.exists():
        return subprocess.call([str(binary), *extra_args], cwd=str(home))
    script = _repo_root() / script_name
    if script.exists():
        return subprocess.call([sys.executable, str(script), *extra_args], cwd=str(_repo_root()))
    print(f"[dagents] missing {exe_name} and {script_name}", file=sys.stderr)
    return 1


def _doctor() -> int:
    home = _runtime_home()
    print(f"DAgents installation: {home}")
    ok = True
    for name in _expected_binary_names():
        path = home / name
        found = path.exists()
        print(("[ok] " if found else "[missing] ") + name)
        ok = ok and found
    env_path = home / ".env"
    print("[ok] .env found" if env_path.exists() else "[info] .env not found; defaults and .env.example will be used")
    runtime_dir = home / ".runtime"
    print("[ok] .runtime found" if runtime_dir.exists() else "[missing] .runtime")
    return 0 if ok else 1


def _expected_binary_names() -> list[str]:
    if os.name == "nt":
        return ["dagents-api.exe", "dagents_register_center.exe"]
    return ["dagents-api", "dagents_register_center"]


def _runtime_home() -> Path:
    raw = os.getenv("DAGENTS_HOME")
    if raw:
        return Path(raw).expanduser().resolve()
    if getattr(sys, "frozen", False):
        return Path(sys.executable).resolve().parent
    return _repo_root()


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


if __name__ == "__main__":
    raise SystemExit(main())
