"""`dagents serve` 守护进程与钩子单测。"""

from __future__ import annotations

import os
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from app.cli import daemon
from app.cli.main import build_parser


def test_add_serve_arguments_parsed_by_main() -> None:
    """serve 子命令应识别后台/停止/状态 flags。"""
    parser = build_parser()
    args = parser.parse_args(["serve", "--stop"])
    assert args.command == "serve"
    assert args.stop is True
    assert args.foreground is False

    args_fg = parser.parse_args(["serve", "--foreground"])
    assert args_fg.foreground is True


def test_read_pid_missing_and_invalid(tmp_path: Path) -> None:
    """无文件或非法内容时返回 None。"""
    pid_path = tmp_path / "dagents-api.pid"
    assert daemon._read_pid(pid_path) is None

    pid_path.write_text("not-a-pid\n", encoding="utf-8")
    assert daemon._read_pid(pid_path) is None

    pid_path.write_text("4242\n", encoding="utf-8")
    assert daemon._read_pid(pid_path) == 4242


def test_run_hook_dir_missing_is_noop(tmp_path: Path) -> None:
    """钩子目录不存在时直接成功。"""
    assert daemon._run_hook_dir(tmp_path, daemon._STARTUP_DIR, "startup") == 0


def test_run_hook_dir_runs_scripts_in_order(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """应按文件名顺序执行钩子并传递非零退出码。"""
    hook_root = tmp_path / ".runtime" / "scripts" / "serve" / "startup.d"
    hook_root.mkdir(parents=True)
    (hook_root / "02-b.sh").write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
    (hook_root / "01-a.sh").write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")

    calls: list[str] = []

    def fake_run(_home: Path, script: Path) -> int:
        calls.append(script.name)
        return 0

    monkeypatch.setattr(daemon, "_run_hook_script", fake_run)
    rc = daemon._run_hook_dir(tmp_path, daemon._STARTUP_DIR, "startup")
    assert rc == 0
    assert calls == ["01-a.sh", "02-b.sh"]


def test_run_hook_dir_stops_on_failure(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """任一脚本失败应中止后续钩子。"""
    hook_root = tmp_path / ".runtime" / "scripts" / "serve" / "startup.d"
    hook_root.mkdir(parents=True)
    (hook_root / "01-fail.sh").write_text("#!/bin/sh\nexit 1\n", encoding="utf-8")

    monkeypatch.setattr(daemon, "_run_hook_script", lambda _home, _script: 1)
    assert daemon._run_hook_dir(tmp_path, daemon._STARTUP_DIR, "startup") == 1


def test_print_serve_status_no_pid(tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    """无 PID 文件时 status 返回 1。"""
    pid_path = tmp_path / "dagents-api.pid"
    rc = daemon._print_serve_status(tmp_path, pid_path)
    assert rc == 1
    assert "not running" in capsys.readouterr().out


def test_run_serve_command_status_delegates(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """--status 分支应调用状态打印。"""
    monkeypatch.setattr(daemon, "_print_serve_status", lambda _home, _pid: 0)
    rc = daemon.run_serve_command(
        tmp_path,
        binary_stem="dagents-api",
        script_name="run_agent_api.py",
        extra_args=[],
        foreground=False,
        stop=False,
        status=True,
        no_hooks=False,
        no_wait=True,
    )
    assert rc == 0


def test_run_serve_command_rejects_duplicate_start(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    """已有存活 PID 时拒绝再次后台启动。"""
    pid_path = tmp_path / "dagents-api.pid"
    pid_path.write_text("99\n", encoding="utf-8")
    monkeypatch.setattr(daemon, "_read_pid", lambda _path: 99)
    monkeypatch.setattr(daemon, "_pid_alive", lambda _pid: True)

    rc = daemon.run_serve_command(
        tmp_path,
        binary_stem="dagents-api",
        script_name="run_agent_api.py",
        extra_args=[],
        foreground=False,
        stop=False,
        status=False,
        no_hooks=True,
        no_wait=True,
    )
    assert rc == 1
    assert "already running" in capsys.readouterr().err


def test_run_serve_command_background_writes_pid(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """后台启动应写入 PID 并跳过 health 等待（--no-wait）。"""
    monkeypatch.setattr(daemon, "_read_pid", lambda _path: None)
    monkeypatch.setattr(daemon, "_pid_alive", lambda _pid: False)
    monkeypatch.setattr(daemon, "_start_serve_daemon", lambda *_a, **_k: 12345)

    rc = daemon.run_serve_command(
        tmp_path,
        binary_stem="dagents-api",
        script_name="run_agent_api.py",
        extra_args=[],
        foreground=False,
        stop=False,
        status=False,
        no_hooks=True,
        no_wait=True,
    )
    assert rc == 0
    assert (tmp_path / "dagents-api.pid").read_text(encoding="utf-8").strip() == "12345"


def test_stop_removes_stale_pid(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """陈旧 PID 文件应在 stop 时清理。"""
    pid_path = tmp_path / "dagents-api.pid"
    pid_path.write_text("1\n", encoding="utf-8")
    monkeypatch.setattr(daemon, "_read_pid", lambda _path: 1)
    monkeypatch.setattr(daemon, "_pid_alive", lambda _pid: False)

    rc = daemon.run_serve_command(
        tmp_path,
        binary_stem="dagents-api",
        script_name="run_agent_api.py",
        extra_args=[],
        foreground=False,
        stop=True,
        status=False,
        no_hooks=True,
        no_wait=True,
    )
    assert rc == 0
    assert not pid_path.exists()


def test_wait_for_health_success(monkeypatch: pytest.MonkeyPatch) -> None:
    """健康检查在 200 时应立即成功。"""
    fake_resp = MagicMock()
    fake_resp.status = 200
    fake_resp.__enter__ = lambda self: self
    fake_resp.__exit__ = lambda *args: None
    monkeypatch.setattr(
        daemon.urllib.request,
        "urlopen",
        lambda *_a, **_k: fake_resp,
    )
    assert daemon._wait_for_health("http://127.0.0.1:8000", timeout_s=1.0) is True
