"""
从仓库根目录启动，避免手动配置 PYTHONPATH。

用法（在 DAgents 根目录）:
  python run_agent.py
"""
from __future__ import annotations

import sys
from pathlib import Path

_ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(_ROOT))

from app.harness.cli.main import main  # noqa: E402


if __name__ == "__main__":
    main(_ROOT)
