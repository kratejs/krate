---
title: Accessibility
order: 24
description: What Krate Native guarantees screen readers and keyboard users on Linux, macOS and Windows, and what authors must do for title bars, menus and dialogs.
---

# Accessibility

> **Experimental.** Part of [Krate Native](/docs/native/). The Linux checks run as GUI tests
> through AT-SPI, the same bus Orca reads. On macOS the tests ask our own AppKit objects
> through NSAccessibility, and read the AX tree when the test process is allowed to; on
> Windows they read the UI Automation tree. The macOS and Windows tests run only in the CI
> native job: nothing here has been tried with VoiceOver or Narrator by hand, and no screen
> reader was used on any platform. The compatibility row `native-accessibility` says the
> same, per platform.

A Krate Native window has two kinds of content. The **page** inside the webview is exposed
by the engine (WebKitGTK, WKWebView, WebView2) from your HTML, so semantic markup, labels
and ARIA are your job, exactly as on the web. Everything **around** the page is drawn or
configured by the shell: window titles, menus, dialogs, the tray, notifications, split-pane
dividers and the caption buttons. This page is about that second kind.

## What Krate guarantees

| | Linux (AT-SPI) | macOS (NSAccessibility) | Windows (UI Automation) |
|---|---|---|---|
| Window named by its title, in every `titleBar` style | yes (tested) | yes (tested in process) | yes (tested) |
| The page reachable in `native`, `hidden`, `none` and transparent windows | yes (tested) | yes (tested in process) | yes (tested) |
| Remote views reachable | yes (tested) | not tested | not tested |
| Offscreen windows left out of the tree | yes (tested) | hidden from AX (tested in process) | reported `IsOffscreen`, still listed |
| Menu bar and context menu: names, roles, checked, disabled, separators, accelerators | yes (tested) | yes (tested in process) | menu bar and popup items tested |
| Menus open from the keyboard | F10 and Alt+mnemonic (tested) | system | Alt and Esc (Esc tested) |
| Message dialogs: named, buttons named, default button, Esc | yes (tested) | system (default button set) | named, labels spelled out in the text |
| Tray icon always has a name | StatusNotifierItem title and tooltip | accessibility label | tip |
| Notification action buttons always have a label | yes | yes | yes |
| Split-pane divider: name, focus, arrow keys resize | yes (tested) | NSSplitView role (tested) | **no keyboard access** |
| Tab moves through every view of a split window | yes (tested) | system | not tested |
| Caption buttons of a hidden title bar | none drawn | traffic lights, system labels | drawn by Windows, named by it |
| Thumbnail toolbar buttons always have a name | n/a | n/a | tooltip, or the id |

## What you must do

**Window titles.** The `title` of a window is its accessible name, also when the title bar
is `hidden` or `none` and the user never sees it. Give every window a distinct, meaningful
title (`"Settings"`, not `""`) and update it when the document changes.

**Custom title bars.** With `titleBar: "none"` there are no caption buttons, on any
platform. If you draw your own, make them real controls:

```html
<header data-krate-drag>
  <h1>Notes</h1>
  <button aria-label="Close window" onclick="...">x</button>
</header>
```

- Use `<button>` (or `role="button"` with `tabindex="0"`) and a text label or `aria-label`.
  An icon-only `<div>` is invisible to a screen reader.
- Controls inside a drag region stay clickable and focusable: native controls, elements
  with an ARIA widget role (`button`, `link`, `tab`, `menuitem`, `checkbox`, `slider`, ...),
  and elements with `tabindex` 0 or more. A press on them is never turned into a window
  move. Mark a region `data-krate-drag="false"` to cut a hole in a drag region.
- Do not lay an invisible drag layer over controls (an absolutely positioned
  `data-krate-drag` element above a button): the press reaches the layer, not the button.
  Put the drag attribute on the bar and leave the controls in it.
- Keep the page's keyboard path complete. A drag region cannot be moved from the keyboard;
  offer the window manager's shortcut, and a `Close` control reachable by Tab.
- On Windows a `none` window has no system menu, so Alt+Space does nothing; Alt+F4 closes.
  `hidden` keeps the caption buttons, which Windows draws and names itself.

**Menus.** Give every item a `label`. Put `&` before the mnemonic letter (`"&File"`): screen
readers speak "File", and Alt+F opens it on Linux and Windows. Use `accelerator` so the
shortcut is announced. Disabled items are announced as unavailable.

**Dialogs.**
- Give a message dialog a `title` (Linux, Windows) and a `message`. A dialog without a title
  is named after its kind on Linux ("Error") and after the app on Windows.
- Windows message boxes have fixed buttons (OK; Yes and No; Yes, No and Cancel). Krate names
  the mapping in the box text when your labels differ ("Yes: Save, No: Discard"), and
  honors `default`. Two-button boxes cannot be closed with Esc; add a third button if the
  user must be able to dismiss without choosing.
- On macOS, Esc presses only a button titled "Cancel". Name your dismiss button that.
- `default` picks the button Enter presses on every platform.

**Tray.** Set `tooltip` or `title`. Each stands in for the other, and with neither the
icon is named after the app, which is rarely what you want.

**Notifications.** A `title` is required. Give every action a `label`; an action without one
is named by its id.

**Thumbnail buttons and jump lists (Windows).** Set `tooltip` on every thumbnail button: it
is the only name the button has. A jump list task needs a `title`.

**Split panes.** On Linux each divider is a named, focusable split pane: focus it and use
the arrow keys. The views are named "Pane 1 of 2" and so on; give each page a `<title>`
too. On Windows the divider is mouse-only. Bind your own shortcut that calls
`setDividerPosition` (see [Views and split panes](/docs/native/views/)) for keyboard users.

**Focus after a dialog.** The window that opened a dialog is the dialog's parent; pass its
`window` so focus returns to it. Dialogs without a parent return focus to wherever the
window manager puts it.

## Testing your app

Turn the screen reader on and use only the keyboard: Orca (`orca`) on Linux, VoiceOver
(Cmd+F5) on macOS, Narrator (Win+Ctrl+Enter) on Windows. Linux also has `accerciser` to
browse the AT-SPI tree. The tests Krate itself runs are in
`packages/native/ACCESSIBILITY.md`.

## Known gaps

- No platform was tested with a real screen reader.
- Windows: split-pane dividers have no keyboard access; frameless windows have no system
  menu; offscreen windows stay in the UI Automation tree (marked offscreen).
- Windows tray, thumbnail button tooltips and jump list entries are named, but the shell
  surfaces they appear in (notification area, taskbar thumbnail) are not read by tests.
- macOS: Esc dismisses an alert only through a "Cancel" button; the split divider is
  reachable by VoiceOver but not by a plain keyboard.
- Linux: the AT-SPI tests need a window manager for focus and `at-spi2-core`, and run on
  X11 only. Wayland has not been checked.
