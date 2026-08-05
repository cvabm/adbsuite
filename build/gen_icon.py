"""Generate ADB Suite app icon (PNG + multi-size ICO)."""
from __future__ import annotations

import os
from pathlib import Path

from PIL import Image, ImageChops, ImageDraw

SIZE = 1024
ROOT = Path(__file__).resolve().parent
OUT_PNG = ROOT / "appicon.png"
OUT_ICO = ROOT / "windows" / "icon.ico"


def rounded_mask(size: int, radius: int) -> Image.Image:
    m = Image.new("L", (size, size), 0)
    md = ImageDraw.Draw(m)
    md.rounded_rectangle([0, 0, size - 1, size - 1], radius=radius, fill=255)
    return m


def main() -> None:
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    radius = int(SIZE * 0.22)
    mask = rounded_mask(SIZE, radius)

    # Gradient background: deep indigo -> app blue
    bg = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    bd = ImageDraw.Draw(bg)
    for y in range(SIZE):
        t = y / (SIZE - 1)
        r = int(26 + (79 - 26) * t)
        g = int(63 + (140 - 63) * t)
        b = int(168 + (255 - 168) * t)
        bd.line([(0, y), (SIZE, y)], fill=(r, g, b, 255))
    bg.putalpha(mask)
    img = Image.alpha_composite(img, bg)

    # Soft top highlight
    hi = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    hd = ImageDraw.Draw(hi)
    hd.ellipse(
        [int(SIZE * 0.08), int(-SIZE * 0.2), int(SIZE * 0.92), int(SIZE * 0.5)],
        fill=(255, 255, 255, 42),
    )
    ha = ImageChops.multiply(hi.split()[3], mask)
    hi.putalpha(ha)
    img = Image.alpha_composite(img, hi)
    d = ImageDraw.Draw(img)

    # Phone body
    pw, ph = int(SIZE * 0.38), int(SIZE * 0.62)
    px = (SIZE - pw) // 2
    py = int(SIZE * 0.16)
    pr = int(SIZE * 0.07)

    d.rounded_rectangle([px, py, px + pw, py + ph], radius=pr, fill=(245, 248, 255, 255))

    pad = int(SIZE * 0.028)
    sr = int(SIZE * 0.05)
    sx0 = px + pad
    sy0 = py + pad + int(SIZE * 0.02)
    sx1 = px + pw - pad
    sy1 = py + ph - pad - int(SIZE * 0.04)
    d.rounded_rectangle([sx0, sy0, sx1, sy1], radius=sr, fill=(15, 22, 38, 255))

    # Speaker bar
    nbw, nbh = int(pw * 0.28), int(SIZE * 0.012)
    nbx = px + (pw - nbw) // 2
    nby = py + int(SIZE * 0.035)
    d.rounded_rectangle(
        [nbx, nby, nbx + nbw, nby + nbh], radius=nbh // 2, fill=(200, 208, 220, 255)
    )

    # Screen: terminal / ADB vibe
    sw = sx1 - sx0
    margin = int(SIZE * 0.028)
    line_h = int(SIZE * 0.042)
    x = sx0 + margin
    y = sy0 + margin + int(SIZE * 0.02)

    def draw_line(yy: int, color: tuple[int, int, int, int], w_frac: float, thick: int | None = None) -> None:
        th = thick or max(6, int(SIZE * 0.018))
        w = int(sw * w_frac) - margin * 2
        d.rounded_rectangle([x, yy, x + w, yy + th], radius=th // 2, fill=color)

    # First prompt >
    d.line(
        [
            (x + int(SIZE * 0.01), y),
            (x + int(SIZE * 0.035), y + int(line_h * 0.45)),
            (x + int(SIZE * 0.01), y + int(line_h * 0.9)),
        ],
        fill=(62, 207, 142, 255),
        width=max(6, int(SIZE * 0.012)),
    )
    draw_line(y + int(line_h * 0.25), (79, 140, 255, 255), 0.55)
    y += int(line_h * 1.35)
    draw_line(y, (90, 110, 140, 255), 0.72, max(5, int(SIZE * 0.014)))
    y += int(line_h * 1.05)
    draw_line(y, (90, 110, 140, 255), 0.48, max(5, int(SIZE * 0.014)))
    y += int(line_h * 1.05)
    draw_line(y, (90, 110, 140, 255), 0.62, max(5, int(SIZE * 0.014)))
    y += int(line_h * 1.35)

    # Second prompt + cursor
    d.line(
        [
            (x + int(SIZE * 0.01), y),
            (x + int(SIZE * 0.035), y + int(line_h * 0.45)),
            (x + int(SIZE * 0.01), y + int(line_h * 0.9)),
        ],
        fill=(62, 207, 142, 255),
        width=max(6, int(SIZE * 0.012)),
    )
    cx = x + int(SIZE * 0.055)
    cy = y + int(line_h * 0.15)
    cw, ch = int(SIZE * 0.04), int(line_h * 0.7)
    d.rounded_rectangle([cx, cy, cx + cw, cy + ch], radius=4, fill=(233, 236, 244, 230))

    # Home indicator
    hiw, hih = int(pw * 0.28), int(SIZE * 0.01)
    hix = px + (pw - hiw) // 2
    hiy = py + ph - int(SIZE * 0.028)
    d.rounded_rectangle(
        [hix, hiy, hix + hiw, hiy + hih], radius=hih // 2, fill=(180, 190, 210, 255)
    )

    # Connected badge (USB plug)
    bx = px + pw - int(SIZE * 0.02)
    by = py + ph - int(SIZE * 0.02)
    br = int(SIZE * 0.11)
    d.ellipse([bx - br + 6, by - br + 8, bx + br + 6, by + br + 8], fill=(0, 0, 0, 50))
    d.ellipse([bx - br, by - br, bx + br, by + br], fill=(62, 207, 142, 255))
    d.ellipse(
        [bx - br + 8, by - br + 8, bx + br - 8, by + br - 8],
        outline=(255, 255, 255, 220),
        width=max(4, int(SIZE * 0.01)),
    )
    plug_w, plug_h = int(SIZE * 0.055), int(SIZE * 0.038)
    d.rounded_rectangle(
        [bx - plug_w // 2, by - plug_h // 2, bx + plug_w // 2, by + plug_h // 2],
        radius=6,
        fill=(255, 255, 255, 255),
    )
    pin_w = int(SIZE * 0.012)
    pin_h = int(SIZE * 0.02)
    gap = int(SIZE * 0.016)
    for dx in (-gap, gap):
        d.rounded_rectangle(
            [
                bx + dx - pin_w // 2,
                by - plug_h // 2 - pin_h + 2,
                bx + dx + pin_w // 2,
                by - plug_h // 2 + 2,
            ],
            radius=3,
            fill=(255, 255, 255, 255),
        )

    # Subtle outer stroke
    stroke = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    sd = ImageDraw.Draw(stroke)
    sd.rounded_rectangle(
        [2, 2, SIZE - 3, SIZE - 3], radius=radius, outline=(255, 255, 255, 40), width=4
    )
    img = Image.alpha_composite(img, stroke)

    OUT_PNG.parent.mkdir(parents=True, exist_ok=True)
    OUT_ICO.parent.mkdir(parents=True, exist_ok=True)
    img.save(OUT_PNG, "PNG")

    sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    img_256 = img.resize((256, 256), Image.Resampling.LANCZOS)
    img_256.save(OUT_ICO, format="ICO", sizes=sizes)

    print(f"wrote {OUT_PNG} ({OUT_PNG.stat().st_size} bytes)")
    print(f"wrote {OUT_ICO} ({OUT_ICO.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
