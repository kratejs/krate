---
title: React Compatibility
order: 5
description: How Krate transpiles React syntax to signals — supported hooks, bare-read auto-calling, and what is intentionally not supported.
---

# React Compatibility

Krate's compiler transpiles React syntax to Krate signals and effects
**automatically**. There is nothing to enable: any file that imports from
`react` (or uses the `React.*` namespace) is rewritten at build time. This makes
it practical to drop an existing React component into a Krate app and have it
work without a rewrite — most of the time.

This is a **transpilation**, not a React runtime. There is no React in the
output and no virtual DOM. The compiler rewrites imports and calls to the
primitives that `@krate/runtime` already ships, then compiles the component the
same way as hand-written Krate code.

```tsx
// You write React…
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
// …and the compiler emits the equivalent Krate code.
// useState  → createSignal      useEffect → createEffect
// useRef    → { current: null }  {seconds} → seconds()
```

## Supported APIs

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

Named imports, aliased imports (`import { useState as state }`), and the
`React.*` namespace all work:

```tsx
import React from 'react';

export default function Counter() {
  const [count, setCount] = React.useState(0);
  return <button onClick={() => setCount(count + 1)}>{count}</button>;
}
```

### Components & helpers

| React | Rewritten to |
|-------|--------------|
| `forwardRef` | `forwardRef` |
| `createContext` | `createContext` |
| `createElement` | `h` |
| `memo` | identity (components already run once) |
| `lazy` | identity passthrough |
| `<Fragment>` / `<React.Fragment>` | a JSX fragment (`<>…</>`) |

### Bare reads are auto-called

React reads a value directly (`{count}`); Krate signals are getters (`count()`).
The compiler bridges the difference by rewriting **bare value-position reads to
calls**, so unmodified React works:

```tsx
const [count, setCount] = useState(0);

return (
  <div>
    <span>{count}</span>                      {/* → count()               */}
    <span>{count + 1}</span>                  {/* → count() + 1           */}
    <span>{`n=${count}`}</span>               {/* → `n=${count()}`        */}
    <button onClick={() => setCount(count + 1)}>
      {/* → setCount(count() + 1) */}
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
a runtime counter. This means the server-rendered id and the hydrated id always
agree — strictly better than React's runtime counter for server rendering.

```tsx
const id = useId();
return (
  <div>
    <label htmlFor={id}>Name</label>
    <input id={id} />
  </div>
);
```

### `useReducer`

`useReducer` maps to `createReducer`, which returns `[getter, dispatch]` — the
same getter contract as `createSignal`, so it reacts and hydrates like any other
signal.

```tsx
const [count, dispatch] = useReducer((state, action) => state + action, 0);
return <button onClick={() => dispatch(1)}>{count}</button>;
```

### Attributes & `style`

React-style attribute aliases are translated to their HTML/SVG equivalents,
including `className` → `class`, `htmlFor` → `for`, `tabIndex` → `tabindex`,
`readOnly`, `autoComplete`, `colSpan`, `srcSet`, `contentEditable`, and SVG
presentation attributes such as `strokeWidth`, `fillRule`, `clipPath`, and
`strokeDasharray`.

A **literal** `style={{ … }}` object is folded to a CSS string, converting
camelCase keys to kebab-case and adding `px` to dimension values:

```tsx
<div style={{ fontSize: 14, backgroundColor: 'red' }} />
// → <div style="font-size:14px;background-color:red">
```

`onDoubleClick` is translated to the DOM `dblclick` event. `key` and
`suppressHydrationWarning` are compiler-only and never appear in the output.

## shadcn/ui

shadcn/ui copies component source into your project, so its components compile
like any other Krate source — no config, no islands. The patterns shadcn relies
on are handled by the compiler:

- **`cva` (class-variance-authority)** — a literal `const x = cva(base, config)`
  and calls like `x({ variant, size })` fold to a static class string at build
  time, including `defaultVariants` and `compoundVariants`.
- **`cn` / `clsx` / `tailwind-merge`** — imported from `@krate/runtime`; a call
  with statically-known arguments folds to a literal class string (no hydration
  binding).
- **Rest-spread props** — `{...props}` on an intrinsic element expands to the
  call-site attributes, and `children` forwards through it.
- **Defaults** — `const Comp = asChild ? Slot : "button"` resolves at build time
  from the parameter default.
- **`asChild` / `Slot`** — a `<Slot>` (or a tag aliased to it) renders its single
  child element with the Slot's props (including `className`) merged onto it.
- **Attributes** — `data-*`, `aria-*`, and camelCase aliases pass through.

```tsx
// A copy of the shadcn/ui Button compiles to static HTML.
import { Slot, cva, cn } from '@krate/runtime';

