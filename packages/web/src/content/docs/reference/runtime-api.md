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

### Conditional markup and `krateCondSwap`

A reactive conditional (`{open() ? <A/> : <B/>}`, `{open() && <A/>}`) renders
no wrapper element. The active branch sits between comment markers and the
inactive branch is carried in a `<template>` placed after the page content:

```html
<ul><li>a</li><!--k:c1--><li>shown</li><!--/k:c1--></ul>
...
<template data-kc="c1" data-kb="a"><li>hidden</li></template>
```

Conditionals are therefore valid inside `<table>`, `<tbody>`, `<tr>`, `<ul>`,
`<ol>`, `<select>` and `<p>`, and CSS child selectors (`li:last-child`,
`tr + tr`) see only the real children. Hydration binds handlers and nested
slots in both branches; `krateCondSwap(startMarker, show, template)` moves the
bound nodes between the markers and a detached fragment when the condition
changes, so state in either branch survives toggling. Marker ids and list keys
are percent-encoded (`%`, `<`, `>`) so user data cannot close a comment.

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

The router initializes itself from every page's hydration script
(`initRouter()` / `reinitRouter()` are idempotent). It intercepts clicks on
`<Link>` anchors (`data-krate-link`) and same-origin `GET` form submissions,
fetches the target document and swaps it in place. Shared layouts stay
mounted (see [Routing & Layouts](/docs/core-concepts/routing/#layout-persistence)).

```typescript
import { krateRouter, navigate } from '@krate/runtime'

await krateRouter.push('/about', { scroll: true, state: { from: 'nav' } })
await krateRouter.replace('/search?q=krate')
krateRouter.back()
krateRouter.forward()
await krateRouter.refresh()          // re-fetch the current route, keep scroll + history
await krateRouter.prefetch('/pricing') // warm the document and its page chunk

navigate('/about', { replace: false, scroll: true }) // returns false when handled client-side
```

Every navigation method resolves once the new route's DOM and URL are in place
(page scripts may still be loading). With `spa: false` the same API performs
full-page navigations (`refresh()` reloads, `prefetch()` resolves immediately).

| API | Description |
|-----|-------------|
| `krateRouter.push/replace(url, opts)` | Client navigation. `opts`: `scroll` (default `true`), `state` (stored on the history entry). |
| `krateRouter.back()/forward()` | History traversal. |
| `krateRouter.refresh()` | Re-fetches the current route bypassing the prefetch cache; scroll position and history are kept. |
| `krateRouter.prefetch(url)` | Fetches and caches a same-origin document. |
| `subscribeNavigation(fn)` | Calls `fn({ url, pathname, search, hash, status })` on URL and loading-status changes; returns an unsubscribe function. `pathname` excludes the base path. |
| `getNavigationState()` | The current value delivered to subscribers. |
| `getHistoryState()` | The `state` passed to the navigation that created the current entry. |
| `invalidatePrefetchCache(url?)` | Drops one prefetched document, or all of them. |

**Events** (on `window`, `detail` in parentheses):

- `krate:navigate-start` (`url`, `from`, `kind`): before fetching. Cancelable for
  `push`/`replace`/`refresh` (call `preventDefault()` to veto the navigation).
- `krate:navigate` (`url`, `status`): after the DOM swap, history and scroll update.
- `krate:navigate-end` (`url`, `status`): after the new page scripts ran.
- `krate:navigate-error` (`url`, `status`, `error`): a non-2xx response
  (`status`), a network failure (`status: 0`, cancelable: `preventDefault()`
  keeps the current page instead of the hard-reload fallback), or a page chunk
  that failed to load (`error: "script-load"`, followed by a full load).
- `krate:revalidate`: dispatch it (server actions that call `revalidatePath` /
  `revalidateTag` do) to clear the prefetch cache and `refresh()` the current route.

**Behavior details**

- **Scroll restoration** is keyed by history entry: back/forward restores the
  position the entry had when you left it; new navigations scroll to the top
  or to the URL hash. `scroll: false` / `<Link scroll={false}>` keeps the position.
- **Hash links** to the current page scroll in place without a fetch; back/forward
  between hash entries never refetches.
- **Redirects** are followed: the address bar shows the final URL. A response that
  is not HTML (a download, JSON) or a redirect to another origin falls back to a
  full navigation.
- **Error statuses**: a server-rendered error document (`404`, `500`, ...) is shown
  in place, keeping the SPA alive. Without an HTML body, `404`/`410` load
  `<basePath>/404.html` and `5xx` load `<basePath>/500.html`; other statuses
  (`401`, `403`, `429`) do a full navigation so the browser shows the real response.
- **Prefetch cache**: documents prefetched by hover, viewport or `prefetch()` stay
  fresh for 30 seconds (`window.__KRATE_CFG__.prefetchTTL`), are used at most once,
  and are bounded (LRU). `refresh()` and `krate:revalidate` bypass it.
- **Races**: a newer navigation aborts the older fetch; a superseded response never
  replaces newer content.
- **Accessibility**: after a navigation focus moves to `#root` (its temporary
  `tabindex` is removed on blur) unless it is inside a kept layout, and a
  live region (`#__krate-route-announcer`) announces the new page title.
- **Active links**: the link to the current path gets `aria-current="page"` and
  `data-krate-active="exact"`; links to an ancestor segment (`/docs` on
  `/docs/intro`, never `/docs-old`) get `data-krate-active="segment"`.
- **Forms**: same-origin `GET` forms navigate client-side with the form fields as
  the query string. Add `data-krate-reload` to a form to opt out; forms with file
  inputs are left to the browser.
- **View Transitions**: elements with `data-view-transition="name"` get a matching
  `view-transition-name`, and the swap runs inside `document.startViewTransition`
  when supported (honoring `prefers-reduced-motion`). History, scroll and focus
  update after the transition's update callback ran.

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
