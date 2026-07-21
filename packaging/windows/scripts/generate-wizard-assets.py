#!/usr/bin/env python3
"""生成 Inno Setup 向导图（与 Web UI tokens.css 深色主题对齐）。

依赖：Pillow（仅维护者本地/CI 可选；生成后的 BMP 已提交仓库）。
"""
from __future__ import annotations

import os
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "assets"
BRAND = ROOT.parents[1] / "node" / "webui" / "frontend" / "src" / "assets" / "brand-icon.png"

# tokens.css dark
BG = (24, 24, 24)  # #181818
SURFACE = (30, 30, 30)  # #1e1e1e
PRIMARY = (55, 148, 255)  # #3794ff
TEXT = (204, 204, 204)  # #cccccc
MUTED = (157, 157, 157)  # #9d9d9d
BORDER = (43, 43, 43)  # #2b2b2b


def _fonts():
    for bold, regular in (
        ("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"),
        ("C:/Windows/Fonts/segoeuib.ttf", "C:/Windows/Fonts/segoeui.ttf"),
    ):
        try:
            return (
                ImageFont.truetype(bold, 14),
                ImageFont.truetype(regular, 10),
            )
        except OSError:
            continue
    default = ImageFont.load_default()
    return default, default


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    font, font_sm = _fonts()
    icon = Image.open(BRAND).convert("RGBA") if BRAND.is_file() else None

    w, h = 164, 314
    sidebar = Image.new("RGB", (w, h), BG)
    draw = ImageDraw.Draw(sidebar)
    draw.rectangle([0, 0, 3, h], fill=PRIMARY)
    if icon:
        icon40 = icon.resize((40, 40), Image.Resampling.LANCZOS)
        sidebar.paste(icon40, (20, 28), icon40)
        ty = 78
    else:
        draw.rounded_rectangle([20, 28, 84, 92], radius=8, fill=SURFACE, outline=BORDER)
        draw.text((28, 44), "DA", fill=PRIMARY, font=font)
        ty = 108
    draw.text((20, ty), "DAgents", fill=TEXT, font=font)
    draw.text((20, ty + 22), "本机智能助手", fill=MUTED, font=font_sm)
    draw.text((20, h - 56), "Workbench", fill=MUTED, font=font_sm)
    sidebar.save(OUT / "wizard-sidebar.bmp")

    sw, sh = 55, 58
    small = Image.new("RGB", (sw, sh), SURFACE)
    if icon:
        icon32 = icon.resize((32, 32), Image.Resampling.LANCZOS)
        small.paste(icon32, (11, 12), icon32)
    else:
        sd = ImageDraw.Draw(small)
        sd.rounded_rectangle([6, 8, sw - 6, sh - 8], radius=6, fill=BG, outline=BORDER)
        sd.text((14, 18), "DA", fill=PRIMARY, font=font)
    small.save(OUT / "wizard-small.bmp")
    print(f"wrote {OUT}/wizard-sidebar.bmp, wizard-small.bmp")


if __name__ == "__main__":
    main()
