"""Validate the version metadata required for a DAgents release.

This check intentionally runs without project dependencies so it can be used as
an early gate on a release PR and again from the tag release workflow.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
VERSION_FILE = ROOT / "node/internal/version/version.go"
CHANGELOG_FILE = ROOT / "CHANGELOG.md"
README_FILE = ROOT / "README.md"
HANDBOOK_FILE = ROOT / "docs/handbook/README.md"
ROADMAP_FILE = ROOT / "docs/roadmap.md"
SEMVER = re.compile(r"^\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?$")
VERSION_CONST = re.compile(r'const\s+Version\s*=\s*"([^"]+)"')


def read_version() -> str:
    match = VERSION_CONST.search(VERSION_FILE.read_text(encoding="utf-8"))
    if not match:
        raise ValueError(f"cannot find Version constant in {VERSION_FILE}")
    return match.group(1)


def validate(expected: str | None = None) -> list[str]:
    errors: list[str] = []
    try:
        version = read_version()
    except (OSError, ValueError) as error:
        return [str(error)]

    if not SEMVER.fullmatch(version):
        errors.append(f"version.go contains invalid SemVer: {version!r}")
    if expected and version != expected:
        errors.append(f"expected version {expected}, found {version}")

    files = {
        "CHANGELOG.md": CHANGELOG_FILE,
        "README.md": README_FILE,
        "docs/handbook/README.md": HANDBOOK_FILE,
        "docs/roadmap.md": ROADMAP_FILE,
    }
    contents: dict[str, str] = {}
    for label, path in files.items():
        try:
            contents[label] = path.read_text(encoding="utf-8")
        except OSError as error:
            errors.append(f"cannot read {label}: {error}")

    if "CHANGELOG.md" in contents and f"## [{version}]" not in contents["CHANGELOG.md"]:
        errors.append(f"CHANGELOG.md has no section for {version}")
    if "README.md" in contents:
        if f"release-v{version}-green" not in contents["README.md"]:
            errors.append(f"README.md has no release badge for v{version}")
        if f"当前版本为 **v{version}**" not in contents["README.md"]:
            errors.append(f"README.md has no current-version marker for v{version}")
    if "docs/handbook/README.md" in contents and f"当前发布 **v{version}**" not in contents["docs/handbook/README.md"]:
        errors.append(f"docs/handbook/README.md has no current-release marker for v{version}")
    if "docs/roadmap.md" in contents and f"v{version}" not in contents["docs/roadmap.md"]:
        errors.append(f"docs/roadmap.md has no release marker for v{version}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", help="also require this exact version")
    args = parser.parse_args()

    errors = validate(args.version)
    if errors:
        for error in errors:
            print(f"release metadata error: {error}", file=sys.stderr)
        return 1

    print(f"release metadata is consistent: v{read_version()}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
