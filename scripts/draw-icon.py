#!/usr/bin/env python3
"""Draws design/chosen/app-icon-source.png (1024 px), app-icon-256.png and windows/tray.png from geometry, so the icon is
flat and crisp: a near-black tile, four crop corners in Smugshot red, two square eyes, one raised eyebrow
and a smirk. Run from the repo root; needs Pillow. Then `swift scripts/make-icon.swift` on a Mac to
rebuild Resources/AppIcon.icns.

The glyph is the same one MenuBarIcon.swift draws at 18 points, on a 1024 grid.
"""
from PIL import Image, ImageDraw

BLACK = (20, 20, 22)       # the tile: near-black, not grey
RED = (255, 58, 92)        # crop corners: the overlay's #FF2D55, a step brighter
WHITE = (255, 255, 255)

S = 4                      # drawn at 4x, then shrunk, for smooth edges
N = 1024 * S


def px(v):
    return int(round(v * S))


def bezier(p0, p1, p2, steps=64):
    return [((1 - t) ** 2 * p0[0] + 2 * (1 - t) * t * p1[0] + t ** 2 * p2[0],
             (1 - t) ** 2 * p0[1] + 2 * (1 - t) * t * p1[1] + t ** 2 * p2[1])
            for t in (i / steps for i in range(steps + 1))]


def stroke(draw, points, width, colour):
    pts = [(px(x), px(y)) for x, y in points]
    draw.line(pts, fill=colour, width=px(width), joint="curve")
    r = px(width) / 2
    for x, y in (pts[0], pts[-1]):  # round caps
        draw.ellipse([x - r, y - r, x + r, y + r], fill=colour)


def draw_icon():
    im = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)

    # The tile, with the Mac's icon corner (about 22 %).
    d.rounded_rectangle([0, 0, N - 1, N - 1], radius=px(228), fill=BLACK)

    # Crop corners: a rounded frame, then the middle of each side is taken away again.
    lo, hi, w, arm, r = 160, 864, 56, 220, 96
    d.rounded_rectangle([px(lo), px(lo), px(hi), px(hi)], radius=px(r), outline=RED, width=px(w))
    gap = (lo + arm, hi - arm)
    d.rectangle([px(gap[0]), px(lo - 1), px(gap[1]), px(lo + w + 1)], fill=BLACK)
    d.rectangle([px(gap[0]), px(hi - w - 1), px(gap[1]), px(hi + 1)], fill=BLACK)
    d.rectangle([px(lo - 1), px(gap[0]), px(lo + w + 1), px(gap[1])], fill=BLACK)
    d.rectangle([px(hi - w - 1), px(gap[0]), px(hi + 1), px(gap[1])], fill=BLACK)
    half = w / 2
    for cx, cy in [(gap[0], lo + half), (gap[1], lo + half), (gap[0], hi - half), (gap[1], hi - half),
                   (lo + half, gap[0]), (lo + half, gap[1]), (hi - half, gap[0]), (hi - half, gap[1])]:
        d.ellipse([px(cx - half), px(cy - half), px(cx + half), px(cy + half)], fill=RED)  # round arm ends

    # Eyes: two squares.
    for x in (344, 594):
        d.rectangle([px(x), px(424), px(x + 88), px(512)], fill=WHITE)

    # The raised eyebrow over the right eye, and the smirk, higher on the right.
    stroke(d, bezier((496, 402), (600, 298), (696, 362)), 34, WHITE)
    stroke(d, bezier((402, 656), (520, 712), (642, 596)), 34, WHITE)

    return im.resize((1024, 1024), Image.LANCZOS)


def draw_tray():
    """The Windows tray glyph: the same corners and face as MenuBarIcon.swift, white on nothing, out to the
    edges, so the tray shows it as big as the system's own glyphs. Tinted black at run time on a light taskbar."""
    G = 18                      # the Mac menu bar glyph's grid
    u = 256 / G
    im = Image.new("RGBA", (256 * S, 256 * S), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    g = lambda v: v * u        # grid units to 256-pixel units (y counted from the top)

    lo, hi, w, arm, r = 0.8, 17.2, 1.6, 4.0, 2.0
    d.rounded_rectangle([px(g(lo)), px(g(lo)), px(g(hi)), px(g(hi))], radius=px(g(r)), outline=WHITE, width=px(g(w)))
    gap = (lo + arm, hi - arm)
    clear = (0, 0, 0, 0)
    d.rectangle([px(g(gap[0])), px(g(lo - 1)), px(g(gap[1])), px(g(lo + w + 0.1))], fill=clear)
    d.rectangle([px(g(gap[0])), px(g(hi - w - 0.1)), px(g(gap[1])), px(g(hi + 1))], fill=clear)
    d.rectangle([px(g(lo - 1)), px(g(gap[0])), px(g(lo + w + 0.1)), px(g(gap[1]))], fill=clear)
    d.rectangle([px(g(hi - w - 0.1)), px(g(gap[0])), px(g(hi + 1)), px(g(gap[1]))], fill=clear)
    half = w / 2
    for cx, cy in [(gap[0], lo + half), (gap[1], lo + half), (gap[0], hi - half), (gap[1], hi - half),
                   (lo + half, gap[0]), (lo + half, gap[1]), (hi - half, gap[0]), (hi - half, gap[1])]:
        d.ellipse([px(g(cx - half)), px(g(cy - half)), px(g(cx + half)), px(g(cy + half))], fill=WHITE)

    # Eyes, eyebrow and smirk from MenuBarIcon.swift, with y flipped (the Mac counts from the bottom).
    for x in (5.4, 10.7):
        d.rectangle([px(g(x)), px(g(G - 8.6 - 1.9)), px(g(x + 1.9)), px(g(G - 8.6))], fill=WHITE)
    flip = lambda p: (g(p[0]), g(G - p[1]))
    stroke(d, list(map(flip, cubic((9.7, 12.2), (10.4, 13.9), (11.9, 14.0), (12.7, 12.6)))), g(1.3), WHITE)
    stroke(d, list(map(flip, cubic((5.6, 6.2), (8.4, 4.9), (11.9, 5.2), (12.9, 7.9)))), g(1.3), WHITE)
    return im.resize((256, 256), Image.LANCZOS)


def cubic(p0, p1, p2, p3, steps=64):
    out = []
    for i in range(steps + 1):
        t = i / steps
        a, b, c, e = (1 - t) ** 3, 3 * (1 - t) ** 2 * t, 3 * (1 - t) * t ** 2, t ** 3
        out.append((a * p0[0] + b * p1[0] + c * p2[0] + e * p3[0], a * p0[1] + b * p1[1] + c * p2[1] + e * p3[1]))
    return out


if __name__ == "__main__":
    icon = draw_icon()
    # The source tile fills its canvas with white outside the corners (what make-icon.swift expects).
    flat = Image.new("RGB", icon.size, (255, 255, 255))
    flat.paste(icon, mask=icon.split()[3])
    flat.save("design/chosen/app-icon-source.png", optimize=True)
    small = icon.resize((256, 256), Image.LANCZOS)
    small.save("design/chosen/app-icon-256.png", optimize=True)
    draw_tray().save("windows/tray.png", optimize=True)  # embedded in the Windows exe for its tray icon
    print("wrote design/chosen/app-icon-source.png, app-icon-256.png and windows/tray.png")
