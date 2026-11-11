---
title: Memory
order: 15
description: How a Krate Native app uses memory - windows that share one web process, background mode for tray apps, and what hidden windows do.
---

# Memory

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

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
- The shell runs a Go collection that returns free heap to the system and, on
  Linux, glibc `malloc_trim`, about 400 ms later (once for a burst of hides).

| OS | Also |
|----|------|
| Linux | Nothing else: WebKitGTK has no call that puts a web process under memory pressure. It does throttle a page whose window is unmapped. |
| Windows | `ICoreWebView2Controller::put_IsVisible(FALSE)` and, through `QueryInterface` to `ICoreWebView2_19`, `put_MemoryUsageTargetLevel(LOW)`; shown again, the level goes back to NORMAL and the controller visible. A WebView2 runtime without `ICoreWebView2_19` skips the memory target. |
| macOS | Nothing: WebKit throttles hidden and miniaturized views by itself. |

Only a released window gives its web process memory back; a hidden page keeps
what it has.

### Measurements

Linux, WebKitGTK 2.54, Xvfb with software rendering, the "Native Tasks"
example with a Go host and one signal. PSS in MB, summed over the shell, the
Go host and the WebKit processes; three runs, median.

| State | One web process per window (before) | Shared web process (now) |
|-------|-------------------------------------|--------------------------|
| 1 window | 135 PSS, 55 private dirty | 137 PSS, 56 private dirty |
| 3 windows | 210 PSS, 96 private dirty (3 web processes) | 156 PSS, 59 private dirty (1 web process) |
| Cost of each extra window | 31 to 43 PSS, 19 to 22 private dirty | 1.5 to 36 PSS (median 7.8), 1.4 to 1.8 private dirty |
| All windows hidden, no release | 211 PSS, 97 private dirty (3 web processes) | |
| All windows released | | 86 PSS, 35 private dirty (0 web processes) |

Private dirty memory is the steady number. PSS also counts the shared library
pages of the WebKit processes, so it moves with what else runs on the machine
(the three runs of one configuration differ by up to 30 MB, and the 36 MB extra
window above is such an outlier). The memory a released tray app still holds is
the shell (about 23 MB private, plus the mapped WebKitGTK and GTK libraries),
the Go host (2 MB) and the WebKit network process (12 MB private); it does not
reach 45 MB of PSS in this setup, only of private memory.

On Windows and macOS the same flow is implemented but has not been measured.
