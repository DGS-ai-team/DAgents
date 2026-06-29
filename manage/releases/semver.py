"""轻量 semver 比较（x.y.z，可选 -prerelease）。"""

from __future__ import annotations


def _parse_version(raw: str) -> tuple[tuple[int, ...], str]:
    text = str(raw or "").strip().lstrip("vV")
    if not text:
        return (), ""
    main, _, tail = text.partition("-")
    parts: list[int] = []
    for piece in main.split("."):
        piece = piece.strip()
        if not piece.isdigit():
            break
        parts.append(int(piece))
    return tuple(parts), tail.lower()


def compare_versions(left: str, right: str) -> int:
    """Return -1 if left<right, 0 if equal, 1 if left>right."""
    lp, lpre = _parse_version(left)
    rp, rpre = _parse_version(right)
    for i in range(max(len(lp), len(rp))):
        lv = lp[i] if i < len(lp) else 0
        rv = rp[i] if i < len(rp) else 0
        if lv < rv:
            return -1
        if lv > rv:
            return 1
    if lpre == rpre:
        return 0
    if not lpre:
        return 1
    if not rpre:
        return -1
    if lpre < rpre:
        return -1
    if lpre > rpre:
        return 1
    return 0


def upgrade_available(current: str, latest: str) -> bool:
    return compare_versions(current, latest) < 0
