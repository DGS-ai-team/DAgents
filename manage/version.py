"""Project version lookup shared by the Manage runtime and release tooling."""

from __future__ import annotations

from pathlib import Path


def read_version() -> str:
    """Read the canonical repository version without importing build tooling."""

    version_file = Path(__file__).resolve().parents[1] / "VERSION"
    return version_file.read_text(encoding="utf-8").strip()
