#!/usr/bin/env python3
"""Draws the ZipNest icon (original design: a letter "Z" built from a zipper on a
purple-to-pink tile) using only the Python standard library.

Writes:
  zipnest.ico          Windows icon (16, 24, 32, 48, 64, 256 px)
  zipnest-256.png      preview
  ic_fg.png / ic_bg.png / ic_mono.png
                       Android adaptive-icon layers (432 px = 108dp @ xxxhdpi)

usage: make_icons.py <out_dir>
"""
import math
import os
import struct
import sys
import zlib

PURPLE = (0.42, 0.17, 0.88)
PINK = (0.88, 0.27, 0.54)
WHITE = (1.0, 1.0, 1.0)
GOLD = (1.0, 0.76, 0.24)
GOLD_DARK = (0.91, 0.60, 0.07)
SHADOW = (0.25, 0.08, 0.45)


def mix(c1, c2, t):
    t = max(0.0, min(1.0, t))
    return tuple(c1[i] + (c2[i] - c1[i]) * t for i in range(len(c1)))


def rounded_rect_alpha(x, y, x0, y0, x1, y1, r):
    """1.0 inside a rounded rectangle, 0.0 outside (sample point test)."""
    if x < x0 or x > x1 or y < y0 or y > y1:
        return 0.0
    cx = min(max(x, x0 + r), x1 - r)
    cy = min(max(y, y0 + r), y1 - r)
    return 1.0 if (x - cx) ** 2 + (y - cy) ** 2 <= r * r else 0.0


def _seg(u, v, ax, ay, bx, by):
    """Returns (distance along segment 0..len, signed perpendicular distance, length)."""
    dx, dy = bx - ax, by - ay
    length = math.hypot(dx, dy)
    tx, ty = dx / length, dy / length
    along = (u - ax) * tx + (v - ay) * ty
    perp = -(u - ax) * ty + (v - ay) * tx
    return along, perp, length


# The Z: three strokes in a unit square (scaled by the caller).
STROKES = [((0.27, 0.28), (0.73, 0.28)), ((0.73, 0.28), (0.27, 0.72)), ((0.27, 0.72), (0.73, 0.72))]
HALF = 0.085      # half stroke width
TOOTH = 0.027     # tooth half-width across the stroke
STEP = 0.034      # tooth length along the stroke


def z_glyph(u, v, mono=False):
    """Colour of the zipper-Z at (u,v) in its own unit square, or None."""
    # Slider + pull tab at the start of the top stroke.
    if rounded_rect_alpha(u, v, 0.17, 0.215, 0.33, 0.345, 0.03):
        if mono:
            return WHITE
        return GOLD_DARK if not rounded_rect_alpha(u, v, 0.19, 0.235, 0.31, 0.325, 0.02) else GOLD
    if rounded_rect_alpha(u, v, 0.215, 0.33, 0.285, 0.47, 0.03):
        if mono:
            return WHITE if not rounded_rect_alpha(u, v, 0.235, 0.39, 0.265, 0.44, 0.012) else None
        if rounded_rect_alpha(u, v, 0.235, 0.39, 0.265, 0.44, 0.012):
            return None
        return GOLD
    hit = None
    for i, ((ax, ay), (bx, by)) in enumerate(STROKES):
        along, perp, length = _seg(u, v, ax, ay, bx, by)
        inside = -HALF <= perp <= HALF and 0 <= along <= length
        cap = math.hypot(u - ax, v - ay) <= HALF or math.hypot(u - bx, v - by) <= HALF
        if not (inside or cap):
            continue
        if mono:
            return WHITE
        col = WHITE
        if inside and abs(perp) <= TOOTH and 0.02 < along < length - 0.02:
            k = int(along / STEP) % 2
            col = GOLD if (k == 0) == (perp < 0) else GOLD_DARK
        hit = col if hit is None or col != WHITE else hit
    return hit


