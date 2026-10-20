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

This page covers both paths.

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
// useState  -> createSignal      useEffect -> createEffect
// useRef    -> { current: null }  {seconds} -> seconds()
```

### Hooks

| React | Rewritten to |
|-------|--------------|
| `useState` | `createSignal` |
| `useEffect` | `createEffect` |
| `useLayoutEffect` / `useInsertionEffect` | `createEffect` |
| `useMemo` | `createMemo` |
| `useReducer` | `createReducer` |
| `useRef` | `{ current: initial }` |
| `useCallback` | the wrapped function |
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
| `createRef` | lowered to `{ current: null }` |
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

### Attributes and `style`

React-style attribute aliases are translated to their HTML/SVG equivalents,
including `className` -> `class`, `htmlFor` -> `for`, `tabIndex` -> `tabindex`,
`readOnly`, `autoComplete`, `colSpan`, `srcSet`, `contentEditable`, and SVG
presentation attributes such as `strokeWidth`, `fillRule`, `clipPath`, and
`strokeDasharray`.

A **literal** `style={{ ... }}` object is folded to a CSS string, converting
camelCase keys to kebab-case and adding `px` to dimension values:

```tsx
<div style={{ fontSize: 14, backgroundColor: 'red' }} />
// -> <div style="font-size:14px;background-color:red">
```

`onDoubleClick` is translated to the DOM `dblclick` event. `key` and
`suppressHydrationWarning` are compiler-only and never appear in the output.

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

  If Preact is missing, the build fails with an actionable error rather than
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
- Krate warns when the installed Preact is older than 10.19, which is the
  minimum version the island hooks and compat behavior are tested against.

Two runtimes are involved at an island boundary: the page (Krate signals) and
the island (Preact). Props crossing the boundary may be serializable values,
bridged signal getters, or function references (handlers and setters). A local
Krate component cannot be rendered inside an external subtree -- the build
reports this instead of silently dropping it.

Primitives that keep internal pointer state (for example Radix Tooltip) work
when controlled: bind `open` to a page signal and wire `onOpenChange` plus
explicit pointer/focus handlers on the trigger.

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

Event props follow React semantics: events **bubble** (a child handler does not
stop an ancestor handler unless it calls `stopPropagation()`), `onChange` maps to
`input`, `onFocus`/`onBlur` map to the bubbling `focusin`/`focusout`, and
non-bubbling events (`onMouseEnter`/`onMouseLeave`, `onScroll`, ...) are attached
directly. `onClickCapture`-style capture handlers are supported. Controlled
`value`/`checked` bind by DOM property, so programmatic updates work after the
user has interacted. Hydration skips writes when the DOM value already
matches, preserving focus, caret position, and selection.

If you rely on unsupported behavior, write the component in Krate's own style --
see [Reactivity](/docs/core-concepts/reactivity/) and
[Migrating from Next.js](/docs/guides/migrating-from-nextjs/).
