"""Check that branding has one source and known native build inputs.

The native shells need checked-in images because Go ``embed`` and the Tauri
bundler consume files from their package directories. The files are generated
from ``shared/branding/brand-icon.png``; this check prevents a second web
favicon source from quietly returning to the repository and verifies that the
native build inputs remain present.
"""

from __future__ import annotations

import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CANONICAL = ROOT / "shared" / "branding" / "brand-icon.png"
NODE_PUBLIC = ROOT / "node" / "webui" / "frontend" / "public"
NODE_INDEX = ROOT / "node" / "webui" / "frontend" / "index.html"
NODE_MAIN = ROOT / "node" / "webui" / "frontend" / "src" / "main.js"
MANAGE_PUBLIC = ROOT / "manage" / "console" / "frontend" / "public"
MANAGE_MAIN = ROOT / "manage" / "console" / "frontend" / "src" / "main.js"
TRAY_GENERATOR = ROOT / "packaging" / "windows" / "scripts" / "generate-tray-icons.py"
WIZARD_GENERATOR = ROOT / "packaging" / "windows" / "scripts" / "generate-wizard-assets.py"

GENERATED_NATIVE_ASSETS = (
    ROOT / "desktop" / "tray" / "assets" / "icon.ico",
    ROOT / "desktop" / "tray" / "assets" / "icon_pending.ico",
    ROOT / "desktop" / "tray-tauri" / "src-tauri" / "icons" / "icon.ico",
    ROOT / "desktop" / "tray-tauri" / "src-tauri" / "icons" / "icon_pending.ico",
    ROOT / "desktop" / "tray-tauri" / "src-tauri" / "icons" / "tray-icon.ico",
    ROOT / "desktop" / "tray-tauri" / "src-tauri" / "icons" / "icon.png",
    ROOT / "desktop" / "tray-tauri" / "src-tauri" / "icons" / "icon_pending.png",
    ROOT / "packaging" / "windows" / "assets" / "wizard-sidebar.bmp",
    ROOT / "packaging" / "windows" / "assets" / "wizard-small.bmp",
)


def main() -> int:
    errors: list[str] = []

    if not CANONICAL.is_file():
        errors.append(f"missing canonical brand source: {CANONICAL.relative_to(ROOT)}")

    for filename in ("favicon.ico", "favicon.png"):
        for public_dir in (NODE_PUBLIC, MANAGE_PUBLIC):
            duplicate = public_dir / filename
            if duplicate.exists():
                errors.append(f"Web UI public must not contain a copied favicon: {duplicate.relative_to(ROOT)}")

    try:
        index = NODE_INDEX.read_text(encoding="utf-8")
        if "/favicon" in index:
            errors.append("Node index.html still references a public favicon")
    except OSError as error:
        errors.append(f"cannot read Node index.html: {error}")

    try:
        main_js = NODE_MAIN.read_text(encoding="utf-8")
        if '@dagents-brand/brand-icon.png' not in main_js:
            errors.append("Node main.js does not import the canonical brand icon")
    except OSError as error:
        errors.append(f"cannot read Node main.js: {error}")

    try:
        manage_main = MANAGE_MAIN.read_text(encoding="utf-8")
        if '@dagents-brand/brand-icon.png' not in manage_main:
            errors.append("Manage main.js does not import the canonical brand icon")
    except OSError as error:
        errors.append(f"cannot read Manage main.js: {error}")

    try:
        generator = TRAY_GENERATOR.read_text(encoding="utf-8")
        expected_source = 'BRAND = REPO / "shared" / "branding" / "brand-icon.png"'
        if expected_source not in generator:
            errors.append("tray icon generator does not use shared/branding/brand-icon.png")
        for output_dir in (
            'REPO / "desktop" / "tray" / "assets"',
            'REPO / "desktop" / "tray-tauri" / "src-tauri" / "icons"',
        ):
            if output_dir not in generator:
                errors.append(f"tray icon generator is missing output directory: {output_dir}")
    except OSError as error:
        errors.append(f"cannot read tray icon generator: {error}")

    try:
        wizard_generator = WIZARD_GENERATOR.read_text(encoding="utf-8")
        expected_source = 'BRAND = ROOT.parents[1] / "shared" / "branding" / "brand-icon.png"'
        if expected_source not in wizard_generator:
            errors.append("wizard asset generator does not use shared/branding/brand-icon.png")
    except OSError as error:
        errors.append(f"cannot read wizard asset generator: {error}")

    for asset in GENERATED_NATIVE_ASSETS:
        if not asset.is_file() or asset.stat().st_size == 0:
            errors.append(f"missing native branding build input: {asset.relative_to(ROOT)}")

    if errors:
        for error in errors:
            print(f"brand-assets: {error}", file=sys.stderr)
        return 1

    print("brand-assets: canonical source and native generated inputs are present")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
