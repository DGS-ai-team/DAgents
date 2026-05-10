"""上下文指标：`OpenAIConversationContext.messages` 导出到 Prometheus。"""

from __future__ import annotations

import sys
import unittest
import uuid
from pathlib import Path

from prometheus_client import generate_latest

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.context.models import OpenAIConversationContext  # noqa: E402
from app.observability import metrics as m  # noqa: E402


class SessionContextMetricsTestCase(unittest.TestCase):
    def tearDown(self) -> None:
        m.refresh_session_context_metrics({})

    def test_refresh_reflects_context_messages(self) -> None:
        """metrics 反映各 session 的 ctx.messages 条数。"""
        sid = f"ut_ctx_{uuid.uuid4().hex}"
        safe_sid = m.sanitize_prometheus_label_value(sid)
        ctx = OpenAIConversationContext(
            session_id=sid,
            messages=[
                {"role": "user", "content": "hello"},
                {"role": "assistant", "content": "", "tool_calls": [{"id": "c1", "type": "function", "function": {"name": "x", "arguments": "{}"}}]},
            ],
        )
        m.refresh_session_context_metrics({sid: ctx})
        body = generate_latest().decode("utf-8")
        self.assertIn(f'dagents_session_context_messages_count{{session_id="{safe_sid}"}} 2.0', body)
        self.assertNotIn("dagents_session_context_message_content_chars", body)

    def test_refresh_clears_removed_session(self) -> None:
        """session 从映射中移除后对应 series 被摘掉。"""
        sid = f"ut_ctx_{uuid.uuid4().hex}"
        safe_sid = m.sanitize_prometheus_label_value(sid)
        ctx = OpenAIConversationContext(session_id=sid, messages=[{"role": "system", "content": "x"}])
        m.refresh_session_context_metrics({sid: ctx})
        self.assertIn(safe_sid, generate_latest().decode("utf-8"))
        m.refresh_session_context_metrics({})
        body = generate_latest().decode("utf-8")
        self.assertNotIn(f'dagents_session_context_messages_count{{session_id="{safe_sid}"}}', body)


if __name__ == "__main__":
    unittest.main()
