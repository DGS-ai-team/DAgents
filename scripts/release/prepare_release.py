"""Prepare the repository metadata for a DAgents release.

Example:
    python scripts/release/prepare_release.py 0.9.17 \
      --summary "- Improve release validation\n- Add artifact checksums"
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import tempfile
from datetime import date
from pathlib import Path

try:
    from .validate_release import SEMVER
except ImportError:  # Support direct execution: python scripts/release/prepare_release.py
    from validate_release import SEMVER


ROOT = Path(__file__).resolve().parents[2]
CANONICAL_VERSION_FILE = ROOT / "VERSION"
CHANGELOG_FILE = ROOT / "CHANGELOG.md"
README_FILE = ROOT / "README.md"
HANDBOOK_FILE = ROOT / "docs/handbook/README.md"
ROADMAP_FILE = ROOT / "docs/roadmap.md"
PACKAGE_FILES = (
    ROOT / "node/webui/frontend/package.json",
    ROOT / "manage/console/frontend/package.json",
    ROOT / "desktop/tray-tauri/package.json",
)
APP_VERSION_FILES = (ROOT / "desktop/tray-tauri/src-tauri/tauri.conf.json",)
PACKAGE_LOCK_FILES = tuple(path.with_name("package-lock.json") for path in PACKAGE_FILES)
CARGO_FILE = ROOT / "desktop/tray-tauri/src-tauri/Cargo.toml"
CARGO_LOCK_FILE = ROOT / "desktop/tray-tauri/src-tauri/Cargo.lock"
ROADMAP_RELEASE = re.compile(r"准备发布 v\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?")


def replace_once(text: str, pattern: str | re.Pattern[str], replacement: str, label: str, source: Path) -> str:
    updated, count = re.subn(pattern, replacement, text, count=1)
    if count != 1:
        raise ValueError(f"{label}: expected exactly one match in {source}, found {count}")
    return updated


def atomic_write(path: Path, text: str) -> None:
    """Replace one metadata file atomically after all release edits validate."""

    with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, delete=False) as tmp:
        tmp.write(text)
        temp_name = Path(tmp.name)
    temp_name.replace(path)


def update_package_json(text: str, version: str, path: Path) -> str:
    payload = json.loads(text)
    if not isinstance(payload, dict):
        raise ValueError(f"{path}: package metadata is not a JSON object")
    if not isinstance(payload.get("version"), str):
        raise ValueError(f"{path}: package version is missing")
    payload["version"] = version
    return json.dumps(payload, ensure_ascii=False, indent=2) + "\n"


def update_package_lock(text: str, version: str, path: Path) -> str:
    updated = replace_once(
        text,
        r'("version"\s*:\s*")[^"]+("\s*,\s*"lockfileVersion")',
        rf"\g<1>{version}\g<2>",
        "lockfile version",
        path,
    )
    return replace_once(
        updated,
        r'("":\s*\{\s*"name"\s*:\s*"[^"]+",\s*"version"\s*:\s*")[^"]+("\s*,)',
        rf"\g<1>{version}\g<2>",
        "lock root package version",
        path,
    )


def update_cargo_package(text: str, version: str, path: Path) -> str:
    section = r"\[\[package\]\]" if path.name == "Cargo.lock" else r"\[package\]"
    return replace_once(
        text,
        rf'(?m)(^{section}\s*\nname\s*=\s*"dagents-shell"\s*\nversion\s*=\s*")[^"]+("\s*)',
        rf"\g<1>{version}\g<2>",
        "Cargo package version",
        path,
    )


def prepare(version: str, summary: str, release_date: str) -> None:
    if not SEMVER.fullmatch(version):
        raise ValueError(f"invalid SemVer: {version!r}")
    if not summary.strip():
        raise ValueError("release summary must not be empty")
    try:
        date.fromisoformat(release_date)
    except ValueError as error:
        raise ValueError(f"invalid release date: {release_date!r}; expected YYYY-MM-DD") from error

    canonical_version = CANONICAL_VERSION_FILE.read_text(encoding="utf-8")
    changelog = CHANGELOG_FILE.read_text(encoding="utf-8")
    if f"## [{version}]" in changelog:
        raise ValueError(f"CHANGELOG.md already contains a section for {version}")
    if not re.search(r"## \[Unreleased\]\s*\n", changelog):
        raise ValueError("CHANGELOG.md has no [Unreleased] section")
    readme = README_FILE.read_text(encoding="utf-8")
    handbook = HANDBOOK_FILE.read_text(encoding="utf-8")
    roadmap = ROADMAP_FILE.read_text(encoding="utf-8")

    metadata_updates: dict[Path, str] = {
        CANONICAL_VERSION_FILE: f"{version}\n",
    }
    for path in PACKAGE_FILES:
        metadata_updates[path] = update_package_json(path.read_text(encoding="utf-8"), version, path)
    for path in APP_VERSION_FILES:
        metadata_updates[path] = update_package_json(path.read_text(encoding="utf-8"), version, path)
    for path in PACKAGE_LOCK_FILES:
        metadata_updates[path] = update_package_lock(path.read_text(encoding="utf-8"), version, path)
    metadata_updates[CARGO_FILE] = update_cargo_package(
        CARGO_FILE.read_text(encoding="utf-8"), version, CARGO_FILE
    )
    metadata_updates[CARGO_LOCK_FILE] = update_cargo_package(
        CARGO_LOCK_FILE.read_text(encoding="utf-8"), version, CARGO_LOCK_FILE
    )

    if not canonical_version.strip():
        raise ValueError(f"{CANONICAL_VERSION_FILE} is empty")
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

    for path, content in metadata_updates.items():
        atomic_write(path, content)
    atomic_write(README_FILE, readme_updated)
    atomic_write(HANDBOOK_FILE, handbook_updated)
    if roadmap_count:
        atomic_write(ROADMAP_FILE, roadmap_updated)
    atomic_write(CHANGELOG_FILE, changelog_updated)


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
