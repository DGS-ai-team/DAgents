"""工具结果保留策略测试。"""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from app.harness.tools.result_policy import (
    DISPLAY_CONTENT_MAX_CHARS,
    MODEL_CONTENT_MAX_CHARS,
    package_tool_result,
)


class ToolResultPolicyTests(unittest.TestCase):
    def test_sensitive_output_persists_only_redacted_copy(self) -> None:
        """命中敏感信息时，模型、展示与落盘引用都不应保留明文密钥。"""
        with tempfile.TemporaryDirectory() as tmp:
            with patch("app.harness.tools.result_policy.resolve_runtime_root", return_value=Path(tmp)):
                result = package_tool_result(
                    tool_name="bash_run",
                    content="token=secret-token password=hunter2 sk-abcdefghijklmnopqrstuvwxyz",
                )
                persisted = Path(result.raw_ref).read_text(encoding="utf-8")

        self.assertTrue(result.sensitive_filtered)
        self.assertTrue(result.raw_ref)
        self.assertNotIn("secret-token", result.model_content)
        self.assertNotIn("hunter2", result.display_content)
        self.assertNotIn("sk-abcdefghijklmnopqrstuvwxyz", result.display_content)
        self.assertIn("token=<redacted>", persisted)
        self.assertIn("password=<redacted>", persisted)
        self.assertIn("<redacted-openai-key>", persisted)
        self.assertNotIn("secret-token", persisted)
        self.assertNotIn("hunter2", persisted)
        self.assertNotIn("sk-abcdefghijklmnopqrstuvwxyz", persisted)

    def test_long_non_sensitive_output_keeps_raw_reference_for_ui_debugging(self) -> None:
        """超长但非敏感输出可落盘完整原文，同时模型与展示内容按各自上限裁剪。"""
        raw_text = "A" * (DISPLAY_CONTENT_MAX_CHARS + 100)
        with tempfile.TemporaryDirectory() as tmp:
            with patch("app.harness.tools.result_policy.resolve_runtime_root", return_value=Path(tmp)):
                result = package_tool_result(tool_name="search_file", content=raw_text)
                persisted = Path(result.raw_ref).read_text(encoding="utf-8")

        self.assertTrue(result.truncated)
        self.assertFalse(result.sensitive_filtered)
        self.assertTrue(result.raw_ref)
        self.assertLess(len(result.model_content), len(raw_text))
        self.assertIn("[TRUNCATED]", result.model_content)
        self.assertIn("[TRUNCATED]", result.display_content)
        self.assertEqual(persisted, raw_text)
        self.assertLessEqual(MODEL_CONTENT_MAX_CHARS, DISPLAY_CONTENT_MAX_CHARS)


if __name__ == "__main__":
    unittest.main()
