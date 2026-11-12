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
- a 5 chunk response with 50 ms gaps from `/__krate/stream/...` arrives
  incrementally (the test fails if the chunks arrive within 120 ms of each
  other, which means buffering). Streaming GETs (`/__krate/stream/`,
  `/__krate/signals`, `/__krate/hmr`) travel over web messages, because
  WebView2 reads a WebResourceRequested response stream to its end before the
  page sees it; every other response is delivered whole (`msgstream.go`),
- navigating the top-level page to `https://example.com/` is cancelled,
- `/report` receives the results; 1000 goroutines calling Dispatch all run on
  the main thread; two windows work side by side.

Without `KRATE_NATIVE_GUI=1` the GUI tests are skipped.

The same command also runs the tray, hotkey and toast tests
(`TestGUITrayLifecycle`, `TestGUIHotkeys`, `TestGUIToast`):

- Tray: add, modify (tooltip, image, a tooltip over the 127 character limit,
  the stock icon), a rejected invalid PNG, click and double-click events (sent
  as the messages the shell would post), the `TaskbarCreated` re-add, delete,
  delete again, add again. Skipped when there is no `Shell_TrayWnd`.
- Hotkeys: register, duplicate combination (the error must say "already
  registered by another app"), duplicate handle, unknown key, `WM_HOTKEY`
  reaching the handler, unregister, register again, and a stale id being
  ignored. It also injects the real key presses and logs whether they fired;
  a locked or non-interactive desktop drops injected input, so that part never
  fails the test.
- Toast: `Notify` succeeds (skipped if notifications are turned off), the
  AppUserModelID is registered with the app name, the toast is tracked, no
  Failed event arrives, replacing by id, `CloseNotification`.

Without a display, `go test ./platform/windows -count=1 -v` still runs
`TestToastXMLAcceptedByWinRT` (the real WinRT XML parser accepts every toast
the builder writes, including hostile text), `TestToastDelegateInterfaces`
(the delegate objects answer `IAgileObject` and the right
`TypedEventHandler` id and call back), `TestHStringRoundTrip` and
`TestAUMIDRegistration`.

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

### Tray, hotkeys and toasts (not visible to a test)

8. Tray: the icon appears in the notification area (or the overflow
   flyout), is sharp at 100%, 150% and 200% scaling, and shows its tooltip on
   hover. Left click reports `click`, double click `double-click`. Right click
   shows the menu; clicking outside the menu closes it (this is the
   `SetForegroundWindow` workaround; if the menu stays up, that is the bug).
   Choosing an item reports `menu` with its id.
9. Tray after explorer restarts: end `explorer.exe` in Task Manager and start
   it again (File, Run new task). The icon comes back by itself.
10. Tray on quit: after the app quits normally the icon is gone at once, with
    no ghost that disappears only when the mouse moves over it. (A killed
    process always leaves a ghost; that is Windows.)
11. Hotkeys: register `Ctrl+Alt+Shift+F9`, press it from another app and the
    handler fires once even when the keys are held (`MOD_NOREPEAT`). Registering
    `Win+L` or `Ctrl+Alt+Del` fails with a clear error. Start two copies of the
    app: the second gets "already registered by another app".
12. Toasts: a toast appears with the app name (not the exe name) and the
    default icon if `IconPath` is set. Buttons are shown; clicking the body
    and each button reports the notification id and the action id. Dismissing
    (the x) reports `closed`. The toast is in the Action Center afterwards.
    With Focus assist on, `Notify` still succeeds and the toast goes to the
    Action Center without a banner. With notifications turned off for the app
    in Settings, `Notify` returns the "turned off" error.
13. Toast after quit: quit the app and click a toast in the Action Center.
    Nothing happens (documented: a closed app is not started from a toast).
14. Packaged app: from an MSIX build the toast carries the package's name and
    icon and no `HKCU\Software\Classes\AppUserModelId` key is written.
15. Host children: with node, bun and deno installed, an app with a TypeScript
    host calls a host function from the page (the `hostchild` tests do this
    with a script, but not through the shell and a window). A firewall prompt
    must not appear (the listener is `127.0.0.1` only).

## 3. If something fails

- `Run` returns "WebView2 Runtime not found" listing paths: the registry
  lookup or the `CreateWebViewEnvironmentWithOptionsInternal` call failed;
  the bracketed text has the HRESULT per attempt.
- Window opens but stays blank: the WebResourceRequested filter did not match
  or the response stream failed; look for `krate native:` lines on stderr.
- `Shell_NotifyIcon failed`: no taskbar yet (explorer not running); the icon is
  re-added on `TaskbarCreated` only after one `SetTray` has succeeded.
- No toast and no error: Focus assist or the per-app switch in Settings,
  System, Notifications. The error text from `Notify` says "turned off" when
  Windows reports it. `Show` succeeding does not prove a banner was drawn.
- A toast is rejected with an HRESULT: the XML failed WinRT's parser; run
  `TestToastXMLAcceptedByWinRT` and look at `buildToastXML` in `toast.go`.
- A streamed response (signals, hmr, async iterables) never arrives: the
  fetch wrapper (`streamShim` in `msgstream.go`) was not injected or the web
  message channel failed; look for `add_WebMessageReceived` lines on stderr.
  Any other response that never ends is held back by WebView2 until it does.

## Print dialog

`TestGUIPagePrintDialog` opens the system print dialog, which nothing in a test can
close, so it only runs on request:

```sh
KRATE_NATIVE_GUI=1 KRATE_NATIVE_PRINT_DIALOG=1 go test ./platform/windows -run TestGUIPagePrintDialog -v
```

Check that the Windows print dialog appears for the window and that cancelling it
leaves the app running. The PDF output, zoom, find, capture, downloads and cookies
are covered by the other `TestGUIPage*` tests.

