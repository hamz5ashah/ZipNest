#!/usr/bin/env python3
"""Normalises the AppxSignature.p7x entry written by osslsigncode: its local
header uses a data descriptor while the central directory says it does not,
which Windows' strict package reader may reject. The entry is re-written with
sizes in the local header (no descriptor) and the central directory/end
records are regenerated. The signed digests do not cover this entry, so the
signature stays valid.   usage: fix_signature_entry.py <package.msix>
"""
import struct, sys, zlib

path = sys.argv[1]
d = open(path, "rb").read()
i = d.rfind(b"PK\x06\x06")
n = struct.unpack("<Q", d[i + 24:i + 32])[0]
cd_off = struct.unpack("<Q", d[i + 48:i + 56])[0]

records, p = [], cd_off
for _ in range(n):
    fixed = struct.unpack("<IHHHHHHIIIHHHHHII", d[p:p + 46])
    nl, el, cl = fixed[10], fixed[11], fixed[12]
    records.append([list(fixed), d[p + 46:p + 46 + nl + el + cl]])
    p += 46 + nl + el + cl

last = records[-1]
name = last[1][:last[0][10]]
assert name == b"AppxSignature.p7x", "signature must be the last entry"
f = last[0]
sig_off = f[16]
lfh = struct.unpack("<IHHHHHIIIHH", d[sig_off:sig_off + 30])
data_start = sig_off + 30 + lfh[9] + lfh[10]
method, crc, csize, usize = f[4], f[7], f[8], f[9]
comp = d[data_start:data_start + csize]
plain = comp if method == 0 else zlib.decompress(comp, -15)
assert zlib.crc32(plain) & 0xFFFFFFFF == crc and len(plain) == usize

out = bytearray(d[:sig_off])
out += struct.pack("<IHHHHHIIIHH", 0x04034B50, 20, 0, method, lfh[4], lfh[5], crc, csize, usize, len(name), 0)
out += name + comp
f[3] = 0  # general purpose flags: no data descriptor
f[11] = 0  # no extra field
last[1] = name

cd_start = len(out)
for fixed, tail in records:
    fixed[11] = len(tail) - fixed[10] - fixed[12]
    out += struct.pack("<IHHHHHHIIIHHHHHII", *fixed) + tail
cd_size = len(out) - cd_start
z64 = len(out)
out += struct.pack("<IQHHIIQQQQ", 0x06064B50, 44, 45, 45, 0, 0, n, n, cd_size, cd_start)
out += struct.pack("<IIQI", 0x07064B50, 0, z64, 1)
out += struct.pack("<IHHHHIIH", 0x06054B50, 0, 0, 0xFFFF, 0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0)
open(path, "wb").write(out)
print("normalised", path)
