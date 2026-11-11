---
title: SDK - Windows, Dialogs and the Desktop
order: 6
description: The UI half of the Krate Native SDK - windows, menus, tray, global shortcuts, dialogs, clipboard, notifications and app lifecycle from Go host code and from the UI, with the platform support matrix.
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

The page can only call what `app.json` grants:

```json
{ "permissions": ["window", "dialog", "clipboard", "menu", "notify"] }
```

A call without the permission fails with code `not_found`, as if the function
did not exist. `clipboard:read` is separate from `clipboard` because a page
that can read the clipboard sees what the user copied in other applications.
Go host code is trusted and is never gated.

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
`Focus`, `Show`, `Hide` and `Close` do what they say. `SetPreventClose(true)`
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

Windows are named by id. The first is `"main"`; `open()` returns a handle with
the id of the new one. A page does not know its own id unless it is told, so
pass it through the route (`/settings?w=<id>`) if a window must control itself.

## Host function ids

| Area | Functions | Streams |
|------|-----------|---------|
| window | `krate/window.Open`, `List`, `State`, `Close`, `SetTitle`, `SetSize`, `SetPosition`, `SetAlwaysOnTop`, `SetFullscreen`, `SetFrameless`, `Minimize`, `Maximize`, `Restore`, `Focus`, `Show`, `Hide`, `SetPreventClose`, `Navigate` | `krate/window.events` |
| dialog | `krate/dialog.OpenFile`, `OpenFolder`, `SaveFile`, `Message` | |
| clipboard | `krate/clipboard.ReadText`, `ReadImage`, `ReadFiles` (need `clipboard:read`), `WriteText`, `WriteImage`, `WriteFiles`, `Clear` | |
| menu | `krate/menu.SetApp`, `Popup` | `krate/menu.events` |
| tray | `krate/tray.Set`, `Remove` | `krate/tray.events` |
| shortcut | `krate/shortcut.Register`, `Unregister` | `krate/shortcut.events` |
| notify | `krate/notify.Send`, `Close` | `krate/notify.events` |
| app | `krate/app.Quit`, `Relaunch`, `Supports` | `krate/app.events` |

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
| Frameless, position, always on top | Verified (X11; Wayland ignores position and always-on-top) | Compile-only | Compile-only |
| Fullscreen, maximize, minimize | Verified with a window manager (openbox on Xvfb) | Compile-only | Compile-only |
| Close requested (prevent close) | Verified | Compile-only | Compile-only |
| Notifications | Verified against a fake `org.freedesktop.Notifications`; actions via `ActionInvoked` | `ErrUnsupported`: `UNUserNotificationCenter` needs an app bundle | `ErrUnsupported`: toasts need an AppUserModelID from a Start menu shortcut |
| Tray | Verified against a fake StatusNotifierWatcher (StatusNotifierItem plus `com.canonical.dbusmenu`); needs a StatusNotifier host | `ErrUnsupported` | `ErrUnsupported` |
| Global shortcuts | Verified on X11 (`XGrabKey`); Wayland uses the GlobalShortcuts portal (verified against a fake portal) or returns `ErrUnsupported` | `ErrUnsupported` | `ErrUnsupported` |
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
code that calls `krate/dialog.OpenFile` or listens on `krate/menu.events` can
be tested without a window. In Go, the `sdk` package test helpers run the
operations against an in-memory platform.
