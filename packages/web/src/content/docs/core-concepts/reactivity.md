---
title: Reactivity
description: Signals, effects, memos, context, and async resources in the Krate runtime.
order: 2
---

# Reactivity: Signals, Effects & Memos

Krate's reactivity system is inspired by SolidJS: fine-grained, signal-based,
with no virtual DOM and no diffing.

All primitives are exported from `@krate/runtime`.

## Signals

```tsx
import { createSignal } from '@krate/runtime';

const [count, setCount] = createSignal(0);
```

- `count()` reads the current value.
- `setCount(next)` sets the value and notifies subscribers.
- `setCount(prev => ...)` performs a functional update.

Signals are read inside components and effects; the compiler and runtime track
which effects depend on which signals automatically.

## Effects

```tsx
import { createEffect } from '@krate/runtime';

const dispose = createEffect(() => {
  console.log(`count is ${count()}`);
});
dispose(); // remove the effect
```

An effect re-runs whenever any signal it reads changes. The tracker works via a
context stack, so nested reads inside `createEffect` are captured correctly.

## Memos

```tsx
import { createMemo } from '@krate/runtime';

const doubled = createMemo(() => count() * 2);
doubled(); // memoized until count changes
```

A memo caches its computed value and only recomputes when its dependencies
change. Only the final consumer re-renders — the memo itself is cached.

## Cleanup & mount

```tsx
import { onCleanup, onMount } from '@krate/runtime';

createEffect(() => {
  const timer = setInterval(() => {}, 1000);
  onCleanup(() => clearInterval(timer)); // runs before re-run and on dispose
});

onMount(() => {
  // runs once after the initial synchronous render completes
});
```

## Context

```tsx
import { createContext } from '@krate/runtime';

const ThemeCtx = createContext('light');

// ThemeCtx.Provider — wraps children with a value
// ThemeCtx.useContext() — reads the nearest provider value
// ThemeCtx.defaultValue — fallback when no provider is present
```

## Resources

```tsx
import { createResource } from '@krate/runtime';

const [user, { mutate, refetch }] = createResource(
  () => userId(),                                  // reactive source
  async (id) => fetch(`/api/user/${id}`).then(r => r.json()), // fetcher
);

user()            // current data (or undefined)
user.loading      // boolean
user.error        // error or undefined
user.state        // 'unresolved' | 'loading' | 'ready' | 'error' | 'refreshing'
mutate(prev => ({ ...prev, name: 'new' }))  // optimistic update
refetch()         // trigger a re-fetch
```

Resources re-fetch automatically when their source signal changes and abort
in-flight requests when the source changes mid-flight.

## Conditional rendering (`showIf`)

For conditionally rendering an element, Krate supports a `showIf` prop — sugar
for the `{expr && <el/>}` guard:

```tsx
const [count] = createSignal(0);

// These are equivalent:
{count() > 0 && <p>Positive</p>}
<p showIf={count() > 0}>Positive</p>
```

- Works on intrinsic elements (`<div>`) **and** components (`<List>`).
- `visibleIf` is accepted as an alias.
- A bare attribute (`<p showIf>`) is always true.
- The attribute is compiler-erased: it never appears in the emitted HTML and is
  never passed to a component's props.
- A statically-known test folds at build time (the losing branch is not
  emitted); a reactive test becomes part of the hydration bundle and toggles the
  element's visibility as the signal changes.

## CSS Signals

Krate provides three **zero-JS** state primitives compiled entirely to hidden
`<input>` controllers, `<label>` triggers, and `:has()` CSS. They ship no
JavaScript at all — the state lives in the DOM and the cascade:

| Primitive | Shape | Underlying control |
|-----------|-------|--------------------|
| `createCSSChoice(initial, options?)` | one-of-N | radio group |
| `createCSSToggle(initial)` | on/off | single checkbox |
| `createCSSFlags([...])` | N independent booleans | checkbox per flag |
| `createCSSGroup(initial \| null, opts?)` | optional one-of-N | radio group + closed sentinel |
| `createCSSRange(initial, { min, max, step })` | discrete number | indexed radio chain |
| `createCSSStack(['root'], opts?)` | navigation path | nested radio levels |

