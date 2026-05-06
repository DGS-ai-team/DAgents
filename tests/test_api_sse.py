from __future__ import annotations

import sys
import unittest
from pathlib import Path

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.context.models import OpenAIConversationContext
from app.harness.api.app import create_app
from app.harness.service.interface import AgentEventEnvelope


class _SseFakeRuntime:
    """与当前 `AgentService` 编排一致：仅实现 `run_turn` + `flush_cancelled_turn`。"""

    async def run_turn(
        self,
        ctx: OpenAIConversationContext,
        *,
        request_type: str,
        content: str | None = None,
        resume_value: object | None = None,
    ):
        yield AgentEventEnvelope(event_type="assistant", payload={"content": "pong"}, meta={})
        yield AgentEventEnvelope(event_type="done", payload={}, meta={})

    def flush_cancelled_turn(self, ctx: OpenAIConversationContext) -> None:
        pass


class ApiSseTestCase(unittest.TestCase):
    def test_release_session_api(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            create_resp = client.post("/v1/sessions", json={"session_id": "release-test"})
            self.assertEqual(create_resp.status_code, 200)
            self.assertEqual(create_resp.json()["session_id"], "release-test")
            self.assertIn("release-test", app.state.service._session_queues)

            release_resp = client.delete("/v1/sessions/release-test")
            self.assertEqual(release_resp.status_code, 200)
            body = release_resp.json()
            self.assertEqual(body["session_id"], "release-test")
            self.assertTrue(body["released"])
            self.assertNotIn("release-test", app.state.service._session_queues)
            self.assertNotIn("release-test", app.state.service._session_contexts)
            self.assertNotIn("release-test", app.state.service._session_consumer_tasks)

    def test_submit_and_stream_sse(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            # 覆盖懒加载 runtime，避免走真实 OpenAI；SSE 事件名与 `_map_event_envelope_to_stream` 一致。
            app.state.service._runtime = _SseFakeRuntime()

            resp = client.post(
                "/v1/messages",
                json={
                    "session_id": "sse-test",
                    "client_id": "test-client",
                    "content": "hello",
                    "source": "test",
                    "priority": "other",
                },
            )
            self.assertEqual(resp.status_code, 200)
            body = resp.json()
            self.assertTrue(body["accepted"])

            with client.stream("GET", "/v1/streams?client_id=test-client") as sse_resp:
                self.assertEqual(sse_resp.status_code, 200)
                chunks = []
                for line in sse_resp.iter_lines():
                    if line:
                        chunks.append(line)
                    if isinstance(line, str) and "event: done" in line:
                        break

            joined = "\n".join(str(x) for x in chunks)
            self.assertIn("event: assistant", joined)
            self.assertIn("event: done", joined)


if __name__ == "__main__":
    unittest.main()

