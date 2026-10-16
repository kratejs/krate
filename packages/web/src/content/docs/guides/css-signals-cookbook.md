---
title: CSS Signals Cookbook
description: Zero-JS patterns for common widgets.
---

# CSS Signals Cookbook

Copy-paste patterns for common widgets. All are zero-JS (plus the small inline
ARIA synchroniser where the role needs it). See the
[guide](/docs/core-concepts/css-signals/) and
[reference](/docs/reference/css-signals/).

## Tabs

```tsx
const tab = createCSSChoice('a', { as: 'tabs' });
return (
  <div>
    <div role="tablist">
      <button {...tab.trigger('a')}>A</button>
      <button {...tab.trigger('b')}>B</button>
    </div>
    <div {...tab.panel('a')}>A panel</div>
    <div {...tab.panel('b')}>B panel</div>
  </div>
);
```

## Accordion (single-open, closable)

```tsx
const open = createCSSGroup(null, { as: 'accordion' });
return (
  <div>
    <button {...open.trigger('q1')}>Question 1</button>
    <div {...open.panel('q1')}>Answer 1</div>
    <button {...open.trigger('q2')}>Question 2</button>
    <div {...open.panel('q2')}>Answer 2</div>
  </div>
);
```

Re-clicking an open trigger closes it (the compiler emits a paired close label).

## Disclosure (open/close)

```tsx
const open = createCSSGroup(null, { as: 'disclosure' });
<button {...open.trigger('body')}>Toggle</button>
<div {...open.panel('body')}>Body</div>
```

## Dropdown menu

```tsx
const menu = createCSSGroup(null, { as: 'menu', anchor: 'bottom-start' });
<div style="position:relative">
  <button {...menu.trigger('m')}>Open</button>
  <div {...menu.panel('m')}>
    <button {...menu.close()}>Profile</button>
    <button {...menu.close()}>Sign out</button>
  </div>
</div>
```

## Dialog / drawer

```tsx
const dlg = createCSSGroup(null, { as: 'dialog' });
<button {...dlg.trigger('d')}>Open</button>
<div {...dlg.panel('d')}>
  <h2>Title</h2>
  <button {...dlg.close()}>Close</button>
</div>
```

Style the panel fixed/centered (or anchored) via CSS.

## Filter chips (multi-select)

```tsx
const f = createCSSFlags(['vegan', 'spicy']);
<div>
  <button {...f.trigger('vegan')}>Vegan</button>
  <button {...f.trigger('spicy')}>Spicy</button>
  <div showIf={f.is('vegan') && f.is('spicy')}>Vegan + spicy</div>
</div>
```

## Stepper / progress

```tsx
const step = createCSSRange(1, { min: 0, max: 4, step: 1 });
<div>
  <button onClick={() => setStep(step() - 1)}>Back</button>
  <button onClick={() => setStep(step() + 1)}>Next</button>
  <div class="progress"><div class="krc0-fill"></div></div>
</div>
```

## Theme / variant switcher with live text + vars

```tsx
const tier = createCSSChoice('solo', {
  options: ['solo', 'pro'],
  name: 'tier',
  vars: { '--price': { solo: '"$9"', pro: '"$29"' } },
});
<button {...tier.trigger('solo')}>Solo</button>
<button {...tier.trigger('pro')}>Pro</button>
<p class="price">Price: <span class="krc-live"></span></p>
```

```css
.price::after { content: var(--price); }
```

## Wizard (drill-down)

```tsx
const [stack, { push, pop, clear }] = createCSSStack(['home']);
<button onClick={() => push('settings')}>Settings</button>
<div showIf={stack.top() === 'settings'}>
  <strong>Settings</strong>
  <button onClick={() => push('sound')}>Sound</button>
  <button onClick={() => pop()}>Back</button>
</div>
<div showIf={stack.top() === 'sound'}>
  <strong>Sound</strong>
  <button onClick={() => pop()}>Back</button>
</div>
```

## Toast (dismissable)

```tsx
const open = createCSSToggle(true);
<div {...open.panel(true)}>
  Saved. <button {...open.close()}>Dismiss</button>
</div>
```

## Reusing a scope across components

```tsx
const TabsCtx = createScope<string>();
// root
const tab = createCSSChoice('a');
return <TabsCtx.Provider value={tab}>{children}</TabsCtx.Provider>;
// part
const tab = TabsCtx.use();
return <button {...tab.trigger(props.value)}>{props.children}</button>;
```
