---
title: Compatibility Matrix
order: 6
description: The public Krate compatibility matrix for React source components, Preact islands, SSR, and the browser conformance corpus.
---

# Compatibility Matrix

This is the support contract for React compatibility. A surface is only marked
stable when the referenced corpus and browser tests pass. Known divergences are
listed instead of silently hidden.

Last verified: **2026-10-21**. See the [roadmap](/roadmap/) for phase
status and the [React compatibility guide](/docs/guides/react-compatibility/)
for usage.

## Status key

- **Stable** - covered by the corpus and the cross-browser gate.
- **Beta** - implemented and tested; edge cases may still change before v1.
- **Partial** - a documented subset works; unsupported patterns must produce a diagnostic.
- **Planned** - not part of the supported surface yet.

## Surfaces

| Surface | Source | Islands | SSR | v1 target |
|---|---|---|---|---|
| Hooks: state and reducers | Beta | Beta | n/a | React 18 parity |
| Hooks: effects and memo | Beta | Beta | n/a | React 18 parity |
| Context and refs | Beta | Beta | n/a | Stable |
| Transitions and deferred values | Beta | Beta | n/a | Documented semantics |
| Class components | Partial | n/a | n/a | Full common subset |
| Error boundaries | Planned | Planned | Planned | Source and island |
| Children, keys, and refs | Partial | Partial | n/a | Documented semantics |
| Events | Partial | Partial | n/a | React 18 parity |
| Forms and inputs | Partial | Partial | n/a | React 18 parity |
| Portals | Beta | Beta | n/a | SSR-safe |
| Suspense | Beta | Beta | n/a | SSR-safe |
| React 19 actions and forms | Beta | Partial | n/a | Source semantics |
| Server actions | Beta | n/a | Beta | Progressive enhancement |
| Compiled React packages | n/a | Beta | n/a | Documented compat |
| shadcn-style dashboard | Beta | n/a | n/a | Stable |
| Next.js app corpus | Beta | n/a | n/a | Migration-ready |
| Local components in islands | n/a | Planned | n/a | Supported |
| SSR island hydration | n/a | Planned | Planned | Streaming-safe |
| Supported Preact versions | n/a | Beta | n/a | 10.20 through current |

## Details

### Hooks: state and reducers

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

useState and useReducer lower to signals. Reducer dispatch uses the same getter contract as a signal.

Evidence: `TestReactUseReducerBuild`, `TestRewriteReactUseReducer`, `TestRewriteReactNamespaceUseReducer`

### Hooks: effects and memo

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

Effects with empty deps use mount timing; dependency arrays use createEffectDeps/createMemoDeps. Passive-effect timing and cleanup ordering still differ from React.

Evidence: `TestRewriteReactEffectDepsAndMemo`, `TestRewriteReactEffectEmptyDepsMountOnce`

### Context and refs

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

Context values stay reactive across components. forwardRef, callback refs, createRef, and useRef are covered.

Evidence: `TestUseContextTranspile`, `TestCrossComponentDynamicContextTranspile`, `TestReactForwardRefCallback`, `TestReactCreateRefBinds`

### Transitions and deferred values

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

useTransition lowers to a pending signal. useDeferredValue, startTransition, and React 19 async transitions are best-effort rather than React scheduling.

Evidence: `TestRewriteReactUseTransitionLowered`

### Class components

**Source:** Partial · **Islands:** n/a · **SSR:** n/a

State, setState, mount/update/unmount, forceUpdate, helpers, and instance fields are lowered. shouldComponentUpdate, snapshots, derived state, and error lifecycle methods are rejected with diagnostics.

Evidence: `TestClassComponentLowering`, `TestSynthesizeClassComponentRejectsUnsupported`

### Error boundaries

**Source:** Planned · **Islands:** Planned · **SSR:** Planned

No React-compatible error boundary lifecycle yet. Build failures and island load errors use Krate fallbacks.

Evidence: Planned; no stable test reference yet.

### Children, keys, and refs

**Source:** Partial · **Islands:** Partial · **SSR:** n/a

Static Children helpers and cloneElement fold at build time. Keyed updates intentionally do not run a React reconciler.

Evidence: `TestReactChildrenMapSSR`, `TestReactCloneElementFold`, `TestReactKeyNotEmitted`

### Events

**Source:** Partial · **Islands:** Partial · **SSR:** n/a

Delegated bubbling, capture, non-bubbling, focus, and controlled value/checked paths are covered. Composition and full synthetic-event surface remain gaps.

Evidence: `TestEventDelegationBubbles`, `TestNonBubblingEventUsesDirectListener`, `TestCaptureEventUsesCaptureListener`

### Forms and inputs

**Source:** Partial · **Islands:** Partial · **SSR:** n/a

