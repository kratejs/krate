---
title: React Compatibility
order: 4
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
   are bundled into a small, isolated Preact island, server-rendered where possible, and hydrated in place.
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
| `useState` | `createSignal(init, krateHookState)` (React state snapshots; a lazy initializer `useState(() => x)` or `useState(loadFn)` is called once) |
| `useEffect` | `createEffectDeps(fn, () => deps)` (passive phase, see [Effects](#effects)) |
| `useLayoutEffect` / `useInsertionEffect` | `createEffectDeps(fn, () => deps, 'layout' / 'insertion')` |
| `useMemo` | `createMemoDeps(fn, () => deps)`; without deps, `createMemo` |
| `useReducer` | `createReducer(reducer, init(arg), krateHookState)` (stable `dispatch`, Object.is bail-out) |
| `useRef` | `{ current: initial }` |
| `useCallback` | `createCallbackDeps(fn, () => deps)`; without deps, the wrapped function |
| `useContext` | `Context.useContext()` |
| `useId` | a per-instance id shared by the HTML and the hydration code (see [`useId`](#useid)) |
| `useTransition` | a pending signal plus an async-aware `startTransition` (see [Suspense](#suspense-lazy-use-and-transitions)) |
| `useDeferredValue` | a deferred getter (`createDeferredValue`) |
| `useSyncExternalStore` | a store getter (`createExternalStore`) |
| `useImperativeHandle` / `useDebugValue` | compat shim |
| `useEffectEvent` | a stable function over live state that does not subscribe its caller (see [`useEffectEvent`](#useeffectevent)) |
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
| `createContext` | supported: `<Ctx.Provider value>`, React 19 `<Ctx value>`, `<Ctx.Consumer>{(v) => ...}</Ctx.Consumer>`, any constant default (objects included), provider values that change at runtime, and nested or self-nesting providers (see [Context](#context)) |
| `createElement` | lowered to `h` |
| `memo` | identity (components already run once) |
| `lazy` | rendered at build time; its hydration code is a separate chunk (see [Suspense](#suspense-lazy-use-and-transitions)) |
| `Suspense` | a real boundary: fallback while anything inside is pending |
| `<Fragment>` / `<React.Fragment>` | a JSX fragment |
| `react/jsx-runtime`, `jsx`, `jsxs`, `jsxDEV` | lowered to Krate JSX |
| `react-dom` / `react-dom/client` | `createPortal` and `flushSync` map to the compat runtime; `useFormStatus` and `requestFormReset` are supported; `findDOMNode`, `render`, `hydrate` and `unmountComponentAtNode` (removed in React 19) report KR8001 |
| `Children.only` / `toArray` / `count` | folded at build time on literal input, otherwise compat shim |
| `Children.map` | folded at build time for a literal list and inline arrow, including `props.children` of a call site written with literal elements (Tabs / RadioGroup); otherwise compat shim |
| `cloneElement` | folded at build time on a literal element and props, otherwise compat shim |
| `isValidElement` | folded at build time on literal input, otherwise compat shim |
| `createPortal` / `<Portal>` | mounted into its container after hydration (see [Portals](#portals)) |
| `<title>` / `<meta>` / `<link>` (React 19) | hoisted into `<head>` (see [Document metadata](#document-metadata)) |
| `Component` / `PureComponent` | lowerable class components (see below) |
| `useActionState` | lowered to signals: `[state, formAction, isPending]`, sequential queue, previous state sent to server actions, no-JS fallback with permalink |
| `useOptimistic` | lowered to signals: updates apply while an action is pending and revert when it settles |
| `useFormStatus` | form-scoped status (`pending`, `data`, `method`, `action`) of the form containing the component |
| `<form action={fn}>` / `formAction={fn}` | server actions and client functions, run in an async transition without a page reload, form reset on success |
| `StrictMode` / `Profiler` | render their children; `Profiler`'s `onRender` is never called (warning KR4005) |
| `<Activity mode>` | children render in a wrapper that hides them while keeping their state; effects clean up while hidden (see [`Activity`](#activity)) |
| `<ViewTransition>` | names its element child for view transitions (see [`ViewTransition`](#viewtransition)) |
| `<SuspenseList>` | renders its children; `revealOrder` and `tail` are ignored (warning KR8031) |
| `cache` | identity: Krate renders each page once |
| `flushSync` | `flushSync(fn)` is `fn()`: state changes already apply synchronously |
| `useFormState` (react-dom) | `useActionState` |
| `preload`, `preinit`, `preconnect`, `prefetchDNS`, `preloadModule`, `preinitModule` (react-dom) | add the `<link>` or `<script>` to the document head once, on the client |
| `version`, `captureOwnerStack` | `"19.2.0"` and `null` (as in production React) |
| custom element props | strings, numbers and booleans are attributes; objects, arrays and functions are properties (see [Custom elements](#custom-elements)) |

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
  until it is shown. `cond ? <Child a/> : <Child b/>` is one instance whose
  props switch (see [Effects](#effects)).
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
per component instance) that routes errors to boundaries and mounts and
unmounts components with their conditional branch, with the same fresh-state
remount semantics as other pages. A class's `render()` body is evaluated once
per mount (bindings update afterwards), so side effects inside `render()`, such
as logging, run fewer times than in React.

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
  after `setState({ error: null })`), the children mount fresh. Changing a
  class element's `key` (`<ErrorBoundary key={attempt}>`) unmounts it and
  mounts a new instance with fresh state.
- **Render retry.** As in React 18, an error thrown while rendering during an
  update is retried once before the fallback commits, so
  `getDerivedStateFromError` runs twice for it (once for errors in effects and
  lifecycles).
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
  have rendered) are ignored. Each Preact island has its own error boundary;
  a render failure shows a fallback in the island placeholder and sets
  `data-krate-island-error`.

### Context

Context is resolved where a component is rendered, as in React: a read sees the
nearest provider above it, including providers that a parent component wraps
around `{props.children}` (`<Level><Level /></Level>` nests). A provider whose
value is a build-time constant is folded into the static HTML; a value that
reads state is passed to the readers as a getter, so every consumer updates
when it changes. Without a provider, readers get the `createContext` default.

```tsx
const Theme = createContext("light");

function ThemeBox(props) {
  const [dark, setDark] = useState(false);
  return (
    <Theme value={dark ? "dark" : "light"}>
      <button onClick={() => setDark(!dark)}>flip</button>
      {props.children}
    </Theme>
  );
}

<ThemeBox>
  <Theme.Consumer>{(theme) => <p>{theme}</p>}</Theme.Consumer>
</ThemeBox>
```

A `Consumer` child must be a single render function with a plain parameter
(or a `{ a, b }` pattern), otherwise the build reports KR8004. Legacy context
(`contextTypes`, `childContextTypes`, `getChildContext`) was removed in
React 19 and reports KR8005.

### Portals

`createPortal(children, container)` (and the built-in `<Portal container>`)
renders its children in the component's place in the tree and moves them into
the container once the page hydrates; like React, the portal content is not
part of the server-rendered HTML. The container can be `document.body`,
`document.getElementById("id")`, `document.querySelector("selector")` or any
expression the component can evaluate in the browser (a container that cannot
be resolved reports KR8006 and falls back to `document.body`). React events
bubble from portal content to the component's ancestors, a portal inside a
conditional branch mounts and unmounts with the branch (`{open &&
createPortal(...)}` and `{open ? createPortal(...) : null}` both work), and its
content is removed when the component goes away. A `createPortal` call anywhere
else, such as inside a `.map()` callback, is not lowered and reports KR8010.

### Document metadata

As in React 19, a `<title>`, `<meta>` or `<link>` rendered anywhere in a
component is placed in the document `<head>`. A `<title>` that reads state
keeps `document.title` up to date; reactive `<meta content>` and `<link href>`
update in place. Identical elements (the same stylesheet rendered twice) are
emitted once, and `<link rel="stylesheet">` is hoisted only with a
`precedence`, as in React. Elements inside `<svg>`, with an `itemProp`, or
inside a conditional branch or list item stay where they are rendered.
`<Head>` keeps working and is the way to set head content from layouts.

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

### Values computed during render

React recomputes everything in a component body on each render. Krate runs the
body once, so a body value that depends on state, props or context is compiled
to a lazily recomputed **derived value**, and its reads are auto-called like
state reads:

```tsx
const [todos, setTodos] = useState(initial);
const [filter, setFilter] = useState("all");
const visible = todos.filter((t) => filter === "all" || t.done === (filter === "done"));
const left = todos.filter((t) => !t.done).length;   // always current
const label = left === 1 ? "item" : "items";
```

This covers plain `const`/`let` bindings, destructuring (`const { a, b } =
obj`), values derived from other derived values, `props.x` reads in components
that take a `props` parameter, and a `let` built up by later body statements
(`let cls = "a"; if (on) cls += " b";`). Values that read nothing reactive stay
plain constants. A binding that cannot be recomputed safely (reassigned inside a
handler, or built by statements that also write other variables) keeps its
first value and is reported as `KR8301`. See
[Reactivity](/docs/core-concepts/reactivity/#render-values-in-react-source-components).

### Custom hooks

A custom hook is an ordinary function that runs once per component instance,
so its own state and effects belong to that instance. When it returns a value
that depends on its state (`return [on, toggle]`, `return { value, setValue }`),
the result is a derived getter, and the call site reads it like any other
derived value:

```tsx
function useToggle(initial: boolean) {
  const [on, setOn] = useState(initial);
  const toggle = useCallback(() => setOn((v) => !v), []);
  return [on, toggle] as const;
}

const [open, toggleOpen] = useToggle(false); // open is current, toggleOpen works
```

A project custom hook (declared in the module or imported from a relative
path) called with an argument that reads state or props gets that argument as
a live getter; the hook's plain parameters read through it, so
`useDebounce(value, 300)` and `useDocumentTitle("count " + n)` follow `value`
and `n`. A destructured hook parameter is read once, when the hook is called.
Hooks from packages are called with plain values. A hook that bumps a state it
never reads (`const [, force] = useState(0)` in a store hook) recomputes its
result when the bump happens.

An `if` at the top of a component or hook body that calls a state setter
(`if (props.value !== prev) { setPrev(props.value); setChanges(changes + 1); }`,
React's "derive state from props" pattern) runs as an effect that re-checks the
condition when the state or props it reads change.

### Component patterns

These React patterns compile to live DOM, not a one-time snapshot:

- **Destructured props** (`function Label({ value, tone = "x", ...rest })`)
  follow call-site changes; see
  [Passing signals as props](/docs/core-concepts/reactivity/#passing-signals-as-props).
- **Function as children and render props**: `<Toggle>{(on, flip) => ...}</Toggle>`
  passes the function as `children`, and `props.children(on, flip)` /
  `props.render(n)` render its result. Their markup is created in the browser,
  so the server HTML of that slot is empty.
- **`Children.map(props.children, (c, i) => cloneElement(c, {...}))`** unrolls
  over the elements the caller wrote. A child whose props read the caller's
  state is reported (the unrolled element would move into the wrong scope).
- **Higher-order components**: `const Counted = withCounter(Button)` where
  `withCounter` declares one inner function component and returns it compiles
  as that component with the wrapped component substituted. Wrappers with other
  statements, patterns or rest parameters are reported as unresolved.
- **Spread props**: `<Inner {...props} />`, `<input {...field} />` and
  conditional spreads apply their attributes and handlers, and re-apply them
  when the state they read changes. A component spread whose attributes are not
  known at build time (an arbitrary object) is reported and not passed.
- **JSX held in a variable** (`const badge = on ? <b/> : <i/>`, a `let content`
  assigned in a `switch`) renders and updates like an inline conditional.
- **`{n && <b/>}`** renders `0` for `n === 0` like React, and `{name || <i/>}`
  renders `name` when it is truthy.
- **`dangerouslySetInnerHTML`** that reads state re-applies when it changes;
  **SVG** elements created at runtime (list rows, conditional `<g>`) keep the
  SVG namespace, and a `<foreignObject>` is HTML again.
- **Context in list rows**: rows of a list inside `<Ctx.Provider value={...}>`
  read the provider's live value.
- **Store hooks made by a module-level factory** (`const useStore = create(...)`,
  `const { api, useStore } = create(...)`) run in the browser; the component that
  calls them renders its slot empty on the server.

### Context values

`useContext(Ctx)` returns the provided value itself (an object, a number),
never the getter that carries it from the provider: `const { color } =
useContext(Theme)`, `log(theme)` and `[theme]` dependency arrays behave as in
React, and consumers update when the provider's value changes, including
through intermediate components.

### Module-level state

A module evaluates once. A module-level constant created by code (`const store
= createStore(0)`) is created once per page and shared by every component that
uses it, top-level statements that use it (`window.__store = store`) run once,
and helpers its initializer calls are bundled into the page script, including
helpers imported from other local modules. An imported binding that hydration
code needs but the compiler cannot copy (for example one bound by a
destructuring export) is reported as `KR8302`.

User bindings named like runtime helpers (`h`, `findSlot`, `kbind*`,
`krate*`) are renamed in the compiled output, so `hits.map((h) => <li>{h}</li>)`
works.

### useId

`useId()` gives every component instance its own id, and the id in the static HTML is
the one the hydrated page uses, so `htmlFor`/`id` and `aria-*` pairs match before
and after hydration. It works in the page, in child components (also ones with no
state, rendered once at build time), in nested children, in lists, when a component
is rendered more than once, in conditional branches and in layouts.

The ids look like `krate-<component path>`, not React's `:r0:`. A component in a keyed list
row takes `krate-<list>:$<key>-u<n>`, numbered in render order within the row, and
rows the client creates later number theirs the same way, so a row has the same ids
whether the server or the browser rendered it. A hook of your own that calls `useId`
works too, but its ids exist only after hydration (the HTML has no `id` yet).

### useEffectEvent

`const onTick = useEffectEvent(() => log(count))` returns a function that always
runs the latest closure, so it sees the latest props and state, and the effect that
calls it does not depend on what it reads (React 19.2 semantics). Call it from
effects (or other effect events); it is lowered to `createEffectEvent`, which is also
in the compat runtime and the trimmed runtime.

### Activity

`<Activity mode="visible" | "hidden">` renders its children inside a
`<krate-activity>` wrapper with `display: contents`. In `hidden` mode the wrapper is
`display: none` and:

- the children stay in the page and keep their state, including DOM state such as
  an input's text and scroll position,
- their effects are cleaned up when the mode becomes `hidden` and set up again when
  it becomes `visible`, and effects do not run at all while the page loads hidden,
- `mode` may be a literal or any expression, and a missing `mode` means `visible`.

Differences from React: there is a wrapper element (React adds none and hides the
children themselves), class components inside run their lifecycles as usual rather
than unmounting, and nothing is rendered at lower priority. Props other than `mode`
warn (`KR8031`).

### ViewTransition

`<ViewTransition name="hero" default="slow-fade">` gives its first element child
`data-view-transition="hero"` and `data-view-transition-class="slow-fade"`; the
router turns them into `view-transition-name` and `view-transition-class` and runs
navigations inside `document.startViewTransition`, as it does for any element with
`data-view-transition`. Without `name` Krate generates one; `default="none"` turns it
off. Style the animation with `::view-transition-group(*.slow-fade)`. Children that
are not a host element are wrapped in a `<div>`, and only the first element child of
several is named. `enter`, `exit`, `update` and `share` classes and the `onEnter`,
`onExit`, `onUpdate` and `onShare` callbacks have no effect and warn (`KR8031`): put
the class in `default` and select the event in CSS. `unstable_ViewTransition` is
accepted too. The router needs `spa` and `viewTransitions` left on.

### Custom elements

As in React 19, `<my-card title="x" count={3} open data={{ a: 1 }} onSelect={fn} />`
renders `title`, `count` and an empty `open` attribute; `false` is omitted; and
objects, arrays and functions render no attribute at all (the server HTML used to
contain `data="[object Object]"`). The client sets those as properties on the element,
and again when the element class is defined after the page loaded, so a property
setter on the class sees the value. A component that renders such a prop is
hydrated even when it has no state, because nothing else sets the property.

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
interactive. As in React, a reveal that replaces a fallback waits until at
least 300ms after the most recent fallback appeared, so sequential and nested
reveals do not flash, and content that suspends again outside a transition is
hidden in place (`display: none !important`, with the fallback after it)
rather than removed, so its state and DOM nodes survive.

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
`use(Context)` is a plain context read and works anywhere. The compiler
decides from bindings, not names: an argument bound to a `createContext(...)`
result, in the same module or imported from another bundled module, is a
context read (folded to its default at build time); anything else is treated
as a possible promise, and the runtime checks the context marker on Krate
context objects, so a promise named `userContext` suspends as expected.
`use(promise)` used
outside a declaration (KR4003) or destructured (KR4004) is a build error, and a
component calling it outside any `<Suspense>` gets warning KR4008.

**`lazy()`.** Declare lazy components at module level with a static import:

```tsx
const Chart = lazy(() => import('./Chart'));
const Panel = lazy(() => import('./panels').then((m) => ({ default: m.Panel })));
```

Krate renders the HTML of a lazy component at build time like any other
component, so splitting its render code would save nothing. What is split is
its **hydration code**: the component's event handlers and bindings (and those
of its children) are written to `lazy-<Name>.<hash>.js` next to the page
script. As in React, the enclosing `<Suspense>` shows its fallback until the
component is ready: the build ships the fallback with the lazy content (already
rendered) in a template, and the chunk is loaded with a dynamic import when the
component first renders on the client, that is when the page hydrates, or when
a hidden branch holding it is first shown. The page links the chunk with
`<link rel="modulepreload">`, so its download overlaps the page load; it
still only runs when the component renders. The boundary reveals the content
once the chunk has run, so it is interactive from the first frame it is
visible.

A loader that is not a static import works too when it resolves to a local
component, for example `lazy(() => delay(300, { default: Report }))`: the
loader runs when the component first hydrates and the boundary shows its
fallback until it resolves. The default export must be a component (KR4007);
a failed load is reported to the nearest error boundary. Other loaders are
rejected (KR4001), `lazy()` must not be called inside a component (KR4002),
and a lazy package import (`import('some-package')`) is compiled like a static import
(warning KR4006).

**Transitions.** `useTransition()` returns `[isPending, startTransition]`.
`isPending` becomes true when a transition starts and stays true until the
action settles, including async actions and the work they await, and until
every overlapping transition started through the same hook settled. The
top-level `startTransition` accepts async functions too. Form actions,
`useActionState` dispatches and `useOptimistic` share this one transition
scope: optimistic values stay applied, and a Suspense boundary keeps its
revealed content, until every transition and action has settled. Updates made after an
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

**State snapshots.** `useState` and `useReducer` state follows React's
render snapshots. Outside rendering (event handlers, effect bodies,
cleanups) a state read returns the value of the last commit, so
`setCount(count + 1)` called three times in one handler adds one, while
functional updates (`setCount((c) => c + 1)`) always see the latest pending
value. Effect setups and cleanups read the state and props of the commit
they belong to: a cleanup sees the values of the render its effect ran for,
as a React closure would. Krate-native `createSignal` signals keep reading
their latest value.

**Commit order.** Signal writes are batched: every update made in the same
task produces one commit. Updates made while a discrete event (click, key,
input, change, submit, focus, ...) is dispatched are flushed synchronously
once all of that event's handlers ran: render, commit, layout and passive
effects complete before the browser event returns (React's synchronous
lane). Continuous events (mouse and pointer moves, scroll, wheel) keep the
normal batching. The updates an async action makes when it settles (its
result, the pending flags of `useActionState` and `useFormStatus`,
optimistic reverts) commit in a later task, like React's transitions, so
the pending render is on screen for at least one task. Like React's commit,
each commit reads `document.activeElement`, so a focused element the commit
disabled (a submit button with `disabled={pending}`) keeps or loses focus as
it does under React in each browser. An update made elsewhere (a timeout, a
promise callback) commits in a later task, after the microtasks queued with
it. A commit runs after the DOM bindings have been updated, in React's order:

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
| an inline function, object or array, or a local bound to one (`[handler]`, `[{ page }]`) | re-runs after every commit that changes the component's state or props, since React sees a new identity on every render |
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
conditional branch (`{show && <Counter />}`, `cond ? <A /> : <B />`) is
unmounted when the branch hides: its cleanups run, its refs and bindings are
released and its state is discarded. Showing the branch again mounts a fresh
instance, so state starts from its initial value and every effect runs
again, as in React. The branch's DOM nodes are reused and re-bound, so
uncontrolled DOM state inside them (typed text in an uncontrolled input,
scroll position) survives the toggle, which React would reset.

**Same type in both branches.** `isEdit ? <Form mode="edit" /> : <Form
mode="new" />` keeps one `Form` instance, as React does: the compiler lowers the
pair to `<Form mode={isEdit ? "edit" : "new"} />`, so flipping the condition
changes the props and leaves state, typed input, refs and mount effects alone.
Props set in one branch only switch to `undefined` in the other, differing
children switch with the condition, and chains (`a ? <X/> : b ? <X/> : <X/>`)
and early returns of one type (`if (alt) return <X/>; return <X/>;`) work the
same way. Only components that hold state are lowered; a stateless component
keeps its two branches, so its output does not change. Branches with different
`key` props (or a key on one side only) remount, as in React. When a pair
cannot be lowered, the build warns with `KR8360` instead of resetting state
silently: render-prop function children, a `ref` or spread that differs between
the branches, a condition with side effects, a lazy component, or the same
component inside a wrapper element that each branch builds
(`cond ? <div><X/></div> : <div><X/></div>`). Render one element and pass the
differing values as props, or move the condition inside the wrapper.

**Closures.** Krate reads state through live getters, so a timer or promise
callback created inside an effect reads the latest committed value when it
runs, where React's closure would keep the value of its render.

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
| KR2008 | a hook is called inside a condition or loop (Rules of Hooks) | call it unconditionally at the top level, or move the part into a child component |

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

`key` and `suppressHydrationWarning` never appear in the output. `key` drives
list reconciliation and remounting, see [Keys and lists](#keys-and-lists).
Event handler props are covered in
[Events, forms and refs](#events-forms-and-refs).

## Keys and lists

A `.map()` whose array can change at runtime is reconciled by key, with React's
semantics:

- **Rows are matched by `key`**, the key of the element, component or
  `<Fragment key>` the callback returns. Keys may be strings or numbers and
  compare as strings (`1` and `'1'` are the same key). A row whose key is still
  present keeps its DOM nodes, input values, focus and selection, component
  state, effects and refs, and is moved into the new order with the fewest DOM
  moves. Rows whose key disappeared unmount (effect cleanups run, refs detach)
  before new rows mount; new rows run their effects after they are inserted.
- **Without a key** rows are matched by index, as in React: reordering the
  array moves the data, while state (an input value, a component's `useState`)
  stays with the position.
- **Rows update in place.** When the item for an existing key is a new object,
  or its index changes, only the parts of the row that read it update; the row
  is not remounted. Components rendered in a row receive live props, including
  simple destructured props (`function Row({ item })`). A non-arrow callback
  (`items.map(renderRow)`) or a destructured callback parameter
  (`items.map(({ id, name }) => ...)`) receives plain values, so a row whose
  item changed is re-rendered (its state resets); prefer `item => ...`.
- **Conditionals inside rows** (`{open === item.id && <Panel />}`) mount and
  unmount their components (effects and cleanups run on each toggle), and
  values a row derives from state or props stay live.
- **Duplicate keys** log React's "Encountered two children with the same key"
  error. The first occurrence keeps its row; each duplicate gets its own row.
- **Nested lists, text rows** (`items.map(t => t.label)`), keyed fragments, and
  lists inside `<table>`/`<tbody>`, `<select>` and `<ul>` all work: rows are
  delimited by comment markers, which are valid in every content model.
- **Hydration adopts server-rendered rows by key.** The build renders each row
  between `<!--k:ID:$key-->` markers; on load the list binds the existing nodes
  (handlers, live attributes, component state) instead of re-rendering them,
  so refs attach once and nothing flickers. A row whose key could not be
  computed at build time is adopted by position.

Changing the `key` of a single element remounts it, React's "reset state with
a key" pattern:

```tsx
<Profile key={userId} userId={userId} />
// a new userId unmounts the old Profile (cleanups run) and mounts a fresh one
```

This applies to any element whose key reads state or props, including inside
conditionals and list rows.

Lists over build-time constants (a literal array, or a prop known at build
time) cannot reorder, so they render statically and are never warned about.


## shadcn/ui

shadcn/ui copies component source into your project, so its components compile
like any other Krate source -- no config. Components that wrap a Radix primitive
run as Preact islands (see [Using shadcn/ui with Krate](#using-shadcnui-with-krate)).
The patterns shadcn relies on are handled by the compiler:

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

### Using shadcn/ui with Krate

`npx shadcn@latest init` does not detect Krate, so set the project up by hand
once, then `npx shadcn@latest add button dialog tabs ...` works as usual: it
only writes files into `src/components/ui` and installs packages.

1. **Install the dependencies** the generated components import:

   ```sh
   pnpm add class-variance-authority clsx tailwind-merge lucide-react radix-ui preact
   ```

   `preact` is required as soon as one component is built on Radix (see below),
   and it must be Preact 11 or newer: current shadcn components pass `ref` as an
   ordinary prop, as React 19 does, and Preact 10 does not deliver it to function
   components (a Dialog fails when it opens). You do not need
   `preact-render-to-string`; Krate bundles its own.

2. **Write `components.json`** in the project root (the `new-york` style, no RSC,
   Tailwind CSS variables, and aliases that match your `tsconfig.json` paths):

   ```json
   {
     "$schema": "https://ui.shadcn.com/schema.json",
     "style": "new-york",
     "rsc": false,
     "tsx": true,
     "tailwind": { "config": "", "css": "src/styles/globals.css", "baseColor": "neutral", "cssVariables": true, "prefix": "" },
     "iconLibrary": "lucide",
     "aliases": { "components": "@/components", "utils": "@/lib/utils", "ui": "@/components/ui", "lib": "@/lib", "hooks": "@/hooks" }
   }
   ```

   `tsconfig.json` needs `"paths": { "@/*": ["./src/*"] }`, which the app
   template already has.

3. **Create the stylesheet** `src/styles/globals.css`: Tailwind, the theme
   variables, and an `@theme inline` block that maps them to Tailwind colors, so
   utilities such as `bg-primary` and `text-muted-foreground` exist:

   ```css
   @import "tailwindcss";

   :root {
     --background: oklch(1 0 0);
     --foreground: oklch(0.145 0 0);
     --primary: oklch(0.205 0 0);
     --primary-foreground: oklch(0.985 0 0);
     --muted-foreground: oklch(0.556 0 0);
     --border: oklch(0.922 0 0);
     --ring: oklch(0.708 0 0);
     --radius: 0.625rem;
   }

   @theme inline {
     --color-background: var(--background);
     --color-foreground: var(--foreground);
     --color-primary: var(--primary);
     --color-primary-foreground: var(--primary-foreground);
     --color-muted-foreground: var(--muted-foreground);
     --color-border: var(--border);
     --color-ring: var(--ring);
     --radius-md: calc(var(--radius) - 2px);
     --radius-lg: var(--radius);
   }
   ```

   Copy the full variable list (accent, popover, card, destructive, ...) from
   the shadcn theming page for the components you add, and import the file once
   from your layout or page: `import "../styles/globals.css"`.

4. **Create `src/lib/utils.ts`**, the `cn` helper every component imports:

   ```ts
   import { clsx, type ClassValue } from "clsx";
   import { twMerge } from "tailwind-merge";

   export function cn(...inputs: ClassValue[]) {
     return twMerge(clsx(inputs));
   }
   ```

**What runs where.** Components with no Radix primitive (`Button`, `Card`,
`Badge`, `Input`, `Textarea`, `Table`, ...) compile like any other Krate source
and ship no JavaScript. `asChild` works for them too: `import { Slot } from
"radix-ui"` (and `@radix-ui/react-slot`) is Krate's own Slot.

Components built on a Radix primitive (`Dialog`, `DropdownMenu`, `Tabs`,
`Accordion`, `Select`, `Tooltip`, `Checkbox`, `Switch`, `Popover`, ...) run as
**Preact islands**. A file that renders a component of a React package such as
`radix-ui` or `lucide-react` as a tag is bundled for Preact, and each outermost
use on a page, for example one `<Dialog>...</Dialog>` with everything inside it,
becomes one island. Krate renders the island on the server when it can, so the
trigger button is in the HTML before the island loads, and the island hydrates
in the browser. A page that has islands loads Preact once for all of them; a
page without any loads none. Two or more Radix-based components can share a page.

- A name like `Dialog` can be both your wrapper and a Radix export
  (`import { Dialog as DialogPrimitive } from "radix-ui"` inside `function
  Dialog`). That is how the generated files are written, and it is fine.
- Provider components such as `TooltipProvider` must contain the components that
  need them in the same page tree: put `<TooltipProvider>` around the
  `<Tooltip>` elements you render. A Radix component rendered without its
  provider reports it in the build output (`KR8008`, the island stays
  client-only) and fails in the browser.
- If the build prints `KR8008` for an island, the message names the cause; it
  says Preact is missing only when it is.

### Known limits of Radix on Preact

Two differences between Preact and React change how a few Radix components
behave. They are in Preact, not in Krate: the same failures appear with a
plain Preact build and no Krate at all. They were checked in Chromium against
real React 19 running the same shadcn source, with `radix-ui` 1.6.7 and 1.8.1
and Preact 10.29 and 11.2 (11.2 is the newest release). Switching Radix or
Preact version does not help.

**Tooltip closes about 10 ms after it opens.** This affects the `Tooltip` that
shadcn generates (no `open` prop), on every Radix and Preact version above,
with hover and with keyboard focus. Radix announces an opening tooltip with a
`tooltip.open` event from a passive effect, and every other tooltip closes when
it hears it. React runs that effect before the tooltip content mounts. Preact
runs the content's own effect first, so the tooltip hears its own event and
closes. Make the tooltip controlled, which sends the event before the content
exists. Replace `Tooltip` in `components/ui/tooltip.tsx`:

```tsx
function Tooltip({
  open: openProp,
  defaultOpen = false,
  onOpenChange,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Root>) {
  const [openState, setOpenState] = React.useState(defaultOpen)
  return (
    <TooltipPrimitive.Root
      data-slot="tooltip"
      open={openProp ?? openState}
      onOpenChange={(next) => {
        setOpenState(next)
        onOpenChange?.(next)
      }}
      {...props}
    />
  )
}
```

With this wrapper, hover, focus, Escape and a tooltip inside a `Dialog` all stay
open and close correctly. Re-apply it after `npx shadcn add tooltip --overwrite`.

**A layer opened inside another layer's content dismisses the outer layer.**
Radix decides whether focus is still "inside" a popover by letting React
bubble the focus event up through portals. Preact does not, so opening a
`Dialog` from inside `PopoverContent` closes the popover and the dialog never
appears. Keep the `Dialog` outside the popover and open it with state:

```tsx
const [popover, setPopover] = React.useState(false)
const [dialog, setDialog] = React.useState(false)

<Popover open={popover} onOpenChange={setPopover}>
  <PopoverTrigger asChild><Button>Open</Button></PopoverTrigger>
  <PopoverContent>
    <Button onClick={() => { setPopover(false); setDialog(true) }}>Edit</Button>
  </PopoverContent>
</Popover>
<Dialog open={dialog} onOpenChange={setDialog}>...</Dialog>
```

Only Dialog inside Popover was tested. A menu, popover or similar layer opened
from inside another layer's content relies on the same mechanism, so use the
same state-driven pattern. A `Select` inside a `Dialog` and a `Tooltip` inside
a `Dialog` (with the controlled wrapper) work.

Tested and working: Dialog, Sheet, DropdownMenu, ContextMenu, Menubar,
NavigationMenu, Popover, HoverCard, Select, Tabs, Accordion, Collapsible,
Checkbox and Switch. In a project on Preact 10, shadcn's own components fail
because they pass `ref` as a prop, which is why Preact 11 is required.

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
  `node_modules`. Local React-compatible modules imported by the page and used
  as tags inside that subtree are bundled into the same island, so a local
  wrapper can contain or be contained by a compiled package component. A
  same-file function component is reported with a diagnostic because it belongs
  to the Krate runtime, not the island runtime.
- The subtree is captured as-is and rendered with **Preact**. Preact is not a
  Krate dependency; install it in your app:

  ```sh
  pnpm add preact
  ```

  Preact 10.20 or newer is required. If Preact is missing, the build fails with an actionable error rather than
  shipping a broken island.
- Each island gets an entry file (`chunks/island_<id>.js`). The entries of all
  pages are bundled together with code splitting, so a module two pages import
  (a store, a query client, the library itself) is one shared file with one
  instance, and the Preact modules are emitted once and wired through an import
  map. See [State across navigation](#state-across-navigation).
- Where all bridged props have build-time initial values, Krate renders the
  island with `preact-render-to-string` and places that markup inside the
  placeholder. The client then **hydrates** it instead of replacing it. Islands
  with unknown runtime props, browser-only components, or portals stay
  client-only; the build warns when an island cannot be server-rendered.
- Signal-valued props are bridged as live getters, and the island re-renders
  when those signals change. Function props (event handlers and signal setters)
  are passed by reference, so page-to-island callbacks work. Plain object and
  string props are captured when the island mounts.
- Each island is wrapped in a Preact error boundary. A render failure marks the
  placeholder with `data-krate-island-error` and shows a fallback instead of
  taking down the rest of the page.
- Krate warns when the installed Preact is older than 10.20, which is the
  minimum version the island hooks and compat behavior are tested against.

In an `async` page, layout or component the same packages render without a page
island: stateless components (an icon) become plain HTML, stateful ones
(a Radix checkbox) become `<krate-island>` boundaries rendered on the server
and hydrated in place, and parts of a compound component such as
`Dialog.Root` / `Dialog.Trigger` stay in one tree. See
[Async Server Components](/docs/features/server-components/#compiled-react-packages-in-server-components).

Two runtimes are involved at an island boundary: the page (Krate signals) and
the island (Preact). Props crossing the boundary may be serializable values,
bridged signal getters, or function references (handlers and setters).

Primitives that keep internal pointer state (for example Radix Tooltip) work
when controlled: bind `open` to a page signal and wire `onOpenChange` plus
explicit pointer/focus handlers on the trigger.

### Choosing when an island mounts

By default an island mounts as soon as the page script runs. Put a client
directive on the island's root tag to defer it:

```tsx
import { LineChart, Line } from 'recharts';

<LineChart client:visible width={600} height={300} data={data}>
  <Line dataKey="sales" />
</LineChart>
```

| Directive | Mounts |
|-----------|--------|
| `client:load` | at load (the default) |
| `client:idle` | when the browser is idle (`requestIdleCallback`, 2 s at most) |
| `client:visible` | when the island comes within 200 px of the viewport (`IntersectionObserver`) |

A deferred island keeps its server-rendered markup, which is not interactive
until it mounts. Its chunk is not preloaded and is not fetched before then, so
a heavy island below the fold costs nothing until it is needed. The directive is
removed at build time and never reaches the component. On anything that is
not an island the directive does nothing, and the build says so (`KR8012`).
Without `IntersectionObserver` the island mounts at load. A page that is itself a
React-library island (below) always mounts at load.

## Libraries built on React hooks (islands)

State, forms and data-fetching libraries from the React ecosystem call React's
hooks from their own code (`useSyncExternalStore`, `useRef`, `useContext`).
Krate's source components do not have React's render model: they run once and
update through signals, and a library's hooks cannot be lowered. A
component that calls such a hook therefore runs in a **Preact island**, where
React's model exists, instead of being lowered.

Krate decides this at build time. A user module is routed to the island path
when it:

- calls a `use*` hook imported from an npm package that depends on `react`
  (`useForm`, `useSWR`, `useQuery`, `useStore`, ...), or a store hook made at
  module level (`const useStore = create(...)`),
- calls a hook exported by another module that does (a custom hook wrapping
  `useSWR`, a store hook exported from `store.ts`),
- renders a component built by such a package at module level
  (``const Box = styled.div`...` ``, `motion(Button)`),
- hands a value built by such a package to a JSX attribute
  (`const queryClient = new QueryClient()` then `client={queryClient}`), or
- starts with `/** @jsxImportSource <react package> */` (emotion's `css` prop).

```tsx
// src/components/Counter.tsx
import { useStore } from '../store';        // const useStore = create(...)

export function Counter() {
  const n = useStore((s) => s.n);
  const inc = useStore((s) => s.inc);
  return <button onClick={inc}>clicked {n}</button>;
}
```

```tsx
// src/pages/index.tsx: Counter is rendered as an island
import { Counter } from '../components/Counter';

export default function Home() {
  return <main><Counter /><Counter /></main>;
}
```

The module is compiled for Preact (`react` resolves to `preact/compat`) and
rendered on the server with the same code, then hydrated. Props follow the
island rules above: serializable values, signals bridged as live getters, and
function references. A page whose own module calls the hook is one island.

What was checked in Chromium (Preact 10.29): each page loaded without console
errors and rendered the expected markup, and where the library has an
interaction it was clicked or typed into.

| Library | Result | Version checked |
|---------|--------|-----------------|
| zustand | works: store hooks, selectors, state shared by the islands of a page | 5.0.16 |
| valtio, jotai | work: `useSnapshot`, `useAtom` | 2.3.2, 2.20.3 |
| react-redux with Redux Toolkit | works: `Provider` in the island's module or in a layout, `useSelector`, `useDispatch`, a slice and `configureStore` | 9.3.1, 2.14.0 |
| react-hook-form | works: `register`, `required` validation, `handleSubmit`, `formState` | 7.91.0 |
| SWR | works: `useSWR` directly or inside a custom hook | 2.5.1 |
| TanStack Query | works: `useQuery` under a `QueryClientProvider` in the island's module or in a layout, one client for every island | 5.106.1, 5.105.3 |
| TanStack Table | works: `useReactTable` with `flexRender`, rendered on the server | 8.21.3 |
| react-i18next | works: `useTranslation` after `i18n.use(initReactI18next).init()` | 15.7.4 |
| usehooks-ts | works: `useCounter`, `useToggle` | 3.1.1 |
| recharts, Headless UI | work (they need `import React from "react"`) | 2.15.4, 2.2.10 |
| framer-motion | works: `motion.*` and `useAnimation` | 11.18.2 |
| motion (`motion/react`, v12 and later) | works: `motion.*` with `initial`/`animate`/`transition`, `AnimatePresence` with an exit animation. Building logs the warning KR7116, which does not affect the result | 14.14.0 |
| cmdk | works: `Command`, `Command.Input`, `Command.List`, `Command.Item` filtering as you type, `Command.Empty` | 1.1.1 |
| embla-carousel-react | works: `useEmblaCarousel`, snap list, `scrollNext()` | 8.6.0 |
| @dnd-kit | works: `DndContext`, `SortableContext`, `useSortable` with the pointer sensor, reordering through a real drag | core 6.3.1, sortable 10.0.0 |
| next-themes | partly: `ThemeProvider` and `useTheme` switch the `<html>` class and theme state when the provider is in the island's module; a provider in a layout is not lifted (KR8016) | 0.4.6 |
| sonner | partly: `toast()` and `<Toaster />` work when both are in one island module; a component that only calls `toast()` fails to build (`cannot bundle "sonner"`), so the usual root-layout `<Toaster />` plus `toast()` elsewhere does not work yet | 2.0.8 |
| styled-components, `@emotion/styled`, emotion `css` prop, MUI `Button` | work, client-only (see below); a `ThemeProvider` in a layout reaches them | 6.6.0, 11.14.1, 11.14.0, 9.5.0 |

These were run by hand against the real packages. `TestBrowserEcosystemLibraries`
repeats the checks for motion, cmdk, embla-carousel-react, @dnd-kit,
TanStack Table, next-themes and sonner against the pinned versions in
`packages/compiler/internal/build/testdata/ecosystem_libs/package.json`; it is
skipped unless `KRATE_BROWSER` is set and either `KRATE_ECOSYSTEM_LIBS` points
at a `node_modules` directory holding them or `KRATE_ECOSYSTEM_INSTALL=1` lets
it run `npm install`. The other repository tests run
stand-ins with the same import shapes (default `import React`, namespace
import, `require("react")`) and the same behavior (a store with module-level
state, a query client with a context provider, a theme provider), because the
repository does not install the libraries; they cover the behavior described on
this page in Chromium, but not the libraries themselves. A library that is not
listed may work, but nothing was run for it.

### Providers in layouts

A context provider a layout wraps around `{children}` reaches the islands of
every page under that layout, with one provider instance for all of them:

```tsx
// src/pages/_layout.tsx
import { useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Provider } from 'react-redux';
import { ThemeProvider } from 'styled-components';
import { store } from '../store';

const theme = { name: 'light' };

export default function Layout({ children }) {
  const [client] = useState(() => new QueryClient());
  return (
    <div>
      <nav><Link href="/">Home</Link></nav>
      <Provider store={store}>
        <QueryClientProvider client={client}>
          <ThemeProvider theme={theme}>{children}</ThemeProvider>
        </QueryClientProvider>
      </Provider>
    </div>
  );
}
```

The layout itself is compiled by Krate, and an island is a Preact tree, so
context cannot cross between them. Krate therefore lifts the wrappers out of the
layout: the layout renders `{children}` as if they were not there, and the chain
(`Provider > QueryClientProvider > ThemeProvider`, with the props as written)
becomes a generated module that every island of the page renders inside. This
holds on the server too: the markup rendered at build time already contains the
context, and the island hydrates onto it.

- **One instance per page and site.** The props are evaluated once, in the
  generated module, and that module is shared by all islands and all pages, so
  they receive the same `QueryClient`, store or theme object. A client created
  with `useState(() => new QueryClient())` in the layout is created once, not per
  render and not per island.
- **Nested layouts** compose: the providers of the outer layout wrap those of
  the inner one, so an inner `ThemeProvider` overrides the outer one for the
  pages under it.
- **Which wrappers.** A tag on the path to `{children}` that comes from an npm
  package built on React, or from a module of the project that renders such a
  component around its `children` (a `Providers` component), is lifted. Other
  elements stay in the layout.
- **Props.** A prop can be a literal, an import, a module-level value, or a
  value the layout component computes from those: a plain `const`,
  `useState(() => value)` with no setter in use, or `useMemo(() => value, [])`.
  Computing a prop from the layout's own `params`, from a signal, or from a
  state setter cannot work, because the islands would not see it change: the
  build reports `KR8015` with the reason.
- **No markup of its own.** A lifted wrapper must render only its children. One
  that adds elements (a `<div>` around them) cannot move into the islands, since
  Krate renders the page content; the wrapper then stays in the layout and the
  build reports `KR8016`.
- **Layout components stay Krate's.** Krate components in the layout still
  cannot read the provider's context, and a hook of a React library called
  inside the layout is still `KR8013`.

### State across navigation

The islands of all pages are bundled together, so a module-level store is one
store for the whole site, as in a React single-page app. A zustand or Redux
store, a query client and the library modules they use are evaluated once; when
the client-side router moves to another page, the new page's islands import the
modules that are already loaded.

- **Same instance, same value.** The count in a zustand store, a Redux slice and
  the cache of a query client survive navigation, forward and back.
- **Components start fresh.** An island is a Preact tree under the page's
  content, and the router replaces that content: when a page changes, its
  islands unmount (effects clean up, store subscriptions and query observers are
  released) and the next page's islands mount. State inside a component
  (`useState`) therefore resets, as when a React router swaps route elements.
  Keep what must last in a store.
- **Static pages stay static.** A page without islands loads no island code, and
  the Preact import map is a small JSON block.
- **Caching.** The entry file of an island (`island_<id>.js`) is revalidated on
  every visit; the shared chunks (`isl-<hash>.js`) are content-hashed and cached
  as immutable.

### Pages rendered per request

Pages with `dynamic = "force-dynamic"`, `revalidate = N` (ISR) or a streaming
region run their JSX in Node on every request, where a component that needs
React's render model cannot run. Their islands are therefore not executed there:
the server answers with the placeholder of each island and the markup rendered
for it at build time, and the page script mounts it as on a static page. The rest
of the page is rendered per request as usual.

Because that markup is rendered at build time, an island's props must be known
then: a prop that only exists per request (a search parameter, a cookie) makes
the island client-only, as on a static page, and the build says so (`KR8008`).

### CSS-in-JS

styled-components, emotion and MUI create their styles in the browser while
rendering. Their islands are **client-only**: the build prints a `KR8008`
notice, the placeholder is empty in the server HTML, and the component and its
styles appear when the island mounts. For content that must be styled before
JavaScript loads (above-the-fold text, SEO content), use CSS modules, Tailwind
or a stylesheet. A `css={...}` prop on a DOM element in a module without
`/** @jsxImportSource @emotion/react */` is reported as `KR8014` (the styles
would be dropped). Setting `jsxImportSource` to emotion in `tsconfig.json`
for the whole project is not supported.

### Limits, and what the build refuses

Each limit below is either a build error or a documented behavior, never a
silent failure in the browser:

- **Hooks need a client component.** A layout, a template or an `async` server
  page or component that (through its imports) calls a React-library hook fails
  with `KR8013`, naming the package and the hook. Add `"use client"` to the
  module that calls it (it then hydrates as a client component inside the
  server-rendered output), or use `createSignal` / `createResource`. Rendering
  the library's *components* (icons, Radix primitives) from a server tree needs
  no directive; see
  [Compiled React packages in server components](/docs/features/server-components/#compiled-react-packages-in-server-components).
  A static (synchronous) layout cannot host compiled components at all: make it
  `async`.
- **No bridge to Krate state from inside.** The island reads the page's
  signals only as props, and the page cannot read the island's store. Krate
  components that import the same store module do not see it.
- **A page that is itself an island** cannot take props (`params`,
  `searchParams`): the build fails with `KR8013`. Move the component that calls
  the hook into its own module and render it from a page that passes the
  values in. Such a module also cannot use Krate-only APIs (`createSignal`,
  `<Head>`).
- **Component state is not kept across pages.** Module-level state is (see
  [State across navigation](#state-across-navigation)); the state of a
  component resets when its island unmounts.
- **No islands in layouts.** A layout is rendered by Krate and persists across
  pages; a React-library island has to be rendered by a page. (Providers are
  the exception, see [Providers in layouts](#providers-in-layouts).)

## Libraries built on a React reconciler (real-React islands)

`@react-three/fiber`, `@react-three/drei`, `react-konva` and similar libraries
draw through `react-reconciler`, React's renderer API. Preact does not have it,
so these libraries cannot run on the Preact islands above. A module that uses
one runs in a **real-React island** instead: an island bundled with `react`
and `react-dom` themselves, with no Preact alias anywhere in its subtree.

```tsx
// src/components/Viewer.tsx
import { Canvas } from '@react-three/fiber';
import { OrbitControls } from '@react-three/drei';

export default function Viewer({ color }: { color: string }) {
  return (
    <Canvas>
      <mesh><boxGeometry /><meshStandardMaterial color={color} /></mesh>
      <OrbitControls />
    </Canvas>
  );
}
```

```tsx
// src/pages/model.tsx
import Viewer from '../components/Viewer';

export default function Page() {
  return <Viewer client:visible client:fallback="Loading 3D viewer..." color="#c33" />;
}
```

### How a module opts in

Krate does not need a directive on every tag: the requirement belongs to the
package, so the build decides from the module's imports, the same way it
detects React-library hooks.

- A package that depends on (or has a peer dependency on) `react-reconciler`,
  `@react-three/fiber` or `react-konva` needs real React. That covers
  `@react-three/fiber`, `@react-three/drei`, `react-konva` and the libraries
  built on them. Nothing needs to be configured.
- Any other package can be added in `krate.config.ts`:

  ```typescript
  react: { islands: ["some-canvas-library"] },
  ```

A user module that imports such a package becomes a real-React island, and so
does every island module that imports it. Using the component as a tag in a page
renders the island there. A reconciler library used as a tag directly in a page
(`<Canvas>` imported in the page itself) is an island too. A library that pulls
in `react-reconciler` without being listed fails the Preact island build and the
message names `react: { islands: [...] }`.

### `"use client"` modules

The usual Next way works unchanged: a `"use client"` module that imports a
reconciler library (`"use client"; import { Canvas } from "@react-three/fiber"`)
is a real-React island, whether a sync page, an async page, an async layout or
another `"use client"` component imports it. Everything inside the module
(its own hooks, effects and the other components it imports) runs on real
React. A `"use client"` parent that renders such a component becomes a real-React
island as a whole; Krate does not nest a real-React root inside a Preact island,
because the two runtimes cannot share a tree.

### Rendering

A real-React island is **client-rendered**. Server-rendering a WebGL or canvas
tree is not meaningful, so the server emits only the placeholder. Put
`client:fallback="text"` on the tag to render a text fallback there until
React mounts it (escaped, text only). To reserve space, wrap the tag in an
element with a fixed size. `client:load`, `client:idle` and `client:visible`
work as for other islands, so a heavy viewer below the fold is fetched only
when it nears the viewport. Both directives are removed at build time and
never reach the component.

### Props and children

- Props are serialized like any other island: literals, bridged signal getters
  (the island re-renders when the signal changes) and function references.
- The island is one React tree. `children` and nested tags in the island's JSX
  (`<Canvas><mesh/></Canvas>`) are compiled for React inside it, and may use
  elements, components imported from packages and imported local modules.
- **Krate components cannot be passed in.** A same-file Krate function used
  inside the island, or JSX built by a Krate component and handed to a prop,
  cannot cross the boundary; the build reports the same-file case (`KR8007`).
  Move the component into its own module (it is then bundled and run on React)
  or render it outside the island.
- The island cannot read Krate signals, only the props the page passes in, and
  Krate cannot read the island's state.
- Providers a layout wraps around its children are not applied to a real-React
  island. Render the provider inside the island's own module.
- A page that is itself a real-React island, and a synchronous layout, follow
  the limits of the other React-library islands (`KR8013`).
- **In `async` pages and layouts** a real-React component also works, used
  directly (`<Canvas>` from the library) or through a wrapper module. It never
  goes through the Preact boundary path: the server sends a `<krate-island>`
  with the `client:fallback` text, the serialized props and the children as an
  element tree, and the browser mounts it with `react-dom`. Props must be
  serializable (as for any client component); children may be host elements and
  other real-React components, with serializable props.

### What it costs

Real React is not part of Krate's runtime, and the 25 KB counter-page figure
does not include it. The cost is paid only by pages that load a real-React
island, as one shared chunk of `react`, `react-dom` and the libraries, cached
across those pages: React plus `react-dom` is roughly 60 KB gzipped for the
client renderer, and `react-reconciler` libraries add their own weight (Three.js
is several hundred KB). Pages without such an island load none of it, and
`krate check` counts the island bytes against the page budget. All real-React
islands of a page mount from one entry, and the pages of a site share the
React chunk, so a second viewer page adds only its own code.

The project must have `react` and `react-dom` (18.3 or newer, 19 recommended)
installed; the build says so if they are missing. Both resolve to the project's
copy, so a reconciler library and the island share one React.

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
  listeners. Handlers inside a portal keep working, and events from portal
  content bubble to the React ancestors of the portal, not the DOM ancestors
  of its container.

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
- **Constant values.** `value="fixed"` with an `onChange` is controlled too:
  typing snaps back to the constant, as in React.
- **Mount behavior.** Text inputs get React's mount-time caret (at the end of
  the value), so typing after `focus()` appends; a controlled textarea starts
  with the caret at the beginning, as in React. An `autoFocus` element is
  focused when its conditional branch mounts, and focus stays on a list item
  (matched by `id`) when the list re-renders.
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
  detached and the new one attached. An inline callback ref
  (`ref={(n) => log(n, count)}`) is re-bound whenever a state value it reads
  changes, and each call sees the state of its own render, matching React's
  new-callback-per-render behavior. Callback refs never subscribe the
  surrounding binding to signals they read.
- Hydration refs attach after every component on the page has bound its DOM
  and before layout effects (components mounted later attach at once), so a ref on server-rendered list content that is re-rendered
  on hydration is never called with the stale node.
- **`ref.current` read during render** is shown once and not refreshed on
  later renders (Krate does not re-render); it is reported as `KR5008`. Keep
  values the UI shows in state.
- **`ref` as a prop**, `forwardRef`, and `useImperativeHandle(ref, create,
  deps)` are supported. Refs in lists keep the right element per item.
- Refs to class component instances receive the instance (see Class
  components).

### Build diagnostics

| Code | Reported for |
|------|--------------|
| `KR5001` | an unknown event prop on a built-in element (`onClickcapture`, `onTap`); a "did you mean" hint names the React event |
| `KR5002` | a lowercase event prop given a function (`onclick={fn}`) |
| `KR5003` | a string ref (`ref="field"`, also in class components), removed in React 19 and ignored |
| `KR5004` | `value`/`checked` without `onChange`, `onInput`, `readOnly` or `disabled` (a read-only field) |
| `KR5005` | `value` with `defaultValue`, or `checked` with `defaultChecked` (the default is ignored) |
| `KR5006` | `value={cond ? x : undefined}`: a field that switches between controlled and uncontrolled |
| `KR5007` | `onChange` on a `contentEditable` element (use `onInput`) |
| `KR5008` | `ref.current` read in rendered JSX (shown once, never refreshed) |
| `KR6001` | a list that can change at runtime renders elements without a `key` |
| `KR6002` | two items of a list resolve to the same key at build time |
| `KR6003` | a map callback keys some returned elements but not others |

## What is not supported

- **Running React itself in a page.** There is no React runtime; compiled
  packages run through Preact islands, not React. The exception is a
  [real-React island](#libraries-built-on-a-react-reconciler-real-react-islands),
  for libraries built on a React reconciler.
- **React libraries' hooks in lowered source components.** `zustand`,
  `react-hook-form`, `swr` and `@tanstack/react-query` hooks need React's render
  model, so the module that calls them is run as a Preact island instead (see
  [Libraries built on React hooks](#libraries-built-on-react-hooks-islands)).
  Where an island cannot be used (layouts, server pages) the build fails with
  `KR8013`. The signal-based replacements are `createSignal` (with a module-level
  signal for shared state) for client state and `createResource` for fetching.
- **Sass, Less and Stylus.** There is no compiler for them: importing a `.scss`,
  `.sass`, `.less` or `.styl` file is a build error (`KR7123`). Compile it to CSS
  in a prebuild step, or use CSS modules or Tailwind (see
  [Styling](/docs/core-concepts/styling/#sass-less-and-stylus)).
- **CSS-in-JS at first paint.** styled-components, emotion and MUI work only as
  client-only islands; their styles do not exist in the server HTML.
- **React 19-only semantics** in compiled packages. Preact's compatibility layer
  is close to React 18 behavior; version-specific APIs may differ.
- **State reuse across conditional branches, the uncommon shapes.** One
  stateful component type in both branches of a ternary keeps its instance (see
  [Effects](#effects)). Shapes the compiler cannot lower restart the component
  and warn with `KR8360`: the same component inside a wrapper element each
  branch builds, a differing `ref` or spread, render-prop children. `{c &&
  <Panel/>}` next to `{!c && <Panel/>}` are two positions, so they remount in
  React as well. A stateless component, or an external island, in both branches
  is not merged: uncontrolled DOM state inside it (typed text) is reset on the
  flip.
- **Hook ordering and rules-of-hooks** for transpiled source. Components run
  once per mount, so there is no hook order to preserve.
- **`Activity` DOM structure, `SuspenseList` ordering, `ViewTransition` events.**
  See the sections above.
- **A custom hook that calls `useId`** has no id in the static HTML.
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

Forms work without JavaScript and are enhanced by the shared runtime.

### React 19 actions

`<form action={fn}>` and `<button formAction={fn}>` accept a server action or a
client function. On submit the function receives the `FormData` inside an async
transition; the page does not reload and uncontrolled fields reset after
success. The React 19 hooks follow React's semantics:

- `useActionState(action, initial, permalink?)` returns
  `[state, formAction, isPending]`. Calls are queued in order and receive the
  previous state; with a server action the previous state travels to the
  server and the returned state back. Without JavaScript the form posts to the
  action with the initial state and redirects to `permalink`.
- `useFormStatus()` reads the pending status, data, method and action of the
  form that contains the component.
- `useOptimistic(state, update?)` applies updates while an action is pending and
  reverts to the latest state when it settles.
- `requestFormReset(form)` resets a form after its pending action.

Inline `"use server"` functions inside server components are hoisted into
generated server actions; in a `"use client"` module they are an error
(`KR7001`). See
[Server Actions](/docs/guides/server-actions/) for the module format, wire
format, request context (`headers()`, `cookies()`, `redirect()`), runtime
requirements, and security model.

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
The four P2 differences that remain at v1 are all render-observability details,
not DOM or interaction differences:

| Fixture | Difference |
| --- | --- |
| `r18/class_pure_component` | A `PureComponent` render log is one update behind because Krate updates the text binding without re-running the body. |
| `r18/context_memo_child` | A memo child's render log differs for the same reason; the rendered context value is identical. |
| `r18/hook_usecallback_identity` | Render counters kept in refs differ because bodies are not re-run; handler behavior matches. |
| `r18/hook_useid` | `useId` strings use Krate's `krate-<n>-...` format instead of React's `:r0:` style; ids stay unique and labels resolve. |

P0 and P1 differences fail the differential (`KRATE_REACT_EXIT=1`). P2 entries
are intentionally documented rather than hidden.

See [Compile-Time Evaluation](/docs/guides/compile-time-evaluation/) for how
known React expressions, `.map()` callbacks, and local helpers are folded before
shipping.
