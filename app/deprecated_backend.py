"""Python FastAPI Agent 运行时弃用说明（集中常量与启动告警）。

逻辑：
1. 提供中英文弃用文案，供日志、OpenAPI、/health 复用；
2. `log_deprecation_warning` 在进程启动时打 WARNING，不阻断服务；
3. 新部署与本地助手应使用 Go Agent Node（``node/``）。

保留原因：DAgentsUI OpenAPI、A2A（``agent_peer``）、Register Center、v0.2.0 PyInstaller 发布。
"""

from __future__ import annotations

import logging
from typing import Any

# 供 /health、OpenAPI 等机器可读字段
DEPRECATED_BACKEND = True
DEPRECATED_BACKEND_REPLACEMENT = "go-agent-node"

DEPRECATED_BACKEND_NOTICE_ZH = (
    "【已弃用】Python FastAPI Agent 运行时（run_agent_api.py / app/harness）。"
    "新功能与本地单 Agent 助手请使用 Go Agent Node（node/ + packaging/agent-client/config.yaml）。"
    "本栈仍维护用于 DAgentsUI、OpenAPI 契约、A2A 与 Register Center，直至后续版本收敛。"
)

DEPRECATED_BACKEND_NOTICE_EN = (
    "DEPRECATED: Python FastAPI Agent backend. "
    "Use Go Agent Node (node/) for new deployments and local assistant. "
    "Retained for DAgentsUI, OpenAPI, A2A, and Register Center."
)

DEPRECATED_BACKEND_DOC = "docs/architecture/overview.md"


def log_deprecation_warning(logger: logging.Logger | None = None) -> None:
    """在 Python Agent 进程启动时输出弃用 WARNING（不抛异常、不中断启动）。"""
    target = logger or logging.getLogger("app.deprecated_backend")
    target.warning(DEPRECATED_BACKEND_NOTICE_ZH)


def health_deprecation_fields() -> dict[str, Any]:
    """返回可并入 GET /health 的弃用元数据字段。"""
    return {
        "deprecated": DEPRECATED_BACKEND,
        "replacement": DEPRECATED_BACKEND_REPLACEMENT,
        "deprecation_notice": DEPRECATED_BACKEND_NOTICE_ZH,
        "successor_doc": DEPRECATED_BACKEND_DOC,
    }
