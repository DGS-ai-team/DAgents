from __future__ import annotations

import unittest

from app.cli.render import format_tool_call
from app.cli.tool_calls import normalize_tool_call_item


class NormalizeToolCallItemTests(unittest.TestCase):
    def test_openai_function_format(self) -> None:
        item = {
            "id": "call-1",
            "type": "function",
            "function": {
                "name": "bash_run",
                "arguments": '{"command": "ls -la"}',
            },
        }
        got = normalize_tool_call_item(item)
        self.assertEqual(got["id"], "call-1")
        self.assertEqual(got["name"], "bash_run")
        self.assertEqual(got["arguments"], {"command": "ls -la"})

    def test_flat_node_approval_format(self) -> None:
        item = {
            "id": "call-2",
            "name": "write_file",
            "arguments": {"path": "/tmp/a.txt", "content": "hi"},
        }
        got = normalize_tool_call_item(item)
        self.assertEqual(got["name"], "write_file")
        self.assertEqual(got["arguments"]["path"], "/tmp/a.txt")

    def test_format_tool_call_renders_openai_payload(self) -> None:
        update = format_tool_call(
            {
                "tool_calls": [
                    {
                        "id": "call-3",
                        "function": {
                            "name": "read_file",
                            "arguments": '{"path": "README.md"}',
                        },
                    }
                ]
            }
        )
        self.assertIsNotNone(update)
        assert update is not None
        self.assertIn("read_file", update.text)
        self.assertIn("call-3", update.text)


if __name__ == "__main__":
    unittest.main()
