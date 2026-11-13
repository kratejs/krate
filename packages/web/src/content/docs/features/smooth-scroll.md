---
title: Smooth Scroll
description: Lenis smooth scrolling, configured from krate.config.
order: 13
---

# Smooth Scroll

Krate integrates [Lenis](https://lenis.darkroom.engineering) (MIT, by darkroom.engineering) as an opt-in feature. Set `smoothScroll` in `krate.config.ts`; nothing from Lenis is built or shipped while the key is unset.

```typescript
import { defineConfig } from "@krate/core";

export default defineConfig({
  smoothScroll: true,
});
```

`true` uses the Lenis defaults. An object enables it and tunes it:

```typescript
export default defineConfig({
  smoothScroll: {
    lerp: 0.08,
    wheelMultiplier: 1.1,
    anchors: { offset: -80 },
    prevent: ".modal, .map",
  },
});
```

Set `enabled: false` inside the object to turn it off without deleting the options.

## What ships

The build vendors Lenis and writes one hashed chunk, `chunks/lenis.<hash>.js`, holding the library and your options. Every page loads it with `defer`, including pages with no other client JavaScript, and inlines the small Lenis stylesheet. The lenis-snap plugin is added only when `snap` is enabled. The instance is available as `window.lenis`.

## Options

Options follow the [Lenis options](https://github.com/darkroomengineering/lenis#instance-settings). Anything you leave out keeps the Lenis default. Options that are functions or DOM nodes in Lenis are expressed as data here.

| Option | Meaning |
|--------|---------|
| `lerp` | Interpolation per frame, above 0 and at most 1 (Lenis default 0.1) |
| `duration` | Animation time in seconds; switches to time-based easing |
| `easing` | A preset name or `cubic-bezier(x1, y1, x2, y2)` (see below) |
| `smoothWheel` | Smooth mouse wheel input (default true) |
| `syncTouch`, `syncTouchLerp`, `touchInertiaExponent` | Mimic touch inertia while keeping scroll in sync |
| `infinite` | Infinite scrolling |
| `orientation`, `gestureOrientation` | `vertical` or `horizontal`; gestures also accept `both` |
| `wheelMultiplier`, `touchMultiplier` | Scale input deltas |
| `autoResize`, `naiveDimensions` | How size changes are detected |
| `overscroll`, `allowNestedScroll` | Behaviour of nested scrollable elements |
| `autoRaf` | Run the animation frame loop (default true). Set false to call `window.lenis.raf(time)` yourself |
| `autoToggle` | Start and stop Lenis from the wrapper's `overflow` |
| `stopInertiaOnNavigate` | Stop inertia when an internal link is clicked |
| `respectReducedMotion` | Honor `prefers-reduced-motion` (default true) |
| `anchors` | `true`, or scroll options: `offset`, `duration`, `easing`, `immediate`, `lock` |
| `prevent` | CSS selector; events from matching elements are not smoothed |
| `wrapper`, `content`, `eventsTarget` | CSS selectors for the scroll container, its content and the event target |
| `snap` | `true`, or lenis-snap options (below) |

Values are checked at build time: a `lerp` outside the range, an unknown `easing` or a misspelled key is reported by the config validator.

### Easing

Lenis takes a function; Krate takes a name. Presets: `easeOutExpo` (the Lenis default), `linear`, the `easeIn`, `easeOut` and `easeInOut` forms of `Quad`, `Cubic`, `Quart` and `Sine`, plus `easeInExpo` and `easeInOutExpo`. Any `cubic-bezier(x1, y1, x2, y2)` with x values between 0 and 1 also works.

## Preventing smoothing

Mark an element with Lenis's own attributes, or use the `prevent` selector:

```html
<div data-lenis-prevent>native scrolling in here</div>
<div data-lenis-prevent-wheel>wheel is native, touch is smoothed</div>
```

Also available: `data-lenis-prevent-touch`, `data-lenis-prevent-vertical` and `data-lenis-prevent-horizontal`.

## Anchors

With `anchors` on, in-page `#links` animate to their target. Use `offset` to clear a fixed header:

```typescript
smoothScroll: { anchors: { offset: -80, duration: 1.2 } }
```

## Snap

```typescript
smoothScroll: { snap: { type: "proximity", distanceThreshold: "40%", debounce: 300 } }
```

`type` is `proximity` (default), `mandatory` or `lock`. Elements opt in with `data-lenis-snap`, whose value is the alignment (`start`, `center`, `end`, or several separated by spaces):

```tsx
<section data-lenis-snap="start">...</section>
```

Snap points are registered at load and again after every client-side navigation.

## Client-side navigation

With the SPA router, navigation scrolls go through Lenis so its internal position never disagrees with the page:

- A new page jumps to the top immediately.
- Back and forward restore the saved position immediately.
- A same-page `#hash` animates to its target.

## Using the instance

```tsx
const lenis = (window as unknown as { lenis?: any }).lenis;

lenis?.scrollTo("#pricing", { offset: -80 });
lenis?.stop();
lenis?.start();
lenis?.on("scroll", ({ scroll, velocity }: any) => {});
```

Stop Lenis while a full-screen overlay is open, or give the overlay `data-lenis-prevent`, so the page behind does not scroll. See the Lenis documentation for the full instance API.
