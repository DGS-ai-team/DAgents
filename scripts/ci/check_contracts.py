"""Validate public route coverage and the checked-in Workgroup fixtures.

This intentionally uses the source route registrations as the implementation
surface and OpenAPI/JSON fixtures as the compatibility surface. It keeps the
check useful without requiring generated Go or Python clients.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError as error:  # pragma: no cover - CI installs project requirements
    raise SystemExit("PyYAML is required to validate the OpenAPI document") from error


ROOT = Path(__file__).resolve().parents[2]
OPENAPI_FILE = ROOT / "docs/architecture/openapi-node.yaml"
FIXTURE_ROOT = ROOT / "docs/design/fixtures/workgroup-d05"
SCHEMA_VERSION = "0.5.0"
ROUTE_RE = re.compile(r'HandleFunc\("([A-Z]+) ([^"]+)"')
RETIRED_OPERATIONS = {("GET", "/v1/policy")}


def registered_routes() -> set[tuple[str, str]]:
    routes: set[tuple[str, str]] = set()
    for path in (ROOT / "node/internal/api").glob("*.go"):
        if path.name.endswith("_test.go"):
            continue
        routes.update((method, route) for method, route in ROUTE_RE.findall(path.read_text(encoding="utf-8")))
    return routes


def openapi_routes(document: dict) -> set[tuple[str, str]]:
    routes: set[tuple[str, str]] = set()
    paths = document.get("paths")
    if not isinstance(paths, dict):
        raise ValueError("OpenAPI document has no paths object")
    for route, item in paths.items():
        if not isinstance(route, str) or not isinstance(item, dict):
            raise ValueError(f"invalid OpenAPI path item: {route!r}")
        for method in item:
            if method.lower() in {"get", "post", "put", "patch", "delete", "options", "head", "trace"}:
                routes.add((method.upper(), route))
    return routes


def validate_openapi(errors: list[str]) -> None:
    try:
        document = yaml.safe_load(OPENAPI_FILE.read_text(encoding="utf-8"))
        if not isinstance(document, dict):
            raise ValueError("document root is not an object")
        documented = openapi_routes(document)
    except (OSError, ValueError, yaml.YAMLError) as error:
        errors.append(f"OpenAPI cannot be loaded: {error}")
        return

    registered = registered_routes()
    missing = sorted(registered - documented)
    stale = sorted(documented - registered - RETIRED_OPERATIONS)
    errors.extend(f"route missing from OpenAPI: {method} {route}" for method, route in missing)
    errors.extend(f"OpenAPI documents unknown route: {method} {route}" for method, route in stale)


def validate_fixtures(errors: list[str]) -> None:
    if not FIXTURE_ROOT.is_dir():
        errors.append(f"fixture root missing: {FIXTURE_ROOT}")
        return

    json_files = sorted(FIXTURE_ROOT.rglob("*.json"))
    for path in json_files:
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as error:
            errors.append(f"invalid JSON fixture {path.relative_to(ROOT)}: {error}")
            continue
        if path.parent.name == "schemas":
            # assign_workgroup_task.tool.json is a small indirection wrapper
            # kept beside the real JSON Schema files.
            is_schema_ref = isinstance(payload, dict) and "use" in payload
            if not is_schema_ref and (
                not isinstance(payload, dict) or "$schema" not in payload or "title" not in payload
            ):
                errors.append(f"schema fixture lacks $schema/title: {path.relative_to(ROOT)}")

    index_file = FIXTURE_ROOT / "INDEX.json"
    try:
        index = json.loads(index_file.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        errors.append(f"invalid fixture index: {error}")
        return
    if not isinstance(index, dict):
        errors.append("fixture index is not a JSON object")
    elif index.get("schema_version") != SCHEMA_VERSION:
        errors.append(
            f"fixture index schema_version={index.get('schema_version')!r}, expected {SCHEMA_VERSION!r}"
        )


def main() -> int:
    errors: list[str] = []
    validate_openapi(errors)
    validate_fixtures(errors)
    if errors:
        for error in errors:
            print(f"contract error: {error}", file=sys.stderr)
        return 1
    print("API and Workgroup contracts are consistent")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
