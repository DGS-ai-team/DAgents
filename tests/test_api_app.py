"""FastAPI 网关单测：路由到 `AgentService` 的接线与 SSE 编码。"""

from __future__ import annotations

import sys
import types
import unittest
from unittest.mock import patch

from tests.test_support.stub_settings import settings_namespace


if "openai" not in sys.modules:
    fake_openai = types.ModuleType("openai")

    class _FakeAsyncOpenAI:
        """精简环境下的 OpenAI SDK 占位类。"""

        def __init__(self, *_args: object, **_kwargs: object) -> None:
            """保持构造兼容；测试不会真实调用。"""

    fake_openai.AsyncOpenAI = _FakeAsyncOpenAI  # type: ignore[attr-defined]
    sys.modules["openai"] = fake_openai

try:
    from fastapi.testclient import TestClient

    from app.harness.api import app as api_app
    from app.harness.streaming.events import StreamEvent
except ImportError as exc:  # pragma: no cover - 仅精简环境触发
    TestClient = None  # type: ignore[assignment]
    api_app = None  # type: ignore[assignment]
    StreamEvent = None  # type: ignore[assignment]
    _API_SKIP = f"FastAPI API 依赖链未就绪（{exc!r}）；请执行 pip install -r requirements.txt"
else:
    _API_SKIP = ""


class FakeAgentService:
    """API 测试用服务替身。

    逻辑：
    1. 记录所有实例，便于测试断言 lifespan 创建的服务；
    2. 异步方法保持与真实 `AgentService` 路由调用形态一致；
    3. 仅记录调用参数，不触发真实队列、runtime 或外部网络。
    """

    instances: list["FakeAgentService"] = []

    def __init__(self, *args: object, **kwargs: object) -> None:
        """记录构造参数和调用列表。"""
        self.args = args
        self.kwargs = kwargs
        self.started = False
        self.stopped = False
        self.created_sessions: list[str | None] = []
        self.submitted_messages: list[dict[str, object]] = []
        self.submitted_resumes: list[dict[str, object]] = []
        self.released_sessions: list[tuple[str, bool]] = []
        FakeAgentService.instances.append(self)

    async def start(self) -> None:
        """模拟服务启动。"""
        self.started = True

    async def stop(self) -> None:
        """模拟服务停止。"""
        self.stopped = True

    async def create_session(self, session_id: str | None = None) -> str:
        """返回传入 session 或固定替身 session。"""
        self.created_sessions.append(session_id)
        return session_id or "generated-session"

    async def submit_message(self, **kwargs: object) -> None:
        """记录普通消息入队参数。"""
        self.submitted_messages.append(dict(kwargs))

    async def submit_resume(self, **kwargs: object) -> None:
        """记录 resume 入队参数。"""
        self.submitted_resumes.append(dict(kwargs))

    def cancel_current_turn(self, session_id: str) -> bool:
        """模拟取消当前 turn。"""
        return session_id == "s-cancel"

    async def release_session(self, session_id: str, *, clear_persisted: bool = True) -> bool:
        """记录会话释放参数。"""
        self.released_sessions.append((session_id, clear_persisted))
        return True


@unittest.skipIf(TestClient is None or api_app is None, _API_SKIP)
class FastApiRouteTests(unittest.TestCase):
    """FastAPI 路由与服务层接线测试。"""

    def setUp(self) -> None:
        """清理服务实例列表，避免用例间串状态。"""
        FakeAgentService.instances.clear()

    def _client(self):
        """构造带替身服务的 TestClient。

        逻辑：
        1. patch `AgentService`，避免 lifespan 拉起真实 runtime；
        2. patch `get_settings`，关闭 metrics 和 registry 自登记；
        3. 返回 `TestClient` 上下文管理器。
        """
        assert api_app is not None
        settings = settings_namespace(metrics_enabled=False)
        return patch.multiple(
            api_app,
            AgentService=FakeAgentService,
            get_settings=lambda *args, **kwargs: settings,
        )

    def test_session_message_resume_cancel_and_release_routes(self) -> None:
        """核心 HTTP 路由应调用对应服务方法并返回稳定响应。"""
        assert api_app is not None and TestClient is not None
        with self._client():
            app = api_app.create_app()
            with TestClient(app) as client:
                create_resp = client.post("/v1/sessions", json={"session_id": "s-api"})
                self.assertEqual(create_resp.status_code, 200)
                self.assertEqual(create_resp.json()["session_id"], "s-api")

                msg_resp = client.post(
                    "/v1/messages",
                    json={"session_id": "s-api", "client_id": "c-api", "content": "hello"},
                )
                self.assertEqual(msg_resp.status_code, 200)
                self.assertEqual(msg_resp.json()["priority"], "human")

                resume_resp = client.post(
                    "/v1/messages",
                    json={
                        "session_id": "s-api",
                        "client_id": "c-api",
                        "request_type": "resume",
                        "resume_value": {"type": "approve"},
                    },
                )
                self.assertEqual(resume_resp.status_code, 200)
                self.assertEqual(resume_resp.json()["priority"], "resume")

                cancel_resp = client.post("/v1/sessions/s-cancel/cancel")
                self.assertEqual(cancel_resp.status_code, 200)
                self.assertTrue(cancel_resp.json()["cancelled"])

                release_resp = client.delete("/v1/sessions/s-api")
                self.assertEqual(release_resp.status_code, 200)
                self.assertTrue(release_resp.json()["released"])

        service = FakeAgentService.instances[-1]
        self.assertTrue(service.started)
        self.assertTrue(service.stopped)
        self.assertEqual(service.submitted_messages[0]["priority"], "human")
        self.assertEqual(service.submitted_resumes[0]["priority"], "resume")
        self.assertEqual(service.released_sessions, [("s-api", True)])

    def test_message_route_rejects_blank_content(self) -> None:
        """普通 message 缺正文时应返回 422，且不调用服务入队。"""
        assert api_app is not None and TestClient is not None
        with self._client():
            app = api_app.create_app()
            with TestClient(app) as client:
                resp = client.post(
                    "/v1/messages",
                    json={"session_id": "s-api", "client_id": "c-api", "content": "  "},
                )
                self.assertEqual(resp.status_code, 422)
        self.assertEqual(FakeAgentService.instances[-1].submitted_messages, [])


@unittest.skipIf(StreamEvent is None or api_app is None, _API_SKIP)
class SseEncodingTests(unittest.TestCase):
    """SSE 编码格式测试。"""

    def test_to_sse_outputs_event_and_json_data(self) -> None:
        """`_to_sse` 应输出标准 event/data 空行结尾格式。"""
        assert StreamEvent is not None and api_app is not None
        event = StreamEvent(
            client_id="c1",
            session_id="s1",
            type="assistant",
            seq=0,
            ts="2026-05-18T00:00:00+00:00",
            data={"content": "你好"},
        )

        text = api_app._to_sse(event)

        self.assertTrue(text.startswith("id: 0\nevent: assistant\n"))
        self.assertIn('"content": "你好"', text)
        self.assertTrue(text.endswith("\n\n"))

    def test_parse_last_event_id_ignores_invalid_values(self) -> None:
        """`Last-Event-ID` 非整数时应忽略，避免重连请求因脏 header 失败。"""
        assert api_app is not None
        self.assertEqual(api_app._parse_last_event_id("42"), 42)
        self.assertIsNone(api_app._parse_last_event_id("not-an-int"))
        self.assertIsNone(api_app._parse_last_event_id(None))


if __name__ == "__main__":
    unittest.main()
