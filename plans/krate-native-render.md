# Krate Native Render: compiled UI without a webview

Status: proposal (2026-11-12). Not started. Builds on the Krate Native work in
PR #26 (branch `native`).

## Goal

Run a Krate app's TSX and CSS as a native, GPU-rendered UI with no system
webview: smaller, faster to start, lower memory, the same look on every OS, and
one renderer that can later reach iOS and Android. The webview stays the
default target; this is a second target that an app opts into
(`native.renderer: "krate"`), for apps that fit the supported CSS subset.

What makes this tractable where React Native had to invent its own primitives:
Krate's compiler sees all of the app's CSS and markup, so it can resolve the
cascade at build time, compile dynamic selectors into small matchers, emit
direct tree operations instead of DOM code, and warn about every property or
selector the target does not support.

Not a goal: running arbitrary web content (remote sites stay in webviews, with
the isolation from PR #26), or matching browsers for CSS outside the supported
subset.

## The stack

All memory-safe Rust from the window down to the GPU, plus the existing Go
shell and a C JavaScript engine.

| Layer | Library | Role | Status (Nov 2026) |
|---|---|---|---|
| Windows, input, IME (desktop) | winit | event loop, windows, keyboard, pointer, IME | 0.30 stable; 0.31 in beta. Desktop solid. iOS: basic soft keyboard and IME since 0.30.8. Android: IME events only in a draft PR (GameActivity only). |
| GPU | wgpu | Metal, Vulkan, D3D12, GLES | Mature on every target. |
| 2D rendering | Vello (`vello_gpu`, formerly vello_hybrid; `vello_cpu` fallback) | paths, gradients, images, clips, blurred rounded rects | `vello_cpu` described as ready for production with an unstable API; the GPU hybrid renderer "roughly beta quality". Pin versions; expect API churn. |
| Layout | Taffy | block, flexbox, grid, absolute positioning | Mature (used by Bevy, Blitz, Dioxus). No inline formatting context: inline content is laid out by Parley inside Taffy leaves (as Blitz does). |
| Text | Parley (+ fontique, swash/skrifa) | shaping, bidi, rich inline layout, line breaking, editing (PlainEditor) | Active, steady progress; used by Bevy and others. |
| Accessibility | AccessKit | one tree, platform adapters | Windows (UIA), macOS (NSAccessibility), Linux (AT-SPI) shipped. Android adapter shipped (`accesskit_android`). iOS adapter in development (NLnet grant since Nov 2025), not released. |
| JavaScript | quickjs-ng (cgo) | runs the page logic and the signal runtime | Replaces the transpiled modernc QuickJS for this target (faster; cgo is accepted for this target). |
| Host | the existing Go shell (`packages/native`) | SDK, permissions, host code, packaging, updater | Reused as is; becomes a library the render core calls into. |
| Mobile shells (phase 3) | Kotlin Activity + Swift view controller, our own | surface, text input (InputConnection / UITextInput), lifecycle, accessibility bridge | Ours to write; avoids depending on winit's Android IME and AccessKit's iOS adapter until they mature. |

Reference implementation to read before writing anything: Blitz (DioxusLabs),
which combines Stylo + Taffy + Parley + Vello + AccessKit for HTML/CSS. We take
its structure for inline layout and painting but drop Stylo and the HTML parser:
the compiler does the cascade.

## Architecture

```
 build time (krate compiler, Go)
   TSX + CSS
     -> IR (existing)
     -> render target codegen (new):
          node tree template + computed styles per node
          selector matchers for dynamic state (:hover, :focus, classes, media)
          JS that mutates nodes through a host API (no DOM)
          KR diagnostics for unsupported CSS/HTML
     -> app bundle (tree, styles, JS, assets) + the shell

 run time (one process)
   Rust render core (staticlib, C ABI)            Go shell (cgo)
     winit event loop (owns the main thread)  <-> SDK, permissions, host calls,
     node tree + style application                 updater, packaging, workers
     Taffy layout, Parley text
     Vello paint via wgpu
     hit testing, focus, scrolling, editing
     AccessKit tree
   quickjs-ng: page JS + signal runtime, mutating the node tree via host calls
```

- Event-loop ownership: the Rust core owns the main thread (winit and the OSes
  require it); Go code runs on its own threads and posts work to the loop, the
  same model the webview platform layer uses today.
- The node tree is retained. JS mutations (set text, set attribute/class,
  insert/remove/move node, set style) are batched per frame; changed subtrees
  are restyled with the compiled matchers, re-laid out by Taffy, and repainted.
- The runtime contract the compiler targets is a small host API (create node,
  set prop, set class, insert, remove, listen, focus, measure, scroll), not the
  DOM, so the existing hydration codegen gets a sibling backend rather than a
  DOM emulation.

## CSS support

Tiered, enforced by the compiler (a KR diagnostic names anything outside the
tier the app targets):

1. Tier 1 (v1): box model, display block/flex/grid/none, positioning (relative,
   absolute, fixed), overflow and scrolling, z-index and stacking contexts,
   colors, backgrounds (color, linear/radial gradients, images), borders and
   border-radius, box-shadow, opacity, transforms (2D), fonts and text
   (family, size, weight, style, line-height, alignment, decoration,
   letter/word spacing, white-space, text-overflow), cursor, pointer-events,
   visibility, CSS variables (resolved at build time where static, at runtime
   otherwise), media queries (width, height, prefers-color-scheme,
   prefers-reduced-motion), :hover, :active, :focus, :focus-visible,
   :disabled, :checked, class and attribute selectors, descendant/child
   combinators.
2. Tier 2: transitions and keyframe animations (opacity, transform, colors,
   sizes), filters (blur, drop-shadow), backdrop-filter, clip-path (basic
   shapes), 3D transforms, :nth-child and friends, sibling combinators,
   container queries.
3. Out of scope: anything that needs a full browser (floats beyond simple
   cases, multi-column, writing modes beyond horizontal-tb + bidi, print
   styles, iframes, SVG beyond what we render as images or paths).

Tailwind works as long as the utilities used fall in the supported tiers; the
compiler already sees which ones are used.

HTML elements: div/span-like containers, text, img, a (navigation inside the
app), button, input (text, password, number, checkbox, radio, range), textarea,
select (native popup per OS), label, form semantics, ul/ol/li, headings and
landmarks (for accessibility roles). Everything else is a diagnostic.

## Phases and effort

Estimates are engineer-weeks for one experienced engineer familiar with Rust,
Go and UI internals, for production quality on desktop. With parallel agent
workers (as in rounds 3 to 5 of Krate Native) the calendar time shrinks a lot,
but real-device testing, text input feel and accessibility still need human
review.

### Phase 0: spike (1 to 2 weeks)

Render `examples/native-tasks` and one CSS-heavy page through a throwaway
compiler output and a minimal Rust core on desktop (Linux first), with a text
field you can type into and an AccessKit tree a screen reader can read.
Measure binary size, startup, memory against the webview target. Decision gate:
continue only if fidelity is acceptable and startup/memory are clearly better.

### Phase 1: desktop v1 (about 25 to 38 weeks)

| Work item | Weeks |
|---|---|
| Render core: node tree, style application, Taffy integration, inline layout with Parley inside Taffy (Blitz as reference) | 3 to 5 |
| Painting with Vello: backgrounds, borders, radius, shadows, gradients, images, clipping, opacity, transforms, stacking order | 2 to 3 |
| Input: hit testing, pointer and keyboard events, focus order, focus rings, scrolling (wheel, trackpad, scrollbars), selection | 3 to 4 |
| Text editing and IME on Windows, macOS, Linux (inputs, textarea, selection, clipboard, undo) | 3 to 5 |
| Accessibility: AccessKit tree from nodes and roles, live updates, focus, actions | 2 to 3 |
| Compiler target: tree codegen, build-time cascade, dynamic selector matchers, CSS tiers and diagnostics | 4 to 6 |
| JS runtime: quickjs-ng via cgo, host API, the signal runtime ported to the host API, event dispatch | 3 to 4 |
| Integration: Go shell as a library, event loop ownership, SDK wiring (windows, menus, dialogs, tray work as today), packaging and signing on three OSes, CI | 2 to 3 |
| Fidelity harness: render each test page in both targets, compare screenshots, track diffs in CI | 2 |
| Docs, examples, migration notes for apps moving from the webview target | 1 to 2 |

Total: about 25 to 38 engineer-weeks (6 to 9 engineer-months).

### Phase 2: parity and polish (about 10 to 16 weeks)

Tier 2 CSS (transitions, animations, filters, backdrop blur), select popups and
date inputs per OS, drag and drop inside the page, devtools (an element
inspector over the dev control channel), performance work (incremental restyle
and layout, layer caching, damage-based repaint), memory budgets like the
webview target's.

### Phase 3: mobile (about 14 to 22 weeks; can start after phase 1)

| Work item | Weeks |
|---|---|
| Android shell: Activity with SurfaceView, lifecycle, our own InputConnection for the soft keyboard and IME, `accesskit_android`, Go as a library in the APK, Gradle packaging and signing | 6 to 10 |
| iOS shell: view controller with a Metal layer, UITextInput, VoiceOver (own UIAccessibility bridge, or AccessKit's iOS adapter if released by then), lifecycle, Xcode packaging, App Store signing | 8 to 12 |
| Touch: gestures, momentum scrolling, safe areas, keyboard insets, density | inside the two above |

Mobile also needs the Krate Native SDK areas reworked for mobile semantics
(no tray, no multiple windows, permissions through the OS prompts), which is
separate from rendering and not counted here.

### Totals

- Desktop, production-ready: phase 0 + 1 + 2, about 36 to 56 engineer-weeks
  (9 to 14 engineer-months).
- Plus mobile: add about 14 to 22 weeks (4 to 6 engineer-months).

## Risks

- Vello API churn and GPU driver bugs (mitigate: pin versions, keep `vello_cpu`
  as a fallback, fidelity harness in CI on all OSes).
- Text input feel and IME correctness, the classic weak spot of custom-drawn
  UIs (mitigate: native text input plumbing per OS, test with real IMEs for
  CJK, and with dictation).
- Accessibility parity with the webview (mitigate: AccessKit plus the a11y test
  suites from PR #26 run against both targets).
- Scope creep toward "a browser" (mitigate: the compiler's tiers are the
  contract; anything else is a diagnostic, not a runtime fallback).
- Build complexity: Rust + Go + C on five platforms (mitigate: per-OS CI
  runners; `cargo-ndk` and Xcode only in phase 3).
- Two-runtime binaries (Go + Rust) on mobile: size and startup to be measured
  in phase 0.

## Verification

- Screenshot comparison of every test page against the webview target, per OS.
- The existing native e2e tests, run against the new target where the feature
  exists.
- Accessibility tests (AT-SPI, AX, UIA as in PR #26) against the new target.
- Benchmarks (`native-bench.yml`) extended with the new target: startup, memory,
  size next to the webview, Electron and Tauri.

## Sources (checked 2026-11-12)

- AccessKit: https://github.com/AccessKit/accesskit, iOS adapter grant https://nlnet.nl/project/AccessKit-iOS/, Android crate https://docs.rs/crate/accesskit_android/0.7.2/source/README.md
- winit: 0.30 changelog https://docs.rs/crate/winit/latest/source/src/changelog/v0.30.md, Android IME draft PR rust-windowing/winit#4451, 0.31 betas on docs.rs (winit-uikit, winit-android)
- Vello: Linebender 2026 Q1 update https://linebender.org/blog/tmil-25/, vello_gpu/vello_hybrid docs https://docs.rs/vello_hybrid, vello_cpu docs https://docs.rs/vello_cpu
- Blitz (reference implementation): https://github.com/DioxusLabs/blitz
