"""
FastAPI 入口：

用法（在 DAgents 根目录）:
  python run_agent_api.py
"""

from __future__ import annotations

import sys
from pathlib import Path

import uvicorn

_ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(_ROOT))

from app.config.env import load_env, resolve_runtime_root  # noqa: E402
from app.config.host_snapshot import capture_host_snapshot_at_startup  # noqa: E402
from app.config.logging_setup import configure_app_logging, numeric_level_to_uvicorn  # noqa: E402
from app.config.settings import get_settings  # noqa: E402
from app.config.startup_checks import emit_linux_cross_user_shell_startup_hints  # noqa: E402
from app.harness.api.app import app  # noqa: E402


def main() -> None:
    env = resolve_runtime_root()
    load_env(resolve_runtime_root())
    # `.env` 写入 os.environ 后再加载 Settings，并与 uvicorn 对齐日志级别。
    get_settings(reload=True)
    s = get_settings()
    level = configure_app_logging(s.app_log_level)
    capture_host_snapshot_at_startup()
    emit_linux_cross_user_shell_startup_hints()
    # 直接传入 app 对象，避免在打包产物中依赖字符串动态导入。
    uvicorn.run(
        app,
        host=s.api_host,
        port=s.api_port,
        reload=False,
        log_level=numeric_level_to_uvicorn(level),
    )


if __name__ == "__main__":
    main()

