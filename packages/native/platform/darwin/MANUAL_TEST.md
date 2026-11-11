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

Without `KRATE_NATIVE_GUI=1` the three GUI tests skip; the pure-Go unit tests
always run.

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
| Crash only on Intel | Struct-by-value argument passing for NSRect (32 bytes, passed in memory on amd64). |
