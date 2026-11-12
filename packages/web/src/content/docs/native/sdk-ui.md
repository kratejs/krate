---
title: SDK - Windows, Dialogs and the Desktop
order: 10
description: The Native SDK for windows, dialogs, clipboard, menus, tray, shortcuts and notifications, with the permission each call needs and its Go and TypeScript API.
---

# SDK: Windows, Dialogs and the Desktop

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

The UI half of the Native SDK covers everything that touches the desktop
rather than data: windows, the menu bar, the tray, global shortcuts, file and
message dialogs, the clipboard, notifications and the app lifecycle. Each area
is a Go package for [Go host code](/docs/native/go-host/) and a TypeScript
module for the UI.

| Area | Go package | UI module | Permission |
|------|------------|-----------|------------|
| Windows | `sdk/window` | `@krate/app/window` | `window` |
| Dialogs | `sdk/dialog` | `@krate/app/dialog` | `dialog` |
| Clipboard | `sdk/clipboard` | `@krate/app/clipboard` | `clipboard` (write), `clipboard:read` (read) |
| Menus | `sdk/menu` | `@krate/app/menu` | `menu` |
| Tray | `sdk/tray` | `@krate/app/tray` | `tray` |
| Global shortcuts | `sdk/shortcut` | `@krate/app/shortcut` | `shortcut` |
| Notifications | `sdk/notify` | `@krate/app/notify` | `notify` |
| Camera, microphone, location | `sdk/media` | `@krate/app/media` | `media`, `media:manage`, `location` |
| Lifecycle | `sdk/app` | `@krate/app/app` | `app` |
| Badge, displays, power, login, theme, crash | `sdk/badge`, `sdk/screen`, `sdk/power`, `sdk/login`, `sdk/theme`, `sdk/crash` | `@krate/app/badge` and so on | `badge`, `screen`, `power`, `power:inhibit`, `login`, `theme`, `crash` ([OS integration](/docs/native/system/)) |
| Web contents: print, find, zoom, capture | `sdk/page` | `@krate/app/page` | `page` |
| Downloads | `sdk/downloads` | `@krate/app/downloads` | `downloads` |
| Cookies and site data | `sdk/cookies` | `@krate/app/cookies` | `cookies` |
| Remote content | `sdk/remote` | `@krate/app/remote` | `remote`, `remote:intercept`, `remote:network` ([Remote content](/docs/native/remote/)) |

Go packages live under `github.com/kratejs/krate/packages/native/sdk/`.

## Permissions

