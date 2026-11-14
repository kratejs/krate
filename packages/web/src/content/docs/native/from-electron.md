---
title: Migrating from Electron
order: 22
description: Move an Electron app to Krate Native - concepts, a full API mapping table, a worked port with tests, security, packaging, debugging and what is different.
---

# Migrating from Electron

> **Experimental.** Part of [Krate Native](/docs/native/), which is under construction.
> This guide describes what exists in this release. Where Krate Native has no
> equivalent of an Electron API, the table says so instead of suggesting a
> workaround that does not exist.

An Electron app is a Node.js main process, one or more Chromium renderer
processes and a preload script between them. A Krate Native app is a Go shell
that shows your pages in the **system webview** (WebKitGTK on Linux, WKWebView
on macOS, WebView2 on Windows) and runs your **host code** (Go, or TypeScript
on an embedded QuickJS) in the native process. The UI is TSX that Krate
compiles; the page is plain HTML, CSS and a small signal runtime.

What carries over: HTML, CSS and the web platform your pages use, TypeScript,
npm packages that run in a browser, and the shape of your app (windows, a menu,
a tray, dialogs, files, a settings store). What does not: anything in the page
that needs Node, `require`, Electron's modules or a specific Chromium version.

The [worked port](#a-worked-port) below converts a small real Electron app,
file by file. Its Electron original and the ported app are both in the
repository: [`examples/native-from-electron`](https://github.com/kratejs/krate/tree/main/examples/native-from-electron).

## Concepts

| Electron | Krate Native |
|----------|--------------|
| Main process (`main.js`) | **Host code**: Go files, or TypeScript marked `@host` or named `*.host.ts` ([Host code](/docs/native/host-code/), [Go host code](/docs/native/go-host/)). It runs in the native process. There is no `app.on('ready')`; startup work runs from Go `init`/`main` code or from the first page. |
| Renderer process | A page: a route of your app, prerendered at build time and shown in the system webview. Pages are TSX (`src/pages`). |
| Preload script and `contextBridge.exposeInMainWorld` | Nothing to write. The page `import`s a host module and calls its functions; the compiler generates the stubs and the `.d.ts` types ([Splitting host code from the UI](/docs/native/host-code/#splitting-host-code-from-the-ui)). The page always runs isolated, with no Node. |
| `ipcMain.handle` and `ipcRenderer.invoke` | An exported host function and a call to it. Rejections arrive as a `KrateError` with a `code`. |
| `ipcRenderer.on` and `webContents.send` (main to renderer) | A **host signal** (`hostSignal()`, Go `krate.NewSignal`) for state that pages follow, a host **stream** (an `async function*`, Go `<-chan T`) for a sequence of values, or [`view.post`](/docs/native/views/#messages) for a one-off message to a view. |
| `ipcRenderer.send` / `ipcMain.on` (fire and forget) | A host function whose result you ignore. There is no `sendSync`. |
| Renderer to renderer (`MessageChannelMain`, `postMessage` through the main process) | `view.post` between views, or [message ports](/docs/native/processes/#message-ports) (`@krate/app/port`). |
| `BrowserWindow` | A window: the first one is `main`, others open with `open()` from `@krate/app/window` (Go `window.Open`). Options: [`WindowOptions`](/docs/native/sdk-ui/). |
| `BrowserView`, `WebContentsView` | A [view](/docs/native/views/): several webviews in one window with split panes, their own routes and per-view scopes. |
| `webContents` | The web contents of a view: [`@krate/app/page`](/docs/native/webcontents/) (print, find, zoom, capture), `@krate/app/downloads`, `@krate/app/cookies`. |
| `session` | Cookies and site data for the app's own pages ([`cookies`](/docs/native/webcontents/#cookies-and-site-data)); for remote pages, a [partition](/docs/native/remote/#the-security-model) with its own storage, user agent, proxy and request rules. |
| `protocol.handle`, `webRequest` | The app's pages are served by the shell from the app directory over a fixed scheme (`krate://app`, `https://app.localhost` on Windows). You cannot register another scheme or intercept the app's own requests. For remote pages: [request rules](/docs/native/remote/#request-rules) (block, redirect, headers). |
| `utilityProcess.fork` | [Workers](/docs/native/processes/#workers): `native.workers`, started by name, with their own narrowed permissions. |
| Child processes (`child_process`) | Go host code uses `os/exec`; TypeScript host code on QuickJS has no `child_process`; use a [worker](/docs/native/processes/#workers) or a Node, Bun or Deno [host runtime](/docs/native/runtimes/). |
| `MessageChannelMain` | [Ports](/docs/native/processes/#message-ports), shell-backed (the engines cannot share a real `MessagePort`). |
| `app.getPath('userData')` | `appDataDir()` from `@krate/app/os`. |
| `electron-store` or a JSON file in `userData` | `@krate/app/kv` (a key-value store), `@krate/app/db` (SQLite), or files through `@krate/app/fs`. |
| `electron-builder`, Forge | `krate native package` ([Packaging](/docs/native/packaging/)). |
| `electron-updater`, `autoUpdater` | `@krate/app/updater` with a signed JSON feed ([Updates](/docs/native/updater/)). |
| Playwright for Electron | `krate native test` and `@krate/app/driver` ([Testing](/docs/native/testing/)). |

## A worked port

`examples/native-from-electron` contains a small Electron app, **Scratchpad**,
and its port. The Electron version is in `electron/` (pinned versions in
`electron/package.json`, no `node_modules`; run it with `npm install` and
`npm start` there). The Krate Native version is the rest of the directory. Both do the same
things:

- a window with a text area, a **Settings** window opened from the menu;
- an application menu with accelerators (New, Open, Save As, Settings, About);
- a tray icon with Show and Quit;
- open and save dialogs, and files read and written through them;
- a draft saved automatically to the app's data directory;
- a notification;
- a settings store (theme and font size) that the main window follows when the
  settings window changes it.

| Electron | Krate Native |
|----------|--------------|
| `electron/main.js` | `src/lib/store.host.ts` (host functions and signal) and the setup code in `src/pages/index.tsx` |
| `electron/preload.js` | none |
| `electron/renderer/index.html`, `renderer.js` | `src/pages/index.tsx` |
| `electron/renderer/settings.html`, `settings.js` | `src/pages/settings.tsx` |
| `electron/package.json` (`build`) | `krate.config.ts` (`native`) |
| `electron/tests/app.spec.js` | `tests/port.native.test.ts` |

### Step 1: create the project

Start from `krate native init scratchpad` and add `@krate/app`, `@krate/core`
and `@krate/runtime` to `package.json`
([Getting started](/docs/native/getting-started/)). The Electron `package.json`
fields that described the app move to the `native` block:

```ts
// krate.config.ts
export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.scratchpad',     // build.appId
    productName: 'Scratchpad',           // build.productName
    version: '1.0.0',                    // version
    window: { width: 900, height: 640, resizable: true },
    permissions: ['app', 'dialog', 'fs', 'kv', 'menu', 'notify', 'os:info', 'tray', 'window'],
  },
});
```

`native.permissions` has no Electron counterpart. It is an allowlist; see
[Step 8](#step-8-permissions) and [Security](#security).

### Step 2: turn `ipcMain.handle` into host functions

In `main.js` each channel is a handler registered with `ipcMain.handle`, and the
preload script re-exports it as a method. Draft loading and saving, and the
settings:

```js
// electron/main.js
ipcMain.handle('draft:load', async () => { /* readFile(userData/draft.txt) */ });
ipcMain.handle('draft:save', (_event, text) => fs.writeFile(draftFile(), text, 'utf8'));
ipcMain.handle('settings:get', () => settings);
ipcMain.handle('settings:update', async (_event, patch) => { /* validate, write, broadcast */ });
```

In Krate Native they are exported functions of a host module. A file named
`*.host.ts` is host code, so it runs in the native process, not in the webview:

```ts
// src/lib/store.host.ts
import { hostSignal } from '@krate/app/host';
import * as kv from '@krate/app/kv';
import * as fs from '@krate/app/fs';

export const settings = hostSignal<Settings>(defaults);

export async function updateSettings(patch: Partial<Settings>): Promise<Settings> {
  await load();
  const next = { ...settings.get(), ...patch };
  if (next.theme !== 'light' && next.theme !== 'dark') throw new Error('theme must be light or dark');
  await kv.set('settings', next);
  settings.set(next);
  return next;
}

export async function loadDraft(): Promise<string> {
  return (await fs.exists('draft.txt')) ? fs.readText('draft.txt') : '';
}
export async function saveDraft(text: string): Promise<void> {
  await fs.writeText('draft.txt', text);
}
```

What changed:

- **No registration, no channel names.** The export is the API. The page imports
  it (`import { saveDraft } from '../lib/store.host'`) and the compiler turns the
  import into a call to the id `src/lib/store.host.saveDraft`.
- **No `fs` from Node.** TypeScript host code runs on an embedded QuickJS by
  default, which has no Node built-ins (importing `node:fs` is `KR7230`). The
  same `@krate/app/fs` and `@krate/app/kv` the page can use also work in host
  code, in-process. A relative path is relative to the app data directory, so
  `draft.txt` is Electron's `path.join(app.getPath('userData'), 'draft.txt')`.
  If you need Node itself, set `native.host.runtime` to `node`, `bun` or `deno`
  ([Host runtimes](/docs/native/runtimes/)); the SDK modules are not wired into
  those children yet.
- **Settings in `kv`, not a JSON file.** `kv.get`/`kv.set` replace the
  `settings.json` read and write. Use `@krate/app/db` if the data is relational.
- **Errors.** `throw new Error(...)` in a host function rejects the page's call,
  as `ipcRenderer.invoke` does. In a dev build the message ends with the source
  location (`(at src/lib/store.host.ts:36:64)`). Throw an object with a `code`
  to give the page something to branch on.
- **Arguments are data.** Functions, class instances and DOM objects cannot cross
  ([`KR7190`](/docs/native/host-code/#data-across-the-boundary-kr7190)). Dates,
  `Uint8Array`s and plain objects can.

### Step 3: replace `webContents.send` with a host signal

Electron broadcasts the new settings to every window:

```js
for (const win of BrowserWindow.getAllWindows()) win.webContents.send('settings', settings);
```

and each renderer subscribes through the preload script (`onSettings`). In
Krate Native the host module exports a signal (above: `settings`) and every
page that reads it is updated when the host calls `settings.set(next)`:

```tsx
// src/pages/index.tsx and src/pages/settings.tsx
import { getSettings, settings } from '../lib/store.host';

// A host signal reads as undefined until the host has pushed its first value.
const theme = () => settings()?.theme ?? 'light';

<main data-theme={theme()}>...</main>
```

The settings window calls `updateSettings`; the main window, which never asked,
changes theme. A host signal that has no value yet reads as `undefined`: the
first version of the port threw a `TypeError` on `settings().theme` in the page
until the value arrived, which the page-errors check of the test caught.

### Step 4: the pages

`renderer/index.html` and `renderer.js` become one TSX component. DOM code
(`document.getElementById`, `addEventListener`, `textContent`) becomes signals
and JSX:

```tsx
const [text, setText] = createSignal('');
<textarea id="note" value={text()} onInput={(e) => edit(e.currentTarget.value)} />
<p id="status">{status()}</p>
```

The draft is saved 300 ms after the last keystroke by the same debounce the
Electron renderer uses. A second window is a second route: `src/pages/settings.tsx`
is the page `/settings`.

### Step 5: windows, menu, tray

Electron's `main.js` builds these at startup. Krate Native has no `ready`
event, so the example does the setup once from the main page, in an effect (a Go app would do it from Go
host code, as in the [Windows, dialogs and the desktop](/docs/native/sdk-ui/#from-go)
example):

```tsx
// Menu: Menu.setApplicationMenu(Menu.buildFromTemplate(...))
menu.setApp([{ label: '&File', submenu: [
  { id: 'file.new', label: 'New', accelerator: 'CmdOrCtrl+N' },
  { id: 'file.settings', label: 'Settings...', accelerator: 'CmdOrCtrl+,' },
  // ...
] }]);
menu.onMenu('file.new', newNote);                  // template `click` handlers
menu.onMenu('file.settings', () => openWindow({ path: '/settings', title: 'Settings', width: 420, height: 280, fixed: true }));

// Tray: new Tray(nativeImage.createFromPath(...)) with setContextMenu and 'click'
fetch('/tray.png').then((r) => r.arrayBuffer()).then((png) =>
  tray.set({ tooltip: 'Scratchpad', icon: new Uint8Array(png), menu: [
    { id: 'tray.show', label: 'Show' }, { id: 'tray.quit', label: 'Quit' } ] }));
tray.onEvent((e) => { /* e.kind is click, middle-click, right-click or menu; e.id is the item */ });
```

Differences to know about:

- Menu items have an `id` and the click arrives as an event (`onMenu(id, fn)` or
  `onClick`), not as a `click` function in the template. There are **no `role`
  items** (`role: 'quit'`, `editMenu`): add an item with its own id and call
  `quit()`. On macOS, an app menu without an Edit menu does not give you the
  standard editing shortcuts, so build an Edit menu from items yourself.
  Accelerators are written `CmdOrCtrl+...`; Electron's `CommandOrControl` is not
  accepted on Linux and Windows.
- There is **one application menu** for all windows (`setApp`); each click event
  carries the id of the window it came from.
- The tray icon is PNG bytes (`Uint8Array`), not a `nativeImage`. The example
  ships `public/tray.png` and fetches it. On Linux the tray needs a
  StatusNotifier host or an XEmbed tray; without one `tray.set` rejects with
  `unsupported`, so the example catches it and reports it in the page.
- The tray and menu belong to the app, not the page, but in this example they are
  created by the main page, so reloading the main page sets them again. For a
  tray app that stays alive with no window set `native.window.keepAlive`
  ([Memory](/docs/native/memory/#windows-and-background-mode)).
- `BrowserWindow`'s `parent` and `modal`, `show: false`, `minWidth` and `maxWidth`
  and `backgroundColor` options have no counterpart in `open()`. Hide a window
  after opening it with `hide()`.

### Step 6: dialogs and files without a preload bridge

The Electron renderer cannot touch the file system, so the main process shows
the dialog and reads the file:

```js
ipcMain.handle('file:open', async () => {
  const { canceled, filePaths } = await dialog.showOpenDialog(mainWindow, { /* filters */ });
  if (canceled || filePaths.length === 0) return null;
  return { path: filePaths[0], text: await fs.readFile(filePaths[0], 'utf8') };
});
```

In Krate Native the page may call both the dialog and the file API, and the
**SDK** keeps it safe: a path is allowed only inside the app directories or when
the user chose it in a dialog.

```tsx
async function openDialog() {
  const [path] = await dialog.openFile({ filters: [{ name: 'Text', patterns: ['*.txt', '*.md'] }] });
  if (path) setText(await fs.readText(path));      // [] when the user cancels
}
async function saveAs() {
  const path = await dialog.saveFile({ name: 'note.txt', overwrite: true });   // null when cancelled
  if (path) await fs.writeText(path, text());
}
```

`dialog.openFile` and `saveFile` grant the chosen path to the `fs` scope, so
`readText` and `writeText` succeed for it and reject with `permission_denied`
(`reason: "outside_scope"`) for every other path outside the app directories.
That is stricter than Electron, where the main process reads whatever path the
renderer sends it. If you want Electron's shape (the page only passes a request),
put the dialog and the read in a Go or TypeScript host function; host code is
trusted and is not scoped. Filters use glob patterns (`*.txt`), not extensions.
Cancelling gives `[]` or `null`, not `{ canceled: true }`.

Files the app is asked to open (double click, command line, drop on the window)
arrive through `onOpenFile` and `onFileDrop`
([Files and drag and drop](/docs/native/files/)); that replaces the `open-file`
app event and `webUtils.getPathForFile`.

### Step 7: notifications

```js
const n = new Notification({ title, body });
n.on('click', () => { mainWindow.show(); mainWindow.focus(); });
n.show();
```

becomes

```tsx
await notify.send({ id: 'reminder', title: 'Scratchpad', body: '...' });
notify.onAction('reminder', '', () => main().focus());     // '' is a click on the body
```

`Notification.isSupported()` has no counterpart; call `send` and handle the
`unsupported` rejection (Linux without a notification service answers that).
Actions (`actions: [{ id, label }]`), urgency and replacing a notification by
sending the same `id` are supported. On macOS notifications need an app bundle;
`krate native dev` runs from a generated dev bundle for that reason.

### Step 8: permissions

The ported app uses `app`, `dialog`, `fs`, `kv`, `menu`, `notify`, `tray` and
`window`. `krate native build` finds that out by reading the imports and writes
exactly this list to `app.json`. `native.permissions` is only an allowlist: the
build fails with `KR7210` if the code needs something the list does not name.
`krate native dev` and `krate native test` grant the list as written (or the
non-sensitive defaults when there is none), which is why `fs`, `tray` and
`os:info` (used by the test) are listed. Run `krate native check` to see which
import needs which permission. See [Release builds, derived permissions](/docs/native/release/#derived-permissions).

### Step 9: tests

Electron's Playwright test launches the app and drives the renderer:

```js
const app = await electron.launch({ args: ['.'] });
const page = await app.firstWindow();
await page.fill('#note', 'hello');
```

`tests/port.native.test.ts` does the same with `launch()` from
`@krate/app/driver`, and also covers files, the menu, a second window, the tray
and the notification. Run it with `krate native test` (on a headless Linux
machine, `xvfb-run -a krate native test`). Details in [Testing](#testing).

## API mapping

`@krate/app/<area>` is the TypeScript module, `krate/<area>.<op>` the SDK id
(what `page.call()` takes and what the [permission table](/docs/native/sdk/#permissions)
lists), and the Go column is the package under
`github.com/kratejs/krate/packages/native/sdk/`. "None" means there is no
equivalent in this release.

### App

| Electron | Krate Native | Notes |
|----------|--------------|-------|
| `app.quit()` | `quit()` from `@krate/app/app` (`krate/app.quit`); Go `app.Quit()` | |
| `app.relaunch()` | `relaunch()` (`krate/app.relaunch`) | |
| `app.getPath('userData')` | `appDataDir()` (`krate/os.appDataDir`); Go `sdkos.AppDataDir()` | Named after the `appId`: `~/.local/share/<id>`, `~/Library/Application Support/<id>`, `%APPDATA%\<id>`. |
| `getPath('cache' / 'logs' / 'temp')` | `cacheDir()`, `logDir()`, `tempDir()`; `configDir()` | `appData`, `home`, `documents`, `downloads`, `desktop`, `exe`: none. Ask the user with `dialog`. |
| `app.getVersion()`, `app.getName()`, `app.getLocale()`, `process.platform`, `process.arch` | `info()` (`krate/os.info`): `appVersion`, `appId`, `locale`, `os` (`linux`, `darwin`, `windows`), `arch`, `osVersion`, `hostname` | The product name is not in `info()`. |
| `app.on('ready')`, `whenReady` | none | Run startup code from Go host code or the first page. |
| `app.on('window-all-closed')` | The app quits when the last window closes; `native.window.keepAlive: true` keeps it running | |
| `app.on('before-quit')`, `will-quit` | none at app level. Per window: `setPreventClose(true)` and `onCloseRequested` | |
| `app.requestSingleInstanceLock()`, `second-instance` | Go `app.SingleInstance(id, args)`; `onSecondInstance` (`krate/app.events`) | TypeScript-only apps cannot take the lock directly; an app with `fileAssociations` takes it automatically. |
| `app.setAsDefaultProtocolClient`, `open-url` | `native.deepLinks` (registered by the installer); `onOpenURL` | |
| `open-file` | `native.fileAssociations`; `onOpenFile`, `openedFiles` (Go `app.OnOpenFile`) | |
| `app.setLoginItemSettings` | `setLoginItem(enabled)`, `getLoginItem()` from `@krate/app/login` | Needs the `login` permission. |
| `app.setBadgeCount`, `dock.setBadge` | `setBadge(n \| text)`, `clearBadge()` from `@krate/app/badge` | |
| `win.setProgressBar` | `setProgress(state, value)` | |
| `win.flashFrame`, `dock.bounce` | `setUrgent(on)` | |
| `app.dock.hide()` | `setDockVisible`, `setActivationPolicy` from `@krate/app/chrome`; `bundle.macos.agent` | macOS only. |
| `app.isPackaged` | none | |
| `app.commandLine.appendSwitch`, GPU flags | none | The engine is the system webview. |

### Windows and views

| Electron | Krate Native | Notes |
|----------|--------------|-------|
| `new BrowserWindow({ width, height, title, resizable, frame, alwaysOnTop, fullscreen, x, y })` | `open({ width, height, title, fixed, frameless, alwaysOnTop, fullscreen, position, path })` from `@krate/app/window` (`krate/window.open`); Go `window.Open` | `path` is the route. `fixed` is `resizable: false`. `titleBar`, `transparent`, `backdrop`, `vibrancy`, `permissions` also exist. Always-on-top and position are ignored on Wayland. |
| `win.loadFile`, `win.loadURL('file://')` | `win.navigate(path)` (`krate/window.navigate`) | App routes only. For a web page use [`@krate/app/remote`](/docs/native/remote/). |
| `win.close()`, `show()`, `hide()`, `focus()`, `minimize()`, `maximize()`, `unmaximize()` | the same names on the `Window` object (`restore()` for unmaximize); `main()`, `get(id)` | `hide({ release: true })` also frees the webview. |
| `win.setTitle`, `setSize`, `setPosition`, `setAlwaysOnTop`, `setFullScreen` | `setTitle`, `setSize`, `setPosition`, `setAlwaysOnTop`, `setFullscreen` | |
| `win.getBounds()`, `isMaximized()` | `state()` (`krate/window.state`) | No `move`/`resize` events; read the state. |
| `win.on('close')` with `preventDefault` | `setPreventClose(true)` and `onCloseRequested` | |
| window events (`focus`, `blur`, `closed`) | `onEvent`: `close-requested`, `closed`, `focus`, `blur`, `released`, `restored` | |
| `BrowserWindow.getAllWindows()` | `list()` (`krate/window.list`) | |
| `win.setMenu`, `setMenuBarVisibility`, `autoHideMenuBar` | none | One application menu for all windows. |
| `parent`, `modal`, `show: false`, `minWidth`, `maxWidth`, `backgroundColor`, `icon` per window | none | `native.icons` is the app icon. |
| `titleBarStyle`, `titleBarOverlay`, `vibrancy`, `backgroundMaterial`, `trafficLightPosition` | `native.window.titleBar`, `titleBarOverlay`, `vibrancy`, `backdrop`, `trafficLightPosition`; drag regions with `-webkit-app-region: drag` or `data-krate-drag` | [Window chrome](/docs/native/window-chrome/). |
| `WebContentsView`, `BrowserView` | `win.addView`, `setLayout`, `split`, `pane`, `focusView`, `navigateView`, `getView` | [Views](/docs/native/views/). |
| `screen.getAllDisplays()`, `getPrimaryDisplay()` | `listDisplays()`, `primaryDisplay()` from `@krate/app/screen` | |
| `screen.on('display-added' ...)` | `onDisplaysChanged` | |
| window position by display | `displayOfWindow()`, `centerWindow()` | |

### Menus, tray, shortcuts, notifications

| Electron | Krate Native | Notes |
|----------|--------------|-------|
| `Menu.setApplicationMenu` | `setApp(items)` from `@krate/app/menu`; Go `menu.SetApp` | `kind`: `normal`, `check`, `radio`, `separator`, `submenu`; `disabled`; `checked`; `accelerator`. No `role`, `icon`, `visible`, `sublabel`. |
| `MenuItem.click` | `onMenu(id, fn)`, `onClick(fn)`; Go `menu.OnItem` | |
| `Menu.popup`, `webContents.on('context-menu')` | `popup(items)`; `setContextMenu({ mode })`, `onContextMenu`, `defaultContextItems` | Modes `default`, `none`, `custom`, `extend`. Built-in actions are ids that start with `krate:`. |
| `new Tray(image)`, `setToolTip`, `setContextMenu`, `setTitle` | `tray.set({ icon, tooltip, title, menu })`, `tray.remove()`; events `click`, `middle-click`, `right-click`, `menu` | `icon` is PNG bytes. `displayBalloon`, `setImage` alone, `getBounds`: none (call `set` again). |
| `globalShortcut.register` | `register(accelerator, fn)` from `@krate/app/shortcut`, returns an unregister function | Needs `shortcut`. Wayland needs the GlobalShortcuts portal. |
| `new Notification(...)` | `send({ id, title, body, icon, actions, urgency, silent, timeoutMs })`, `close(id)`, `onAction(id, action, fn)`, `onEvent` | |
| `Notification.isSupported()` | `app.supports("notify")` (`krate/app.supports`) | Also `"tray"`, `"dialog"`, `"clipboard"`, `"shortcut"`, `"media"`, `"window:alwaysOnTop"` and more. |

### Dialogs, clipboard, shell, files

| Electron | Krate Native | Notes |
|----------|--------------|-------|
| `dialog.showOpenDialog` | `openFile(opts)`, `openFolder(opts)` from `@krate/app/dialog`; Go `dialog.OpenFile` | Returns paths, `[]` when cancelled. `multiple`, `filters` (glob patterns), `dir`, `title`, `showHidden`. Grants the paths to `fs`. |
| `dialog.showSaveDialog` | `saveFile({ name, dir, filters, overwrite })` | `null` when cancelled. |
| `dialog.showMessageBox` | `message({ kind, title, message, detail, buttons, default })` returns the button index; `confirm(title, text)` | `-1` when dismissed. No `checkboxLabel`. |
| `dialog.showErrorBox` | `message({ kind: "error", ... })` | |
| `clipboard.readText`, `writeText` | `readText()`, `writeText()` from `@krate/app/clipboard` | Reading needs `clipboard:read`. |
| `clipboard.readHTML`, `readRTF`, `write({ text, html, rtf })` | `readHtml()`, `readRtf()`, `write({ text, html, rtf })` | |
| `clipboard.readImage`, `writeImage` | `readImage()`, `writeImage(png)` | PNG bytes, not a `nativeImage`. |
| `clipboard.availableFormats`, `readBuffer`, `writeBuffer` | `formats()`, `readFormat(mime)`, `writeFormat(mime, bytes)` | |
| `clipboard.clear` | `clear()` | |
| `shell.openExternal` | `openExternal(url)` from `@krate/app/os` (`krate/os.openExternal`) | `http`, `https` and `mailto` only. Needs `shell:openExternal`. |
| `shell.openPath`, `showItemInFolder`, `trashItem`, `beep` | none | |
| `fs` in the main process | Go: the standard library. TypeScript host code: `@krate/app/fs` (`readFile`, `readText`, `readDir`, `stat`, `exists`, `writeFile`, `writeText`, `mkdir`, `remove`, `rename`). | The `fs` SDK is scoped to the app directories and dialog grants. Reads are limited to 64 MiB. |
| `safeStorage` | `secretSet`, `secretGet`, `secretDelete` from `@krate/app/os` | OS credential store (Keychain, Credential Manager, Secret Service). Needs `os:secrets` and `os:secrets.read`. |
| `electron-store` | `kv.get`, `kv.set`, `kv.delete`, `kv.keys` | |
| `better-sqlite3` | `db.exec`, `db.query`, `db.transaction` from `@krate/app/db` | SQLite. |
| `net.request`, `net.fetch` | `fetch` in the page; `net/http` in Go host code | Use `fetch` in the page or `net/http` in Go host code. TypeScript host code on QuickJS has a limited API surface ([`KR7193`](/docs/native/host-code/#other-errors) warns about APIs it lacks). The `net` permission is reserved. |
| `nativeImage` | none | Images are PNG bytes. |

### System

| Electron | Krate Native | Notes |
|----------|--------------|-------|
| `nativeTheme.shouldUseDarkColors`, `themeSource`, `updated` | `getTheme()`, `setTheme("system" \| "light" \| "dark")`, `onThemeChange` from `@krate/app/theme` | `setTheme` changes native title bars and dialogs; use `prefers-color-scheme` in CSS for the page. |
| `powerMonitor` (`suspend`, `resume`, `lock-screen`, `on-battery`, `getSystemIdleTime`) | `powerState()`, `idleTime()`, `onPower(kind, fn)`, `onPowerEvent` from `@krate/app/power` | Kinds: `suspend`, `resume`, `lock`, `unlock`, `ac`, `battery`, `idle`, `active`. |
| `powerSaveBlocker` | `preventSleep(reason, display?)`, `allowSleep(id)` | Needs `power:inhibit`. |
| `autoUpdater`, `electron-updater` | `check()`, `download(onProgress)`, `apply()`, `relaunch()`, `update()` from `@krate/app/updater` | Signed feed with delta updates, staged rollouts, channels and rollback; see [Packaging](#packaging-and-updates). |
| `crashReporter` | `@krate/app/crash`: `listCrashReports`, `readCrashReport`, `deleteCrashReport`, `onCrash`, `setCrashAutoReload` | Reports are JSON files under the app's log directory. Nothing is uploaded; sending them is the app's job. |
| `desktopCapturer.getSources` | `sources()`, `chooseSource(id)` from `@krate/app/media` | macOS and Windows list screens and windows; no engine accepts a chosen source in this release. |
| `systemPreferences.getMediaAccessStatus`, `askForMediaAccess` | `osStatus(kind)`, `osRequest(kind)` | For camera, microphone, screen and location. |
| `systemPreferences` (other keys, accent color, animations) | none | |
| `session.setPermissionRequestHandler` | `onRequest(handler)`, `getPolicy`, `setPolicy`, `native.media` from `@krate/app/media` | Covers `camera`, `microphone`, `screen`, `location`, `notifications`, `clipboardRead` for the app's pages. Remote pages are always denied. |
| `inAppPurchase`, `TouchBar`, `pushNotifications`, `net.isOnline` | none | |

### Web contents, sessions, processes

| Electron | Krate Native | Notes |
|----------|--------------|-------|
| `webContents.print`, `printToPDF` | `print()`, `printToPdf(opts)` from `@krate/app/page` | Needs `page`. |
| `webContents.findInPage`, `stopFindInPage` | `find(text, opts)`, `stopFind()`, `onFind` | |
| `setZoomFactor`, `getZoomFactor` | `setZoom(f)`, `zoom()` | |
| `capturePage` | `capture()` (PNG) | |
| `webContents.executeJavaScript`, `insertCSS` | none | Host code cannot run script in a page. Send a signal, a stream value or a view message and let the page act. (`krate native dev` has a control channel for tests; it is not an app API.) |
| `webContents.openDevTools` | F12 or Ctrl+Shift+I (Cmd+Option+I) in dev builds; `native.debug: true` keeps it in a release build | |
| `webContents.send` | host signal, host stream, `view.post` | |
| `session.cookies` | `list`, `get`, `set`, `delete`, `clearSiteData` from `@krate/app/cookies` | WebKit keeps no cookies for the app scheme, so on Linux and macOS pass an `http(s)` URL. |
| `session.on('will-download')`, `downloadItem` | `configure`, `list`, `cancel`, `onEvent`, `onDone` from `@krate/app/downloads` | |
| `session.webRequest`, `protocol.handle` | remote partitions only: `setRules`, `setUserAgent`, `setProxy` from `@krate/app/remote` | None for the app's own pages. |
| `session.fromPartition` | `partition: "persist:name"` on a remote view or window | |
| `<webview>`, loading a remote site | `addView`, `openWindow` from `@krate/app/remote` | The remote page gets no access to the app. |
| `utilityProcess.fork` | `startWorker(name)`, `native.workers` | |
| `MessageChannelMain` | `createChannel()`, `transferPort()`, `portFromId()` from `@krate/app/port` | |
| Offscreen rendering (`offscreen: true`) | `openOffscreen`, `captureFrame`, `frames` from `@krate/app/offscreen` | |
| `webContents.debugger`, `--remote-debugging-port` | `native.automation`, `@krate/app/automation` | CDP on WebView2, WebKit inspector on WebKitGTK, Safari inspection on macOS. |
| Chrome extensions | `native.extensions` | WebView2 and macOS 15.4 or newer only. |
| `ipcRenderer.sendSync`, `remote`, `@electron/remote` | none | |

## Security

Electron's advice is to turn on `contextIsolation` and `sandbox`, turn off
`nodeIntegration`, and expose a narrow bridge. In Krate Native that is how every
page runs, and the rest differs in these ways.

- **Permissions come from the code.** A build reads which `@krate/app/<area>`
  imports and `krate/...` ids your UI and TypeScript host code use, and grants
  exactly those permissions to the pages in `app.json`. Nothing in the page can
  ask for more at run time. In Electron the preload script decides, and nothing
  checks that it exposes only what the page needs. See
  [Derived permissions](/docs/native/release/#derived-permissions); `krate native
  check` prints a report of which import needs which permission.
- **`native.permissions` is an allowlist.** The build fails (`KR7210`) when the
  code needs something outside it, so a new `import` of a sensitive area (`fs`,
  `clipboard:read`, `os:secrets.read`, `shell:openExternal`, `tray`, `shortcut`)
  cannot slip in unnoticed. `fs` is not granted by default.
- **Per-window and per-view scopes.** `native.window.permissions`, the
  `permissions` option of `open()` and of `addView()` narrow what one window's or
  view's page may call. The shell identifies the calling webview from the
  platform, not from anything the page sends, so a narrow page cannot claim a
  wide window's id. A window can never open a wider one. See [Per-window
  scopes](/docs/native/release/#per-window-scopes).
- **Origin lock.** An app view is never navigated off the app origin. Web pages go
  in [remote views](/docs/native/remote/), which have their own storage, no
  bridge, an empty permission scope, an origin allowlist for navigation, denied
  permissions and cancelled downloads. In Electron the same page sits in the same
  kind of renderer as your app and you build those rules yourself with
  `will-navigate`, `setWindowOpenHandler` and `session` handlers.
- **The `fs` scope.** Page file access is limited to the app directories and
  paths the user chose in a dialog. Host code is trusted and not scoped.
- **Limits that remain.** Every window and view of the app shares one origin, so
  their `localStorage` and cookies are shared and a scope is not an isolation
  boundary for content you do not trust. The page's Content Security Policy
  forbids `eval`. In browser mode (`krate native dev --browser`) scopes only
  catch mistakes. Go host code and TypeScript host code are not permission-gated;
  review them like Electron's main process.

## Packaging and updates

| Electron | Krate Native |
|----------|--------------|
| `electron-builder` or Forge config (`build`, `forge.config.js`) | `native` in `krate.config.ts`: `appId`, `productName`, `version`, `icons`, `bundle`, `deepLinks`, `fileAssociations`, `updater` |
| `electron-builder` (all targets), `electron-forge make` | `krate native package [--target <os/arch>] [--format <list>]` |
| NSIS | `bundle.windows.installer: "nsis"` (needs `makensis`) |
| MSIX / AppX | `"msix"`, and `msix-store` for the Microsoft Store |
| dmg, zip, mas | `dmg` (macOS only), `zip`, `--target mas` |
| deb, rpm, AppImage, snap, flatpak, tar.gz | `bundle.linux.formats` / `--format` (AppImage needs `appimagetool`) |
| Universal macOS build | `--target darwin/universal` or `--arch universal` |
| `.icns` / `.ico` from a source image | `native.icons` (one PNG; the packager writes the rest) |
| asar | none; the app directory is copied as is, or embedded in a [release executable](/docs/native/release/) |
| Bundled Chromium and Node | none: the system webview and a Go shell. A release is one executable per platform with the app embedded; the shell binary alone is about 16 MB on Linux, 11 MB stripped (see [Getting started](/docs/native/getting-started/#footprint-linux-measured)) |
| `CSC_LINK`, `WIN_CSC_LINK`, `notarize` | `krate native sign` and `notarize`, `KRATE_MACOS_IDENTITY`, `KRATE_WINDOWS_PFX`, `KRATE_GPG_KEY`, notary credentials ([Signing](/docs/native/signing/)) |
| `latest.yml`, `electron-updater` publish providers | A JSON feed you host anywhere over https (`latest-{target}.json`, written by `package` with `--update-base-url`) and an Ed25519 key from `krate native keygen` |
| Differential updates (blockmap) | `--delta-from <dir>`: signed bsdiff patches against earlier releases, with fallback to the full download |
| Staged rollout | `--rollout`, `krate native rollout`; `channel` |

Differences to plan for:

- **The webview is a system component.** On Windows the installer checks for the
  WebView2 Runtime and can download, embed or fix it
  (`bundle.windows.webview2`). On Linux the machine needs WebKitGTK 4.1
  (2.40 or newer). `krate native doctor` reports all of this.
- **Code signing and the update signature are two things.** The platform
  certificate makes the OS trust the app; the updater key makes the app trust the
  update. You need both, and Krate does not sign for you without your keys.
- **Stores forbid self-updating.** Under Flatpak, Snap, the Mac App Store and the
  Microsoft Store the `updater` calls report the store's own mechanism.
- **Building.** `krate native build` runs without a Go toolchain when a shell is
  available and the app has no Go host code or plugins; Go host code, plugins and
  `--release` single-file executables need Go. Cross-compiling uses
  `CGO_ENABLED=0`, so you can build every platform from one machine, apart from
  formats that need a platform tool (`dmg`, `makensis`, `appimagetool`).

## Testing

| Electron with Playwright | Krate Native |
|--------------------------|--------------|
| `_electron.launch({ args })` | `launch()` from `@krate/app/driver`; `krate native test` builds the app first |
| `app.firstWindow()` | `app.page()` (the `main` window) |
| `app.windows()` | `app.windows()`, `app.views()`, `app.waitForView(id)` |
| `page.fill`, `click`, `press`, `type` | the same names (DOM events, not OS input) |
| `page.locator(...).textContent()` | `page.text(selector)`, `page.value(selector)` |
| `expect(...).toHaveText` and auto-waiting | `page.waitFor(selector \| predicate)`, `poll(fn)`; Node's `assert` |
| `page.evaluate` | `page.evaluate(fn, ...args)` (the function is serialized) |
| `electronApp.evaluate(({ app }) => ...)` | `page.call("krate/os.appDataDir")`, `page.call("src/lib/store.host.loadDraft")`: any host function or SDK id |
| Mocking `dialog.showOpenDialog` | A dialog cannot be clicked; call the code behind it (the example exposes `loadFile` and `saveFile` as a test hook) |
| Clicking the menu or tray | `app.emit.menu(id)`, `app.emit.tray(kind, id)`, `app.emit.shortcut(accelerator)` |
| `page.screenshot()` | Not available yet |
| Test runner | Node's built-in `node:test` |
| Console and page errors | `page.errors()`, `app.logs()` |

Each `launch()` starts the real shell with fresh data, config and cache
directories, so tests never see your real settings. Input is synthesized in the
page (`isTrusted` is false), native dialogs and notifications are not operated,
and a notification can be asserted only as "sent" or "unsupported". CI needs a
display (`xvfb-run -a krate native test` on Linux; the macOS and Windows hosted
runners have one). See [Testing your app](/docs/native/testing/).

## Debugging

| Electron | Krate Native |
|----------|--------------|
| `electron .` with reload | `krate native dev`: hot reload of pages, in-place reload of TypeScript host code, a restart of the Go host child on `.go` edits |
| DevTools in each window | F12 or Ctrl+Shift+I (Cmd+Option+I on macOS), per window and per view; on in dev, off in release unless `native.debug` |
| `--inspect` for the main process | `krate native dev --debug-host` (Node, Bun, Deno host children); `--debug-go` runs the Go host under Delve (`--debug-config vscode` writes the launch config) |
| Main-process `console.log` | The terminal of `krate native dev` and the app log; page errors from every window are printed with the window or view id |
| Opening the app in a browser | `krate native dev --browser` (scopes are not a boundary there) |
| Crash reports | `@krate/app/crash` |
| Checking the machine | `krate native doctor`; the dev overlay shows build and page errors, and the dev Memory panel shows memory |

See [Development](/docs/native/dev/).

## Things that are different

- **System webviews, not one Chromium.** WebKitGTK, WKWebView and WebView2 differ
  in CSS and JavaScript support, in fonts and scrolling, and ship on the user's
  schedule. Test on all three. Features that depend on Chromium internals
  (`chrome.*` APIs, some codecs, DevTools protocol access from the page) are not
  there. WebKit has no `MessagePort` sharing between webviews, which is why
  [ports](/docs/native/processes/#message-ports) are shell-backed.
- **No Node in the page, and no Node by default in host code.** TypeScript host
  code runs on QuickJS unless you choose `node`, `bun` or `deno`. Many npm
  packages that touch `fs`, `path` or `child_process` belong in Go host code, in a
  worker or in a Node host child.
- **No `require`, no `remote`, no synchronous IPC.** Every host call is
  asynchronous. Host calls never run at build time: during prerendering a host
  call or signal read yields no value, and a top-level `await` of one is `KR7102`.
- **Prerendered pages, no server.** SSR, API routes, middleware and server
  actions are errors in a native build. Move per-request work into host code.
- **Routing.** Use `<Link>` for navigation. A plain `<a href>` is a full page
  load. Dynamic routes are prerendered as a shell that matches at run time.
- **Startup and size.** Cold start to a visible window is about 0.2 s and a
  release executable about 16 MB (10.9 MB stripped) on the Linux test machine;
  memory is mostly the system webview. These are order-of-magnitude figures; see
  [Getting started](/docs/native/getting-started/#footprint-linux-measured).
- **Window lifetime.** The app quits when the last window closes, unless
  `native.window.keepAlive` is set. The tray exists as long as it is set.
- **Platform status.** Linux (WebKitGTK) is the most tested platform. The macOS
  and Windows implementations compile and have GUI tests in the CI native job;
  check the [compatibility matrix](/docs/guides/compatibility/#krate-native)
  for what each feature has been tested with.
- **No Electron-only modules.** `ipcMain`, `BrowserWindow`, `contextBridge`,
  `nativeImage`, `protocol`, `Menu` and the rest do not exist at all. Search your
  code for `require('electron')` and `from 'electron'`: every hit is a row in the
  tables above or a "none".

## Migration checklist

1. Run `krate native init` and move your UI into `src/pages`.
2. List every `ipcMain.handle`/`ipcMain.on` channel and write a host function for
   each; delete the preload script.
3. Replace each `webContents.send` with a host signal, stream or view message.
4. Move startup code (menu, tray, shortcuts) to the first page or to Go host code.
5. Replace Node `fs`, `path`, `os` in the main process with `@krate/app/fs`, the
   directory functions, or Go.
6. Move settings to `kv` or `db`.
7. Run `krate native check`, set `native.permissions`, fix any `KR72xx` finding.
8. Port the Playwright tests to `krate native test`.
9. Set up `krate native package`, signing and the update feed.
10. Test on all three operating systems.
