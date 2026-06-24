from __future__ import annotations

import argparse
import asyncio

from app.cli.last_session import load_last_session
from app.cli.session_controller import SessionController
from app.cli.tui.app import DAgentsTuiApp


def run_chat(args: argparse.Namespace) -> int:
    """启动 Textual TUI 聊天客户端。"""
    config_path = getattr(args, "config_path", None)
    session_id = str(args.session or "").strip() or None
    if session_id is None:
        session_id = load_last_session(args.api, config_path=config_path)
    controller = SessionController(
        api_base=args.api,
        session_id=session_id,
        show_reasoning=args.show_reasoning,
        config_path=config_path,
    )

    async def _main() -> None:
        app = DAgentsTuiApp(controller=controller)
        await app.run_async()

    try:
        asyncio.run(_main())
    except KeyboardInterrupt:
        return 0
    return 0
