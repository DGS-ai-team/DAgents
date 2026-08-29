"""Check licenses of installed npm packages against the project policy.

This intentionally inspects installed package metadata rather than trying to
infer licenses from package names. It covers the two shipped Vue applications;
the lockfiles remain the source of dependency versions and npm audit covers
known security advisories.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path


ALLOWED_LICENSES = {
    "0BSD",
    "Apache-2.0",
    "BSD-2-Clause",
    "BSD-3-Clause",
    "BlueOak-1.0.0",
    "ISC",
    "MIT",
    "MPL-2.0 OR Apache-2.0",
    "(MPL-2.0 OR Apache-2.0)",
}


def package_roots(node_modules: Path) -> list[Path]:
    roots: list[Path] = []
    for package_json in node_modules.rglob("package.json"):
        package_dir = package_json.parent
        if package_dir.parent.name == "node_modules":
            roots.append(package_json)
        elif package_dir.parent.parent.name == "node_modules" and package_dir.parent.name.startswith("@"):
            roots.append(package_json)
    return roots


def license_value(package_json: Path) -> str | None:
    metadata = json.loads(package_json.read_text(encoding="utf-8"))
    value = metadata.get("license")
    if isinstance(value, str):
        return value
    licenses = metadata.get("licenses")
    if isinstance(licenses, list):
        values: list[str] = []
        for item in licenses:
            if not isinstance(item, dict) or not isinstance(item.get("type"), str):
                return None
            values.append(item["type"])
        if values:
            return " OR ".join(values)
    return None


def main() -> int:
    roots = [
        Path("node/webui/frontend/node_modules"),
        Path("manage/console/frontend/node_modules"),
    ]
    failures: list[str] = []
    inspected: set[tuple[str, str]] = set()
    for node_modules in roots:
        if not node_modules.is_dir():
            failures.append(f"missing installed dependencies: {node_modules}")
            continue
        for package_json in package_roots(node_modules):
            try:
                metadata = json.loads(package_json.read_text(encoding="utf-8"))
                name = str(metadata.get("name", package_json.parent.name))
                version = str(metadata.get("version", "unknown"))
                license_name = license_value(package_json)
            except (OSError, json.JSONDecodeError, TypeError) as exc:
                failures.append(f"{package_json}: cannot read package metadata: {exc}")
                continue
            key = (name, version)
            if key in inspected:
                continue
            inspected.add(key)
            if license_name not in ALLOWED_LICENSES:
                failures.append(f"{name}@{version}: license={license_name!r}")

    if failures:
        print("npm license policy violations:", file=sys.stderr)
        for failure in sorted(failures):
            print(f"- {failure}", file=sys.stderr)
        return 1
    print(f"npm license policy passed for {len(inspected)} package/version pairs")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
