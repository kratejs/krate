---
title: React Compatibility
order: 5
description: How Krate transpiles React source to signals, runs compiled React packages as Preact islands, and what is intentionally not supported.
---

# React Compatibility

Krate supports React in two ways:

1. **Source transpilation.** Any file that imports from `react` (or uses the
   `React.*` namespace) is rewritten at build time to Krate signals and
   effects. There is no React runtime and no virtual DOM: the compiler rewrites
   imports and calls, then compiles the component like hand-written Krate code.
2. **Islands for compiled packages.** Packages that ship only compiled
   JavaScript and were built against React (for example `@radix-ui/react-*`)
   are bundled into a small, isolated Preact island and hydrate on the client.
   See [Compiled React packages](#compiled-react-packages-islands).

This page covers both paths. For the current status of every surface and the
tests backing it, see the [compatibility matrix](/docs/guides/compatibility/).

## Source transpilation

```tsx
// You write React...
import { useState, useEffect, useRef } from 'react';

export default function Timer() {
  const [seconds, setSeconds] = useState(0);
  const intervalRef = useRef(null);

  useEffect(() => {
    intervalRef.current = setInterval(() => setSeconds(s => s + 1), 1000);
    return () => clearInterval(intervalRef.current);
  }, []);

  return <p>Seconds elapsed: {seconds}</p>;
}
```

```tsx
// ...and the compiler emits the equivalent Krate code.
// useState  -> createSignal      useEffect -> createEffectDeps
// useRef    -> { current: null }  {seconds} -> seconds()
```

### Hooks

| React | Rewritten to |
|-------|--------------|
| `useState` | `createSignal` (a lazy initializer `useState(() => x)` is called once) |
| `useEffect` | `createEffectDeps(fn, () => deps)` (passive phase, see [Effects](#effects)) |
| `useLayoutEffect` / `useInsertionEffect` | `createEffectDeps(fn, () => deps, 'layout' / 'insertion')` |
| `useMemo` | `createMemoDeps(fn, () => deps)`; without deps, `createMemo` |
| `useReducer` | `createReducer` (stable `dispatch`, lazy `init`, Object.is bail-out) |
| `useRef` | `{ current: initial }` |
| `useCallback` | `createCallbackDeps(fn, () => deps)`; without deps, the wrapped function |
| `useContext` | `Context.useContext()` |
| `useId` | a build-time literal (see below) |
| `useTransition` | a pending signal plus an async-aware `startTransition` (see [Suspense](#suspense-lazy-use-and-transitions)) |
| `useDeferredValue` | a deferred getter (`createDeferredValue`) |
| `useSyncExternalStore` | a store getter (`createExternalStore`) |
| `useImperativeHandle` / `useDebugValue` | compat shim |
| `use` | `use(promise)` suspends the nearest `<Suspense>`; `use(Context)` is a context read |

Named imports, aliased imports (`import { useState as state }`), and the
`React.*` namespace all work:

```tsx
import React from 'react';

export default function Counter() {
  const [count, setCount] = React.useState(0);
  return <button onClick={() => setCount(count + 1)}>{count}</button>;
}
```

### Components and helpers

| React | Behavior |
|-------|----------|
| `forwardRef` | lowered: the ref parameter becomes `props.ref`, which binds at the rendered element |
| `ref` as a prop (React 19) | supported: `function Input({ ref, ...p }) { return <input ref={ref} {...p} /> }` needs no `forwardRef` |
| `createRef` | lowered to `{ current: null }` |
| `useImperativeHandle` | attaches the handle before effects run; re-created when a dependency changes, detached on unmount |
| `createContext` | supported, including provider values that change at runtime |
| `createElement` | lowered to `h` |
| `memo` | identity (components already run once) |
| `lazy` | rendered at build time; its hydration code is a separate chunk (see [Suspense](#suspense-lazy-use-and-transitions)) |
| `Suspense` | a real boundary: fallback while anything inside is pending |
| `<Fragment>` / `<React.Fragment>` | a JSX fragment |
| `react/jsx-runtime`, `jsx`, `jsxs`, `jsxDEV` | lowered to Krate JSX |
| `react-dom` / `react-dom/client` | `createPortal` and `flushSync` map to the compat runtime (other entry points are not supported) |
| `Children.only` / `toArray` / `count` | folded at build time on literal input, otherwise compat shim |
| `Children.map` | folded at build time for a literal list and inline arrow, otherwise compat shim |
| `cloneElement` | folded at build time on a literal element and props, otherwise compat shim |
| `isValidElement` | folded at build time on literal input, otherwise compat shim |
| `createPortal` / `<Portal>` | rendered into `document.body` and cleaned up on dispose |
| `Component` / `PureComponent` | lowerable class components (see below) |
| `useOptimistic` / `useActionState` / `useFormStatus` | React 19 compat shims |
| `StrictMode` / `Profiler` | render their children; `Profiler`'s `onRender` is never called (warning KR4005) |

Class components that follow the common subset are lowered to functions:
`state` (field or constructor), `render`, `this.state.x`, `this.props.x`,
`this.setState` (object or updater), `componentDidMount`,
`componentDidUpdate` (state diff), `componentWillUnmount`, `forceUpdate`,
helper methods, arrow-function handlers, and instance fields. Unsupported
lifecycle hooks (`shouldComponentUpdate`, `getSnapshotBeforeUpdate`, derived
state, and error boundaries) produce a specific build diagnostic.

### Bare reads are auto-called

React reads a value directly (`{count}`); Krate signals are getters (`count()`).
The compiler bridges the difference by rewriting **bare value-position reads to
calls**, so unmodified React works:

```tsx
const [count, setCount] = useState(0);

return (
  <div>
    <span>{count}</span>                      {/* -> count()               */}
    <span>{count + 1}</span>                  {/* -> count() + 1           */}
    <span>{`n=${count}`}</span>               {/* -> `n=${count()}`        */}
    <button onClick={() => setCount(count + 1)}>
      {/* -> setCount(count() + 1) */}
      Increment
    </button>
  </div>
);
```

The rewrite is scope-aware: it never touches shadowed locals, function
parameters, setters, or values that are already called. Krate-native
`createSignal` code (which already writes `count()`) is left untouched.

### `useId`

`useId()` resolves to a **stable, per-instance build-time literal** rather than
a runtime counter. The server-rendered id and the hydrated id always agree.

### `useReducer`

`useReducer` maps to `createReducer`, which returns `[getter, dispatch]` -- the
same getter contract as `createSignal`, so it reacts and hydrates like any other
signal.

### Suspense, lazy, use() and transitions

**Boundaries.** `<Suspense fallback={...}>` (also `React.Suspense` or an
aliased import) shows its fallback while any component inside it is suspended
and reveals the content once everything inside has settled. Siblings under one
boundary reveal together, and a suspension counts only for the innermost
boundary around it, so an outer boundary keeps showing its content (with the
inner fallback). The fallback is rendered as normal Krate output and can be
interactive.

At build time a boundary renders its content when the content does not depend
on pending work. When a component inside it reads `use(promise)` or a
`createResource`, the promise cannot be resolved at build time: the page ships
the fallback, and the content waits (already rendered, with its value slots
empty) in a hoisted `<template>`. Hydration binds the content there and swaps
it in when the boundary has nothing pending. Effects of the content run in the
commit after it is revealed, and are cleaned up while the boundary shows its
fallback again (like a hidden conditional branch, see [Effects](#effects)). Request-time (runtime and server)
components inside a boundary keep using streaming regions.

**`use(promise)`.** Write it as the initializer of a declaration in the
component body:

```tsx
function User({ userPromise }) {
  const user = use(userPromise);          // may be conditional: cond ? use(p) : null
  return <p>{user.name}</p>;
}
```

The value is reactive. While the promise is pending the component's hydration
is suspended (its boundary shows the fallback); when it resolves the component
runs again with the value. As in React, the same promise object never suspends
again once it resolved, a rejected promise is reported to the nearest error
boundary, and promises must be cached: a promise created during rendering is
reused by position when the component runs again, like React's replay. If the
promise changes later (for example a new promise in state), the boundary shows
its fallback again, except inside a transition, where the revealed content
stays and the transition stays pending until the new promise resolves.
`use(Context)` is a plain context read and works anywhere; the compiler treats
the argument as a context when it is a module-level `createContext` binding or
its name ends in `Context` or `Ctx`. `use(promise)` used
outside a declaration (KR4003) or destructured (KR4004) is a build error, and a
component calling it outside any `<Suspense>` gets warning KR4008.

**`lazy()`.** Declare lazy components at module level with a static import:

```tsx
const Chart = lazy(() => import('./Chart'));
const Panel = lazy(() => import('./panels').then((m) => ({ default: m.Panel })));
```

Krate renders the static HTML of everything at build time, so splitting the
render code of a lazy component would save nothing: its HTML is in the page
from the first byte. What is split is its **hydration code**. The component's
event handlers and bindings (and those of its children) are written to
`lazy-<Name>.<hash>.js` next to the page script and loaded with a dynamic
import when the page hydrates, which is the first time the component renders
on the client (React's lazy also starts loading on first render). Until the
chunk has run, the content is visible but not interactive, as with React
hydrating server-rendered lazy content; a boundary still showing its fallback
waits for the chunk before revealing. The default export must be a component
(KR4007); a failed chunk load is reported to the nearest error boundary. The
loader must be a static `import()` (KR4001), `lazy()` must not be called inside
a component (KR4002), and a lazy package import (`import('some-package')`) is
compiled like a static import (warning KR4006).

**Transitions.** `useTransition()` returns `[isPending, startTransition]`.
`isPending` becomes true when a transition starts and stays true until the
action settles, including async actions and the work they await, and until
every overlapping transition started through the same hook settled. The
top-level `startTransition` accepts async functions too. Updates made after an
`await` inside an action are ordinary updates, as in React 19; wrap them in
another `startTransition` to mark them.

**`useDeferredValue(value, initialValue?)`** returns a getter that keeps the
previous value during an urgent update and catches up in a later task (a
`setTimeout(0)` after the update). With `initialValue` (React 19) the first
value is `initialValue` and the real value follows the same way. Updates made
inside a transition are not deferred again.

**`useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot?)`** returns
a getter. It subscribes once, re-reads the snapshot on every store
notification and once right after subscribing (so a change between the first
read and the subscription is not missed), stores a new snapshot only when it
differs by `Object.is`, and unsubscribes when the page is torn down. The build
renders `getServerSnapshot` (when it is an inline function) and the client
hydrates from `getSnapshot`. A `getSnapshot` that returns a new object on
every call logs a warning, as React does.

The compat runtime (`@krate/runtime/react`, used by code that is not lowered
at build time) implements the same semantics with render-time placeholders:
a component that suspends renders a placeholder that the nearest `<Suspense>`
claims, and the placeholder re-renders the component when its promise settles.

### Effects

Krate components run once, so there is no render for an effect to follow.
Each effect hook instead watches its dependency array: the compiler passes a
thunk (`() => [a, b]`) that the runtime re-reads whenever a signal it reads
changes, and the effect re-runs in the next commit when any element differs
by `Object.is` (so `NaN` equals `NaN` and `+0` differs from `-0`). Setting
state to the value it already has (by `Object.is`) is a no-op: no binding or
effect re-runs.

**Commit order.** Signal writes are batched: every update made in the same
task (an event handler, a timeout, a promise callback) produces one commit.
A commit runs after the DOM bindings have been updated, in React's order:

1. Mutation phase, per component: insertion-effect cleanup and setup, then
   layout-effect cleanup.
2. Layout phase: layout-effect setups. This happens in a microtask, before the
   browser paints.
3. Passive phase, in a later task (after paint): every passive cleanup, then
   every passive setup.

Within each phase, children run before parents and siblings in document
order; effects of one component run in declaration order. Pending passive
effects are always flushed before the next update is processed, as in React.
A layout effect that sets state therefore re-commits before paint.

**Dependency arrays.**

| Pattern | Behavior |
|---------|----------|
| `[a, b]` | re-runs when `a` or `b` changes |
| `[]` | runs once after mount; its cleanup runs on unmount |
| `const deps = [a, b]` in the same component | inlined, same as `[a, b]` |
| an expression (`props.deps`, `cond ? [a] : [b]`, `[...items, a]`) | evaluated live and compared at run time |
| a parameter or imported array | compared at run time, but the captured array never changes ([KR2001](#effect-diagnostics)) |
| omitted (or `null` / `undefined`) | re-runs after every commit that changes the component's own state or props ([KR2002](#effect-diagnostics)) |

React re-runs a dependency-less effect after every render. Krate has no
re-renders, so it uses the closest observable equivalent: the effect's
dependencies are the component's `useState`/`useReducer` state and its props.
Re-renders caused only by a parent or a context change are not modelled; pass
an explicit array when that matters.

`useMemo(fn, deps)` recomputes synchronously, when it is read, after its deps
changed, so the value is current within the same update. `useCallback(fn,
deps)` keeps one function identity until its deps change (so effects that
list the callback re-run exactly when React's would). `useImperativeHandle`
attaches the handle in the layout phase, re-creates it when its deps change,
and resets the ref to `null` on unmount.

**Unmounting.** When a list re-render removes a component, its cleanups run
parent before child (React's deletion order): insertion and layout cleanups
immediately, passive cleanups in the passive phase. A component inside a
conditional branch that becomes hidden has all of its effect cleanups run,
and when the branch is shown again every effect setup runs again, like React's
`<Activity mode="hidden">`. Unlike React's `{cond && <Child />}`, the
component's state is kept while it is hidden.

**Closures.** Krate state reads are live getters, so code inside an effect
always sees the current value. In a cleanup this differs from React, where a
cleanup sees the values of the render its effect ran for ([KR2007](#effect-diagnostics)):
copy the value into a local in the effect body (`const id = roomId; return ()
=> leave(id);`).

Compiled packages and code the compiler could not lower use the same runtime
through `@krate/runtime/react`: `useEffect`, `useLayoutEffect` and
`useInsertionEffect` get their phases, and dependency elements that are state
getters are read live. There `useMemo` and `useCallback` compute once per
component, which is what React does when the deps never change.

#### Effect diagnostics

| Code | Reported when | What to do |
|------|---------------|------------|
| KR2001 | a dependency array is a binding the compiler cannot resolve | write it inline or as `const deps = [...]` in the component |
| KR2002 | an effect has no dependency array | pass the dependencies, or `[]` to run once |
| KR2003 | an effect callback is `async` (its Promise is not a cleanup) | call an async function inside the effect |
| KR2004 | `useCallback` has no dependency array (one identity for the component's lifetime) | pass the dependencies |
| KR2005 | `useMemo` has no dependency array (recomputes when a signal it reads changes) | pass the dependencies |
| KR2006 | the dependency argument is not an array | pass `[a, b]` or omit it |
| KR2007 | an effect cleanup reads state | copy the value in the effect body |

These are warnings; diagnostics for files under `node_modules` are not
printed.

### Attributes and `style`

Elements and attributes are serialized with the same rules as
`react-dom/server` (React 19), from one table shared by every render path:

- **Names.** React prop names map to their HTML/SVG attributes: `className`
  -> `class`, `htmlFor` -> `for`, `tabIndex` -> `tabindex`, `readOnly`,
  `autoComplete`, `colSpan`, `srcSet`, `contentEditable`, SVG presentation
  attributes (`strokeWidth` -> `stroke-width`, `fillRule`, `clipPath`, ...),
  and the namespaced SVG attributes `xlinkHref` -> `xlink:href`,
  `xmlnsXlink` -> `xmlns:xlink`, `xmlLang`, `xmlSpace`. SVG names that are
  already camelCase in SVG (`viewBox`, `markerHeight`, `preserveAspectRatio`)
  are kept as written.
- **Boolean attributes** (`disabled`, `checked`, `hidden`, `readOnly`, ...)
  render bare when truthy and are omitted when `false`, `null` or
  `undefined`. `hidden="until-found"` is kept. `translate` is enumerated
  (`"yes"`/`"no"`), and `draggable`, `spellCheck` and `contentEditable` render
  `"true"`/`"false"`. `aria-*` and `data-*` stringify booleans.
- **Form values.** `<textarea value>`/`defaultValue` render as the textarea's
  content; `<select value>`/`defaultValue` (an array for `multiple`) mark the
  matching `<option selected>`; `defaultValue`/`defaultChecked` on `<input>`
  become `value`/`checked`.
- **URLs.** A `javascript:` URL in `href`, `src`, `action`, `formAction` or
  `xlinkHref` is replaced, as in React 19, with
  `javascript:throw new Error('React has blocked a javascript: URL as a security precaution.')`.
  The hydration runtime applies the same rule to reactive attribute updates.
  An empty `src` (and an empty `href` other than on `<a>`) is omitted.
- **Text.** JSX text is HTML-escaped, so `<p>&lt;b&gt;</p>` renders the
  characters `<b>`, never a tag. `<script>` and `<style>` children are raw text.
- **Validation.** Invalid element or attribute names, and children on void
  elements (`<br>text</br>`), are build errors. Void elements never get a
  closing tag.

`style={{ ... }}` objects are serialized to a CSS string: camelCase keys become
kebab-case, vendor prefixes keep their leading dash (`WebkitTransition` ->
`-webkit-transition`, `msTransform` -> `-ms-transform`), numbers get `px`
except for unitless properties (`lineHeight`, `opacity`, `zIndex`, `flex`,
`fontWeight`, ...) and custom properties (`--gap`), and `null`, `undefined`,
boolean and empty values are skipped. An object with no declarations omits the
attribute.

```tsx
<div style={{ fontSize: 14, lineHeight: 1.5, '--gap': 4, color: null }} />
// -> <div style="font-size:14px;line-height:1.5;--gap:4">
```

Lowercase `<script>` and `<style>` elements render where they are written
(for example a `<style>` inside an `<svg>`). Use `<Head>`, `<Script>` and
`<Style>` to place content in the document head or body end.

`key` and `suppressHydrationWarning` are compiler-only and never appear in the
output. Event handler props are covered in
[Events, forms and refs](#events-forms-and-refs).

## shadcn/ui

shadcn/ui copies component source into your project, so its components compile
like any other Krate source -- no config, no islands. The patterns shadcn relies
on are handled by the compiler:

- **`cva` (class-variance-authority)** -- a literal `const x = cva(base, config)`
  and calls like `x({ variant, size })` fold to a static class string at build
  time, including `defaultVariants` and `compoundVariants`.
- **`cn` / `clsx` / `tailwind-merge`** -- imported from `@krate/runtime`; a call
  with statically-known arguments folds to a literal class string.
- **Rest-spread props** -- `{...props}` on an intrinsic element expands to the
  call-site attributes, and `children` forwards through it.
- **Defaults** -- `const Comp = asChild ? Slot : "button"` resolves at build time
  from the parameter default.
- **`asChild` / `Slot`** -- a `<Slot>` (or a tag aliased to it) renders its single
  child element with the Slot's props (including `className`) merged onto it.
- **Attributes** -- `data-*`, `aria-*`, and camelCase aliases pass through.

## Compiled React packages (islands)

Some packages ship compiled JavaScript only, built against React's runtime
(`react/jsx-runtime`, `React.*`, and internal packages). Krate cannot transpile
those files, so it bundles the imported component into an **island**: a small
Preact tree that mounts into a placeholder and hydrates on the client.

```tsx
import * as Popover from '@radix-ui/react-popover';

<Popover.Root>
  <Popover.Trigger>Open</Popover.Trigger>
  <Popover.Content>Hello</Popover.Content>
</Popover.Root>
```

How it works:

- A component is treated as an island when its implementation lives under
  `node_modules`. Local components (including path aliases) still compile
  through Krate.
- The subtree is captured as-is and rendered with **Preact**. Preact is not a
  Krate dependency; install it in your app:

  ```sh
  pnpm add preact
  ```

  Preact 10.20 or newer is required. If Preact is missing, the build fails with an actionable error rather than
  shipping a broken island.
- The islands for a page share one chunk (`chunks/island.<hash>.js`). Shared
  Preact modules are emitted once and wired through an import map.
- Signal-valued props are bridged as live getters, and the island re-renders
  when those signals change. Function props (event handlers and signal setters)
  are passed by reference, so page-to-island callbacks work. Plain object and
  string props are captured when the island mounts.
- Island output is **client-only** in v1: the placeholder is server-rendered and
  the island renders after its chunk loads. If rendering throws, the placeholder
  is marked `data-krate-island-error` and shows a fallback message.
- Krate warns when the installed Preact is older than 10.20, which is the
  minimum version the island hooks and compat behavior are tested against.

Two runtimes are involved at an island boundary: the page (Krate signals) and
the island (Preact). Props crossing the boundary may be serializable values,
bridged signal getters, or function references (handlers and setters). A local
Krate component cannot be rendered inside an external subtree -- the build
reports this instead of silently dropping it.

Primitives that keep internal pointer state (for example Radix Tooltip) work
when controlled: bind `open` to a page signal and wire `onOpenChange` plus
explicit pointer/focus handlers on the trigger.

## Events, forms and refs

Event handlers, form controls and refs follow React 18/19 semantics as
observed in a browser. The browser conformance suite (`/react-events/` in
`examples/`) exercises them in a real browser.

### Events

- **Synthetic events.** Handlers receive a React-style event: `type`, `target`,
  `currentTarget` (the element whose handler is running), `nativeEvent`,
  `eventPhase`, `bubbles`, `cancelable`, `defaultPrevented`, `timeStamp`,
  `isTrusted`, `preventDefault()`, `stopPropagation()`,
  `isDefaultPrevented()`, `isPropagationStopped()` and a no-op `persist()`.
  Type-specific fields (`key`, `code`, `repeat`, modifier keys, `button`,
  `clientX`/`pageX`, `relatedTarget`, `deltaY`, `touches`, `dataTransfer`,
  `clipboardData`, `data`, `animationName`, `propertyName`, ...) and
  `getModifierState()` come from the native event. `currentTarget` is `null`
  after the handler returns, as in React.
- **Root delegation.** Like React 17+, bubbling events are delegated to the app
  root (`#root`, else `<body>`) with one capture-phase and one bubble-phase
  listener per event type, installed the first time a handler needs it.
  Capture handlers (`onClickCapture`) run top-down, then bubble handlers run
  bottom-up. Native listeners interleave the same way: a listener added with
  `addEventListener` on an element inside the root runs before React bubble
  handlers, a listener on `document` runs after them.
- **`stopPropagation()`** stops the remaining React handlers and native
  propagation, so `document` listeners do not see the event. Called in a
  capture handler it also skips every bubble handler.
- **Event names.** `onDoubleClick` uses `dblclick`; `onFocus`/`onBlur` bubble
  (they use `focusin`/`focusout`) and report `type` `focus`/`blur`.
  `onMouseEnter`/`onMouseLeave`/`onPointerEnter`/`onPointerLeave`, `onScroll`,
  `onLoad`/`onError`, `onToggle`, `onInvalid` and media events (`onPlay`,
  `onEnded`, ...) do not bubble and are attached to the element.
  `onSubmit`/`onReset`, composition events (`onCompositionStart`/`Update`/`End`)
  and clipboard, drag, touch, wheel, pointer and animation/transition events are
  delegated.
- **`onChange`** follows React: text inputs, textareas and range/number inputs
  fire on every `input` event; checkboxes and radios fire on `click` (only when
  the checked state actually changed, so clicking a checked radio does nothing);
  selects and file inputs fire on `change`. `onChange` runs after `onInput` and
  `onClick` for the same native event.
- **`onBeforeInput`** fires for text insertion (and at the end of an IME
  composition) with `data`, not for deletions, matching React.
- **Errors** thrown by a handler do not stop the other handlers; the first error
  is rethrown after the dispatch.
- Handlers on `window`/`document` registered in `useEffect` stay native
  listeners. Handlers inside a portal moved outside the root keep working.

### Forms

- **Controlled fields.** `value` and `checked` make a field controlled: after
  `onChange` the DOM is restored to the state value, so a handler that does not
  update state (or a field without `onChange`) keeps its value, and a handler
  that transforms the value (`setV(v.toUpperCase())`) shows the transformed
  value immediately. Radio groups restore every radio in the group. `<select
  value>` and `<select multiple value={[...]}>` are controlled the same way.
- **Value attribute sync.** As in React, the `value` attribute of a controlled
  input tracks the current value, so `form.reset()` restores the current
  controlled value.
- **Uncontrolled fields.** `defaultValue`/`defaultChecked` set the initial value
  only. Changing them later updates the default (visible only while the user
  has not edited the field) and never overwrites user input. `form.reset()`
  restores the defaults.
- **`value={undefined}`** (or `null`) leaves the field uncontrolled. Switching a
  field between controlled and uncontrolled logs React's console warning at
  runtime.

### Refs

- **Object refs** (`useRef`, `createRef`) get `current` set to the node before
  any layout effect runs, including refs forwarded to a child component, and
  reset to `null` on unmount (a conditional branch hiding, a list item
  removed, or navigation).
- **Callback refs** are called with the node on mount and with `null` on
  unmount. A React 19 cleanup function returned by the callback is called on
  unmount instead. When the callback's identity changes the old callback is
  detached and the new one attached. Callback refs never subscribe to signals
  they read.
- **`ref` as a prop**, `forwardRef`, and `useImperativeHandle(ref, create,
  deps)` are supported. Refs in lists keep the right element per item.
- Refs to class component instances are not supported yet.

### Build diagnostics

| Code | Reported for |
|------|--------------|
| `KR5001` | an unknown event prop on a built-in element (`onClickcapture`, `onTap`); a "did you mean" hint names the React event |
| `KR5002` | a lowercase event prop given a function (`onclick={fn}`) |
| `KR5003` | a string ref (`ref="field"`), removed in React 19 and ignored |
| `KR5004` | `value`/`checked` without `onChange`, `onInput`, `readOnly` or `disabled` (a read-only field) |
| `KR5005` | `value` with `defaultValue`, or `checked` with `defaultChecked` (the default is ignored) |
| `KR5006` | `value={cond ? x : undefined}`: a field that switches between controlled and uncontrolled |
| `KR5007` | `onChange` on a `contentEditable` element (use `onInput`) |

## What is not supported

- **Running React itself.** There is no React runtime; compiled packages run
  through Preact islands, not React.
- **React 19-only semantics** in compiled packages. Preact's compatibility layer
  is close to React 18 behavior; version-specific APIs may differ.
- **Hook ordering and rules-of-hooks** for transpiled source. Components run
  once per mount, so there is no hook order to preserve.
- **Dynamic `Children.*` / `cloneElement`** inputs that are not statically
  known. Literal inputs fold at build time; dynamic inputs use the compat shim.
- **`Children.forEach`** and dynamic `Children.map` callbacks run in the compat
  shim at runtime, not at build time.

If you rely on unsupported behavior, write the component in Krate's own style --
see [Reactivity](/docs/core-concepts/reactivity/) and
[Migrating from Next.js](/docs/guides/migrating-from-nextjs/).

## Server actions

A file-level `"use server"` module is supported for mutations. Use an imported
action as a form action, or call it from an event handler:

```tsx
import { saveFeedback } from "../actions/feedback";

<form action={saveFeedback}>
  <input name="text" />
  <button type="submit">Save</button>
</form>
```

Forms work without JavaScript and are enhanced by the shared runtime. See
[Server Actions](/docs/guides/server-actions/) for the module format, direct
calls, runtime requirements, and security model.

## Diagnostics and conformance

Compatibility limits are reported with stable diagnostic codes (`KR2001`,
`KR8001`, ...). Every code, with the reason it fires and what to do instead, is
listed in the [diagnostic codes reference](/docs/reference/diagnostics/).

Behavior is checked against React itself. A browser differential runs a corpus
of React 18 source components and a React 19 reference app in both real
React 19 and Krate, drives them with the same clicks and keystrokes in
Chromium, Firefox and WebKit, and compares the DOM, form state, focus and the
order of effects, cleanups, lifecycles and ref callbacks after every step.
Known differences are tracked with a severity in the repository
(`packages/compiler/internal/build/testdata/reactbrowser/divergences.json`).
