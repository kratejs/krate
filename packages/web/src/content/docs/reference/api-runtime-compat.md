---
title: API Runtime Compatibility
order: 6
description: Which Web APIs and Node globals the embedded QuickJS runtime for API routes and middleware supports, generated from the compiler ledger.
---

<!-- Generated from packages/native/jsruntime/compat.json by
     scripts/sync-runtime-compat.mjs. Do not edit by hand; edit the ledger and
     run pnpm sync:docs. -->

# API Runtime Compatibility

API routes and middleware run in Krate's embedded QuickJS runtime unless
`ssr.apiRuntime` (or `ssr.middlewareRuntime`) selects `"node"`, `"bun"` or
`"deno"`. This page lists what that runtime provides. It is generated from the
ledger the compiler embeds, and every supported row names the Go tests that
verify it. Tests named `corpus:<case>` are cases of the differential corpus
(`packages/compiler/internal/build/testdata/api_runtime_corpus`), which runs
each route under QuickJS and under Node and compares the responses; a row that
cites a case with a recorded difference from Node is at most partial.

Each feature group below is installed the first time code reads one of its
globals, so a route pays only for the APIs it uses. Pooled API and middleware
workers install the groups the build found in their sources when they start.

Coverage: The WinterTC Minimum Common Web Platform API globals, plus the Node globals route handlers commonly reach for (Buffer, process, setImmediate, require, node: modules as one class).

## Status key

- **Supported** - implemented and covered by the listed tests.
- **Partial** - implemented with the limits given in the notes.
- **Not supported** - undefined, or a stub that throws `NotSupportedError`.
- **By design** - handled another way on purpose.

## Build-time warnings

