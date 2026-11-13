---
title: Build Functions
order: 6
description: Run a function once during the native build and bake its result into the app with @build or //krate:build. Rules for literal arguments and plain-data results.
---

# Build Functions

> **Experimental.** Part of [Krate Native](/docs/native/), which is under construction.

A **build function** is a host function that runs once while the app is
built. Its result is baked into the app: UI code that calls it gets the value
at prerender time, so the value is in the HTML and no host call happens at run
time. It is opt-in, because the compiler cannot prove that a function has no
side effects.

## Marking one

TypeScript: the `@build` decorator, or the string `"@build"` as the first
statement of the body (the same two forms as `@host`). A build function is host
code, so the rules of [host code](/docs/native/host-code) apply; it can sit in
a `.host.ts` module or inline in a UI module.

```ts
// src/lib/build.host.ts
@build
export function buildInfo(label: string): string {
  return `${label} ${new Date().toISOString().slice(0, 10)}`;
}
```

Go: the `//krate:build` directive in the doc comment of an exported function
that returns one value (and optionally an error).

```go
// Edition is evaluated while the app is built.
//
//krate:build
func Edition(name string) string { return "Krate " + name + " edition" }
```

UI code calls them like any host function, but the call is replaced by the
value:

```tsx
import { buildInfo } from '../lib/build.host';
import { Edition } from '../lib/tasks.go';

export default function Home() {
  return <p>{buildInfo('Built')} - {Edition('Tasks')}</p>;
}
```

The call is a plain synchronous value, not a promise; `await buildInfo('x')`
still works. Build functions are not registered with the host, so they are not
in the host bundle or in `app.json`.

## Arguments are literals

Every argument must be a literal: a string, number, boolean, `null`, or an
array or object of those. Each distinct call with literal arguments gets its
own baked value, so `greet("ann")` and `greet("bob")` are evaluated
separately. Anything else (a variable, an expression) is KR7240. Using the
function without calling it (passing it, aliasing it, a namespace import,
`export *`) is KR7241.

## How they are evaluated

- TypeScript build functions are bundled with esbuild together with the
  modules they import and run in a bare QuickJS VM: the ECMAScript built-ins,
  no `fetch`, timers, file system, `process`, or the SDK. Methods that depend
  on the locale or time zone (`toLocaleString`, `Date#toString`, ...) throw, so
  the value does not depend on the build machine. Keep a module that holds
  build functions free of imports that need a browser or Node API (put them in
  their own module). A function that throws, rejects or never settles is
  KR7242.
- Go build functions run in a small generated Go program, built inside your
  module with the same glue as the Go host, so arguments and results go
  through the normal wire conversion. This needs the Go toolchain, and only
  when the UI uses a Go build function (KR7245 if the program cannot be built).

## What can be baked

Plain data only: strings, finite numbers, booleans, `null`, arrays and plain
objects. At most 8 MiB of JSON, nested at most 512 levels, arrays of at most
1,000,000 elements. NaN, Infinity, cycles, functions, symbols, bigint, `Date`,
`Map`, `Set`, binary data, class instances and promises that never settle are
KR7243. `@build` on a component, a generator or a stream, a default export, or
a Go function that returns a channel or nothing is KR7244.

## Calling the SDK from host code

The same `@krate/app/<area>` imports work in TypeScript host code. In the
webview they go over HTTP; in host code, which runs in the shell's QuickJS VM,
`@krate/app` finds `globalThis.__krateHost.call` and dispatches **in-process**
into the shell's registry, with no HTTP:

```ts
// src/lib/notes.host.ts
import * as kv from '@krate/app/kv';

export async function addNote(text: string) {
  await kv.set('last-note', text); // in-process
}
```

`__krateHost.call(id, args)`, `__krateHost.stream(id, args)` and
`__krateHost.signal(id)` are the underlying primitives; `call` returns a
promise, `stream` an async iterable and `signal` an object with `get()`,
`subscribe(fn)` and `close()`. Host code is trusted: these calls are **not**
gated by the app's permissions (the permissions gate the UI), so a host-code
call to `krate/kv.set` works even when `app.json` does not grant `kv`. The
permissions derived for a release still include what host code can reach.
Go host code calls the data packages (`db`, `kv`, `fs`) directly; they use the
same files as the ids.

## Testing UI that calls host code

`mockHost(moduleId, impl)` from `@krate/app/testing` matches calls by module
id, the part of the id before the last dot. For `import { List } from
"../lib/tasks.go"` the id is `src/lib/tasks.List`, so the module is
`src/lib/tasks`; for a TypeScript host module it is the path without the
extension, which keeps `.host` (`src/lib/notes.host`).

```ts
import { afterEach, expect, it } from 'vitest';
import { mockHost, resetHostMocks } from '@krate/app/testing';
import { visibleTitles } from '../src/pages/titles'; // calls List() from tasks.go

afterEach(resetHostMocks);

it('shows the titles Go returns', async () => {
  mockHost('src/lib/tasks', { List: async () => [{ id: 1, title: 'mocked' }] });
  expect(await visibleTitles()).toEqual(['mocked']);
});
```

## Host calls as resources

A host call is a promise, so it fits `createResource` from `@krate/runtime`,
which integrates with Suspense boundaries:

```tsx
const [tasks, { refetch }] = createResource(undefined, () => List());
// tasks() is undefined while loading; tasks.state and tasks.error report the rest.
```

No adapter is needed; the call carries the resource's abort signal when you
pass it as the last argument (`(_, { signal }) => List({ signal })`).
