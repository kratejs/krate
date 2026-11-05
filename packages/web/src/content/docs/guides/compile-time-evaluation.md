---
title: Compile-Time Evaluation
order: 11
description: How Krate evaluates known values, static lists, local helpers, and pure expressions during the build.
---

# Compile-Time Evaluation

Krate evaluates expressions during the build whenever their inputs are known
and they cannot change at runtime. The result is written as static HTML with no
hydration binding for that value.

```tsx
const stats = { open: 2, closed: 5 };
const total = stats.open + stats.closed;
const entries = Object.entries(stats);

return (
  <section data-total={total}>
    <p>{entries.map(([name, count]) => `${name}: ${count}`).join(", ")}</p>
  </section>
);
```

Both the attribute and the text above are folded at build time. The page emits
no runtime expression for either value.

## What is evaluated

- Literals, arrays, objects, templates, operators, and conditionals.
- Computed object keys (`{ [KEY]: value }`, `{ [`${a}-${b}`]: value }`,
  `{ [Kind.Primary]: value }`) whose key folds to a string or number. A key
  that does not fold (it reads a signal, or something only known at request
  time) leaves the whole object to the runtime; the key is never replaced by
  the expression's spelling.
- Module constants and imported constants that resolve to literal values.
- Component locals, including destructuring.
- Local helper functions whose bodies stay within the pure subset.
- Bounded `for`, `while`, and `for-of` loops that build known arrays.
- Deterministic built-ins such as `Math`, `Object`, `Array`, `JSON`, `String`,
  `Number`, `Boolean`, `parseInt`, and `parseFloat`.
- `.map()` and `.flatMap()` callbacks with inline functions, named local
  functions, destructured parameters, defaults, rest parameters, and
  index/array parameters.

## Static lists

A list whose source and callback are known at build time is flattened directly
into static slots:

```tsx
const items = [{ id: 1, title: "Home" }, { id: 2, title: "Docs" }];

<ul>
  {items.map(({ id, title }) => <li key={id}>{title}</li>)}
</ul>
```

Both `.map()` and `.flatMap()` flatten this way. This emits the two `<li>`
elements and **no list runtime, list markers, or `kbindList` call**. A list is
kept dynamic only when its source is signal-backed or otherwise unknown at
build time.

## Element types held in variables

`<Tag>` where `Tag` is a variable renders the element it names when the build
can fold the value to one tag:

```tsx
const Wrap = "section";

function Box({ as: Tag = "div", children }) {
  return <Tag>{children}</Tag>;
}

<Wrap><Box as="h2">Title</Box></Wrap>
```

A string constant, a prop the caller passes, a parameter default, and a
condition on those all fold. A tag that depends on a signal, on a value only
known at request time, or on an omitted prop with no default does not: the
build stops with a [KR8320](/docs/reference/diagnostics/#kr8320) error instead of
emitting an empty placeholder. Pass the tag as a string, render each case as
its own element, or put the component in a `*.runtime.tsx` file, where the tag
is an ordinary JavaScript value at request time.

## Namespace members as tags

A member tag of a namespace import renders the component the member names,
whatever the case of the namespace and however many barrel files sit between:

```tsx
import * as ui from "./ui"; // ui/index.ts: export { Button } from "./Button"

<ui.Button>Save</ui.Button>
```

The build follows `export { X } from`, `export { default as X } from`,
`export *` and `export * as group from` to the function that declares the
component, and `<ui.group.Input />` resolves the same way. A member that
does not exist, or is not a function component, stops the build with a
[KR8321](/docs/reference/diagnostics/#kr8321) error that names the tag, instead
of emitting an unknown element. Members of an npm React library are mounted as
islands, as named imports are.

## Module values changed by top-level code

A module-level value is not always its initializer. Top-level statements that
change it run at build time, in source order, before anything reads it:

```tsx
const squares = [];
for (let i = 0; i < 3; i++) squares.push(i * i);
let total = 0;
for (const s of squares) total += s;

<ul>{squares.map((s) => <li>{s}</li>)}</ul> // three items
<p>{total}</p>                                // 5
```

Assignments, `++`, property writes, `push` and other method calls, loops,
branches, `Object.assign`, and calls to functions of the module that do any of
these are run, in the module that declares the value and in the modules it is
imported from. Code that only reads a value (`console.log(cfg)`,
`window.__cfg = cfg`) or that runs later (a callback passed to
`addEventListener`) does not affect it, and a module with no such statements
folds from its initializers exactly as before.

When a statement cannot be run at build time (it reads the browser, the clock
or an imported function, or sorts or reverses in place), the values it may
have changed stay unknown instead of keeping their initial value. A component
that hydrates reads them in the browser, where the hydration script replays the
statement. A component rendered at build time (`@static`, `@server`) has no run
time, so the build fails with [KR8322](/docs/reference/diagnostics/#kr8322).
Make the top-level code plain, or compute the value in one expression.

## Reactive values

A known initial value is not the same as a static value. If an expression reads
a signal, the initial value is used for SSR output but the hydration binding is
retained:

```tsx
const [count, setCount] = createSignal(0);

<p>{count() + 1}</p>
```

The `0 + 1` initial is rendered on the server, then `count()` remains reactive
after hydration.

## Purity rule

Krate does not evaluate values that depend on time, randomness, I/O, the
environment, or imported executable code. Expressions using `Date`,
`Math.random`, `fetch`, `process`, `Intl`, or locale-dependent string methods
stay runtime values. This keeps builds deterministic and reproducible.

A constrained JavaScript-engine fallback handles pure built-ins that the Go
constant interpreter does not model, such as `new Array(...)`. The fallback only
runs after a purity check and only when every identifier in the expression is a
known local value or an allowed global.

## Notes

- A local helper is folded only when its body is evaluable without side effects.
- A loop must have a statically known bound and a pure body.
- Unsupported or impure expressions are left to the runtime; they are never
  silently replaced with a partial value.
- Static `.map()` output still honors child component hydration and props.
