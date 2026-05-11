"""应用进程级 logging 初始化（与 uvicorn 日志并存）。"""

from __future__ import annotations

import logging
import os
import sys
from typing import Final

_LEVEL_ALIASES: Final[dict[str, int]] = {
    "CRITICAL": logging.CRITICAL,
    "ERROR": logging.ERROR,
    "WARNING": logging.WARNING,
    "WARN": logging.WARNING,
    "INFO": logging.INFO,
    "DEBUG": logging.DEBUG,
    "NOTSET": logging.NOTSET,
}


def resolve_log_level(name: str) -> int:
    """将配置字符串解析为 logging 数值级别。

    逻辑：
    1. 去空白并转大写后在别名表中查找；
    2. 未命中或空串时回退 **`logging.INFO`**，避免非法字符串导致启动失败。

    关键分支：
    - 兼容 **`WARN`** → **`WARNING`**。
    """

    key = (name or "").strip().upper()
    if not key:
        return logging.INFO
    return _LEVEL_ALIASES.get(key, logging.INFO)


def numeric_level_to_uvicorn(level: int) -> str:
    """将 logging 数值级别映射为 **`uvicorn.run(log_level=...)`** 接受的字符串。

    逻辑：标准五级一一对应；非常规数值回退 **`info`**，避免 uvicorn 拒参。
    """

    table = {
        logging.DEBUG: "debug",
        logging.INFO: "info",
        logging.WARNING: "warning",
        logging.ERROR: "error",
        logging.CRITICAL: "critical",
    }
    return table.get(level, "info")


def configure_app_logging(level_name: str | None = None, *, force: bool = True) -> int:
    """配置 root logging：stderr **`StreamHandler`** + 统一格式。

    逻辑：
    1. 解析 **`level_name`**，缺省时读环境变量 **`APP_LOG_LEVEL`**（再缺省为 INFO）；
    2. **`logging.basicConfig`** 设置 root level 与 handler；
    3. 非 DEBUG 时压低 **`httpx`** / **`httpcore`** 默认日志，避免 INFO 下噪声。

    关键分支：
    - **`force=True`**：重复初始化时仍覆盖，便于 **`load_env` 之后**以 `.env` 为准刷新配置。

    副作用：
    - 修改 root logger 与上述第三方 logger 的级别。

    Returns:
        解析后的数值级别，供 **`uvicorn.run(log_level=numeric_level_to_uvicorn(...))`** 使用。
    """

    raw = (
        level_name
        if level_name is not None
        else (os.environ.get("APP_LOG_LEVEL", "INFO") or "INFO")
    )
    level = resolve_log_level(raw)
    fmt = "%(asctime)s %(levelname)s [%(name)s] %(message)s"
    datefmt = "%Y-%m-%d %H:%M:%S"
    logging.basicConfig(level=level, format=fmt, datefmt=datefmt, stream=sys.stderr, force=force)
    # DEBUG 下保留 httpx 链路以便排障；否则避免每次请求刷屏。
    if level > logging.DEBUG:
        logging.getLogger("httpx").setLevel(logging.WARNING)
        logging.getLogger("httpcore").setLevel(logging.WARNING)
    else:
        logging.getLogger("httpx").setLevel(logging.DEBUG)
        logging.getLogger("httpcore").setLevel(logging.DEBUG)
    return level
