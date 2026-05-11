"""观测指标：`parse_usage_tokens` 与 token Gauge 快照写入。"""

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
            {
                "prompt_tokens": 1,
                "completion_tokens": 2,
                "total_tokens": 3,
                "prompt_audio_tokens": 0,
                "prompt_cached_tokens": 0,
                "prompt_cache_hit_tokens": 0,
                "prompt_cache_miss_tokens": 0,
            },
        )
        self.assertEqual(
            usage_fields_from_openai_usage({"prompt_tokens": 4, "completion_tokens": 5}),
            {
                "prompt_tokens": 4,
                "completion_tokens": 5,
                "total_tokens": None,
                "prompt_audio_tokens": 0,
                "prompt_cached_tokens": 0,
                "prompt_cache_hit_tokens": 0,
                "prompt_cache_miss_tokens": 0,
            },
        )

    def test_usage_fields_includes_prompt_details_and_cache(self) -> None:
        u = SimpleNamespace(
            prompt_tokens=10,
            completion_tokens=20,
            total_tokens=30,
            prompt_cache_hit_tokens=3,
            prompt_cache_miss_tokens=7,
            prompt_tokens_details=SimpleNamespace(audio_tokens=1, cached_tokens=5),
        )
        self.assertEqual(
            usage_fields_from_openai_usage(u),
            {
                "prompt_tokens": 10,
                "completion_tokens": 20,
                "total_tokens": 30,
                "prompt_audio_tokens": 1,
                "prompt_cached_tokens": 5,
                "prompt_cache_hit_tokens": 3,
                "prompt_cache_miss_tokens": 7,
            },
        )

    def test_sanitize_model_label(self) -> None:
        self.assertEqual(sanitize_model_label(""), "unknown")
        self.assertEqual(sanitize_model_label("  "), "unknown")
        self.assertTrue(sanitize_model_label("a/b:c").startswith("a"))

    def test_record_llm_token_usage_unique_model(self) -> None:
        """用唯一 model label，避免与其它用例在同一 Registry 上混淆。"""
        label = f"ut_{uuid.uuid4().hex}"
        safe = sanitize_model_label(label)
        record_llm_token_usage(prompt_tokens=13, completion_tokens=17, model=label)
        body = generate_latest().decode("utf-8")
        self.assertIn(f'dagents_llm_prompt_tokens{{model="{safe}"}} 13.0', body)
        self.assertIn(f'dagents_llm_completion_tokens{{model="{safe}"}} 17.0', body)

    def test_record_llm_token_usage_overwrites_gauge(self) -> None:
        """Gauge 以后一次 set 为准（上游累计值不应在本进程再 inc）。"""
        label = f"ut_{uuid.uuid4().hex}"
        safe = sanitize_model_label(label)
        record_llm_token_usage(prompt_tokens=10, completion_tokens=5, model=label)
        record_llm_token_usage(prompt_tokens=100, completion_tokens=50, model=label)
        body = generate_latest().decode("utf-8")
        self.assertIn(f'dagents_llm_prompt_tokens{{model="{safe}"}} 100.0', body)
        self.assertIn(f'dagents_llm_completion_tokens{{model="{safe}"}} 50.0', body)

    def test_record_llm_token_usage_writes_cache_gauges_when_usage_given(self) -> None:
        label = f"ut_{uuid.uuid4().hex}"
        safe = sanitize_model_label(label)
        u = SimpleNamespace(
            prompt_tokens=100,
            completion_tokens=50,
            prompt_cache_hit_tokens=10,
            prompt_cache_miss_tokens=90,
            prompt_tokens_details=SimpleNamespace(audio_tokens=2, cached_tokens=20),
        )
        record_llm_token_usage(prompt_tokens=100, completion_tokens=50, model=label, usage=u)
        body = generate_latest().decode("utf-8")
        self.assertIn(f'dagents_llm_prompt_audio_tokens{{model="{safe}"}} 2.0', body)
        self.assertIn(f'dagents_llm_prompt_cached_tokens{{model="{safe}"}} 20.0', body)
        self.assertIn(f'dagents_llm_prompt_cache_hit_tokens{{model="{safe}"}} 10.0', body)
        self.assertIn(f'dagents_llm_prompt_cache_miss_tokens{{model="{safe}"}} 90.0', body)


if __name__ == "__main__":
    unittest.main()
