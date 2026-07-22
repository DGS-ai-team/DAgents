#!/usr/bin/env python3
"""生成 Inno Setup 向导图（与 Web UI tokens.css 浅色主题对齐）。

依赖：Pillow（仅维护者本地/CI 可选；生成后的 BMP 已提交仓库）。
副标题含中文，必须使用带 CJK 字形的字体，否则会落成 □□（tofu）。
"""
from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "assets"
BRAND = ROOT.parents[1] / "node" / "webui" / "frontend" / "src" / "assets" / "brand-icon.png"

# tokens.css light（与 dagents-installer.iss Workbench 浅色主题一致）
BG = (245, 246, 248)  # #f5f6f8
SURFACE = (255, 255, 255)  # #ffffff
PRIMARY = (37, 99, 235)  # #2563eb
TEXT = (48, 36, 31)  # #30241f
MUTED = (104, 85, 75)  # #68554b
BORDER = (229, 231, 235)  # #e5e7eb

# Prefer CJK-capable fonts first (subtitle is Chinese). Latin-only fonts render as tofu.
_CJK_FONT_CANDIDATES = (
    "/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
    "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
    "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
    "/usr/share/fonts/truetype/noto/NotoSansSC-Regular.otf",
    "C:/Windows/Fonts/msyh.ttc",  # Microsoft YaHei
    "C:/Windows/Fonts/msyh.ttf",
    "C:/Windows/Fonts/simhei.ttf",
)

_LATIN_BOLD_CANDIDATES = (
    "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
    "C:/Windows/Fonts/segoeuib.ttf",
    "C:/Windows/Fonts/arialbd.ttf",
)

_LATIN_REGULAR_CANDIDATES = (
    "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    "C:/Windows/Fonts/segoeui.ttf",
    "C:/Windows/Fonts/arial.ttf",
)


def _truetype(path: str, size: int) -> ImageFont.FreeTypeFont | None:
    try:
        return ImageFont.truetype(path, size)
    except OSError:
        return None


def _first_font(paths: tuple[str, ...], size: int) -> ImageFont.ImageFont:
    for path in paths:
        font = _truetype(path, size)
        if font is not None:
            return font
    return ImageFont.load_default()


def _fonts() -> tuple[ImageFont.ImageFont, ImageFont.ImageFont, ImageFont.ImageFont]:
    """Return (latin_bold, latin_regular, cjk_regular) for title / English / Chinese."""
    latin_bold = _first_font(_LATIN_BOLD_CANDIDATES, 14)
    latin_regular = _first_font(_LATIN_REGULAR_CANDIDATES, 10)
    cjk = _first_font(_CJK_FONT_CANDIDATES, 11)
    return latin_bold, latin_regular, cjk


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    font, font_sm, font_cjk = _fonts()
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
    draw.text((20, ty + 22), "本机智能助手", fill=MUTED, font=font_cjk)
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
