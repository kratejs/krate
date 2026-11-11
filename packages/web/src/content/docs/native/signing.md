---
title: Signing and notarization
order: 13
description: krate native sign and notarize - macOS codesign and notarytool, Windows signtool or osslsigncode, Linux gpg signatures, and where secrets come from.
---

# Signing and notarization

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

```sh
krate native package --sign [--notarize]     # sign while packaging
krate native sign [dir] [--target <os/arch>]  # sign what is already packaged
krate native notarize [dir]                   # macOS: notarize and staple
```

`sign` works on `dist-native/packages/<os>-<arch>/`, signs the files inside,
then rebuilds the zip, dmg and installers from the signed files and refreshes
the update signature, so every archive carries signed code. The signing tools
are platform tools; each is found in `PATH` and a missing one is `KR7224` with a
hint. Secrets come from environment variables or files. They are never printed:
a password argument shows as `****` in the command echo.

## macOS

Runs only on macOS (`codesign` is part of macOS). The identity is
`--identity "Developer ID Application: Acme Inc (ABCDE12345)"` or
`KRATE_MACOS_IDENTITY`. With no identity the app is signed **ad hoc**
(`--sign -`), which runs locally but is not trusted by Gatekeeper.

Signing goes inside out: every nested Mach-O file, framework and helper bundle
first (the Go host binary, bundled runtimes), then the `.app`, each with
`codesign --force --options runtime --timestamp --entitlements <file> --sign
<id>`, then `codesign --verify --deep --strict`. An ad hoc signature uses
`--timestamp=none`.

**Entitlements** are generated next to the package as `entitlements.plist`, or
pass `--entitlements <file>`. A WKWebView app needs none: the page's JavaScript
and its JIT run in Apple's WebContent process, and the embedded QuickJS host
and the Go runtime do not JIT. If `native.host.runtime` is `node`, `bun` or
`deno` the file gets `com.apple.security.cs.allow-jit` and
`allow-unsigned-executable-memory`, which their JIT needs under the hardened
runtime. The app is not sandboxed by default, so the network client
entitlement is not added.

### Notarization

`krate native notarize` (or `package --sign --notarize`) zips the app with
`ditto`, runs `xcrun notarytool submit --wait`, then `xcrun stapler staple` and
`validate`, and rebuilds the archives so the ticket is inside them. Credentials,
first match wins:

| Source | Variables or flag |
|--------|-------------------|
| Keychain profile (best) | `--keychain-profile <name>` or `KRATE_NOTARY_PROFILE`; create it once with `xcrun notarytool store-credentials` |
| App Store Connect API key | `KRATE_NOTARY_KEY` (path of the `.p8`), `KRATE_NOTARY_KEY_ID`, `KRATE_NOTARY_ISSUER` |
| Apple ID | `KRATE_NOTARY_APPLE_ID`, `KRATE_NOTARY_TEAM_ID`, `KRATE_NOTARY_PASSWORD` (app-specific password) |

Missing credentials are `KR7226`. A password given to `notarytool` is a process
argument that other users on the machine can see; prefer a profile or key. A
rejected submission prints Apple's status and the id for `xcrun notarytool log`
(`KR7225`).

## Windows

Authenticode with `signtool` on Windows, or `osslsigncode` anywhere. Without
either, `KR7224`. A pure Go signer is out of scope.

| Variable or flag | Meaning |
|------------------|---------|
| `KRATE_WINDOWS_PFX` or `--pfx` | certificate file |
| `KRATE_WINDOWS_PFX_PASSWORD` | its password (environment only) |
| `KRATE_WINDOWS_CERT_SHA1` or `--thumbprint` | certificate in the Windows store (signtool only) |
| `KRATE_WINDOWS_TIMESTAMP_URL` or `--timestamp-url` | RFC 3161 server, default `http://timestamp.digicert.com` |

It runs `signtool sign /fd SHA256 /tr <ts> /td SHA256 /f <pfx> /p <password>`
(or `/sha1 <thumbprint>`) on the `.exe` and `.dll` files of the install tree
before the zip, the NSIS installer and the MSIX are built, then on the NSIS
installer and the MSIX. An MSIX installs only if its signature's subject
equals `bundle.windows.publisher`.

## Linux

With `--gpg-key <id>` (or `KRATE_GPG_KEY`) every `.deb`, `.rpm`, `.tar.gz` and
`.AppImage` gets a detached, armored signature `<file>.asc` from `gpg`. The key
passphrase is handled by gpg-agent.

## Update signatures are separate

Update files are signed with the updater key, not with a platform certificate;
see [Updates](/docs/native/updater/). Do both for a release: the platform
signature makes the OS trust the app, the updater signature makes the app trust
the update.
