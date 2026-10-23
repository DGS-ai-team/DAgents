"""Check the canonical brand source, retired Node UI paths, and native inputs.

The native Desktop needs checked-in images because the Tauri bundler consumes
files from its package directory. The files are generated
from ``shared/branding/brand-icon.png``. This check also guards against
reintroducing a standalone browser entry for the shared Desktop UI.
"""

from __future__ import annotations

import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CANONICAL = ROOT / "shared" / "branding" / "brand-icon.png"
NODE_WEBUI_ROOT = ROOT / "node" / "webui"
NODE_STATIC_WEBUI_ROOT = ROOT / "node" / "internal" / "webui"
DESKTOP_UI_ROOT = ROOT / "desktop" / "tray-tauri" / "src" / "ui"
DESKTOP_PACKAGE = ROOT / "desktop" / "tray-tauri" / "package.json"
MANAGE_PUBLIC = ROOT / "manage" / "console" / "frontend" / "public"
MANAGE_MAIN = ROOT / "manage" / "console" / "frontend" / "src" / "main.js"
TRAY_GENERATOR = ROOT / "packaging" / "windows" / "scripts" / "generate-tray-icons.py"
WIZARD_GENERATOR = ROOT / "packaging" / "windows" / "scripts" / "generate-wizard-assets.py"

GENERATED_NATIVE_ASSETS = (
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
        duplicate = MANAGE_PUBLIC / filename
        if duplicate.exists():
            errors.append(f"Manage Console must not contain a copied favicon: {duplicate.relative_to(ROOT)}")

    for retired_path in (NODE_WEBUI_ROOT, NODE_STATIC_WEBUI_ROOT):
        if retired_path.exists():
            errors.append(f"retired Node WebUI path must not be present: {retired_path.relative_to(ROOT)}")

    if not (DESKTOP_UI_ROOT / "DesktopApp.vue").is_file():
        errors.append("Desktop Vue entry is missing from desktop/tray-tauri/src/ui")
    if not DESKTOP_PACKAGE.is_file():
        errors.append("Desktop package metadata is missing")

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

    print("desktop-ui: Node WebUI is retired; Desktop source, canonical brand, and native inputs are present")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
