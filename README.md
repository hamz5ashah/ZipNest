# ZipNest

A free archive manager for **Windows** and **Android**, written from scratch.
It works like a classic archiver: browse folders, open archives like folders, and
add, extract, test and delete files, with password support.

| | Windows (`ZipNest.exe`) | Android (`ZipNest.apk`) |
|---|---|---|
| Open / extract | ZIP (incl. AES & ZipCrypto passwords), **RAR4 & RAR5** (incl. encrypted, solid, multi-volume), 7z (incl. AES), TAR, TAR.GZ/BZ2/XZ/ZST, GZ, BZ2, XZ, ZST | same |
| Create | ZIP and **7z** (optional **AES-256** password; 7z can hide file names), TAR, TAR.GZ, TAR.XZ, TAR.ZST; split volumes; self-extracting .exe (Windows) | same (no .exe) |
| Add to an existing ZIP, delete from ZIP/TAR | ✓ | delete ✓ |
| Test integrity (CRC / HMAC) | ✓ | ✓ |
| Browse inside archives, open a file directly | ✓ | ✓ |
| Explorer right-click menu ("Add to archive", "Extract Here", ...) | ✓ (Options menu) | — |
| Open archives from other apps (Files, browser, chat apps) | — | ✓ |
| Command line | `zipnest-cli.exe` / `ZipNest.exe x file.rar` | — |

### What's new in 1.5 — ZipNest's own format: `.nest`

A modern archive format with the features that make RAR popular:

- **Solid compression** with Zstandard on all CPU cores (files grouped by type).
  In our test it was faster *and* smaller than ZIP and our 7z: 20 MB in 0.17 s.
- **AES-256-GCM** encryption with an **Argon2id** password key; optional **hidden
  file names**; every block is authenticated (wrong passwords and tampering are detected).
- **Recovery record** (Reed–Solomon, 1–10 %) and a **Repair** command: a damaged
  archive is rebuilt byte-for-byte as long as the damage is within the recovery size.
- **Lock** an archive, add a **comment**, split into **volumes**.
- Windows (Add dialog, toolbar *Repair*, Explorer menus, `.nest` file association),
  Android (create, open, repair) and command line (`-rr5`, `--lock`, `--comment=`, `r`).

`.nest` archives open only in ZipNest. To share files with people who don't have
ZipNest, use ZIP or 7z, or send a self-extracting .exe.

### What's new in 1.4 — a cleaner, friendlier look

- **Windows:** navigation pane (Home, Desktop, Documents, Downloads, drives, recent
  archives), Back / Forward / Up buttons, a live *Filter this list* box, an archive info
  banner (format, files, size, ratio, password) with *Extract all / Test / Close*
  buttons, a *Ratio* column, Segoe UI font throughout, progress with speed and time
  left, and a new About window.
- **Android:** crisp drawn file icons with type badges (PDF, JPG, MP3, ZIP…) that look
  the same on every phone, tappable breadcrumb path, an archive info card with
  *Extract all*, and icon buttons in the bottom bar.

### What's new in 1.3

- **Create 7z archives**, the format WinRAR can only read: LZMA2, multi-core (solid
  blocks compressed in parallel), optional **AES-256** and **encrypted file names**.
  Archives are verified with the official 7-Zip.
- **Split into volumes** (10 MB … 25 GB presets, or `-v100m` on the command line) for
  every format, and **open split sets** (`.001`, `.002` …) of ZIP, 7z and TAR.
- **Self-extracting archives (.exe)**: a 2 MB stub with a destination picker,
  password prompt, progress bar and silent mode (`archive.exe -s -d C:\target`).
- **Convert** archives to another format (e.g. RAR → 7z, ZIP → TAR.XZ) from the
  toolbar, the right-click menu (classic and Windows 11) or `zipnest c in.rar out.7z`.
- **Find** files by name (wildcards) inside the open archive or below a folder.
- Android: create 7z (with hidden names), split into parts, convert archives.

### ZipNest vs. WinRAR

