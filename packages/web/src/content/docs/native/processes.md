---
title: Workers, Ports, Offscreen and Automation
order: 21
description: Worker processes, message ports, offscreen frames, DevTools automation and browser extensions in Krate Native, with what each system webview allows.
---

# Workers, ports, offscreen rendering, automation and extensions

> **Preview.** Part of [Krate Native](/docs/native/). The Linux (WebKitGTK)
> paths are tested end to end through the real shell. The macOS (WKWebView) and
> Windows (WebView2) paths are written and compiled, the vtable slots and
> interface ids of WebView2 are checked against `WebView2.h`, and their GUI
> tests run in the CI native job; nothing here has been confirmed on a real Mac
> or Windows machine by the author.

These are Electron's advanced process and automation features, where the system
webviews allow them. Where they do not, the page says so.

| Electron | Krate Native | Linux | macOS | Windows |
| --- | --- | --- | --- | --- |
| `utilityProcess.fork` | workers (`@krate/app/worker`) | yes | yes | yes |
| `MessageChannelMain` | ports (`@krate/app/port`) | yes | yes | yes |
| offscreen rendering | offscreen windows and frames (`@krate/app/offscreen`) | yes | yes | yes |
| remote debugging, `webContents.debugger` | automation (`@krate/app/automation`) | WebKit inspector over HTTP | Safari inspection | CDP |
| extensions | `native.extensions` | no | macOS 15.4+ | yes |

Permissions: `worker`, `port` (implies `port:receive`), `port:receive`,
`offscreen`, `automation` and `extension`. The build derives them from the
modules the code imports.

## Workers

A worker is a separate process started by name. Declare it in `native.workers`:

```ts
native: {
  workers: {
    // A TypeScript module on Node, Bun or Deno (default node).
    indexer: { entry: 'src/workers/indexer.ts', permissions: ['kv'], restart: { policy: 'onFailure' } },
    // A Go main package that calls worker.Serve, or a prebuilt executable.
    crunch: { go: './cmd/crunch' },
  },
}
```

From a page or TypeScript host code:

```ts
import { startWorker } from '@krate/app/worker';

const w = await startWorker('indexer');
w.onmessage = (data) => console.log('indexed', data);
w.onexit = ({ code, crashed }) => console.log('exited', code, crashed);
await w.postMessage({ path: '/home/me/notes' });
await w.terminate();
```

Inside the worker:

```ts
import { onMessage, postMessage } from '@krate/app/worker-host';
onMessage(async (job) => { await postMessage({ done: job }); });
```

A Go worker is `worker.Serve(func(m worker.Message) { ... worker.Emit(v) })`
from `github.com/kratejs/krate/packages/native/sdk/worker`; Go host code starts
workers with `worker.Start(name, nil)`, `worker.Post`, `worker.OnEvent`.

What a worker gets:

- **The same transport as host children.** A worker serves the host child
  protocol (a unix socket, or loopback TCP with a token for JavaScript on
  Windows) and starts and stops like any host child.
- **Its own, narrower SDK.** A worker calls the SDK through an endpoint that
  carries its identity, and may use only the permissions listed in its
  declaration, which must be a subset of the app's. A worker that lists nothing
  can call nothing but its own messaging.
- **Logs.** Its stdout and stderr go to the app log, prefixed with
  `[worker <name>]`.
- **A restart policy.** `never` (default), `onFailure` (a crash or non-zero
  exit) or `always`. The delay starts at `backoffMs` (200) and doubles to
  10 seconds; `max` (5) restarts in a row mark the worker `failed`; a run longer
  than 30 seconds resets the count. Events: `started`, `exit`, `crash`,
  `restarting`, `stopped`, `failed`, `message`.
- **Messages** over the wire codec: JSON, `Uint8Array`, `Date`, `Map`, `Set`.

A release build copies worker scripts to `host/workers` next to the executable.
A TypeScript worker needs Node, Bun or Deno on the user's machine; use a Go or
`exec` worker for a self-contained app. A Go worker is compiled for the build
machine's platform.

Ids: `krate/worker.start`, `krate/worker.stop`, `krate/worker.list`,
`krate/worker.specs`, `krate/worker.post`, `krate/worker.events` (stream),
`krate/worker.emit` (worker to app).

## Message ports

A channel has two ends. Hand each to a window, view or worker; messages then go
straight between the ends without host code relaying them.

```ts
import { createChannel, transferPort, portFromId } from '@krate/app/port';

const { port1, port2 } = await createChannel();
await transferPort(port2, 'peer');         // a view id, or `worker:${w.id}`
await peerView.post({ port: port2.id });   // tell it which end it holds
port1.onmessage = (e) => console.log(e.data);
await port1.postMessage({ hello: 'peer' });

// In the peer: const port = portFromId(id); port.onmessage = ...
```

- Only the holder of an end can post, listen or close it. Host code is trusted
  and may act on any end.
- An end can only be handed to a scope that has `port:receive`; otherwise
  `transferPort` fails with `permission_denied`. `port` (create channels,
  transfer) implies `port:receive`.
