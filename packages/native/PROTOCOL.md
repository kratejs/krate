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

Binary values (`[]byte` in Go, `Uint8Array` in TS) never appear inside the
JSON. They are written as `{"$b": i}`, where `i` indexes the frame's blob list.
A JSON object whose only key is `$b` is reserved; a user object with the single
key `$b` is escaped as `{"$b$": <object>}`.

Other tagged values:

| JSON | Value |
| --- | --- |
| `{"$d": "2026-11-11T07:23:15.123Z"}` | `Date` / `time.Time` (RFC 3339, UTC, millisecond precision) |
| `{"$m": [[k, v], ...]}` | `Map` (TS only; Go maps are plain objects) |
| `{"$s": [v, ...]}` | `Set` (TS only) |

Plain JSON values (objects, arrays, strings, numbers, booleans, null) are sent
as is.

## POST /__krate/call

Request body: one frame.

```json
{"calls": [{"id": 1, "fn": "tasks.List", "args": []}, {"id": 2, "fn": "tasks.Add", "args": ["Buy milk"]}]}
```

`fn` is `<module id>.<export name>`. The module id is assigned by the compiler
(a stable hash-free path such as `src/lib/tasks`). `args` omits `ctx`.

Response: `200`, `content-type: application/x-krate-frames`, one frame.

```json
{"results": [
  {"id": 1, "ok": [ ... ]},
  {"id": 2, "err": {"code": "not_found", "message": "no such task", "data": null}},
  {"id": 3, "stream": "s7"}
]}
```

Results may arrive in any order. A call that returns a channel or an async
iterable answers with a stream id; its values are read from
`GET /__krate/stream/<id>`.

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
id is single-use and expires 30 s after it is issued if never opened.

## GET /__krate/signals?names=a,b

Response body: a stream of frames. On connect, the current value of each named
signal, then one frame per change:

```json
{"sig": "src/lib/tasks.Count", "v": 3}
```

A reconnect gets the current values again. Signals are read-only from the UI.

## GET /__krate/hmr (dev only)

Same frame stream carrying the dev hub's messages as `{"hmr": <message>}`.

## Golden vectors

`wire/testdata/*.bin` and their `*.json` descriptions are shared by the Go
(`wire`) and TS (`@krate/app`) codec tests. Both sides must decode every vector
and re-encode it byte for byte.
