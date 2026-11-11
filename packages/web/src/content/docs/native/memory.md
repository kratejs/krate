---
title: Memory Profiles
order: 15
description: native.memory profiles (balanced, minimal, performance), the levers each one applies to the webview, the QuickJS host and the Go runtime, the native.features overrides, and measured numbers.
---

# Memory profiles

> **Preview.** Part of [Krate Native](/docs/native/). Linux (WebKitGTK) is the
> first and, for now, the only platform that applies the webview levers. On
> macOS and Windows the profile is accepted and written to `app.json`, the
> shared host levers (QuickJS limit, Go limit, idle trimming) apply, and the
> webview behaves as before.

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
  },
};
```

| Profile | What it does |
|---------|--------------|
| `performance` | The engine defaults. No lever below runs. This is how the preview behaved before profiles existed. |
| `balanced` (default) | Switches off the webview features the app does not use, uses the document-viewer cache model, drops the HTTP disk cache, sets memory pressure thresholds, lowers the host limits and returns freed memory to the OS when the app is idle. No measurable CPU cost. |
| `minimal` | Everything in `balanced` with tighter thresholds and limits, and JavaScriptCore's JIT turned off in the web and network processes. Page script runs several times slower than with the JIT. Pick it for apps whose scripts are light. |

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

## Levers

Linux, verified. Each lever is a small function in `platform/linux` that checks
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

### macOS and Windows

Not tuned yet. The profile and features are written to `app.json` and the
shared host levers run, but WKWebView and WebView2 configuration is unchanged.

## Measured

Linux, WebKitGTK 2.54 under Xvfb with software rendering, the shell in
`--dev` mode (developer extras on), each number the median of 5 runs
alternated between profiles, sampled 16 s after start from
`/proc/<pid>/smaps_rollup`. Values are **PSS / private dirty** in MB.

`examples/native-tasks` (prebuilt-shell layout, Go host child):

| Profile | Shell | WebKitWebProcess | WebKitNetworkProcess | Go host child | Total |
|---------|-------|------------------|----------------------|---------------|-------|
| `performance` | 62 / 21 | 86 / 18 | 30 / 11 | 8 / 2 | 186 / 52 |
| `balanced` | 62 / 20 | 87 / 19 | 30 / 11 | 8 / 2 | 187 / 53 |
| `minimal` | 61 / 19 | 79 / 17 | 30 / 11 | 8 / 3 | 178 / 50 |
| `balanced`, one process (`krate native build`) | 62 / 21 | 86 / 18 | 30 / 11 | none | 178 / 51 |

A blank page (no host code):

| Profile | Shell | WebKitWebProcess | WebKitNetworkProcess | Total |
|---------|-------|------------------|----------------------|-------|
| `performance` | 57 / 16 | 74 / 15 | 30 / 11 | 162 / 43 |
| `balanced` | 57 / 16 | 74 / 15 | 30 / 11 | 162 / 43 |
| `minimal` | 57 / 16 | 70 / 13 | 30 / 11 | 157 / 41 |

What to read from this:

- On a small app almost all of the memory is shared library code and the
  engine's fixed start-up cost. The levers act on what the app adds on top
  (caches, unused subsystems, a growing heap), so `balanced` is within noise
  of `performance` here. Its value shows up as bounded growth, not as a lower
  idle number.
- The one-process layout is the largest single saving for an app with Go host
  code: the whole child process.
- `minimal` saves about 8 MB PSS, all in the web process, by turning the JIT
  off. Turning the JIT off in the shell as well (setting `JSC_useJIT=false`
  from outside) measured the same, so there is no re-exec.
- Absolute PSS depends on what else shares the libraries on the machine (a
  second WebKit app lowers every number). Private dirty is steadier. Compare
  profiles within one session, as the table does.

### Measure it yourself

`krate native profile --memory` reports per-process PSS and private dirty of a
running app (it lands with the memory measurement tooling). Or sample the
process tree directly:

```sh
for p in $(pgrep -P $APP_PID) $APP_PID; do
  awk '/^Pss:/{s=$2}/^Private_Dirty:/{d=$2}END{print s/1024, d/1024}' /proc/$p/smaps_rollup
done
```

Edit `memory` in a built `app.json` to compare profiles without rebuilding.
