"""`app.config` 单测：环境解析与 `.env` 加载入口。

覆盖清单 §1（`get_settings` / `load_env` / `resolve_runtime_root`）的 P1/P2 要点；
与 `Settings.load` 强耦合的 `AGENT_ID` 写盘逻辑通过临时目录 + 固定 `AGENT_ID` 规避随机 UUID。
"""

from __future__ import annotations

import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

from app.config.env import load_env, resolve_runtime_root
from app.config.settings import Settings, get_settings


def _reset_settings_singleton() -> None:
    """将进程内 `get_settings` 缓存置空，避免用例间串状态。"""
    import app.config.settings as sm

    sm._settings = None


class ResolveRuntimeRootTests(unittest.TestCase):
    """`resolve_runtime_root`：源码树与 frozen 分支的解析语义。"""

    def test_source_mode_points_at_repo_root(self) -> None:
        """源码模式下应解析到含 `app/config/env.py` 的仓库根（绝对路径）。"""
        root = resolve_runtime_root()
        self.assertTrue(root.is_absolute())
        self.assertTrue((root / "app" / "config" / "env.py").is_file())

    def test_frozen_mode_uses_executable_parent(self) -> None:
        """PyInstaller frozen 时以可执行文件父目录为根，避免误用源码树路径。"""
        fake_exe = Path("/opt/bundle/myagent")
        with patch("sys.frozen", True, create=True):
            with patch("sys.executable", str(fake_exe)):
                root = resolve_runtime_root()
        self.assertEqual(root, fake_exe.parent)


class LoadEnvTests(unittest.TestCase):
    """`load_env`：存在 `.env` 时委托 `python-dotenv`，不存在则跳过。"""

    @patch("dotenv.load_dotenv")
    def test_load_env_invokes_dotenv_when_file_exists(self, mock_load: MagicMock) -> None:
        """根目录存在 `.env` 时应调用 `load_dotenv(..., override=False)`，以保留进程已有变量。"""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / ".env").write_text("DUMMY_FROM_TEST=1\n", encoding="utf-8")
            load_env(project_root=root)
        mock_load.assert_called_once()
        _args, kwargs = mock_load.call_args
        self.assertIs(kwargs.get("override"), False)

    @patch("dotenv.load_dotenv")
    def test_load_env_skips_dotenv_when_file_missing(self, mock_load: MagicMock) -> None:
        """无 `.env` 文件时不应触发 `load_dotenv`（仅打印提示）。"""
        with tempfile.TemporaryDirectory() as tmp:
            load_env(project_root=Path(tmp))
        mock_load.assert_not_called()


class SettingsLoadTests(unittest.TestCase):
    """`Settings.load` / `get_settings`：默认值与环境覆盖。"""

    def tearDown(self) -> None:
        """每条用例结束后丢弃单例，防止污染同进程其它用例。"""
        _reset_settings_singleton()

    def _base_agent_env(self, tmp: Path) -> dict[str, str]:
        """固定 `AGENT_ID`，避免 `_resolve_agent_id` 走随机 UUID 分支。"""
        return {
            "AGENT_ID": "unit-test-agent-id",
        }

    def test_llm_timeout_default_when_unset_or_blank(self) -> None:
        """`LLM_TIMEOUT` 未设置或空串时回落到 120。"""
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp)
            with patch("app.config.runtime_layout.resolve_runtime_root", return_value=p):
                with patch.dict(os.environ, {**self._base_agent_env(p), "LLM_TIMEOUT": ""}, clear=False):
                    _reset_settings_singleton()
                    s = Settings.load()
            self.assertEqual(s.llm_timeout, 120)

    def test_llm_timeout_override_from_env(self) -> None:
        """显式 `LLM_TIMEOUT` 应覆盖默认整数。"""
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp)
            with patch("app.config.runtime_layout.resolve_runtime_root", return_value=p):
                with patch.dict(
                    os.environ,
                    {**self._base_agent_env(p), "LLM_TIMEOUT": "77"},
                    clear=False,
                ):
                    _reset_settings_singleton()
                    s = Settings.load()
            self.assertEqual(s.llm_timeout, 77)

    def test_metrics_enabled_false_when_env_says_false(self) -> None:
        """布尔环境变量支持常见假值（与 `_env_bool` 一致）。"""
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp)
            with patch("app.config.runtime_layout.resolve_runtime_root", return_value=p):
                with patch.dict(
                    os.environ,
                    {**self._base_agent_env(p), "METRICS_ENABLED": "false"},
                    clear=False,
                ):
                    _reset_settings_singleton()
                    s = Settings.load()
            self.assertFalse(s.metrics_enabled)

    def test_api_cors_csv_parses_dedup_and_order(self) -> None:
        """`API_CORS_ALLOW_ORIGINS` 逗号分隔、去空白、去重且保序。"""
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp)
            raw = "http://a.example, http://b.example ,http://a.example"
            with patch("app.config.runtime_layout.resolve_runtime_root", return_value=p):
                with patch.dict(
                    os.environ,
                    {**self._base_agent_env(p), "API_CORS_ALLOW_ORIGINS": raw},
                    clear=False,
                ):
                    _reset_settings_singleton()
                    s = Settings.load()
            self.assertEqual(
                s.api_cors_allow_origins,
                ["http://a.example", "http://b.example"],
            )

    def test_agent_session_store_enabled_default_true(self) -> None:
        """未定义 `AGENT_SESSION_STORE_ENABLED` 时默认开启 SQLite 会话持久化（路径由 `runtime_layout` 固定）。"""
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp)
            with patch("app.config.runtime_layout.resolve_runtime_root", return_value=p):
                with patch.dict(os.environ, self._base_agent_env(p), clear=False):
                    os.environ.pop("AGENT_SESSION_STORE_ENABLED", None)
                    _reset_settings_singleton()
                    s = Settings.load()
            self.assertTrue(s.agent_session_store_enabled)

    def test_agent_session_store_enabled_false_from_env(self) -> None:
        """显式 `AGENT_SESSION_STORE_ENABLED=false` 时关闭持久化（`AgentService` 使用纯内存）。"""
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp)
            with patch("app.config.runtime_layout.resolve_runtime_root", return_value=p):
                with patch.dict(
                    os.environ,
                    {**self._base_agent_env(p), "AGENT_SESSION_STORE_ENABLED": "false"},
                    clear=False,
                ):
                    _reset_settings_singleton()
                    s = Settings.load()
            self.assertFalse(s.agent_session_store_enabled)

    def test_get_settings_singleton_and_reload(self) -> None:
        """`get_settings` 默认缓存；`reload=True` 时随环境变化重建。"""
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp)
            with patch("app.config.runtime_layout.resolve_runtime_root", return_value=p):
                with patch.dict(
                    os.environ,
                    {**self._base_agent_env(p), "LLM_MODEL": "first-model"},
                    clear=False,
                ):
                    _reset_settings_singleton()
                    a = get_settings()
                    self.assertEqual(a.llm_model, "first-model")
                with patch.dict(
                    os.environ,
                    {**self._base_agent_env(p), "LLM_MODEL": "second-model"},
                    clear=False,
                ):
                    b = get_settings(reload=True)
                    self.assertEqual(b.llm_model, "second-model")


if __name__ == "__main__":
    unittest.main()
