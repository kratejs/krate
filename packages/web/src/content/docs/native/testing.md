---
title: Testing your app
order: 20
description: krate native test runs your app in the real shell and drives its pages and native events from Node tests, headless on Linux CI.
---

# Testing your app

> **Preview.** Part of [Krate Native](/docs/native/). The driver and the runner
> are exercised on Linux (WebKitGTK). The macOS and Windows code paths compile
> and run in the CI jobs of this repository, but have not been used by hand.

`krate native test` is to a native app what Playwright is to an Electron app:
your tests start the real app, drive its pages, call its host functions and
fire the native events (tray, menu, shortcut) its code listens for.

```sh
krate native test                  # every *.native.test.ts in the project
krate native test tasks            # files whose path contains "tasks"
krate native test tests/ui --grep "adds"
krate native test --list           # show which files it would run
```

It builds the app the way `krate native dev` does into `.krate/native/test/app`,
bundles the test files (TypeScript is stripped, relative imports are bundled,
packages stay imports), and runs them with Node's built-in test runner
(`node --test`, Node.js 20.10 or newer). There is nothing to install besides
`@krate/app`, which ships the driver.

## A test

```ts
// tests/tasks.native.test.ts
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { launch, type App, type Page } from "@krate/app/driver";

let app: App;
let page: Page;

before(async () => {
  app = await launch();           // starts the shell on the built app
  page = app.page();              // the main window's page
  await page.waitFor("#add");
});
after(async () => { await app?.close(); });

test("adds a task", async () => {
  await page.fill("#title", "Buy milk");
  await page.click("#add");
  await page.waitFor(() => document.querySelectorAll("#list li").length === 2);
  assert.equal(await page.text("#open"), "Open tasks: 2");
});
```

`examples/native-tasks/tests/` and `examples/native-views/tests/` have working
suites (a form, a host error, host calls; several views and a menu event).

Each `launch()` starts its own shell process with a fresh data, config and cache
directory (set `isolate: false` to use your real ones), so a test file never sees
another's database or key-value store. Files run one at a time because they open
real windows; `--workers <n>` runs several.

## Flags

| Flag | Meaning |
| --- | --- |
| `[patterns...]` | Files, directories, globs (`tests/**/*.native.test.ts`) or path fragments. Default: every `*.native.test.{ts,mts,js,mjs}` outside `node_modules`, `dist*` and dot directories. |
| `--grep <regexp>` | Run only tests whose name matches (`node --test-name-pattern`). |
| `--workers <n>` | Test files run at once (default 1). |
| `--list` | Print the test files and stop. |
| `--shell <path>` | The shell binary, as for `krate native dev`. An app that uses native plugins runs under a shell built for it. |

The first argument is the project directory when it holds a `krate.config.*`
file. The exit code is Node's: 0 when every test passed.

## `launch(options)`

Under `krate native test` the shell and the built app come from
`KRATE_TEST_SHELL` and `KRATE_TEST_APP`. Outside it pass `shell` and `appDir`.

| Option | Default | |
| --- | --- | --- |
| `shell`, `appDir` | the environment | Shell binary and built app directory. |
| `env` | | Extra environment for the shell and the host code. |
| `isolate` | `true` | Empty data, config and cache directories (`XDG_*` on Linux, `APPDATA` and `LOCALAPPDATA` on Windows, `HOME` on macOS). Removed on `close()`. |
| `startTimeout` | 60000 | Milliseconds to wait for the app to start and its main page to finish loading; `launch()` resolves only then, so a first `waitFor` does not spend its own timeout on a cold start. |
| `timeout` | 5000 | Default for `waitFor` and the actions that wait. |

## The app

| | |
| --- | --- |
| `app.page(viewId = "main")` | A `Page` for a view. |
| `app.window(id = "main")` | An `AppWindow`: `views()` and `view(id = id)` (the primary view by default). |
| `app.views()`, `app.windows()` | What is open now: each view has `id`, `window` and `primary`; a window's primary view has the window's id. |
| `app.waitForView(id)`, `app.waitForViewCount(n)` | Wait for a window or view the app opens. |
| `app.emit.tray(kind = "click", id)` | A tray event (`click`, `middle-click`, `right-click`, or `menu` with an item id). |
| `app.emit.menu(id, { window, checked })` | A menu item use. |
| `app.emit.shortcut(accelerator)` | The global shortcut the app registered for that accelerator, written as the app wrote it (`"CmdOrCtrl+Shift+K"`). |
| `app.logs()` | The last 500 lines the shell and the host code wrote to stderr. |
| `app.close()` | Quits the app and removes the isolated directories. |
| `poll(fn, { timeout, interval })` | Calls `fn` in the test process until it returns something truthy; for conditions that mix pages and host events. |

