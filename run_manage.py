"""
Manage 统一控制面启动入口。

用法（在 DAgents 根目录）:
  python run_manage.py

环境变量见 manage/README.md。
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import uvicorn

from manage.config import ManageSettings


def _root_dir() -> Path:
    if getattr(sys, "frozen", False) and hasattr(sys, "_MEIPASS"):
        return Path(getattr(sys, "_MEIPASS"))
    return Path(__file__).resolve().parent


_ROOT = _root_dir()
sys.path.insert(0, str(_ROOT))


def _load_app():
    module_path = _ROOT / "manage" / "manage_app.py"
    spec = importlib.util.spec_from_file_location("manage_app_entry", module_path)
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
    settings = ManageSettings.from_env()
    uvicorn.run(app, host=settings.host, port=settings.port, reload=False)


if __name__ == "__main__":
    main()
