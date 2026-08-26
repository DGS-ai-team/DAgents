"""Prepare the repository metadata for a DAgents release.

Example:
    python scripts/release/prepare_release.py 0.9.17 \
      --summary "- Improve release validation\n- Add artifact checksums"
"""

from __future__ import annotations

import argparse
import re
import sys
from datetime import date
from pathlib import Path

try:
    from .validate_release import SEMVER
except ImportError:  # Support direct execution: python scripts/release/prepare_release.py
    from validate_release import SEMVER


ROOT = Path(__file__).resolve().parents[2]
VERSION_FILE = ROOT / "node/internal/version/version.go"
CHANGELOG_FILE = ROOT / "CHANGELOG.md"
README_FILE = ROOT / "README.md"
HANDBOOK_FILE = ROOT / "docs/handbook/README.md"
ROADMAP_FILE = ROOT / "docs/roadmap.md"
VERSION_CONST = re.compile(r'(const\s+Version\s*=\s*")[^"]+("\s*)')
ROADMAP_RELEASE = re.compile(r"准备发布 v\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?")


def replace_once(text: str, pattern: str | re.Pattern[str], replacement: str, label: str, source: Path) -> str:
    updated, count = re.subn(pattern, replacement, text, count=1)
    if count != 1:
        raise ValueError(f"{label}: expected exactly one match in {source}, found {count}")
    return updated


def prepare(version: str, summary: str, release_date: str) -> None:
    if not SEMVER.fullmatch(version):
        raise ValueError(f"invalid SemVer: {version!r}")
    if not summary.strip():
        raise ValueError("release summary must not be empty")
    try:
        date.fromisoformat(release_date)
    except ValueError as error:
        raise ValueError(f"invalid release date: {release_date!r}; expected YYYY-MM-DD") from error

    version_text = VERSION_FILE.read_text(encoding="utf-8")
    changelog = CHANGELOG_FILE.read_text(encoding="utf-8")
    if f"## [{version}]" in changelog:
        raise ValueError(f"CHANGELOG.md already contains a section for {version}")
    if not re.search(r"## \[Unreleased\]\s*\n", changelog):
        raise ValueError("CHANGELOG.md has no [Unreleased] section")
    readme = README_FILE.read_text(encoding="utf-8")
    handbook = HANDBOOK_FILE.read_text(encoding="utf-8")
    roadmap = ROADMAP_FILE.read_text(encoding="utf-8")

    version_updated = replace_once(
        version_text,
        VERSION_CONST,
        rf'\g<1>{version}\g<2>',
        "version constant",
        VERSION_FILE,
    )
    readme_updated = replace_once(
        readme,
        r"release-v\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?-green",
        f"release-v{version}-green",
        "README release badge",
        README_FILE,
    )
    readme_updated = replace_once(
        readme_updated,
        r"当前版本为 \*\*v\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?\*\*",
        f"当前版本为 **v{version}**",
        "README current version",
        README_FILE,
    )
    handbook_updated = replace_once(
        handbook,
        r"当前发布 \*\*v\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?\*\*",
        f"当前发布 **v{version}**",
        "handbook current release",
        HANDBOOK_FILE,
    )

    roadmap_updated, roadmap_count = ROADMAP_RELEASE.subn(
        f"准备发布 v{version}", roadmap, count=1
    )

    changelog_entry = f"## [{version}] - {release_date}\n\n{summary.strip()}\n\n"
    unreleased = re.compile(r"(## \[Unreleased\]\s*\n)")
    changelog_updated, changelog_count = unreleased.subn(
        rf"\g<1>\n{changelog_entry}", changelog, count=1
    )
    if changelog_count != 1:
        raise ValueError("CHANGELOG.md has no [Unreleased] section")

    VERSION_FILE.write_text(version_updated, encoding="utf-8")
    README_FILE.write_text(readme_updated, encoding="utf-8")
    HANDBOOK_FILE.write_text(handbook_updated, encoding="utf-8")
    if roadmap_count:
        ROADMAP_FILE.write_text(roadmap_updated, encoding="utf-8")
    CHANGELOG_FILE.write_text(changelog_updated, encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="release version without the leading v")
    parser.add_argument("--summary", required=True, help="Markdown summary for CHANGELOG.md")
    parser.add_argument("--date", default=date.today().isoformat(), help="release date (YYYY-MM-DD)")
    args = parser.parse_args()

    try:
        prepare(args.version, args.summary, args.date)
    except (OSError, ValueError) as error:
        print(f"release preparation failed: {error}", file=sys.stderr)
        return 1
    print(f"prepared release metadata for v{args.version}; run validate_release.py next")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