- Messages queue for an end nobody listens on yet, up to 1024 messages or
  16 MiB; past that `postMessage` fails with `port_full` (backpressure). One
  message is limited to 8 MiB.
- `close()` closes both ends after the other side has read what was queued. A
  channel also closes when its holder (window or worker) goes away.

**Honest limit.** The engines give two top-level webviews no way to share a real
`MessagePort`, so a `Port` here is an object backed by the shell. Inside one page
the browser's own `MessageChannel` is untouched. Go host code has
`port.Channel`, `port.Transfer`, `port.Post`, `port.PostBytes`, `port.Listen`.

Ids: `krate/port.channel`, `krate/port.transfer`, `krate/port.post`,
`krate/port.listen` (stream), `krate/port.close`.

## Offscreen rendering

No system webview streams frames like Chromium's offscreen mode. The closest
honest equivalent: a window that is never shown, plus snapshots.

```ts
import { openOffscreen, frames, captureFrame } from '@krate/app/offscreen';

const id = await openOffscreen({ path: '/report', width: 1280, height: 720 });
const png = await captureFrame(id);                       // one PNG
for await (const f of frames(id, { fps: 15, format: 'bgra' })) {
  consume(f.data, f.width, f.height, f.dropped);          // premultiplied BGRA
}
```

- Linux: a `GtkOffscreenWindow` and `webkit_web_view_get_snapshot`. macOS and
  Windows: a window parked far outside every display (never activated, off the
  taskbar) and `WKWebView.takeSnapshot` or WebView2 `CapturePreview`.
  `captureFrame` also works on any visible window or view.
- Frames are PNG or raw premultiplied BGRA (`width * 4` bytes a row). A stream
  takes one frame at a time and the next only after the consumer returned, so a
  slow consumer skips frames; `dropped` counts them. Up to 60 frames a second.
- This is polling, not a paint callback. Measured on Linux (WebKitGTK 2.54,
  software rendering, a loaded 12-core CI-class machine, snapshot call only, no
  encoding): about 1100 snapshots a second of a static 320x200 page and about
  230 a second of an animated 800x600 canvas. PNG encoding and your consumer
  come on top; expect well under the 60 frame cap for large PNG frames.
- An open offscreen window counts as a window; close it with `window.close`.

Ids: `krate/offscreen.open`, `krate/offscreen.capture`, `krate/offscreen.frames`
(stream).

## Automation

An endpoint for tests and debugging sessions. It exists only in
`krate native dev` and in builds with `native.debug`; a release build without it
ignores `native.automation` and the ids answer `unsupported`.

```ts
native: { debug: true, automation: { port: 9222 } }
```

The endpoint is printed when the app starts (`automation endpoint (cdp): http://127.0.0.1:9222`)
and returned by `automationEndpoint()`.

| Engine | Endpoint | Playwright, Puppeteer |
| --- | --- | --- |
| WebView2 | Chrome DevTools Protocol on `--remote-debugging-port` | attach: `chromium.connectOverCDP('http://127.0.0.1:9222')`, `puppeteer.connect({ browserURL })` |
| WebKitGTK | WebKit remote inspector over HTTP (`WEBKIT_INSPECTOR_HTTP_SERVER`): open it in a browser | cannot attach (not CDP) |
| WKWebView | `isInspectable`: Safari's Develop menu, no network endpoint | cannot attach; `safaridriver` drives Safari, not an embedded view |

On WebView2, `cdp('Page.captureScreenshot', { format: 'png' })` sends one protocol
method in process (`CallDevToolsProtocolMethod`) and `cdpEvents('Page.loadEventFired')`
subscribes to an event; elsewhere both reject with code `unsupported`. Microsoft
documents `--remote-debugging-port` with Edge WebDriver's `DebuggerAddress` for
WebView2 apps; `connectOverCDP` against the same port is ordinary Chromium CDP,
but was not run by the author.

Ids: `krate/automation.endpoint`, `krate/automation.cdp`,
`krate/automation.cdpEvents` (stream).

## Browser extensions

```ts
native: { extensions: ['./extensions/dark-mode'] }
```

The folders (each with a `manifest.json`) are packaged under `extensions/` and
loaded when the app starts. A page can list, reload by declared name and remove
them with `@krate/app/extension`; it cannot name a path.

- Windows: `ICoreWebView2Profile7::AddBrowserExtension` with
  `AreBrowserExtensionsEnabled`. Needs a runtime that has the interface.
- macOS 15.4 and later: `WKWebExtension` and `WKWebExtensionController`. The
  API's match patterns cover http, https and file pages, so a content script is
  not expected to run on the app's own `krate://app` pages. Older systems answer
  `unsupported`.
- Linux: WebKitGTK has no extension API; every call answers `unsupported`.

Ids: `krate/extension.declared`, `krate/extension.list`, `krate/extension.load`,
`krate/extension.remove`.

## Testing

`examples/native-procs` runs all of this through the real shell (worker, a port
between two views, a port to the worker, offscreen frames, the automation and
extension calls); `TestNativeE2EProcsGUI` drives it with `krate native test`'s
control channel. Pure logic (the restart policy, the port broker, frame
encoding) has unit tests that run on every OS.
