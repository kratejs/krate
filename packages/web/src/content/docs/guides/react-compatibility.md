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
| `useTransition` | lowered to a pending signal plus a starter function |
| `useDeferredValue` | compat shim (`@krate/runtime/react`) |
| `useSyncExternalStore` | compat shim |
| `useImperativeHandle` / `useDebugValue` | compat shim |
| `use` | compat shim |

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
| `lazy` | identity passthrough |
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
| `StrictMode` / `Profiler` | pass-through |

### Class components

Classes extending `Component` or `PureComponent` are lowered at build time:
`render()` becomes the component body (so the page is rendered to HTML at
build time), and a small runtime instance provides `this`. The semantics
follow React 18:

- **Updates are batched.** `this.setState(partial | updater, callback)` and
  `this.forceUpdate(callback)` queue an update; `this.state` changes when it
  commits. Updaters run in order with `(state, props)`.
- **Render and commit phases.** An update recomputes props and state,
  applies `static getDerivedStateFromProps(props, state)` (also before the
  first render), asks `shouldComponentUpdate(nextProps, nextState)` (or
  compares shallowly for `PureComponent`), calls
  `getSnapshotBeforeUpdate(prevProps, prevState)` before the DOM changes,
  then writes the DOM. `componentDidUpdate(prevProps, prevState, snapshot)`
  and setState callbacks run after the DOM is updated, children before
  parents. A skipped update still updates `this.props`/`this.state` and runs
  the callbacks, as in React.
- **Props are live.** A parent's state change reaches `this.props` and
  triggers `componentDidUpdate`. A class re-renders when the component that
  created it re-renders (unless it bails out), but not when it was passed in
  as `props.children`.
- **Mounting follows conditionals.** A class inside `{show ? <Child/> : null}`
  mounts when the branch is shown and unmounts (`componentWillUnmount`,
  parent first) when it is hidden; showing it again creates a fresh instance
  with fresh state. Components in an initially hidden branch do not mount
  until it is shown.
- **Also supported:** `static defaultProps`, `static contextType` with
  `this.context`, getters and setters, private `#members`, class fields and
  arrow handlers, instance fields assigned in any method (`this.timer = ...`),
  `componentWillReceiveProps` / `componentWillUpdate` and their `UNSAFE_`
  forms (skipped when `getDerivedStateFromProps` or `getSnapshotBeforeUpdate`
  is defined, as in React), and refs: `<MyClass ref={r}/>` sets `r.current` to
  the instance (with its public methods) after `componentDidMount` and to
  `null` before `componentWillUnmount`.
- **Render locals stay reactive.** `const { count } = this.state` and other
  render locals derived from state or props are recomputed by every binding.

Rejected with a coded warning (the component renders nothing):
`UNSAFE_componentWillMount` (KR3005), legacy `contextTypes` (KR3006), other
static members and static blocks (KR3003), computed member names (KR3004),
`this` inside a static lifecycle (KR3007), and `this.<name>` reads of
properties nothing defines, such as string refs (KR3002).

Pages that render a class component hydrate through an owner tree (one owner
per component instance). The same pages get React's mount/unmount behavior
for function components in conditional branches.

React's `Component`/`PureComponent` from `@krate/runtime/react`, rendered with
`createElement` at runtime, use the same instance controller. Compiled npm
packages run on Preact inside islands and keep Preact's class semantics.

### Error boundaries

A class with `static getDerivedStateFromError(error)` and/or
`componentDidCatch(error, info)` is an error boundary:

```tsx
class ErrorBoundary extends Component {
  state = { error: null };
  static getDerivedStateFromError(error) { return { error: error.message }; }
  componentDidCatch(error, info) { report(error, info.componentStack); }
  render() {
    if (this.state.error) {
      return <button onClick={() => this.setState({ error: null })}>Try again</button>;
    }
    return this.props.children;
  }
}
```

- **What is caught** mirrors React: errors thrown by descendants while they
  hydrate, while a binding re-renders, in a render-time guard
  (`if (this.state.n === 5) throw ...` re-checks on every update), in
  lifecycles and in effects. Errors in event handlers, timers and promises
  are not caught.
- **On an error** the boundary unmounts its whole subtree (cleanups and
  `componentWillUnmount` run for components that had mounted), renders the
  state from `getDerivedStateFromError`, then calls
  `componentDidCatch(error, { componentStack })` after the fallback is in the
  DOM. Caught errors are logged with `console.error`, like React.
- **Recovery.** When the boundary renders its children again (for example
  after `setState({ error: null })`), the children mount fresh.
- **Nesting.** The innermost boundary catches. An error thrown by a
  boundary's own render or fallback goes to the boundary above it.
- **At build time** a `throw` while the page is rendered inside a boundary
  renders the boundary's fallback instead of failing the build (warning
  KR3009), and `componentDidCatch` is called when the page hydrates. Retrying
  after a build-time error renders an empty children region, because the
  subtree could not be built; rebuild once the data is fixed. A build-time
  throw with no boundary fails the build (KR3010).
- **Without `getDerivedStateFromError`** the subtree is unmounted but its
  last DOM stays until `componentDidCatch` calls `setState` (warning KR3008).
- **Uncaught errors.** React 18 unmounts the whole root. Krate keeps the
  server-rendered page, logs the error with its component stack, and unmounts
  only the component that threw (and its descendants).
- Errors in a hidden conditional branch's bindings (which React would never
  have rendered) are ignored. Error boundaries around islands are planned.

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
- Refs to class component instances receive the instance (see Class
  components).

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
