from __future__ import annotations

import unittest

from app.cli.render import (
    format_compact_token_count,
    format_inline_usage,
    format_input_strip_usage,
    parse_usage_round,
    parse_usage_strip,
)


class FormatCompactTokenCountTests(unittest.TestCase):
    def test_unknown_returns_empty(self) -> None:
        self.assertEqual(format_compact_token_count(None), "")

    def test_small_values_use_commas(self) -> None:
        self.assertEqual(format_compact_token_count(0), "ctx 0")
        self.assertEqual(format_compact_token_count(1234), "ctx 1,234")

    def test_large_values_use_k_suffix(self) -> None:
        self.assertEqual(format_compact_token_count(10000), "ctx 10.0k")
        self.assertEqual(format_compact_token_count(12345), "ctx 12.3k")

    def test_parse_usage_strip(self) -> None:
        snap = parse_usage_strip(
            {
                "prompt_tokens": 100,
                "completion_tokens": 20,
                "prompt_cache_hit_tokens": 80,
            }
        )
        self.assertTrue(snap.has_data)
        self.assertEqual(format_input_strip_usage(snap), "↑100 ↓20 · hit 80 (80%)")

    def test_parse_usage_round_and_inline(self) -> None:
        snap = parse_usage_round(
            {
                "round_prompt_tokens": 1200,
                "round_completion_tokens": 80,
                "round_reasoning_tokens": 42,
            }
        )
        self.assertTrue(snap.has_data)
        self.assertEqual(format_inline_usage(snap), " · ↑1,200 ↓80 · think 42")


if __name__ == "__main__":
    unittest.main()
