---
title: Memory
order: 15
description: Memory profiles, feature detection, Linux levers, shared web process, background mode, macOS and Windows levers, and measured numbers.
---

# Memory

> **Preview.** Part of [Krate Native](/docs/native/). Linux (WebKitGTK) is the
> platform where the levers are written and measured. The macOS (WKWebView) and
> Windows (WebView2) levers compile and have unit-tested decision tables, but
> have never run on those systems; see [macOS and Windows](#macos-and-windows-untested).

## Profiles and config

A native app is several processes: the shell (the window, the Go runtime and
the QuickJS host), the system webview's web process and network process, and
a Go host child when the app has Go host code and is not linked into one
executable. The profile decides how hard Krate trims them.

```ts
// krate.config.ts
export default {
  native: {
    memory: "balanced", // "balanced" (default) | "minimal" | "performance"
    features: { media: true }, // optional overrides, see below
    host: { memoryMB: 64 }, // optional QuickJS heap limit override
    window: { keepAlive: false }, // tray apps: see Windows and background mode
  },
};
```

`krate native build` writes these to `app.json` as `memory`, `features`,
`hostjs.memoryMB` and `window.keepAlive`, and the shell reads them once at
load. One path carries the profile and the features to every platform layer.

| Profile | What it does |
|---------|--------------|
| `performance` | The engine defaults. No lever below runs. This is how the preview behaved before profiles existed. |
| `balanced` (default) | Switches off the webview features the app does not use, uses the document-viewer cache model, drops the HTTP disk cache, sets memory pressure thresholds, lowers the host limits and returns freed memory to the OS when the app is idle. No measurable CPU cost. |
| `minimal` | Everything in `balanced` with tighter thresholds and limits, and JavaScriptCore's JIT turned off in the web and network processes (Linux). Page script runs several times slower than with the JIT. Pick it for apps whose scripts are light. |

## Features

The `balanced` and `minimal` profiles switch off optional engine features that
the UI does not use. `krate native build` scans the built UI (HTML and
JavaScript) and writes the result to `app.json` as `features`:

| Feature | Found by |
|---------|----------|
| `media` | `<video>`, `<audio>`, `new Audio(`, `MediaSource`, `HTMLMediaElement`, `createElement("video" or "audio")`; also on when `webrtc` is |
| `webaudio` | `AudioContext`, `OfflineAudioContext`, `webkitAudioContext` |
| `webgl` | `getContext("webgl")`, `"webgl2"`, `"experimental-webgl"` |
| `webrtc` | `RTCPeerConnection`, `getUserMedia`, `getDisplayMedia` |
| `spellcheck` | `contenteditable`, or a `spellcheck` attribute that is not false |

The scan is textual and errs on the side of turning a feature on: a library in
your bundle that mentions `AudioContext` switches Web Audio on. A feature your
code reaches in a way the scan cannot see (an API name built at run time)
would be off, so set it explicitly:

```ts
native: { features: { webgl: true } }   // force on
native: { features: { media: false } }  // force off, even if detected
```

An `app.json` without a `features` block (built before profiles existed) keeps
every feature on.

## Linux levers

Verified on Linux. Each lever is a small function in `platform/linux` that checks
that the loaded WebKitGTK has the symbol it needs and skips itself otherwise.

| Lever | Profiles | What it saves |
|-------|----------|---------------|
| Cache model `DOCUMENT_VIEWER` | balanced, minimal | No resource cache between loads. The app's pages come from the custom scheme, so a browser-style cache would only hold memory. |
| Memory pressure settings (`webkit_website_data_manager_set_memory_pressure_settings`) | balanced, minimal | The web and network processes drop caches and collect garbage once they pass 200 MB (balanced: conservative 0.5, strict 0.75, polled every 5 s) or 150 MB (minimal: 0.4, 0.6, every 3 s). WebKitGTK has one process-wide setting for both processes. The kill threshold is off. Applied before the first process starts. |
| WebKit settings off unless the feature is on: `enable-media`, `-mediasource`, `-media-stream`, `-webaudio`, `-webgl`, `-webrtc`, `-encrypted-media` | balanced, minimal | The corresponding subsystem is not initialized in the web process. |
| `enable-dns-prefetching`, `enable-offline-web-application-cache`, `enable-page-cache` off | balanced, minimal | No prefetch resolver, no application cache, no whole previous pages kept for back navigation. |
| Spell checking follows the `spellcheck` feature | balanced, minimal | The enchant dictionaries are not loaded. |
| Throwaway HTTP disk cache directory, favicons off, intelligent tracking prevention off | balanced, minimal | Nothing is written to a cache that the app origin never fills. `localStorage` and IndexedDB stay in the app's data directory and persist. |
| `JSC_useJIT=false` for the web and network processes | minimal | The JIT code region. Costs CPU on script-heavy pages. A `JSC_useJIT` you set yourself wins. |
| `malloc_trim(0)` (glibc, found with `dlsym`, skipped on musl) | balanced, minimal | Returns freed GTK and GLib heap pages to the kernel. |
| `debug.FreeOSMemory()` and a QuickJS full GC | balanced, minimal | Returns the Go heap and collects cycles in the host VM. |
| Go soft memory limit (`GOMEMLIMIT`) 64 MiB (balanced) or 32 MiB (minimal), unless the variable is set | balanced, minimal | Keeps the Go heap from growing past the limit when idle. It is a soft limit: a program whose live heap is larger still works, with more GC. Applies to the shell, the Go host child and the one-process executable. |
| QuickJS heap limit 64 MiB (balanced), 32 MiB (minimal) instead of 256 MiB; `native.host.memoryMB` overrides | balanced, minimal | A runaway host script fails early instead of growing. |

Trimming happens 2 seconds after the app first goes quiet (the first page has
loaded), when a window closes, and after 30 seconds (15 on `minimal`) without
a request reaching the host.

### One process for apps with Go host code

`krate native build` links an app that has Go host code into **one
executable** when Go is installed, like `--release`. The Go host child's
runtime (its own heap, scheduler and socket) is gone. `krate native build
--layout shell` keeps the prebuilt shell with a Go host child; `krate native
dev` always runs two processes so that Go code can be rebuilt without closing
the window.

## Windows and background mode

A webview is most of an app's memory: the web process of one window costs
more than the Go shell and the host code together. Three things keep that
cost low as windows come and go.

### Windows share one web process

Every window of an app loads the same origin, so they do not need separate web
processes.

| OS | What happens |
|----|--------------|
| Linux | The first window creates a WebKitWebView; every later one is created with `webkit_web_view_new_with_related_view` of a live view. They share the web context, the website data (cookies, local storage) and one WebKitWebProcess. Closing the first window is fine: the next window is related to any view that is still alive, and when the last view goes the web process exits and the next window starts a new one. Settings are applied to each view as it is created. |
| Windows | The app creates one `ICoreWebView2Environment` (one user data folder, `%LOCALAPPDATA%\<appId>\WebView2`) and every window asks it for a controller, so all windows run under one WebView2 browser process. The renderer processes are WebView2's own business; Krate does not pass `--renderer-process-limit`. |
| macOS | All windows use one `WKProcessPool`. WebKit starts a WebContent process per web view unless the pool says otherwise, so the pool is created with the private `_WKProcessPoolConfiguration.usesSingleWebProcess` switch when the class and both selectors exist (`respondsToSelector` is checked); a WebKit without them keeps one process per window. |

A window can also hold several views as split panes
([Views](/docs/native/views/)); they share the same web process, and an extra view
costs about 1 MB of web process memory on Linux.

### Release a window, run in the background

`Hide` with release hides a window and destroys its webview:

```go
window.Get("settings").Hide(window.HideOptions{Release: true})
// later, for example on a tray click:
window.Get("settings").Show()
```

```ts
import * as win from "@krate/app/window";
await win.get("settings").hide({ release: true });
await win.get("settings").show();
```

When every window is released the web process is gone; what is left is the Go
shell, the Go host and, on Linux, the WebKit network process. Before the
webview is destroyed the shell asks the page for `location.pathname +
location.search + location.hash` and the scroll position (a page that does
not answer within 1.5 seconds is released with the route WebKit reports).
`Show` (or `Focus`) creates a new webview on that route and scrolls back to
the saved position once the page has loaded. The page is loaded again, so it
starts with fresh JavaScript state and subscribes to the host signals again;
signals are the source of truth and the endpoint sends their current values
first. A route set with `Navigate` while the window is released replaces the
saved one. A window opened with a scoped permission list keeps its window id
across the restore.

The window events stream reports `released` and `restored`. Where the platform
cannot release a webview, `app.Supports("window:release")` is false and
`Hide` with `Release` only hides the window.

`native.window.keepAlive` (`window.keepAlive` in `app.json`) is for tray apps:

```ts
export default defineConfig({
  native: { window: { keepAlive: true } },
});
```

With it the app does not quit when its last window closes. Closing the last
window (the title bar button, or `Close`) releases that window instead of
destroying it, and `Show` brings it back, so a tray click, a menu item or a
second launch (`app.OnSecondInstance`) reopens the main window with
`window.Main().Show()`. At most one window is kept this way; another window
that closes while one is released is destroyed. Closing a window that is
already released destroys it, and the app keeps running with no window until
`app.Quit()`. Without `keepAlive` the app quits when the last window is gone,
as before.

### Hidden and minimized windows shrink

A window that is hidden (`Hide`) or minimized keeps its page. The shell tells
the page, then trims its own heap:

- The page gets a `krate:memory-pressure` event on `window`. A page uses it to
  drop caches and large buffers (`onMemoryPressure(fn)` in `@krate/app/window`
  wraps it and returns a function that stops listening). It fires on `Hide`
  and when the window is minimized; it does not fire when the window is merely
  covered.
- The shell runs the same trim as the idle reaper (a Go collection that
  returns free heap to the system and, on Linux, glibc `malloc_trim`), about
  400 ms later (once for a burst of hides).

| OS | Also |
|----|------|
| Linux | Nothing else: WebKitGTK has no call that puts a web process under memory pressure. It does throttle a page whose window is unmapped. |
| Windows | `ICoreWebView2Controller::put_IsVisible(FALSE)` and, through `QueryInterface` to `ICoreWebView2_19`, `put_MemoryUsageTargetLevel(LOW)`; shown again, the level goes back to the one a visible view has in the profile (LOW for `minimal`, NORMAL otherwise) and the controller is visible. A WebView2 runtime without `ICoreWebView2_19` skips the memory target. |
| macOS | Nothing: WebKit throttles hidden and miniaturized views by itself. |

Only a released window gives its web process memory back; a hidden page keeps
what it has.


## macOS and Windows (untested)

> **Untested.** Everything in this section was written on Linux from Apple's
> WebKit headers and the WebView2 headers and documentation. It compiles for
> `darwin` and `windows`, its decision tables are unit tested, but none of it has
> run on a Mac or a Windows machine, and no memory number has been measured.
> The `Native memory report` step in CI (`.github/workflows/ci.yml`, job
> `native`) runs `krate native profile --memory` on macOS and Windows runners
> and uploads `report.json` for the `balanced` and `performance` profiles; this
> section stays marked untested until that step reports. The window release and
> hide behavior described above is untested there too. Treat the savings as
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

Every switch and feature name below was checked against the Chromium sources
(`content_switches.cc`, `gpu_switches.cc`, `content_features.cc`) and
Microsoft's WebView2 browser flag list, and a unit test lists each emitted name
with its source, so an argument cannot be added without that check. That a
name exists does not mean WebView2 honors it or that it saves memory: no
number has been measured on Windows. Microsoft's own page says browser flags
are for development and may change; the arguments are also skipped
automatically if the runtime refuses them.

| Argument | Profiles | Why |
| --- | --- | --- |
| `--renderer-process-limit=1` | `balanced`, `minimal` | One origin, trusted content. A limit, not a guarantee: site isolation can still add processes for cross-origin frames. |
| `--disable-features=SpareRendererForSitePerProcess` | `balanced`, `minimal` | Chromium feature that keeps an idle renderer process warm. |
| `...,BackForwardCache` | `balanced`, `minimal` | Keeps whole previous pages alive. |
| `...,msWebOOUI,msPdfOOUI` | `balanced`, `minimal` | Edge out-of-process UI surfaces. Not in Microsoft's flag list; Tauri ships the same two names by default, and Chromium ignores a feature name it does not know. Effect unmeasured. |
| `--js-flags=--max-old-space-size=512` | `minimal` | Caps the V8 heap. A page that really needs more crashes its renderer. |
| `--disable-gpu-shader-disk-cache` | `minimal` | No on-disk shader cache. |
| `--disable-webgl --disable-webgl2` | `balanced`, `minimal`, when `webgl` is unused | No WebGL contexts. Both are defined in Chromium `content_switches.cc`. |

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

## Measuring

`krate native profile --memory [dir] [--idle 30s] [--out dist-native]` builds
the app, runs it and reports **PSS** and **private dirty** memory per process
role next to a blank-page floor built from the same shell; see
[Measuring memory](/docs/native/release/#measuring-memory). On Linux you can
also sample the process tree yourself:

```sh
for p in $(pgrep -P $APP_PID) $APP_PID; do
  awk '/^Pss:/{s=$2}/^Private_Dirty:/{d=$2}END{print s/1024, d/1024}' /proc/$p/smaps_rollup
done
```

Edit `memory` in a built `app.json` (prebuilt-shell layout) to compare profiles
without rebuilding.

## Measured

Linux only. WebKitGTK 2.54 under Xvfb with software rendering, on a shared
12-core machine (load average below 2 during the runs). The example is
`examples/native-tasks` with a scenario script baked into the page. Every
number is the median of 5 runs, interleaved between the "before" build
(the tree before profiles, shared web process and the one-process layout) and
the "after" build, sampled 14 s after launch from `/proc/<pid>/smaps_rollup`
summed over the process tree. Cells are **PSS / private dirty** in MB; the
Total column is the median of the totals, so a row need not equal the sum of
its cells.

| Setup | Shell | Web process(es) | Network process | Go host child | Total |
|-------|-------|-----------------|-----------------|---------------|-------|
| Blank page, before | 70 / 29 | 76 / 17 | 32 / 13 | none | **183 / 64** |
| Blank page, after | 73 / 33 | 74 / 15 | 34 / 15 | none | **182 / 63** |
| 1 window, before (shell + Go host child) | 77 / 35 | 86 / 18 | 30 / 11 | 10 / 5 | **205 / 72** |
| 1 window, after (`krate native build`, one process, `balanced`) | 76 / 35 | 87 / 19 | 32 / 13 | none | **195 / 67** |
| 1 window, after, `--layout shell` (`balanced`) | 76 / 34 | 86 / 19 | 30 / 11 | 10 / 5 | **203 / 69** |
| 1 window, after, `minimal` | 74 / 33 | 81 / 19 | 30 / 11 | none | **186 / 64** |
| 3 windows, before (3 web processes) | 72 / 31 | 149 / 60 | 25 / 12 | 10 / 5 | **256 / 109** |
| 3 windows, after (1 web process) | 84 / 35 | 105 / 29 | 30 / 11 | none | **220 / 76** |
| After, all windows released (`keepAlive`) | 90 / 32 | none | 38 / 11 | none | **131 / 47** |

What to read from this:

- **Windows are where the big saving is.** Three windows cost 256 / 109 before
  (three web processes) and 220 / 76 after (one). Releasing every window of a
  tray app drops the web process: 131 / 47, most of what is left being the
  shell and the network process.
- **The one-process layout removes the Go host child**, about 10 / 5 per app
  (compare the two "after" 1 window rows with `--layout shell`).
- **`balanced` does not lower an idle small app by itself.** The blank page is
  the same before and after (183 / 64 and 182 / 63), and the app minus the
  blank page shrinks from 22 / 7 to 13 / 4, which is mostly the host child.
  Its levers bound growth (caches, unused subsystems, a growing heap) rather
  than lowering the first number.
- **`minimal` saves about 9 / 3 more**, mostly in the web process, by turning
  the JIT off.
- **Noise is large.** Run to run, the PSS total of one configuration varied by
  up to 30 MB (for example 193 to 225 for the 1 window before row), so
  differences under about 15 MB between single-window rows are not
  significant; the 3-window and released rows are. Private dirty is steadier
  than PSS, which also counts shared library pages and moves with whatever
  else uses WebKit on the machine.
- Not measured: macOS and Windows, hardware rendering, apps with media, WebGL
  or large pages, and a page that grows its heap over time.
