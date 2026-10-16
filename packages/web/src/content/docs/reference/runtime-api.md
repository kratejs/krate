---
title: Runtime API
description: Every export from @krate/runtime.
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

#### Persistent state (`persist`)

Pass a `persist` option to survive reloads. The value is read from storage when
the signal is created and written on every change. This is SSR-safe (storage is
only touched in the browser) and syncs across tabs by default.

```typescript
// localStorage under the key "count"
const [count, setCount] = createSignal(0, { persist: 'count' })

// sessionStorage, or full control
const [theme, setTheme] = createSignal('light', {
  persist: {
    key: 'theme',
    store: 'session',            // 'local' (default) | 'session' | 'memory' | Storage
    serialize: JSON.stringify,   // optional
    deserialize: JSON.parse,     // optional
    sync: false,                 // cross-tab sync (default true for 'local')
  },
})
```

If storage is unavailable (private mode, quota exceeded) the signal falls back
to an in-memory store instead of throwing; clear a stored value with
`clearPersisted`.

#### Durable SSR state

When a page embeds `window.__KRATE_STATE__` (a JSON object keyed by persistence
key), that server-chosen value takes precedence over browser storage on the
first hydration, so the hydrated value matches what the server rendered. A
project can provide it with a `krate.state.json` file at the root, which the
build injects into every page.

### `createCSSChoice` / `createCSSToggle` / `createCSSFlags`

Zero-JS state compiled to hidden inputs + `:has()` CSS. `createCSSChoice` is a
radio group, `createCSSToggle` a single checkbox, and `createCSSFlags`
independent checkboxes. None of them ship client JavaScript; a declaration that
cannot be compiled is a build error (use `createSignal` instead).

```typescript
const [tab, setTab]   = createCSSChoice('overview')            // options inferred
const [tab, setTab]   = createCSSChoice('a', ['a', 'b', 'c'])  // explicit options
const [dark, setDark] = createCSSToggle(false)
const [flags, setFlag] = createCSSFlags(['bold', 'italic'])     // flags.bold()
```

### `createCSSGroup`

An optional radio group with an explicit closed (`null`) state — accordions,
disclosures, dialogs, popovers, dropdown menus.

```typescript
const [open, setOpen] = createCSSGroup(null, { as: 'accordion' })
setOpen('faq'); setOpen(null)
```

### `createCSSRange`

A discrete integer range — steppers, stepped sliders, progress indicators.
`set(n() +/- 1)` compiles to bounded stepper labels.

```typescript
const [step, setStep] = createCSSRange(0, { min: 0, max: 4, step: 1 })
```

### `createCSSStack`

A drill-down navigation stack over a declared tree; `push` targets are literals.

```typescript
const [stack, { push, pop, clear }] = createCSSStack(['root'])
stack.top(); push('settings'); pop(); clear()
```

### Live text & `vars`

Reading a getter as text (`{tab()}`) compiles to a zero-JS live value backed by
the inherited `--krate-current` property. `vars` publishes more custom
properties per option (values verbatim; quote strings for `content:`).

```typescript
const [plan, setPlan] = createCSSChoice('solo', ['solo', 'pro'])
<p>Plan: {plan()}</p>

const [tier, setTier] = createCSSChoice('solo', {
  options: ['solo', 'pro'],
  vars: { '--price': { solo: '"$9"', pro: '"$29"' } },
})
```

### ARIA options (`as`)

Every CSS primitive accepts an options object with an `as` role preset
(`radio`/`tabs`/`listbox`/`menu`/`accordion`/`disclosure`/`dialog`/`popover`/
`switch`), plus `label` and raw `aria` overrides. Native presets stay zero-JS;
`tabs`/`listbox`/disclosure presets inject a tiny ARIA synchroniser (no page
hydration bundle).

