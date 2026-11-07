---
title: CSS Signals
description: Zero-JS interactive state compiled to hidden inputs and :has() CSS.
order: 7
---

# CSS Signals

CSS Signals are interactive state primitives that compile to **zero client
JavaScript**: hidden `<input>` controllers, `<label>` triggers, and `:has()` CSS.
Tabs, accordions, disclosures, dropdowns, dialogs, popovers, toasts, filter
chips, steppers, and progress bars all work with no hydration bundle.

The state lives in the DOM and the cascade, not in JS.

:::warning Experimental
CSS Signals are **experimental**. The API, generated markup and class names, and
build-time behavior may change between releases.
:::

## Two ways to author

Both compile to the same output. Pick per component.

**1. Getter/setter (`showIf` panels):**

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

**2. Scope handles** (first-class values with `trigger`/`panel`/`close`):

```tsx
function Tabs() {
  const tab = createCSSChoice('overview');

  return (
    <div>
      <button {...tab.trigger('overview')}>Overview</button>
      <button {...tab.trigger('features')}>Features</button>
      <div {...tab.panel('overview')}>Overview content</div>
      <div {...tab.panel('features')}>Features content</div>
    </div>
  );
}
```

The helpers expand at build time into the exact `class`/`for`/ARIA wiring, so you
own the markup. A handle is a plain value - pass it as a prop or through context
(see [Composing across components](#composing-across-components)).

## The primitives

| Primitive | Shape | Compiles to |
|-----------|-------|-------------|
| `createCSSChoice(initial, options?)` | one-of-N | radio group |
| `createCSSToggle(initial)` | on/off | single checkbox |
| `createCSSFlags([...])` | N independent booleans | checkbox per flag |
| `createCSSGroup(initial, opts?)` | optional one-of-N (initial may be `null`) | radio group + closed sentinel |
| `createCSSRange(initial, { min, max, step })` | discrete integer | indexed radio chain |
| `createCSSStack(['root'], opts?)` | declared navigation tree | nested radio levels |

**Options** may be given as an array (`createCSSChoice('a', ['a', 'b'])`) or an
options object (`createCSSChoice('a', { options: ['a', 'b'], as: 'tabs' })`).
When omitted, they are inferred from the literals you use (setter calls,
`trigger`/`panel`/`is` arguments, or call-site `value` props).

## The handle methods

| Method | Use |
|--------|-----|
| `scope.trigger(value?)` | spread on the trigger element -> `<label for>` |
| `scope.panel(value?)` | spread on a panel element -> `:has()`-toggled wrapper |
| `scope.close()` | spread on an element that closes a group (sentinel) |
| `scope.current()` | renders the selected value as **live text** (zero JS) |
| `scope.is(value)` / `on()` / `off()` | condition atoms for `showIf` |

## Composing across components

A scope handle is a value, so composition is ordinary prop passing or context.
The base library uses context so parts read the scope without the consumer
repeating it:

```tsx
import { createScope, createCSSChoice } from '@krate/runtime';

const TabsCtx = createScope<string>();

export function Tabs(props) {
  const tab = createCSSChoice('overview');
  return (
    <TabsCtx.Provider value={tab}>
      <div class="tabs">{props.children}</div>
    </TabsCtx.Provider>
  );
}

export function Tab(props) {
  const tab = TabsCtx.use();
  return <button class="tab" {...tab.trigger(props.value)}>{props.children}</button>;
}
```

```tsx
<Tabs defaultValue="overview">
  <Tab value="overview">Overview</Tab>
  <Tab value="features">Features</Tab>
</Tabs>
```

The compiler resolves `TabsCtx.use()` to the nearest
`<TabsCtx.Provider value={scope}>` and rewrites the spreads. You can also pass a
handle directly: `<Tab scope={tab} value="overview"/>`.

A handle passed as a prop reaches any depth, through components with or without
client state, and through `{...props}` spreads. The handle only exists at build
time: the receiving components use `scope.trigger()`, `.panel()`, `.close()`
and `.current()` in their JSX, and the compiler expands them against the
declaring component. A component that reads the handle in code that runs in the
browser (an event handler, an effect, a computed value), or an island from an
npm package that receives one, fails the build with
[KR8330](/docs/reference/diagnostics/#kr8330).

The getter and setter of the destructured form (`const [tab, setTab] = ...`)
are run-time functions and can be passed to components with client state. A
component without client state is rendered at build time, so it cannot read
them; the build fails with [KR8331](/docs/reference/diagnostics/#kr8331). Pass
the scope handle instead.

## Compound conditions

Panels accept arbitrary boolean logic over CSS state - `&&`, `||`, `!`,
`===`/`!==` - across any combination of scopes:

```tsx
const [plan, setPlan] = createCSSChoice('free', ['free', 'pro']);
const [admin, setAdmin] = createCSSToggle(false);

<div showIf={plan() === 'pro' && admin()}>Pro + admin</div>
<div showIf={plan.is('pro') || admin.on()}>Either (handle form)</div>
```

The compiler normalizes the expression to DNF (OR of AND-terms), compiles each
atom to a `:has()` / `:not(:has())` fragment, and deduplicates equivalent
conditions - still zero JS.

## Live text and custom properties

Reading a getter as text, or `scope.current()`, compiles to a `.krc-live` element
whose `::after` content is the inherited `--krate-current` custom property:

```tsx
<p>Selected plan: {plan()}</p>          {/* getter form */}
<p>Selected plan: {plan.current()}</p>  {/* handle form */}
```

Every choice-like scope publishes `--krate-current`; toggles publish `"on"`/
`"off"`. Pass `vars` to publish more inheritable properties for computed
text/themes:

```tsx
const [tier] = createCSSChoice('solo', {
  options: ['solo', 'pro'],
  vars: { '--price': { solo: '"$9"', pro: '"$29"' } },
});
```

```css
.price::after { content: var(--price); }
```

## Forms

Pass a `name` to submit the selected value under a friendly key, and `disabled`
to disable specific options:

```tsx
const [plan, setPlan] = createCSSChoice('free', {
  options: ['free', 'pro', 'enterprise'],
  name: 'plan',
  disabled: ['enterprise'],
});
```

The controllers submit `plan=<selected>`; disabled options' inputs are
`disabled` and their triggers get `aria-disabled`.

## ARIA roles (`as`)

Native defaults (radio groups, checkboxes) ship **no JavaScript**. Roles that
need synthesized state or keyboard handling inject a tiny inline enhancer (still no page bundle; about 3 KB, once per page):

| `as` | Widget | Extra JS |
|------|--------|----------|
| *(default)* | radio group / checkboxes | none |
| `switch` | on/off | none |
| `tabs` / `tablist` | tabbed panels | enhancer |
| `listbox` | options list | enhancer |
| `accordion` / `disclosure` | single-open | enhancer |
| `menu` / `menubar` | dropdown | enhancer |
| `nav` | disclosure navigation | enhancer |
| `dialog` / `modal` | modal | enhancer |
| `popover` | popover | enhancer |

Pass `aria: false` to opt out, or `aria: { role, panelRole }` for raw overrides.

## Positioning and motion

- **Anchor positioning**: an overlay scope (`as: 'popover' | 'menu' | 'menubar'`)
  anchors its panel to the trigger with `anchor()` by default; set
  `anchor: 'bottom-start' | 'top' | 'right' | ...` to choose a side. Progressive -
  browsers without support use the component's own CSS.
- **Transitions**: `transition: true` fades/slides a panel when it opens
  (`@starting-style`). The transition is emitted under
  `prefers-reduced-motion: no-preference`, so reduced-motion users get the panel
  without animation. Closing is never animated.

## How it compiles

- A **scope anchor** carries the class (e.g. `krc0`) and the hidden controller
  inputs. A single intrinsic root element gets the class directly; a fragment or
  component root wraps in `<div style="display:contents">`.
- **Panel wrappers** are `display:contents` elements that CSS toggles between
  `display:none` and `display:contents`, so a panel keeps its own `display`.
- **Class names** are compact (`krc0`, `krc0-r-overview`) and the index is
  base62-encoded. A selected trigger gets `--krate-active*`; a focused control
  outlines its label via `:has(:focus-visible)`.

## Browser support

Requires `:has()` (2023+: Chrome/Edge 105+, Safari 15.4+, Firefox 121+). Krate
emits a `@supports not selector(:has(*))` fallback that **reveals all panels**
(static, non-interactive) so content is never blank on older browsers.

For full interactivity on older engines, set a compat mode: `compat: { mode:
"legacy" }` compiles the same scopes to class-based CSS driven by a tiny scoped
shim, and `compat: { mode: "hybrid" }` ships both and picks at runtime. See
[Browser Support](/docs/core-concepts/browser-support/).

## Limits

CSS Signals express a fixed, enumerated state with no JS. Reach for
`createSignal` when you need any of:

- **Data-driven options** (options known only at runtime).
- **Reading or writing the value from JS** (imperative control, effects, logic).
- **Controlled props** - there is no `value`/`onChange`; state is compiled.
- **Continuous values** (a drag slider) - `createCSSRange` is discrete integers.
- **Arbitrary push/pop history** - `createCSSStack` is a declared tree.

Also: a trigger must be a labelable element (its `class` may be computed, for
example a forwarded `class` prop); option values must be **literals**, or
come from a `.map()` over an array that is known at build time - parts built
that way are unrolled and each value gets its rules. A value that cannot be read
at build time is reported as `KR8820`, because its panel would never be hidden. A
non-compilable declaration is a **hard build error**, never a silent fallback.

| Need | Use |
|------|-----|
| Enumerable zero-JS UI state | a CSS Signal |
| Arbitrary/inspected state | `createSignal` |
| Data-driven options / form value | native controls (`<NativeSelect>`, `<input>`) |

See the [CSS Signals reference](/docs/reference/css-signals/) for the full API
and the [cookbook](/docs/guides/css-signals-cookbook/) for patterns.
