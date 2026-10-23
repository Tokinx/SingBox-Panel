#!/usr/bin/env python3
"""Generate the PWA icon set from the panel logo.

Run from web/: python3 scripts/gen-pwa-icons.py

- icon-{192,512}.png          purpose "any"      — logo on a transparent canvas
- icon-maskable-{192,512}.png purpose "maskable" — logo inside the 80% safe zone
                               on a full-bleed white canvas, so Android's
                               adaptive-icon mask never clips the artwork.
"""
from pathlib import Path

from PIL import Image

WEB = Path(__file__).resolve().parent.parent
SOURCE = WEB / "src/assets/singbox-panel-logo.png"
PUBLIC = WEB / "public"

# The mark is a dark cube; a white canvas matches the panel's light chrome.
# Source artwork: src/assets/logo.svg (rendered to the 1024px PNG above).
BRAND = (255, 255, 255, 255)
SIZES = (192, 512)


def fit(image: Image.Image, box: int) -> Image.Image:
    width, height = image.size
    scale = min(box / width, box / height)
    return image.resize((max(1, round(width * scale)), max(1, round(height * scale))), Image.LANCZOS)


def center(canvas: Image.Image, logo: Image.Image) -> None:
    canvas.paste(logo, ((canvas.width - logo.width) // 2, (canvas.height - logo.height) // 2), logo)


def main() -> None:
    logo = Image.open(SOURCE).convert("RGBA")
    PUBLIC.mkdir(parents=True, exist_ok=True)
    for size in SIZES:
        canvas = Image.new("RGBA", (size, size), (0, 0, 0, 0))
        center(canvas, fit(logo, round(size * 0.86)))
        canvas.save(PUBLIC / f"icon-{size}.png")

        maskable = Image.new("RGBA", (size, size), BRAND)
        center(maskable, fit(logo, round(size * 0.62)))
        maskable.save(PUBLIC / f"icon-maskable-{size}.png")
        print(f"icon-{size}.png, icon-maskable-{size}.png")


if __name__ == "__main__":
    main()
