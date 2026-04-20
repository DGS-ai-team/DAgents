"""从仓库根目录 `.env` 加载环境变量（需安装 `python-dotenv`）。"""

from __future__ import annotations

from pathlib import Path


def load_env(project_root: Path | None = None) -> None:
    """
    加载 **`project_root/.env`** 到 `os.environ`（不覆盖已存在的环境变量）。

    `project_root` 默认为：本文件所在 **`app/config/`** 的上两级（仓库根目录）。
    """
    try:
        from dotenv import load_dotenv
    except ImportError:
        return

    root = project_root or Path(__file__).resolve().parent.parent.parent
    env_file = root / ".env"
    if env_file.is_file():
        load_dotenv(env_file, override=False)
