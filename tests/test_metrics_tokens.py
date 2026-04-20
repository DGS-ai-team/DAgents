"""观测指标：`parse_usage_tokens` 与 token Counter 累加。"""

from __future__ import annotations

import unittest
import uuid
from types import SimpleNamespace

from prometheus_client import generate_latest

from app.observability.metrics import (
    parse_usage_tokens,
    record_llm_token_usage,
    sanitize_model_label,
    usage_fields_from_openai_usage,
)


class MetricsTokensTestCase(unittest.TestCase):
    def test_parse_usage_tokens_none(self) -> None:
        self.assertEqual(parse_usage_tokens(None), (0, 0))

    def test_parse_usage_tokens_dict(self) -> None:
        self.assertEqual(parse_usage_tokens({"prompt_tokens": 1, "completion_tokens": 2}), (1, 2))
        self.assertEqual(parse_usage_tokens({}), (0, 0))

    def test_parse_usage_tokens_object(self) -> None:
        u = SimpleNamespace(prompt_tokens=9, completion_tokens=3)
        self.assertEqual(parse_usage_tokens(u), (9, 3))

    def test_parse_usage_tokens_clamps_negative(self) -> None:
        self.assertEqual(parse_usage_tokens({"prompt_tokens": -1, "completion_tokens": 5}), (0, 5))

    def test_usage_fields_from_openai_usage(self) -> None:
        u = SimpleNamespace(prompt_tokens=1, completion_tokens=2, total_tokens=3)
        self.assertEqual(
            usage_fields_from_openai_usage(u),
            {"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3},
        )
        self.assertEqual(
            usage_fields_from_openai_usage({"prompt_tokens": 4, "completion_tokens": 5}),
            {"prompt_tokens": 4, "completion_tokens": 5, "total_tokens": None},
        )

    def test_sanitize_model_label(self) -> None:
        self.assertEqual(sanitize_model_label(""), "unknown")
        self.assertEqual(sanitize_model_label("  "), "unknown")
        self.assertTrue(sanitize_model_label("a/b:c").startswith("a"))

    def test_record_llm_token_usage_unique_model(self) -> None:
        """用唯一 model label，避免与其它用例在同一 Registry 上累加混淆。"""
        label = f"ut_{uuid.uuid4().hex}"
        safe = sanitize_model_label(label)
        record_llm_token_usage(prompt_tokens=13, completion_tokens=17, model=label)
        body = generate_latest().decode("utf-8")
        self.assertIn(f'dagents_llm_prompt_tokens_total{{model="{safe}"}} 13.0', body)
        self.assertIn(f'dagents_llm_completion_tokens_total{{model="{safe}"}} 17.0', body)


if __name__ == "__main__":
    unittest.main()
