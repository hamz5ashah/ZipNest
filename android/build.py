#!/usr/bin/env python3
"""Builds ZipNest.apk without the Android SDK / Gradle.

Pipeline: archive engine (Go) cross-compiled per ABI -> lib/<abi>/libzipnest.so
          binary manifest + resources.arsc (buildtools/axml.py)
          -> javac (against the Android 14 framework jar)
          -> dx (Java bytecode -> classes.dex)
          -> zip -> apksig (v1 + v2 signature, verification)

Run buildtools/fetch_tools.sh once to download the tool jars.
"""
import glob
import os
import shutil
import subprocess
import sys
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "buildtools"))
from axml import Element as E, Ref, Hex, encode_manifest, encode_arsc  # noqa: E402

PKG = "com.zipnest.app"
VERSION_CODE = 7
VERSION_NAME = "2.0.0"
MIN_SDK = 26
TARGET_SDK = 34
THEME = 16974401  # android.R.style.Theme_Material_Light_NoActionBar

CACHE = os.path.join(HERE, "buildtools", "cache")
ANDROID_JAR = os.path.join(CACHE, "android-all.jar")
DX_JAR = os.path.join(CACHE, "dx.jar")
APKSIG_JAR = os.path.join(CACHE, "apksig.jar")
OUT = os.path.join(HERE, "build")

# Signing key settings; override with environment variables (e.g. GitHub secrets).
KEY_PASSWORD = os.environ.get("ANDROID_KEYSTORE_PASSWORD") or "zipnest"
KEY_ALIAS = os.environ.get("ANDROID_KEY_ALIAS") or "zipnest"


def manifest():
    mimes = ["application/zip", "application/x-zip-compressed", "application/vnd.rar",
             "application/x-rar-compressed", "application/rar", "application/x-rar",
             "application/x-7z-compressed", "application/x-tar", "application/gzip",
             "application/x-gzip", "application/x-bzip2", "application/x-xz",
             "application/x-compressed-tar"]
    view_filter = E("intent-filter", {}, [
        E("action", {"android:name": "android.intent.action.VIEW"}),
        E("category", {"android:name": "android.intent.category.DEFAULT"}),
        E("data", {"android:scheme": "content"}),
        E("data", {"android:scheme": "file"}),
    ] + [E("data", {"android:mimeType": m}) for m in mimes])

    return E("manifest", {
        "package": PKG,
        "android:versionCode": VERSION_CODE,
        "android:versionName": VERSION_NAME,
    }, [
        E("uses-sdk", {"android:minSdkVersion": MIN_SDK, "android:targetSdkVersion": TARGET_SDK}),
        E("uses-permission", {"android:name": "android.permission.MANAGE_EXTERNAL_STORAGE"}),
        E("uses-permission", {"android:name": "android.permission.READ_EXTERNAL_STORAGE",
                              "android:maxSdkVersion": 32}),
        E("uses-permission", {"android:name": "android.permission.WRITE_EXTERNAL_STORAGE",
                              "android:maxSdkVersion": 29}),
        E("application", {
            "android:label": "ZipNest",
            "android:icon": Ref(0x7F010000),
            "android:theme": Ref(THEME),
            "android:largeHeap": True,
            "android:allowBackup": False,
            "android:requestLegacyExternalStorage": True,
            "android:extractNativeLibs": True,   # the engine must exist as a real executable file
            "android:usesCleartextTraffic": True,  # the interface is served by the engine on 127.0.0.1
        }, [
            E("activity", {
                "android:name": PKG + ".MainActivity",
                "android:exported": True,
                "android:launchMode": 1,                 # singleTop
                "android:configChanges": Hex(0x04A0),    # keyboardHidden|orientation|screenSize
                "android:windowSoftInputMode": Hex(0x10),  # adjustResize
            }, [
                E("intent-filter", {}, [
                    E("action", {"android:name": "android.intent.action.MAIN"}),
                    E("category", {"android:name": "android.intent.category.LAUNCHER"}),
                ]),
                view_filter,
            ]),
            E("provider", {
                "android:name": PKG + ".CacheProvider",
                "android:authorities": PKG + ".files",
                "android:exported": False,
                "android:grantUriPermissions": True,
            }),
        ]),
    ])


# Go target for each Android ABI. arm64 uses Go's native android port; the
# others are static Linux executables, which Android runs as well.
ABIS = {
    "arm64-v8a": {"GOOS": "android", "GOARCH": "arm64"},
    "armeabi-v7a": {"GOOS": "linux", "GOARCH": "arm", "GOARM": "7"},
    "x86_64": {"GOOS": "linux", "GOARCH": "amd64"},
    "x86": {"GOOS": "linux", "GOARCH": "386"},
}


