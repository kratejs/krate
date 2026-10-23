---
title: Compile-Time Evaluation
order: 8
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
