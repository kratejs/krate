---
title: SDK - OS Integration
order: 11
description: Badges and progress, displays, power and idle, launch at login, the system theme and crash reports in the Native SDK, with the platform behind each.
---

# SDK: OS Integration

> **Experimental.** Part of [Krate Native](/docs/native/), which is under construction.

Six SDK areas reach the parts of the operating system an app lives in rather
than the window it draws. Each is a Go package for
[Go host code](/docs/native/go-host/) and a TypeScript module for the UI, and
each is gated by a [permission](/docs/native/sdk-ui/#permissions).

| Area | Go package | UI module | Permission |
|------|------------|-----------|------------|
| Badge and progress | `sdk/badge` | `@krate/app/badge` | `badge` |
| Displays | `sdk/screen` | `@krate/app/screen` | `screen` |
| Power and idle | `sdk/power` | `@krate/app/power` | `power`, `power:inhibit` (sleep blockers) |
| Launch at login | `sdk/login` | `@krate/app/login` | `login` |
| Theme | `sdk/theme` | `@krate/app/theme` | `theme` |
| Crash reports | `sdk/crash` | `@krate/app/crash` | `crash` |

Export names in the TypeScript modules are unique across the SDK (`setBadge`,
`listDisplays`, `getTheme`, ...) on purpose: the compiler puts every module of
a page in one namespace, so two modules that both export `set` or `get` collide
(warning KR7116) when a page imports both. The Go packages use short names
(`badge.Set`, `screen.List`).

`badge`, `screen`, `power` and `theme` are granted when `app.json` lists no
permissions. `power:inhibit`, `login` and `crash` must be listed: they keep the
machine awake, change what starts at login, and expose stack traces.
`app.supports("badge")` (and `"screen"`, `"power"`, `"login"`, `"theme"`,
`"crash"`) says whether the area can work right now.

## Badge and progress

```ts
import * as badge from "@krate/app/badge";

await badge.setBadge(3);                  // a count; setBadge(0) or clearBadge() removes it
await badge.setProgress("normal", 0.4);   // none | normal | indeterminate | error | paused
await badge.setUrgent(true);              // bounce, flash or mark urgent
```

| Call | Id |
|------|----|
| `setBadge(text or count)`, `clearBadge()` | `krate/badge.set` |
| `getBadge()` | `krate/badge.get` |
| `setProgress(state, value)`, `clearProgress()` | `krate/badge.setProgress` |
| `getProgress()` | `krate/badge.progress` |
| `setUrgent(on)` | `krate/badge.setUrgent` |

- **macOS:** `NSDockTile` badge label and a progress bar drawn into the dock
  tile. `error` and `paused` look like `normal`. `setUrgent` bounces the dock
  icon. Untested until CI runs it.
- **Windows:** `ITaskbarList3` `SetProgressValue` and `SetProgressState`
  (all five states), and `SetOverlayIcon` with a badge the SDK draws (counts
  above 99 read "99+"; other text longer than two glyphs becomes "!").
  `setUrgent` flashes the taskbar button. Untested until CI runs it.
- **Linux:** the Unity LauncherEntry D-Bus signal
  (`com.canonical.Unity.LauncherEntry.Update`), which GNOME Dash to Dock, KDE
  Plasma's task manager and Plank show. It carries a count, a progress
  fraction and an urgent flag, so only numbers can be a badge (other text
  rejects with `unsupported`) and indeterminate progress shows as none. The
  launcher matches the app by `<appId>.desktop`. Verified against a real
  D-Bus session with a listener.

## Displays

```ts
import * as screen from "@krate/app/screen";

const all = await screen.listDisplays();          // primary first
const here = await screen.displayOfWindow("");    // the main window's display
await screen.centerWindow("", all[1].id); // center on another display
screen.onDisplaysChanged(({ displays }) => redraw(displays));
```

A `Display` has `id`, `name`, bounds (`x`, `y`, `width`, `height`), a work area
(`workX`, `workY`, `workWidth`, `workHeight`: the part not covered by docks,
taskbars and menu bars), `scale`, `primary` and `refreshHz` (0 when unknown).
Coordinates are logical pixels in the space `window.state` and
`window.setPosition` use. An id is stable while the display configuration does
not change. `displayOfWindow` is the display with the largest overlap with the window.

Ids: `krate/screen.list`, `krate/screen.primary`, `krate/screen.ofWindow`,
`krate/screen.centerWindow`, and the stream `krate/screen.events`.

- **Linux:** GDK monitors (X11 and Wayland); changes from `monitor-added`,
  `monitor-removed` and the screen's `monitors-changed` and `size-changed`.
  Verified against Xvfb, which has one monitor.
- **macOS:** `NSScreen`, converted to top-left coordinates; changes from
  `NSApplicationDidChangeScreenParametersNotification`. Untested.
- **Windows:** `EnumDisplayMonitors`, `GetMonitorInfo`, `GetDpiForMonitor`;
  changes from `WM_DISPLAYCHANGE`. Untested.

## Power and idle

```ts
import * as power from "@krate/app/power";

const s = await power.powerState();       // { onBattery, idleSeconds, locked, idleState }
power.onPower("suspend", saveWork);
power.onPower("resume", reconnect);
await power.setIdleThreshold(300);        // "idle" and "active" events
const id = await power.preventSleep("exporting", true);
await power.allowSleep(id);
```

Events (`krate/power.events`): `suspend`, `resume`, `lock`, `unlock`, `ac`,
`battery`, `idle`, `active`. Suspend, resume, lock and unlock come from the OS;
battery and idle events come from sampling every three seconds while an event
stream is open. Idle events fire only after `setIdleThreshold`. Blockers end
when released or when the app exits. Ids: `krate/power.state`,
`krate/power.idleTime`, `krate/power.setIdleThreshold`,
`krate/power.preventSleep`, `krate/power.allowSleep`, `krate/power.events`.

- **Linux:** logind `PrepareForSleep` and the session's `Lock` and `Unlock`
  signals and `LockedHint`/`IdleHint`; UPower `OnBattery` (or
  `/sys/class/power_supply` without UPower); idle time from GNOME Mutter's
  IdleMonitor, `org.freedesktop.ScreenSaver.GetSessionIdleTime`, or logind's
  `IdleSinceHint`; blockers through `org.gnome.SessionManager.Inhibit`, else
  `org.freedesktop.PowerManagement.Inhibit` and `org.freedesktop.ScreenSaver.Inhibit`.
  logind's own `Inhibit` returns a file descriptor, which the SDK's D-Bus
  client cannot receive, so it is not used. Verified against fake services on
  private buses.
- **macOS:** `NSWorkspace` sleep and wake notifications, distributed
  screen-lock notifications, `IOPSCopyPowerSourcesInfo`,
  `CGEventSourceSecondsSinceLastEventType`, `IOPMAssertionCreateWithName`.
  Untested.
- **Windows:** `WM_POWERBROADCAST`, `WTSRegisterSessionNotification`,
  `GetSystemPowerStatus`, `GetLastInputInfo`, `SetThreadExecutionState`.
  Untested.

## Launch at login

```ts
import * as login from "@krate/app/login";

await login.setLoginItem(true, { args: ["--background"] });
const { enabled, method } = await login.getLoginItem();
```

Ids: `krate/login.get`, `krate/login.set`. The entry starts the running
executable with the given arguments only (not the arguments of the current run,
which may carry a deep link). In development the executable is the shell, so
use a release build to try it.

- **Linux:** `~/.config/autostart/<appId>.desktop` (XDG autostart,
  `XDG_CONFIG_HOME` honoured). An entry the user disabled in desktop settings
  (`Hidden=true` or `X-GNOME-Autostart-enabled=false`) reads as off. Verified.
- **macOS:** `SMAppService.mainApp` (macOS 13 and later) when the app runs from
  a signed bundle and no arguments are given, else a LaunchAgent plist in
  `~/Library/LaunchAgents/<appId>.plist`. Untested.
- **Windows:** a value named `<appId>` under
  `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`. A packaged (MSIX) app
  declares a `windows.startupTask` extension in its manifest instead; these calls
  then reject with `unsupported`, because toggling a StartupTask from code is
  not implemented. Untested.

## Theme

```ts
import * as theme from "@krate/app/theme";

const t = await theme.getTheme();   // { scheme, highContrast, system, source }
theme.onThemeChange((t) => document.documentElement.dataset.theme = t.scheme);
await theme.setTheme("dark");       // "system" (default), "light" or "dark"
```

Native title bars follow the system without any call. `setTheme` overrides them
(and, on Linux, GTK dialogs and menus) until `setTheme("system")`. The page's own
`prefers-color-scheme` is the webview's business and is not changed. Ids:
`krate/theme.get`, `krate/theme.set`, `krate/theme.events`.

- **Linux:** `org.freedesktop.portal.Settings` (`color-scheme` and `contrast`
  in `org.freedesktop.appearance`, and its `SettingChanged` signal), falling
  back to `GTK_THEME` and `~/.config/gtk-3.0/settings.ini`. GTK 3 does not read
  the portal itself, so the SDK sets `gtk-application-prefer-dark-theme`.
  Verified against a fake portal and in a real GTK window.
- **macOS:** `NSApp.effectiveAppearance`; changes from the
  `AppleInterfaceThemeChangedNotification` distributed notification and the
  accessibility display-options notification (not KVO). Untested.
- **Windows:** the `AppsUseLightTheme` registry value, `SystemParametersInfo`
  for high contrast, `WM_SETTINGCHANGE` for changes, and
  `DwmSetWindowAttribute(DWMWA_USE_IMMERSIVE_DARK_MODE)` for title bars. Untested.

## Crash reports

```ts
import * as crash from "@krate/app/crash";

for (const r of await crash.listCrashReports()) {   // reports from earlier runs
  upload(await crash.readCrashReport(r.id));         // your own endpoint
  await crash.deleteCrashReport(r.id);
}
await crash.setCrashAutoReload(true);                // reload a view whose process died
crash.onCrash((e) => console.warn(e.kind, e.reason, e.reloaded));
```

Ids: `krate/crash.list`, `krate/crash.read`, `krate/crash.delete`,
`krate/crash.setAutoReload`, and the stream `krate/crash.events`.

A report is a JSON file under `<logDir>/crashes` (`os.logDir()`), with `id`,
`kind` (`panic`, `fatal` or `webview`), `time`, `reason`, `appId`,
`appVersion`, `os`, `arch`, `osVersion`, `goVersion`, `shellVersion`,
`process` (`shell`, `host` or `webview`), `window`, `view` and the Go `stack`.
At most 50 are kept. Nothing is sent anywhere; uploading is the app's job.

- **Go panics and fatal errors** end the process before any code can run, so the
  shell and the Go host child ask the runtime to copy its crash output to
  `pending-<pid>.log` (`runtime/debug.SetCrashOutput`). The next
  `listCrashReports()` (or the shell, when the Go host child exits) turns the log of
  a dead process into a report, and a crash of the child while the shell runs
  also raises a crash event. A crash of the shell itself is therefore reported
  on the next launch, never live.
- **Webview process crashes** are reported at once, as an event and a report:
  WebKitGTK `web-process-terminated`, `WKWebView`
  `webViewWebContentProcessDidTerminate:`, WebView2 `ProcessFailed` (render and
  GPU process failures). The `reason` is `crashed`, `killed`, `out-of-memory`,
  `unresponsive` or `unknown`. With `setCrashAutoReload(true)` the shell reloads the
  view; it stops after three reloads of one view in 30 seconds, so a page that
  kills its renderer on load cannot loop.
- Verified on Linux by killing the web process of a real window. macOS and
  Windows are untested until CI runs the same test.

In Go: `crash.List`, `crash.Read`, `crash.Delete`, `crash.SetAutoReload` and
`crash.OnCrash` mirror the TypeScript API. The package `crashreport` is the
store the shell uses, for code that wants to write a report itself.
