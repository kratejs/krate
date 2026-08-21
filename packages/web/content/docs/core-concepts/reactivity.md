---
title: Reactivity
order: 2
---

# Reactivity: Signals, Effects & Memos

Krate's reactivity system is inspired by SolidJS: fine-grained, signal-based,
with no virtual DOM and no diffing.

All primitives are exported from `@krate/runtime`.

## Signals

```tsx
import { createSignal } from '@krate/runtime';

const [count, setCount] = createSignal(0);
```

- `count()` reads the current value.
- `setCount(next)` sets the value and notifies subscribers.
- `setCount(prev => ...)` performs a functional update.

Signals are read inside components and effects; the compiler and runtime track
which effects depend on which signals automatically.

## Effects

```tsx
import { createEffect } from '@krate/runtime';

const dispose = createEffect(() => {
  console.log(`count is ${count()}`);
});
dispose(); // remove the effect
```

An effect re-runs whenever any signal it reads changes. The tracker works via a
context stack, so nested reads inside `createEffect` are captured correctly.

## Memos

```tsx
import { createMemo } from '@krate/runtime';

const doubled = createMemo(() => count() * 2);
doubled(); // memoized until count changes
```

A memo caches its computed value and only recomputes when its dependencies
change. Only the final consumer re-renders — the memo itself is cached.

## Cleanup & mount

```tsx
import { onCleanup, onMount } from '@krate/runtime';

createEffect(() => {
  const timer = setInterval(() => {}, 1000);
  onCleanup(() => clearInterval(timer)); // runs before re-run and on dispose
});

onMount(() => {
  // runs once after the initial synchronous render completes
});
```

## Context

```tsx
import { createContext } from '@krate/runtime';

const ThemeCtx = createContext('light');

// ThemeCtx.Provider — wraps children with a value
// ThemeCtx.useContext() — reads the nearest provider value
// ThemeCtx.defaultValue — fallback when no provider is present
```

## Resources

```tsx
import { createResource } from '@krate/runtime';

const [user, { mutate, refetch }] = createResource(
  () => userId(),                                  // reactive source
  async (id) => fetch(`/api/user/${id}`).then(r => r.json()), // fetcher
);

user()            // current data (or undefined)
user.loading      // boolean
user.error        // error or undefined
user.state        // 'unresolved' | 'loading' | 'ready' | 'error' | 'refreshing'
mutate(prev => ({ ...prev, name: 'new' }))  // optimistic update
refetch()         // trigger a re-fetch
```

Resources re-fetch automatically when their source signal changes and abort
in-flight requests when the source changes mid-flight.

## Compile-time validation

The reactive dependency graph is validated at build time and surfaced as `⚠`
warnings. The compiler detects:

- **Unused signals** — declared but never read or written.
- **Write-only effects** — effects that call setters but read no signals.
- **No-read effects** — effects that read and write nothing (run exactly once).
- **Circular dependencies** — including self-referential feedback loops like
  `setCount(count() + 1)`.

These warnings help you catch bugs before they reach the browser.
