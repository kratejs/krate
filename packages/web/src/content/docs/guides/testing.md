---
title: Testing
order: 10
description: Test Krate apps — build checks, types, and browserless component tests.
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

## Testing API routes

`src/api/*.ts` handlers are ordinary functions. With the embedded QuickJS
runtime they run in-process; for Node semantics, test the compiled
`dist/api/*.js` directly with Node.

## See also

- [Quality Checks](/docs/features/quality-checks/)
- [Error Handling](/docs/guides/error-handling/)
- [Deployment](/docs/guides/deployment/)
