---
title: CSS Signals Errors
description: Every CSS-signal build error and how to fix it.
---

# CSS Signals Errors

CSS Signals have **no silent fallback**: a declaration that can't be compiled to
CSS fails the build with a hint. This page maps each error to its fix.

## State read / written in an unsupported place

**`the state is rendered as text; only showIf panels and onClick triggers are supported`**

The getter is used somewhere other than a `showIf` panel, a bare live-text read,
or a trigger's `onClick`. Fixes:

```tsx
<div showIf={tab() === 'a'}>…</div>   // a panel
<p>{tab()}</p>                        // live text (getter form)
<p>{tab.current()}</p>                // live text (handle form)
```
Anything else (arithmetic, passing to a function, an attribute) → use
`createSignal`.

**`the state is used in a dynamic attribute`** — a value like
`class={tab()}` or `data-x={open()}`. Use a `showIf` panel or a trigger instead;
or `createSignal` if you truly need a dynamic attribute.

**`the state is captured by a nested function`** — the scope is referenced
inside a nested `function`. Inline it (a trigger's `onClick` arrow may call the
setter) or use `createSignal`.

## Triggers

**`trigger <section> cannot become a <label>`** — triggers must be labelable:
`button`, `a`, `label`, `span`, `li`, `div`.

**`trigger <button> has a dynamic class`** / **`panel <div> has a dynamic class`**
— the trigger/panel must have a **static** class string (the compiler merges the
generated class in). Move dynamic classes to a child element.

## Values and options

**`needs a literal initial value`** / **`initial value must be a string or number literal`**
— the initial must be a literal, not a prop or expression. Set the real initial
at the call site with `defaultValue` (handles) or a literal in the declaration.

**`createCSSRange needs integer min/max/step literals`** — pass integer literals;
`createCSSRange` is discrete. For continuous values use `createSignal`.

**`createCSSFlags needs a literal array of flag names`** — pass a literal array.

**`createCSSStack must destructure [stack, { push, pop, clear }]`** — destructure
exactly that shape.

**`unknown "as" role preset`** — see the [role presets table](/docs/reference/css-signals/#aria-role-presets-as).

**`vars property <p> must map options to string literals`** — `vars` values must
be string literals (quote strings you intend for `content:`).

## Handles and composition

**`CSS scope helper spread at line N is not bound to a scope handle: is there a <Ctx.Provider value={scope}> above it, or is the handle passed as a prop?`**

`{...scope.trigger(...)}` couldn't resolve `scope`. Either the part is used
outside its provider, or the handle wasn't passed. Fix:

```tsx
// root
return <TabsCtx.Provider value={tab}>{children}</TabsCtx.Provider>;
// part
const tab = TabsCtx.use();
```

or pass it explicitly: `<Part scope={tab} />` then use `props.scope.trigger(...)`.

## Choosing the right tool

If a fix above says "use `createSignal`", the state isn't enumerable/CSS-expressible:

| Symptom | Do this |
|---------|---------|
| Options come from data | native control, or `createSignal` |
| You read/modify the value in JS | `createSignal` |
| You need controlled props | `createSignal` |
| Continuous value (drag) | `createSignal` |
| Dynamic trigger/panel class | `createSignal` (or move the class to a child) |

See [Limits](/docs/core-concepts/css-signals/#limits).
