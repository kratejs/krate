# Manual test on Windows

The Win32 and COM code in this package cannot be exercised from Linux; it was
checked against WebView2.h by reading and by compiling for windows/amd64 and
windows/arm64. These steps verify it on a real machine.

## Prerequisites

- Windows 10 1809+ or Windows 11, amd64 or arm64, with a desktop session
  (not a service or SSH session; the tests open windows).
- The Evergreen WebView2 Runtime (installed by default on Windows 11 and by
  Microsoft Edge updates). Check with:
  `reg query "HKLM\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" /v pv`
- Go (version from `packages/native/go.mod`).

## 1. Automated GUI test

From `packages\native`:

```
set KRATE_NATIVE_GUI=1
go test ./platform/windows -run GUI -count=1 -v
```

Expected: three windows appear and close by themselves; all of
TestGUIRoundTrip, TestGUIDispatchFromManyGoroutines and TestGUITwoWindows
pass in well under 30 seconds. They assert, from inside the page:

- `location.origin` is `https://app.localhost` and `crypto.subtle` works
  (secure context),
- a 64 KiB POST to `/echo` comes back byte for byte,
- a 5 chunk response with 50 ms gaps arrives incrementally (the test fails if
  the chunks arrive within 120 ms of each other, which means buffering),
- navigating the top-level page to `https://example.com/` is cancelled,
- `/report` receives the results; 1000 goroutines calling Dispatch all run on
  the main thread; two windows work side by side.

Without `KRATE_NATIVE_GUI=1` the GUI tests are skipped.

## 2. Things the test does not cover

Check by hand with a small program (`platform.New`, `NewWindow`, `Run`):

1. Persistence: set `localStorage.x = "1"` in the page, quit, start again,
   read it back. The profile lives in `%LOCALAPPDATA%\<AppID>\WebView2`.
2. Closing the last window ends `Run` (unless `KeepAlive`); closing one of two
   windows keeps the app running.
3. Resizing the window resizes the page; dragging the window to a monitor
   with a different scale factor keeps the layout crisp (per-monitor DPI).
4. Developer tools: F12 and the context menu entry open only when `Debug` is
   true.
5. Streaming cancel: open a page that holds `fetch('/slow')` open (handler
   blocks on `r.Context().Done()`), navigate or close the window, and confirm
   the handler's context is cancelled.
6. Missing runtime: rename the registry key above (or test on a clean VM
   without Edge WebView2) and confirm `Run` returns an error that names the
   Evergreen bootstrapper link.
7. Fallback loader: put `WebView2Loader.dll` next to the executable on a
   machine where the registry route fails and confirm it is used.

## 2b. Views (split panes)

`TestGUIViews` covers identity, layout and removal. By hand, from a program that
calls `AddView`:

- Two views side by side leave a 6 px gap; the pointer turns into the
  left-right (or up-down) arrows over it and dragging moves the divider. A pane
  stops at its minimum size. The result is the same at 100 and 200 percent DPI.
- Dragging past the window edge, or releasing the button outside the window,
  ends the drag (capture is released).
- `Release` then `Show` brings back every view on its route with the divider
  where it was.
- `window.close()` in a secondary view removes it and sends a `closed` event
  (this relies on the WindowCloseRequested handler GUID, unverified).

## 3. If something fails

- `Run` returns "WebView2 Runtime not found" listing paths: the registry
  lookup or the `CreateWebViewEnvironmentWithOptionsInternal` call failed;
  the bracketed text has the HRESULT per attempt.
- Window opens but stays blank: the WebResourceRequested filter did not match
  or the response stream failed; look for `krate native:` lines on stderr.
- Large or streamed responses hang: the IStream `Read` path. The stream
  blocks on a background thread and pumps messages if WebView2 reads it on the
  UI thread; see `com_windows.go` (`goStream.read`).
