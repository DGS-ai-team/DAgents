"""`dagents serve` 守护进程与钩子单测（unittest，CI discover 可发现）。"""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

from app.cli import daemon
from app.cli.main import build_parser


class CliDaemonTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmpdir.cleanup)
        self.tmp_path = Path(self._tmpdir.name)

    def test_add_serve_arguments_parsed_by_main(self) -> None:
        """serve 子命令应识别后台/停止/状态 flags。"""
        parser = build_parser()
        args = parser.parse_args(["serve", "--stop"])
        self.assertEqual(args.command, "serve")
        self.assertTrue(args.stop)
        self.assertFalse(args.foreground)

        args_fg = parser.parse_args(["serve", "--foreground"])
        self.assertTrue(args_fg.foreground)

    def test_read_pid_missing_and_invalid(self) -> None:
        """无文件或非法内容时返回 None。"""
        pid_path = self.tmp_path / "dagents-api.pid"
        self.assertIsNone(daemon._read_pid(pid_path))

        pid_path.write_text("not-a-pid\n", encoding="utf-8")
        self.assertIsNone(daemon._read_pid(pid_path))

        pid_path.write_text("4242\n", encoding="utf-8")
        self.assertEqual(daemon._read_pid(pid_path), 4242)

    def test_run_hook_dir_missing_is_noop(self) -> None:
        """钩子目录不存在时直接成功。"""
        self.assertEqual(daemon._run_hook_dir(self.tmp_path, daemon._STARTUP_DIR, "startup"), 0)

    def test_run_hook_dir_runs_scripts_in_order(self) -> None:
        """应按文件名顺序执行钩子并传递非零退出码。"""
        hook_root = self.tmp_path / ".runtime" / "scripts" / "serve" / "startup.d"
        hook_root.mkdir(parents=True)
        (hook_root / "02-b.sh").write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
        (hook_root / "01-a.sh").write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")

        calls: list[str] = []

        def fake_run(_home: Path, script: Path) -> int:
            calls.append(script.name)
            return 0

        with patch.object(daemon, "_run_hook_script", fake_run):
            rc = daemon._run_hook_dir(self.tmp_path, daemon._STARTUP_DIR, "startup")
        self.assertEqual(rc, 0)
        self.assertEqual(calls, ["01-a.sh", "02-b.sh"])

    def test_run_hook_dir_stops_on_failure(self) -> None:
        """任一脚本失败应中止后续钩子。"""
        hook_root = self.tmp_path / ".runtime" / "scripts" / "serve" / "startup.d"
        hook_root.mkdir(parents=True)
        (hook_root / "01-fail.sh").write_text("#!/bin/sh\nexit 1\n", encoding="utf-8")

        with patch.object(daemon, "_run_hook_script", lambda _home, _script: 1):
            rc = daemon._run_hook_dir(self.tmp_path, daemon._STARTUP_DIR, "startup")
        self.assertEqual(rc, 1)

    def test_print_serve_status_no_pid(self) -> None:
        """无 PID 文件时 status 返回 1。"""
        pid_path = self.tmp_path / "dagents-api.pid"
        with patch("sys.stdout", new_callable=MagicMock) as mock_out:
            rc = daemon._print_serve_status(self.tmp_path, pid_path)
        self.assertEqual(rc, 1)
        self.assertIn("not running", mock_out.write.call_args_list[0].args[0])

    def test_run_serve_command_status_delegates(self) -> None:
        """--status 分支应调用状态打印。"""
        with patch.object(daemon, "_print_serve_status", return_value=0):
            rc = daemon.run_serve_command(
                self.tmp_path,
                binary_stem="dagents-api",
                script_name="run_agent_api.py",
                extra_args=[],
                foreground=False,
                stop=False,
                status=True,
                no_hooks=False,
                no_wait=True,
            )
        self.assertEqual(rc, 0)

    def test_run_serve_command_rejects_duplicate_start(self) -> None:
        """已有存活 PID 时拒绝再次后台启动。"""
        pid_path = self.tmp_path / "dagents-api.pid"
        pid_path.write_text("99\n", encoding="utf-8")
        with (
            patch.object(daemon, "_read_pid", return_value=99),
            patch.object(daemon, "_pid_alive", return_value=True),
            patch("sys.stderr", new_callable=MagicMock) as mock_err,
        ):
            rc = daemon.run_serve_command(
                self.tmp_path,
                binary_stem="dagents-api",
                script_name="run_agent_api.py",
                extra_args=[],
                foreground=False,
                stop=False,
                status=False,
                no_hooks=True,
                no_wait=True,
            )
        self.assertEqual(rc, 1)
        self.assertIn("already running", mock_err.write.call_args_list[0].args[0])

    def test_run_serve_command_background_writes_pid(self) -> None:
        """后台启动应写入 PID 并跳过 health 等待（--no-wait）。"""
        with (
            patch.object(daemon, "_read_pid", return_value=None),
            patch.object(daemon, "_pid_alive", return_value=False),
            patch.object(daemon, "_start_serve_daemon", return_value=12345),
        ):
            rc = daemon.run_serve_command(
                self.tmp_path,
                binary_stem="dagents-api",
                script_name="run_agent_api.py",
                extra_args=[],
                foreground=False,
                stop=False,
                status=False,
                no_hooks=True,
                no_wait=True,
            )
        self.assertEqual(rc, 0)
        self.assertEqual(
            (self.tmp_path / "dagents-api.pid").read_text(encoding="utf-8").strip(),
            "12345",
        )

    def test_stop_removes_stale_pid(self) -> None:
        """陈旧 PID 文件应在 stop 时清理。"""
        pid_path = self.tmp_path / "dagents-api.pid"
        pid_path.write_text("1\n", encoding="utf-8")
        with (
            patch.object(daemon, "_read_pid", return_value=1),
            patch.object(daemon, "_pid_alive", return_value=False),
        ):
            rc = daemon.run_serve_command(
                self.tmp_path,
                binary_stem="dagents-api",
                script_name="run_agent_api.py",
                extra_args=[],
                foreground=False,
                stop=True,
                status=False,
                no_hooks=True,
                no_wait=True,
            )
        self.assertEqual(rc, 0)
        self.assertFalse(pid_path.exists())

    def test_wait_for_health_success(self) -> None:
        """健康检查在 200 时应立即成功。"""
        fake_resp = MagicMock()
        fake_resp.status = 200
        fake_resp.__enter__ = lambda self: self
        fake_resp.__exit__ = lambda *args: None
        with patch.object(daemon.urllib.request, "urlopen", return_value=fake_resp):
            self.assertTrue(daemon._wait_for_health("http://127.0.0.1:8000", timeout_s=1.0))


if __name__ == "__main__":
    unittest.main()
