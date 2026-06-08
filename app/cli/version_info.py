from __future__ import annotations

import getpass
import os

# 与 harness API `FastAPI(..., version=...)` 对齐，供 CLI 展示。
CLI_VERSION = "0.2.3"


def get_cli_username() -> str:
    """解析当前系统用户名，供 TUI 欢迎区展示。

    逻辑：
    1. 优先 `getpass.getuser()`；
    2. 失败时回退环境变量 `USER` / `USERNAME`；
    3. 仍无则返回 `guest`。
    """
    try:
        name = getpass.getuser().strip()
        if name:
            return name
    except OSError:
        pass
    for key in ("USER", "USERNAME"):
        value = os.environ.get(key, "").strip()
        if value:
            return value
    return "guest"


def get_cli_version() -> str:
    """返回 CLI 展示用版本号字符串。"""
    return CLI_VERSION
