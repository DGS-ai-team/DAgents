"""Add missing Node route stubs to the checked-in OpenAPI document.

The detailed request/response schemas remain hand-authored. This helper only
prevents newly registered public routes from silently disappearing from the
machine-readable API surface.
"""

from __future__ import annotations

import argparse
import re

from check_contracts import RETIRED_OPERATIONS, ROOT, registered_routes


OPENAPI_FILE = ROOT / "docs/architecture/openapi-node.yaml"
PATH_RE = re.compile(r"^  (/[^:]+):$", re.MULTILINE)
METHOD_RE = re.compile(r"^    (get|post|put|patch|delete|options|head|trace):$", re.MULTILINE)
INSERT_MARKER = "\ncomponents:\n"


def documented_routes(text: str) -> set[tuple[str, str]]:
    routes: set[tuple[str, str]] = set()
    matches = list(PATH_RE.finditer(text))
    for index, match in enumerate(matches):
        end = matches[index + 1].start() if index + 1 < len(matches) else text.find(INSERT_MARKER, match.end())
        block = text[match.end() : end if end >= 0 else len(text)]
        for method in METHOD_RE.findall(block):
            routes.add((method.upper(), match.group(1)))
    return routes


def stub_block(route: str, methods: list[str]) -> str:
    lines = [f"  {route}:"]
    for method in methods:
        lines.extend(
            [
                f"    {method.lower()}:",
                "      x-contract-status: surface-only",
                "      summary: Route registered in the Node HTTP server",
                "      responses:",
                '        "200":',
                "          description: OK",
            ]
        )
    return "\n".join(lines) + "\n"


def sync(*, write: bool) -> int:
    text = OPENAPI_FILE.read_text(encoding="utf-8")
    documented = documented_routes(text)
    missing = sorted(registered_routes() - documented)
    if not missing:
        print("OpenAPI route surface is up to date")
        return 0

    if not write:
        for method, route in missing:
            print(f"missing: {method} {route}")
        return 1

    if INSERT_MARKER not in text:
        raise ValueError(f"cannot find insertion marker {INSERT_MARKER!r} in {OPENAPI_FILE}")
    blocks = []
    seen_paths: set[str] = set()
    for method, route in missing:
        if (method, route) in RETIRED_OPERATIONS:
            continue
        if route in seen_paths:
            continue
        seen_paths.add(route)
        methods = sorted(m for m, r in missing if r == route)
        blocks.append("\n" + stub_block(route, methods).rstrip("\n"))
    updated = text.replace(INSERT_MARKER, "\n" + "\n".join(blocks) + INSERT_MARKER, 1)
    OPENAPI_FILE.write_text(updated, encoding="utf-8")
    print(f"added {len(seen_paths)} OpenAPI route path stubs")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--write", action="store_true", help="write missing route stubs")
    parser.add_argument("--check", action="store_true", help="check coverage without writing")
    args = parser.parse_args()
    if args.write and args.check:
        parser.error("--write and --check are mutually exclusive")
    return sync(write=args.write)


if __name__ == "__main__":
    raise SystemExit(main())
