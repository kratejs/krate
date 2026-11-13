---
title: Motion
description: Motion (the animation engine behind Framer Motion) with declarative data-motion attributes, configured from krate.config.
order: 19
---

# Motion

Krate integrates [Motion](https://motion.dev) (MIT, by Motion B.V.), the animation engine behind Framer Motion, as an opt-in feature. Set `motion` in `krate.config.ts`; nothing from Motion is built or shipped while the key is unset.

```typescript
import { defineConfig } from "@krate/core";

export default defineConfig({
  motion: true,
});
```

`true` uses the defaults. An object enables it and tunes it:

```typescript
export default defineConfig({
  motion: {
    reducedMotion: "user",
    transition: { type: "spring", stiffness: 180, damping: 22 },
    viewport: { amount: 0.3, once: true },
    stagger: { delay: 0.08 },
    easings: { snappy: [0.2, 0.9, 0.3, 1] },
    presets: {
      pop: {
        initial: { opacity: 0, scale: 0.8 },
        animate: { opacity: 1, scale: 1 },
        transition: { ease: "snappy", duration: 0.4 },
      },
    },
  },
});
```

Set `enabled: false` inside the object to turn it off without deleting the options.

## What ships

The build vendors the Motion browser bundle and writes one hashed chunk, `chunks/motion.<hash>.js`, holding the library and your options. Every page loads it with `defer`, including pages with no other client JavaScript. The whole library is available as `window.Motion`, so `animate`, `scroll`, `inView`, `hover`, `press`, `stagger`, `spring`, `motionValue`, `transform` and the rest work from any client script. `window.krateMotion` exposes the resolved config and a `refresh(root)` function.

Motion is not the React package: Krate has no virtual DOM, so there are no `<motion.div>` components. The declarative attributes below cover the common cases, and the DOM API covers the rest.

## Declarative attributes

When `declarative` is on (the default), elements with these attributes are animated after the page loads and again after every soft navigation.

```tsx
export default function Hero() {
  return (
    <section data-motion-stagger="0.1">
      <h1 data-motion="fade-up">Hello</h1>
      <p data-motion="fade-up">Animated on entering the viewport.</p>
      <button data-motion-hover='{"scale":1.05}' data-motion-press='{"scale":0.97}'>
        Start
      </button>
    </section>
  );
}
```

| Attribute | Meaning |
|-----------|---------|
| `data-motion="name"` | A preset: a built-in or one from `motion.presets` |
| `data-motion-initial` | JSON state applied instantly before the entrance, such as `{"opacity":0,"y":20}` |
| `data-motion-animate` | JSON state animated to; a keyframe list per property is allowed |
| `data-motion-hover` | JSON state animated to while hovered, restored on leave |
| `data-motion-press` | JSON state animated to while pressed, restored on release |
| `data-motion-transition` | JSON transition such as `{"type":"spring","bounce":0.3}` |
| `data-motion-delay`, `data-motion-duration`, `data-motion-ease` | Shorthands for the matching transition keys |
| `data-motion-trigger` | `view` (default) plays on entering the viewport, `load` plays immediately, `none` never plays on its own |
| `data-motion-amount`, `data-motion-margin`, `data-motion-once` | Per-element viewport options |
| `data-motion-stagger` | On a container: delay in seconds between the animated descendants |
| `data-motion-stagger-from`, `data-motion-stagger-ease` | Where the stagger starts and its easing |
| `data-motion-scroll-animate` | JSON state linked to scroll progress |
| `data-motion-scroll` | `page` links to page scroll; otherwise to the element's progress |
| `data-motion-scroll-offset` | JSON scroll offsets such as `["start end","end start"]` |

Attributes on the element win over the preset, and the preset wins over the config defaults.

### Built-in presets

`fade`, `fade-up`, `fade-down`, `fade-left`, `fade-right`, `zoom-in`, `zoom-out`, `blur-in`, `slide-up`, `slide-down`, `slide-left`, `slide-right`, `flip-x`, `flip-y` and `rotate-in`. A name in `motion.presets` replaces a built-in with the same name.

### Avoiding a flash

While the script loads, elements with `data-motion` or `data-motion-initial` are hidden by a small inline rule, then revealed once bound. If the script fails to load, the page shows them again after three seconds. Set `declarative: false` to skip the rule and the binding.

## Options

| Key | Meaning |
|-----|---------|
| `reducedMotion` | `"user"` (default) follows `prefers-reduced-motion`; `"always"` and `"never"` force it. Reduced motion drops transform animations and keeps opacity and other properties |
| `declarative` | Bind `data-motion-*` attributes (default `true`) |
| `transition` | Default transition: `type`, `duration`, `delay`, `ease`, `stiffness`, `damping`, `mass`, `bounce`, `visualDuration`, `repeat`, `repeatType`, `repeatDelay` |
| `viewport` | Entrance defaults: `amount` (`"some"`, `"all"` or 0 to 1), `margin`, `once` |
| `stagger` | Defaults for staggered containers: `delay`, `from`, `ease` |
| `easings` | Named cubic beziers as `[x1, y1, x2, y2]`, usable anywhere an `ease` is accepted |
| `presets` | Named presets with `initial`, `animate`, `hover`, `press` and `transition` |

An `ease` is one of `linear`, `easeIn`, `easeOut`, `easeInOut`, `circIn`, `circOut`, `circInOut`, `backIn`, `backOut`, `backInOut`, `anticipate`, a name from `easings`, or `cubic-bezier(x1,y1,x2,y2)`. Invalid values fail the build with the option name in the message, and unknown keys produce a warning.

## Using the library directly

```tsx
import { onMount } from "@krate/runtime";

export default function Box() {
  onMount(() => {
    window.Motion.animate("#box", { rotate: 360 }, { type: "spring", bounce: 0.4 });
  });
  return <div id="box" />;
}
```

Elements added by your own code can be bound by calling `window.krateMotion.refresh(container)`.