| | ZipNest 1.3 | WinRAR |
|---|---|---|
| Price | Free, no nag screen | Paid (trial nag) |
| Create ZIP / 7z / TAR.GZ/XZ/ZST | ✅ / ✅ / ✅ | ✅ / ❌ / ❌ |
| Own format with recovery record + repair | ✅ `.nest` | ✅ `.rar` |
| Create RAR | ❌ (RAR is licensed to WinRAR only) | ✅ |
| Open RAR4/RAR5, 7z, ZIP, TAR, GZ, BZ2, XZ, ZST | ✅ | ✅ |
| AES-256 passwords, hidden file names | ✅ (ZIP, 7z) | ✅ (RAR, ZIP) |
| Multi-core ZIP compression | ✅ | ❌ (ZIP is single-threaded) |
| Split volumes, self-extracting .exe, convert, find | ✅ | ✅ |
| Windows 11 top-level menu | ✅ | ✅ |
| Android app with the same engine | ✅ | separate app |

### What's new in 1.2

- **Windows 11 context menu:** a "ZipNest ›" submenu directly in the new Windows 11
  menu (no "Show more options" needed), with WinRAR-style entries that adapt to the
  selection: *Open with ZipNest, Extract files..., Extract to "name\\", Extract Here,
  Test archive* for archives and *Add to archive..., Add to "name.zip"* for other files.
- **ZipNest Setup dialog** (shown by the installer, and later via *Options → Settings*):
  choose which archive types to associate (RAR, ZIP, 7Z, TAR, GZ, BZ2, XZ, ZST, ZIPX, 001,
  JAR, CBR/CBZ), desktop / Start Menu shortcuts, and shell integration
  (cascaded menu, icons in menus, Windows 11 menu).
- New right-click actions: *Extract files...* (choose a folder) and *Test archive*.
- Split archives (`.001`) open directly.
- Fix: a crash when an operation finished in a few milliseconds; quick operations no
  longer flash a progress window.

#### How the Windows 11 menu works

Windows 11 only shows apps in its new context menu if they register a COM "explorer
command" through an app package. ZipNest ships `ZipNestShell.dll` (the menu, written in C)
and a tiny signed package `ZipNestShell.msix` that points to it (no files are moved; the
program stays in its install folder). Because ZipNest is not signed by a commercial
certificate authority, the installer adds ZipNest's own certificate (`ZipNest.cer`) to the
computer's *Trusted People* store on Windows 11 so Windows accepts the package; the
uninstaller removes it again. The private signing key is created on first build in
`installer/signing/` — keep it private and back it up (updates must use the same key).
On Windows 10 the classic menu is used. The Windows 11 menu is x64-only for now.

### What's new in 1.1

- New icon (a "Z" made from a zipper) across Windows, the installer and Android,
  including an Android adaptive icon and Android 13+ themed icon.
- ZIP compression uses all CPU cores (about 3.7× faster on a 4-core PC with many files).
- Android: new colours, automatic dark mode, "Sort by" (name, size, date, type),
  and a clear "Allow access" screen when file permission is missing.
- Windows: remembers window size/position, column widths and your last
  format/compression choice in the Add dialog.

### About RAR

ZipNest **reads** RAR archives (RAR4 and RAR5) but cannot **create** them. RAR
compression is proprietary to RARLAB and licensed only to WinRAR; no other
program may legally create `.rar` files. ZipNest creates ZIP (compatible
with every OS, including Windows Explorer, WinRAR and 7-Zip) and TAR formats instead.

## Install

**Windows 10/11 (64-bit):** run **`ZipNest-1.2.0-setup.exe`** and follow the wizard
(install folder → options: Start Menu / desktop shortcut, associate archive files,
Explorer right-click menu → Install). It appears in *Settings → Apps* and can be
uninstalled from there. Windows SmartScreen may warn because the installer is not
code-signed; choose *More info → Run anyway*.
A portable version (no install) is also provided: unzip
`ZipNest-1.2.0-windows-amd64.zip` (or `-arm64`) and run `ZipNest.exe`; the
right-click menu can then be enabled in **Options → Integrate with Windows Explorer**.

**Android 8.0+:** copy `ZipNest-1.2.0.apk` to the phone, open it and allow
"Install unknown apps" for your file manager. On first start ZipNest asks for
"All files access", which it needs to read and write archives anywhere on the device.

## Using it

**Windows** — double-click an archive to open it, double-click folders to go in,
Backspace to go up. Select files and use the toolbar: *Add*, *Extract To*,
*Extract Here*, *Test*, *View*, *Delete*, *Info*. Drag files onto the window to
add them (into the open ZIP, or into a new archive).

