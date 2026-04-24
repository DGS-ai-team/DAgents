from __future__ import annotations

import sys
import unittest
from pathlib import Path

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.harness.api.app import create_app


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
            class _FakeAssistantChunk:
                __slots__ = ("content",)

                def __init__(self, content: str) -> None:
                    self.content = content

            class FakeGraph:
                async def astream(self, payload, config=None, stream_mode=None):
                    assert isinstance(config, dict)
                    assert isinstance(config.get("configurable"), dict)
                    assert stream_mode == ["messages", "updates"]
                    yield {"messages": [_FakeAssistantChunk(content="pong")]}

            app.state.service._get_graph = lambda: FakeGraph()  # type: ignore[method-assign]

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
            self.assertIn("event: chunk", joined)
            self.assertIn("event: done", joined)


if __name__ == "__main__":
    unittest.main()

