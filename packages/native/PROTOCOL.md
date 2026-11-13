# Host call protocol

The webview and the host talk over one HTTP-shaped channel on the app origin
(`krate://app` on macOS and Linux, `https://app.localhost` on Windows). In
release builds the platform layer hands each request to an `http.Handler` in
process; there is no TCP listener. `krate native dev --browser` serves the same
handler on a loopback port.

## Frames

Every body below is a sequence of frames. A frame is:

```
u32 big-endian length N | N bytes JSON | u16 big-endian blob count K | K x (u32 big-endian length M | M bytes)
```

Binary values never appear inside the JSON; the blob list carries their bytes.
Plain JSON values (objects, arrays, strings, numbers, booleans, null) are sent
as is. Everything else is a tagged value: a JSON object whose ONLY key is one of
four reserved tags.

| JSON | Value |
| --- | --- |
| `{"$b": i}` | binary value (`[]byte` / `Uint8Array`); `i` indexes the frame's blob list |
| `{"$d": "2026-11-11T07:23:15.123Z"}` | `time.Time` / `Date` |
| `{"$m": [[k, v], ...]}` | `Map` (entries in order, keys of any kind) |
| `{"$s": [v, ...]}` | `Set` |

### Escapes

The reserved keys are `$b`, `$d`, `$m` and `$s`. For each tag `T` in that set:

- An object with several keys is always a plain object, even if one key is a tag
  (`{"$b":1,"x":2}` is a plain object).
- A user object whose only key is `T` is written as `{"T$": <object>}`.
- A user object whose only key is already `T$` is wrapped under that same key:
  `{"T$": {"T$": x}}`.
- A decoder that meets an object whose only key is `T$` yields the wrapped
  object: its keys are literal (never read as tags) and its values decode as
  usual. The value must be an object; anything else is a malformed frame.
- An empty object and an object whose only key is anything else are written as
  is.

So `{"$d": "x"}` travels as `{"$d$":{"$d":"x"}}` and arrives as `{"$d": "x"}`.

### Binary, dates, maps and sets

- A blob index is a non-negative integer below the frame's blob count.
- Dates are RFC 3339 in UTC with exactly three fractional digits and a `Z`
  suffix, years 0000 to 9999 (`toISOString`, Go `2006-01-02T15:04:05.000Z`).
  Sub-millisecond time is truncated. Decoders (both sides) accept any RFC 3339
  date-time: any number of fractional digits (or none), `Z` or a numeric
  offset; the instant is what counts.
- Go has no Map or Set type; generated codecs never emit `$m` or `$s` (the Encoder can, via `BeginMap` and `BeginSet`, for hosts bridging JS); a Go map is a plain
  JSON object (the TS decoder returns a plain object for it, not a `Map`). A
  Go decoder reads `$m` and `$s` as follows, so that a TS `Map` or `Set`
  argument is accepted:
  - `$s` is an array: `BeginArray` (or the strict `BeginSet`) opens it.
  - `$m` is an object when every key is a string: `BeginObject` opens it and
    `Key` returns each key. A non-string key is an error from `Key`.
  - `$m` is a list of pairs for any key kind: `BeginMap` (or `BeginArray`)
    opens it and each entry is a two-element array.
  - `Kind` reports `KindMap` and `KindSet` so generic readers can choose.

### Numbers and strings

- Both sides emit the shortest decimal that round-trips a double, with no
  whitespace: integers as digits, a fraction when needed, an exponent form
  (`1e+21`, `1.5e-7`, `5e-324`) only below 1e-6 or from 1e21 up. Both decoders
  accept any JSON number, so `1E5` and `-0` are fine.
- `-0`: JS writes it as `0`; Go writes `-0`; both decode `-0` to negative zero.
  NaN and infinities cannot be sent.
- Integers beyond 2^53 are exact in Go and rounded on the TS side; send them as
  strings if the exact value matters.
- Strings are UTF-8. Go escapes like `encoding/json` without HTML escaping
  (including U+2028 and U+2029 as `\u2028`, `\u2029`, and invalid UTF-8 as
  `\ufffd`). JS leaves U+2028 and U+2029 raw and writes lone surrogates as
  `\udXXX`. Decoders accept every JSON string form; a lone surrogate decodes to
  U+FFFD in Go.
- Objects are written in insertion order with no whitespace. JS orders
  integer-like keys first and ascending whatever the insertion order; key order
  carries no meaning, but the shared vectors rely on it for byte equality.

## POST /__krate/call

Request body: one frame.

```json
{"calls": [{"id": 1, "fn": "tasks.List", "args": []}, {"id": 2, "fn": "tasks.Add", "args": ["Buy milk"]}]}
```

`fn` is `<module id>.<export name>`. The module id is assigned by the compiler
(a stable hash-free path such as `src/lib/tasks`). `args` omits `ctx`.

Response: `200`, `content-type: application/x-krate-frames`, a stream of
frames, one per call, each flushed as soon as that call finishes. A slow call
never holds back a fast one in the same batch.

```json
{"id": 1, "ok": [ ... ]}
{"id": 2, "err": {"code": "not_found", "message": "no such task", "data": null}}
{"id": 3, "stream": "s7"}
```

Every call in the request gets exactly one result frame, in completion order.
A call that returns a channel or an async iterable answers with a stream id;
its values are read from `GET /__krate/stream/<id>`.

A Go `error` that is not a `*krate.Error` has code `internal`. A panic has code
`panic`; its message carries the stack only in dev builds.

