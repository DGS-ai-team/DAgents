"""
Register Center 启动入口。

用法（在 DAgents 根目录）:
  python run_register_center.py
"""

from __future__ import annotations

import importlib.util
import os
import sys
from pathlib import Path

import uvicorn

_ROOT = Path(__file__).resolve().parent
_REGISTER_CENTER_DIR = _ROOT / "register_center"
sys.path.insert(0, str(_REGISTER_CENTER_DIR))


def _load_app():
    """按文件路径加载 register_center 目录内的 FastAPI app。

    逻辑：
    1. 构造 `register_center/rc_app.py` 的绝对路径；
    2. 基于 `importlib` 创建模块规格并执行模块代码；
    3. 从模块对象中读取 `app` 并返回。

    关键分支/边界：
    - 若模块规格或 loader 为空，立即抛出 `RuntimeError`；
    - 若模块内不存在 `app`，立即抛出 `RuntimeError`；
    - 通过 `sys.path` 注入目录，保证 `rc_app.py` 内对同目录模块的导入可解析。

    与外部交互：
    - 读取本地文件系统中的 Python 模块文件。

    异常说明：
    - 不吞异常；导入/执行失败会直接向上抛出，便于启动阶段快速失败。

    副作用说明：
    - 会在 `sys.modules` 中注册临时模块名 `register_center_rc_app`。
    """

    module_path = _REGISTER_CENTER_DIR / "rc_app.py"
    spec = importlib.util.spec_from_file_location("register_center_rc_app", module_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"无法加载模块规格: {module_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    app_obj = getattr(module, "app", None)
    if app_obj is None:
        raise RuntimeError(f"模块未暴露 app: {module_path}")
    return app_obj


app = _load_app()


def main() -> None:
    """启动 Register Center 的 uvicorn 服务。

    逻辑：
    1. 读取环境变量中的 host/port（未设置时使用默认值）；
    2. 调用 `uvicorn.run` 启动 FastAPI 应用；
    3. 将进程阻塞在事件循环，直至外部中断。

    关键分支/边界：
    - `REGISTER_CENTER_PORT` 非法时回退到默认 `8010`；
    - `REGISTER_CENTER_HOST` 为空时回退到默认 `0.0.0.0`。

    与外部交互：
    - 打开 HTTP 监听端口，对外提供 REST API。

    异常说明：
    - 不主动吞异常，启动失败会直接向上抛出。

    副作用说明：
    - 绑定本地网络端口并进入长期运行状态。
    """

    host = os.getenv("REGISTER_CENTER_HOST", "0.0.0.0").strip() or "0.0.0.0"
    raw_port = os.getenv("REGISTER_CENTER_PORT", "8010").strip()
    try:
        port = int(raw_port)
    except ValueError:
        port = 8010

    uvicorn.run(app, host=host, port=port, reload=False)


if __name__ == "__main__":
    main()