def build_engine():
    out = {}
    for abi, env in ABIS.items():
        dst = os.path.join(OUT, "lib", abi, "libzipnest.so")
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        e = dict(os.environ, CGO_ENABLED="0", **env)
        run(["go", "build", "-trimpath", "-ldflags", "-s -w", "-o", dst, "./cmd/zipnest-core"],
            cwd=os.path.join(HERE, ".."), env=e)
        out[abi] = dst
    return out


def run(cmd, **kw):
    print("+", " ".join(cmd[:6]), "..." if len(cmd) > 6 else "")
    subprocess.run(cmd, check=True, **kw)


def main():
    for f in (ANDROID_JAR, DX_JAR, APKSIG_JAR):
        if not os.path.exists(f):
            sys.exit("missing %s - run buildtools/fetch_tools.sh first" % f)
    shutil.rmtree(OUT, ignore_errors=True)
    classes = os.path.join(OUT, "classes")
    os.makedirs(classes)

    engines = build_engine()
    sources = sorted(glob.glob(os.path.join(HERE, "src", "**", "*.java"), recursive=True))
    # --release 8 limits java.* to the Java 8 API (what Android supports); android.* comes from the framework jar.
    run(["javac", "-nowarn", "-Xlint:-options", "-encoding", "UTF-8", "--release", "8",
         "-classpath", ANDROID_JAR, "-d", classes] + sources)

    dex = os.path.join(OUT, "classes.dex")
    run(["java", "-cp", DX_JAR, "com.android.dx.command.Main", "--dex",
         "--min-sdk-version=%d" % MIN_SDK, "--output=" + dex, classes])

    unsigned = os.path.join(OUT, "unsigned.apk")
    with zipfile.ZipFile(unsigned, "w") as z:
        z.writestr("AndroidManifest.xml", encode_manifest(manifest()), zipfile.ZIP_DEFLATED)
        z.write(dex, "classes.dex", zipfile.ZIP_DEFLATED)
        # Adaptive launcher icon (Android 8+): background + foreground layers, plus a
        # monochrome layer for Android 13 themed icons. ic_launcher must stay entry 0
        # (resource id 0x7F010000, referenced by the manifest).
        drawables = [("ic_launcher", "res/drawable/ic_launcher.xml"),
                     ("ic_launcher_fg", "res/drawable/ic_launcher_fg.png"),
                     ("ic_launcher_bg", "res/drawable/ic_launcher_bg.png"),
                     ("ic_launcher_mono", "res/drawable/ic_launcher_mono.png")]
        z.writestr("resources.arsc", encode_arsc(0x7F, PKG, drawables), zipfile.ZIP_STORED)
        z.writestr("res/drawable/ic_launcher.xml", encode_manifest(E("adaptive-icon", {}, [
            E("background", {"android:drawable": Ref(0x7F010002)}),
            E("foreground", {"android:drawable": Ref(0x7F010001)}),
            E("monochrome", {"android:drawable": Ref(0x7F010003)}),
        ])), zipfile.ZIP_DEFLATED)
        assets = os.path.join(HERE, "..", "assets")
        for name, src in (("fg", "ic_fg.png"), ("bg", "ic_bg.png"), ("mono", "ic_mono.png")):
            z.write(os.path.join(assets, src), "res/drawable/ic_launcher_%s.png" % name, zipfile.ZIP_STORED)
        for abi, path in engines.items():
            z.write(path, "lib/%s/libzipnest.so" % abi, zipfile.ZIP_DEFLATED)
        z.write(os.path.join(HERE, "..", "THIRD_PARTY_NOTICES.md"), "assets/THIRD_PARTY_NOTICES.md",
                zipfile.ZIP_DEFLATED)

    keystore = os.path.join(HERE, "buildtools", "debug-signing.p12")
    if not os.path.exists(keystore):
        run(["keytool", "-genkeypair", "-keystore", keystore, "-storetype", "PKCS12",
             "-storepass", KEY_PASSWORD, "-keypass", KEY_PASSWORD, "-alias", KEY_ALIAS,
             "-keyalg", "RSA", "-keysize", "2048", "-validity", "10000",
             "-dname", "CN=ZipNest Debug, O=ZipNest"])

    signer_classes = os.path.join(OUT, "signer")
    os.makedirs(signer_classes)
    run(["javac", "-nowarn", "-cp", APKSIG_JAR, "-d", signer_classes,
         os.path.join(HERE, "buildtools", "Signer.java")])
    apk = os.path.join(OUT, "ZipNest.apk")
    run(["java", "--add-exports", "java.base/sun.security.x509=ALL-UNNAMED",
         "--add-exports", "java.base/sun.security.pkcs=ALL-UNNAMED",
         "--add-exports", "java.base/sun.security.util=ALL-UNNAMED",
         "-cp", os.pathsep.join([APKSIG_JAR, signer_classes]), "Signer",
         keystore, KEY_PASSWORD, KEY_ALIAS, unsigned, apk])
    print("APK:", apk, os.path.getsize(apk), "bytes")


if __name__ == "__main__":
    main()
