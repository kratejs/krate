# @krate/app

Client runtime and host bindings for Krate desktop apps. UI code (TSX in the
system webview) imports host functions; the compiler rewrites those imports into
calls into this package. See `packages/native/PROTOCOL.md` for the wire format.

- `@krate/app`: `call`, `stream`, `hostSignal.signal(name)`, `KrateError`, `app`.
- `@krate/app/client`: `createClient({ fetch?, origin?, windowId?, createSignal? })`,
  the transport used by generated stubs.
- `@krate/app/host`: `hostSignal(initial)` for host-side TS code.
- `@krate/app/testing`: `mockHost(moduleId, impl)`, `resetHostMocks()`,
  `createFakeHost(registry)` (an in-memory fetch speaking the protocol).

## Signals

`signal(name)` returns a callable getter (`Count()`) with `.subscribe(fn)`. The
value is `undefined` until the first frame arrives. To make reads reactive,
pass the runtime's factory once at startup:

```ts
import { createSignal } from "@krate/runtime";
import { createClient, setClient } from "@krate/app";
setClient(createClient({ createSignal }));
```

`@krate/runtime` is not imported by this package, so there is no dependency
cycle and no hard dependency.

## Codec notes

The wire format, including the escape rule for all four tags (`$b`, `$d`, `$m`,
`$s`), is specified in `packages/native/PROTOCOL.md`; `src/wire.ts` implements
it. The golden vectors in `packages/native/wire/testdata` are shared with the Go
codec and run by `test/shared-vectors.test.ts`. A Go `map` arrives as a plain
object, not a `Map`; a `Map` or `Set` argument is accepted by Go as an object
(string keys) / array. Calls with binary arguments are never deduplicated.

## Preliminary modules

`@krate/app/os`, `/db` and `/kv` are placeholders that call host functions named
`krate/os.<Name>`, `krate/db.<Name>` and `krate/kv.<Name>`. Their names and
signatures will change when the Go SDK backs them.

## Desktop SDK (UI half)

`@krate/app/window`, `/dialog`, `/clipboard`, `/menu`, `/tray`, `/shortcut`,
`/notify` and `/app` call the host functions `krate/<area>.<Name>` and subscribe
to the streams `krate/<area>.events` (`onMenu(id, fn)`, `onClick`, `onEvent`,
`onCloseRequested`, `onAction`, `onSecondInstance`). Each needs its permission
in `app.json`; see the SDK page in the Native docs.

