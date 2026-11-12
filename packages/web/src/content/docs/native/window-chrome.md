---
title: Window Chrome
order: 11
description: Custom title bars and drag regions, Mica, Acrylic and vibrancy, caption buttons, macOS tabs and sheets, thumbnail buttons, jump lists and agent apps.
---

# Window chrome

> **Preview.** Part of [Krate Native](/docs/native/). Linux (WebKitGTK) is the
> platform where this is tested: drag regions, frameless resizing and
> transparency run in the GUI tests. The macOS and Windows code compiles, its
> pure logic (hit testing, colors, command lines, the Info.plist) is unit-tested,
> and platform GUI tests that read the state back run in the CI native job, but
> none of it has run on a Mac or a Windows PC by hand yet. The compatibility
> matrix rows stay "untested" until that CI run passes.

**Chrome** is everything around the page: the title bar, the window buttons, the
backdrop behind a see-through page, tabs, the dock and taskbar. Krate gives a
page the same controls Electron has, from config and from `@krate/app/chrome`,
and says so with `unsupported` when the platform has nothing to offer.

| Feature | Linux | macOS | Windows |
|---------|:-----:|:-----:|:-------:|
| `titleBar: "hidden"` / `"none"` | yes | yes | yes |
| Drag regions, double click maximizes | yes | yes | yes |
| Resize edges on a frameless window | yes (drawn by the page script) | yes (AppKit) | yes (hit test) |
| Transparent window | yes (needs an RGBA visual) | yes | yes |
| Backdrop (Mica, Mica Alt, Acrylic) | no | no | Windows 11 22H2+ |
| Vibrancy | no | yes | no |
| Caption buttons over a hidden title bar | no | traffic lights | yes (`titleBarOverlay`) |
| Traffic light position | no | yes | no |
| Document edited, represented file | no | yes | no |
| Sheets | no | yes | no |
| Native window tabs | no | yes | no |
| Thumbnail toolbar buttons | no | no | yes |
| Jump list tasks | no | no | yes |
| Share menu | no | yes | no |
| Dock icon, menu-bar-only apps | no | yes | no |

Ask in advance with `app.supports("chrome:<feature>")`: `titleBar`,
`transparent`, `backdrop`, `vibrancy`, `titleBarOverlay`, `trafficLights`, `drag`,
`resize`, `document`, `sheets`, `tabs`, `thumbButtons`, `jumpList`, `share` and
`dock`.

## Config

The keys of `native.window` in `krate.config.ts`:

```ts
native: {
  window: {
    titleBar: "hidden",             // "native" (default), "hidden" or "none"
    transparent: true,              // window and webview background see-through
    backdrop: "mica",               // Windows 11: "none" | "mica" | "micaAlt" | "acrylic"
    vibrancy: "sidebar",            // macOS: a material, see below
    titleBarOverlay: { color: "#1f2430", symbolColor: "#ffffff", height: 40 }, // Windows; true for the defaults
    trafficLightPosition: { x: 16, y: 20 },  // macOS
    tabbingIdentifier: "docs", tabbingMode: "preferred",  // macOS
  },
  bundle: { macos: { agent: true } }, // menu-bar-only app: LSUIElement in Info.plist
}
```

Every value is checked when the config loads (an error names the
`native.window.*` key and says what is allowed), and a key for another platform is
ignored there: the same config can carry `backdrop` and `vibrancy` together. `window.open` takes the same
options per window (`titleBar`, `transparent`, `backdrop`, `vibrancy`,
`titleBarOverlay`, `trafficLightPosition`, `tabbingIdentifier`, `tabbingMode`);
`frameless: true` still works and means `titleBar: "none"`.

What the three title bar styles mean:

- **`native`** is the system title bar.
- **`hidden`** removes the bar but keeps the window controls over the page:
  the traffic lights on macOS, the caption buttons on Windows. On Linux there
  are no system controls to keep, so it behaves like `none`; the page draws its own.
- **`none`** removes the bar and the controls. The window keeps its shadow and
  its resize edges (and its rounded corners on macOS and Windows 11).

## Drag regions

A custom title bar needs a way to say "this part moves the window". Mark it in
the page, with markup or CSS, and Krate does the rest on every platform:

```html
<header data-krate-drag>
  <h1>My app</h1>
  <button>Settings</button>           <!-- stays clickable -->
  <div data-krate-drag="false">...</div>   <!-- explicitly not draggable -->
</header>
```

```css
header { -webkit-app-region: drag; }   /* or: app-region: drag */
header .tools { -webkit-app-region: no-drag; }
```

