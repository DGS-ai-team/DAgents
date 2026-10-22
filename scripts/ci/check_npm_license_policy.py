"""Check licenses of installed npm packages against the project policy.

This intentionally inspects installed package metadata rather than trying to
infer licenses from package names. It covers the two shipped Vue applications;
the lockfiles remain the source of dependency versions and npm audit covers
known security advisories.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


ALLOWED_LICENSES = {
    "0BSD",
    "Apache-2.0",
    "BSD-2-Clause",
    "BSD-3-Clause",
    "BlueOak-1.0.0",
    "ISC",
    "MIT-0",
    "MIT",
    "MPL-2.0",
    "Python-2.0",
    "Zlib",
}

# npm metadata still contains a few non-SPDX spellings. Keep these explicit so
# new packages cannot silently inherit a permissive interpretation.
LEGACY_LICENSE_ALIASES = {
    "MIT/X11": "MIT",
}

SPDX_TOKEN = re.compile(r"\s*(\(|\)|AND\b|OR\b|[A-Za-z0-9.+-]+)")


def license_expression_allowed(
    expression: str | None,
    package_name: str | None = None,
    package_version: str | None = None,
) -> bool:
    """Accept only allowed SPDX IDs, respecting OR and AND semantics."""
    if not expression:
        return False
    if (package_name, package_version, expression.strip()) == ("duck", "0.1.12", "BSD"):
        # duck's metadata uses a generic label; its bundled LICENSE is BSD-2-Clause.
        expression = "BSD-2-Clause"
    expression = LEGACY_LICENSE_ALIASES.get(expression.strip(), expression.strip())
    tokens: list[str] = []
    position = 0
    while position < len(expression):
        match = SPDX_TOKEN.match(expression, position)
        if not match:
            return False
        tokens.append(match.group(1))
        position = match.end()

    index = 0

    def parse_primary() -> bool:
        nonlocal index
        if index >= len(tokens):
            raise ValueError("missing license identifier")
        token = tokens[index]
        if token == "(":
            index += 1
            value = parse_or()
            if index >= len(tokens) or tokens[index] != ")":
                raise ValueError("unmatched opening parenthesis")
            index += 1
            return value
        if token in {")", "AND", "OR"}:
            raise ValueError("unexpected operator")
        index += 1
        return token in ALLOWED_LICENSES

    def parse_and() -> bool:
        nonlocal index
        value = parse_primary()
        while index < len(tokens) and tokens[index] == "AND":
            index += 1
            value = parse_primary() and value
        return value

    def parse_or() -> bool:
        nonlocal index
        value = parse_and()
        while index < len(tokens) and tokens[index] == "OR":
            index += 1
            alternative = parse_and()
            value = value or alternative
        return value

    try:
        result = parse_or()
        return index == len(tokens) and result
    except ValueError:
        return False


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
    if isinstance(value, dict) and isinstance(value.get("type"), str):
        return value["type"]
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
        Path("manage/console/frontend/node_modules"),
        Path("desktop/tray-tauri/node_modules"),
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
            if not license_expression_allowed(license_name, name, version):
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
