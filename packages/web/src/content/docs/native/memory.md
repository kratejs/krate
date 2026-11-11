---
title: Memory
order: 16
description: How Krate Native keeps the system webview's memory down - the balanced, minimal and performance profiles and the WKWebView and WebView2 levers (untested).
---

# Memory

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

The system webview is most of a native app's memory. `native.memory` picks a
profile (`balanced` by default, `minimal`, `performance`) and `native.features`
records which web features the app uses; the compiler detects them and writes
both into `app.json`. This page has one section per platform family; the Linux
section is written by the Linux work and may be added next to this one.

## macOS and Windows

> **Untested.** Everything in this section was written on Linux from Apple's
> WebKit headers and the WebView2 headers and documentation. It compiles for
> `darwin` and `windows`, its decision tables are unit tested, but none of it has
> run on a Mac or a Windows machine, and no memory number has been measured.
> The `Native memory report` step in CI (`.github/workflows/ci.yml`, job
> `native`) runs `krate native profile --memory` on macOS and Windows runners
> and uploads `report.json` for the `balanced` and `performance` profiles; this
> section stays marked untested until that step reports. Treat the savings as
> hypotheses.

Every lever is guarded. A missing Objective-C selector, a failed
`QueryInterface` or a rejected argument skips that lever and logs one line to
stderr; it never stops the app from starting. If the WebView2 runtime refuses
the memory arguments, the app starts again without them.

Inputs, read from `app.json`: `memory` (`balanced` when missing or unknown) and
`features` (`media`, `webaudio`, `webgl`, `webrtc`, `spellcheck`). When the
manifest has no `features` object, nothing is switched off for lack of
detection. When it has one, every feature that is missing or false counts as
unused and its lever applies.

### Profiles

| Profile | macOS (WKWebView) | Windows (WebView2) |
| --- | --- | --- |
| `performance` | Nothing changes. | Nothing changes. |
| `balanced` | Page cache off, scripts cannot open windows on their own, unused features off. | One renderer process, spare renderer and back/forward cache off, unused features off. |
| `minimal` | `balanced`, plus the WebKit memory cache is purged after the first load and every two minutes. | `balanced`, plus a 512 MB JavaScript heap limit, no on-disk shader cache and `MemoryUsageTargetLevel` LOW while visible. |

### macOS

The default `WKWebsiteDataStore` is always kept, so `localStorage` and
cookies persist. All windows already share one `WKProcessPool`.

| Lever | API | Profiles |
| --- | --- | --- |
| `mediaTypesRequiringUserActionForPlayback = all` | public | `balanced`, `minimal`, when `media` is unused |
| `javaScriptCanOpenWindowsAutomatically = NO` | public | `balanced`, `minimal` |
| Remove `WKWebsiteDataTypeMemoryCache` with `removeDataOfTypes:modifiedSince:completionHandler:` | public | `minimal` |
| WebGL off (`WebGLEnabled`, `_setWebGLEnabled:`) | private | `balanced`, `minimal`, when `webgl` is unused |
| Web Audio off (`WebAudioEnabled`, `_setWebAudioEnabled:`) | private | when `webaudio` is unused |
| `navigator.mediaDevices` off (`MediaDevicesEnabled`, `_setMediaDevicesEnabled:`) | private | when `webrtc` is unused |
| Back/forward page cache off (`UsesBackForwardCache`, `_setUsesBackForwardCache:` or `_setPageCacheEnabled:`) | private | `balanced`, `minimal` |

The private levers use `WKPreferencesPrivate.h` names remembered without access
to the header. They try `-[WKPreferences _setEnabled:forFeature:]` with a
feature found by key in `+[WKPreferences _features]`, then the direct setters,
each only after `respondsToSelector:`. They are not public API, may change in
any macOS release and can be absent, in which case they are skipped. The
completion handler of `removeDataOfTypes:...` is declared non-null, so a
no-op block is passed.

### Windows

Windows has one WebView2 environment for the whole app, so the browser
arguments are set once through an `ICoreWebView2EnvironmentOptions` object
implemented in Go (`AdditionalBrowserArguments`). WebView2 requires identical
arguments for one user data folder: a second copy of the app started with a
different `memory` profile while the first runs falls back to starting without
the arguments.

| Argument | Profiles | Why |
| --- | --- | --- |
| `--renderer-process-limit=1` | `balanced`, `minimal` | One origin, trusted content. A limit, not a guarantee: site isolation can still add processes for cross-origin frames. |
| `--disable-features=SpareRendererForSitePerProcess` | `balanced`, `minimal` | Chromium feature that keeps an idle renderer process warm. |
| `...,BackForwardCache` | `balanced`, `minimal` | Keeps whole previous pages alive. |
| `...,msWebOOUI,msPdfOOUI` | `balanced`, `minimal` | Edge out-of-process UI surfaces. Not documented by Microsoft; Tauri ships the same two names by default. Effect unmeasured. |
| `--js-flags=--max-old-space-size=512` | `minimal` | Caps the V8 heap. A page that really needs more crashes its renderer. |
| `--disable-gpu-shader-disk-cache` | `minimal` | No on-disk shader cache. |
| `--disable-webgl` | `balanced`, `minimal`, when `webgl` is unused | No WebGL contexts. |

Not used on purpose: `msSmartScreenProtection` stays on because it is a
security feature; `--disable-background-networking` is left out because it also
stops component updates such as certificate revocation data; `--disable-gpu`
moves all rendering to the CPU and is only a choice the app can make through
`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`. `media`, `webaudio` and `spellcheck`
have no safe Chromium switch.

Two more Windows levers: when `webrtc` is unused, camera and microphone
permission requests are denied without a prompt (`PermissionRequested`; a
safeguard more than a saving), and in `minimal` the webview's
`ICoreWebView2_19::MemoryUsageTargetLevel` is set to LOW at creation. That
interface needs a recent runtime; on an older one the call is skipped. The
`ICoreWebView2Settings` stay as they are: none of them affects memory.