const buttonVariants = cva('inline-flex items-center rounded-md', {
  variants: {
    variant: { default: 'bg-blue-600 text-white', destructive: 'bg-red-600 text-white' },
    size: { default: 'h-9 px-4', lg: 'h-10 px-8' },
  },
  defaultVariants: { variant: 'default', size: 'default' },
});

export function Button({ className, variant, size, asChild = false, ...props }: any) {
  const Comp = asChild ? Slot : 'button';
  return <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />;
}
```

`<Button variant="destructive" size="lg">Delete</Button>` renders
`<button class="… bg-red-600 text-white h-10 px-8">Delete</button>`, and
`<Button asChild><a href="/docs">Docs</a></Button>` renders the `<a>` with the
button classes merged on — all zero client JS.

The `cn`/`clsx`/`cva`/`twMerge`/`Slot`/`cloneElement` helpers ship in
`@krate/runtime`, so component libraries that normally depend on
`class-variance-authority`, `clsx`, `tailwind-merge`, or `@radix-ui/react-slot`
can import them from `@krate/runtime` instead.

## Compound (Radix-style) components

Radix UI exposes compound components through a **namespace API** —
`<Popover.Root>`, `<Popover.Trigger>`, `<Accordion.Item>`, and so on. Krate
resolves these dotted tags at build time, so the same authoring pattern works
whether the components come from your own source or a source-shipping library:

```tsx
import * as Accordion from '../components/ui/accordion';

<Accordion.Root>
  <Accordion.Item title="What is Krate?">…</Accordion.Item>
</Accordion.Root>
```

A namespace import (`import * as Card`) and a namespace re-export barrel
(`export * as Card from './card'`) both resolve `<Card.Root>` to the declared
`Root` function. This is the same shape Radix components use.

:::note
The published `@radix-ui/react-*` npm packages ship **compiled JavaScript only**
(no source), built on `react/jsx-runtime`, `React.*`, and internal `@radix-ui/*`
packages. Running those exact files would require a React runtime — which Krate
deliberately does not ship. Instead, use the built-in
[`@krate/components`](/docs/reference/component-library/) library, which
reimplements the common Radix/shadcn primitives as Krate source with the same
compound API, or copy the relevant component into your project. Both compile to
static HTML with no client runtime.
:::

## What is not supported

Krate transpiles React **syntax**, it does not run the React runtime. The
following are intentionally not implemented and will not behave like React:

- **Actual React package code.** Importing `react`, `react-dom`, or npm
  components built against them (for example `@radix-ui/react-*`) does not pull
  in a React runtime. Use the built-in [`@krate/components`](/docs/reference/component-library/)
  library, which reimplements the common UI primitives natively.
- **`React.Children.*`**, `cloneElement`, and `isValidElement`. Use the
  `children` prop directly.
- **Rules-of-hooks and call-order semantics.** Components run once per mount, so
  there is no hook ordering to preserve.

Event props follow React semantics: events **bubble** (a child handler does not
stop an ancestor handler unless it calls `stopPropagation()`), `onChange` maps to
`input`, `onFocus`/`onBlur` map to the bubbling `focusin`/`focusout`, and
non-bubbling events (`onMouseEnter`/`onMouseLeave`, `onScroll`, …) are attached
directly. `onClickCapture`-style capture handlers are supported. Controlled
`value`/`checked` bind by DOM property, so programmatic updates work after the
user has interacted.

If you rely on these, write the component in Krate's own style — see
[Reactivity](/docs/core-concepts/reactivity/) and
[Migrating from Next.js](/docs/guides/migrating-from-nextjs/).
