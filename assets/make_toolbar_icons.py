#!/usr/bin/env python3
"""Draws ZipNest's toolbar icons (simple original glyphs) as 32x32 PNGs.
usage: make_toolbar_icons.py <out_dir>"""
import math, os, sys
from make_icons import png_bytes, rounded_rect_alpha, shade, render as icon_render

def circle(u, v, cx, cy, r): return (u-cx)**2 + (v-cy)**2 <= r*r
def seg_dist(u, v, ax, ay, bx, by):
    dx, dy = bx-ax, by-ay
    t = max(0, min(1, ((u-ax)*dx + (v-ay)*dy) / (dx*dx+dy*dy)))
    return math.hypot(u-(ax+t*dx), v-(ay+t*dy))
def poly(u, v, pts):
    inside = False
    j = len(pts)-1
    for i in range(len(pts)):
        xi, yi = pts[i]; xj, yj = pts[j]
        if (yi > v) != (yj > v) and u < (xj-xi)*(v-yi)/(yj-yi)+xi: inside = not inside
        j = i
    return inside

def box(u, v, s=1.0, ox=0, oy=0):
    """A small ZipNest logo tile (used by the Add / Extract Here glyphs)."""
    r, g, b, a = shade((u - ox) / s, (v - oy) / s)
    return (r, g, b) if a > 0 else None

def add(u, v):
    if circle(u, v, 0.74, 0.74, 0.24):
        if circle(u, v, 0.74, 0.74, 0.21):
            if (abs(u-0.74) < 0.04 and abs(v-0.74) < 0.14) or (abs(v-0.74) < 0.04 and abs(u-0.74) < 0.14): return (1,1,1)
            return (0.16,0.62,0.30)
        return (0.08,0.40,0.18)
    return box(u, v, 0.85)

def extract(u, v):
    # arrow
    if poly(u, v, [(0.58,0.06),(0.80,0.06),(0.80,0.40),(0.94,0.40),(0.69,0.66),(0.44,0.40),(0.58,0.40)]):
        return (0.15,0.45,0.85)
    # folder
    if rounded_rect_alpha(u, v, 0.04, 0.36, 0.30, 0.48, 0.03): return (0.85,0.62,0.10)
    if rounded_rect_alpha(u, v, 0.04, 0.42, 0.86, 0.92, 0.04):
        return (0.98,0.78,0.25) if v > 0.52 else (0.92,0.68,0.15)
    return None

def test(u, v):
    if circle(u, v, 0.5, 0.5, 0.44):
        if seg_dist(u, v, 0.28, 0.52, 0.44, 0.68) < 0.07 or seg_dist(u, v, 0.44, 0.68, 0.74, 0.34) < 0.07: return (1,1,1)
        return (0.16,0.62,0.30) if circle(u, v, 0.5, 0.5, 0.40) else (0.08,0.40,0.18)
    return None

def view(u, v):
    if seg_dist(u, v, 0.62, 0.62, 0.88, 0.88) < 0.08: return (0.40,0.30,0.20)
    if circle(u, v, 0.42, 0.42, 0.32):
        if circle(u, v, 0.42, 0.42, 0.25):
            return (0.78,0.90,1.0) if not circle(u, v, 0.34, 0.34, 0.07) else (1,1,1)
        return (0.25,0.30,0.45)
    return None

def delete(u, v):
    if seg_dist(u, v, 0.18, 0.18, 0.82, 0.82) < 0.11 or seg_dist(u, v, 0.82, 0.18, 0.18, 0.82) < 0.11:
        return (0.85,0.18,0.18)
    return None

def info(u, v):
    if circle(u, v, 0.5, 0.5, 0.44):
        if circle(u, v, 0.5, 0.27, 0.07) or (abs(u-0.5) < 0.06 and 0.40 < v < 0.78): return (1,1,1)
        return (0.18,0.45,0.85) if circle(u, v, 0.5, 0.5, 0.40) else (0.10,0.28,0.60)
    return None

def up(u, v):
    if poly(u, v, [(0.5,0.08),(0.90,0.50),(0.66,0.50),(0.66,0.92),(0.34,0.92),(0.34,0.50),(0.10,0.50)]):
        return (0.20,0.60,0.75)
    return None

def wizard(u, v):  # "Extract here"-style: box with a small arrow
    if poly(u, v, [(0.70,0.52),(0.92,0.52),(0.92,0.70),(0.98,0.70),(0.81,0.95),(0.64,0.70),(0.70,0.70)]):
        return (0.15,0.45,0.85)
    return box(u, v, 0.80)

