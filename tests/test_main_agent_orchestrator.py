"""`MainAgentTurnOrchestrator` 单测：覆盖主消息分支与工具闭环。"""

from __future__ import annotations

import unittest
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from app.context.models import OpenAIConversationContext, PendingToolCall, RunTurnPhase
from app.harness.queue.message_queue import MessageEnvelope
from app.harness.service.interface import AgentEventEnvelope
from tests.test_support.stub_settings import settings_namespace

try:
    from app.core.main_agent.agent import MainAgentTurnOrchestrator
except ImportError as exc:  # pragma: no cover - 仅精简环境触发
    MainAgentTurnOrchestrator = None  # type: ignore[assignment]
    _ORCHESTRATOR_SKIP = f"MainAgentTurnOrchestrator 依赖链未就绪（{exc!r}）；请执行 pip install -r requirements.txt"
else:
    _ORCHESTRATOR_SKIP = ""


class FakeRuntime:
    """可脚本化的 runtime 替身。

    逻辑：
    1. 按构造时传入的事件列表逐条 yield；
    2. 若事件中包含 `tool_call`，同步写入 `ctx.pending_tool_calls`，模拟真实 runtime 的副作用；
    3. `human_message` 时追加 user 消息，便于测试 pending 打断后仍能继续进入新一轮推理。
    """

    def __init__(self, events: list[AgentEventEnvelope]) -> None:
        """保存待产出的事件序列。"""
        self._events = list(events)
        self.calls: list[tuple[str, str]] = []

    async def run_turn(
        self,
        ctx: OpenAIConversationContext,
        *,
        request_type: str,
        content: str | None = None,
    ):
        """按预置事件模拟一次 `run_turn`。

        逻辑：
        1. 记录调用参数；
        2. `human_message` 时写入 user，模拟真实 runtime 的入口副作用；
        3. 遇到 `tool_call` 时填充 pending 后再 yield。
        """
        self.calls.append((request_type, str(content or "")))
        if request_type == "human_message":
            ctx.messages.append({"role": "user", "content": str(content or "")})
        for event in self._events:
            if event.event_type == "tool_call":
                pending: list[PendingToolCall] = []
                for call in list(event.payload.get("tool_calls") or []):
                    pending.append(
                        PendingToolCall(
                            call_id=str(call.get("id") or ""),
                            name=str(call.get("name") or ""),
                            arguments=dict(call.get("arguments") or {}),
                        )
                    )
                ctx.pending_tool_calls = pending
                ctx.run_turn_phase = RunTurnPhase.AWAITING_TOOL_EXECUTION
            yield event