def tile_bg(u, v):
    c = mix(PURPLE, PINK, (u + (1 - v)) / 2 * 0.9 + 0.05)
    # soft top highlight
    if v < 0.5:
        c = mix(c, WHITE, (0.5 - v) * 0.16)
    return c


def shade(u, v):
    """Full icon: rounded tile + Z with a soft drop shadow."""
    if not rounded_rect_alpha(u, v, 0.03, 0.03, 0.97, 0.97, 0.22):
        return (0, 0, 0, 0)
    col = tile_bg(u, v)
    if z_glyph(u - 0.012, v - 0.022) is not None:          # shadow
        col = mix(col, SHADOW, 0.35)
    g = z_glyph(u, v)
    if g is not None:
        col = g
    return col + (1.0,)


def render_fn(fn, size, ss=4):
    rows = []
    for y in range(size):
        row = bytearray()
        for x in range(size):
            acc = [0.0, 0.0, 0.0, 0.0]
            for sy in range(ss):
                for sx in range(ss):
                    r, g, b, a = fn((x + (sx + 0.5) / ss) / size, (y + (sy + 0.5) / ss) / size)
                    acc[0] += r * a
                    acc[1] += g * a
                    acc[2] += b * a
                    acc[3] += a
            a = acc[3] / (ss * ss)
            if acc[3] > 0:
                r, g, b = (acc[i] / acc[3] for i in range(3))
            else:
                r = g = b = 0
            row += bytes((int(r * 255 + .5), int(g * 255 + .5), int(b * 255 + .5), int(a * 255 + .5)))
        rows.append(bytes(row))
    return rows


def render(size, ss=4):
    """The full app icon at the given size (rows of RGBA bytes)."""
    return render_fn(shade, size, ss)


def png_bytes(rows, size):
    raw = b"".join(b"\x00" + r for r in rows)

    def chunk(tag, data):
        c = struct.pack(">I", len(data)) + tag + data
        return c + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)

    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr)
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def ico_bytes(pngs):
    """pngs: list of (size, png_data). PNG-compressed ICO (Vista+)."""
    header = struct.pack("<HHH", 0, 1, len(pngs))
    offset = 6 + 16 * len(pngs)
    entries = b""
    blob = b""
    for size, data in pngs:
        s = 0 if size >= 256 else size
        entries += struct.pack("<BBBBHHII", s, s, 0, 0, 1, 32, len(data), offset + len(blob))
        blob += data
    return header + entries + blob


# Android adaptive icon: 108dp canvas, the launcher mask shows the centre 72dp and
# the logo must stay inside the 66dp safe zone -> glyph occupies the middle ~58%.
def _adaptive(u, v, mono=False):
    s, o = 0.68, 0.16
    g = z_glyph((u - o) / s, (v - o) / s, mono)
    if g is None:
        if not mono and z_glyph((u - o - 0.008) / s, (v - o - 0.014) / s) is not None:
            return SHADOW + (0.30,)
        return (0, 0, 0, 0)
    return g + (1.0,)


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else "."
    os.makedirs(out, exist_ok=True)
    pngs = {s: png_bytes(render(s), s) for s in (16, 24, 32, 48, 64, 256)}
    with open(os.path.join(out, "zipnest.ico"), "wb") as f:
        f.write(ico_bytes([(s, pngs[s]) for s in (16, 24, 32, 48, 64, 256)]))
    with open(os.path.join(out, "zipnest-256.png"), "wb") as f:
        f.write(pngs[256])
    n = 432
    with open(os.path.join(out, "ic_fg.png"), "wb") as f:
        f.write(png_bytes(render_fn(lambda u, v: _adaptive(u, v), n, 2), n))
    with open(os.path.join(out, "ic_mono.png"), "wb") as f:
        f.write(png_bytes(render_fn(lambda u, v: _adaptive(u, v, True), n, 2), n))
    with open(os.path.join(out, "ic_bg.png"), "wb") as f:
        f.write(png_bytes(render_fn(lambda u, v: tile_bg(u, v) + (1.0,), n, 1), n))
    print("icons written to", out)


if __name__ == "__main__":
    main()
