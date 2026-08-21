---
title: Runtime API
order: 2
---

# Runtime API

Everything the client runtime exports from `@krate/runtime`.

## Reactive primitives

### `createSignal`

```typescript
const [getValue, setValue] = createSignal(initialValue)
getValue()           // read current value
setValue(next)        // set value (triggers subscribers)
setValue(prev => ...) // functional update
```

### `createEffect`

```typescript
const dispose = createEffect(() => { /* re-runs when deps change */ })
dispose()            // remove the effect entirely
```

### `createMemo`

```typescript
const value = createMemo(() => expensiveCalc(input()))
value()              // read memoized value
```

### `onCleanup`

```typescript
onCleanup(() => { /* runs before effect re-execution and on dispose */ })
```

### `onMount`

```typescript
onMount(() => { /* runs once after the initial render */ })
```

### `forwardRef`

```typescript
const FancyInput = forwardRef((props, ref) => {
  ref((el: Element) => el.focus());
  return h('input', props);
});
```

Forwards a `ref` callback through to the underlying DOM element.

### `disposeAll`

```typescript
disposeAll()
```

Disposes every live effect (used internally on SPA navigation and in test
harnesses).

## DOM helpers

### `h` — hyperscript

```typescript
const el = h('div', { class: 'foo', onClick: () => {} }, child1, child2)
// Functions as children become effects:
//   h('span', null, () => signal())
```

### `mount`

```typescript
mount(() => h(App, null), '#root')
```

### `hydrate`

```typescript
hydrate(() => h(App, null), '#root')
```

Hydrates SSR content, binding effects to existing DOM via `data-k`/`data-kh`
attributes.

### `insert` / `clearNodes`

```typescript
insert(parent, value, startMarker)   // replace content between two <!--k--> markers
clearNodes(start, end)               // clear nodes between two markers
```

Low-level helpers used by the compiled hydration code to manage dynamic
regions delimited by `<!--k-->` comment markers.

## JSX runtime

```typescript
import { jsx, jsxs, Fragment } from '@krate/runtime/jsx-runtime'
```

Automatic JSX transform — use `<div>` syntax in TSX files.

## SPA router

```typescript
import { initRouter } from '@krate/runtime'
initRouter()
```

Call once to enable client-side navigation via `data-krate-link` anchors and
`<Link>`. Handles `pushState`, `popstate`, stylesheet diffing, and tree
reconciliation. On navigation the router diffs the live content root against
the parsed new page via `reconcileTrees` — unchanged nodes (keyed by
`<!--k:-->` comment markers, `data-k` attributes, and `id` anchors) are kept in
place so their state (focus, scroll, media, CSS animations) survives. Effects
are disposed before the diff and stale `__krate_*` handler props are stripped;
the new page's hydration JS then rebinds the kept nodes. Emits a
`krate:navigate` CustomEvent after each navigation.

`reinitRouter()` tears down and re-registers the router listeners — useful
after a full page replacement or for HMR in dev.

## Context

```typescript
import { createContext } from '@krate/runtime'

const ThemeCtx = createContext('light')
// ThemeCtx.Provider — wraps children with a context value
// ThemeCtx.useContext() — reads nearest Provider value
// ThemeCtx.defaultValue — fallback when no Provider
```

## Resources

```typescript
import { createResource } from '@krate/runtime'

const [user, { mutate, refetch }] = createResource(
  () => userId(),                          // reactive source
  async (id) => fetch(`/api/user/${id}`).then(r => r.json())  // fetcher
)

user()          // current data (or undefined)
user.loading    // boolean
user.error      // error or undefined
user.state      // 'unresolved' | 'loading' | 'ready' | 'error' | 'refreshing'
mutate(prev => ({ ...prev, name: 'new' }))  // optimistic update
refetch()       // trigger re-fetch
```

## Type exports

```typescript
import type {
  Component, PropsWithChildren, ComponentProps,
  RefCallback, Context, ResourceReturn, ResourceActions,
} from '@krate/runtime'
```
