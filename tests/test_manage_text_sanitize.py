"""Tests for Unicode sanitization helpers."""

import unittest

from manage.platform.text import sanitize_json_value, sanitize_unicode


class TextSanitizeTest(unittest.TestCase):
    def test_removes_lone_surrogate(self) -> None:
        bad = "hello\udc80world"
        cleaned = sanitize_unicode(bad)
        self.assertNotIn("\udc80", cleaned)
        cleaned.encode("utf-8")  # must not raise

    def test_sanitize_nested_dict(self) -> None:
        payload = {"content": "x\udc80y", "nested": {"note": "a\ud800b"}}
        cleaned = sanitize_json_value(payload)
        self.assertIsInstance(cleaned["content"], str)
        cleaned["content"].encode("utf-8")
        cleaned["nested"]["note"].encode("utf-8")


if __name__ == "__main__":
    unittest.main()
