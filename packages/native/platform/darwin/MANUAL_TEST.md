# macOS GUI smoke test

The darwin layer was written without access to a Mac. Run this once on real
hardware (Apple silicon and, if you can, Intel) before trusting it.

## Setup

- macOS 12 or newer, logged-in GUI session (not plain SSH).
- Go 1.26.1 (`GOTOOLCHAIN=go1.26.1`), no Xcode needed.

## Run

```sh
cd packages/native
KRATE_NATIVE_GUI=1 CGO_ENABLED=0 go test ./platform/darwin -run . -v -count=1
```

Without `KRATE_NATIVE_GUI=1` the GUI tests skip; the pure-Go unit tests
always run. The GUI run includes `ext_gui_test.go`: the tray item and its menu
(read back from the `NSStatusItem` and `NSMenu`, and driven with
`performActionForItemAtIndex:`), hotkey register, duplicate and unregister,
notifications in an unbundled process (the documented error) and in a
generated `.app` (`TestNotifyBundled` copies the test binary into a bundle
and runs it; whether the notification is allowed depends on the machine, the
test only requires that the bundle is recognized and nothing crashes).

The test binary is not an app bundle, so its first window can open behind other
windows. Click the new icon in the Dock if nothing appears.

## What should happen

1. `TestScenario` opens a 640x480 window. Within a few seconds the page, served
   from `krate://app/`, runs and posts a report. The test passes when:
   - `location.origin` is `krate://app`
   - `crypto.subtle.digest` works (needs a secure context)
   - a 64 KiB `fetch` POST to `/echo` comes back byte-identical
   - `/stream` delivers `chunk0..chunk4` in at least two reads spread over
     150 ms or more (incremental delivery, 50 ms between chunks)
   - the page's `POST /report` reaches the Go handler
   Then the title changes to "renamed", the window resizes to 500x400 and its
   body text becomes "eval ok" (visible for an instant before it closes).
2. `TestDispatchFromManyGoroutines` runs 1000 `Dispatch` calls from 1000
   goroutines; all must run on the main thread.
3. `TestTwoWindows` opens two windows and both must report.
4. The process exits with `PASS`. The app does not hang at the end.

## Also check by hand (not covered by the test)

Run a small program that uses `platform.New` with `Debug: true`:

- Cmd+Q quits and `Run` returns (put a print after `Run`).
- Cmd+C / Cmd+V / Cmd+A / Cmd+Z work inside an `<input>` (Edit menu).
- Cmd+W closes the window; closing the last window makes `Run` return.
- Right-click shows "Inspect Element" (macOS 13.3+ or older with the pref key).
- A link to `https://example.com` and `window.open` do nothing (navigation
  lock); a link to `/other` works.
- Dragging the window while a stream is in flight does not stall delivery.

## Views (split panes)

`TestGUIViews` adds a second view to a window and checks the request identity,
the layout and removal. By hand, from a program that calls `AddView`:

- Two views side by side show a draggable divider; dragging it changes the
  ratio `Layout()` reports, and a pane stops at its `MinWidth`.
- `SetLayout` rearranges the panes without reloading their pages.
- `RemoveView` gives the space to the neighbour; `Release` then `Show` brings
  back every view with its route and divider position.
- `webView:startURLSchemeTask:` receives the right web view: the view tag in
  `/viewping` requests matches the view that loaded the page.
- Known gap: `window.close()` in a secondary view does nothing yet.

## Tray, global shortcut and notifications (by hand)

CI cannot click the status bar, press a key system-wide or look at the
notification center. Run these once on a Mac. The simplest way is
`krate native dev` on an app that uses `tray`, `shortcut` and `notify` (it
starts the shell from the generated dev bundle `.krate/dev/<name>.app`).

Tray:

1. The icon appears in the menu bar with the tooltip on hover. A black or
   white single-color PNG follows light and dark mode (switch the appearance in
   System Settings); a colored PNG keeps its colors. A 36 px PNG is 18 pt high.
2. With a menu: a left click and a right click both open it, and the page gets
   a `click` event. Menu items arrive as `menu` events with their id. A check
   item toggles its checkmark; choosing a radio item moves the checkmark; the
   disabled item is grayed; the submenu opens; accelerators are shown.
