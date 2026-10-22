---
title: Reactivity
description: Signals, effects, memos, context, and async resources in the Krate runtime.
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

## Conditional rendering (`showIf`)

For conditionally rendering an element, Krate supports a `showIf` prop — sugar
for the `{expr && <el/>}` guard:

```tsx
const [count] = createSignal(0);

// These are equivalent:
{count() > 0 && <p>Positive</p>}
<p showIf={count() > 0}>Positive</p>
```

- Works on intrinsic elements (`<div>`) **and** components (`<List>`).
- `visibleIf` is accepted as an alias.
- A bare attribute (`<p showIf>`) is always true.
- The attribute is compiler-erased: it never appears in the emitted HTML and is
  never passed to a component's props.
- A statically-known test folds at build time (the losing branch is not
  emitted); a reactive test becomes part of the hydration bundle and toggles the
  element's visibility as the signal changes.

## Passing signals as props

A prop whose value reads a signal stays live in the child. The compiler passes
it as a getter, so every `props.<name>` read (in rendered JSX, effects, memos
and handlers) sees the current value:

```tsx
function Badge(props: { label: string }) {
  return <span>{props.label}</span>; // updates when count changes
}

export default function Page() {
  const [count, setCount] = createSignal(0);
  return <Badge label={`clicked ${count()} times`} />;
}
```

Destructuring reads the value once: `function Badge({ label })` sees only the
initial value of a signal-derived prop, and the build warns about it. Keep the
`props` parameter (and read `props.label`), or pass an accessor
(`label={() => ...}`) and call it in the child.

Custom hooks are plain functions: a call such as `useTitle("Home")` runs once
per component instance during hydration, in source order with the rest of the
component body.

## Render values in React source components

Krate components run once. A component written in React style (a module that
imports from `react`) relies on its body running on every render instead, so
the compiler turns the body's render-dependent values into **derived values**:

```tsx
const [raw, setRaw] = useState("12");
const value = Number(raw);                    // krateDerived(() => Number(raw()))
const label = value > 1 ? "items" : "item";   // derived from a derived value
let cls = "tab";
if (active) cls += " on";                     // folded into cls's computation
```

- A top-level body binding becomes derived when its initializer reads state,
  a memo, `props.x`, a context value, a custom hook result or another derived
  value. Everything else (constants, closures, refs) is left as written, so it
  costs nothing at runtime.
- Every read of the binding (text, attributes, lists, conditions, handlers,
  effects and dependency arrays) sees the current value. A derived value is
  computed lazily on read and recomputed only after one of its inputs changed,
  so every reader in an update sees the same value.
- An object or array literal built in the body (`const options = { page }`)
  also gets a new identity whenever the component's own state or props change,
  as it would on a React re-render, so effects that list it as a dependency
  re-run the same way.
- A `let` assigned by later body statements is computed together with those
  statements. When that is not possible (the statements also write another
  variable, or a handler reassigns the binding) the value keeps its first
  value and the build warns with `KR8301`.
- Custom hooks that return render-dependent values return a derived getter;
  `const [on, toggle] = useToggle()` and `const counter = useCounter()` read
  the current result.

Krate-native modules (no React import) keep run-once semantics: write
`createMemo` or call the getters yourself.

## CSS Signals

Interactive state can be expressed with **zero client JavaScript** using CSS
Signals (`createCSSChoice`, `createCSSToggle`, `createCSSFlags`,
`createCSSGroup`, `createCSSRange`, `createCSSStack`) � compiled to hidden
inputs plus `:has()` CSS. They are described in full in their own guide.

- [CSS Signals guide](/docs/core-concepts/css-signals/) � concepts, both authoring
  styles, composition, roles, browser support, and limits.
- [CSS Signals API reference](/docs/reference/css-signals/).
- [Cookbook](/docs/guides/css-signals-cookbook/) and [errors](/docs/guides/css-signals-errors/).

## Compile-time validation

The reactive dependency graph is validated at build time and surfaced as `⚠`
warnings. The compiler detects:

- **Unused signals** — declared but never read or written.
- **Write-only effects** — effects that call setters but read no signals.
- **No-read effects** — effects that read and write nothing (run exactly once).
- **Circular dependencies** — including self-referential feedback loops like
  `setCount(count() + 1)`.

These warnings help you catch bugs before they reach the browser.