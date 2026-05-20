from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path
from typing import Sequence

from app.cli.chat import run_chat

DEFAULT_API_BASE = "http://127.0.0.1:8000"


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if args.command == "chat":
        return run_chat(args)
    if args.command in {"serve", "api"}:
        return _run_installed_or_python("dagents-api", "run_agent_api.py", args.extra)
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

    chat = subparsers.add_parser("chat", help="Start an interactive terminal chat")
    chat.add_argument("--api", default=os.getenv("DAGENTS_API_BASE", DEFAULT_API_BASE), help="DAgents API base URL")
    chat.add_argument("--session", default=None, help="Session ID to create or reuse")
    chat.add_argument("--client-id", default=None, help="SSE client ID")
    chat.add_argument("--show-reasoning", action="store_true", help="Print reasoning stream events")

    serve = subparsers.add_parser("serve", help="Start the Agent API backend")
    serve.add_argument("extra", nargs=argparse.REMAINDER)

    api = subparsers.add_parser("api", help="Alias for serve")
    api.add_argument("extra", nargs=argparse.REMAINDER)

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
