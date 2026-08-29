"""Require immutable commit pins for third-party GitHub Actions."""

from __future__ import annotations

import re
import sys
from pathlib import Path


USES_RE = re.compile(r"^\s*uses:\s*([^\s#]+)")
SHA_RE = re.compile(r"^[0-9a-fA-F]{40}$")


def main() -> int:
    failures: list[str] = []
    for workflow in sorted(Path(".github/workflows").glob("*.yml")):
        for line_number, line in enumerate(workflow.read_text(encoding="utf-8").splitlines(), 1):
            match = USES_RE.match(line)
            if not match:
                continue
            reference = match.group(1)
            if reference.startswith("./"):
                continue
            if "@" not in reference or not SHA_RE.fullmatch(reference.rsplit("@", 1)[1]):
                failures.append(f"{workflow}:{line_number}: action must use a 40-character commit SHA: {reference}")
    if failures:
        print("Unpinned GitHub Actions:", file=sys.stderr)
        print("\n".join(failures), file=sys.stderr)
        return 1
    print("all third-party GitHub Actions use immutable commit pins")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
