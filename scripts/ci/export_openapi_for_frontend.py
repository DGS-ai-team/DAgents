from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Export DAgents OpenAPI schema into a DAgentsUI checkout")
    parser.add_argument(
        "--frontend",
        default="../DAgentsUI",
        help="Path to the DAgentsUI repository checkout, relative to the DAgents repository root by default",
    )
    parser.add_argument(
        "--skip-types",
        action="store_true",
        help="Only update openapi.json; do not run pnpm gen:types in the frontend repository",
    )
    return parser.parse_args()


def main() -> int:
    backend_root = Path(__file__).resolve().parents[2]
    args = parse_args()
    frontend_root = Path(args.frontend)
    if not frontend_root.is_absolute():
        frontend_root = backend_root / frontend_root
    frontend_root = frontend_root.resolve()

    openapi_target = frontend_root / "openapi.json"
    if not (frontend_root / "package.json").exists():
        print(f"[openapi-sync] frontend checkout not found: {frontend_root}")
        return 1

    export_cmd = [
        sys.executable,
        str(backend_root / "export_openapi_schema.py"),
        "--output",
        str(openapi_target),
    ]
    subprocess.run(export_cmd, cwd=backend_root, check=True)

    if not args.skip_types:
        subprocess.run(["pnpm", "gen:types"], cwd=frontend_root, check=True)

    print(f"[openapi-sync] updated {openapi_target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())