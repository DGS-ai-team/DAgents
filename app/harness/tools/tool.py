"""Agent 可调用的工具定义与注册。"""

from __future__ import annotations

from typing import Any

from app.harness.tools.agent_peer import (
    agent_broadcast,
    agent_discover,
    agent_send_message,
)
from app.harness.tools.bash import bash_run

def get_tools() -> list[Any]:
    """返回供 OpenAI runtime 注册的工具列表。"""
    # 先最小集启用，后续可按稳定性逐步放开更多工具。
    return [
        bash_run,
        agent_discover,
        agent_send_message,
        agent_broadcast,
    ]