- `data-krate-drag` (or `="true"`) marks a drag region, `="false"` or `="no-drag"`
  cuts a hole in one. The nearest marked ancestor decides.
- `-webkit-app-region` and `app-region` work in CSS, in inline styles and in Tailwind
  arbitrary properties (`[app-region:drag]`). Most engines drop the property while
  parsing, so Krate reads the page's style sheets as text (it follows
  `@media`, `@supports` and `@layer`, not CSS nesting or `@import`).
- `--krate-app-region: drag` as a custom property also works, for rules that
  must be computed.
- Buttons, links, inputs and other controls inside a drag region stay clickable
  unless you mark them `drag`. That includes elements with an ARIA widget role
  (`role="button"`, `tab`, `menuitem`, ...), elements with `tabindex` 0 or more and
  editable content, so a custom caption button built from a `div` works. See
  [Accessibility](/docs/native/accessibility/) for what a custom title bar needs.
- A double press maximizes the window (macOS follows the user's double click
  setting: zoom, minimize or nothing).

How it works: the shell puts a small script (about 3 KB, injected only into a
page that mentions `data-krate-drag` or `app-region`, or into every page of an app
with a hidden or transparent main window) into your HTML. It asks the shell to
start the native move (`gtk_window_begin_move_drag`, `performWindowDragWithEvent:`,
`WM_NCLBUTTONDOWN` with `HTCAPTION`) when you press in a region. The request names
no window: the shell finds the window from the webview that sent it, like every
other request. WebView2 also honors `app-region` itself; Krate turns that on for
windows without a native title bar (`IsNonClientRegionSupportEnabled`) and the
script then leaves CSS regions to the engine.

On a frameless Linux window the window manager gives no resize border, so the
script adds eight thin transparent edges around the page that start a native
resize. They are not drawn on a decorated, maximized or fullscreen window.

`chrome.startDrag()` and `chrome.toggleMaximize()` do the same from your own code
(for a press you handle yourself); a drag only starts while the mouse button is
still down.

## Windows

- **Backdrops.** `backdrop: "mica" | "micaAlt" | "acrylic"` sets
  `DWMWA_SYSTEMBACKDROP_TYPE` and makes the webview background transparent
  (`DefaultBackgroundColor`); leave the page's own `background` transparent where the
  material should show. They need Windows 11 22H2 (build 22621); earlier builds
  answer `unsupported`, and `"none"` always works.
- **Caption buttons.** With `titleBar: "hidden"` the window keeps its caption, and
  Windows draws minimize, maximize and close over the page's top right corner
  (DWM extends the frame by `titleBarOverlay.height`). The window answers
  `WM_NCHITTEST` with `HTMAXBUTTON` there, so hovering the maximize button shows
  Snap Layouts on Windows 11. `color` and `symbolColor` set the caption and
  caption text colors (Windows 11); `height` is the height of the area, 32 by
  default. The buttons are the system's, so the page must leave that corner
  transparent or empty: the example in `examples/native-chrome` keeps its bar
  clear of the right 140 pixels. `titleBar: "none"` has no buttons at all.
- **Thumbnail toolbar buttons** are up to seven buttons under the taskbar
  preview. The shell can add them once and never remove them, so Krate adds seven
  hidden slots and shows the ones you set. Icons are PNGs (16x16 is right). The
  buttons appear once Explorer creates the taskbar button; buttons set before that
  are applied then. A click arrives as a `thumb-click` event with your button id.

```ts
import { mainChrome, setJumpList, onChromeEvent } from "@krate/app/chrome";
await mainChrome().setThumbButtons([
  { id: "prev", tooltip: "Previous", icon: prevPng },
  { id: "play", tooltip: "Play", icon: playPng, dismissOnClick: false },
]);
onChromeEvent((e) => e.kind === "thumb-click" && console.log("clicked", e.id));
```

- **Jump list tasks.** `setJumpList` replaces the tasks in the app's taskbar
  jump list. A task starts the app with its `args`; with
  [single instance](/docs/native/sdk-ui/) on, a running instance receives them as a
  second-instance event instead. An argument that starts with `-` is passed after a
  `--`, so the shell does not read it as one of its own flags; `LaunchArgs` and the
  second-instance event give you the arguments without it. The list is committed
  before the call returns.

```ts
await setJumpList([
  { title: "New window", args: ["--new-window"], description: "Open another window" },
  { title: "Open notes", args: ["--open", "C:\\Users\\me\\Notes"], iconPath: "C:\\Program Files\\Acme\\acme.exe", iconIndex: 0 },
]);
```

## macOS