## A page

| | |
| --- | --- |
| `evaluate(fn \| expression, ...args)` | Runs a function in the page and returns its JSON value (a promise is awaited). The function is serialized: it cannot use variables of the test, pass them as arguments. A string is an expression. |
| `click(selector)` | Waits until the first match is visible and enabled, then dispatches pointer and mouse events and a click. |
| `fill(selector, text)` | Sets a text field, textarea, select or editable element and fires `input` and `change`. |
| `type(selector, text)`, `press(key, { selector })` | Key events: `"Enter"`, `"Escape"`, `"Tab"`, `"a"`, `"Control+a"`. The common default actions are done too: text insertion, Backspace, Enter submitting a form or pressing a button, Space on buttons, Tab moving focus. |
| `text(selector)`, `value(selector)` | `textContent` of an element; the value of an input. Both wait for the element. |
| `exists(selector)`, `count(selector)` | Right now, no waiting. |
| `waitFor(selector \| predicate, { timeout, interval })` | Waits for a match, or for a predicate (run in the page) to be truthy and returns its value. Times out with the reason (no element, not visible, disabled, page not ready). |
| `url()`, `title()` | The page's address and title. |
| `errors()` | Errors the page threw: uncaught errors and unhandled rejections from the first line of the document, and errors the Krate runtime caught. Assert it is empty. |
| `call(fn, ...args)` | Calls a host function as the page would: `"src/lib/tasks.List"` (the module id and export name), or an SDK id such as `"krate/kv.get"`. Plain JSON arguments and results, no streams. Rejects with a `HostCallError` carrying the `code`. |
| `screenshot()` | Not available yet: the platform layer has no webview capture API. It rejects with that message; assert on the DOM instead. |

## Native events and app logic

`app.emit` publishes to the same event streams the platform feeds, so host code
and pages see an injected event as they would a real one. Use it to test what
happens when the tray is clicked or a menu item is used without anyone at the
machine:

```ts
await app.emit.menu("file.new");
await page.waitFor("#untitled");
```

A shortcut press needs the shortcut to be registered, and the operating system
must have accepted the registration (a session that refuses global shortcuts,
such as Wayland without the GlobalShortcuts portal, makes the app's own
`register` fail). Calling any SDK id as the app would is `page.call`.

## What it does not do

- **No operating-system input.** Clicks, typing and key presses are DOM events
  dispatched in the page. They exercise your handlers, not the webview's
  hit-testing, focus rules or IME, and `isTrusted` is false. A control that
  checks for it will not react.
- **No OS-level native UI.** Native dialogs, the real tray icon, menu bars and
  notifications are not clicked; fake their events with `app.emit` and call the
  SDK from a test where you need the result.
- **No screenshots yet** (see above).
- **No `--browser` mode.** The tests drive the native shell. Use
  `krate native dev --browser` to look at an app in a browser.
- A script that runs for a long time inside the page holds the control channel;
  keep predicates short. A page that never answers fails after 45 seconds.
- Windows opened by the app appear in `app.views()` as it opens them; a test
  that needs one waits with `app.waitForView(id)`.
- The Content Security Policy of the page forbids `eval`, so `evaluate` passes
  your function as code, not as a string it compiles; it cannot call `new
  Function` either.

The control channel behind all this is documented in
[PROTOCOL.md](https://github.com/kratejs/krate/blob/main/packages/native/PROTOCOL.md)
("Dev control channel"): `views`, `query-in <view> <js>` and
`emit <area> <json>` next to the older `query`, so you can drive an app from
any language.

## Continuous integration

The tests open real windows, so the machine needs a desktop session.

- **Linux.** Run under a virtual display and install the webview library
  (`libwebkit2gtk-4.1-0` on Debian and Ubuntu):

  ```yaml
  - run: sudo apt-get install -y xvfb libwebkit2gtk-4.1-0
  - run: xvfb-run -a krate native test
  ```

  Without `$DISPLAY` or `$WAYLAND_DISPLAY` the command stops at once and says so.
- **macOS and Windows.** The hosted runners (`macos-latest`, `windows-latest`)
  have an interactive desktop; run `krate native test` directly. Windows needs
  the WebView2 runtime, which hosted runners include.
- The Go host code is compiled during the build, so the job needs a Go toolchain
  when the app has Go host code.
