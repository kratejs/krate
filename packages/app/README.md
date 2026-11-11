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

Besides PROTOCOL.md: every tag (`$b`, `$d`, `$m`, `$s`) uses the same escape as
`$b`. A user object whose only key is a tag is sent as `{"<tag>$": obj}`; one
whose only key is already `<tag>$` is wrapped under that same key. Decoders
treat a single-key object `{"<tag>$": obj}` as a literal `obj`. Calls with
binary arguments are never deduplicated.

## Preliminary modules

`@krate/app/os`, `/db` and `/kv` are placeholders that call host functions named
`krate/os.<Name>`, `krate/db.<Name>` and `krate/kv.<Name>`. Their names and
signatures will change when the Go SDK backs them.
