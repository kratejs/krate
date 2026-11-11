---
title: Updates
order: 14
description: In-app updates for Krate Native apps - the signed update feed, krate native keygen, the check/download/apply/relaunch flow and how each OS swaps the files.
---

# Updates

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

An app checks a JSON feed, downloads a file, verifies an Ed25519 signature
against a public key built into the app, replaces its own files and restarts.
Only full updates exist today; delta updates are a planned follow-up.

## Setup

```sh
krate native keygen --key-out krate-updater.key
```

writes the private key (mode 0600; keep it out of git) and prints the public
key:

```ts
native: {
  updater: {
    url: "https://dl.acme.com/updates/latest-{target}.json",
    publicKey: "9bX...=",
  },
}
```

`url` must be https and may contain `{target}` (`<goos>-<goarch>`, for example
`linux-amd64`, `windows-arm64`, `darwin-arm64`) and `{version}` (the running
version). Both are written to [`app.json`](/docs/native/app-json/). The app
needs the `updater` [permission](/docs/native/sdk/#permissions).

For each release:

```sh
KRATE_UPDATER_PRIVATE_KEY=$(cat krate-updater.key) krate native package \
  --target linux/amd64 --notes "Faster search" --update-base-url https://dl.acme.com/updates
```

Besides the installers this writes the update file and
`latest-linux-amd64.json` next to it; upload both. (`KRATE_UPDATER_PRIVATE_KEY_FILE`
names a file instead.) The private key must match the configured public key
(`KR7227`).

## Feed

```json
{
  "version": "1.2.0",
  "notes": "Faster search",
  "pub_date": "2026-11-11T10:00:00Z",
  "url": "https://dl.acme.com/updates/acme-notes-1.2.0-linux-amd64.tar.gz",
  "signature": "<base64>",
  "size": 18241234
}
```

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

## Update files

| OS | File | Contents |
|----|------|----------|
| Linux | `<name>-<version>-linux-<arch>.tar.gz` | the executable and its files, flat |
| Windows | `<name>-<version>-windows-<arch>.zip` | `<Name>/<Name>.exe` and its files |
| macOS | `<name>-<version>-darwin-<arch>.zip` | `<Name>.app` |
| AppImage | the `.AppImage` file itself (add it to the feed by hand) | |

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

`updater.update(onProgress)` runs check, download and apply. Errors carry a
code: `unsupported` (no `native.updater` config), `no_update` (download or apply
before check), `not_downloaded`, `bad_signature`, `permission_denied`.
Host ids: `krate/updater.check`, `krate/updater.download` (a stream of
`{ done, total }`), `krate/updater.apply`, `krate/updater.relaunch`.

## From Go

```go
import "github.com/kratejs/krate/packages/native/updater"

u, err := updater.Check(ctx, updater.Config{URL: url, PublicKey: key, Version: "1.0.0", CacheDir: cache})
if err == nil && u != nil {
    err = u.Download(ctx, func(done, total int64) { /* progress */ })
    if err == nil { err = u.Apply() }
    if err == nil { err = updater.Relaunch() }
}
```

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

The running process keeps its already loaded code until `relaunch`.

## Not done yet

Delta updates: a later feed can carry optional `patches` entries (binary diffs
from named older versions) and fall back to the full file; clients that do not
know the field ignore it. Rollback after a failed first start, staged rollouts
and background download scheduling are left to the app.
