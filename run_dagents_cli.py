from __future__ import annotations

import sys
from pathlib import Path

_ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(_ROOT))

from app.cli.main import main  # noqa: E402


if __name__ == "__main__":
    raise SystemExit(main())
