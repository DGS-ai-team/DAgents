#!/usr/bin/env python3
"""一次性迁移：旧版路径迁入 **`<repo>/.runtime/`**。

1. 仓库根 **`history/*.jsonl`** → **`<repo>/.runtime/history/`**；
2. 仓库根 **`skills/`**（若存在且目标尚无同名目录）→ **`<repo>/.runtime/skills/`**。

用法（仓库根）::

    PYTHONPATH=. python scripts/migrate_runtime_layout.py
"""

from __future__ import annotations

import shutil
import sys
from pathlib import Path


def main() -> int:
    """搬迁 JSONL 与 skills 目录。"""
    root = Path(__file__).resolve().parents[1]
    if str(root) not in sys.path:
        sys.path.insert(0, str(root))

    from app.config.env import load_env, resolve_runtime_root

    load_env(resolve_runtime_root())
    repo = resolve_runtime_root()
    rt = repo / ".runtime"
    hist_dst = rt / "history"
    skills_dst = rt / "skills"

    hist_dst.mkdir(parents=True, exist_ok=True)

    src_hist = repo / "history"
    moved_jsonl = 0
    if src_hist.is_dir() and src_hist.resolve() != hist_dst.resolve():
        for path in sorted(src_hist.glob("*.jsonl")):
            if not path.is_file():
                continue
            dest = hist_dst / path.name
            if dest.exists():
                print(f"[migrate] skip exists: {dest}", flush=True)
                continue
            shutil.move(str(path), str(dest))
            print(f"[migrate] moved {path} -> {dest}", flush=True)
            moved_jsonl += 1
        try:
            if src_hist.is_dir() and not any(src_hist.iterdir()):
                src_hist.rmdir()
                print(f"[migrate] removed empty {src_hist}", flush=True)
        except OSError:
            pass
    else:
        print(f"[migrate] history: no legacy dir or same as target ({src_hist})", flush=True)

    src_skills = repo / "skills"
    if src_skills.is_dir() and src_skills.resolve() != skills_dst.resolve():
        if skills_dst.exists():
            if any(skills_dst.iterdir()):
                print(f"[migrate] skills: skip, target not empty {skills_dst}", flush=True)
            else:
                skills_dst.rmdir()
                shutil.move(str(src_skills), str(skills_dst))
                print(f"[migrate] moved {src_skills} -> {skills_dst}", flush=True)
        else:
            shutil.move(str(src_skills), str(skills_dst))
            print(f"[migrate] moved {src_skills} -> {skills_dst}", flush=True)
    else:
        print(f"[migrate] skills: skip ({src_skills} missing or already under .runtime)", flush=True)

    print(f"[migrate] done jsonl_moved={moved_jsonl}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
