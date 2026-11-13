---
title: Stores and universal apps
order: 13
description: Universal macOS apps, Flatpak, Snap, the Mac App Store and the Microsoft Store - what Krate generates, what the shell does inside a sandbox, and how to submit.
---

# Stores and universal apps

> **Experimental.** Part of [Krate Native](/docs/native/), which is under construction.

[Packaging](/docs/native/packaging/) makes installers you host yourself. This
page covers the other ways to reach users: one macOS app that runs natively on
Apple silicon and Intel, the Linux sandbox formats (Flatpak, Snap) and the two
Windows and macOS stores. Stores deliver updates themselves, so a store build
never runs the [self-updater](/docs/native/updater/).

What was tested where is in the
[compatibility matrix](/docs/guides/compatibility/#krate-native). The package
structure of every format here is checked by Go tests on any OS; running the
result inside Flatpak, Snap, the App Store sandbox or the Microsoft Store is
listed there as tested only where CI does it.

## Universal macOS apps

```sh
krate native build --release --arch universal --package
krate native package --arch universal          # both builds exist already
```

or `native.bundle.macos.universal: true` (it applies when you pass no
`--target`). `--target darwin/universal` is the same thing.

A release build compiles `darwin/arm64` and `darwin/amd64` (Go cross compiles
both from any OS). `package` then merges them in pure Go into one `.app`: the
executable, and every Mach-O file the two builds share (the Go host child,
compiled Node, Bun and Deno hosts), becomes a universal file with a
`FAT_MAGIC` header, one `fat_arch` entry per slice and each slice aligned to
2^14. There is no `lipo` dependency; on a Mac you can check the result with
`lipo -info "Acme Notes.app/Contents/MacOS/Acme Notes"`.

Files that are not Mach-O must be identical in both builds. A file only one
build has, or a text file that differs, is `KR7300` with the path.

Host runtimes need a tool per architecture:

| Runtime | Universal build |
|---------|-----------------|
| `quickjs` | Nothing extra: it is linked into the executable |
| `bun`, `deno` | Cross compile by themselves (`bun build --compile --target`, `deno compile --target`) |
| `node` | A single executable application is one `node` binary per architecture. The one that runs on the machine is used; the other comes from `KRATE_NODE_DARWIN_ARM64` or `KRATE_NODE_DARWIN_AMD64` (download it from nodejs.org). Without it the build stops with a diagnostic that names the variable |

The prebuilt-shell layout holds one platform, so a universal build needs the
compiled-executable layout (Go installed).

### Updates

The package is named `<app>-<version>-darwin-universal.zip` and its feed
`latest-darwin-universal.json`. The app learns its update target from a
`krate-dist.json` marker in `Contents/Resources`, so `{target}` in
`native.updater.url` expands to `darwin-universal` for it. Apps built before
you switched keep asking for `darwin-arm64` or `darwin-amd64`: publish the old
feeds as well until they have updated once, and they will install the
universal build.

## Flatpak

```sh
krate native package --format flatpak [--update-base-url https://dl.acme.com/notes]
```

It writes `packages/flatpak-<arch>/` with everything `flatpak-builder` needs:
`<appId>.yml` (a manifest on the `org.gnome.Platform` runtime, which carries
WebKitGTK 4.1), `<appId>.desktop`, `<appId>.metainfo.xml`
([AppStream](https://www.freedesktop.org/software/appstream/docs/)), hicolor
icons named by the app id and the `files/` directory with your prebuilt app.
When `flatpak-builder` and `flatpak` are installed it builds
`<app>-<version>-linux-<arch>.flatpak`; the runtime and SDK must be installed
first (`flatpak install flathub org.gnome.Platform//48 org.gnome.Sdk//48`, or set
`KRATE_FLATPAK_INSTALL_DEPS=1`). If `appstreamcli` is installed the metainfo is
validated and a failure is `KR7302`. Without the tools you still get the
manifest and a note.

Configuration:

```ts
bundle: {
  linux: {
    formats: ["deb", "flatpak"],
    summary: "Notes that stay on your machine",     // AppStream and snap
    description: "Acme Notes keeps ...\n\nSecond paragraph.",
    license: "MIT",                                  // SPDX; "LicenseRef-proprietary" for closed source
    homepage: "https://acme.com/notes",
    flatpak: { runtimeVersion: "48", finishArgs: ["--filesystem=xdg-documents"] },
  },
}
```

**Permissions.** `finish-args` are derived from the app's permissions, the same
list `krate native build` writes to `app.json`, and nothing broader:

| Always | `--share=ipc`, `--socket=wayland`, `--socket=fallback-x11`, `--device=dri`, `--socket=pulseaudio` |
|--------|------|
| `net`, `media`, `location`, `updater`, or a camera or microphone in the UI | `--share=network` |
| `notify` | `--talk-name=org.freedesktop.Notifications` |
| `tray` | `--talk-name=org.kde.StatusNotifierWatcher` |
| `os:secrets` | `--talk-name=org.freedesktop.secrets` |
| `media`, camera, microphone, screen capture | `--filesystem=xdg-run/pipewire-0` |
| `location` | `--system-talk-name=org.freedesktop.GeoClue2` |
| `power` | `--talk-name=org.freedesktop.ScreenSaver` |

File access is not granted: the dialogs go through the file chooser portal,
which hands the app the files the user picks. Add `--filesystem=...` through
`flatpak.finishArgs` only for what the app reads without asking.

**Inside the sandbox** the shell detects `FLATPAK_ID` (and `/.flatpak-info`) and:

- opens links with the `org.freedesktop.portal.OpenURI` portal (falling back to
  `xdg-open`), and sends notifications through `org.freedesktop.portal.Notification`;
- sets `GTK_USE_PORTAL=1`, so open and save dialogs use the file chooser portal;
- names its tray item `<appId>.StatusNotifierItem` (an app may own names below its
  own id; the PID based `org.kde.*` name is not allowed in the sandbox);
- keeps app data in the sandbox: `appDataDir()` and friends follow
  `XDG_DATA_HOME` and the other XDG variables Flatpak points to
  `~/.var/app/<appId>/`;
- refuses `updater.check()` with a message that names `flatpak update`.

### Flathub

`package` also writes `<appId>.flathub.yml` and
`<app>-<version>-flatpak-sources-<arch>.tar.gz`. The manifest builds from that
archive (`type: archive`, with its `sha256`), so the build needs no network:
the archive holds the prebuilt app directory. Host the archive at
`--update-base-url`, then follow Flathub's
[submission guide](https://docs.flathub.org/docs/for-app-authors/submission):
fork `flathub/flathub`, add the manifest as `<appId>.yml` on a branch named
`new-pr`, open the pull request. Flathub prefers apps built from source; an
app delivered as a prebuilt binary is reviewed case by case, so say so in the
pull request. The id must be one you control (a reverse domain, or
`io.github.<user>.<App>`), add screenshots to the metainfo and read the
[quality guidelines](https://docs.flathub.org/docs/for-app-authors/metainfo-guidelines/quality-guidelines).
Merge the per-architecture manifests by hand: each `only-arches` source goes in
the same module.

## Snap

```sh
krate native package --format snap
```

It writes `packages/snap-<arch>/snap/snapcraft.yaml` with `files/` (the app) and
`snap/gui/icon.png`, and builds the `.snap` with `snapcraft pack` when
`snapcraft` is installed (`sudo snap install snapcraft --classic`). The
default builds in LXD (`lxd init --auto` once); on an Ubuntu 24.04 machine
such as a CI runner set `KRATE_SNAP_DESTRUCTIVE=1` to build in place.

The project is `base: core24`, `confinement: strict`, `grade: stable` (change
them with `bundle.linux.snap`), the app uses `extensions: [gnome]` for GTK 3
and the desktop integration and stages `libwebkit2gtk-4.1-0` from the Ubuntu
archive. Plugs, derived from the permissions: always `desktop`, `desktop-legacy`,
`wayland`, `x11`, `opengl`, `audio-playback` and `home`; `network` (as for
Flatpak), `unity7` for `tray` or `notify`, `password-manager-service` for
`os:secrets`, `screen-inhibit-control` for `power:inhibit`, `camera` and
`audio-record` when the UI uses them. Extra plugs go in
`bundle.linux.snap.plugs`.

The shell detects `SNAP_NAME` and `SNAP` and behaves as in Flatpak: links through
the OpenURI portal (the snap's `xdg-open` is the fallback), notifications through
the portal when it is running, file dialogs through `GTK_USE_PORTAL=1`, data in
the XDG directories snapcraft sets under `$SNAP_USER_DATA`, and no self-updater
(`snap refresh` delivers updates).

**Store submission.** `snapcraft login`, `snapcraft register <name>` (the name
comes from the product name; `acme-notes` for "Acme Notes"), then
`snapcraft upload --release=stable acme-notes_1.2.0_amd64.snap`. A strict snap
that uses an interface the store does not connect automatically needs a request
on the [snapcraft forum](https://forum.snapcraft.io/c/store-requests/19).

## Mac App Store

```sh
krate native package --target mas [--identity "Apple Distribution: Acme (ABCDE12345)"] \
  [--installer-identity "3rd Party Mac Developer Installer: Acme (ABCDE12345)"]
```

`--target mas` is `darwin/<this machine>` (or universal with `--arch universal`
or `bundle.macos.universal`) in the Mac App Store variant. Configuration:

```ts
bundle: { macos: { category: "productivity", mas: {
  teamId: "ABCDE12345",
  provisioningProfile: "signing/Acme_Notes_MAS.provisionprofile",   // do not commit
} } }
```

What differs from a direct build:

- **App Sandbox.** The app is signed with `com.apple.security.app-sandbox` and the
  entitlements its permissions need: `network.client` (default; set
  `mas.network` to add `server`), `files.user-selected.read-write`
  (`mas.files`: `read-only` or `none`), `device.camera`, `device.audio-input`,
  `personal-information.location` when the UI uses them, and the application
  identifier `TEAMID.<appId>` and team identifier. A Node, Bun or Deno host
  child also gets `allow-jit` and `allow-unsigned-executable-memory`. Anything
  else goes in a plist named by `mas.entitlements` (boolean and string keys
  are merged).
- **Helper executables** (compiled host children) are signed with
  `app-sandbox` and `inherit` only, so they run in the app's sandbox.
- **No updater.** `native.updater` is removed from the packaged `app.json` with a
  warning, and a running sandboxed app refuses updater calls. A release
  executable carries its `app.json` inside, so the refusal is what protects it;
  drop `native.updater` from the config you build for the store if you want the
  block gone as well.
- **Provisioning profile** embedded as `Contents/embedded.provisionprofile`.
  Krate checks that it names `TEAMID.<appId>` (`KR7301` if not).
- **Signing** with `codesign` (no hardened runtime, which the store does not need)
  and a `.pkg` made by `productbuild --component App.app /Applications --sign`.
  Identities: `--identity` or `KRATE_MAS_APP_IDENTITY` must be
  `3rd Party Mac Developer Application: ...` or `Apple Distribution: ...` (a
  Developer ID identity is refused); `--installer-identity` or
  `KRATE_MAS_INSTALLER_IDENTITY` must be `3rd Party Mac Developer Installer: ...`
  or `Mac Installer Distribution: ...`. Without an identity the app is signed
  ad hoc with the sandbox entitlement (without the restricted identifiers a
  profile would be needed for): it runs on the machine that built it, which is
  what CI tests, but it cannot be uploaded.
- **Output:** `packages/darwin-<arch>/<App>.app` and
  `<app>-<version>-darwin-<arch>-mas.pkg`. No zip, dmg or feed.

**Inside the sandbox** the app's home directory is its container
(`~/Library/Containers/<appId>/Data`), so `appDataDir()` and the database and
key-value stores land there. The Go host child's socket lives under that home
(`.krate-<pid>`), because the container's cache and temp paths are longer than a
Unix socket path may be. Files the user picks in an open or save dialog become
readable and writable for that run (the sandbox grants them with the dialog);
Krate does not keep that access across launches, so an app that reopens files
from a previous session asks again. Security-scoped bookmarks are not used.

Upload with Transporter or `xcrun altool --upload-package app.pkg --type osx
--apple-id <id> --bundle-id <appId> --bundle-version <build> --bundle-short-version-string <version>`.
App Store Connect needs an app record with the same bundle id, screenshots and a
privacy policy; its validation can ask for more `Info.plist` keys (for example
`ITSAppUsesNonExemptEncryption`) than Krate writes - add them through a
custom plist step if it does. Notarization does not apply to the store.

## Microsoft Store

```sh
krate native package --target windows/amd64 --format msix-store
krate native package --arch universal --target windows/amd64 --format msix-store   # x64 + arm64 bundle
```

```ts
bundle: { windows: { store: {
  identityName: "12345Acme.AcmeNotes",
  publisherId: "CN=01234567-89AB-CDEF-0123-456789ABCDEF",
  publisherDisplayName: "Acme Inc",
} } }
```

Copy the three values from Partner Center: your app, Product management,
Product identity. They replace the identity Krate derives for a self-hosted MSIX
(`bundle.windows.publisher` is not needed). Output:

| File | Use |
|------|-----|
| `<app>-<version>-windows-<arch>-store.msix` | the package, unsigned (the Store signs it) |
| `<app>-<version>-windows-<arch>.msixupload` | a zip that holds the package: upload this |
| `<app>-<version>-windows-universal.msixbundle` | x64 and arm64 in one bundle (`windows/universal`) |
| `<app>-<version>-windows-universal.msixupload` | the bundle, for upload |

A bundle has its own manifest (`AppxMetaData/AppxBundleManifest.xml`) and block
map; the packages inside are stored uncompressed.

**Rules for full-trust apps.** The Windows App Certification Kit and Partner Center
check, among much else, that the package is a full-trust desktop app with the
`runFullTrust` capability (Partner Center asks you to justify it when you
submit), that the identity matches the reserved one, that the version ends in
`.0`, that the logos exist, and that nothing updates itself. Krate checks what
can be checked without Windows after building and refuses the package
(`KR7301`) when the identity, executable, logos, capability or version are
wrong, or when the package holds a self-updater (`native.updater` in `app.json`,
`Update.exe`). `native.updater` is removed from the packaged `app.json` with a
warning; the marker `krate-dist.json` next to the executable turns the updater
off for a release executable too. Run the Windows App Certification Kit on a
Windows machine before you submit; Krate cannot run it.

**Requirements outside the package:** the app needs the WebView2 Runtime, which
is part of Windows 11 and current Windows 10; the Store does not install it. See
[the packaging page](/docs/native/packaging/#the-webview2-runtime). Submit under
Partner Center, Apps and games, your app, Start the submission, Packages: drag the
`.msixupload` in. Test the package on a machine with developer mode by signing a
copy (`krate native sign`) with a certificate whose subject equals `publisherId`.

## CI

Everything above is written by the compiler on any OS. To exercise the tools:

| Format | CI step |
|--------|---------|
| universal | macOS runner: `krate native build --release --arch universal --package`, then `lipo -info` |
| flatpak | `sudo apt-get install flatpak flatpak-builder appstream`; `flatpak remote-add --user --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo`; `KRATE_FLATPAK_INSTALL_DEPS=1 krate native package --format flatpak` |
| snap | Ubuntu 24.04 runner: `sudo snap install snapcraft --classic`; `KRATE_SNAP_DESTRUCTIVE=1 krate native package --format snap` |
| mas | macOS runner: `krate native package --target mas` (ad hoc), then launch the app |

## Diagnostics

`KR7300` universal merge (missing build, differing file), `KR7301` store identity
or rule, `KR7302` Flatpak or Snap file or tool.
