#!/usr/bin/env python3
"""Builds the (unsigned) sparse MSIX package ZipNestShell.msix.

Layout follows Microsoft's packaging rules: stored (uncompressed) ZIP entries,
AppxBlockMap.xml with SHA-256 hashes of 64 KiB blocks, [Content_Types].xml,
and ZIP64 end records. Sign the result afterwards (osslsigncode).

usage: make_msix.py <version x.y.z.w> <out.msix>
"""
import base64, hashlib, os, struct, sys, zlib

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "..", "assets"))
from make_icons import render, png_bytes  # noqa: E402

BLOCK = 64 * 1024


class Writer:
    def __init__(self, path):
        self.f = open(path, "wb")
        self.entries = []   # (name, crc, size, offset)

    def add(self, name, data):
        """Stored entry, sizes known up front (no data descriptor)."""
        nb = name.encode("utf-8")
        crc = zlib.crc32(data) & 0xFFFFFFFF
        off = self.f.tell()
        # version 20, no flags, method 0, fixed DOS date 1980-01-01
        lfh = struct.pack("<IHHHHHIIIHH", 0x04034B50, 20, 0, 0, 0, 0x21, crc, len(data), len(data), len(nb), 0)
        self.f.write(lfh + nb + data)
        self.entries.append((nb, crc, len(data), off))
        return len(lfh) + len(nb)

    def close(self):
        cd_start = self.f.tell()
        for nb, crc, size, off in self.entries:
            self.f.write(struct.pack("<IHHHHHHIIIHHHHHII", 0x02014B50, 45, 20, 0, 0, 0, 0x21, crc, size, size,
                                     len(nb), 0, 0, 0, 0, 0, off) + nb)
        cd_size = self.f.tell() - cd_start
        z64 = self.f.tell()
        n = len(self.entries)
        self.f.write(struct.pack("<IQHHIIQQQQ", 0x06064B50, 44, 45, 45, 0, 0, n, n, cd_size, cd_start))
        self.f.write(struct.pack("<IIQI", 0x07064B50, 0, z64, 1))
        self.f.write(struct.pack("<IHHHHIIH", 0x06054B50, 0, 0, 0xFFFF, 0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0))
        self.f.close()


def png(size):
    return png_bytes(render(size), size)


def main():
    version, out = sys.argv[1], sys.argv[2]
    manifest = open(os.path.join(HERE, "AppxManifest.xml"), encoding="utf-8").read().replace("@VERSION@", version)
    files = [
        ("Assets/Square44x44Logo.png", png(44)),
        ("Assets/Square150x150Logo.png", png(150)),
        ("Assets/StoreLogo.png", png(50)),
        ("AppxManifest.xml", manifest.encode("utf-8")),
    ]
    w = Writer(out)
    blockmap = ['<?xml version="1.0" encoding="UTF-8" standalone="no"?>',
                '<BlockMap xmlns="http://schemas.microsoft.com/appx/2010/blockmap" '
                'HashMethod="http://www.w3.org/2001/04/xmlenc#sha256">']
    for name, data in files:
        lfh = w.add(name, data)
        blockmap.append('<File Name="%s" Size="%d" LfhSize="%d">' % (name.replace("/", "\\"), len(data), lfh))
        for i in range(0, max(len(data), 1), BLOCK):
            h = base64.b64encode(hashlib.sha256(data[i:i + BLOCK]).digest()).decode()
            blockmap.append('<Block Hash="%s"/>' % h)
        blockmap.append("</File>")
    blockmap.append("</BlockMap>")
    w.add("AppxBlockMap.xml", "".join(blockmap).encode("utf-8"))
    content_types = ('<?xml version="1.0" encoding="UTF-8"?>'
                     '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
                     '<Default Extension="png" ContentType="image/png"/>'
                     '<Override PartName="/AppxManifest.xml" ContentType="application/vnd.ms-appx.manifest+xml"/>'
                     '<Override PartName="/AppxBlockMap.xml" ContentType="application/vnd.ms-appx.blockmap+xml"/>'
                     '<Override PartName="/AppxSignature.p7x" ContentType="application/vnd.ms-appx.signature"/>'
                     '</Types>')
    w.add("[Content_Types].xml", content_types.encode("utf-8"))
    w.close()
    print("wrote", out)


if __name__ == "__main__":
    main()
