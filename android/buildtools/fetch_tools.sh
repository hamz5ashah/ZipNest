#!/usr/bin/env bash
# Downloads the build-time jars (not shipped in the APK except via libs/) from Maven Central.
#   android-all.jar : Android 14 framework classes (Robolectric build) used as the compile bootclasspath
#   dx.jar          : the DEX compiler
#   apksig.jar      : Google's APK signing library
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p cache
M=https://repo1.maven.org/maven2
get() {
  local out=$1 path=$2
  [ -s "cache/$out" ] && unzip -tq "cache/$out" >/dev/null 2>&1 && { echo "have $out"; return; }
  for i in 1 2 3 4 5 6; do
    if curl -fsSL -o "cache/$out" "$M/$path" && unzip -tq "cache/$out" >/dev/null 2>&1; then echo "got $out"; return; fi
    sleep $((i * 5))
  done
  echo "failed to download $out" >&2; exit 1
}
get android-all.jar org/robolectric/android-all/14-robolectric-10818077/android-all-14-robolectric-10818077.jar
get dx.jar          com/jakewharton/android/repackaged/dalvik-dx/16.0.1/dalvik-dx-16.0.1.jar
get apksig.jar      com/android/tools/build/apksig/2.3.0/apksig-2.3.0.jar
