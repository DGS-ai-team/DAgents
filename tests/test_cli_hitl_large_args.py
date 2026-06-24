"""超大 write_file 参数下 Python HITL 路径回归（见 scripts/test_python_hitl_large_args.py）。"""

from __future__ import annotations

import asyncio
import unittest

from scripts.test_python_hitl_large_args import (
    GO_CLIENT_SSE_LINE_LIMIT,
    build_hitl_required_data,
    build_write_file_args,
    run_size,
    stage_hitl_expand,
    stage_sse_encode,
    stage_sse_parse,
)


class HitlLargeArgsTests(unittest.TestCase):
    def test_small_payload_roundtrip(self) -> None:
        args, raw = build_write_file_args(128)
        data = build_hitl_required_data(args, raw, "write_file")
        encode_ok, block = stage_sse_encode(data)
        self.assertTrue(encode_ok.ok)
        parse_ok, event = stage_sse_parse(block)
        self.assertTrue(parse_ok.ok)
        assert event is not None
        expand_ok, approval = stage_hitl_expand(event.data)
        self.assertTrue(expand_ok.ok)
        assert approval is not None

    def test_one_mib_sse_line_exceeds_go_client_limit(self) -> None:
        args, raw = build_write_file_args(900_000)
        data = build_hitl_required_data(args, raw, "write_file")
        encode_ok, block = stage_sse_encode(data)
        self.assertTrue(encode_ok.ok)
        self.assertGreater(len(block), GO_CLIENT_SSE_LINE_LIMIT)


class HitlLargeArgsAsyncTests(unittest.IsolatedAsyncioTestCase):
    async def test_100k_all_stages(self) -> None:
        report = await run_size(100_000, slow_ms=30_000.0, tool="write_file")
        self.assertTrue(report.all_ok, report.stages)

    async def test_1m_session_and_stream(self) -> None:
        report = await run_size(1_048_576, slow_ms=30_000.0, tool="write_file")
        failed = [s for s in report.stages if not s.ok and s.name != "rich_syntax"]
        self.assertFalse(failed, failed)


if __name__ == "__main__":
    unittest.main()
