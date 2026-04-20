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

from app.config.env import load_env  # noqa: E402


def main() -> None:
    load_env(_ROOT)
    uvicorn.run("app.harness.api.app:app", host="0.0.0.0", port=8000, reload=False)


if __name__ == "__main__":
    main()

