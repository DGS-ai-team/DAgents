"""`MainAgentTurnOrchestrator` 单测：覆盖主消息分支与工具闭环。"""

from __future__ import annotations

import asyncio
import unittest
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from app.context.models import OpenAIConversationContext, PendingToolCall, RunTurnPhase
from app.harness.queue.message_queue import MessageEnvelope
from app.harness.service.interface import AgentEventEnvelope
from tests.test_support.stub_settings import settings_namespace

try:
    from app.core.main_agent.agent import MainAgentTurnOrchestrator
    from app.core.main_agent.runtime_openai import OpenAIImplicitReActRuntime
except ImportError as exc:  # pragma: no cover - 仅精简环境触发
    MainAgentTurnOrchestrator = None  # type: ignore[assignment]
    OpenAIImplicitReActRuntime = None  # type: ignore[assignment]
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
        summary_runtime = SimpleNamespace(should_compress=lambda *_args, **_kwargs: {"should_compress": False})
        summary_patcher = patch("app.core.main_agent.agent.init_summary_agent", return_value=summary_runtime)
        summary_patcher.start()
        self.addCleanup(summary_patcher.stop)
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

    async def test_mixed_auto_and_approval_tool_calls_wait_as_one_batch(self) -> None:
        """同一轮只要有工具需审批，整批 tool_calls 都应等待 resume 后统一执行。"""
        safe_call = {
            "id": "call-safe",
            "name": "safe_tool",
            "arguments": {"value": 1},
            "raw_arguments": "{\"value\":1}",
        }
        danger_call = {
            "id": "call-danger",
            "name": "danger_tool",
            "arguments": {"cmd": "deploy"},
            "raw_arguments": "{\"cmd\":\"deploy\"}",
        }
        runtime = FakeRuntime(
            [
                AgentEventEnvelope(
                    event_type="tool_call",
                    payload={"tool_calls": [safe_call, danger_call], "assistant_content": "need tools"},
                    meta={},
                ),
                AgentEventEnvelope(event_type="done", payload={"finish_reason": "tool_calls"}, meta={}),
            ]
        )
        safe_spec = SimpleNamespace(invoke=lambda args, ctx: {"safe": True})
        danger_spec = SimpleNamespace(invoke=lambda args, ctx: {"danger": True})
        orchestrator, submit, emitted = self._make_orchestrator(
            tool_map={"safe_tool": safe_spec, "danger_tool": danger_spec}
        )
        ctx = OpenAIConversationContext(session_id="s3-mixed")
        env = MessageEnvelope(session_id="s3-mixed", request_type="message", content="run", client_id="c3")

        def _approval_policy(*, tool_name: str, tool_args: dict, context: OpenAIConversationContext) -> bool:
            del tool_args, context
            return tool_name == "danger_tool"

        with patch("app.core.main_agent.agent.should_require_tool_approval", side_effect=_approval_policy):
            await orchestrator.handle_message(ctx=ctx, runtime=runtime, env=env, base_meta={})

        self.assertEqual([item.event_type for item in emitted], ["tool_call", "approval_required", "done"])
        self.assertEqual([item.call_id for item in ctx.pending_tool_calls], ["call-safe", "call-danger"])
        approval_calls = emitted[1].payload["args"]["tool_calls"]
        self.assertEqual([item["id"] for item in approval_calls], ["call-safe", "call-danger"])
        self.assertFalse(any(msg.get("role") == "tool" for msg in ctx.messages))
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

    async def test_resume_reject_closes_pending_tools_and_submits_tool_result(self) -> None:
        """reject resume 应补齐 tool 消息、清空 pending 并回灌 tool_result 继续推理。"""
        orchestrator, submit, emitted = self._make_orchestrator()
        ctx = OpenAIConversationContext(
            session_id="s6",
            pending_tool_calls=[
                PendingToolCall(call_id="call-a", name="tool_a", arguments={}),
                PendingToolCall(call_id="call-b", name="tool_b", arguments={}),
            ],
            run_turn_phase=RunTurnPhase.AWAITING_TOOL_EXECUTION,
        )
        env = MessageEnvelope(
            session_id="s6",
            request_type="resume",
            resume_value={"type": "reject"},
            client_id="c6",
        )

        await orchestrator.handle_message(ctx=ctx, runtime=FakeRuntime([]), env=env, base_meta={})

        self.assertEqual(ctx.pending_tool_calls, [])
        self.assertEqual(ctx.run_turn_phase, RunTurnPhase.IDLE)
        self.assertEqual([item.event_type for item in emitted], ["tool_result", "tool_result"])
        self.assertEqual([item.payload.get("rejected") for item in emitted], [True, True])
        self.assertEqual([msg["role"] for msg in ctx.messages[-2:]], ["tool", "tool"])
        self.assertEqual([msg["tool_call_id"] for msg in ctx.messages[-2:]], ["call-a", "call-b"])
        submit.assert_awaited_once()
        self.assertEqual(submit.await_args.kwargs["request_type"], "tool_result")

    async def test_resume_selection_must_cover_all_pending_tools(self) -> None:
        """selection 未覆盖全部 pending 时应拒绝变更，避免留下半闭合 tool_calls。"""
        orchestrator, submit, emitted = self._make_orchestrator()
        ctx = OpenAIConversationContext(
            session_id="s7",
            pending_tool_calls=[
                PendingToolCall(call_id="call-a", name="tool_a", arguments={}),
                PendingToolCall(call_id="call-b", name="tool_b", arguments={}),
            ],
            run_turn_phase=RunTurnPhase.AWAITING_TOOL_EXECUTION,
        )
        env = MessageEnvelope(
            session_id="s7",
            request_type="resume",
            resume_value={"type": "selection", "approved": ["call-a"], "rejected": []},
            client_id="c7",
        )

        await orchestrator.handle_message(ctx=ctx, runtime=FakeRuntime([]), env=env, base_meta={})

        self.assertEqual([item.call_id for item in ctx.pending_tool_calls], ["call-a", "call-b"])
        self.assertEqual(ctx.run_turn_phase, RunTurnPhase.AWAITING_TOOL_EXECUTION)
        self.assertEqual([item.event_type for item in emitted], ["error", "done"])
        self.assertEqual(emitted[-1].payload.get("finish_reason"), "resume_selection_invalid")
        submit.assert_not_awaited()

    async def test_resume_selection_executes_and_rejects_all_pending_tools(self) -> None:
        """selection 覆盖全部 pending 时应执行批准项、补齐拒绝项并回灌 tool_result。"""
        tool_spec = SimpleNamespace(invoke=lambda args, ctx: f"ok:{args['value']}:{ctx.session_id}")
        orchestrator, submit, emitted = self._make_orchestrator(tool_map={"tool_a": tool_spec})
        ctx = OpenAIConversationContext(
            session_id="s8",
            pending_tool_calls=[
                PendingToolCall(call_id="call-a", name="tool_a", arguments={"value": "v"}),
                PendingToolCall(call_id="call-b", name="tool_b", arguments={}),
            ],
            run_turn_phase=RunTurnPhase.AWAITING_TOOL_EXECUTION,
        )
        env = MessageEnvelope(
            session_id="s8",
            request_type="resume",
            resume_value={"type": "selection", "approved": ["call-a"], "rejected": ["call-b"]},
            client_id="c8",
        )

        await orchestrator.handle_message(ctx=ctx, runtime=FakeRuntime([]), env=env, base_meta={})

        self.assertEqual(ctx.pending_tool_calls, [])
        self.assertEqual(ctx.run_turn_phase, RunTurnPhase.IDLE)
        self.assertEqual([item.event_type for item in emitted], ["tool_result", "tool_result"])
        self.assertEqual(emitted[0].payload.get("tool_call_id"), "call-a")
        self.assertEqual(emitted[1].payload.get("tool_call_id"), "call-b")
        self.assertTrue(emitted[1].payload.get("rejected"))
        self.assertEqual([msg["tool_call_id"] for msg in ctx.messages[-2:]], ["call-a", "call-b"])
        submit.assert_awaited_once()
        self.assertEqual(submit.await_args.kwargs["tool_result"]["results"][1]["rejected"], True)

    async def test_cancelled_reasoning_delta_is_not_flushed_as_assistant(self) -> None:
        """取消 reasoning-only 流时不应把 reasoning 写成 assistant 历史消息。"""
        assert OpenAIImplicitReActRuntime is not None
        runtime = object.__new__(OpenAIImplicitReActRuntime)
        runtime._max_tool_loops = 10

        async def _fake_stream(_messages, _system_prompt):
            yield {"kind": "reasoning_delta", "text": "hidden reasoning"}
            raise asyncio.CancelledError()

        runtime._request_model_stream = _fake_stream
        ctx = OpenAIConversationContext(session_id="s8")

        with patch("app.core.main_agent.runtime_openai.get_system_prompt", return_value="system"):
            stream = runtime.run_turn(ctx, request_type="human_message", content="hello")
            first = await anext(stream)
            self.assertEqual(first.event_type, "reasoning")
            with self.assertRaises(asyncio.CancelledError):
                await anext(stream)

        runtime.flush_cancelled_turn(ctx)

        self.assertFalse(any(msg.get("role") == "assistant" for msg in ctx.messages))
        self.assertEqual(ctx.assistant_stream_buffer, "")
        self.assertEqual(ctx.run_turn_phase, RunTurnPhase.IDLE)

    async def test_tool_loop_limit_blocks_tool_message_without_model_call(self) -> None:
        """tool_message 达到循环上限时应直接停止，不能继续请求模型。"""
        assert OpenAIImplicitReActRuntime is not None
        runtime = object.__new__(OpenAIImplicitReActRuntime)
        runtime._max_tool_loops = 2
        called = False

        async def _fake_stream(_messages, _system_prompt):
            nonlocal called
            called = True
            yield {"kind": "final", "message": {"content": "unexpected", "tool_calls": []}}

        runtime._request_model_stream = _fake_stream
        ctx = OpenAIConversationContext(session_id="s9", tool_loop_count=2)

        events: list[AgentEventEnvelope] = []
        with patch("app.core.main_agent.runtime_openai.get_system_prompt", return_value="system"):
            async for event in runtime.run_turn(ctx, request_type="tool_message", content="tool_message"):
                events.append(event)

        self.assertFalse(called)
        self.assertEqual([item.event_type for item in events], ["error", "done"])
        self.assertEqual(events[-1].payload.get("finish_reason"), "error")
        self.assertEqual(ctx.tool_loop_count, 2)
        self.assertEqual(ctx.run_turn_phase, RunTurnPhase.IDLE)

    async def test_human_message_resets_tool_loop_count_before_model_call(self) -> None:
        """新 human 话轮应重置旧工具循环计数，避免误触上限。"""
        assert OpenAIImplicitReActRuntime is not None
        runtime = object.__new__(OpenAIImplicitReActRuntime)
        runtime._max_tool_loops = 2

        async def _fake_stream(_messages, _system_prompt):
            yield {"kind": "finish_reason", "finish_reason": "stop"}
            yield {"kind": "final", "message": {"content": "ok", "tool_calls": []}}

        runtime._request_model_stream = _fake_stream
        ctx = OpenAIConversationContext(session_id="s10", tool_loop_count=2)

        events: list[AgentEventEnvelope] = []
        with patch("app.core.main_agent.runtime_openai.get_system_prompt", return_value="system"):
            async for event in runtime.run_turn(ctx, request_type="human_message", content="hello"):
                events.append(event)

        self.assertEqual([item.event_type for item in events], ["done"])
        self.assertEqual(ctx.tool_loop_count, 0)
        self.assertEqual(ctx.run_turn_phase, RunTurnPhase.IDLE)
        self.assertEqual(ctx.messages[-1], {"role": "assistant", "content": "ok"})


if __name__ == "__main__":
    unittest.main()
