from __future__ import annotations

import logging
import os
import sys
from pathlib import Path

SESSION_CONTROLLER_LOGGER_NAME = "app.cli.session_controller"
API_CLIENT_LOGGER_NAME = "app.cli.api_client"
_DEFAULT_LOG_FILENAME = "session_controller.log"

_CONFIGURED = False


def resolve_log_dir() -> Path:
    """解析 CLI 日志目录。

    逻辑：
    1. `DAGENTS_LOG_DIR` 优先；
    2. 否则 `DAGENTS_HOME/logs` 或 PyInstaller 可执行文件旁 `logs/`；
    3. 开发态落仓库根 `logs/`。

    关键边界：目录不存在时由调用方 `mkdir`。
    """
    override = os.getenv("DAGENTS_LOG_DIR", "").strip()
    if override:
        return Path(override).expanduser().resolve()
    home = os.getenv("DAGENTS_HOME", "").strip()
    if home:
        return Path(home).expanduser().resolve() / "logs"
    if getattr(sys, "frozen", False):
        return Path(sys.executable).resolve().parent / "logs"
    return Path(__file__).resolve().parents[2] / "logs"


def get_session_controller_logger() -> logging.Logger:
    """获取 SessionController 专用 Logger，并幂等挂载文件 Handler。

    逻辑：
    1. 创建 `logs/` 与 `session_controller.log`；
    2. 按 `DAGENTS_LOG_LEVEL`（默认 INFO）设置级别；
    3. 进程内已配置则直接返回，避免重复 Handler。

    副作用：写入日志文件；不向 root logger 传播（避免污染全局）。

    异常说明：目录创建失败向上抛出。
    """
    global _CONFIGURED
    logger = logging.getLogger(SESSION_CONTROLLER_LOGGER_NAME)
    if _CONFIGURED:
        return logger

    log_dir = resolve_log_dir()
    log_dir.mkdir(parents=True, exist_ok=True)
    log_path = log_dir / _DEFAULT_LOG_FILENAME

    handler = logging.FileHandler(log_path, encoding="utf-8")
    handler.setFormatter(
        logging.Formatter(
            "%(asctime)s %(levelname)s [%(name)s] %(message)s",
            datefmt="%Y-%m-%d %H:%M:%S",
        )
    )
    level_name = os.getenv("DAGENTS_LOG_LEVEL", "INFO").strip().upper()
    level = getattr(logging, level_name, logging.INFO)
    logger.setLevel(level)
    handler.setLevel(level)
    logger.addHandler(handler)
    logger.propagate = False

    api_logger = logging.getLogger(API_CLIENT_LOGGER_NAME)
    api_logger.setLevel(level)
    api_logger.addHandler(handler)
    api_logger.propagate = False

    _CONFIGURED = True
    logger.info("session file logging enabled path=%s level=%s", log_path, level_name)
    return logger


def get_api_client_logger() -> logging.Logger:
    """获取 API 客户端 Logger（与 SessionController 共用 session_controller.log）。"""
    get_session_controller_logger()
    return logging.getLogger(API_CLIENT_LOGGER_NAME)
