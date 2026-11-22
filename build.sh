#!/usr/bin/env bash
# Builds every ZipNest deliverable into dist/.
#   Windows: ZipNest-<v>-setup.exe installer, plus portable ZIPs (x64 and ARM64)
#   Android: ZipNest.apk (arm64, armv7, x86_64, x86)
# Requirements: Go 1.24+, Python 3, a JDK (17+), NSIS 3 (makensis), MinGW-w64 (x86_64-w64-mingw32-gcc),
# osslsigncode and openssl. No Android SDK or Windows SDK needed.
set -euo pipefail
cd "$(dirname "$0")"
VERSION=1.5.0

echo "== tests"
go test ./core/

echo "== Windows resources"
(cd cmd/zipnest && go run ../../tools/winres)
(cd cmd/zipnest-sfx && go run ../../tools/winres "ZipNest self-extracting archive" "zipnest-sfx.exe")

rm -rf dist && mkdir -p dist
for arch in amd64 arm64; do
  out=dist/ZipNest-$VERSION-windows-$arch
  mkdir -p "$out"
  echo "== Windows $arch"
  # Self-extractor stub first: ZipNest.exe embeds it (cmd/zipnest/sfx_windows.go).
  mkdir -p cmd/zipnest/sfx
  GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o cmd/zipnest/sfx/zipnest-sfx.exe ./cmd/zipnest-sfx
  GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o "$out/ZipNest.exe" ./cmd/zipnest
  GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$out/zipnest-cli.exe" ./cmd/zipnest-core
  cp README.md THIRD_PARTY_NOTICES.md "$out/"
  (cd dist && python3 -c "import shutil,sys; shutil.make_archive(sys.argv[1], 'zip', '.', sys.argv[1])" "$(basename "$out")")
done

echo "== Windows 11 context menu (shell extension + signed package)"
stage=dist/.installer-files
mkdir -p "$stage"
cp dist/ZipNest-$VERSION-windows-amd64/* "$stage/"
x86_64-w64-mingw32-gcc -shared -O2 -Wall -o "$stage/ZipNestShell.dll" shellext/zipnest_shell.c shellext/zipnest_shell.def \
  -lole32 -luuid -lshlwapi -lshell32 -ladvapi32 -static-libgcc
# Self-signed package-signing certificate (subject must match Publisher="CN=ZipNest"
# in installer/msix/AppxManifest.xml). Kept in installer/signing; keep it private.
key=installer/signing/zipnest-msix.key
cert=installer/signing/zipnest-msix.pem
if [ ! -f "$key" ]; then
  mkdir -p installer/signing
  openssl req -x509 -newkey rsa:3072 -sha256 -days 3650 -nodes -keyout "$key" -out "$cert" -subj "/CN=ZipNest" \
    -addext "extendedKeyUsage=codeSigning" -addext "basicConstraints=critical,CA:FALSE" \
    -addext "keyUsage=critical,digitalSignature" 2>/dev/null
  chmod 600 "$key"
fi
openssl x509 -in "$cert" -outform der -out "$stage/ZipNest.cer"
CERT_SHA1=$(openssl x509 -in "$cert" -noout -fingerprint -sha1 | cut -d= -f2 | tr -d :)
python3 installer/msix/make_msix.py "$VERSION.0" dist/.unsigned.msix
rm -f "$stage/ZipNestShell.msix"
osslsigncode sign -certs "$cert" -key "$key" -in dist/.unsigned.msix -out "$stage/ZipNestShell.msix" >/dev/null
python3 installer/msix/fix_signature_entry.py "$stage/ZipNestShell.msix"
osslsigncode verify -CAfile "$cert" -in "$stage/ZipNestShell.msix" | grep -q "Signature verification: ok"
rm -f dist/.unsigned.msix

echo "== Windows installer"
(cd installer && python3 make_bitmaps.py && makensis -V2 -DVERSION=$VERSION -DCERT_SHA1=$CERT_SHA1 \
  -DSRCDIR=../$stage -DOUTFILE=../dist/ZipNest-$VERSION-setup.exe zipnest.nsi)
rm -rf "$stage"

echo "== Android"
android/buildtools/fetch_tools.sh
python3 android/build.py
cp android/build/ZipNest.apk "dist/ZipNest-$VERSION.apk"

echo "== Linux command-line tool"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/zipnest-linux-amd64 ./cmd/zipnest-core

ls -la dist
