"""Tests for case tool message resolution."""

import unittest

from manage.cases.jsonl import parse_jsonl_bytes
from manage.cases.models import CaseMessage
from manage.cases.tool_resolve import filter_unlinked_tool_messages, resolve_tool_name, build_tool_call_map


class ToolResolveTest(unittest.TestCase):
    def test_resolve_via_assistant_tool_calls(self):
        raws = [
            {
                "role": "assistant",
                "tool_calls": [
                    {
                        "id": "call-1",
                        "function": {"name": "read_file", "arguments": '{"path":"a.txt"}'},
                    }
                ],
            },
            {"role": "tool", "tool_call_id": "call-1", "content": "file body"},
        ]
        m = build_tool_call_map(raws)
        name = resolve_tool_name(raws[1], raws[:1], m)
        self.assertEqual(name, "read_file")

    def test_async_job_from_tool_name_line(self):
        raws = [
            {
                "role": "assistant",
                "tool_calls": [
                    {
                        "id": "async-job-job-1",
                        "function": {"name": "tool_callback", "arguments": "{}"},
                    }
                ],
            },
            {
                "role": "tool",
                "tool_call_id": "async-job-job-1",
                "content": (
                    "[ASYNC_TOOL_RESULT]\n"
                    "tool_name=bash_run\n"
                    "job_id=job-1\n"
                    "source_tool_call_id=call-bg-1\n"
                    "done"
                ),
            },
        ]
        m = build_tool_call_map(raws)
        name = resolve_tool_name(raws[1], raws[:1], m)
        self.assertEqual(name, "bash_run")

    def test_async_job_via_background_tool_chain(self):
        raws = [
            {
                "role": "assistant",
                "tool_calls": [
                    {
                        "id": "call-bg-1",
                        "function": {
                            "name": "bash_run",
                            "arguments": '{"command":"echo hi"}',
                        },
                    }
                ],
            },
            {
                "role": "tool",
                "tool_call_id": "call-bg-1",
                "content": "[BASH_RESULT] status=RUNNING job_id=job-1",
            },
            {
                "role": "tool",
                "tool_call_id": "async-job-job-1",
                "content": "[ASYNC_TOOL_RESULT]\njob_id=job-1\nresult ok",
            },
        ]
        m = build_tool_call_map(raws)
        name = resolve_tool_name(raws[2], raws[:2], m)
        self.assertEqual(name, "bash_run")

    def test_filter_drops_orphan_tool(self):
        messages = [
            CaseMessage(id="1", role="user", content="hi", raw={"role": "user", "content": "hi"}),
            CaseMessage(
                id="2",
                role="tool",
                content="orphan",
                raw={"role": "tool", "tool_call_id": "missing", "content": "orphan"},
            ),
            CaseMessage(
                id="3",
                role="assistant",
                content="",
                raw={
                    "role": "assistant",
                    "tool_calls": [
                        {"id": "call-1", "function": {"name": "grep_file", "arguments": "{}"}},
                    ],
                },
            ),
            CaseMessage(
                id="4",
                role="tool",
                content="ok",
                raw={"role": "tool", "tool_call_id": "call-1", "content": "ok"},
            ),
        ]
        filtered = filter_unlinked_tool_messages(messages)
        self.assertEqual(len(filtered), 3)
        self.assertEqual([m.id for m in filtered], ["1", "3", "4"])

    def test_parse_jsonl_filters_orphans(self):
        raw = (
            b'{"recorded_at":"t1","message":{"role":"user","content":"start"}}\n'
            b'{"recorded_at":"t2","message":{"role":"tool","tool_call_id":"ghost","content":"x"}}\n'
            b'{"recorded_at":"t3","message":{"role":"assistant","tool_calls":[{"id":"c1","function":{"name":"bash_run","arguments":"{}"}}]}}\n'
            b'{"recorded_at":"t4","message":{"role":"tool","tool_call_id":"c1","content":"ok"}}\n'
        )
        messages = parse_jsonl_bytes(raw)
        self.assertEqual(len(messages), 3)
        self.assertEqual(messages[-1].content, "ok")


if __name__ == "__main__":
    unittest.main()
