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
| Lifecycle | `sdk/app` | `@krate/app/app` | `app` |

Go packages live under `github.com/kratejs/krate/packages/native/sdk/`.

## Permissions

The page can only call what `app.json` grants, through the same `permissions`
list as the data half (see [Native SDK: data and system](/docs/native/sdk/#permissions)
for the full table):

```json
{ "permissions": ["window", "dialog", "clipboard", "menu", "notify", "tray"] }
```

With no list, `window`, `app`, `menu`, `dialog`, `notify` and `clipboard`
(write) are granted. `clipboard:read`, `tray` and `shortcut` must be listed:
a page that can read the clipboard sees what the user copied in other
applications, and tray icons and global shortcuts outlive the window. A call
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

Windows are named by id. The first is `"main"`; `open()` returns a handle with
the id of the new one. A page does not know its own id unless it is told, so
pass it through the route (`/settings?w=<id>`) if a window must control itself.

## Host function ids

The table of every id with its permission, arguments and result is in
[Native SDK: data and system](/docs/native/sdk/#ids); `sdk/ids.go` is its
source. By area:

| Area | Functions | Streams |
|------|-----------|---------|
| window | `krate/window.open`, `List`, `State`, `Close`, `SetTitle`, `SetSize`, `SetPosition`, `SetAlwaysOnTop`, `SetFullscreen`, `SetFrameless`, `Minimize`, `Maximize`, `Restore`, `Focus`, `Show`, `Hide`, `SetPreventClose`, `Navigate` | `krate/window.events` |
| dialog | `krate/dialog.openFile`, `OpenFolder`, `SaveFile`, `Message` | |
| clipboard | `krate/clipboard.readText`, `ReadImage`, `ReadFiles` (need `clipboard:read`), `WriteText`, `WriteImage`, `WriteFiles`, `Clear` | |
| menu | `krate/menu.setApp`, `Popup` | `krate/menu.events` |
| tray | `krate/tray.set`, `Remove` | `krate/tray.events` |
| shortcut | `krate/shortcut.register`, `Unregister` | `krate/shortcut.events` |
| notify | `krate/notify.send`, `Close` | `krate/notify.events` |
| app | `krate/app.quit`, `Relaunch`, `Supports` | `krate/app.events` |

`SingleInstance` and `LaunchArgs` exist only for Go host code.

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
WebKitGTK through purego, a private D-Bus session bus and fake desktop
services). "Compile-only" means the code builds for the target and is written
against the platform API but has not run in this release.

| Capability | Linux | macOS | Windows |
|------------|-------|-------|---------|
| Open, save, folder and message dialogs | Verified (GtkFileChooserNative, GtkMessageDialog; created with the right options and answered programmatically, since a native dialog cannot be clicked headlessly) | Compile-only (NSOpenPanel, NSSavePanel, NSAlert) | Compile-only (GetOpenFileNameW, SHBrowseForFolderW, MessageBoxW; custom button labels are not supported) |
| Clipboard text, PNG image, file list | Verified | Text, PNG and files compile-only | Compile-only |
| Menu bar and context menus | Verified (GtkMenuBar in each window; popups at the pointer) | Compile-only (global menu bar) | Compile-only (accelerators are shown but not active) |
| Multiple windows on routes, title, size | Verified | Existing platform layer | Existing platform layer |
| Windows share one web process; hide with release, `keepAlive`, memory-pressure event | Verified (related web views; the WebKitWebProcess count is asserted) | Compile-only (private single-process switch behind `respondsToSelector`) | Compile-only (one shared environment; `put_IsVisible` and `MemoryUsageTargetLevel` on hide) |
| Frameless, position, always on top | Verified (X11; Wayland ignores position and always-on-top) | Compile-only | Compile-only |
| Fullscreen, maximize, minimize | Verified with a window manager (openbox on Xvfb) | Compile-only | Compile-only |
| Close requested (prevent close) | Verified | Compile-only | Compile-only |
| Notifications | Verified against a fake `org.freedesktop.Notifications`; actions via `ActionInvoked` | Untested (implemented, not yet run on a Mac): `UNUserNotificationCenter`, needs an app bundle; `krate native dev` runs from a generated dev bundle | `ErrUnsupported`: toasts need an AppUserModelID from a Start menu shortcut |
| Tray | Verified against a fake StatusNotifierWatcher (StatusNotifierItem plus `com.canonical.dbusmenu`); needs a StatusNotifier host | Untested (implemented, not yet run on a Mac): `NSStatusItem` with an `NSMenu` | `ErrUnsupported` |
| Global shortcuts | Verified on X11 (`XGrabKey`); Wayland uses the GlobalShortcuts portal (verified against a fake portal) or returns `ErrUnsupported` | Untested (implemented, not yet run on a Mac): Carbon `RegisterEventHotKey`, no Accessibility permission | `ErrUnsupported` |
| Single instance, deep links | Verified (unix socket in the runtime dir) | The same code; URL scheme registration is an Info.plist packaging step | The same code; scheme registration is a registry packaging step |

### Linux details

- **Wayland.** A client cannot place windows or keep them above others;
  `SetPosition` and `SetAlwaysOnTop` are accepted and ignored by compositors.
  Global shortcuts need the `org.freedesktop.portal.GlobalShortcuts` portal
  (the user may be asked to confirm); without it `shortcut.Register` returns
  `ErrUnsupported`.
- **Tray.** Without a StatusNotifier host (stock GNOME without the AppIndicator
  extension) `tray.New` returns `ErrUnsupported`.
- **Notifications.** If no notification service owns
  `org.freedesktop.Notifications` the call returns `ErrUnsupported`.

### macOS details

The tray, global shortcuts and notifications are implemented against AppKit,
Carbon and `UserNotifications` through purego. They compile for macOS and the
logic around them (key table, menu model, error mapping) is unit tested, but
they have not been run on a Mac yet; `platform/darwin/MANUAL_TEST.md` lists
what to check by hand.

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
