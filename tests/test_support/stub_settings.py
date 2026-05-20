"""单测用 Settings 替身：避免真实 sqlite / 注册中心 / 指标路由依赖。

逻辑：
1. 用 `types.SimpleNamespace` 提供 `AgentService` / `create_app` / `lifespan` 读取的字段子集；
2. 调用方通过 `**overrides` 覆盖个别键以模拟分支。

关键边界：
- 未列字段若被业务新增读取，单测可能 `AttributeError`，届时在此补默认项即可。
"""

from __future__ import annotations

from types import SimpleNamespace


def settings_namespace(**overrides: object) -> SimpleNamespace:
    """构造进程内配置替身（默认关闭持久化与外部登记）。"""
    base: dict[str, object] = {
        "max_queue_size": 0,
        "agent_max_active_session_queues": 8,
        "agent_session_idle_evict_seconds": 0,
        "agent_session_store_enabled": False,
        "metrics_enabled": False,
        "registry_url": "",
        "discovery_groups": [],
        "agent_id": "test-agent",
        "agent_public_base_url": "",
        "agent_registry_ttl_seconds": 60,
        "agent_peer_shared_token": "",
        "agent_peer_http_retry_attempts": 2,
        "triggers_enabled": True,
        "trigger_scheduler_poll_seconds": 5,
        "api_cors_allow_origins": ["http://localhost:5173"],
    }
    base.update(overrides)
    return SimpleNamespace(**base)