Controlled text, select, checkbox, radio, textarea, and submit handling run in the browser corpus. React 19 form actions (action/formAction functions, reset after success, no-JS server action posts) run in the browser gate. defaultValue/uncontrolled parity remains.

Evidence: `TestFormValueUsesPropertyBinding`, `browser:/react-forms/`, `TestReactActionFormsCompile`, `browser:/react-actions/`

### Portals

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

createPortal and Portal mount into document.body with cleanup. SSR output remains a placeholder until hydration.

Evidence: `TestPortalRendersAndMounts`

### Suspense

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

Fallback shells and runtime regions are covered. React lazy/loadable semantics and nested island Suspense boundaries remain incomplete.

Evidence: `TestBuildSuspenseBoundaryStaticFallback`, `TestBuildSuspenseBoundaryRuntimePrimary`

### React 19 actions and forms

**Source:** Beta · **Islands:** Partial · **SSR:** n/a

Source useActionState (queued, previous state over the action transport, permalink no-JS fallback), useFormStatus (form-scoped), useOptimistic (revert on settle), requestFormReset, and function form actions are lowered and run in the browser gate. Compiled packages get the same useActionState/useOptimistic runtime; their useFormStatus is idle. use() and async startTransition are tracked under Suspense and transitions.

Evidence: `TestLowerUseActionState`, `TestLowerUseFormStatusDestructured`, `TestLowerUseOptimistic`, `TestReactActionFormsCompile`, `browser:/react-actions/`

### Server actions

**Source:** Beta · **Islands:** n/a · **SSR:** Beta

File-level "use server" modules compile to hashed POST endpoints with client proxies, bound arguments, no-JS form posts, SSR forms, registry validation, origin checks, body limits, and no-store responses. Enhanced forms return results without reloading; errors use a {message, digest} envelope; actions read headers() and cookies() and can redirect(). Inline "use server" functions are a coded build error (KR7001).

Evidence: `TestPrepareActionsBuildsManifestAndBundle`, `TestRewriteActionImports`, `TestSameOriginActionRequest`, `TestActionSidecarTransport`, `TestActionHandlerSecurity`, `TestInlineUseServerIsCodedError`, `fixture:examples/src/pages/react-actions.tsx`, `browser:/react-actions/`

### Compiled React packages

**Source:** n/a · **Islands:** Beta · **SSR:** n/a

Radix and other compiled packages run as Preact islands with live props, callbacks, portals, and controlled primitives.

Evidence: `TestIslandSignalPropBridge`, `TestIslandChunksMinified`, `browser:/react-demo/`

### shadcn-style dashboard

**Source:** Beta · **Islands:** n/a · **SSR:** n/a

Copied shadcn source components, cva/cn folding, cards, controlled filters, and derived lists run in the browser corpus.

Evidence: `TestShadcnButtonFoldsVariantsAndSpread`, `browser:/react-dashboard/`

### Next.js app corpus

**Source:** Beta · **Islands:** n/a · **SSR:** n/a

The benchmark Next App Router fixture is the migration-shaped corpus. It is source-corpus coverage today, not a browser-run Krate app.

Evidence: `fixture:packages/benchmark/fixtures/next`

### Local components in islands

**Source:** n/a · **Islands:** Planned · **SSR:** n/a

A local Krate component cannot currently render inside a compiled island subtree; the build reports it instead of dropping it.

Evidence: Planned; no stable test reference yet.

### SSR island hydration

**Source:** n/a · **Islands:** Planned · **SSR:** Planned

Islands are client-only in v1. The placeholder renders on the server and the island mounts after its chunk loads.

Evidence: Planned; no stable test reference yet.

### Supported Preact versions

**Source:** n/a · **Islands:** Beta · **SSR:** n/a

The island build warns below 10.20. The conformance jobs exercise the minimum, an intermediate version, and the workspace version.

Evidence: `ci:preact-conformance`

## How claims are verified

The compiler and runtime suites pin lowering, diagnostics, and runtime
primitives. The browser corpus runs Chromium, Firefox, and WebKit against the
same built output, including the React demo, shadcn-style dashboard, forms,
islands, styles, and event interactions. The Preact jobs cover the minimum,
intermediate, and workspace versions.

Corpus:

- Radix suite and compiled-package islands: `examples/src/pages/react-demo.tsx`
- shadcn-style dashboard and derived lists: `examples/src/pages/react-dashboard.tsx`
- Forms and controlled inputs: `examples/src/pages/react-forms.tsx`
- Class components: `examples/src/pages/react-demo.tsx`
- Next.js App Router fixture: `packages/benchmark/fixtures/next`

A documented limitation is acceptable; a silent one is a bug. Report a
divergence with a minimal component and a browser.
