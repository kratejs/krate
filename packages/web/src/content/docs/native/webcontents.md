---
title: Web contents: print, find, zoom, downloads
order: 12
description: Print and PDF, find in page, zoom, capturing a view, downloads and cookies for the views of a Krate Native app.
---

# Web contents

> **Experimental.** Part of [Krate Native](/docs/native/). Linux (WebKitGTK) is the
> platform where this is tested. The macOS (WKWebView) and Windows (WebView2)
> implementations are written against the platform APIs and have GUI tests that
> run in CI, but nobody has run them on those systems yet.

These controls act on one **view**: a window's primary view or any view added
with [`addView`](/docs/native/views/). A view is named by its id (the window id
is the id of its primary view); leave it out and the call acts on the view it
came from. Three SDK areas hold them:

| Area | Go package | UI module | Permission |
|------|------------|-----------|------------|
| Page: print, find, zoom, capture | `sdk/page` | `@krate/app/page` | `page` |
| Downloads | `sdk/downloads` | `@krate/app/downloads` | `downloads` |
| Cookies and site data | `sdk/cookies` | `@krate/app/cookies` | `cookies` |

None of the three permissions is on by default. `krate native build`
[derives](/docs/native/release/#derived-permissions) them from the imports, like
every other permission. The ids are listed in [Native SDK: data and
system](/docs/native/sdk/#ids).

## Print and PDF

```ts
import { print, printToPdf } from '@krate/app/page';
import { writeFile } from '@krate/app/fs';

await print();                                    // the system print dialog
const pdf = await printToPdf({ pageSize: 'A4', landscape: true, printBackground: true,
                               margins: { top: 0.5, bottom: 0.5, left: 0.4, right: 0.4 } });
await writeFile('/tmp/page.pdf', pdf);            // needs the fs permission
```

`printToPdf` resolves to the PDF bytes (they start with `%PDF`). Sizes are in
inches. `pageSize` is `"A3"`, `"A4"` (default), `"A5"`, `"Letter"`, `"Legal"`
or `"Tabloid"`, or `{ width, height }`; the margins default to 0.4 inch.
`landscape` turns the paper. Go host code has `page.PrintToPDF(view, opts)` and
`page.Print(view)`.

`print` returns when the dialog has been requested, not when the user is done.

| | Linux | macOS | Windows |
|---|---|---|---|
| Dialog | GTK print dialog (`webkit_print_operation_run_dialog`) | print panel as a sheet on the window (`printOperationWithPrintInfo:`, macOS 11) | `ShowPrintUI` with the system dialog (`ICoreWebView2_16`); older runtimes call `window.print()` |
| PDF | `webkit_print_operation_print` to a file through GTK's "Print to File" printer | an `NSPrintSaveJob` to a temporary file (macOS 11) | `PrintToPdf` (`ICoreWebView2_7`) |
| Backgrounds | the view's `print-backgrounds` setting for the job | a private WebKit preference, when present | `ShouldPrintBackgrounds` |

On Linux the PDF needs GTK's file print backend (`libprintbackend-file`, part
of `libgtk-3-0` on Debian and Ubuntu); without it the call fails with a message
that says so. The tests check the PDF page size on Linux only.

## Find in page

```ts
import { find, onFind, stopFind } from '@krate/app/page';

const stop = onFind((e) => console.log(`${e.active} of ${e.matches}`));
await find('needle');                       // highlights the first match
await find('needle', { continue: true });   // next; add backward: true for previous
await find('Needle', { matchCase: true, noWrap: true });
await stopFind();
```

`onFind` delivers `{ view, text, matches, active, final }`. `matches` is the
number of matches (0 when there are none) and `active` the 1-based index of the
highlighted one. The engines differ in what they can report:

- **Linux** counts the matches itself (`WebKitFindController`). WebKitGTK does
  not say which match is highlighted, so Krate follows the index as you step.
- **macOS** highlights with `findString:withConfiguration:` (macOS 11), which
  only answers "found or not". The count comes from the visible text of the
  page and can differ from what is highlighted (hidden text, text split across
  elements).
- **Windows** has no find interface in the WebView2.h Krate builds against, so it runs
  `window.find` in the page and counts in the visible text, with the same caveat.

## Zoom

```ts
import { zoom, setZoom } from '@krate/app/page';
await setZoom(1.25);        // 0.25 to 5, per view
const z = await zoom();
```

Zoom is per view. Ctrl plus, Ctrl minus and Ctrl 0 (Cmd on macOS) keep working
and step through 25, 33, 50, 67, 75, 80, 90, 100, 110, 125, 150, 175, 200, 250,
300, 400 and 500 percent: on Linux Krate handles the keys on the web view, on
macOS the View menu has Zoom In, Zoom Out and Actual Size, and on Windows
WebView2's own accelerator keys stay enabled. A released window gets its zoom
back when it is shown again.

## Capture

```ts
import { capture } from '@krate/app/page';
const png = await capture();      // PNG bytes of the visible area of the view
```

The image has the pixel size of the view (on a high-DPI display, scaled by the
display factor). It shows what the view shows now: capture after the page has
painted.

## Downloads

Downloads started by the page (a link with the `download` attribute, a blob
URL, a navigation to a response with `Content-Disposition: attachment`) are
handled by the SDK once the `downloads` area is linked into the app. By
default each one asks where to save with the native save dialog, starting in
the user's Downloads folder with the suggested file name.

```ts
import * as downloads from '@krate/app/downloads';

await downloads.configure({ dir: '/home/me/Downloads/myapp' });  // save silently, free names
const stop = downloads.onEvent((e) => console.log(e.id, e.state, e.received, '/', e.total));
downloads.onDone((e) => console.log('saved to', e.path));
await downloads.cancel(id);
console.log(await downloads.list());
```

Events carry `{ id, view, url, path, state, received, total, error }`, where
`state` is `started` (the destination is chosen and `path` is set), `progress`,
`completed`, `cancelled` or `failed`. `configure({ dir, ask: true })` shows the
save dialog starting in `dir`; `configure({})` goes back to asking. Silent saves
create the folder and never overwrite a file: a taken name becomes `name (1).ext`.
A download you decline (cancel the dialog) ends as `cancelled`.

Without the `downloads` area (an app that never imports it), downloads follow
the engine's default.

| | Linux | macOS | Windows |
|---|---|---|---|
| Engine | `download-started` and the `WebKitDownload` signals | `WKDownload` (macOS 11.3) | `DownloadStarting` (`ICoreWebView2_4`) |
| Pages of the app origin | fetched by the shell (see below) | fetched by the shell | the engine downloads them (untested) |

**The app origin.** WebKit cannot download a page of a custom URL scheme (the
scheme handler lives in the UI process, the download engine in another), so on
Linux and macOS a download of a URL on `krate://app` is fetched by the shell
from your handler directly, through the same code path as any request from that
view, and reports the same events. Two consequences: the file URL is requested
once more than the page made it, and the file name comes from the link's
`download` attribute (looked up in the DOM), then `Content-Disposition`, then
the last path segment. A link that is not in the DOM when the click happens
loses its `download` name. Blob URLs and `https:` downloads use the engine.

## Cookies and site data

```ts
import * as cookies from '@krate/app/cookies';

await cookies.set({ name: 'sid', value: 'abc', httpOnly: true, sameSite: 'lax',
                    expires: Date.now() + 86400_000 }, 'https://api.example.com');
console.log(await cookies.list('https://api.example.com'));
console.log(await cookies.get('sid', 'https://api.example.com'));
await cookies.delete('sid', 'https://api.example.com');
await cookies.clearSiteData({ cache: true, storage: true });   // no options: all of it
```

Cookies are addressed by URL; the default is the app origin. **WebKit keeps no
cookies for a custom scheme** (`document.cookie` is inert on `krate://app`, and
the cookie jar refuses a single-label host), so on Linux and macOS the cookie
calls without a URL reject with code `unsupported` and a message that says
why. Pass the URL of an http(s) site, for example the API your pages fetch.
On Windows the app origin is `https://app.localhost` and has cookies like any
other site. `clearSiteData` works everywhere:

| `clearSiteData` | Linux (`webkit_website_data_manager_clear`) | macOS (`removeDataOfTypes:`) | Windows (`ClearBrowsingData`) |
|---|---|---|---|
| `cookies` | cookies | cookies | cookies |
| `cache` | memory and disk cache | memory, disk, offline and fetch caches | disk cache |
| `storage` | local and session storage, IndexedDB, service workers, DOM cache | local and session storage, IndexedDB, WebSQL, service workers | DOM storage and service workers |

Cookies persist across runs (Linux keeps them in `cookies.sqlite` in the app's
data directory). An older WebView2 runtime without `ICoreWebView2Profile2` can
only clear cookies and answers `unsupported` for the rest.

## Go

```go
import (
    "github.com/kratejs/krate/packages/native/sdk/cookies"
    "github.com/kratejs/krate/packages/native/sdk/downloads"
    "github.com/kratejs/krate/packages/native/sdk/page"
)

pdf, err := page.PrintToPDF("main", page.PDFOptions{PageSize: "Letter"})
png, err := page.Capture("main")
err = page.SetZoom("main", 1.5)
err = downloads.Configure(downloads.Options{Dir: dir})
cancel, err := downloads.OnEvent(func(e downloads.Event) { /* ... */ })
c, ok, err := cookies.Get("https://api.example.com", "sid")
```

Go host code is trusted and never checked against the permissions. Every call
answers `krate.ErrUnsupported` (code `unsupported` in the page) when the
platform lacks the capability, with the reason.

## Testing

Linux runs real tests for each control: a PDF that starts with `%PDF` and has
the asked page size, a PNG of the view's size, find counts for case rules and
stepping, a zoom round trip and the Ctrl keys, downloads that complete to disk
(an attachment, a blob, a `download` link), are declined or cancelled, and a
cookie round trip. `TestNativeE2EPageGUI` runs the same flow from a page
(`examples/native-page`) through the shell. The macOS and Windows packages
carry the same GUI tests (`gui_page_test.go`), which CI runs on `macos-latest`
and `windows-latest`. The print dialog is opened only on request
(`KRATE_NATIVE_PRINT_DIALOG=1`) because a test cannot close a system dialog.
