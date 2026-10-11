---
title: CSS Signals API
description: Reference for the zero-JS CSS-signal primitives and scope handles.
order: 4
---

# CSS Signals API

All primitives are imported from `@krate/runtime` and compile to zero client
JavaScript (a small inline ARIA synchroniser is added only for roles that need
it). See the [CSS Signals guide](/docs/core-concepts/css-signals/) for concepts.

:::warning Experimental
CSS Signals are **experimental**. The API, generated markup and class names, and
build-time behavior may change between releases.
:::

## Return value

Every primitive returns a **`CSSScope<T>`** (or `CSSFlagsScope<K>`): the
`[getter, setter]` tuple **plus** helper methods.

```ts
type CSSScope<T> = [() => T, (next: T) => void] & {
  trigger(value?: T): Spread;   // trigger element
  panel(value?: T): Spread;     // panel element
  close(): Spread;              // close a group (sentinel)
  current(): T;                 // live text
  is(value: T): boolean;        // condition atom (choice/group/range/flags)
  on(): boolean;                // condition atom (toggle)
  off(): boolean;               // negated atom (toggle)
};
```

Destructure the tuple (`const [tab, setTab]`) or keep the handle
(`const tab`).

## Primitives

### `createCSSChoice(initial, options?)`

One-of-N (radio group). Returns `CSSScope<T>`.

```ts
createCSSChoice('a');                          // options inferred
createCSSChoice('a', ['a', 'b', 'c']);          // explicit array
createCSSChoice('a', { options: ['a', 'b'], as: 'tabs', label: 'Sections' });
```

### `createCSSToggle(initial)`

A single boolean (checkbox). Returns `CSSScope<boolean>`.

```ts
const t = createCSSToggle(false);
<button {...t.trigger()}>Toggle</button>
<div {...t.panel(true)}>On</div>
<div {...t.panel(false)}>Off</div>
<div showIf={t.on()}>On</div>
```

### `createCSSFlags(flags, options?)`

N independent booleans (one checkbox each). Returns `CSSFlagsScope<K>`.

```ts
const f = createCSSFlags(['bold', 'italic']);
<button {...f.trigger('bold')}>Bold</button>
<div showIf={f.is('italic')}>Italic is on</div>
```

### `createCSSGroup(initial | null, options?)`

Optional one-of-N with a `null` "closed" state — the shape behind accordions,
disclosures, dialogs, popovers, menus. Returns `CSSScope<T | null>`.

```ts
const g = createCSSGroup(null, { as: 'accordion', label: 'FAQ' });
<button {...g.trigger('q1')}>Q1</button>
<div {...g.panel('q1')}>A1</div>
<button {...g.close()}>Close</button>
```

### `createCSSRange(initial, { min, max, step })`

A discrete integer (indexed radio chain). Stepper triggers accept a literal
index or a relative step (`setR(r() + 1)` / `r() - 1`). A `.krcN-fill` element
gets a proportional width for progress indicators.

```ts
const r = createCSSRange(2, { min: 0, max: 4, step: 1 });
<button onClick={() => setR(r() + 1)}>Next</button>
<div class="fill"></div>
```

Continuous values are not expressible in pure CSS — use `createSignal`.

### `createCSSStack(nodes, options?)`

A declared navigation tree (nested radio levels). The compiler infers the tree
from where each literal `push('node')` appears; `top()` reads the deepest open
node, `pop()` returns to its parent. A stack expresses a reachable path, not
arbitrary push/pop history.

```ts
const [stack, { push, pop, clear }] = createCSSStack(['root']);
<button onClick={() => push('settings')}>Settings</button>
<div showIf={stack.top() === 'settings'}><button onClick={() => pop()}>Back</button></div>
```

## Options object

| Key | Type | Meaning |
|-----|------|---------|
| `options` | `T[]` | explicit option universe |
| `as` | role preset | ARIA surface (see below) |
| `label` | `string` | accessible label for the scope container |
| `name` | `string` | submit the selected value under this `name` |
| `disabled` | `T[]` | options whose controller input is disabled |
| `anchor` | side | anchor-position panels (`bottom-start`, `top`, `right`, …) |
| `transition` | `boolean` | fade/slide panels on open |
| `aria` | `false`, or an object with `role`/`panelRole`/`container-role` | raw ARIA overrides |
| `vars` | `{ '--prop': { option: 'value' } }` | per-option custom properties |

## ARIA role presets (`as`)

| Preset | Container | Trigger | Panel | Runtime |
|--------|-----------|---------|-------|---------|
| *(none)* | — | — | — | none |
| `switch` | — | `role=switch` | — | none |
| `checkbox` | — | — | — | none |
| `radiogroup` / `radio` | `radiogroup` | — | — | none |
| `tabs` / `tablist` | `tablist` | `tab` | `tabpanel` | +aria-selected |
| `listbox` | `listbox` | `option` | — | +aria-selected |
| `menu` | `menu` | `menuitemradio` | `menu` | +aria-selected |
| `menubar` | `menubar` | `menuitemradio` | `menu` | +aria-selected |
| `accordion` / `disclosure` | — | `button` | `region` | +aria-expanded |
| `dialog` / `modal` | — | `button` | `dialog` | +aria-expanded |
| `popover` | — | `button` | `group` | +aria-expanded |

## Composition

`createScope<T>()` / `createScopeFlags<K>()` create a typed context carrying a
scope handle. `Ctx.use()` (alias `useContext()`) reads it.

```ts
const TabsCtx = createScope<string>();
// root:    <TabsCtx.Provider value={createCSSChoice('a')}>…</TabsCtx.Provider>
// part:    const tab = TabsCtx.use();
// prop:    <Part scope={tab} />  →  props.scope.trigger(props.value)
```

The compiler resolves `Ctx.use()` to the nearest provider and rewrites spreads,
so no JS ships.

## Generated output

- Scope class `krc<index>` on the anchor; controllers are hidden `<input>`s
  (class `krc-h`). Panel wrappers `krc<index>-p-<opt>`; triggers
  `krc<index>-t-<opt>`; a group's close label `krc<index>-ta-<opt>`; compound
  condition wrappers `krc<index>-x-<n>`.
- Theme via `--krate-css-active`, `--krate-css-active-fg`; live text via
  `--krate-current`.
- `:has()` fallback: `@supports not selector(:has(*))` reveals all panels.
