---
title: Testing
order: 13
description: Test Krate apps - build checks, types, and browserless component tests.
---

# Testing

Krate ships no test runner of its own. This guide shows how to test a Krate app
with the tools you already use.

## Compiler-enforced checks

The fastest feedback loop is the compiler itself:

```sh
krate build        # fails on compile errors
krate types        # regenerate route/content types
npx tsc --noEmit   # typecheck the app
krate check        # a11y/SEO/perf quality gates
```

Put these in CI so a bad page can't merge. See
[Quality Checks](/docs/features/quality-checks/) and
[Typed Routes](/docs/features/typed-routes/).

## Unit-testing logic

Component logic is plain TypeScript functions, so you can test it without a
DOM. Extract logic into modules and test with **Vitest**:

```sh
npm i -D vitest
```

```ts
// src/lib/cart.test.ts
import { describe, it, expect } from "vitest";
import { addItem } from "./cart";

describe("addItem", () => {
  it("appends", () => {
    expect(addItem([], { id: "a" })).toHaveLength(1);
  });
});
```

## Testing components

Krate components are functions that return JSX trees, not React elements. You
can call a component and inspect the tree the same way the compiler does, or
render it to a string on the client. For DOM behavior, use a DOM environment:

```sh
npm i -D vitest happy-dom
```

```ts
// vitest.config.ts
import { defineConfig } from "vitest/config";
export default defineConfig({ test: { environment: "happy-dom" } });
```

```ts
import { describe, it, expect } from "vitest";
import { createSignal, createEffect } from "@krate/runtime";

describe("signals", () => {
  it("tracks updates", () => {
    const [n, setN] = createSignal(0);
    const seen: number[] = [];
    createEffect(() => seen.push(n()));
    setN(1);
    expect(seen).toContain(1);
  });
});
```

## End-to-end tests

Build the site and serve the output, then run a browser test (Playwright) or
`curl`/`fetch` assertions against the rendered HTML:

```sh
krate build
krate serve   # serves dist + sidecars
```

Static pages are plain HTML, so most assertions are just string/DOM checks on
`dist/**/index.html`.

## Testing API routes and server actions

Route handlers and server actions are plain functions, so a unit test imports
the module and calls it; no server runs. `@krate/runtime/testing` has two small
helpers for the parts that are not plain: building the `Request` and the
`(request, { params })` call a route gets, and the request context a server
action reads with `cookies()` and `headers()`. It is Node-only; import it from
test files, never from app code.

```ts
// src/api/users/[id].test.ts
import { describe, expect, it } from "vitest";
import { callRoute } from "@krate/runtime/testing";
import * as route from "./[id]";

describe("GET /api/users/:id", () => {
  it("returns the user", async () => {
    const res = await callRoute(route, "GET", "/api/users/7?verbose=1", { params: { id: "7" } });
    expect(res.status).toBe(200);
    expect(await res.json()).toMatchObject({ id: "7" });
  });

  it("creates with a JSON body", async () => {
    const res = await callRoute(route, "POST", "/api/users", { json: { name: "ada" } });
    expect(res.status).toBe(201);
  });
});
```

`callRoute(module, method, pathOrUrl | Request, options)` calls the module's
named export for the method (`GET`, `POST`, ...), like the server does:
`HEAD` is answered from `GET` without a body, `OPTIONS` answers `Allow`, a
default export that takes `(request, context)` handles every method, and a
method with no handler returns `405` with `Allow`. Options are `params` (the
dynamic segments), `headers`, `json` and `body`. The handler is a plain
`(Request, { params }) => Response`, so you can also call it directly:
`await GET(new Request("http://localhost/api/x"), { params: {} })`. A
Next.js `NextRequest` test becomes a plain `Request`; `request.cookies` is a
runtime feature, so pass a `cookie` header (`headers: { cookie: "a=1" }`) and
read it as the route does.

A server action (an export of a `"use server"` file) is an ordinary async
function when you import it in a test. Only `cookies()`, `headers()` and
`getActionContext()` from `@krate/runtime/server` need a request, which
`runAction` provides:

```ts
import { expect, it } from "vitest";
import { runAction } from "@krate/runtime/testing";
import { login } from "./actions";

it("sets the session cookie", async () => {
  const { result, setCookies } = await runAction(() => login(formData), {
    cookies: { theme: "dark" },
    headers: { "user-agent": "vitest" },
  });
  expect(setCookies[0]).toMatchObject({ name: "session", deleted: false });
});
```

`runAction` resolves with the action's return value and every `cookies().set()`
and `delete()` call; an action that throws, including through `redirect()`,
rejects. It does not run the CSRF origin check, argument decoding or the HTTP
layer; cover those with an end-to-end test against `krate start`.

Routes that import native packages or Node built-ins run on the Node sidecar in
production; in a unit test they run in Vitest's Node, so what you import is
what runs. To test the built embedded-runtime artifact instead, load
`dist/.krate/api/<route>.js` (an IIFE exposing `__krate_route`); for the Node
artifact import `dist/.krate/api-node/<route>.js`.

## See also

- [Quality Checks](/docs/features/quality-checks/)
- [Error Handling](/docs/guides/error-handling/)
- [Deployment](/docs/guides/deployment/)
