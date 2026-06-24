from __future__ import annotations

import argparse
import os
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
    if args.command == "doctor":
        return _doctor()
    if args.command == "version":
        from app.cli.version_info import get_cli_version

        print(f"DAgents {get_cli_version()}")
        return 0
    parser.print_help()
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="dagents", description="DAgents command line interface")
    subparsers = parser.add_subparsers(dest="command")

    chat = subparsers.add_parser("chat", help="Start an interactive Textual TUI chat")
    _add_client_config_arguments(chat)
    chat.add_argument(
        "--session",
        default=None,
        help="Session ID to create or reuse（省略时默认进入上次退出时的 session）",
    )
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

    subparsers.add_parser("doctor", help="Check installed files")
    subparsers.add_parser("version", help="Print version information")
    return parser


def _doctor() -> int:
    home = _runtime_home()
    print(f"DAgents CLI home: {home}")
    manage_script = _repo_root() / "run_manage.py"
    if manage_script.exists():
        print("[ok] run_manage.py (Manage 控制面，源码开发)")
    config = home / "config.yaml"
    if config.exists():
        print("[ok] config.yaml")
    else:
        print("[info] config.yaml not found")
    runtime_dir = home / ".runtime"
    print("[ok] .runtime found" if runtime_dir.exists() else "[info] .runtime not found")
    return 0


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