The page can only call what `app.json` grants, through the same `permissions`
list as the data half (see [Native SDK: data and system](/docs/native/sdk/#permissions)
for the full table):

```json
{ "permissions": ["window", "dialog", "clipboard", "menu", "notify", "tray"] }
```

With no list, `window`, `app`, `menu`, `dialog`, `notify`, `clipboard`
(write), `badge`, `screen`, `power` and `theme` are granted. `clipboard:read`, `tray`, `shortcut`, `power:inhibit`, `login` and
`crash` must be listed: a page that can read the clipboard sees what the user copied in other
applications, and tray icons and global shortcuts outlive the window. So must
`media`, `media:manage` and `location`, which gate the camera, microphone,
screen capture and location (a derived build adds `media` and `location` for
code that uses them; see [Camera, microphone and
location](/docs/native/media/)). A call
without its permission rejects with code `permission_denied` and
`data.permission`, like every other SDK id. Go host code is trusted and is
never gated.

A window can be given less than the app has: `native.window.permissions` for
the main window, and the `permissions` option of `window.open` for windows opened
at run time (a subset of the opener's, inherited when omitted). See
[Per-window scopes](/docs/native/release/#per-window-scopes).

## From Go

```go
import (
    "github.com/kratejs/krate/packages/native/krate"
    "github.com/kratejs/krate/packages/native/sdk/dialog"
    "github.com/kratejs/krate/packages/native/sdk/menu"
    "github.com/kratejs/krate/packages/native/sdk/notify"
    "github.com/kratejs/krate/packages/native/sdk/window"
)

func Export(ctx krate.Ctx) ([]string, error) {
    paths, err := dialog.OpenFile(ctx, dialog.OpenOptions{
        Title:    "Choose images",
        Multiple: true,
        Filters:  []dialog.Filter{{Name: "Images", Patterns: []string{"*.png", "*.jpg"}}},
    })
    if err != nil || len(paths) == 0 { // an empty result means the user cancelled
        return nil, err
    }
    notify.Send(notify.Notification{Title: "Export", Body: "Started"})
    return paths, nil
}

func Setup() error {
    menu.SetApp(menu.New(menu.Sub("&File",
        menu.Action("file.new", "New", "CmdOrCtrl+N"),
        menu.Separator(),
        menu.Action("file.quit", "Quit", "CmdOrCtrl+Q"))))
    menu.OnItem("file.new", func(e menu.Event) { /* ... */ })
    settings, err := window.Open(window.Options{Title: "Settings", Path: "/settings", Width: 520, Height: 380})
    _ = settings
    return err
}
```

`window.Open` opens another window on a route of the same app. `settings.SetAlwaysOnTop`,
`SetFullscreen`, `SetFrameless`, `SetPosition`, `Minimize`, `Maximize`, `Restore`,
`Focus`, `Show`, `Hide` and `Close` do what they say; `Hide` with
`window.HideOptions{Release: true}` (`win.hide({ release: true })` in TypeScript)
also frees the window's webview, and `Show` brings it back on the same route
and scroll position ([background mode](/docs/native/memory/#windows-and-background-mode)). `SetPreventClose(true)`
turns a close request into an event (`OnCloseRequested`), so the app can ask
"save changes?" and then call `Close`. `window.SaveState` and `window.RestoreState`
keep a window's size, position and maximized flag between runs.

`shortcut.Register("Ctrl+Shift+K", fn)` returns a function that releases the
shortcut. `app.Quit()` and `app.Relaunch()` end or restart the app,
`app.Supports("tray")` asks whether a capability works right now, and
`app.SingleInstance(id, os.Args[1:])` makes a second launch hand its arguments
to the first (`app.OnSecondInstance`, `app.OnOpenURL`).

Functions that the platform cannot serve return `krate.ErrUnsupported`
(`errors.Is` works, also for errors that came from another process).

## From the UI

```ts
import { openFile, confirm } from "@krate/app/dialog";
import { setApp, onMenu } from "@krate/app/menu";
import { open } from "@krate/app/window";
import * as tray from "@krate/app/tray";

await setApp([{ label: "&File", submenu: [{ id: "new", label: "New", accelerator: "CmdOrCtrl+N" }] }]);
const stop = onMenu("new", () => open({ path: "/editor" }));

const files = await openFile({ filters: [{ name: "Images", patterns: ["*.png"] }] }); // [] when cancelled
if (files.length && (await confirm("Import", `Import ${files.length} files?`))) { /* ... */ }
```

Events reach the UI as host streams: `krate/menu.events`, `krate/window.events`,
`krate/tray.events`, `krate/shortcut.events`, `krate/notify.events` and
`krate/app.events`. The helpers `onMenu`, `onClick`, `onEvent`,
`onCloseRequested`, `onAction` and `onSecondInstance` wrap them; each returns a
function that stops the subscription and reopens a dropped stream on its own.
`onMemoryPressure(fn)` from `@krate/app/window` is a DOM-event helper instead:
the shell dispatches `krate:memory-pressure` on a page whose window was hidden
or minimized so it can drop caches (see [Windows and background
mode](/docs/native/memory/#windows-and-background-mode)).

A window can hold several webviews as split panes, each with its own route and
scope; see [Views and split panes](/docs/native/views/). Opening files, file
associations and the real paths of dropped files are on [Files and drag and
drop](/docs/native/files/).
Printing, find in page, zoom, capturing a view as an image, downloads and
cookies are on their own page, [Web contents](/docs/native/webcontents/).

## Rich clipboard formats

`writeText`, `writeImage` and `writeFiles` each replace the clipboard with one
kind of data. `write` puts several representations of one value on it together,
as a browser does on copy, so a rich-text editor pastes the HTML and a plain
text field pastes the text (Electron's `clipboard.write({ text, html })`):

```ts
import * as clipboard from "@krate/app/clipboard";

await clipboard.write({ text: "Hello world", html: "<p>Hello <b>world</b></p>", rtf: "{\\rtf1 Hello}" });
const html = await clipboard.readHtml(); // "<p>Hello <b>world</b></p>", "" when there is none
const rtf = await clipboard.readRtf();
const formats = await clipboard.formats(); // ["text/plain", "text/html", "text/rtf"]
const bytes = await clipboard.readFormat("text/html"); // Uint8Array | null
await clipboard.writeFormat("application/x-acme-shape", new Uint8Array([1, 2, 3]));
```

```go
clipboard.Write(clipboard.Data{Text: "Hello world", HTML: "<p>Hello <b>world</b></p>"})
html, _ := clipboard.ReadHTML()
names, _ := clipboard.Formats()
b, _ := clipboard.ReadFormat("text/html")
```

Format names are MIME types: `text/plain`, `text/html`, `text/rtf`, `image/png`.
Each platform maps them to its own: on macOS to the UTIs `public.utf8-plain-text`,
`public.html`, `public.rtf` and `public.png`; on Windows to `CF_UNICODETEXT` and
the registered formats `HTML Format`, `Rich Text Format` and `PNG`; on Linux
they are the X selection targets themselves (`UTF8_STRING` and the other text
targets answer `text/plain`). A name the platform does not know is used as it
is, so `writeFormat` and `readFormat` also reach application-specific formats
(a Windows registered format name, a macOS UTI or a Linux target).
`writeFormat` replaces everything on the clipboard with that one format.
`HTML` and `RTF` are returned as text; HTML read from other applications in
UTF-16 is converted.

On Windows the HTML format is wrapped in the `CF_HTML` envelope (a header of byte
offsets and `StartFragment`/`EndFragment` markers, UTF-8). `write` and
`writeFormat("text/html", ...)` build it and `readHtml` and
`readFormat("text/html")` take the fragment out of it; reading the format by its
registered name, `readFormat("HTML Format")`, returns the raw envelope. The
encoder and decoder are plain Go (`platform/caps`) and are unit tested on every
OS. The clipboard stores the HTML as given: it does not add a charset
declaration. `formats()` and the `clipboard:html`, `clipboard:rtf` and
`clipboard:formats` entries of `app.supports` say whether the platform has this
(`ErrUnsupported` otherwise). `write`, `writeFormat` need `clipboard`;
`readHtml`, `readRtf`, `formats` and `readFormat` need `clipboard:read`.

## The webview context menu

A right click in a webview shows the engine's own menu (Back, Reload, Copy, Cut,
Paste, spelling suggestions, Inspect Element in dev builds). The app can change
that for the whole app, for one window or for one view.

In `krate.config.ts`:

```ts
native: { window: { contextMenu: "none" } } // or "default" (the default) or "custom"
```

or at run time (`menu` permission), from the UI or from Go:

```ts
import * as menu from "@krate/app/menu";

await menu.setContextMenu({ mode: "none" });                       // no menu at all
await menu.setContextMenu({ mode: "extend", items: [               // the engine's menu plus these
  { id: "share", label: "Share link" },
]}, { window: "*" });                                              // "*": every window without its own setting
await menu.setContextMenu({ mode: "custom" }, { view: "sidebar" }); // this view only

// custom: the click arrives as an event and the answer is a list of items,
// drawn by the native menu code (the same as menu.popup).
const stop = menu.onContextMenu((e) => [
  ...menu.defaultContextItems(e),   // Copy, Paste, Inspect Element, ... for what was clicked
  { kind: "separator" },
  { id: "share", label: "Share", disabled: !e.linkUrl },
]);
menu.onMenu("share", (e) => { /* ... */ });
```

```go
menu.SetContextMenu("", "", menu.ContextOptions{Mode: menu.ContextCustom})
menu.OnContextMenu(func(i menu.ContextInfo) []menu.Item {
    return append(menu.DefaultContextItems(i), menu.Separator(), menu.Action("share", "Share", ""))
})
```

| Mode | A right click |
| --- | --- |
| `default` | shows the engine's menu |
| `none` | shows nothing |
| `custom` | shows nothing and sends a `ContextMenuEvent`; `onContextMenu` pops up the items its handler returns (`onContext` just reports the event) |
| `extend` | shows the engine's menu with the given items added after a separator; their clicks arrive like any menu click (`onMenu`) |

The most specific setting wins: a view's, then its window's, then the app's. The
window is `""` (the main window), a window id, or `"*"` for the app-wide default;
`view` is a view id and, when given, makes the setting apply to that view only.
Extension items are fixed when set; to build a menu per click use `custom`.

The `ContextMenuEvent` holds `window` and `view`; `x` and `y` in the view (like a
DOM mouse event's `clientX`/`clientY`) and `windowX`, `windowY` in the window
(what `popup` takes); `kinds`, a list of `"page"` plus `"link"`, `"image"`,
`"media"`, `"editable"` and `"selection"`; `linkUrl`, `imageUrl`, `mediaUrl`;
`selectionText`; `misspelled` and `suggestions`; and `inspectable`, true in dev
builds.

`defaultContextItems(e)` builds a menu from the event with the built-in actions:
in an editable field undo, redo, cut, copy, paste and select all; Copy for a
selection; "Copy Link Address" and "Copy Image Address"; spelling suggestions and
"Add to Dictionary" above them when `misspelled` is set; and "Inspect Element" in
dev builds. Any item whose id starts with `krate:` (`menu.actions.copy`,
`menu.actions.replaceMisspelling(word)` and so on) is run by the shell on the view
that showed the menu and is not reported as a click. Labels are in English.

Spell checking is the `spellcheck` feature (`native.features`, found by scanning
the UI for editable fields): with it on the engine reports `misspelled` and
`suggestions`, in the engine's own menu and in the event; with it off the
`balanced` and `minimal` memory profiles switch it off and there are none.

In dev builds the engine's menu keeps "Inspect Element", and custom menus built
with `defaultContextItems` end with it too (`krate:inspectElement`). It opens the
inspector; WebKitGTK cannot select the clicked element for it. A release build
has no developer tools and the item is absent.

How each platform does it (Linux is verified by a GUI test; macOS and Windows are
written against the platform API and covered by GUI tests that run in CI):

- **Linux (WebKitGTK):** the `context-menu` signal; its hit test result gives the
  kind, link, image and media URLs, and the selected text is read with
  `getSelection()`. Suggestions come from the engine's spelling items.
  "Add to Dictionary" activates the engine's own "learn spelling" item.
- **macOS (WKWebView):** the webview is a `WKWebView` subclass that overrides
  `willOpenMenu:withEvent:` (the override point WebKit documents): `none` and
  `custom` empty the menu before it opens, `extend` adds items. WebKit gives no hit
  test, so the kind, URLs and selection come from `elementFromPoint` in the page.
  Suggestions come from WebKit's spelling items. The position of a popup is the
  pointer (as for every `popup`).
- **Windows (WebView2):** `ICoreWebView2_11::ContextMenuRequested`; the target
  gives kind, link, source and selection, `Handled` suppresses the menu, and
  `extend` adds items with `ICoreWebView2Environment9::CreateContextMenuItem`. The
  editing commands go through the DevTools protocol. "Add to Dictionary" and
  spelling suggestions from the engine's items are best-effort and unverified;
  a WebView2 runtime without `ICoreWebView2_11` keeps the default menu.

Windows are named by id. The first is `"main"`; `open()` returns a handle with
the id of the new one. A page does not know its own id unless it is told, so
pass it through the route (`/settings?w=<id>`) if a window must control itself.

## Host function ids

The table of every id with its permission, arguments and result is in
[Native SDK: data and system](/docs/native/sdk/#ids); `sdk/ids.go` is its
source. By area:

| Area | Functions | Streams |
|------|-----------|---------|
| window | `krate/window.open`, `List`, `State`, `Close`, `SetTitle`, `SetSize`, `SetPosition`, `SetAlwaysOnTop`, `SetFullscreen`, `SetFrameless`, `Minimize`, `Maximize`, `Restore`, `Focus`, `Show`, `Hide`, `SetPreventClose`, `Navigate`; views: `krate/window.addView`, `krate/window.removeView`, `krate/window.views`, `krate/window.setLayout`, `krate/window.layout`, `krate/window.focusView`, `krate/window.navigateView`, `krate/window.postMessage`; files: `krate/window.thisView` | `krate/window.events`, `krate/window.viewEvents`, `krate/window.fileDrops` |
| dialog | `krate/dialog.openFile`, `OpenFolder`, `SaveFile`, `Message` | |
| clipboard | `krate/clipboard.readText`, `ReadImage`, `ReadFiles`, `ReadHtml`, `ReadRtf`, `Formats`, `ReadFormat` (need `clipboard:read`), `WriteText`, `WriteImage`, `WriteFiles`, `Write`, `WriteFormat`, `Clear` | |
| menu | `krate/menu.setApp`, `Popup`, `SetContextMenu` | `krate/menu.events`, `krate/menu.contextEvents` |
| tray | `krate/tray.set`, `Remove` | `krate/tray.events` |
| shortcut | `krate/shortcut.register`, `Unregister` | `krate/shortcut.events` |
| notify | `krate/notify.send`, `Close` | `krate/notify.events` |
| media | `krate/media.policy`, `krate/media.setPolicy`, `krate/media.decisions`, `krate/media.decide`, `krate/media.reset`, `krate/media.respond`, `krate/media.osStatus`, `krate/media.osRequest`, `krate/media.sources`, `krate/media.chooseSource` | `krate/media.requests`, `krate/media.events` |
| app | `krate/app.quit`, `Relaunch`, `Supports`, `krate/app.openedFiles` | `krate/app.events`, `krate/app.openFileEvents` |
| page | `krate/page.print`, `PrintToPdf`, `Find`, `StopFind`, `Zoom`, `SetZoom`, `Capture` | `krate/page.findEvents` |
| downloads | `krate/downloads.configure`, `List`, `Cancel` | `krate/downloads.events` |
| cookies | `krate/cookies.list`, `Get`, `Set`, `Delete`, `ClearSiteData` | |
| remote | `krate/remote.open`, `AddView`, `Navigate`, `Post`, `Capabilities`, `SetRules`, `SetUserAgent`, `SetProxy`, `SetClientCertificate`, `ClearData`, `AnswerCertificate`, `AnswerAuth` | `krate/remote.messages`, `Events`, `Requests`, `Certificates`, `Auth` |

`SingleInstance` and `LaunchArgs` exist only for Go host code.

The [OS integration](/docs/native/system/) areas have these ids:

| Area | Ids |
|------|-----|
| badge (`badge`) | `krate/badge.set`, `krate/badge.get`, `krate/badge.setProgress`, `krate/badge.progress`, `krate/badge.setUrgent` |
| screen (`screen`) | `krate/screen.list`, `krate/screen.primary`, `krate/screen.ofWindow`, `krate/screen.centerWindow`, stream `krate/screen.events` |
| power (`power`; blockers need `power:inhibit`) | `krate/power.state`, `krate/power.idleTime`, `krate/power.setIdleThreshold`, `krate/power.preventSleep`, `krate/power.allowSleep`, stream `krate/power.events` |
| login (`login`) | `krate/login.get`, `krate/login.set` |
| theme (`theme`) | `krate/theme.get`, `krate/theme.set`, stream `krate/theme.events` |
| crash (`crash`) | `krate/crash.list`, `krate/crash.read`, `krate/crash.delete`, `krate/crash.setAutoReload`, stream `krate/crash.events` |

The [process and automation](/docs/native/processes/) areas have these ids:

| Area | Ids |
|------|-----|
| port (`port`; using an end needs `port:receive`) | `krate/port.channel`, `krate/port.transfer`, `krate/port.post`, `krate/port.close`, stream `krate/port.listen` |
| worker (`worker`) | `krate/worker.start`, `krate/worker.stop`, `krate/worker.list`, `krate/worker.specs`, `krate/worker.post`, `krate/worker.emit` (called by a worker), stream `krate/worker.events` |
| offscreen (`offscreen`) | `krate/offscreen.open`, `krate/offscreen.capture`, stream `krate/offscreen.frames` |
| automation (`automation`) | `krate/automation.endpoint`, `krate/automation.cdp`, stream `krate/automation.cdpEvents` |
| extension (`extension`) | `krate/extension.declared`, `krate/extension.list`, `krate/extension.load`, `krate/extension.remove` |

## How it works in dev

Go host code runs in a child process that has no windows. Every SDK call is a
serializable message: in the shell process it goes straight to the platform; in
the child the SDK forwards it over a private unix socket
(`KRATE_GOHOST_PARENT`, set by the shell) using the same call protocol as the
page. Events travel back as streams on that socket, so `menu.OnItem` works the
same in the child as it does in a release build where the Go code is linked
into the shell. If the shell restarts the child, the child reconnects its
subscriptions.

## Platform support

"Verified" means a test runs it against the real thing on Linux (GTK 3 and
WebKitGTK through purego, a private D-Bus session bus, and real desktop
services: dunst, stalonetray and the Ayatana indicator service, plus fake ones
for protocol edge cases). "Compile-only" means the code builds for the target
and is written against the platform API, and no test runs it. "CI" means the
CI native job runs a test of it on macos-latest or windows-latest; real key
presses, tray clicks and notification banners are still manual checks.

| Capability | Linux | macOS | Windows |
|------------|-------|-------|---------|
| Open, save, folder and message dialogs | Verified (GtkFileChooserNative, GtkMessageDialog; created with the right options and answered programmatically, since a native dialog cannot be clicked headlessly) | Compile-only (NSOpenPanel, NSSavePanel, NSAlert) | Compile-only (GetOpenFileNameW, SHBrowseForFolderW, MessageBoxW; custom button labels are not supported) |
| Clipboard text, PNG image, file list | Verified | Text, PNG and files compile-only | Compile-only |
| Clipboard HTML (with a text alternative), RTF, named formats | Verified (GTK selection targets) | Untested (`public.html`, `public.rtf`; GUI test runs in CI) | Untested (`HTML Format` with the `CF_HTML` header, `Rich Text Format`; GUI test runs in CI; the header codec is unit tested everywhere) |
| Webview context menu: none, custom, extend, built-in actions | Verified (right click through XTEST; hit test, positions, spelling items not exercised: no dictionary) | Untested (`willOpenMenu:withEvent:`; GUI test runs in CI) | Untested (`ContextMenuRequested`; GUI test runs in CI) |
| Menu bar and context menus | Verified (GtkMenuBar in each window; popups at the pointer) | Compile-only (global menu bar) | Compile-only (accelerators are shown but not active) |
| Multiple windows on routes, title, size | Verified | CI (two windows, title, size) | CI (two windows, title, size) |
| Windows share one web process; hide with release, `keepAlive`, memory-pressure event | Verified (related web views; the WebKitWebProcess count is asserted) | Compile-only (private single-process switch behind `respondsToSelector`) | Compile-only (one shared environment; `put_IsVisible` and `MemoryUsageTargetLevel` on hide) |
| Frameless, position, always on top | Verified (X11; Wayland ignores position and always-on-top, checked under weston) | Compile-only | Compile-only |
| Fullscreen, maximize, minimize | Verified with a window manager (openbox on Xvfb) | Compile-only | Compile-only |
| Close requested (prevent close) | Verified | Compile-only | Compile-only |
| Notifications | Verified against a real dunst daemon (text, icon, urgency, replace by id, close from either side, actions through `dunstctl`) and a fake `org.freedesktop.Notifications` for edge cases | CI (`UNUserNotificationCenter`: the unbundled refusal and the generated bundle are tested; the banner is a manual check). Needs an app bundle; `krate native dev` runs from a generated dev bundle | CI (WinRT toasts called for real, a toast shown; the banner is a manual check). The app registers its AppUserModelID under `HKCU`, packaged apps use their package identity) |
| Tray | Verified against a real StatusNotifier host (StatusNotifierItem plus `com.canonical.dbusmenu`) and a real XEmbed tray (stalonetray); needs one of the two | CI (`NSStatusItem` with an `NSMenu` is created and updated; a real click is a manual check) | CI (`Shell_NotifyIconW` version 4: add, modify and remove an icon, skipped on a runner without a taskbar; the icon returns when explorer restarts) |
| Global shortcuts | Verified on X11 (`XGrabKey`); Wayland uses the GlobalShortcuts portal (verified against a fake portal only: no backend runs headless) or returns `ErrUnsupported` | CI (Carbon `RegisterEventHotKey` registered and released; a real key press is a manual check), no Accessibility permission | CI (`RegisterHotKey` registered and released; a real key press is a manual check) |
| Single instance, deep links | Verified (unix socket in the runtime dir) | CI (single instance); URL scheme registration is an Info.plist packaging step | CI (single instance); scheme registration is a registry packaging step |

### Linux details

- **Wayland.** A client cannot place windows or keep them above others;
  `SetPosition` and `SetAlwaysOnTop` are accepted and ignored by compositors.
  `window.supports` reports `window:position` and `window:alwaysOnTop` as
  unsupported. Global shortcuts need the `org.freedesktop.portal.GlobalShortcuts`
  portal (the user may be asked to confirm); without it `shortcut.Register`
  returns `ErrUnsupported`. The XEmbed tray fallback does not apply on Wayland.
- **Tray.** The icon uses the StatusNotifierItem protocol when a StatusNotifier
  host is running. Without one, if an XEmbed tray manager owns
  `_NET_SYSTEM_TRAY_S<screen>` (a legacy panel, stalonetray), the icon goes
  there through `GtkStatusIcon`; that fallback works on X11 only. With neither
  (stock GNOME without the AppIndicator extension) `tray.New` returns
  `ErrUnsupported`; the error says to install and enable the AppIndicator
  extension (`gnome-shell-extension-appindicator`).
- **Notifications.** If no notification service owns
  `org.freedesktop.Notifications` the call returns `ErrUnsupported`.

### Windows details

None of this has run on a real Windows desktop yet; the pure parts (key table,
toast XML, struct layouts) are tested on Linux and a CI job runs the tray,
hotkey and toast calls on `windows-latest` (see `MANUAL_TEST.md` in
`platform/windows` for what a person still has to look at).

- **Tray.** The icon is a PNG (scaled to the small-icon size), the tooltip is
  cut to the 127 characters the shell holds, and a title stands in for an empty
  tooltip. A left click reports `click`, a double click `double-click`, and a
  right click `right-click` and then shows the menu you set; a chosen item
  reports `menu`. When explorer restarts the icon is added again: the app
  listens for the registered `TaskbarCreated` message (and allows it through
  the message filter when it runs elevated). If there is no taskbar, `tray.Set` returns an error.
- **Global shortcuts.** `RegisterHotKey` on a hidden window, with
  `MOD_NOREPEAT` so holding the keys fires once. A combination another app or
  Windows holds (many `Win+` combinations are reserved) fails with "already
  registered by another app". `CmdOrCtrl` is `Ctrl`; `Super` is the Windows
  key. Keys: letters, digits, `F1` to `F24`, `Space`, `Enter`, `Escape`,
  `Tab`, `Backspace`, `Delete`, `Insert`, `Home`, `End`, `PageUp`, `PageDown`,
  the arrows, `Plus`, `Minus`, `Comma`, `Period`, `Numpad0` to `Numpad9`,
  `PrintScreen` and the media keys.
- **Notifications.** An unpackaged app gets an AppUserModelID derived from its
  app id (`com.acme.app` stays as it is) and registers it, with the product
  name as display name, under `HKCU\Software\Classes\AppUserModelId`. A
  packaged (MSIX) app takes the identity of its package and registers nothing.
  Buttons and clicks come back as notification events. **A click works after
  the app has quit**: a toast uses protocol activation with a URL scheme of its
  own (`krate-toast-` and a hash of the app id), so Windows runs the app's
  executable with the click as its argument. The app registers the scheme
  under `HKCU\Software\Classes` the first time it shows a toast, the NSIS
  installer writes it under `HKLM` and the MSIX manifest declares it. A new
  process started that way hands the click to the instance that is already
  running and exits, and the running app raises its window; when none is
  running the app starts, and the click is delivered to the page as the first
  notification event as soon as it subscribes (`notify.onEvent` in TypeScript, `notify.OnEvent` in Go). Clicks that
  nobody is subscribed to while the app runs are dropped, as before. One limit
  to know: Windows has no per-toast timeout (a toast is on screen for the short
  or long default, and a timeout above 10 seconds or "never" asks for the long
  one). If notifications are turned off for the app (Focus assist, Settings,
  group policy) `notify.Send` returns an error that says so. Urgency is ignored except that a critical toast with
  buttons stays on screen until dismissed.

### macOS details

The tray, global shortcuts and notifications are implemented against AppKit,
Carbon and `UserNotifications` through purego. The logic around them (key
table, menu model, error mapping) is unit tested, and the CI native job creates
a real status item and registers real hot keys on macOS. A real click or key
press is not tested; `platform/darwin/MANUAL_TEST.md` lists what to check by
hand.

- **Tray.** One `NSStatusItem`. A PNG icon that is a single gray tone (black or
  white artwork on transparent) is installed as a template image, so it follows
  the light or dark menu bar; other icons keep their colors. Icons taller than
  18 points are scaled down to 18 points, keeping the aspect ratio. A title
  without an icon works; an icon with neither is an error. With a menu, any
  click opens the menu and is reported as `click`; `right-click` and
  `double-click` are only reported while the tray has no menu. Check items
  toggle themselves when clicked; a radio item switches its radio siblings off.
  macOS menus have no radio style, so a selected radio item shows a checkmark.
  Accelerators are displayed as key equivalents.
- **Global shortcuts.** Carbon `RegisterEventHotKey` works without the
  Accessibility permission and while the app is in the background. `CmdOrCtrl`
  and `Cmd` mean Command, `Ctrl` is Control. Keys name physical positions of an
  ANSI keyboard, so on another layout a letter is the key at that position.
  `plus` is the `=` key with Shift. A combination already registered, by this
  app or by the system, fails with an error naming the combination. macOS 15
  stops delivering hot keys whose only modifiers are Option and/or Shift; use
  Command or Control.
- **Notifications.** `UNUserNotificationCenter` only delivers to a process that
  runs from an `.app` bundle with a bundle identifier. In a packaged app that
  is automatic. `krate native dev` starts the shell from a generated bundle at
  `.krate/dev/<name>.app` (bundle id `<appId>.dev`), so notifications and the
  tray name behave as in the release app. A process outside a bundle, such as a
  bare `krate-shell` or `go run`, gets an `ErrUnsupported` error that says so and
  names the fix, and `app.Supports("notify")` is false. The first notification
  asks for permission; if the user refuses, the call fails with a
  `permission_denied` error that points at System Settings > Notifications.
  Actions become notification buttons, a click on the body reports an empty
  action, and a dismissal reports `closed`. `Close` withdraws a notification by
  its id. `Icon` and `Timeout` are ignored (macOS shows the app icon and decides
  how long a banner stays); urgency low delivers quietly and critical asks for
  time-sensitive delivery, which macOS may downgrade without a special
  entitlement. `Notify` must not be called from the main thread.

### Deep links on Linux

`app.DesktopEntry` generates the `.desktop` file that registers the app as the
handler of its URL schemes (`MimeType=x-scheme-handler/<scheme>`). Write it at
packaging time to `~/.local/share/applications/<app id>.desktop` and run
`update-desktop-database`. The URL arrives as the last command line argument;
with `SingleInstance` a running instance receives it through `OnOpenURL`.

```go
entry := app.DesktopEntry(app.DesktopOptions{
    Name: "Acme", Exec: "/opt/acme/acme", Icon: "acme", Schemes: []string{"acme"},
})
```

## Testing

`@krate/app/testing` and `createFakeHost` serve these functions by id, so UI
code that calls `krate/dialog.openFile` or listens on `krate/menu.events` can
be tested without a window. In Go, the `sdk` package test helpers run the
operations against an in-memory platform.
