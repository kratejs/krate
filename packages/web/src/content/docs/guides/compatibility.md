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
| Class components | Beta | n/a | n/a | Full common subset |
| Error boundaries | Beta | Planned | Partial | Source and island |
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

Context values stay reactive across components. Object and callback refs (with React 19 cleanup functions), ref as a prop, forwardRef, createRef, useRef, and useImperativeHandle with deps attach before layout effects and detach on unmount.

Evidence: `TestUseContextTranspile`, `TestCrossComponentDynamicContextTranspile`, `TestReactForwardRefCallback`, `TestReactCreateRefBinds`, `TestRefBindingsAndEffectOrder`, `browser:/react-events/`

### Transitions and deferred values

**Source:** Beta · **Islands:** Beta · **SSR:** n/a

useTransition lowers to a pending signal. useDeferredValue, startTransition, and React 19 async transitions are best-effort rather than React scheduling.

Evidence: `TestRewriteReactUseTransitionLowered`

### Class components

**Source:** Beta · **Islands:** n/a · **SSR:** n/a

Lowered onto a runtime instance with React 18 ordering: batched setState with callbacks, forceUpdate, componentDidMount/DidUpdate (props and state, children first)/WillUnmount on branch removal, shouldComponentUpdate, PureComponent, getDerivedStateFromProps, getSnapshotBeforeUpdate, defaultProps, contextType, accessors, private members, and instance refs. UNSAFE_componentWillMount, legacy context and other statics are rejected with KR3xxx diagnostics.

Evidence: `TestClassComponentLowering`, `TestClassComponentLoweringEmit`, `TestClassLoweringIssueNamesMember`, `browser:/react-class/`, `fixture:examples/src/pages/react-class.tsx`

### Error boundaries

**Source:** Beta · **Islands:** Planned · **SSR:** Partial

Classes with getDerivedStateFromError/componentDidCatch catch errors from descendants' render, bindings, lifecycles and effects (not event handlers or async code), unmount the failed subtree, and recover on reset. A throw while the page is built renders the boundary fallback (KR3009). Uncaught errors unmount only the failing component. Island boundaries are planned.

Evidence: `TestBoundaryBuildTimeFallback`, `TestBuildTimeThrowWithoutBoundaryFails`, `TestClassBoundaryChildrenBranch`, `browser:/react-class/`

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

Evidence: Planned; no stable test reference yet.

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
