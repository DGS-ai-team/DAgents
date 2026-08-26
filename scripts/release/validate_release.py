"""Validate the version metadata required for a DAgents release.

This check intentionally runs without project dependencies so it can be used as
an early gate on a release PR and again from the tag release workflow.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
VERSION_FILE = ROOT / "VERSION"
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
CARGO_VERSION = re.compile(
    r'^\[package\]\s*\nname\s*=\s*"dagents-shell"\s*\nversion\s*=\s*"([^"]+)"',
    re.MULTILINE,
)
CARGO_LOCK_VERSION = re.compile(
    r'^\[\[package\]\]\s*\nname\s*=\s*"dagents-shell"\s*\nversion\s*=\s*"([^"]+)"',
    re.MULTILINE,
)
SEMVER = re.compile(r"^\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?$")


def read_version() -> str:
    version = VERSION_FILE.read_text(encoding="utf-8").strip()
    if not version:
        raise ValueError(f"{VERSION_FILE} is empty")
    return version


def _read_json_version(path: Path) -> str:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"{path} is not a JSON object")
    version = payload.get("version")
    if not isinstance(version, str) or not version:
        raise ValueError(f"{path} has no package version")
    return version


def _read_lock_version(path: Path) -> tuple[str, str]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"{path} is not a JSON object")
    packages = payload.get("packages")
    if not isinstance(packages, dict):
        raise ValueError(f"{path} has no packages object")
    root = packages.get("")
    if not isinstance(root, dict) or not isinstance(root.get("version"), str):
        raise ValueError(f"{path} has no packages[''] version")
    return str(payload.get("version", "")), root["version"]


def validate(expected: str | None = None) -> list[str]:
    errors: list[str] = []
    try:
        version = read_version()
    except (OSError, ValueError) as error:
        return [str(error)]

    if not SEMVER.fullmatch(version):
        errors.append(f"VERSION contains invalid SemVer: {version!r}")
    if expected and version != expected:
        errors.append(f"expected version {expected}, found {version}")

    for path in PACKAGE_FILES:
        try:
            package_version = _read_json_version(path)
        except (OSError, ValueError, json.JSONDecodeError) as error:
            errors.append(f"cannot read package version from {path.relative_to(ROOT)}: {error}")
            continue
        if package_version != version:
            errors.append(f"{path.relative_to(ROOT)} has version {package_version}, expected {version}")

    for path in APP_VERSION_FILES:
        try:
            app_version = _read_json_version(path)
        except (OSError, ValueError, json.JSONDecodeError) as error:
            errors.append(f"cannot read app version from {path.relative_to(ROOT)}: {error}")
            continue
        if app_version != version:
            errors.append(f"{path.relative_to(ROOT)} has version {app_version}, expected {version}")

    for path in PACKAGE_LOCK_FILES:
        try:
            lock_version, root_version = _read_lock_version(path)
        except (OSError, ValueError, json.JSONDecodeError) as error:
            errors.append(f"cannot read lock version from {path.relative_to(ROOT)}: {error}")
            continue
        if lock_version != version or root_version != version:
            errors.append(
                f"{path.relative_to(ROOT)} has top-level/root versions "
                f"{lock_version!r}/{root_version!r}, expected {version!r}"
            )

    for path, pattern in ((CARGO_FILE, CARGO_VERSION), (CARGO_LOCK_FILE, CARGO_LOCK_VERSION)):
        try:
            cargo_match = pattern.search(path.read_text(encoding="utf-8"))
        except OSError as error:
            errors.append(f"cannot read {path.relative_to(ROOT)}: {error}")
        else:
            if not cargo_match:
                errors.append(f"cannot find dagents-shell package version in {path.relative_to(ROOT)}")
            elif cargo_match.group(1) != version:
                errors.append(
                    f"{path.relative_to(ROOT)} has version {cargo_match.group(1)}, expected {version}"
                )

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
