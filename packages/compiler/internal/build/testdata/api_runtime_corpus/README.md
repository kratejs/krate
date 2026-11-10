# API runtime differential corpus

Each case in this directory is an API route that is run under the embedded
QuickJS runtime and under a reference runtime (Node, and Bun when installed).
The responses must match on the parts the case selects. The Go driver is
`TestAPIRuntimeConformance` in `../../api_runtime_conformance_test.go`, but the
format below is engine-neutral so another harness can replay the same files.

## Layout

```
<group>/<name>.ts     the route module
<group>/<name>.json   the request spec
```

A case is named by its path without the extension, for example
`url/relative-resolution` or `routing/dynamic/[id]`. Cite cases by that name.
Groups are named after the Web API (or behavior) they exercise.

## Route module

A self-contained TypeScript module with no imports. It exports one function
per HTTP method (`GET`, `POST`, ...), each called as `handler(request, ctx)`
with a standard `Request` and `ctx.params` holding dynamic route segments. It
returns a `Response`, a JSON-serializable value, or nothing.

The module is mounted at `/api/<case name>`, so
`url/relative-resolution.ts` serves `/api/url/relative-resolution`. Requests use
the Host `blog.example.com`.

## Request spec

```json
{
  "description": "What the case checks.",
  "requests": [
    {
      "method": "POST",
      "query": "?a=1",
      "headers": { "Content-Type": "text/plain" },
      "body": "text body"
    }
  ],
  "compare": { "headers": ["content-type", "x-custom"], "body": "bytes" },
  "skipRuntimes": { "bun": "why Bun differs from Node here" },
  "heavy": false,
  "divergence": { "reason": "...", "nodeVersion": "v24.0.0", "quickjs": [], "node": [] }
}
```

Only `requests` is required. Requests are sent in order, each to every runtime.

Request fields:

| field        | meaning |
|--------------|---------|
| `method`     | HTTP method, default `GET`. |
| `path`       | Absolute request path, overriding `/api/<case name>` (used by dynamic routes). |
| `query`      | Appended to the path, including the leading `?`. |
| `headers`    | Request headers. |
| `body`       | Body as UTF-8 text. |
| `bodyBase64` | Body as base64, for binary data. Takes precedence over `body`. |
| `bodyRepeat` | Repeat the body this many times (used for ~1 MiB bodies). |
| `status`     | Expected status. Without it, a 5xx answer from the reference runtime fails the case, so a handler that crashes on both sides cannot pass by accident. |

`compare` selects the response parts (all optional):

| field     | default | meaning |
|-----------|---------|---------|
| `status`  | `true`  | Compare the status code. |
| `headers` | `["content-type", "location", "allow", "x-custom"]` | Header names to compare; a header missing on one side is a difference. Content-Type is compared lowercased with spaces and a trailing `;charset=utf-8` removed. |
| `cookies` | `true`  | Compare every `Set-Cookie` value, in order. |
| `body`    | `"bytes"` | `"bytes"` compares the body byte for byte; `"none"` skips it. |

`skipRuntimes` maps a reference runtime other than Node to a reason the case is
skipped there because that runtime differs from Node (Node is the reference for
conformance).

`heavy: true` marks megabyte-sized cases. The Go driver skips them in `-race`
builds, where the embedded runtime is too slow for them; other harnesses may
ignore the flag.

## Divergences

When QuickJS and Node genuinely differ, the case keeps its strict request and
records the difference instead of loosening the comparison:

- `reason`: what differs and why.
- `quickjs`: one entry per request, `{ "status": 200, "body": "..." }` (or
  `bodyBase64`). The driver asserts QuickJS still produces exactly this.
- `node`: the reference output, for documentation.
- `nodeVersion`: the Node version the record was captured with. The `node`
  entries are asserted only on that Node major.

When QuickJS starts matching Node (on the recorded Node major) the case fails
and asks for the divergence entry to be removed. Divergent cases are skipped
for Bun.

Divergences are kept in their own small cases (for example
`url/credential-setters` next to `url/credentials`) so a gap in one behavior does
not hide coverage of the rest of an API.

## Writing cases

- Responses must be deterministic. Compare random values, UUIDs and times by
  shape inside the handler (`/^[0-9a-f-]{36}$/.test(id)`, `elapsed >= 3`) and
  return the result, never the value.
- Error message text is engine-specific, so handlers report the error class and
  DOMException name (`TypeError`, `DOMException:InvalidCharacterError`) rather
  than `message`.
- Do not throw from timers or event listeners: on Node that is an uncaught
  exception that takes the sidecar down.
- `process.env.KRATE_CORPUS_VAR` is set to `corpus-value` on every runtime.

## Running

From `packages/compiler`:

```sh
go test ./internal/build -run 'TestAPIRuntimeConformance' -v
go test ./internal/build -run 'TestAPIRuntimeConformance/node/url/' -v
KRATE_CORPUS_DUMP=$PWD/corpus-dump go test ./internal/build -run TestAPIRuntimeConformance
```

`KRATE_CORPUS_DUMP` writes the full QuickJS and reference bodies (and both
statuses) of every differing response under that directory, since failure
messages clip long bodies. Use it to record a new divergence.
