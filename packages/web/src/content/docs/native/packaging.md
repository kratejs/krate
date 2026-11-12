---
title: Packaging
order: 12
description: krate native package - installers and archives for macOS, Windows and Linux, generated icons and Windows resources, and which formats need a platform tool.
---

# Packaging

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

```sh
krate native package [dir] [--target <os/arch>] [--format <list>] [--out <dir>]
krate native build --package [--target <os/arch>]
```

`package` turns a built app into files you can hand to users. Output goes to
`dist-native/packages/`. Most formats are written by the compiler itself, so
you can build a Linux `.deb`, a Windows `.zip` or an MSIX package on any
machine. A few formats need a platform tool; the table says which.

## What it packages

`package` looks in `dist-native/` for one of two layouts:

1. A **release executable**: `dist-native/<os>-<arch>/<productName>[.exe]`, one
   binary with the app embedded (`krate native build --release`). Anything
   else in that directory is shipped next to it.
2. The **prebuilt shell**: `dist-native/<os>-<arch>/<productName>[.exe]`, the
   `krate-shell` binary under the product name, with `app/` next to it. `krate
   native build` writes this when it finds a shell (see
   [Finding the shell](/docs/native/dev/#finding-the-shell)), and so does
   `krate native build --release` when it falls back to the prebuilt-shell
   layout. No Go toolchain is needed. The shell finds `app/` next to itself, or
   in `Contents/Resources/app` inside a macOS bundle. The older pair
   `dist-native/krate-shell[.exe]` next to `dist-native/app/` is still packaged.

The executable's header is checked against `--target`, so a Linux binary is
never packaged as a Windows app. With neither layout present the command stops
with `KR7220` and says which build command to run.

`krate native build --package` builds, then packages the same target.

## Formats

| OS | Format | Written by | Notes |
|----|--------|------------|-------|
| macOS | `app` | compiler | `<Name>.app`, kept under `packages/darwin-<arch>/` |
| macOS | `zip` | compiler | always; the update file. Keeps modes and symlinks |
| macOS | `dmg` | `hdiutil` | only on macOS; elsewhere you get the `.zip` and a note |
| Windows | `zip` | compiler | always; the update file |
| Windows | `nsis` | `makensis` | install dir, shortcuts, uninstaller, URL protocols, WebView2 check |
| Windows | `msix` | compiler | unsigned; sign it with `krate native sign` |
| Linux | `tar.gz` | compiler | always; the update file |
| Linux | `deb` | compiler | default Linux installer |
| Linux | `rpm` | compiler | via the `rpmpack` library |
| Linux | `appimage` | `appimagetool` | needs a squashfs writer, so the tool is required |

Without `--format` the formats come from the config:
`native.bundle.windows.installer` (`msix` or `nsis`) and
`native.bundle.linux.formats` (`deb`, `rpm`, `appimage`; default `deb`). A tool
that is missing for a format you asked for fails with `KR7221` and an install
hint. `--format` takes a comma separated list and replaces the config.

```ts
export default defineConfig({
  native: {
    appId: "com.acme.notes",
    productName: "Acme Notes",
    version: "1.2.0",
    icons: "assets/icon.png",
    deepLinks: ["acme"],
    bundle: {
      macos: { category: "productivity" },
      windows: { publisher: "CN=Acme Inc", installer: "msix" },
      linux: { formats: ["deb", "rpm"] },
    },
  },
});
```

## Icons

`native.icons` is one PNG, ideally 1024 x 1024. The packager makes everything
else in pure Go:

- `.icns` with PNG elements `ic07` to `ic10` (128 to 1024 pixels) and the
  retina and small variants.
- `.ico` with PNG entries from 16 to 256 pixels.
- Linux hicolor PNGs from 16 to 512 pixels, and the MSIX logos.

Without `native.icons` a placeholder icon is generated. A source under 512
pixels works but gives soft large icons (`KR7223` is an unreadable file).

## macOS

`<Name>.app/Contents/` holds `Info.plist`, `MacOS/<Name>`,
`Resources/icon.icns` and `Resources/app/` (the shell layout). `Info.plist`
carries the bundle id (`native.appId`), the version, `LSApplicationCategoryType`
from `bundle.macos.category` (`productivity` or a full
`public.app-category.*`), `NSHighResolutionCapable`, `LSMinimumSystemVersion`
and, for `deepLinks`, `CFBundleURLTypes`.

**Signatures.** The Go linker signs `darwin/arm64` executables ad hoc, which is
enough for Apple silicon to run them (checked: `LC_CODE_SIGNATURE` with the
ad hoc flag). The `.app` as a whole has no `_CodeSignature`; that needs
`codesign`, so run `krate native sign` on a Mac. A pure Go bundle signer is not
implemented. See [Signing](/docs/native/signing/).

## Windows

The portable `.zip` holds `<Name>/<Name>.exe` and its files. The **NSIS**
script (kept at `packages/windows-<arch>/installer.nsi`) installs to
`Program Files`, adds Start menu and desktop shortcuts and an uninstaller,
registers each `deepLinks` scheme under `HKCR`, and downloads Microsoft's
WebView2 bootstrapper when the runtime is missing. **MSIX** needs
`bundle.windows.publisher`, the subject of your signing certificate (`CN=Acme
Inc`). The package is a zip with `AppxManifest.xml` (identity, logos, protocol
extensions, `runFullTrust`), `[Content_Types].xml` and `AppxBlockMap.xml`
(SHA-256 per 64 KiB block). Files are stored uncompressed so the block map
needs no compressed sizes.

### Icon and version resources

An `.exe` shows its icon and version in Explorer only if they are linked in.
`pkg.WindowsResources` writes a `.syso` object that the Go linker picks up:

```go
// in a build step
err := pkg.WindowsResources(root, cfg, "rsrc_windows_amd64.syso")
```

Put `rsrc_windows_amd64.syso` (and `rsrc_windows_arm64.syso`) next to the
`main.go` of the executable; the file name suffix keeps other platforms from
linking it. It contains the icon (`RT_GROUP_ICON` and `RT_ICON`), the version
information (`RT_VERSION`) and a manifest that turns on per-monitor DPI
awareness, long paths and Common Controls v6. The release build calls it.

## Linux

The `.deb` installs a single executable to `/usr/bin/<name>`. With support
files (the shell layout) the executable and files go to `/usr/lib/<name>/` and
`/usr/bin/<name>` is a symlink. It also installs
`/usr/share/applications/<appId>.desktop` (with
`MimeType=x-scheme-handler/<scheme>` for `deepLinks`), hicolor icons and an
`md5sums` file. `Depends:` lists `libwebkit2gtk-4.1-0` and `libgtk-3-0`; the
`.rpm` requires `webkit2gtk4.1` and `gtk3`. Check a `.deb` with
`dpkg-deb --info` and `--contents`.

## Update files and `--notes`

The zip (macOS, Windows) or tar.gz (Linux) is also the file the
[updater](/docs/native/updater/) downloads. With `KRATE_UPDATER_PRIVATE_KEY` set
`package` signs it and writes `latest-<os>-<arch>.json` beside it.

## Flags

| Flag | Meaning |
|------|---------|
| `--target <os/arch>` | `linux`, `windows`, `darwin` with `amd64` or `arm64`; default this machine |
| `--format <list>` | formats from the table |
| `--out <dir>` | native output directory (default `dist-native`) |
| `--sign`, `--notarize` | sign (and notarize) while packaging; see [Signing](/docs/native/signing/) |
| `--notes <text or @file>` | release notes for the update feed |
| `--update-base-url <url>` | where the update files will be hosted |
| `--delta-from <dir>[,<dir>]`, `--delta-count <n>` | write signed delta updates from the newest `n` (default 3) earlier releases whose update files are in these directories; see [Updates](/docs/native/updater/#delta-updates) |
| `--channel <name>` | write the feed as `latest-<name>-<os>-<arch>.json` for that [channel](/docs/native/updater/#channels) |
| `--rollout <percent>`, `--rollout-start <RFC 3339>`, `--rollout-ramp-hours <h>` | [staged rollout](/docs/native/updater/#staged-rollouts) |
| `--critical`, `--min-version <v>` | offer the update to every install, whatever the rollout says |

## Diagnostics

`KR7220` nothing to package, `KR7221` tool missing, `KR7222` unknown format for
the OS, `KR7223` icon, `KR7228` write failure, `KR7229` metadata (appId,
version, MSIX publisher).