@unittest.skipIf(MainAgentTurnOrchestrator is None, _ORCHESTRATOR_SKIP)
class MainAgentTurnOrchestratorTests(unittest.IsolatedAsyncioTestCase):
    """主编排器核心分支测试。"""

    def _make_orchestrator(self, *, tool_map: dict[str, object] | None = None):
        """构造编排器与事件捕获器。

        逻辑：
        1. patch 配置关闭 summary 压缩，避免单测拉起 summary runtime；
        2. 用 `AsyncMock` 捕获回灌入队；
        3. 用本地列表捕获发出的事件信封。
        """
        emitted: list[AgentEventEnvelope] = []

        async def _emit_envelope(
            *,
            env: MessageEnvelope,
            envelope: AgentEventEnvelope,
            base_meta: dict,
        ) -> None:
            del env, base_meta
            emitted.append(envelope)

        settings = settings_namespace(
            summary_compression_silent_trigger_tokens=0,
            summary_compression_blocking_trigger_tokens=0,
        )
        patcher = patch("app.core.main_agent.agent.get_settings", return_value=settings)
        patcher.start()
        self.addCleanup(patcher.stop)
        submit = AsyncMock()
        assert MainAgentTurnOrchestrator is not None
        orchestrator = MainAgentTurnOrchestrator(
            submit_message=submit,
            emit_envelope=_emit_envelope,
            tool_map=dict(tool_map or {}),
        )
        return orchestrator, submit, emitted

    async def test_human_message_without_tool_emits_done(self) -> None:
        """无工具调用时应透传 assistant 并最终发出 done。"""
        runtime = FakeRuntime(
            [
                AgentEventEnvelope(event_type="assistant", payload={"content": "hi"}, meta={}),
                AgentEventEnvelope(event_type="done", payload={"finish_reason": "stop"}, meta={}),
            ]
        )
        orchestrator, submit, emitted = self._make_orchestrator()
        ctx = OpenAIConversationContext(session_id="s1")
        env = MessageEnvelope(session_id="s1", request_type="message", content="hello", client_id="c1")

        await orchestrator.handle_message(ctx=ctx, runtime=runtime, env=env, base_meta={})

        self.assertEqual([item.event_type for item in emitted], ["assistant", "done"])
        self.assertEqual(emitted[-1].payload.get("finish_reason"), "stop")
        submit.assert_not_awaited()

    async def test_auto_tool_call_appends_tool_and_submits_tool_result(self) -> None:
        """无需审批的工具调用应执行、写入 tool 消息并回灌 `tool_result` 入队。"""
        tool_call = {
            "id": "call-1",
            "name": "demo_tool",
            "arguments": {"x": 1},
            "raw_arguments": "{\"x\":1}",
        }
        runtime = FakeRuntime(
            [
                AgentEventEnvelope(
                    event_type="tool_call",
                    payload={"tool_calls": [tool_call], "assistant_content": ""},
                    meta={},
                ),
                AgentEventEnvelope(event_type="done", payload={"finish_reason": "tool_calls"}, meta={}),
            ]
        )
        tool_spec = SimpleNamespace(invoke=lambda args, ctx: {"ok": True, "args": args, "sid": ctx.session_id})
        orchestrator, submit, emitted = self._make_orchestrator(tool_map={"demo_tool": tool_spec})
        ctx = OpenAIConversationContext(session_id="s2")
        env = MessageEnvelope(session_id="s2", request_type="message", content="run", client_id="c2")

        with patch("app.core.main_agent.agent.should_require_tool_approval", return_value=False):
            await orchestrator.handle_message(ctx=ctx, runtime=runtime, env=env, base_meta={})

        self.assertIn("tool_call", [item.event_type for item in emitted])
        self.assertIn("tool_result", [item.event_type for item in emitted])
        self.assertEqual(ctx.messages[-1]["role"], "tool")
        submit.assert_awaited_once()
        self.assertEqual(submit.await_args.kwargs["request_type"], "tool_result")
        self.assertEqual(submit.await_args.kwargs["priority"], "tool_result")

    async def test_tool_call_requiring_approval_keeps_pending(self) -> None:
        """需要审批的工具调用应发出 approval_required 并保留 pending。"""
        tool_call = {
            "id": "call-approval",
            "name": "danger_tool",
            "arguments": {"cmd": "deploy"},
            "raw_arguments": "{\"cmd\":\"deploy\"}",
        }
        runtime = FakeRuntime(
            [
                AgentEventEnvelope(
                    event_type="tool_call",
                    payload={"tool_calls": [tool_call], "assistant_content": "need tool"},
                    meta={},
                ),
                AgentEventEnvelope(event_type="done", payload={"finish_reason": "tool_calls"}, meta={}),
            ]
        )
        orchestrator, submit, emitted = self._make_orchestrator()
        ctx = OpenAIConversationContext(session_id="s3")
        env = MessageEnvelope(session_id="s3", request_type="message", content="deploy", client_id="c3")

        with patch("app.core.main_agent.agent.should_require_tool_approval", return_value=True):
            await orchestrator.handle_message(ctx=ctx, runtime=runtime, env=env, base_meta={})

        self.assertEqual([item.event_type for item in emitted], ["tool_call", "approval_required", "done"])
        self.assertEqual(len(ctx.pending_tool_calls), 1)
        self.assertEqual(ctx.run_turn_phase, RunTurnPhase.AWAITING_TOOL_EXECUTION)
        submit.assert_not_awaited()

    async def test_human_message_interrupts_existing_pending_tools(self) -> None:
        """pending 阶段收到新 human 时应补 tool 占位、清 pending 并继续新一轮。"""
        runtime = FakeRuntime([AgentEventEnvelope(event_type="done", payload={"finish_reason": "stop"}, meta={})])
        orchestrator, _submit, emitted = self._make_orchestrator()
        ctx = OpenAIConversationContext(
            session_id="s4",
            pending_tool_calls=[
                PendingToolCall(call_id="old-call", name="old_tool", arguments={}),
            ],
            run_turn_phase=RunTurnPhase.AWAITING_TOOL_EXECUTION,
        )
        env = MessageEnvelope(session_id="s4", request_type="message", content="new task", client_id="c4")

        await orchestrator.handle_message(ctx=ctx, runtime=runtime, env=env, base_meta={})

        self.assertEqual(ctx.pending_tool_calls, [])
        self.assertEqual(ctx.messages[0]["role"], "tool")
        self.assertTrue(emitted[0].payload.get("interrupted_by_user_message"))
        self.assertEqual(runtime.calls[0][0], "human_message")

    async def test_resume_approve_executes_pending_and_submits_tool_result(self) -> None:
        """approve resume 应执行 pending 工具、清空 pending 并回灌 tool_result。"""
        tool_spec = SimpleNamespace(invoke=lambda args, ctx: f"ok:{args['value']}:{ctx.session_id}")
        orchestrator, submit, emitted = self._make_orchestrator(tool_map={"resume_tool": tool_spec})
        ctx = OpenAIConversationContext(
            session_id="s5",
            pending_tool_calls=[
                PendingToolCall(call_id="call-resume", name="resume_tool", arguments={"value": "v"}),
            ],
            run_turn_phase=RunTurnPhase.AWAITING_TOOL_EXECUTION,
        )
        env = MessageEnvelope(
            session_id="s5",
            request_type="resume",
            resume_value={"type": "approve"},
            client_id="c5",
        )

        await orchestrator.handle_message(ctx=ctx, runtime=FakeRuntime([]), env=env, base_meta={})

        self.assertEqual(ctx.pending_tool_calls, [])
        self.assertEqual(ctx.messages[-1]["tool_call_id"], "call-resume")
        self.assertEqual([item.event_type for item in emitted], ["tool_result"])
        submit.assert_awaited_once()
        self.assertEqual(submit.await_args.kwargs["request_type"], "tool_result")


if __name__ == "__main__":
    unittest.main()
