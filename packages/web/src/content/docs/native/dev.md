---
title: Development with krate native dev
order: 8
description: Run a Krate Native app with hot reload - Go host restarts, TypeScript host reloads, UI HMR, dev overlay and the host inspector.
---

# Development with `krate native dev`

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

```sh
krate native dev [dir] [--browser] [--port <n>] [--shell <path>]
```

It builds the app directory into `.krate/native/dev/app` (the manifest has
`debug: true`, so the webview's developer tools are on), starts the shell with
`krate-shell --dev`, and watches the project.

## On macOS: a dev app bundle

macOS gives a process a bundle identifier (which notifications need, and which
names the app in the menu bar, the Dock and the notification settings) only
when it runs from an `.app` bundle. With a window (not `--browser`),
`krate native dev` on macOS therefore copies the shell into a minimal bundle
at `.krate/dev/<productName>.app` with an `Info.plist` whose identifier is
`<appId>.dev`, signs it ad hoc when `codesign` is on the PATH, and starts the
shell from there. The bundle is refreshed when the shell or the app id
changes. If it cannot be created the shell runs unbundled and says so;
notifications then return an `ErrUnsupported` error that explains why. CI
tests the bundle creation and the notification code on macOS; whether a banner
appears from a dev bundle is a manual check.

## Two processes

The prebuilt shell owns the window. Your Go code and the generated glue run in
a Go host child process of the shell. `krate native dev` itself is a third,
separate process that builds and tells the shell what to do over the shell's
stdin (`reload-hostjs`, `restart-gohost`, `hmr <json>`, `quit`).

| You edit | What happens |
| --- | --- |
| a `.go` file or `go.mod` | the glue is regenerated, the Go host binary is rebuilt, the shell restarts only that child. The window, scroll position and UI state stay; host signals resend their current values on reconnect. |
| TypeScript host code (`*.host.ts`, `@host` functions, files the host bundle imports) | the host bundle is rebuilt and re-evaluated in place in QuickJS. |
| a page, component or stylesheet | the web dev server's incremental rebuild runs and the page reloads (CSS-only edits swap the stylesheet without a reload). |
| `krate.config.*`, `tsconfig.json`, `.env*` | everything is rebuilt and the shell is restarted (the window closes and reopens). |

If an edit changes what the UI can call (a new function, another signature),
the UI is rebuilt completely.

## Errors

Build errors print in the terminal and show in the dev overlay in the window.

Errors in the page reach the terminal too, from every window and view. The dev
overlay catches an uncaught error, an unhandled promise rejection, a
`console.error` call and a script or stylesheet that fails to load, including
those that happen while the page starts, and the shell prints each one with the
id of the window or view it came from (`main`, or the id you gave `addView`):

```text
[webview] main: Error: boom
[webview]     @src/pages/index.tsx:16:34
[webview]     click@[native code]
[webview] sidebar: unhandled rejection: fetch failed
```

The overlay in the window lists the same errors. The stack is mapped to your
source through the dev build's source maps where the compiler recorded a
position: event handlers and expression bindings of a page. Other frames, such
as the body of an effect or a timer callback, print the generated file and line
(`index.v0hgz6.js:1046:18`). The shell takes the window id from the webview the
report came from, not from the page.

If a page shows nothing or stays on its first state and the terminal says
nothing, query the page: the runtime keeps the errors it caught in
`globalThis.__krateErrs`.
The session keeps running with the last good host code; the next good build
clears the error. A TypeScript host error reaches the UI as a `KrateError`
whose `data.stack` is the stack mapped through the source map (`file:line`);
the overlay shows it as "Host stack" when the error is unhandled.

A failing first build ends the command; fix it and run again.

## How HMR reaches the window

The app origin serves `GET /__krate/hmr` in dev: a stream of frames
`{"hmr": <message>}` (see the host call protocol). The dev overlay in the page
reads it with a streaming `fetch`; it uses the stream instead of `EventSource`
when the build is a native dev build, or the origin is `krate://app` or
`https://app.localhost`. The CLI turns its hub events into messages and hands
them to the shell as `hmr <json>` control lines, which the shell fans out to
every open stream. No extra port or token is involved, so it works the same in
window and browser mode. Pages are served with `Cache-Control: no-cache`.

## Browser mode

`--browser` opens no window. The shell serves the app on `127.0.0.1` (the port
from `--port`, default any free port) and the command prints a tokenized URL.
Nothing is opened automatically. This is meant for Playwright and browser
devtools.

## Developer tools

The webview's own inspector (elements, console, network, sources) is on in
`krate native dev` on every platform, and off in a release build unless the
config sets `native: { debug: true }`. Open it from the page with:

| Platform | Shortcut | Also |
| --- | --- | --- |
| Linux (WebKitGTK) | F12 or Ctrl+Shift+I | right-click, Inspect Element |
| Windows (WebView2) | F12 or Ctrl+Shift+I | right-click, Inspect |
| macOS (WKWebView) | Cmd+Option+I (F12 also works) | right-click, Inspect Element; Safari's Develop menu lists the app on macOS 13.3 and later |

The shell adds the shortcut script to the pages of a build with the tools on, and
opens the inspector of the window or view that had the focus. On macOS it uses
WebKit's `_inspector` accessor, which is not public API; if a macOS release
removes it the shortcut logs a line and the context menu and Safari keep
working. Each window and each view has its own inspector.

With the tools off (a release build without `native.debug`), the shortcut script
is not added, the endpoint it posts to does not exist and the platform's
developer tools are disabled.

## Host inspector

The shell records host traffic in dev: calls (function, duration, error code
and message), stream open and end, signal subscriptions and updates, the newest
500. The overlay toolbar has a "Host" button with the list; the raw feed is
`GET /__krate/dev/events` (add `?follow=1` to stream). Arguments and returned
values are not recorded.

## Memory panel

The toolbar also has a "Memory" button. Its panel shows PSS and private dirty
per process (from `GET /__krate/dev/memory`), the DOM node count and, where the
engine exposes it, the JS heap. It samples every 2 seconds while open and not at
all when closed. See [Memory](/docs/native/memory/#the-dev-memory-panel).

## Finding the shell

In order:

1. `--shell <path>`;
2. `$KRATE_SHELL`;
3. a `krate-shell` (`krate-shell.exe` on Windows) in the directory of the
   running `krate` executable, with symlinks resolved. The
   `@krate/core-<platform>` npm packages put the shell there, next to `krate`,
   so an installed `@krate/core` needs no setup;
4. inside a Krate checkout, a build of `packages/native/cmd/krate-shell` kept in
   `.krate/native/bin` and rebuilt when its sources are newer.

With none of these `krate native dev` stops and lists them. `krate native doctor`
shows which shell was found and whether the webview it needs is installed.

## Limitations

- The shell restarts for a new dynamic route (the fallback table is read at start).
- On Windows the Go host binary cannot be replaced while it runs; a Go edit reports that.
- Page edits reload the page; there is no component-level state-preserving HMR.
- The "Open in editor" button of the web overlay is not available.

## Plugins

An app with [native plugins](/docs/native/plugins/) runs under a shell that
`krate native dev` builds for it with the Go toolchain (cached in
`.krate/native/bin`), instead of the prebuilt `krate-shell`; `--shell` and
`$KRATE_SHELL` are refused. Every plugin op is linked in dev. Editing the Go code,
`go.mod` or manifest of a plugin inside the project restarts the session, because
plugins are linked into the shell and not into the Go host child.

## Testing

`krate native test` runs your own tests against the app; see
[Testing your app](/docs/native/testing/).