Each request carries `x-krate-window: <window id>`. Call ids are unique per
window; cancellation is scoped to the window. In a native window the header is
only a hint: the platform layer tags every request with the webview it came
from (`caps.ViewOf`), the shell maps that view to its window or view id, and
the page's header is ignored, so page script cannot name another window's
scope. Without a native view (`--browser` mode) the header is the identity.
A request from a native view the shell cannot place gets a scope that allows
nothing.

Errors that are not a call's own (bad frame, unknown fn) are a `400` with a
frame `{"error": {"code": "bad_request", "message": "..."}}`.

## POST /__krate/cancel

Body: one frame `{"ids": [1, 2]}` for in-flight calls, or `{"streams": ["s7"]}`.
Cancels the call's `ctx`. Answer: `204`.

## GET /__krate/stream/<id>

Response body: a stream of frames, flushed as they are produced.

```json
{"v": <value>}
{"end": true}
{"err": {"code": "...", "message": "...", "data": null}}
```

Closing the response (the reader cancels) cancels the producer's `ctx`. A stream
id is single-use and expires 30 s after it is issued if never opened. A stream
started from a native view can be opened only from that same view (the same
identity as the call); any other request gets the `404` of an unknown stream.

## GET /__krate/signals?names=a,b

Response body: a stream of frames. On connect, the current value of each named
signal, then one frame per change:

```json
{"sig": "src/lib/tasks.Count", "v": 3}
```

A reconnect gets the current values again. Signals are read-only from the UI.
A signal behind a permission (a plugin signal) is checked against the scope of
the requesting window, identified as for calls (the native view, not the
header).

## GET /__krate/hmr (dev only)

Same frame stream carrying the dev hub's messages as `{"hmr": <message>}`.

## Dev control channel

`krate-shell --dev` (the prebuilt shell; a release executable only when built
with the `krate_devcontrol` tag) reads commands from stdin, one per line, and
answers each with one line on stdout. stdout carries only this protocol; the
host's console output goes to stderr. After startup the shell prints
`ready` (browser mode first prints `url <address>`). Closing stdin quits.

`query` and `query-in` also take the source as `@json <json string>`, a JSON
string literal holding the JavaScript, which is how source with line breaks
travels on one line (`query-in main @json "(() => {\n return 1 })()"`).

A reply is `ok`, `ok <json>` or `error <message>`; replies are in command
order and a command never spans lines (JSON is written without raw newlines).

| Command | Does |
| --- | --- |
| `reload-hostjs`, `restart-gohost` | Reload the host bundle; restart the Go host child. |
| `eval <js>` | Run JavaScript in every view; no result. |
| `query <js>` | Run an expression in every view, await a promise, answer `ok <json>` (the first view to answer wins). Page errors answer `error <stack>`. |
| `views` | `ok [{"id":"main","window":"main","primary":true}, ...]`: every open view of every window, windows in creation order and each window's primary view first. A window's primary view has the window's id. Native windows only. |
| `query-in <view> <js>` | `query`, but in the one view with this SDK id. An unknown view answers `error no view "x" (open views: ...)`. |
| `emit <area> <json>` | Deliver a native event to the app as the platform would: `tray` `{"kind":"click"\|"middle-click"\|"right-click"\|"menu","id":""}`, `menu` `{"id":"file.open","window":"main","checked":false}`, `shortcut` `{"accelerator":"CmdOrCtrl+Shift+K"}` (every registration of that accelerator; or `{"id":"<handle>"}`). The shortcut must have been registered and accepted by the system. |
| `memory` | `ok <json>`: a memory report (see BUDGETS.md). |
| `hmr <json>` | Publish one message on `/__krate/hmr`. |
| `quit` | Quit the app. |

Everything is DOM-level or host-level: `query`/`query-in` run script in the
page, and `emit` publishes to the same event streams the platform feeds
(`krate/tray.events`, `krate/menu.events`, `krate/shortcut.events`), so host
code and pages see the event as they would a real one. Nothing here sends
real operating-system input (mouse, keyboard, tray, global shortcuts); an
event that only the OS can produce is not exercised end to end.

## /__krate/chrome (native windows)

The shell serves this endpoint itself, outside the host call protocol; the page
script that marks drag regions uses it (see the Window chrome docs page).
`GET` answers JSON about the window the request came from (`decorated`,
`resizable`, `maximized`, `fullscreen`, `titleBar`, `nativeRegions`, `edges`);
`POST` takes `{"op": "drag" | "toggleMaximize" | "resize", "edge": "..."}` and
starts the native move, the maximize toggle or an edge resize (204 on success,
400 for an unknown op or edge, 501 when the platform cannot start it). The window
is found from the webview that sent the request, never from a header or
a field the page controls; it needs no permission because it acts on the
requester's own window only.

## Golden vectors

`wire/testdata/*.bin` and their `*.json` descriptions are shared by the Go
(`wire`) and TS (`@krate/app`) codec tests; `wire/testdata/README.md` defines
the format. Both sides must decode every vector and re-encode it byte for byte,
except where a `.flags.json` says one side cannot (Go has no Map/Set encoder; JS
cannot write `-0`). Forms only one side emits live in that side's own tests.

## JavaScript host children

A TypeScript host program on Node, Bun or Deno (`packages/native/hostchild`) is a
child like the Go host child: `KRATE_GOHOST_SOCKET`, `ready` on stdout, exit on
stdin EOF or SIGTERM, and the four endpoints above. It adds one endpoint, so
that the shell learns ids whose kind is only known when the code runs:

`GET /__krate/hostchild/list` answers JSON `{"funcs": [...], "streams": [...], "signals": [...]}`.
