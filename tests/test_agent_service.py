"""`AgentService` 单测：启动/停止、`submit_message` 入队消费、取消与 SSE 映射。"""

from __future__ import annotations

import asyncio
import unittest
from unittest.mock import AsyncMock, MagicMock, patch

from app.harness.queue.message_queue import MessageEnvelope
from tests.test_support.stub_settings import settings_namespace

try:
    from app.harness.service.agent_service import AgentService
    from app.harness.service.interface import AgentEventEnvelope
except ImportError as exc:  # pragma: no cover - 仅精简环境触发
    AgentService = None  # type: ignore[misc, assignment]
    AgentEventEnvelope = None  # type: ignore[misc, assignment]
    _AGENT_SERVICE_SKIP = f"AgentService 依赖链未就绪（{exc!r}）；请执行 pip install -r requirements.txt"
else:
    _AGENT_SERVICE_SKIP = ""


@unittest.skipIf(AgentService is None, _AGENT_SERVICE_SKIP)
class AgentServiceStreamMapTests(unittest.TestCase):
    """`_map_event_envelope_to_stream`：扁平 SSE 字段与 `meta` 合并。"""

    def test_assistant_and_tool_result_shapes(self) -> None:
        """assistant / tool_result 含 `meta` 合并与常用默认值。"""
        base = {"session_id": "sid", "model": "gpt-test"}
        t1, d1 = AgentService._map_event_envelope_to_stream(
            AgentEventEnvelope(event_type="assistant", payload={"content": "hi", "display_type": "markdown"}, meta={}),
            base_meta=base,
        )
        self.assertEqual(t1, "assistant")
        self.assertEqual(d1.get("content"), "hi")
        self.assertEqual(d1["meta"]["session_id"], "sid")

        t2, d2 = AgentService._map_event_envelope_to_stream(
            AgentEventEnvelope(
                event_type="tool_result",
                payload={"content": "{}", "tool_call_id": "c1", "tool_name": "bash_run"},
                meta={"trace": "1"},
            ),
            base_meta=base,
        )
        self.assertEqual(t2, "tool_result")
        self.assertEqual(d2["tool_call_id"], "c1")
        self.assertEqual(d2["meta"]["trace"], "1")

    def test_approval_required_and_done(self) -> None:
        """审批事件扁平化 `args` → `approval_args`；`done` 允许空 payload。"""
        base = {"session_id": "sid", "model": "m"}
        t1, d1 = AgentService._map_event_envelope_to_stream(
            AgentEventEnvelope(
                event_type="approval_required",
                payload={
                    "approval_type": "execute_tool",
                    "message": "ok?",
                    "args": {"tool_calls": []},
                    "approval_id": "aid",
                },
                meta={},
            ),
            base_meta=base,
        )
        self.assertEqual(t1, "approval_required")
        self.assertEqual(d1["approval_id"], "aid")
        self.assertIn("approval_args", d1)

        t2, d2 = AgentService._map_event_envelope_to_stream(
            AgentEventEnvelope(event_type="done", payload={}, meta={}),
            base_meta=base,
        )
        self.assertEqual(t2, "done")
        self.assertIn("meta", d2)