### `createCSSChoice` — mutually-exclusive state

```tsx
import { createCSSChoice } from '@krate/runtime';

function Tabs() {
  const [tab, setTab] = createCSSChoice('overview');

  return (
    <div>
      <button onClick={() => setTab('overview')}>Overview</button>
      <button onClick={() => setTab('features')}>Features</button>

      <div showIf={tab() === 'overview'}>Overview content</div>
      <div showIf={tab() === 'features'}>Features content</div>
    </div>
  );
}
```

The setter may only be called with a **literal** from an `onClick` handler;
those literals (plus the initial value) define the option universe. Pass an
explicit array to override: `createCSSChoice('a', ['a', 'b', 'c'])`. Panels use
the same `showIf` prop with a `tab() === 'x'` test.

### `createCSSToggle` — a single boolean

```tsx
const [dark, setDark] = createCSSToggle(false);
<button onClick={() => setDark(!dark())}>Theme</button>
<div showIf={dark()}>Dark</div>
<div showIf={!dark()}>Light</div>
```

### `createCSSFlags` — independent booleans

```tsx
const [flags, setFlag] = createCSSFlags(['bold', 'italic']);
<button onClick={() => setFlag('bold', !flags.bold())}>Bold</button>
<div showIf={flags.bold()}>Bold is on</div>
<div showIf={!flags.italic()}>Italic is off</div>
```

Each flag is an independent checkbox, so any combination can be active at once.

### Compound conditions

`showIf` accepts arbitrary boolean logic over CSS signal state — `&&`, `||`,
`!`, `===` / `==`, and `!==` / `!=` — across any combination of scopes:

```tsx
const [plat, setPlat] = createCSSChoice('mac', ['mac', 'win']);
const [licensed, setLicensed] = createCSSToggle(false);

// AND across scopes
<div showIf={plat() === 'mac' && licensed()}>Approve for macOS</div>

// OR
<div showIf={plat() === 'win' || !licensed()}>Upgrade</div>

// negation
<div showIf={plat() !== 'mac'}>Non-Mac build</div>
```

The compiler normalizes the expression to DNF (OR of AND-terms) and compiles
each atom to a `:has()` / `:not(:has())` fragment on the shared scope anchor,
so every combination stays zero-JS. Equivalent conditions (e.g. `a && b` and
`b && a`, or `!(a || b)` and `!a && !b`) are deduplicated to a single wrapper
class. An expression the compiler cannot classify is still a hard error.

### `createCSSGroup` — optional one-of-N

An optional radio group: like a choice, but with an explicit **closed** state
(`null`). This is the shape behind single-open accordions, disclosures, dialogs,
popovers, and dropdown menus.

```tsx
const [open, setOpen] = createCSSGroup(null, { as: 'accordion' });

<button onClick={() => setOpen('faq')}>FAQ</button>
<div showIf={open() === 'faq'}>Answer</div>
<button onClick={() => setOpen(null)}>Close</button>
```

### `createCSSRange` — a discrete stepper / slider / progress

A stepped integer range compiled to an indexed radio chain. Set it with a
literal index or a relative step (`r() + 1` / `r() - 1`), which the compiler
turns into bounded stepper labels. A `.krcN-fill` element gets a proportional
width for progress indicators; a track of tick labels gives a stepped slider.

```tsx
const [step, setStep] = createCSSRange(0, { min: 0, max: 4, step: 1 });

<button onClick={() => setStep(step() - 1)}>Back</button>
<button onClick={() => setStep(step() + 1)}>Next</button>
<div class="fill"></div>
```

Continuous drag is not expressible in pure CSS; use `createSignal` for that.

### `createCSSStack` — drill-down navigation

A navigation stack over a **declared** tree of nodes. The compiler infers the
tree from where each literal `push('node')` appears, so `top()` reads the
deepest open node and `pop()` returns to its parent.

```tsx
const [stack, { push, pop, clear }] = createCSSStack(['root']);

<button onClick={() => push('settings')}>Settings</button>
<div showIf={stack.top() === 'settings'}>
  <button onClick={() => pop()}>Back</button>
  <button onClick={() => push('notifications')}>Notifications</button>
</div>
```

