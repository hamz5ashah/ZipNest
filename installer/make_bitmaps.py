#!/usr/bin/env python3
"""Draws the installer artwork (24-bit BMPs) for the NSIS wizard:
welcome.bmp (164x314, welcome/finish pages) and header.bmp (150x57)."""
import os, struct, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "assets"))
from make_icons import render

def bmp(path, w, h, pixel):
    row_pad = (4 - (w * 3) % 4) % 4
    data = bytearray()
    for y in range(h - 1, -1, -1):           # bottom-up rows
        for x in range(w):
            r, g, b = pixel(x, y)
            data += bytes((b, g, r))
        data += b"\0" * row_pad
    header = struct.pack("<2sIHHI", b"BM", 54 + len(data), 0, 0, 54)
    info = struct.pack("<IiiHHIIiiII", 40, w, h, 1, 24, 0, len(data), 2835, 2835, 0, 0)
    open(path, "wb").write(header + info + data)

def mix(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))

def paste(icon_rows, size, ox, oy, base):
    def px(x, y):
        bg = base(x, y)
        ix, iy = x - ox, y - oy
        if 0 <= ix < size and 0 <= iy < size:
            p = icon_rows[iy][ix * 4: ix * 4 + 4]
            a = p[3] / 255
            return tuple(int(p[i] * a + bg[i] * (1 - a)) for i in range(3))
        return bg
    return px

INDIGO, TEAL = (28, 22, 64), (92, 34, 128)   # deep navy -> dark purple (the icon stands out)
out = os.path.dirname(os.path.abspath(__file__))

# Welcome / finish side panel.
icon = render(112)
def welcome_bg(x, y):
    t = (x / 164 * 0.35 + y / 314 * 0.65)
    c = mix(INDIGO, TEAL, t)
    # soft light circle behind the icon
    d = ((x - 82) ** 2 + (y - 120) ** 2) ** 0.5
    if d < 78:
        c = mix(c, (255, 255, 255), 0.10 * (1 - d / 78))
    return c
bmp(os.path.join(out, "welcome.bmp"), 164, 314, paste(icon, 112, 26, 64, welcome_bg))

# Header strip (right side of the page header).
small = render(48)
def header_bg(x, y):
    return (255, 255, 255)
bmp(os.path.join(out, "header.bmp"), 150, 57, paste(small, 48, 96, 4, header_bg))
print("installer bitmaps written")
