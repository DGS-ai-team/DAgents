from __future__ import annotations

import sys
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.config.host_snapshot import HostSnapshot  # noqa: E402
from app.harness.tools.bash import bash_run  # noqa: E402

_SNAP_LINUX_NONROOT = HostSnapshot(
    captured_at_unix=0.0,
    os_kind="linux",
    sys_platform="linux",
    platform_system="Linux",
    platform_release="1",
    machine="x86_64",
    login_name="tester",
    effective_uid=1000,
    effective_gid=1000,
)
_SNAP_LINUX_ROOT = HostSnapshot(
    captured_at_unix=0.0,
    os_kind="linux",
    sys_platform="linux",
    platform_system="Linux",
    platform_release="1",
    machine="x86_64",
    login_name="root",
    effective_uid=0,
    effective_gid=0,
)
_SNAP_WINDOWS = HostSnapshot(
    captured_at_unix=0.0,
    os_kind="windows",
    sys_platform="win32",
    platform_system="Windows",
    platform_release="10",
    machine="AMD64",
    login_name="user",
    effective_uid=None,
    effective_gid=None,
)


class BashPrivilegeGuardTestCase(unittest.TestCase):
    def test_non_root_blocks_su_login_c_bash(self) -> None:
        with patch("app.harness.tools.bash.get_host_snapshot", return_value=_SNAP_LINUX_NONROOT):
            out = bash_run(
                "su - alice -c id",
                shell_type="bash",
                timeout_seconds=1,
            )
        self.assertTrue(out.startswith("ERROR:"))
        self.assertIn("非 root", out)
        self.assertIn("su - <user> -c", out)

    def test_non_root_blocks_sudo_without_noninteractive_flag(self) -> None:
        with patch("app.harness.tools.bash.get_host_snapshot", return_value=_SNAP_LINUX_NONROOT):
            out = bash_run(
                "sudo -u alice id",
                shell_type="bash",
                timeout_seconds=1,
            )
        self.assertTrue(out.startswith("ERROR:"))
        self.assertIn("sudo", out)
        self.assertIn("-n", out)

    def test_non_root_allows_sudo_with_n_mocked(self) -> None:
        with (
            patch("app.harness.tools.bash.get_host_snapshot", return_value=_SNAP_LINUX_NONROOT),
            patch("app.harness.tools.bash._run_bash_command") as mock_run,
        ):
            mock_run.return_value = Mock(stdout="0", stderr="", returncode=0)
            out = bash_run(
                "sudo -n -u alice id",
                shell_type="bash",
                timeout_seconds=1,
            )
        self.assertIn("[BASH_RESULT]", out)
        mock_run.assert_called_once()

    def test_non_root_allows_sudo_long_noninteractive_mocked(self) -> None:
        with (
            patch("app.harness.tools.bash.get_host_snapshot", return_value=_SNAP_LINUX_NONROOT),
            patch("app.harness.tools.bash._run_bash_command") as mock_run,
        ):
            mock_run.return_value = Mock(stdout="", stderr="", returncode=0)
            out = bash_run(
                "sudo --non-interactive -u bob true",
                shell_type="bash",
                timeout_seconds=1,
            )
        self.assertIn("[BASH_RESULT]", out)
        mock_run.assert_called_once()

    def test_non_root_blocks_sudoedit_without_noninteractive(self) -> None:
        with patch("app.harness.tools.bash.get_host_snapshot", return_value=_SNAP_LINUX_NONROOT):
            out = bash_run(
                "sudoedit /etc/foo",
                shell_type="bash",
                timeout_seconds=1,
            )
        self.assertTrue(out.startswith("ERROR:"))

    def test_non_root_blocks_after_statement_separator(self) -> None:
        with patch("app.harness.tools.bash.get_host_snapshot", return_value=_SNAP_LINUX_NONROOT):
            out = bash_run(
                "true && su - bob -c whoami",
                shell_type="bash",
                timeout_seconds=1,
            )
        self.assertTrue(out.startswith("ERROR:"))

    def test_root_allows_pattern_without_running(self) -> None:
        """root 不拦截；此处仍 mock subprocess 以免真实执行 su。"""
        with (
            patch("app.harness.tools.bash.get_host_snapshot", return_value=_SNAP_LINUX_ROOT),
            patch("app.harness.tools.bash._run_bash_command") as mock_run,
        ):
            mock_run.return_value = Mock(
                stdout="ok",
                stderr="",
                returncode=0,
            )
            out = bash_run(
                "su - alice -c id",
                shell_type="bash",
                timeout_seconds=1,
            )
        self.assertIn("[BASH_RESULT]", out)
        mock_run.assert_called_once()

    def test_powershell_shell_not_checked_by_su_guard(self) -> None:
        """非 bash 路径不走 su 片段解析（避免误伤 Windows/Linux 下的 powershell）。"""
        with (
            patch("app.harness.tools.bash.get_host_snapshot", return_value=_SNAP_WINDOWS),
            patch("app.harness.tools.bash._run_powershell_command") as mock_run,
        ):
            mock_run.return_value = Mock(
                stdout="x",
                stderr="",
                returncode=0,
            )
            out = bash_run(
                "Write-Host 'su - u -c noop'",
                shell_type="powershell",
                timeout_seconds=1,
            )
        self.assertIn("[BASH_RESULT]", out)
        mock_run.assert_called_once()


if __name__ == "__main__":
    unittest.main()
