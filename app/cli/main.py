from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path
from typing import Sequence

from app.cli.config_file import resolve_client_settings
from app.cli.chat import run_chat
from app.cli.session_commands import run_delete_session, run_show_session


def _add_client_config_arguments(parser: argparse.ArgumentParser) -> None:
    """为 chat / show / delete 子命令注册共用 YAML 配置参数。"""
    parser.add_argument(
        "--config",
        default=None,
        help="共用 YAML 配置路径（默认：DAGENTS_CONFIG 或 packaging/agent-client/config.yaml）",
    )
    parser.add_argument(
        "--api",
        default=None,
        help="覆盖 config 中的 local.endpoint；无 YAML 时回退 DAGENTS_NODE_ENDPOINT",
    )


def apply_client_settings(args: argparse.Namespace) -> argparse.Namespace:
    """将 YAML 与 CLI 覆盖项合并到 args.api / args.config_path。"""
    api, cfg_path = resolve_client_settings(
        config_path=getattr(args, "config", None),
        api_override=getattr(args, "api", None),
        env_api_fallback=_default_api_base(),
    )
    args.api = api
    args.config_path = cfg_path
    return args


def _default_api_base() -> str:
    """无 YAML 时的 API 回退：DAGENTS_NODE_ENDPOINT / DAGENTS_API_BASE，默认 127.0.0.1:18765。"""
    for key in ("DAGENTS_NODE_ENDPOINT", "DAGENTS_API_BASE"):
        raw = os.getenv(key, "").strip()
        if raw:
            return raw.rstrip("/")
    return "http://127.0.0.1:18765"


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if args.command == "chat":
        return run_chat(apply_client_settings(args))
    if args.command == "show":
        if args.show_command == "session":
            return run_show_session(apply_client_settings(args))
        parser.parse_args(["show", "--help"])
        return 1
    if args.command == "delete":
        if args.delete_command == "session":
            return run_delete_session(apply_client_settings(args))
        parser.parse_args(["delete", "--help"])
        return 1
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
    _add_client_config_arguments(chat)
    chat.add_argument("--session", default=None, help="Session ID to create or reuse")
    chat.add_argument("--show-reasoning", action="store_true", help="Print reasoning stream events")

    show = subparsers.add_parser("show", help="Show runtime resources")
    show_sub = show.add_subparsers(dest="show_command")
    show_session = show_sub.add_parser("session", help="List Agent Node sessions (active + persisted)")
    _add_client_config_arguments(show_session)

    delete = subparsers.add_parser("delete", help="Delete runtime resources")
    delete_sub = delete.add_subparsers(dest="delete_command")
    delete_session = delete_sub.add_parser(
        "session",
        help="Release a session (DELETE /v1/sessions/{id})",
    )
    delete_session.add_argument("session_id", help="Session ID to release")
    _add_client_config_arguments(delete_session)

    register_center = subparsers.add_parser("register-center", help="Start the Register Center")
    register_center.add_argument("extra", nargs=argparse.REMAINDER)

    subparsers.add_parser("doctor", help="Check installed files")
    subparsers.add_parser("version", help="Print version information")
    return parser


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
        return ["dagents_register_center.exe"]
    return ["dagents_register_center"]


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
