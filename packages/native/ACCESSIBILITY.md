# Accessibility of the native shell

What was checked about the UI that Krate Native draws or configures (not the page inside the
webview, which the engine exposes), how, what was wrong, what was fixed, and what is still
open. User-facing guidance is in `docs/native/accessibility`.

Nothing here used a real screen reader. Every check reads the platform's accessibility tree
the way a screen reader's backend does, or asks our own objects for their accessibility
properties. Linux ran here; the macOS and Windows tests are compiled (`go vet` for both OSes)
and run only in the CI native job, so they stay "untested" in the compatibility matrix
(`native-accessibility`) until that job passes.

## Method

### Linux: AT-SPI

`internal/atspi` is a small AT-SPI client over `internal/dbus`: it starts a private
`dbus-daemon` as the accessibility bus with `at-spi2-registryd` on it, walks the tree
(`Accessible`, `Component`, `Action`, `Value`), and synthesizes keys through the registry's
`DeviceEventController` (XTest), as an assistive technology would. The tests in
`platform/linux/a11y` (their own package, because the bridge must be on at `gtk_init`) start
the real GTK/WebKitGTK app with `NO_AT_BRIDGE` unset and `AT_SPI_BUS_ADDRESS` pointing at that
bus, then assert roles, names, states, key bindings and focus.

Needs: `KRATE_NATIVE_GUI=1`, a display, `dbus-daemon`, `at-spi2-registryd` (package
`at-spi2-core`; `KRATE_ATSPI_REGISTRYD` overrides the path), and for focus and key tests a
window manager plus `KRATE_NATIVE_WM=1`. Without the registry the package skips with the
reason. I added `at-spi2-core` and `libatspi2.0-0t64` to the private sysroot
(`~/wt/kn-sysroot`, see its README).

### macOS: NSAccessibility and AX

`platform/darwin/gui_a11y_test.go`. The in-process group asks our own NSWindow, NSMenu items,
NSStatusBarButton, NSSplitView and WKWebView for `accessibilityRole`, `accessibilityLabel` and
`isAccessibilityHidden`; it needs no permission, so it runs on a CI runner. The AX API group
(`AXUIElementCreateApplication`, `AXWindows`, `AXMenuBar`, via purego) needs the process to be
trusted: it checks `AXIsProcessTrusted` and skips with the reason (macOS runners are not
trusted).

### Windows: UI Automation

`platform/windows/uia_windows_test.go` is an `IUIAutomation` client over raw vtable calls, on
an MTA thread (never the UI thread). It reads the window, the WebView2 host, caption buttons,
the menu bar, a real `MessageBoxW` and a real `TrackPopupMenuEx` popup.

## Results and fixes

| Area | Linux | macOS | Windows |
|---|---|---|---|
| Window name = title, all title bar styles | ok, tested | ok by construction, tested in process | ok by construction, tested |
| Webview reachable (native, hidden, none, transparent) | ok, tested | tested in process | tested (Document or Chromium host) |
| Remote view reachable | ok, tested | not tested | not tested |
| Offscreen window | **was exposed**; fixed, tested | **was visible to AX**; fixed (`setAccessibilityHidden:`), tested in process | exposed, `IsOffscreen` true (tested); cannot be removed |
| App menu bar | ok (mnemonics stripped, check/radio roles, checked, disabled, separators, accelerators in the key binding), tested | **top-level items had an empty title**; fixed | tested through UIA |
| Context menu | ok, tested, Esc closes it | system | tested, Esc closes it |
| F10 / Alt+mnemonic / accelerator with the focus in the page | ok, tested | system | not tested |
| Message dialog | **custom title ignored** (GtkMessageDialog names itself "Question"); fixed. Buttons, message, detail, default button, Enter and Esc tested | **default button ignored**; fixed | **labels ignored, empty title unnamed, default ignored**; fixed, tested |
| File dialog | named by title, Cancel button, Esc closes, focus returns, tested | system NSOpenPanel | system |
| Focus after a dialog | parent active again, tested | system | system |
| Split panes | **unnamed dividers and panes**; fixed (`Pane divider`, `Pane N of M`). Divider focusable and resized by arrow keys, Tab visits every view (tested) | NSSplitView role tested | **dividers are mouse-only (gap)** |
| Tray | **title could be empty**; Title and ToolTip now fall back to each other and to the app name (unit tested) | **button had no label**; fixed | tip falls back likewise |
| Notifications | action without label named by its id (unit tested) | same | same |
| Thumbnail buttons | n/a | n/a | **tooltip is the only name; empty tooltip left the button unnamed**; falls back to the id (unit tested) |
| Jump list | n/a | n/a | title already required (validated) |
| Custom title bar drag regions | **a press on an ARIA widget or focusable element inside a drag region was turned into a window move** (preventDefault on mousedown, so no focus, no click); fixed in `internal/chrome/chrome.js`, unit tested. The Linux resize strips are `aria-hidden` | same script | same script |
| Hidden title bar caption buttons | none drawn (page draws them) | traffic lights kept, AppKit labels (tested) | Windows draws and names them; reported, not required |

## Known gaps

- No real screen reader on any platform.
- Windows: split dividers have no keyboard path (apps can bind `setDividerPosition`);
  `titleBar: "none"` removes the system menu (no Alt+Space; Alt+F4 works); offscreen windows
  remain in the UIA tree; a two-button `MessageBoxW` has no Esc; the notification area,
  thumbnail toolbar and jump list are not read by tests; the caption buttons of a hidden title
  bar are not asserted.
- macOS: Esc dismisses an `NSAlert` only through a button titled "Cancel"; the AX API tests
  skip on untrusted processes (so CI runs only the in-process group); the NSSplitView divider is
  not keyboard-resizable without VoiceOver or Full Keyboard Access.
- Linux: X11 only (Wayland unchecked); needs the AT-SPI bridge in the user session for a real
  screen reader to see any of it. A message dialog whose Esc destroys the window logs a
  `gtk_widget_destroy` CRITICAL (the dialog is destroyed twice); harmless and older than this work.
- Web content inside the views: left to the engines, untested here.

## Where things are

- `internal/atspi` (client, bus helper), `platform/linux/a11y` (tests), `platform/linux/a11y.go`
  (ATK helpers: hide offscreen, names), `platform/darwin/a11y_darwin.go`,
  `platform/darwin/gui_a11y_test.go`, `platform/windows/msgbox.go`,
  `platform/windows/uia_windows_test.go`, `platform/caps/chrome.go` (`TrayLabel`,
  `AppDisplayName`, `ThumbButtonName`, `ActionLabel`).
