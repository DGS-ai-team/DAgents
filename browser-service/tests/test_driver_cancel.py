from __future__ import annotations

import asyncio
import tempfile

import dagents_browser.driver as driver_module
from dagents_browser.config import BrowserServiceSettings
from dagents_browser.driver import BrowserUseDriver


def test_cancel_before_agent_start_is_stable() -> None:
    async def scenario() -> None:
        settings = BrowserServiceSettings(
            runtime_root=tempfile.mkdtemp(prefix="dagents-browser-cancel-"),
        )
        driver = BrowserUseDriver(settings)
        # Avoid starting a real Chrome process; the task only needs a session
        # entry to reach the cancellation race under test.
        driver._sessions["cancel-race"] = object()  # noqa: SLF001

        original_factory = driver_module.create_extraction_llm
        driver_module.create_extraction_llm = lambda _settings: object()
        try:
            queued = await driver.call(
                {
                    "op": "run_task",
                    "session_key": "cancel-race",
                    "task": "do not start",
                    "llm": {"provider": "mimo", "model": "test-model"},
                }
            )
            task_id = queued["detail"]["task_id"]
            cancelled = await driver.call(
                {
                    "op": "task_cancel",
                    "session_key": "cancel-race",
                    "task_id": task_id,
                }
            )
            detail = cancelled["detail"]
            assert detail["status"] == "cancelled"
            assert detail["error"] == "cancelled"
        finally:
            driver_module.create_extraction_llm = original_factory
            driver._sessions.clear()  # noqa: SLF001
            await driver.close()

    asyncio.run(scenario())