3. `tray.SetMenu` with no items, then click the icon: `click` arrives without
   a menu; a right click gives `right-click`; a quick double click gives
   `double-click`.
4. `tray.Remove()` removes the icon; quitting the app removes it too.

Global shortcut (try `Cmd+Shift+K`, then `Ctrl+Alt+Space`):

1. Register it, switch to another app and press the keys: the callback runs
   without any Accessibility prompt.
2. Register the same combination twice: the second call fails with an error
   naming the combination. Pick one the system uses (for example `Cmd+Space`
   with Spotlight on): the call fails or the system wins; note which.
3. Unregister it and press the keys again: nothing happens. Register again.
4. `Alt+Shift+K` (only Option and Shift) is expected not to fire on macOS 15.

Notifications:

1. First call from the dev bundle: the permission prompt appears for the
   `<appId>.dev` app. Allow it; the banner shows the title, body and action
   buttons (hover or expand the banner).
2. Click the body: `OnEvent` gets `Action == ""`. Click an action button: it
   gets that action id. Dismiss with the close button: `Closed == true`.
3. Send with the same `ID` twice: the second replaces the first. `Close(id)`
   removes it from the notification center.
4. Refuse the permission (or turn it off in System Settings > Notifications):
   `Send` fails with `permission_denied` and the text points at System
   Settings. Turn it back on and send again: it works without restarting.
5. Run the shell directly (`.krate/native/bin/krate-shell .krate/native/dev/app`,
   no bundle): `Send` fails with the "not running from an .app bundle" error
   and `app.Supports("notify")` is false.
6. With the app frontmost, a notification still shows a banner.

Memory settings: run an app with `memory: "minimal"` and `Debug` on; stderr
shows at most one `krate/native: memory: ... skipped` line per lever that this
macOS does not have, and the app starts either way.

## If something fails

| Symptom | Likely cause |
| --- | --- |
| Crash at start with "unrecognized selector" or an NSException | A selector or type encoding is wrong; the exception text names it. |
| `crypto.subtle` missing, `isSecureContext` false | WKProcessPool `_registerURLSchemeAsSecure:` not honored on this macOS. See `registerSchemePrivate` in `app_darwin.go`. |
| `fetch` rejects with a TypeError, `/echo` handler never runs | Scheme not CORS-enabled (`_registerURLSchemeAsCORSEnabled:`), or WebKit refuses fetch to custom schemes. |
| Echo length is 0 or the server saw 0 bytes | The body arrived in `HTTPBodyStream` and was not drained, see `requestBody` in `scheme_darwin.go`. |
| Stream arrives in a single read after 200 ms | WebKit buffered `didReceiveData`; check `reads` in the failure message. |
| Hang in `TestScenario` with a white window | `decideNavigation` did not call the block (check `invokeBlock1`), or the scheme handler was not registered. |
| `Run` never returns after Cmd+Q | The stop/post-event wakeup in `Quit` is not working (NSPoint struct argument?). |
| `unrecognized selector` naming a `_set...` selector | A memory lever bypassed its guard; every selector must go through `canSend` in `memory_darwin.go`. |
| Tray icon invisible | An icon with no PNG and no title has no width (SetTray refuses it); otherwise check the PNG decodes. |
| Hotkey never fires | The key code table is for an ANSI layout; macOS 15 ignores Option/Shift-only combinations; the handler runs on the main loop, so a blocked main thread delays it. |
| Notification call hangs about two minutes | The permission prompt was never answered (`notifyAuthTimeout`); answer it. |
| Crash only on Intel | Struct-by-value argument passing for NSRect (32 bytes, passed in memory on amd64). |

## Print dialog

`TestGUIPagePrintDialog` opens the system print dialog, which nothing in a test can
close, so it only runs on request:

```sh
KRATE_NATIVE_GUI=1 KRATE_NATIVE_PRINT_DIALOG=1 go test ./platform/darwin -run TestGUIPagePrintDialog -v
```

Check that the macOS print dialog appears for the window and that cancelling it
leaves the app running. The PDF output, zoom, find, capture, downloads and cookies
are covered by the other `TestGUIPage*` tests.

