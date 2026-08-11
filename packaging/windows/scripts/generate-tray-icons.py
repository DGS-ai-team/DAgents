#!/usr/bin/env python3
"""Generate the Windows tray icons from the shared transparent brand mark."""

from __future__ import annotations

from pathlib import Path

from PIL import Image


ROOT = Path(__file__).resolve().parents[1]
REPO = ROOT.parents[1]
BRAND = REPO / "shared" / "branding" / "brand-icon.png"
OUT_DIRS = (
    REPO / "desktop" / "tray" / "assets",
    REPO / "desktop" / "tray-tauri" / "src-tauri" / "icons",
)
ICON_SIZES = (16, 24, 32, 48, 64, 128, 256)
CANVAS_SIZE = 256
LOGO_SCALE = 0.82


def build_icon() -> Image.Image:
    source = Image.open(BRAND).convert("RGBA")
    logo_size = round(CANVAS_SIZE * LOGO_SCALE)
    source.thumbnail((logo_size, logo_size), Image.Resampling.LANCZOS)

    canvas = Image.new("RGBA", (CANVAS_SIZE, CANVAS_SIZE), (0, 0, 0, 0))
    left = (CANVAS_SIZE - source.width) // 2
    top = (CANVAS_SIZE - source.height) // 2
    canvas.paste(source, (left, top), source)
    return canvas


def main() -> None:
    if not BRAND.is_file():
        raise FileNotFoundError(BRAND)
    icon = build_icon()
    sizes = [(size, size) for size in ICON_SIZES]
    for out_dir in OUT_DIRS:
        out_dir.mkdir(parents=True, exist_ok=True)
        filenames = ("icon.ico", "icon_pending.ico")
        if out_dir.name == "icons":
            filenames += ("tray-icon.ico",)
        for filename in filenames:
            icon.save(out_dir / filename, format="ICO", sizes=sizes)
    print("wrote shared Go and Tauri Windows tray icons")


if __name__ == "__main__":
    main()
