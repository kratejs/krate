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
| `useState` | `createSignal(init, krateHookState)` (React state snapshots; a lazy initializer `useState(() => x)` or `useState(loadFn)` is called once) |
| `useEffect` | `createEffectDeps(fn, () => deps)` (passive phase, see [Effects](#effects)) |
| `useLayoutEffect` / `useInsertionEffect` | `createEffectDeps(fn, () => deps, 'layout' / 'insertion')` |
| `useMemo` | `createMemoDeps(fn, () => deps)`; without deps, `createMemo` |
| `useReducer` | `createReducer(reducer, init(arg), krateHookState)` (stable `dispatch`, Object.is bail-out) |
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
| `createContext` | supported: `<Ctx.Provider value>`, React 19 `<Ctx value>`, `<Ctx.Consumer>{(v) => ...}</Ctx.Consumer>`, any constant default (objects included), provider values that change at runtime, and nested or self-nesting providers (see [Context](#context)) |
| `createElement` | lowered to `h` |
| `memo` | identity (components already run once) |
| `lazy` | rendered at build time; its hydration code is a separate chunk (see [Suspense](#suspense-lazy-use-and-transitions)) |
| `Suspense` | a real boundary: fallback while anything inside is pending |
| `<Fragment>` / `<React.Fragment>` | a JSX fragment |
| `react/jsx-runtime`, `jsx`, `jsxs`, `jsxDEV` | lowered to Krate JSX |
| `react-dom` / `react-dom/client` | `createPortal` and `flushSync` map to the compat runtime; `useFormStatus` and `requestFormReset` are supported; `findDOMNode`, `render`, `hydrate` and `unmountComponentAtNode` (removed in React 19) report KR8001 |
| `Children.only` / `toArray` / `count` | folded at build time on literal input, otherwise compat shim |
| `Children.map` | folded at build time for a literal list and inline arrow, otherwise compat shim |
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
  have rendered) are ignored. Error boundaries around islands are planned.

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
conditional branch mounts and unmounts with the branch, and its content is
removed when the component goes away.

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

Hook arguments are read when the hook is called; a hook that must react to a
changing argument should receive it through state it owns or a getter.

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

Inline `"use server"` functions inside components are not supported
(`KR7001`); move them into a `"use server"` module. See
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

See [Compile-Time Evaluation](/docs/guides/compile-time-evaluation/) for how
known React expressions, `.map()` callbacks, and local helpers are folded before
shipping.
