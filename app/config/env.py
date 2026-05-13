"""从仓库根目录 `.env` 加载环境变量（需安装 `python-dotenv`）。"""

from __future__ import annotations

import sys
from pathlib import Path


def resolve_runtime_root() -> Path:
    """解析运行根目录（源码模式/打包模式统一入口）。

    逻辑：
    1. PyInstaller 等 frozen 模式下，返回可执行文件所在目录；
    2. 源码模式下，返回仓库根目录（`app/config/` 上两级）；
    3. 返回值统一为绝对路径。
    """

    if getattr(sys, "frozen", False):
        return Path(sys.executable).resolve().parent
    return Path(__file__).resolve().parent.parent.parent


def load_env(project_root: Path | None = None) -> None:
    """
    加载 **`project_root/.env`** 到 `os.environ`（不覆盖已存在的环境变量）。

    `project_root` 默认为：
    - 源码模式：仓库根目录；
    - 打包模式：可执行文件所在目录。
    """
    try:
        from dotenv import load_dotenv
    except ImportError:
        return

    root = project_root or resolve_runtime_root()
    env_file = root / ".env"
    exists = env_file.is_file()
    print(
        f"[env] resolve root={root} env_file={env_file} exists={exists}",
        flush=True,
    )
    if exists:
        load_dotenv(env_file, override=False)
        print(f"[env] loaded .env from {env_file}", flush=True)
    else:
        print("[env] .env not found, skip loading", flush=True)
