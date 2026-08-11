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
BG_TOP = (243, 243, 243)  # #f3f3f3 --color-bg
BG_BOTTOM = (249, 249, 249)  # #f9f9f9 --color-bg-soft
SURFACE = (255, 255, 255)  # #ffffff
PRIMARY = (0, 120, 212)  # #0078d4
PRIMARY_SOFT = (230, 242, 251)  # soft tint of primary
TEXT = (26, 26, 26)  # #1a1a1a
MUTED = (93, 93, 93)  # #5d5d5d
SUBTLE = (138, 138, 138)  # #8a8a8a
BORDER = (228, 228, 228)  # soft neutral

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


def _fonts() -> tuple[ImageFont.ImageFont, ImageFont.ImageFont, ImageFont.ImageFont, ImageFont.ImageFont]:
    """Return (title_bold, latin_regular, cjk_regular, cjk_small)."""
    title = _first_font(_LATIN_BOLD_CANDIDATES, 18)
    latin_regular = _first_font(_LATIN_REGULAR_CANDIDATES, 10)
    cjk = _first_font(_CJK_FONT_CANDIDATES, 12)
    cjk_sm = _first_font(_CJK_FONT_CANDIDATES, 10)
    return title, latin_regular, cjk, cjk_sm


def _vertical_gradient(size: tuple[int, int], top: tuple[int, int, int], bottom: tuple[int, int, int]) -> Image.Image:
    w, h = size
    img = Image.new("RGB", size, top)
    px = img.load()
    for y in range(h):
        t = y / max(h - 1, 1)
        r = int(top[0] + (bottom[0] - top[0]) * t)
        g = int(top[1] + (bottom[1] - top[1]) * t)
        b = int(top[2] + (bottom[2] - top[2]) * t)
        for x in range(w):
            px[x, y] = (r, g, b)
    return img


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    font_title, font_sm, font_cjk, font_cjk_sm = _fonts()
    icon = Image.open(BRAND).convert("RGBA") if BRAND.is_file() else None

    w, h = 164, 314
    sidebar = _vertical_gradient((w, h), BG_TOP, BG_BOTTOM)
    draw = ImageDraw.Draw(sidebar)

    # Left brand accent
    draw.rectangle([0, 0, 3, h], fill=PRIMARY)
    # Soft top wash (atmosphere without competing with brand)
    for i in range(72):
        t = 1 - (i / 71)
        t = t * t
        wash = (
            int(PRIMARY_SOFT[0] * t + BG_TOP[0] * (1 - t)),
            int(PRIMARY_SOFT[1] * t + BG_TOP[1] * (1 - t)),
            int(PRIMARY_SOFT[2] * t + BG_TOP[2] * (1 - t)),
        )
        draw.line([(4, i), (w - 1, i)], fill=wash)

    # Brand mark plate (soft disk, not a card)
    cx, cy, r = 52, 54, 34
    draw.ellipse([cx - r, cy - r, cx + r, cy + r], fill=PRIMARY_SOFT)
    draw.ellipse([cx - r + 2, cy - r + 2, cx + r - 2, cy + r - 2], outline=(210, 228, 242))

    if icon:
        icon48 = icon.resize((48, 48), Image.Resampling.LANCZOS)
        sidebar.paste(icon48, (cx - 24, cy - 24), icon48)
    else:
        draw.rounded_rectangle([cx - 24, cy - 24, cx + 24, cy + 24], radius=10, fill=SURFACE, outline=BORDER)
        draw.text((cx - 14, cy - 10), "DA", fill=PRIMARY, font=font_title)

    # Brand wordmark — hero of the sidebar
    ty = 104
    draw.text((18, ty), "DAgents", fill=TEXT, font=font_title)
    draw.text((18, ty + 28), "本机智能助手", fill=MUTED, font=font_cjk)

    # Divider + secondary label (mid-lower, not stranded at bottom edge)
    div_y = h - 88
    draw.line([(18, div_y), (w - 18, div_y)], fill=BORDER)
    draw.text((18, div_y + 16), "Workbench", fill=SUBTLE, font=font_sm)
    draw.text((18, div_y + 36), "安装向导", fill=SUBTLE, font=font_cjk_sm)

    sidebar.save(OUT / "wizard-sidebar.bmp")

    sw, sh = 55, 58
    small = Image.new("RGB", (sw, sh), SURFACE)
    sd = ImageDraw.Draw(small)
    sd.ellipse([4, 6, sw - 5, sh - 5], fill=PRIMARY_SOFT, outline=(210, 228, 242))
    if icon:
        icon28 = icon.resize((28, 28), Image.Resampling.LANCZOS)
        small.paste(icon28, ((sw - 28) // 2, (sh - 28) // 2), icon28)
    else:
        sd.text((16, 18), "DA", fill=PRIMARY, font=font_sm)
    small.save(OUT / "wizard-small.bmp")
    print(f"wrote {OUT}/wizard-sidebar.bmp, wizard-small.bmp")


if __name__ == "__main__":
    main()
