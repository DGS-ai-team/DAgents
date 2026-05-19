"""`app.harness.tools.fs` 单测：`search_replace` / `read_file` / `search_file`。"""

from __future__ import annotations

import os
import tempfile
import unittest
from pathlib import Path

from app.harness.tools.fs import read_file, search_file, search_replace


class FsToolsTests(unittest.TestCase):
    """在临时 `FS_ROOT` 下验证文件工具。"""

    def setUp(self) -> None:
        self._tmpdir = tempfile.TemporaryDirectory()
        self._root = Path(self._tmpdir.name).resolve()
        self._prev_fs_root = os.environ.get("FS_ROOT")
        os.environ["FS_ROOT"] = str(self._root)

    def tearDown(self) -> None:
        if self._prev_fs_root is None:
            os.environ.pop("FS_ROOT", None)
        else:
            os.environ["FS_ROOT"] = self._prev_fs_root
        self._tmpdir.cleanup()

    def test_search_replace_single_match(self) -> None:
        """恰好一处匹配时应成功并写盘。"""
        p = self._root / "a.txt"
        p.write_text("hello world\n", encoding="utf-8")
        out = search_replace(path="a.txt", old_string="world", new_string="DAgents")
        self.assertIn("成功: 是", out)
        self.assertEqual(p.read_text(encoding="utf-8"), "hello DAgents\n")

    def test_search_replace_rejects_ambiguous(self) -> None:
        """多处匹配且 replace_all=false 时应失败。"""
        p = self._root / "b.txt"
        p.write_text("x x\n", encoding="utf-8")
        out = search_replace(path="b.txt", old_string="x", new_string="y")
        self.assertIn("成功: 否", out)
        self.assertIn("2 处", out)

    def test_read_file_has_no_line_prefix(self) -> None:
        """read_file 正文不应带 `数字>` 行号前缀。"""
        p = self._root / "c.py"
        p.write_text("def foo():\n    return 1\n", encoding="utf-8")
        out = read_file(path="c.py")
        self.assertIn("def foo():", out)
        self.assertNotRegex(out.split("---", 1)[-1], r"(?m)^\d+>")

    def test_read_file_line_window_and_next_offset(self) -> None:
        """line_offset/line_limit 分页，并给出 next_line_offset。"""
        p = self._root / "lines.txt"
        p.write_text("a\nb\nc\nd\ne\n", encoding="utf-8")
        out = read_file(path="lines.txt", line_offset=2, line_limit=2)
        body = out.split("---", 1)[-1].strip()
        self.assertEqual(body, "b\nc")
        self.assertIn("本页行区间: 2-3 / 5", out)
        self.assertIn("next_line_offset: 4", out)
        self.assertIn("后方是否还有未读取行: 是", out)

    def test_search_file_navigation_and_merge(self) -> None:
        """search_file 应含 read_file 建议、next_index_offset，且相邻命中合并上下文。"""
        p = self._root / "hits.txt"
        lines = ["head\n"] + ["TODO item\n"] * 3 + ["tail\n"]
        p.write_text("".join(lines), encoding="utf-8")
        out = search_file(path="hits.txt", pattern="TODO", count_limit=5)
        self.assertIn("next_index_offset: 无", out)
        self.assertIn("建议 read_file:", out)
        self.assertIn("line_offset=", out)
        # 三处命中相邻，合并后上下文里的 TODO 不应重复出现三次独立块
        self.assertEqual(out.count("上下文:"), 1)
        self.assertNotRegex(out.split("---", 1)[-1], r"(?m)^\d+>")


if __name__ == "__main__":
    unittest.main()
