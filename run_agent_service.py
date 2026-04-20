"""
独立服务入口（常驻）：

用法（在 DAgents 根目录）:
  python run_agent_service.py
"""

from __future__ import annotations

import asyncio
import signal
import sys
from pathlib import Path

_ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(_ROOT))

from app.config.env import load_env  # noqa: E402
from app.config.settings import get_settings  # noqa: E402
from app.harness.service.agent_service import AgentService  # noqa: E402


async def _main() -> None:
    load_env(_ROOT)
    s = get_settings(reload=True)

    service = AgentService(
        max_queue_size=s.max_queue_size,
    )

    loop = asyncio.get_running_loop()

    def _shutdown() -> None:
        asyncio.create_task(service.stop())

    try:
        loop.add_signal_handler(signal.SIGINT, _shutdown)
        loop.add_signal_handler(signal.SIGTERM, _shutdown)
    except NotImplementedError:
        # Windows 某些事件循环不支持 add_signal_handler
        pass

    print(
        "[agent-service] running. waiting for queue messages..."
        f" max_queue_size={s.max_queue_size}",
        flush=True,
    )
    await service.run_forever()


if __name__ == "__main__":
    asyncio.run(_main())

