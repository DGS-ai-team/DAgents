"""占位冒烟：保证 `unittest discover` 能解析核心 Python 包。"""

from __future__ import annotations

import unittest


class WorkspaceImportSmokeTest(unittest.TestCase):
    def test_import_cli_main(self) -> None:
        from app.cli.main import build_parser

        parser = build_parser()
        self.assertEqual(parser.prog, "dagents")


if __name__ == "__main__":
    unittest.main()