- **Title bar.** `hidden` uses a full-size content view with a transparent title
  bar, so the page runs under the traffic lights; `trafficLightPosition` places
  them (x from the left, y from the top). `none` also hides them.
- **Vibrancy.** `vibrancy: "sidebar"` puts an `NSVisualEffectView` behind the page and
  makes the webview transparent. Materials: `titlebar`, `selection`, `menu`,
  `popover`, `sidebar`, `headerView`, `sheet`, `windowBackground`, `hudWindow`,
  `fullScreenUI`, `tooltip`, `contentBackground`, `underWindowBackground` and
  `underPageBackground`.
- **Document state.** `setDocumentEdited(true)` shows the dot in the close button;
  `setRepresentedFile(path)` shows the file's proxy icon in the title bar (drag
  it, command-click it for the path menu).
- **Sheets.** `mainChrome().attachSheet(other)` shows another window of the app as a
  sheet; `endSheet()` on the sheet closes it, and a `sheet-ended` event arrives.
- **Tabs.** Windows with the same `tabbingIdentifier` tab together under
  `tabbingMode: "preferred"`. `mergeAllWindows()` and `moveTabToNewWindow()` move
  them, `tabs()` lists a group, and the tab bar's `+` button sends a `new-tab`
  event: open a window with the same identifier.
- **Share menu.** `share(window, { text, urls, files })` shows the system share
  picker (needs the `share` permission). It returns when the menu is shown.
- **Agent apps.** `bundle.macos.agent: true` writes `LSUIElement` into the
  Info.plist and the shell starts as a menu-bar-only app (no dock icon, no
  application menu), also in dev. `setDockVisible(true)` or
  `setActivationPolicy("regular")` brings the icon back at run time, and
  `"accessory"` hides it. Pair it with a [tray](/docs/native/sdk-ui/) icon.

## Linux

Frameless windows, transparency, drag and resize are the whole story: there is
no backdrop, vibrancy or taskbar API in GTK, and the calls answer `unsupported`.
A transparent window needs an RGBA visual and, to show the desktop behind it, a
compositing window manager; without a compositor the page sees the root window
behind it. `state()` reports `transparent: false` when the screen has no RGBA
visual. The header bar (`GtkHeaderBar`) is not used: `hidden` and `none` both remove
the decorations, and the page draws its own bar with a drag region. Under
Wayland the compositor decides where a dragged window goes, and GTK needs the
press that started the drag, which the script's request carries.

## From the UI

```ts
import { mainChrome, getChrome, share, setDockVisible, onChromeEvent } from "@krate/app/chrome";

const w = mainChrome();
const st = await w.state();     // titleBar, transparent, backdrop, vibrancy, features...
await w.setTitleBar("hidden");  // change the style at run time
await w.setVibrancy("hudWindow");
await w.setTitleBarOverlay({ color: "#101418", height: 36 });
await w.setTrafficLightPosition(16, 20);
await w.setTabbing("docs");
```

A call the platform lacks rejects with `KrateError` code `unsupported`; arguments
are validated before the platform sees them (`bad_args`).

## From Go host code

```go
import "github.com/kratejs/krate/packages/native/sdk/chrome"

w := chrome.Main()
st, _ := w.State()
w.SetVibrancy("sidebar")
w.SetDocumentEdited(true)
chrome.SetJumpList([]chrome.JumpTask{{Title: "New window", Args: []string{"--new-window"}}})
chrome.SetDockVisible(false)
cancel, _ := w.OnNewTab(func() { /* window.Open with the same tabbing identifier */ })
```

`chrome.OnEvent` reports `new-tab`, `tabs-changed`, `thumb-click` and
`sheet-ended`. Build with `-tags krate_no_chrome` to leave the area out.

## Permissions

The window operations need `window` (a default permission), the jump list and the
dock `app`, and the share menu the new `share` permission, which is not a default
one. `krate native build` derives them from the ids your code reaches
(`krate/chrome.*`).

## Testing

`go test ./platform/caps` covers the hit test, colors, tables, command-line
quoting and validation; `go test ./internal/chrome` runs the page script's CSS
parser in QuickJS and the endpoint. The Linux GUI tests in
`platform/linux/gui_chrome_test.go` press the mouse with XTEST on a real window
manager and check that a drag moves the window, a double press maximizes it and an
edge resize grows it. The macOS and Windows GUI tests read the AppKit and Win32
state back (style masks, button frames, `NSVisualEffectView` material, document
state, `WM_NCHITTEST` results, the DWM backdrop type, the WebView2 background
color, jump list and thumbnail button calls) and run in the CI native job.
`examples/native-chrome` is a frameless app that the end-to-end test drives
through the real shell.
