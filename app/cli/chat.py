from __future__ import annotations

import argparse
import asyncio

from app.cli.session_controller import SessionController
from app.cli.tui.app import DAgentsTuiApp


def run_chat(args: argparse.Namespace) -> int:
    """启动 Textual TUI 聊天客户端。"""
    controller = SessionController(
        api_base=args.api,
        session_id=args.session,
        client_id=args.client_id,
        show_reasoning=args.show_reasoning,
    )

    async def _main() -> None:
        app = DAgentsTuiApp(controller=controller)
        await app.run_async()

    try:
        asyncio.run(_main())
    except KeyboardInterrupt:
        return 0
    return 0
