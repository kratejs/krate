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

## Host inspector

The shell records host traffic in dev: calls (function, duration, error code
and message), stream open and end, signal subscriptions and updates, the newest
500. The overlay toolbar has a "Host" button with the list; the raw feed is
`GET /__krate/dev/events` (add `?follow=1` to stream). Arguments and returned
values are not recorded.

## Finding the shell

In order: `--shell <path>`, `$KRATE_SHELL`, and, inside a Krate checkout, a
build of `packages/native/cmd/krate-shell` kept in `.krate/native/bin` and
rebuilt when its sources are newer. Published `@krate/core` packages will ship
the shell later; until then outside a checkout pass `--shell`.

## Limitations

- The shell restarts for a new dynamic route (the fallback table is read at start).
- On Windows the Go host binary cannot be replaced while it runs; a Go edit reports that.
- Page edits reload the page; there is no component-level state-preserving HMR.
- The "Open in editor" button of the web overlay is not available.