@unittest.skipIf(AgentService is None, _AGENT_SERVICE_SKIP)
class AgentServiceLifecycleTests(unittest.IsolatedAsyncioTestCase):
    """`start` / `stop` 与 orchestrator 替身，避免拉起真实 LLM runtime。"""

    async def asyncSetUp(self) -> None:
        """为每条用例准备可 await 的编排替身与 `get_settings` 注入。"""
        self._orch = MagicMock()
        self._orch.handle_message = AsyncMock()
        self._orch.cancel_all_summary_tasks = AsyncMock()
        self._orch.cancel_session_summary_task = AsyncMock()
        self._async_store_mock = MagicMock()

    async def _make_service(
        self,
        *,
        handle_stream: object | None = None,
    ) -> AgentService:
        """构造 `AgentService`：统一 patch settings / model / orchestrator。"""

        def _factory(*_a: object, **_k: object) -> MagicMock:
            return self._orch

        stack = [
            patch("app.harness.service.agent_service.get_settings", return_value=settings_namespace()),
            patch("app.harness.service.agent_service.get_model_config", return_value={"model": "unit"}),
            patch("app.harness.service.agent_service.MainAgentTurnOrchestrator", side_effect=_factory),
            patch("app.harness.service.agent_service.refresh_session_context_metrics"),
            # `OpenAIImplicitReActRuntime` 在 `agent_service._get_runtime` 中懒加载；其 `__init__` 会调用
            # `runtime_openai` 模块内的 `get_openai_client`（与 `agent_service.get_settings` 的 patch 无关）。
            # 新版 OpenAI SDK 在 api_key 为空时于构造期抛错，CI 无密钥时必须替身，避免消费者 task 直接崩掉。
            patch("app.core.main_agent.runtime_openai.get_openai_client", return_value=MagicMock()),
            patch(
                "app.harness.service.agent_service.get_async_tool_result_store",
                return_value=self._async_store_mock,
            ),
        ]
        for p in stack:
            p.start()
            self.addCleanup(p.stop)
        assert AgentService is not None
        return AgentService(max_queue_size=0, handle_stream_event=handle_stream, message_store=None)

    async def test_start_stop_calls_async_store_register(self) -> None:
        """`start` 注册队列发件人；`stop` 注销为 None，避免悬挂回调。"""
        svc = await self._make_service()
        await svc.start()
        self.assertEqual(self._async_store_mock.register_message_queue_sender.call_count, 1)
        await svc.stop()
        self.assertGreaterEqual(self._async_store_mock.register_message_queue_sender.call_count, 2)
        self._async_store_mock.register_message_queue_sender.assert_called_with(None)

    async def test_submit_message_invokes_orchestrator_handle_message(self) -> None:
        """投递 `human` 消息后，消费者应串行调用 `handle_message`（不自动 cancel）。"""
        svc = await self._make_service()
        await svc.start()
        await svc.submit_message(session_id="s1", content="hello", source="test", priority="human", client_id="c1")
        await asyncio.sleep(0.05)
        self._orch.handle_message.assert_awaited()
        _args, kwargs = self._orch.handle_message.call_args
        self.assertIsInstance(kwargs.get("env"), MessageEnvelope)
        self.assertEqual(kwargs["env"].session_id, "s1")
        self.assertEqual(kwargs["env"].content, "hello")
        await svc.stop()

    async def test_cancel_current_turn_cancels_inflight_handle_message(self) -> None:
        """`cancel_current_turn` 应取消尚未完成的 `_handle_message` 子 task。"""
        gate = asyncio.Event()

        async def slow_handle(*_a: object, **_k: object) -> None:
            await gate.wait()

        self._orch.handle_message = slow_handle
        svc = await self._make_service()
        await svc.start()
        await svc.submit_message(session_id="s2", content="block", source="test", priority="human")
        await asyncio.sleep(0.05)
        ok = svc.cancel_current_turn("s2")
        self.assertTrue(ok)
        gate.set()
        await asyncio.sleep(0.05)
        await svc.stop()

    async def test_handle_stream_event_receives_error_mapping_on_exception(self) -> None:
        """编排抛错时服务层映射 `error` + `done` 并转发给 `handle_stream_event`。"""
        self._orch.handle_message = AsyncMock(side_effect=RuntimeError("unit-boom"))
        received: list[tuple[str, dict]] = []

        async def cap(env: MessageEnvelope, event_type: str, data: dict) -> None:
            received.append((event_type, data))

        svc = await self._make_service(handle_stream=cap)
        await svc.start()
        await svc.submit_message(session_id="s3", content="x", source="test", priority="human", client_id="c9")
        await asyncio.sleep(0.08)
        types = [t for t, _ in received]
        self.assertIn("error", types)
        self.assertIn("done", types)
        await svc.stop()


if __name__ == "__main__":
    unittest.main()