When an API route or middleware is built for the embedded runtime, Krate scans
the modules it bundles and reports each not-supported API it uses as
[`KR7160`](/docs/reference/diagnostics/#kr7160), with the file, line and
column and the hint from this table. The build still succeeds. Only global
references count: imports and locals of the same name, `typeof X` checks and
code guarded by them are not reported, and a dependency is reported once per
package and API. Calling the API at run time throws a `NotSupportedError`
whose message carries the same code. Instance methods (such as
`ReadableStream.prototype.tee`) cannot be seen by the scan and only fail at
run time. Routes that import a `node:` builtin are served by Node and are not
scanned.

## Summary

| Status | APIs |
|---|---|
| Supported | 23 |
| Partial | 20 |
| Not supported | 68 |
| By design | 1 |

## Events and abort

DOMException, Event, EventTarget, AbortController and AbortSignal. Feature group: `events`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `DOMException` | Supported | name and legacy code are set from the error name. |  | `TestCompatLedgerBehavior/DOMException`, `TestWebAPIMatrix/atob invalid`, `corpus:errors/error-properties`, `corpus:abort/controller-reason` |
| `Event` | Partial | No DOM, so there is no propagation path: bubbles is recorded but stopPropagation() has no effect. new Event() without a type uses "undefined" instead of throwing TypeError. |  | `TestCompatLedgerBehavior/Event`, `corpus:events/event-props`, `corpus:events/event-requires-type`, `corpus:events/event-target`, `corpus:events/stop-immediate` |
| `EventTarget` | Supported | addEventListener (with once), removeEventListener and dispatchEvent. |  | `TestCompatLedgerBehavior/EventTarget`, `corpus:events/event-props`, `corpus:events/event-target`, `corpus:events/stop-immediate` |
| `CustomEvent` | Not supported |  | Extend Event and attach the payload as a property, or set ssr.apiRuntime to "node" in krate.config.ts. | `corpus:events/custom-event` |
| `ErrorEvent` | Not supported |  | Dispatch a plain Event carrying the error, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `PromiseRejectionEvent` | Not supported |  | Handle the rejection with .catch() where the promise is created. |  |
| `AbortController` | Supported |  |  | `TestCompatLedgerBehavior/AbortController`, `TestWebAPIMatrix/abort`, `corpus:abort/any`, `corpus:abort/controller-reason`, `corpus:abort/events`, `corpus:abort/request-signal` |
| `AbortSignal` | Supported | abort(), timeout(), any() and throwIfAborted(). |  | `TestCompatLedgerBehavior/AbortSignal`, `TestWebAPIMatrix/abort timeout`, `corpus:abort/any`, `corpus:abort/controller-reason`, `corpus:abort/events`, `corpus:abort/request-signal`, `corpus:abort/signal-abort-static`, `corpus:abort/timeout` |
| `reportError` | Not supported |  | Log the error with console.error instead. |  |
| `onerror` | Not supported | There is no global error event; an uncaught error fails the request and is logged. | Wrap the handler body in try/catch. |  |
| `onunhandledrejection` | Not supported | There is no global unhandledrejection event. | Attach .catch() to the promise, or await it inside try/catch. |  |

## Timers

setTimeout and friends, queueMicrotask and performance, driven by the Go job loop. Feature group: `timers`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `setTimeout` | Supported | Timers run on the request's event loop; a handler can await a timeout. |  | `TestCompatLedgerBehavior/setTimeout`, `TestWebAPIMatrix/settimeout fires`, `TestWebAPIMatrix/timer order`, `corpus:timers/set-timeout-wait`, `corpus:timers/ordering`, `corpus:timers/args-and-handles`, `corpus:timers/microtasks-between-timers` |
| `clearTimeout` | Supported |  |  | `TestCompatLedgerBehavior/clearTimeout`, `TestWebAPIMatrix/clearTimeout`, `corpus:timers/clear-timeout`, `corpus:timers/args-and-handles` |
| `setInterval` | Supported |  |  | `TestCompatLedgerBehavior/setInterval`, `TestWebAPIMatrix/setInterval`, `corpus:timers/set-interval` |
| `clearInterval` | Supported |  |  | `TestCompatLedgerBehavior/clearInterval`, `corpus:timers/set-interval`, `corpus:timers/clear-timeout` |
| `setImmediate` | Supported | Node global; scheduled as a zero-delay timer. |  | `TestCompatLedgerBehavior/setImmediate`, `corpus:timers/set-immediate` |
| `clearImmediate` | Supported | Node global. |  | `TestCompatLedgerBehavior/clearImmediate`, `corpus:timers/set-immediate` |
| `queueMicrotask` | Supported |  |  | `TestCompatLedgerBehavior/queueMicrotask`, `TestWebAPIMatrix/queueMicrotask`, `corpus:timers/ordering`, `corpus:timers/microtasks-between-timers` |
| `performance` | Partial | performance.now(), performance.timeOrigin and toJSON() only. |  | `TestCompatLedgerBehavior/performance`, `TestWebAPIMatrix/performance`, `corpus:performance/now` |
| `performance.mark` | Not supported |  | Record performance.now() before and after and subtract. |  |
| `performance.measure` | Not supported |  | Record performance.now() before and after and subtract. |  |

## Encoding

atob, btoa, TextEncoder and TextDecoder. Feature group: `encoding`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `atob` | Partial | Accepts some invalid padding that Node rejects ("Zg=", "Z==="). |  | `TestCompatLedgerBehavior/atob`, `TestWebAPIMatrix/btoa atob`, `TestWebAPIMatrix/atob invalid`, `corpus:base64/atob`, `corpus:base64/atob-invalid`, `corpus:base64/atob-invalid-padding`, `corpus:base64/binary-roundtrip` |
| `btoa` | Supported |  |  | `TestCompatLedgerBehavior/btoa`, `TestWebAPIMatrix/btoa atob`, `corpus:base64/btoa`, `corpus:base64/btoa-invalid`, `corpus:base64/binary-roundtrip` |
| `TextEncoder` | Supported | encode() and encodeInto(). |  | `TestCompatLedgerBehavior/TextEncoder`, `TestWebAPIMatrix/textencoder`, `TestTextEncoder`, `corpus:encoding/text-encoder`, `corpus:encoding/encode-into` |
| `TextDecoder` | Partial | utf-8, utf-16le and windows-1252 (latin1, ascii) labels, with fatal, ignoreBOM and stream; any other label throws RangeError. The windows-1252 labels map bytes 0x80-0x9f to U+0080-U+009F instead of the windows-1252 table (0x80 is U+20AC in Node). |  | `TestCompatLedgerBehavior/TextDecoder`, `TestWebAPIMatrix/textdecoder`, `TestWebAPIMatrix/textdecoder fatal`, `TestWebAPIMatrix/textdecoder stream`, `corpus:encoding/decoder-bom`, `corpus:encoding/decoder-fatal`, `corpus:encoding/decoder-stream`, `corpus:encoding/decoder-labels`, `corpus:encoding/decoder-inputs`, `corpus:encoding/decoder-windows-1252` |

## structuredClone

structuredClone. Feature group: `clone`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `structuredClone` | Partial | Plain data, Date, RegExp, Map, Set, ArrayBuffer and typed arrays, Blob and errors, with cycles; the transfer option throws NotSupportedError. Arrays are copied by index only, so extra own properties on an array are dropped. |  | `TestCompatLedgerBehavior/structuredClone`, `TestWebAPIMatrix/structuredClone`, `TestWebAPIMatrix/structuredClone fn throws`, `corpus:structured-clone/array-extra-props`, `corpus:structured-clone/cycles`, `corpus:structured-clone/date-regexp`, `corpus:structured-clone/errors`, `corpus:structured-clone/map-set`, `corpus:structured-clone/objects`, `corpus:structured-clone/primitives`, `corpus:structured-clone/typed-arrays`, `corpus:structured-clone/uncloneable` |

## URL

URL (parsed by the Go host) and URLSearchParams. Feature group: `url`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `URL` | Partial | WHATWG parsing (Go host); URL.canParse and URL.parse. Host parsing differs from Node: IPv6 literals are kept as written (no zero compression), numeric IPv4 forms (hex, octal, shortened) are not normalized, file://localhost keeps localhost and every blob: URL has origin "null". The username and password setters re-encode existing percent escapes, and the protocol setter applies special/non-special scheme switches that Node ignores. |  | `TestCompatLedgerBehavior/URL`, `TestWebAPIMatrix/url parts`, `TestWebAPIMatrix/url relative`, `TestWebAPIMatrix/url invalid throws`, `TestURLRelativeResolution`, `corpus:url/blob-origin`, `corpus:url/credential-setters`, `corpus:url/credentials`, `corpus:url/file-localhost`, `corpus:url/idna-host`, `corpus:url/invalid-inputs`, `corpus:url/ipv4-number-forms`, `corpus:url/ipv6-literals`, `corpus:url/ipv6-serialization`, `corpus:url/normalization`, `corpus:url/origin`, `corpus:url/percent-encoding`, `corpus:url/protocol-setter-special`, `corpus:url/relative-resolution`, `corpus:url/request-url`, `corpus:url/search-params-mutation`, `corpus:url/serialization`, `corpus:url/setters`, `corpus:url/static-methods` |
| `URLSearchParams` | Supported |  |  | `TestCompatLedgerBehavior/URLSearchParams`, `TestWebAPIMatrix/urlsearchparams eq`, `TestWebAPIMatrix/urlsearchparams toString`, `TestWebAPIMatrix/url searchParams sync`, `corpus:urlsearchparams/constructors`, `corpus:urlsearchparams/duplicates`, `corpus:urlsearchparams/encoding`, `corpus:urlsearchparams/iteration`, `corpus:urlsearchparams/sort`, `corpus:url/search-params-mutation` |
| `URLPattern` | Not supported |  | Match with a RegExp, or read the route params passed to the handler. |  |

## Fetch, Request and Response

Headers, Request, Response and fetch (performed by the Go host). The API and middleware runners always use this feature. Feature group: `fetch`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `Headers` | Partial | Case-insensitive, iterable in sorted order, getSetCookie(). Values are only checked for NUL, CR and LF, so characters Node rejects (outside Latin-1) are accepted. |  | `TestCompatLedgerBehavior/Headers`, `TestWebAPIMatrix/headers case-insensitive`, `TestWebAPIMatrix/headers iterable sorted`, `TestWebAPIMatrix/getSetCookie`, `corpus:headers/append-vs-set`, `corpus:headers/case-insensitive`, `corpus:headers/constructors`, `corpus:headers/get-set-cookie`, `corpus:headers/iteration-order`, `corpus:headers/non-latin1-value`, `corpus:headers/response-headers`, `corpus:headers/set-cookie-multiple`, `corpus:headers/validation` |
| `Request` | Partial | Bodies are promise-based (json, text, formData, arrayBuffer, blob, bytes); clone() of a request with a stream body throws NotSupportedError. A leading BOM is kept by text() (and makes json() throw SyntaxError) where Node strips it, and invalid UTF-8 in the incoming body becomes one U+FFFD per invalid sequence rather than per WHATWG maximal subpart. |  | `TestCompatLedgerBehavior/Request`, `TestWebAPIMatrix/req json async`, `TestWebAPIMatrix/body used`, `TestWebAPIMatrix/formData urlencoded`, `corpus:request/body-used`, `corpus:request/clone`, `corpus:request/constructed`, `corpus:request/consumers`, `corpus:request/headers`, `corpus:request/json-body`, `corpus:request/properties`, `corpus:request/text-body`, `corpus:unicode/bom-body-json`, `corpus:unicode/invalid-utf8-request` |
| `Response` | Partial | Response.json, Response.redirect, Response.error and stream bodies; clone() of a response with a stream body throws NotSupportedError. Response.error() and Response.redirect() return mutable headers; the status must be an integer (200.5 throws RangeError) and statusText is not validated. |  | `TestCompatLedgerBehavior/Response`, `TestBinaryAPIBodies`, `TestWebAPIMatrix/response json`, `TestWebAPIMatrix/response redirect`, `TestWebAPIMatrix/stream body`, `corpus:response/body-types`, `corpus:response/body-used`, `corpus:response/clone`, `corpus:response/consumers`, `corpus:response/custom-status`, `corpus:response/error-static`, `corpus:response/headers-immutable-request`, `corpus:response/implicit-empty`, `corpus:response/json-returned`, `corpus:response/json-static`, `corpus:response/plain-object`, `corpus:response/redirect-headers-immutable`, `corpus:response/redirect-relative`, `corpus:response/redirect-returned`, `corpus:response/redirect-static`, `corpus:response/status-init-coercion`, `corpus:response/status-text` |
| `fetch` | Supported | Performed by the Go host and buffered; honours cache and next.revalidate/tags options. |  | `TestCompatLedgerBehavior/fetch`, `TestFetchExists`, `TestQuickJSFetchOptions`, `TestBinaryFetch` |
| `WebSocket` | Not supported | Defined as a stub that throws NotSupportedError. | Open sockets from a Go API route, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `EventSource` | Not supported | Defined as a stub that throws NotSupportedError. Serving Server-Sent Events from a route works (return a ReadableStream body). | Read the event stream with fetch() and response.body, or set ssr.apiRuntime to "node" in krate.config.ts. |  |

## Streams

ReadableStream and its default reader, plus the unsupported stream stubs. Feature group: `streams`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `TextEncoderStream` | Not supported |  | Encode chunks with TextEncoder inside a ReadableStream source, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `TextDecoderStream` | Not supported |  | Read the stream and decode chunks with new TextDecoder().decode(chunk, { stream: true }), or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `ReadableStream` | Partial | Default readers, async iteration and ReadableStream.from; tee(), pipeTo(), pipeThrough() and BYOB readers throw NotSupportedError. There is no values() method (use for await or getReader()), read results are { value, done } with no value key on the final one, and reading a body directly through body does not set bodyUsed. |  | `TestCompatLedgerBehavior/ReadableStream`, `TestWebAPIMatrix/stream body`, `TestWebAPIMatrix/stream async iter`, `corpus:streams/async-iteration`, `corpus:streams/binary-chunks`, `corpus:streams/body-stream`, `corpus:streams/body-used-after-reader`, `corpus:streams/cancel`, `corpus:streams/enqueue-close-error`, `corpus:streams/error-midstream`, `corpus:streams/from`, `corpus:streams/pull-source`, `corpus:streams/read-result-shape`, `corpus:streams/reader`, `corpus:streams/response-stream`, `corpus:streams/string-chunks`, `corpus:streams/tee`, `corpus:streams/unsupported-helpers`, `corpus:streams/values-method` |
| `ReadableStream.prototype.tee` | Not supported | Instance method, so builds cannot see calls to it. | Read the stream once and buffer the chunks, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `ReadableStream.prototype.pipeTo` | Not supported | Instance method, so builds cannot see calls to it. | Read with getReader() and write each chunk yourself, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `ReadableStream.prototype.pipeThrough` | Not supported | Instance method, so builds cannot see calls to it. | Transform chunks inside a new ReadableStream's pull(), or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `ReadableStreamDefaultReader` | Supported |  |  | `TestCompatLedgerBehavior/ReadableStreamDefaultReader` |
| `ReadableStreamDefaultController` | Not supported | Not exposed as a global; the controller passed to start() and pull() works. | Use the controller argument of the underlying source instead of the class. |  |
| `ReadableByteStreamController` | Not supported |  | Use a default ReadableStream that enqueues Uint8Array chunks. |  |
| `ReadableStreamBYOBReader` | Not supported |  | Use stream.getReader() (a default reader). |  |
| `ReadableStreamBYOBRequest` | Not supported |  | Use stream.getReader() (a default reader). |  |
| `WritableStream` | Not supported | Defined as a stub that throws NotSupportedError. | Collect chunks in an array or push them into a ReadableStream controller, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `WritableStreamDefaultController` | Not supported |  | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `WritableStreamDefaultWriter` | Not supported |  | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `TransformStream` | Not supported | Defined as a stub that throws NotSupportedError. | Transform chunks inside a ReadableStream's pull() or start(), or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `TransformStreamDefaultController` | Not supported |  | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `ByteLengthQueuingStrategy` | Not supported |  | Pass { highWaterMark: n } as the stream strategy. |  |
| `CountQueuingStrategy` | Not supported |  | Pass { highWaterMark: n } as the stream strategy. |  |
| `CompressionStream` | Not supported | Defined as a stub that throws NotSupportedError. | Let the server or CDN compress responses, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `DecompressionStream` | Not supported | Defined as a stub that throws NotSupportedError. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |

## Blob, File and FormData

Blob, File and FormData, with multipart parsing in the Go host. Feature group: `formdata`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `Blob` | Partial | slice, text, arrayBuffer, bytes and stream. The type is only lowercased: non-ASCII and control characters are kept where Node replaces the type with an empty string. |  | `TestCompatLedgerBehavior/Blob`, `TestWebAPIMatrix/blob`, `corpus:blob/parts`, `corpus:blob/readers`, `corpus:blob/slice`, `corpus:blob/type`, `corpus:blob/type-invalid-chars` |
| `File` | Supported |  |  | `TestCompatLedgerBehavior/File`, `TestWebAPIMatrix/formData multipart roundtrip`, `corpus:file/basics`, `corpus:formdata/files` |
| `FormData` | Supported | Parses and serializes urlencoded and multipart bodies; constructing from an HTML form element throws (there is no DOM). |  | `TestCompatLedgerBehavior/FormData`, `TestWebAPIMatrix/formData urlencoded`, `TestWebAPIMatrix/formData multipart roundtrip`, `corpus:formdata/append-get`, `corpus:formdata/files`, `corpus:formdata/invalid-body`, `corpus:formdata/multipart-request`, `corpus:formdata/multipart-request-files`, `corpus:formdata/multipart-roundtrip`, `corpus:formdata/urlencoded-request` |

## Crypto

crypto.getRandomValues, randomUUID and crypto.subtle (digest and HMAC in the Go host). Feature group: `crypto`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `crypto` | Partial | getRandomValues, randomUUID and a subset of crypto.subtle. |  | `TestCompatLedgerBehavior/crypto` |
| `crypto.getRandomValues` | Partial | A float array throws a plain TypeError (Node throws a TypeMismatchError DOMException), and a DataView is accepted. |  | `TestCompatLedgerBehavior/crypto.getRandomValues`, `TestWebAPIMatrix/getRandomValues`, `corpus:crypto/get-random-values`, `corpus:crypto/get-random-values-errors`, `corpus:crypto/get-random-values-quota` |
| `crypto.randomUUID` | Supported |  |  | `TestCompatLedgerBehavior/crypto.randomUUID`, `TestWebAPIMatrix/crypto uuid`, `corpus:crypto/random-uuid` |
| `crypto.subtle` | Partial | digest, and HMAC importKey/sign/verify with raw keys. |  | `TestCompatLedgerBehavior/crypto.subtle`, `TestWebAPIMatrix/subtle unsupported`, `corpus:crypto/unsupported-subtle` |
| `crypto.subtle.digest` | Supported | SHA-1, SHA-256, SHA-384 and SHA-512. |  | `TestCompatLedgerBehavior/crypto.subtle.digest`, `TestBinaryHostFunctions`, `TestWebAPIMatrix/digest`, `corpus:crypto/digest`, `corpus:crypto/digest-errors` |
| `crypto.subtle.importKey` | Partial | Only the raw format with the HMAC algorithm; anything else rejects with NotSupportedError. |  | `TestCompatLedgerBehavior/crypto.subtle.importKey`, `TestWebAPIMatrix/hmac`, `corpus:crypto/hmac` |
| `crypto.subtle.sign` | Partial | HMAC keys only. |  | `TestCompatLedgerBehavior/crypto.subtle.sign`, `TestWebAPIMatrix/hmac`, `corpus:crypto/hmac` |
| `crypto.subtle.verify` | Partial | HMAC keys only (constant-time compare). |  | `TestCompatLedgerBehavior/crypto.subtle.verify`, `corpus:crypto/hmac` |
| `crypto.subtle.encrypt` | Not supported | Rejects with NotSupportedError. | Do this in a Go API route, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `crypto.subtle.decrypt` | Not supported | Rejects with NotSupportedError. | Do this in a Go API route, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `crypto.subtle.generateKey` | Not supported | Rejects with NotSupportedError. | Do this in a Go API route, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `crypto.subtle.deriveKey` | Not supported | Rejects with NotSupportedError. | Do this in a Go API route, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `crypto.subtle.deriveBits` | Not supported | Rejects with NotSupportedError. | Do this in a Go API route, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `crypto.subtle.exportKey` | Not supported | Rejects with NotSupportedError. | Do this in a Go API route, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `crypto.subtle.wrapKey` | Not supported | Rejects with NotSupportedError. | Do this in a Go API route, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `crypto.subtle.unwrapKey` | Not supported | Rejects with NotSupportedError. | Do this in a Go API route, or set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `CryptoKey` | Supported | Instances come from crypto.subtle.importKey. |  | `TestCompatLedgerBehavior/CryptoKey` |
| `Crypto` | Not supported | The interface object is not exposed; the crypto global works. | Use the crypto global directly instead of the Crypto class. |  |
| `SubtleCrypto` | Not supported | The interface object is not exposed; crypto.subtle works. | Use crypto.subtle directly instead of the SubtleCrypto class. |  |

## Console

console methods backed by the Go host. Feature group: `console`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `console` | Partial | log, info, debug, warn, error and trace; arguments are formatted to one line. |  | `TestCompatLedgerBehavior/console`, `TestConsole`, `corpus:console/no-output-effect` |
| `console.assert` | Not supported | Not defined, so calling it throws TypeError. | Use console.log or console.error. |  |
| `console.count` | Not supported | Not defined, so calling it throws TypeError. | Use console.log or console.error. |  |
| `console.dir` | Not supported | Not defined, so calling it throws TypeError. | Use console.log or console.error. |  |
| `console.group` | Not supported | Not defined, so calling it throws TypeError. | Use console.log or console.error. |  |
| `console.groupEnd` | Not supported | Not defined, so calling it throws TypeError. | Use console.log or console.error. |  |
| `console.table` | Not supported | Not defined, so calling it throws TypeError. | Use console.log or console.error. |  |
| `console.time` | Not supported | Not defined, so calling it throws TypeError. | Use console.log or console.error. |  |
| `console.timeEnd` | Not supported | Not defined, so calling it throws TypeError. | Use console.log or console.error. |  |

## Node globals

The Node globals the embedded runtime provides outside the prelude (process.env, a minimal require) and the ones it lacks. Feature group: `node`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `process` | Partial | Only process.env (the server environment). |  | `TestCompatLedgerBehavior/process`, `TestAPIRouteProcessEnv`, `TestSetEnv` |
| `process.env` | Supported |  |  | `TestCompatLedgerBehavior/process.env`, `TestAPIRouteProcessEnv`, `TestSetEnv`, `corpus:process/env` |
| `process.nextTick` | Not supported | Not defined on the embedded process object. | Use queueMicrotask(fn). |  |
| `process.cwd` | Not supported | Not defined on the embedded process object. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `process.argv` | Not supported | Not defined on the embedded process object. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `process.exit` | Not supported | Not defined on the embedded process object. | Return an error Response instead. |  |
| `process.platform` | Not supported | Not defined on the embedded process object. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `process.version` | Not supported | Not defined on the embedded process object. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `process.versions` | Not supported | Not defined on the embedded process object. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `process.hrtime` | Not supported | Not defined on the embedded process object. | Use performance.now(). |  |
| `process.memoryUsage` | Not supported | Not defined on the embedded process object. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `process.uptime` | Not supported | Not defined on the embedded process object. | Use performance.now(). |  |
| `process.on` | Not supported | Not defined on the embedded process object. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `process.stdout` | Not supported | Not defined on the embedded process object. | Use console.log. |  |
| `process.stderr` | Not supported | Not defined on the embedded process object. | Use console.error. |  |
| `Buffer` | Not supported | The Node Buffer global is not defined. | Use Uint8Array with TextEncoder/TextDecoder and atob/btoa, or set ssr.apiRuntime to "node" in krate.config.ts (an API route that imports { Buffer } from "node:buffer" is served by Node automatically). |  |
| `global` | Not supported | Node's alias for the global object is not defined. | Use globalThis. |  |
| `__dirname` | Not supported | Bundled routes have no module file path. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `__filename` | Not supported | Bundled routes have no module file path. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `require` | Partial | A run-time require() resolves only "fs" (readFileSync and readFile, utf-8) and "path" (join, resolve, basename, dirname, extname); static requires are bundled at build time. |  | `TestCompatLedgerBehavior/require` |
| `node:*` | By design | An API route that imports a node: builtin (or anything else esbuild leaves external) is marked nodeOnly in .krate/api-routes.json and served by the Node sidecar, so it is not scanned for this ledger. | Nothing to do: the route runs on Node automatically. Middleware has no such fallback; set ssr.middlewareRuntime to "node" if it needs Node modules. | `TestAPIRouteNodeBuiltinIsNodeOnly` |

## Engine built-ins

Globals that come from the QuickJS engine itself, not the prelude. Feature group: `engine`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `globalThis` | Supported | Provided by QuickJS. |  | `TestCompatLedgerBehavior/globalThis` |
| `WebAssembly` | Not supported | QuickJS has no WebAssembly engine. | Set ssr.apiRuntime to "node" in krate.config.ts. |  |
| `Intl` | Not supported | QuickJS is built without Intl, so Intl.DateTimeFormat, Intl.NumberFormat and friends are undefined. | Format dates and numbers by hand, or set ssr.apiRuntime to "node" in krate.config.ts. |  |

## Global object

Global object aliases and properties that no prelude section provides yet. Feature group: `global`.

| API | Status | Notes | Instead | Tests |
|---|---|---|---|---|
| `self` | Not supported |  | Use globalThis. |  |
| `navigator` | Not supported | There is no navigator, so navigator.userAgent is unavailable. | Read the user agent from request.headers.get("user-agent"). |  |

## See also

- [API Routes](/docs/features/api-routes/)
- [Middleware](/docs/features/middleware/)
- [Diagnostic codes](/docs/reference/diagnostics/)