```typescript
const [tab, setTab] = createCSSChoice('a', { as: 'tabs' })
const [q, setQ]     = createCSSGroup(null, { as: 'dialog', aria: { label: 'Query' } })
const [t, setT]     = createCSSChoice('a', { as: 'tabs', aria: false }) // opt out
```

See [CSS Signals](/docs/core-concepts/reactivity/#css-signals).

### `createReducer`

A `createSignal` whose setter applies a reducer. Returns `[getter, dispatch]`,
so the getter reads like any other signal (and hydrates identically). This is
also the target of React's `useReducer`.

```typescript
const [count, dispatch] = createReducer((state, action) => state + action, 0)
count()          // read current state
dispatch(1)      // reduces: state = reducer(state, action)
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

Every JSX element accepts a `showIf` prop (alias `visibleIf`) for conditional
rendering — sugar for `{expr && <el/>}`, compiled away at build time:

```tsx
<div showIf={count() > 0}>Shown when count is positive</div>
```

With the zero-JS CSS primitives, `showIf` also accepts compound boolean logic
over state — `&&`, `||`, `!`, `===` / `==`, `!==` / `!=` — across one or more
scopes, compiled to `:has()` selector chains:

```tsx
<div showIf={plat() === 'mac' && licensed()}>Approve</div>
<div showIf={plat() === 'win' || !licensed()}>Upgrade</div>
```

Equivalent expressions (commuted operands, De Morgan) dedupe to one wrapper
class; unclassifiable expressions remain a build error.

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

Programmatic navigation:

```typescript
import { navigate } from '@krate/runtime'
navigate('/about', { replace: false, scroll: true })
```

`navigate` returns `false` when handled client-side and `true` when it fell back
to a full page load (router not initialized, or a cross-origin URL). The router
also emits `krate:navigate-start` before the DOM mutation and
`krate:navigate-end` after it — a pairing that powers View Transitions and
progress UI — plus `krate:navigate` after history/scroll updates. Elements with
`data-view-transition="name"` are given a matching `view-transition-name`, and
the router wraps the mutation in `document.startViewTransition` when supported
(honoring `prefers-reduced-motion`).

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

### Class helpers & `Slot`

Dependency-free implementations of the helpers shadcn/ui-style components use.
Literal `cn`/`clsx`/`cva` calls are folded by the compiler; these runtime
versions cover dynamic cases.

```typescript
import { cn, clsx, cva, twMerge, Slot, cloneElement } from '@krate/runtime'

cn('p-4', 'px-2')                       // 'px-2 p-4' (later utilities win)
clsx('a', cond && 'b', { c: true })     // 'a c'
const b = cva('base', { variants: { size: { sm: 'h-8', lg: 'h-10' } } })
b({ size: 'lg' })                       // 'base h-10'
Slot({ children })                      // merges props onto a single child
cloneElement(node, { class: 'x' })      // apply props to an existing node
```

`Slot` implements the `asChild` mechanic: it renders its single child element
instead of wrapping it, merging the Slot's props (including `class`) onto it.

## React compatibility

The compiler rewrites React hooks and `React.*` calls to the primitives above at
build time, so React source runs unchanged (no React runtime is shipped). In
particular:

- `useState` → `createSignal`, `useEffect` → `createEffect`,
  `useMemo` → `createMemo`, `useReducer` → `createReducer`.
- `useRef` → `{ current: initial }`, `useCallback` → the function,
  `useContext` → `Context.useContext()`.
- `memo`/`lazy` → identity, `createElement` → `h`,
  `<Fragment>` → a JSX fragment.
- `useId` → a stable per-instance build-time literal.
- Bare reads (`{count}`) are auto-called (`count()`).

See the [React Compatibility guide](/docs/guides/react-compatibility/) for the
full list and the intentionally unsupported cases.

## Type exports

```typescript
import type {
  Component, PropsWithChildren, ComponentProps,
  RefCallback, Context, ResourceReturn, ResourceActions,
} from '@krate/runtime'
```
