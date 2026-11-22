#!/usr/bin/env python3
"""Minimal replacement for aapt2 used by ZipNest's build.

* encode_manifest(tree) -> Android binary XML (AndroidManifest.xml)
* encode_arsc(package_id, package_name, drawables) -> resources.arsc

Only the features ZipNest needs are implemented: string, int, bool and
reference attribute values, and one 'drawable' resource type in the default
configuration.
"""
import struct

ANDROID_NS = "http://schemas.android.com/apk/res/android"

# android.R.attr ids (read from the Android 14 framework with javap)
ATTR_IDS = {
    "theme": 0x01010000, "label": 0x01010001, "icon": 0x01010002,
    "name": 0x01010003, "exported": 0x01010010, "authorities": 0x01010018,
    "grantUriPermissions": 0x0101001b, "launchMode": 0x0101001d,
    "configChanges": 0x0101001f, "mimeType": 0x01010026, "scheme": 0x01010027,
    "host": 0x01010028, "pathPattern": 0x0101002c, "minSdkVersion": 0x0101020c,
    "versionCode": 0x0101021b, "versionName": 0x0101021c,
    "windowSoftInputMode": 0x0101022b, "targetSdkVersion": 0x01010270,
    "maxSdkVersion": 0x01010271, "allowBackup": 0x01010280,
    "largeHeap": 0x0101035a, "requestLegacyExternalStorage": 0x01010603,
    "extractNativeLibs": 0x010104ea, "drawable": 0x01010199,
}

# Res_value data types
T_REF, T_STRING, T_INT_DEC, T_INT_HEX, T_BOOL = 0x01, 0x03, 0x10, 0x11, 0x12


class Ref(int):
    """Marks an attribute value as a resource reference (@id)."""


class Hex(int):
    """Marks an attribute value as a hexadecimal int (used for flags)."""


def _pad4(b):
    return b + b"\x00" * ((4 - len(b) % 4) % 4)


def string_pool(strings):
    """UTF-16 ResStringPool chunk."""
    offsets = []
    data = b""
    for s in strings:
        offsets.append(len(data))
        enc = s.encode("utf-16-le")
        n = len(s.encode("utf-16-le")) // 2
        if n > 0x7FFF:
            data += struct.pack("<HH", 0x8000 | (n >> 16), n & 0xFFFF)
        else:
            data += struct.pack("<H", n)
        data += enc + b"\x00\x00"
    data = _pad4(data)
    header_size = 28
    strings_start = header_size + 4 * len(strings)
    size = strings_start + len(data)
    return (struct.pack("<HHIIIIII", 0x0001, header_size, size, len(strings), 0, 0,
                        strings_start, 0)
            + b"".join(struct.pack("<I", o) for o in offsets) + data)


class Element:
    def __init__(self, tag, attrs=None, children=None):
        self.tag = tag
        self.attrs = attrs or {}          # "android:name" or "package" -> value
        self.children = children or []