**Android** — tap folders/archives to open them. Long-press to select items, then
use the bottom bar: *Add*, *Extract*, *Test*, *Delete*. The ⋮ menu has *Select all*,
*Storage* (internal / SD card / USB), *New folder* and *Archive information*.
Tap a file inside an archive to open it in another app.

**Command line**

```bash
zipnest-cli l photos.rar                  # list
zipnest-cli x photos.rar D:\out           # extract
zipnest-cli x -pSECRET backup.zip         # extract with password
zipnest-cli t backup.zip                  # test
zipnest-cli a -m9 -pSECRET backup.zip Documents Pictures   # create (AES-256)
zipnest-cli d backup.zip Documents/old.txt                  # delete members
zipnest-cli a -hpSECRET -v700m backup.7z Pictures            # 7z, hidden names, 700 MB parts
zipnest-cli c photos.rar photos.7z                           # convert
ZipNest.exe a --sfx Setup.exe Program                        # self-extracting archive
```

## How it is built

```
core/              archive engine (Go) – shared by both apps
  read.go          list / extract / test for every format
  write.go         create ZIP & TAR, add to ZIP, delete
  zipcrypt.go      WinZip AES-256 (read+write) and ZipCrypto (read)
  core_test.go     round-trip tests
cli/               command-line interface (human and --json output)
cmd/zipnest/      Windows GUI (native Win32 via lxn/walk)
cmd/zipnest-core/ engine as a standalone CLI (also packaged inside the APK)
cmd/zipnest-sfx/  self-extractor stub (plain Win32, embedded into ZipNest.exe)
android/           Android app (Java, no AndroidX) + an SDK-free build
  src/…            MainActivity (UI), Core (talks to the engine), CacheProvider
  build.py         builds the APK without Gradle or the Android SDK
  buildtools/      binary-manifest/resource encoder, APK signer, tool download
assets/            icon generators (the icons are drawn by code)
tools/winres/      embeds icon + manifest + version info into the Windows exe
installer/         Windows setup wizard (NSIS script + artwork), msix/ = Windows 11 menu package
shellext/          ZipNestShell.dll: the Windows 11 context menu (C, COM IExplorerCommand)
```

The Android app runs the same Go engine: it is cross-compiled for each CPU type
and shipped as `lib/<abi>/libzipnest.so`, which the Java UI starts as a process and
talks to over JSON lines. That gives Android the exact same format support as the
desktop, including RAR5.

### Build from source

Needs Go 1.24+, Python 3, a JDK 17+ and NSIS 3 (`makensis`) — no Android SDK, no Gradle:

```bash
./build.sh
```

Outputs go to `dist/`. The first run downloads three build-time jars from Maven
Central (Android framework stubs, the `dx` DEX compiler, Google's `apksig`).

### Automatic builds (GitHub Actions)

`.github/workflows/build.yml` builds everything on every push and pull request and
uploads the installer, portable ZIPs, APK and Linux CLI as a downloadable artifact.
Pushing a version tag publishes a GitHub Release with those files:

```bash
git tag v1.5.0
git push origin v1.5.0
```

Add these repository secrets (Settings → Secrets and variables → Actions) so builds are
signed with your keys: `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`,
`MSIX_KEY_BASE64`, `MSIX_CERT_BASE64` (base64 of the keystore, key and certificate).
Without them the workflow uses temporary keys, which must not be published.

**Signing:** the APK is signed with a locally generated key in
`android/buildtools/debug-signing.p12`. Keep that file: Android only installs
updates signed with the same key. For Google Play, create a release key and
upload through Play App Signing.

## Status and known limitations

- Tested here: the engine (unit tests; RAR4/RAR5/7z sample archives incl.
  encrypted and multi-volume; AES ZIPs cross-checked with an independent
  implementation); the Windows GUI run under Wine (browse, open RAR/ZIP, password
  prompts, extract, create encrypted ZIP) and the installer/uninstaller under
  Wine (all pages, files, shortcuts, registry entries, clean removal); the APK structure, manifest and
  signature with an independent parser.
- **Not yet run on a real Android device or a real Windows PC.** Please try both
  and report anything odd.
- Cannot create RAR archives (RAR compression is licensed to WinRAR only).
- 7z compression is about 10% larger than 7-Zip's "normal" mode (it matches 7-Zip's
  "fast" mode), but faster on multi-core PCs.
- Recovery records and repair are available for `.nest` archives only.

Third-party components and their licences: see `THIRD_PARTY_NOTICES.md`.