Push targets must be literals so the tree is known at build time; a stack
expresses a reachable path, not arbitrary push/pop history.

### Live text & custom properties

Reading a getter as JSX text (`{tab()}`) compiles to a **live value with zero
JS**: the compiler emits a `.krc-live` element whose `::after` content is the
inherited `--krate-current` custom property, updated by the same `:has()` rules
that toggle panels.

```tsx
const [plan, setPlan] = createCSSChoice('solo', ['solo', 'pro']);
<p>Selected plan: {plan()}</p>   // → <span class="krc-live"></span>
```

Every choice-like scope publishes `--krate-current` (the quoted option token);
toggles publish `"on"`/`"off"`. Pass `vars` to publish more inheritable
properties — handy for computed prices or themed variants:

```tsx
const [tier, setTier] = createCSSChoice('solo', {
  options: ['solo', 'pro'],
  vars: { '--price': { solo: '"$9"', pro: '"$29"' } },
});
```

```css
.price::after { content: var(--price); }
```

Values are emitted verbatim, so quote strings you intend for `content:`. Values
must be string literals; a non-literal `vars` value is a build error.

### ARIA roles (`as`)

Every primitive accepts an options object with an `as` role preset. Native
defaults (radio groups, checkboxes) ship **no JavaScript**. Presets that need
synthesized state — `tabs`, `listbox`, `accordion`, `disclosure`, `dialog`,
`menu`, `popover` — inject a tiny ARIA synchroniser automatically (still no page
hydration bundle).

| `as` | Widget | Emits |
|------|--------|-------|
| *(default)* | radio group / checkboxes | native semantics, zero JS |
| `switch` | on/off | `role="switch"` on the checkbox |
| `tabs` | tabbed panels | `tablist` / `tab` / `tabpanel`, `aria-selected` |
| `listbox` | options list | `listbox` / `option`, `aria-selected` |
| `accordion` | single-open disclosure | `button` / `region`, `aria-expanded` |
| `dialog` / `modal` | modal | `aria-haspopup`, `aria-modal` |
| `menu` / `popover` | dropdown | `menu` / `menuitemradio` |

```tsx
const [tab, setTab] = createCSSChoice('a', { as: 'tabs' });
```

Pass `aria: false` to opt out entirely, or `aria: { role: '…', panelRole: '…' }`
for raw overrides (`role`, `panelRole`, `container-role` are recognised).

### How it compiles

The scope class (e.g. `.krc0`) is merged onto the component's root element;
when the component returns a fragment or a component root it is wrapped in a
layout-transparent `<div style="display:contents">` instead, so any valid return
shape works. Panels are wrapped in a `display:contents` element that the CSS
toggles, so a panel's own `display` (e.g. `.panel { display:flex }`) is
preserved when visible.

- Panel bodies are compiled like ordinary JSX — nested components, lists, and
  nested conditionals all work, and interactive children still hydrate normally.
- Triggers must be a labelable element (`<button>`, `<a>`, `<label>`, `<span>`,
  `<li>`, `<div>`) with a static class.
- These primitives are **not** a fallback for arbitrary reactive state: if a
  component can't be compiled (the state read as text, used in a dynamic
  attribute, captured by another function, set to a non-literal, a dynamic
  trigger class, a non-labelable trigger, …) the build **fails** with an error
  telling you to use `createSignal` instead. There is no silent fallback.
- `:has()` requires a 2023+ browser. Controllers are visually hidden but remain
  focusable, so keyboard navigation and screen readers keep working.

The generated class names are deliberately compact (`krc0`, `krc0-r-overview`)
and the scope index is base62-encoded. A selected trigger gets a default
underline via the `--krate-css-active` and `--krate-css-active-fg` custom
properties; override them to theme the selection.

## Compile-time validation

The reactive dependency graph is validated at build time and surfaced as `⚠`
warnings. The compiler detects:

- **Unused signals** — declared but never read or written.
- **Write-only effects** — effects that call setters but read no signals.
- **No-read effects** — effects that read and write nothing (run exactly once).
- **Circular dependencies** — including self-referential feedback loops like
  `setCount(count() + 1)`.

These warnings help you catch bugs before they reach the browser.