def find(u, v):  # binoculars-like: magnifier over a document
    if seg_dist(u, v, 0.66, 0.66, 0.90, 0.90) < 0.07: return (0.40,0.30,0.20)
    if circle(u, v, 0.50, 0.50, 0.26):
        if circle(u, v, 0.50, 0.50, 0.20):
            return (0.78,0.90,1.0) if not circle(u, v, 0.43, 0.43, 0.05) else (1,1,1)
        return (0.25,0.30,0.45)
    if rounded_rect_alpha(u, v, 0.08, 0.06, 0.62, 0.80, 0.04):
        if 0.16 <= u <= 0.52 and any(abs(v - y) < 0.022 for y in (0.18, 0.28, 0.38)): return (0.55,0.58,0.70)
        return (1,1,1) if rounded_rect_alpha(u, v, 0.11, 0.09, 0.59, 0.77, 0.03) else (0.45,0.47,0.60)
    return None

def convert(u, v):  # two arrows in a circle
    r = math.hypot(u - 0.5, v - 0.5)
    a = math.atan2(v - 0.5, u - 0.5)
    if 0.25 < r < 0.38:
        if -2.6 < a < -0.3: return (0.16,0.62,0.30)
        if 0.55 < a < 2.85: return (0.15,0.45,0.85)
    if poly(u, v, [(0.70,0.10),(0.95,0.36),(0.62,0.42)]): return (0.16,0.62,0.30)
    if poly(u, v, [(0.30,0.90),(0.05,0.64),(0.38,0.58)]): return (0.15,0.45,0.85)
    return box(u, v, 0.42, 0.29, 0.29)

def repair(u, v):  # wrench over a mini archive tile
    steel, dark = (0.42, 0.48, 0.62), (0.24, 0.28, 0.40)
    head = circle(u, v, 0.74, 0.26, 0.20) and not circle(u, v, 0.84, 0.16, 0.11)
    handle = seg_dist(u, v, 0.30, 0.70, 0.68, 0.32) < 0.075
    if head or handle:
        edge = not (circle(u, v, 0.74, 0.26, 0.17) or seg_dist(u, v, 0.30, 0.70, 0.68, 0.32) < 0.05)
        if circle(u, v, 0.84, 0.16, 0.13) and not circle(u, v, 0.84, 0.16, 0.11):
            return dark
        return dark if edge else steel
    if circle(u, v, 0.27, 0.73, 0.075):
        return dark
    return box(u, v, 0.62, 0.0, 0.38)

def render(fn, size=32, ss=4):
    rows = []
    for y in range(size):
        row = bytearray()
        for x in range(size):
            acc = [0.0]*4
            for sy in range(ss):
                for sx in range(ss):
                    c = fn((x+(sx+.5)/ss)/size, (y+(sy+.5)/ss)/size)
                    if c:
                        acc[0] += c[0]; acc[1] += c[1]; acc[2] += c[2]; acc[3] += 1
            a = acc[3]/(ss*ss)
            r, g, b = ((acc[i]/acc[3]) if acc[3] else 0 for i in range(3))
            row += bytes((int(r*255+.5), int(g*255+.5), int(b*255+.5), int(a*255+.5)))
        rows.append(bytes(row))
    return png_bytes(rows, size)

if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else "toolbar"
    os.makedirs(out, exist_ok=True)
    for name, fn in [("add", add), ("extract", extract), ("test", test), ("view", view),
                     ("delete", delete), ("info", info), ("up", up), ("extracthere", wizard),
                     ("find", find), ("convert", convert), ("repair", repair)]:
        open(os.path.join(out, name + ".png"), "wb").write(render(fn))
    open(os.path.join(out, "up16.png"), "wb").write(render(up, 16))
    back = lambda u, v: up(v, 1 - u)          # arrow pointing left
    fwd = lambda u, v: up(1 - v, u)           # arrow pointing right
    open(os.path.join(out, "back16.png"), "wb").write(render(back, 16))
    open(os.path.join(out, "fwd16.png"), "wb").write(render(fwd, 16))
    open(os.path.join(out, "logo32.png"), "wb").write(png_bytes(icon_render(32), 32))
    open(os.path.join(out, "logo48.png"), "wb").write(png_bytes(icon_render(48), 48))
    print("toolbar icons written to", out)
