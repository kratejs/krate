---
title: Updates
order: 14
description: In-app updates for Krate Native apps - the signed update feed, krate native keygen, the check/download/apply/relaunch flow and how each OS swaps the files.
---

# Updates

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

An app checks a JSON feed, downloads a file, verifies an Ed25519 signature
against a public key built into the app, replaces its own files and restarts.
It can download a small [patch](#delta-updates) instead of the whole file, the
feed can limit an update to a [share of the installs](#staged-rollouts), and a
version that does not start properly is [rolled back](#rollback-after-a-failed-first-start)
at the next launch.

## Setup

```sh
krate native keygen --key-out krate-updater.key
```

writes the private key (mode 0600 on macOS and Linux, where Windows has no mode bits and relies on your profile folder permissions; keep it out of git) and prints the public
key:

```ts
native: {
  updater: {
    url: "https://dl.acme.com/updates/latest-{target}.json",
    publicKey: "9bX...=",
  },
}
```

`url` must be https and may contain `{target}` (`<goos>-<goarch>`, or `darwin-universal` for a [universal macOS app](/docs/native/distribution/#universal-macos-apps); for example
`linux-amd64`, `windows-arm64`, `darwin-arm64`), `{version}` (the running
version) and `{channel}` (see [Channels](#channels)). All of `native.updater` is
written to [`app.json`](/docs/native/app-json/). The app needs the `updater`
[permission](/docs/native/sdk/#permissions) to call the updater from the UI. A
configured updater is linked into the app even when no code calls it, because
the start-up check that rolls back a bad update lives there.

Optional keys:

| Key | Meaning |
|-----|---------|
| `channel` | the channel that fills `{channel}`; default `stable` |
| `maxStarts` | starts a freshly updated version gets to confirm a healthy start; default 1 |
| `healthTimeout` | seconds one start may take to confirm; default 60 |
| `confirm` | `"auto"` (default: the shell confirms) or `"app"` (the app calls `updater.markHealthy()`) |

For each release:

```sh
KRATE_UPDATER_PRIVATE_KEY=$(cat krate-updater.key) krate native package \
  --target linux/amd64 --notes "Faster search" --update-base-url https://dl.acme.com/updates
```

Besides the installers this writes the update file and
`latest-linux-amd64.json` next to it; upload both. (`KRATE_UPDATER_PRIVATE_KEY_FILE`
names a file instead.) The private key must match the configured public key
(`KR7227`).

## In a store

Flatpak, Snap, the Mac App Store and the Microsoft Store deliver updates
themselves and forbid apps that replace their own files. A copy that runs from
one (the shell reads `FLATPAK_ID`, `SNAP_NAME`, the macOS sandbox container id or
the `krate-dist.json` marker the packager writes) reports the `updater`
capability as absent, the `updater` calls fail with a message that names the
store's own mechanism, and no rollback watchdog is armed. Store builds also drop
`native.updater` from the packaged `app.json`. See
[Stores and universal apps](/docs/native/distribution/).

## Feed

```json
{
  "version": "1.2.0",
  "notes": "Faster search",
  "pub_date": "2026-11-11T10:00:00Z",
  "url": "https://dl.acme.com/updates/acme-notes-1.2.0-linux-amd64.tar.gz",
  "signature": "<base64>",
  "size": 18241234,
  "patches": [
    { "from": "1.1.0", "url": "https://dl.acme.com/updates/acme-notes-1.1.0-to-1.2.0-linux-amd64.krpatch",
      "signature": "<base64>", "size": 114871, "sha512": "<hex>" }
  ],
  "rollout": { "percentage": 25, "start": "2026-11-12T00:00:00Z", "ramp_hours": 48 },
  "critical": false,
  "min_version": "1.0.0"
}
```

`patches`, `rollout`, `critical` and `min_version` are optional. A client that
does not know them ignores them, and a client that knows them accepts a feed
without them.

HTTP 204 or 404 means no update. A `version` not newer than the running one is
also "no update" (semantic version order, prereleases before releases). A
relative `url` resolves against the feed URL.

**Signature.** Ed25519 (RFC 8032), base64, over this message, with fields
joined by single `\n` characters and no trailing newline:

```
krate-update-v1
<appId>
<target>
<version>
<sha512 of the artifact file, lowercase hex>
<size in bytes>
```

`appId` is `native.appId`, `target` is `<os>-<arch>` (for example
`linux-amd64`) and `version` is the feed's `version` string exactly as written.
The file digest is computed as a stream, so large files are never held in
memory. `krate native package` produces the signature; the client verifies it
with the configured public key, the running app's id and target, and the
feed's version before it keeps a download. Because the metadata is signed, a
compromised feed cannot relabel an older validly signed artifact as a newer
version, nor serve another app's or another target's artifact. The client also
refuses any `version` that is not strictly greater than the running one, both
when checking and when downloading. Artifact URLs must be https.

The `size` field in the feed is only a hint for progress and early rejection;
the signed size is the real length of the downloaded file.

## Delta updates

A patch turns the files of one older version into the files of the new one, so
an update downloads kilobytes instead of the whole app. Pass the directories that
hold your earlier update files when you package:

```sh
krate native package --delta-from releases/ --delta-count 3 --update-base-url https://dl.acme.com/updates
```

For the newest 3 earlier versions found (files named like
`<name>-<version>-<os>-<arch>.tar.gz` or `.zip`) it writes
`<name>-<from>-to-<to>-<os>-<arch>.krpatch`, signed with the same key, and lists
them under `patches` in the feed. A patch that would not be smaller than the full
file is left out. Upload the patch files next to the full file.

**What a patch is.** One file with a JSON manifest of the new tree (directories,
symlinks, files with their sha512 and mode) and the data for each changed file: a
`bsdiff` of the installed file (Colin Percival's algorithm, implemented in pure
Go, deflated), the deflated new bytes for a file that is new, or nothing for a
file that is unchanged or only renamed (hashed asset names). The same code
handles every kind of update file: the Linux and Windows install tree, the macOS
`.app` bundle (symlinks and modes included) and a single AppImage file.

**What the client does.** It takes the patch whose `from` is its own version,
downloads it (resumable like the full file) and checks, in this order: the size
and `sha512` of the file, the Ed25519 signature (which binds app id, target, both
versions and the patch bytes), that every installed file the patch builds on has
exactly the hash recorded when the patch was made, and the sha512 and mode of every
file it writes. Everything is built in a staging directory next to the install and
swapped in by `apply` like a full update. On any failure (a modified or missing
installed file, a bad signature, a damaged download, no space) the staging directory is
removed and the client downloads the full file instead; `DeltaError()` says why.
A patch is never applied without verifying its result.

Patch generation reads each file into memory (bsdiff needs about 8 bytes per
byte of the old file; files over 256 MiB are stored whole instead of diffed).
On `examples/native-tasks` (a 15.5 MB executable, 6.6 MB `.tar.gz`) the patch from 1.0.0 to 1.1.0 is
16 KB (0.2 % of the full file) when only the version changed and 115 KB (1.7 %)
after adding a function.

For a file the packager does not feed (an AppImage), `krate native patch` writes
one patch and prints the feed entry:

```sh
krate native patch --patch-old a-1.0.0.AppImage --patch-new a-1.1.0.AppImage --patch-kind file \
  --patch-from 1.0.0 --patch-to 1.1.0 --patch-app-id com.acme.notes --patch-target linux-amd64 \
  --patch-out a-1.0.0-to-1.1.0.krpatch
```

`--patch-kind` is `tree` (also needs `--patch-exe`, the executable's name, to find
the wrapper directory of a Windows zip), `bundle` or `file`.

The NSIS installer and the MSIX are not update files: the updater replaces the
files of an install directory, a bundle or an AppImage, so installer-based
installs do not take delta updates.

## Staged rollouts

`rollout` limits who is offered an update:

```json
"rollout": { "percentage": 25, "start": "2026-11-12T00:00:00Z", "ramp_hours": 48 }
```

Each install has a random id, created the first time it is needed and kept in
the updater's state directory (`<app data dir>/updater/install-id`). From the
id and the release (the `salt` field, default the version) the client computes a
fixed number from 0 to 100, its bucket, and is offered the update when the
bucket is below the effective percentage. The same install keeps its place while
the percentage grows, and each release picks different early adopters. Before
`start` nobody is offered the update; with `ramp_hours` the percentage grows
linearly from 0 at `start` to `percentage` that many hours later.

An install outside the rollout sees "no update" (`check()` resolves `null`).
These fields are not part of the signature, so you can change them after the
release without signing again:

```sh
krate native rollout --feed dist-native/packages/latest-linux-amd64.json --rollout 50
krate native rollout --feed latest-linux-amd64.json,latest-darwin-arm64.json --halt     # percentage 0
krate native rollout --feed latest-linux-amd64.json --rollout 100 --critical
```

Upload the changed feed files. `--halt` stops new installs from being offered the
update; installs that already updated keep it. Without a rollout everybody is
offered the update.

**Forcing.** `critical: true` (`--critical`) and `min_version` (`--min-version`)
offer the update to every install, whatever `rollout` says. `min_version` applies
to installs older than it and sets `required` on the update info, so the UI can
refuse to continue without updating; `critical` is passed on as `critical`. The
updater never installs anything by itself: forcing is the app's decision. A
compromised feed server can already withhold updates, but it cannot make a client
install anything that is not signed.

## Channels

`{channel}` in `native.updater.url` expands to the install's channel: the one set
with `updater.setChannel("beta")` (kept across runs), else `native.updater.channel`,
else `stable`. `krate native package --channel beta` writes
`latest-beta-<os>-<arch>.json`, so one URL template serves every channel:

```ts
updater: { url: "https://dl.acme.com/updates/latest-{channel}-{target}.json", publicKey: "..." }
```

Without `--channel` the packager keeps the old name `latest-<os>-<arch>.json`
(use that with a URL that has no `{channel}`). A version number only ever goes up,
so moving from `beta` back to `stable` offers an update only when `stable` is
newer. Give beta builds prerelease versions (`1.3.0-beta.2`), which sort before
`1.3.0`.

## Rollback after a failed first start

Applying an update keeps the replaced files (`.krate-prev` next to the install,
`.<Name>.app.prev` beside a bundle, `<file>.prev` for an AppImage) and records the
update as unconfirmed in `state.json` in the state directory. The new version must
confirm a healthy start:

- by default the shell does it a few seconds after the first page was served (3 s,
  `KRATE_UPDATER_HEALTH_GRACE_MS` changes it for tests), or at once when the app
  quits normally after that;
- with `confirm: "app"` the app calls `updater.markHealthy()` when it knows it
  works (after a database migration, a login), and the shell never confirms.

Confirming deletes the kept copy. A start that crashes or exits before confirming
counts as failed, and so does one that does not confirm within `healthTimeout`
seconds (default 60; the version then keeps running, but the next launch rolls
back). When a freshly updated version has used `maxStarts` (default 1) unconfirmed
starts, the next launch restores the previous files, starts the previous version
(unless `KRATE_UPDATER_ROLLBACK_NO_RELAUNCH=1`), exits, and records a rollback
event with the reason:

```ts
const s = await updater.status();     // { version, channel, unconfirmed, rollback }
if (s.rollback) {
  report(`${s.rollback.failed} was rolled back: ${s.rollback.reason}`);
  await updater.ackRollback();
}
```

The rolled back version is also remembered, and `check()` does not offer it again
on that install: ship a new version number to try again.

Limits. A crash before the shell's start-up check runs (a loader error, a panic in a
package `init`) is not seen, so that start does not count; and a user who kills the
app within the grace period counts as an unconfirmed start (a normal quit does
not). The previous version is the one this update replaced, so rolling back
needs the files in `.krate-prev` to still be there. The check only exists in
versions built with this feature: the first update from an older version is not
protected.

## Update files

| OS | File | Contents |
|----|------|----------|
| Linux | `<name>-<version>-linux-<arch>.tar.gz` | the executable and its files, flat |
| Windows | `<name>-<version>-windows-<arch>.zip` | `<Name>/<Name>.exe` and its files |
| macOS | `<name>-<version>-darwin-<arch>.zip` | `<Name>.app` |
| AppImage | the `.AppImage` file itself (add it to the feed by hand; `krate native patch` writes its patch) | |

## From the UI

```ts
import * as updater from "@krate/app/updater";

const info = await updater.check();          // { version, notes, pubDate, size } or null
if (info) {
  await updater.download((p) => setProgress(p.done / (p.total || 1)));
  await updater.apply();                      // swaps the files
  await updater.relaunch();                   // starts the new version and quits
}
```

`check()` resolves `{ version, notes, pubDate, size, critical, required, delta,
downloadSize }`: `delta` says a patch is offered and `downloadSize` is what
`download()` fetches (the patch size, or `size`).

`updater.update(onProgress)` runs check, download and apply. Errors carry a
code: `unsupported` (no `native.updater` config), `no_update` (download or apply
before check), `not_downloaded`, `bad_signature`, `permission_denied`.
Also `markHealthy()`, `status()`, `ackRollback()` and `setChannel(name)`.
Host ids: `krate/updater.check`, `krate/updater.download` (a stream of
`{ done, total }`), `krate/updater.apply`, `krate/updater.relaunch`,
`krate/updater.markHealthy`, `krate/updater.status`, `krate/updater.ackRollback`,
`krate/updater.setChannel`.

## From Go

```go
import "github.com/kratejs/krate/packages/native/updater"

u, err := updater.Check(ctx, updater.Config{URL: url, PublicKey: key, AppID: "com.acme.notes", Version: "1.0.0", CacheDir: cache, StateDir: state})
if err == nil && u != nil {
    err = u.Download(ctx, func(done, total int64) { /* progress */ })
    if err == nil { err = u.Apply() }
    if err == nil { err = updater.Relaunch() }
}
```

With `StateDir` set, `Apply` keeps the previous version and records the update; call
`updater.Boot(updater.BootConfig{StateDir: state, Version: "1.1.0"})` first thing at
start-up and `guard.MarkHealthy()` when the app works (the Krate shell does both).

## How it works

**Download.** The file goes to `<cache dir>/updates/<version>/` as `*.part`. An
interrupted download resumes with an HTTP `Range` request when the server
supports it, and restarts when it does not. The signature (and `size`, when the
feed has it) is checked before the file is renamed into place; a bad file is
deleted.

**Apply.**

- *Linux and Windows:* the artifact is unpacked next to the install directory.
  Each top-level entry replaces the installed one by renaming: the old entry
  moves into `.krate-old/` first (Windows lets a running `.exe` be renamed), the
  new one is renamed into place. If any step fails, everything already moved
  is put back. `.krate-old/` is deleted after the swap and again at the next
  start, because Windows cannot delete the running `.exe`. The update must
  contain the running executable's name. An app installed by a package manager
  in a root-owned directory fails with a "not writable" error; update those
  through the package manager.
- *macOS:* the unpacked `.app` replaces the running bundle by renaming the
  bundle directory (the old one moves aside, the new one is renamed in, the old
  one is deleted). The bundle's parent directory must be writable.
- *AppImage:* the file named by `$APPIMAGE` is replaced through a temporary file
  and an atomic rename.

The running process keeps its already loaded code until `relaunch`. When the
updater has a state directory (the SDK always sets one), the replaced entries
are kept instead of deleted until the new version confirms a healthy start (see
[Rollback](#rollback-after-a-failed-first-start)).

## Not done yet

Background download scheduling and update prompts are left to the app. Delta
updates need the installed files to match the version the patch was made for; an
install the user modified takes the full download. The macOS and Windows parts
of the delta and rollback code run in the CI smoke test (`scripts/native-smoke.mjs`)
but were developed on Linux.
