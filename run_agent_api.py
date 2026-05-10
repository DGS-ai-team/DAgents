"""
FastAPI 入口：

用法（在 DAgents 根目录）:
  python run_agent_api.py
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

import uvicorn

_ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(_ROOT))

from app.config.env import load_env, resolve_runtime_root  # noqa: E402
from app.config.host_snapshot import capture_host_snapshot_at_startup  # noqa: E402
from app.config.startup_checks import emit_linux_cross_user_shell_startup_hints  # noqa: E402
from app.harness.api.app import app  # noqa: E402


def _resolve_api_host_port() -> tuple[str, int]:
    """解析 API 监听 host/port（统一从 `.env`/环境变量读取）。

    逻辑：
    1. 读取 `API_HOST` 与 `API_PORT`；
    2. 未配置时回退默认 `127.0.0.1:8000`；
    3. `API_PORT` 非法时回退默认值，避免启动时崩溃。

    关键分支/边界：
    - `API_HOST` 为空字符串时视为未配置；
    - `API_PORT` 非数字或越界时统一回退 8000。

    与外部交互：
    - 仅读取进程环境变量。

    异常说明：
    - 内部兜底，不向外抛端口解析异常。

    副作用说明：
    - 无。
    """

    host = (os.getenv("API_HOST", "127.0.0.1") or "127.0.0.1").strip() or "127.0.0.1"
    raw_port = (os.getenv("API_PORT", "8000") or "8000").strip()
    try:
        port = int(raw_port)
        if not (1 <= port <= 65535):
            port = 8000
        else:
            pass
    except ValueError:
        port = 8000
    return host, port


def main() -> None:
    load_env(resolve_runtime_root())
    capture_host_snapshot_at_startup()
    emit_linux_cross_user_shell_startup_hints()
    host, port = _resolve_api_host_port()
    # 直接传入 app 对象，避免在打包产物中依赖字符串动态导入。
    uvicorn.run(app, host=host, port=port, reload=False)


if __name__ == "__main__":
    main()

