from __future__ import annotations

import sys
import unittest
from pathlib import Path

from langchain_core.messages import AIMessage
from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.harness.api.app import create_app


class ApiSseTestCase(unittest.TestCase):
    def test_submit_and_stream_sse(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            class FakeGraph:
                async def astream(self, payload, config=None, stream_mode=None):
                    assert isinstance(config, dict)
                    assert isinstance(config.get("configurable"), dict)
                    assert stream_mode == ["messages", "updates"]
                    yield {"messages": [AIMessage(content="pong")]}

            app.state.service._get_graph = lambda: FakeGraph()  # type: ignore[method-assign]

            resp = client.post(
                "/v1/messages",
                json={"session_id": "sse-test", "content": "hello", "source": "test", "priority": "other"},
            )
            self.assertEqual(resp.status_code, 200)
            body = resp.json()
            self.assertTrue(body["accepted"])
            request_id = body["request_id"]

            with client.stream("GET", f"/v1/streams/{request_id}") as sse_resp:
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

