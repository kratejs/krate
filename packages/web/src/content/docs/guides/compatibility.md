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
| Events | Beta | Partial | n/a | React 18 parity |
| Forms and inputs | Beta | Partial | n/a | React 18 parity |
| Portals | Beta | Beta | n/a | SSR-safe |
| Suspense | Beta | Beta | n/a | SSR-safe |
| React 19 actions and forms | Planned | Planned | n/a | Source semantics |
| Server actions | Beta | n/a | Beta | Progressive enhancement |
| Compiled React packages | n/a | Beta | n/a | Documented compat |
| shadcn-style dashboard | Beta | n/a | n/a | Stable |
| Next.js app corpus | Beta | n/a | n/a | Migration-ready |
| Local components in islands | n/a | Planned | n/a | Supported |
| SSR island hydration | n/a | Planned | Planned | Streaming-safe |
| Supported Preact versions | n/a | Beta | n/a | 10.20 through current |
| Browser differential corpus | Partial | n/a | n/a | No P0/P1 divergence |
| React 19 reference app | Planned | n/a | n/a | Same browser suite as React 18 |

## Details

### Hooks: state and reducers

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

useState and useReducer lower to signals with React's Object.is bail-out, functional updates, lazy initializers, a stable dispatch, and automatic batching (one commit per task).

Evidence: `TestReactUseReducerBuild`, `TestRewriteReactUseReducer`, `TestRewriteReactNamespaceUseReducer`, `TestUseReducerLazyInitIsPreserved`, `TestLazyUseStateInitializerRunsOnce`, `browser:/react-effects/`

### Hooks: effects and memo

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

Effects run in React's commit order (insertion, layout before paint, passive after paint; children before parents; cleanups before re-runs; parent-first unmount) with Object.is dependency comparison. useMemo/useCallback honour deps synchronously. Effects without deps re-run on the component's own state/props changes only, hidden conditional branches behave like Activity (state kept), and cleanups read live state; each divergence has a KR2xxx diagnostic.

Evidence: `TestRewriteReactEffectDepsAndMemo`, `TestRewriteReactEffectEmptyDepsMountOnce`, `TestEffectPhasesLowerToCreateEffectDeps`, `TestEffectWithoutDepsSynthesizesStateAndProps`, `TestHydrationDeclaresEffectOwners`, `browser:/react-effects/`

### Context and refs

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

Context values stay reactive across components. Object and callback refs (with React 19 cleanup functions), ref as a prop, forwardRef, createRef, useRef, and useImperativeHandle with deps attach before layout effects and detach on unmount.

Evidence: `TestUseContextTranspile`, `TestCrossComponentDynamicContextTranspile`, `TestReactForwardRefCallback`, `TestReactCreateRefBinds`, `TestRefBindingsAndEffectOrder`, `browser:/react-events/`

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

Static Children helpers and cloneElement fold at build time. Keyed updates intentionally do not run a React reconciler. Refs in lists attach per item and detach when an item is removed.

Evidence: `TestReactChildrenMapSSR`, `TestReactCloneElementFold`, `TestReactKeyNotEmitted`, `TestEventHandlersUseReactPropNames`

### Events

**Source:** Beta · **Islands:** Partial · **SSR:** n/a

React 17+ root delegation with capture then bubble order, native listener interleaving, stopPropagation, the synthetic event surface, React onChange semantics, focus/blur bubbling, per-element enter/leave, media and composition events. Unknown or lowercase event props are diagnosed (KR5001, KR5002).

Evidence: `TestEventDelegationBubbles`, `TestNonBubblingEventUsesDirectListener`, `TestCaptureEventUsesCaptureListener`, `TestEventHandlersUseReactPropNames`, `TestEventAndRefDiagnostics`, `browser:/react-events/`

### Forms and inputs

**Source:** Beta · **Islands:** Partial · **SSR:** n/a

Controlled text, select (single and multiple), checkbox, radio group, textarea, number and range inputs restore to state after onChange; uncontrolled defaultValue/defaultChecked only set defaults; form reset and value={undefined} follow React. Form diagnostics: KR5004 to KR5007. React 19 form actions are tracked separately.

Evidence: `TestFormValueUsesPropertyBinding`, `TestFormControlBindings`, `TestFormDiagnostics`, `browser:/react-forms/`, `browser:/react-events/`

### Portals

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

createPortal and Portal mount into document.body with cleanup. SSR output remains a placeholder until hydration.

Evidence: `TestPortalRendersAndMounts`

### Suspense

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

Fallback shells and runtime regions are covered. React lazy/loadable semantics and nested island Suspense boundaries remain incomplete.

Evidence: `TestBuildSuspenseBoundaryStaticFallback`, `TestBuildSuspenseBoundaryRuntimePrimary`

### React 19 actions and forms

**Source:** Planned · **Islands:** Planned · **SSR:** n/a

Compat shims exist for useActionState, useOptimistic, useFormStatus, use, and async transitions, but form actions and pending semantics are not in the browser gate yet.

Evidence: `TestReactBrowserDifferential`, `browser:/react-actions/`

### Server actions

**Source:** Beta · **Islands:** n/a · **SSR:** Beta

File-level "use server" modules compile to hashed POST endpoints with client proxies, no-JS form posts, SSR forms, registry validation, origin checks, and body limits. Inline directives and a request context remain unsupported.

Evidence: `TestPrepareActionsBuildsManifestAndBundle`, `TestRewriteActionImports`, `TestSameOriginActionRequest`, `fixture:examples/src/pages/react-actions.tsx`

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

### Browser differential corpus

**Source:** Partial · **Islands:** n/a · **SSR:** n/a

124 source-component fixtures (React 18 corpus plus the React 19 reference app) run in real React 19 and in Krate in Chromium, Firefox, and WebKit; DOM, form state, focus, and effect/lifecycle/ref ordering are compared after every step. Known divergences are tracked with a severity, and KRATE_REACT_EXIT=1 fails while P0/P1 entries remain.

Evidence: `TestReactBrowserDifferential`, `TestReactBrowserCorpus`, `ci:browser-conformance`

### React 19 reference app

**Source:** Planned · **Islands:** n/a · **SSR:** n/a

Fixtures for useActionState, useOptimistic, useFormStatus, form and button actions with auto-reset, use(promise) and use(context), ref as a prop, ref cleanup callbacks, async transitions, useDeferredValue initial values, document metadata, and server actions under the real server.

Evidence: `TestReactBrowserDifferential`, `fixture:packages/compiler/internal/build/testdata/reactbrowser/r19/app_reference.tsx`

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
