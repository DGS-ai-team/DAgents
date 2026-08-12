"""工作组共享工作区（Manage 侧落盘）。"""

from __future__ import annotations

from pathlib import Path

_README = """# Workgroup workspace

Reserved for Supervisor / group-level assets on Manage.

Member workspaces still live on each Home Node
(`.runtime/workgroup-workers/<workgroup_id>/<member_id>/`).

Supervisor FS tools are not attached yet — this directory is a reservation only.
"""


def materialize_workgroup_workspace(root: Path, workgroup_id: str) -> Path:
    """Create `{root}/{workgroup_id}/` with `data/` + README; return absolute path."""
    wid = (workgroup_id or "").strip()
    if not wid:
        raise ValueError("workgroup_id required")
    wg_root = (root / wid).resolve()
    (wg_root / "data").mkdir(parents=True, exist_ok=True)
    readme = wg_root / "README.md"
    if not readme.exists():
        readme.write_text(_README, encoding="utf-8")
    return wg_root