def encode_manifest(root):
    # Collect attribute names that carry resource ids (must come first in the pool).
    res_names = []
    other = []

    def walk(e):
        for k in e.attrs:
            if k.startswith("android:"):
                n = k[8:]
                if n not in ATTR_IDS:
                    raise KeyError("unknown android attribute: " + n)
                if n not in res_names:
                    res_names.append(n)
            elif k not in other:
                other.append(k)
        if e.tag not in other:
            other.append(e.tag)
        for c in e.children:
            walk(c)
        for v in e.attrs.values():
            if isinstance(v, str) and v not in other:
                other.append(v)

    walk(root)
    res_names.sort(key=lambda n: ATTR_IDS[n])
    strings = list(res_names)
    for s in ["android", ANDROID_NS] + other:
        if s not in strings:
            strings.append(s)
    idx = {s: i for i, s in enumerate(strings)}

    pool = string_pool(strings)
    resmap_body = b"".join(struct.pack("<I", ATTR_IDS[n]) for n in res_names)
    resmap = struct.pack("<HHI", 0x0180, 8, 8 + len(resmap_body)) + resmap_body

    nodes = []
    ns_prefix, ns_uri = idx["android"], idx[ANDROID_NS]
    nodes.append(struct.pack("<HHIIIII", 0x0100, 16, 24, 1, 0xFFFFFFFF, ns_prefix, ns_uri))

    def attr_record(k, v):
        if k.startswith("android:"):
            ns, name = ns_uri, idx[k[8:]]
        else:
            ns, name = 0xFFFFFFFF, idx[k]
        raw = 0xFFFFFFFF
        if isinstance(v, bool):
            typ, data = T_BOOL, 0xFFFFFFFF if v else 0
        elif isinstance(v, Ref):
            typ, data = T_REF, int(v)
        elif isinstance(v, Hex):
            typ, data = T_INT_HEX, int(v)
        elif isinstance(v, int):
            typ, data = T_INT_DEC, v & 0xFFFFFFFF
        else:
            typ, data = T_STRING, idx[v]
            raw = idx[v]
        sort_key = ATTR_IDS[k[8:]] if k.startswith("android:") else 0xFFFFFFFF
        rec = struct.pack("<IIIHBBI", ns, name, raw, 8, 0, typ, data)
        return sort_key, rec

    line = [1]

    def emit(e):
        recs = sorted((attr_record(k, v) for k, v in e.attrs.items()), key=lambda r: r[0])
        body = struct.pack("<IIHHHHHH", 0xFFFFFFFF, idx[e.tag], 20, 20, len(recs), 0, 0, 0)
        body += b"".join(r for _, r in recs)
        nodes.append(struct.pack("<HHIII", 0x0102, 16, 16 + len(body), line[0], 0xFFFFFFFF) + body)
        line[0] += 1
        for c in e.children:
            emit(c)
        nodes.append(struct.pack("<HHIIIII", 0x0103, 16, 24, line[0], 0xFFFFFFFF,
                                 0xFFFFFFFF, idx[e.tag]))
        line[0] += 1

    emit(root)
    nodes.append(struct.pack("<HHIIIII", 0x0101, 16, 24, line[0], 0xFFFFFFFF, ns_prefix, ns_uri))

    body = pool + resmap + b"".join(nodes)
    return struct.pack("<HHI", 0x0003, 8, 8 + len(body)) + body


def encode_arsc(package_id, package_name, drawables):
    """drawables: list of (entry_name, path_in_apk). Type id 1 = 'drawable'.
    Resource id of entry i is (package_id << 24) | (1 << 16) | i."""
    values = string_pool([p for _, p in drawables])
    type_strings = string_pool(["drawable"])
    key_strings = string_pool([n for n, _ in drawables])
    count = len(drawables)

    spec = struct.pack("<HHIBBHI", 0x0202, 16, 16 + 4 * count, 1, 0, 0, count)
    spec += b"\x00\x00\x00\x00" * count

    config = struct.pack("<I", 64) + b"\x00" * 60          # default configuration
    header_size = 20 + len(config)
    entries = b""
    offsets = b""
    for i in range(count):
        offsets += struct.pack("<I", len(entries))
        entries += struct.pack("<HHI", 8, 0, i)                    # ResTable_entry
        entries += struct.pack("<HBBI", 8, 0, T_STRING, i)         # Res_value -> string i
    entries_start = header_size + len(offsets)
    tchunk = struct.pack("<HHIBBHII", 0x0201, header_size, entries_start + len(entries),
                         1, 0, 0, count, entries_start) + config + offsets + entries

    name16 = package_name.encode("utf-16-le")[:254]
    name16 += b"\x00" * (256 - len(name16))
    pkg_header_size = 288
    type_off = pkg_header_size
    key_off = type_off + len(type_strings)
    pkg_body = type_strings + key_strings + spec + tchunk
    pkg = struct.pack("<HHII", 0x0200, pkg_header_size, pkg_header_size + len(pkg_body), package_id)
    pkg += name16 + struct.pack("<IIIII", type_off, 1, key_off, count, 0) + pkg_body

    body = values + pkg
    return struct.pack("<HHII", 0x0002, 12, 12 + len(body), 1) + body
