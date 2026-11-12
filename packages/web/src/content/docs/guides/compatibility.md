---
title: Compatibility Matrix
order: 6
description: The public Krate compatibility matrix for React source components, Preact islands, SSR, the browser conformance corpus, and Krate Native per operating system.
---

# Compatibility Matrix

This is the support contract for React compatibility. A surface is only marked
supported or stable when the referenced tests pass. Known divergences are
listed instead of silently hidden.

Last verified: **2026-11-12**. See the [roadmap](/roadmap/) for phase
status and the [React compatibility guide](/docs/guides/react-compatibility/)
for usage.

## Status key

- **Stable** - covered by the corpus and the cross-browser gate.
- **Supported** - implemented and backed by the tests listed under Evidence, which run in CI; edge cases may still change before v1.
- **Partial** - a documented subset works; unsupported patterns must produce a diagnostic.
- **Planned** - not part of the supported surface yet.
- **Untested** - Krate Native only: the code exists and builds for that OS, but no test has run there. The row's known limits say what has and has not been checked.
- **n/a** - not applicable; for Krate Native it can mean the API answers `ErrUnsupported` on that OS.

## Surfaces

| Surface | Source | Islands | SSR | v1 target |
|---|---|---|---|---|
| Hooks: state and reducers | Supported | Supported | n/a | React 18 parity |
| Hooks: effects and memo | Supported | Supported | n/a | React 18 parity |
| Context and refs | Supported | Supported | n/a | Stable |
| Transitions and deferred values | Supported | Supported | n/a | Documented semantics |
| Class components | Supported | n/a | n/a | Full common subset |
| Error boundaries | Supported | Supported | Partial | Source and island |
| Per-segment not-found and error pages | Supported | n/a | Supported | Next.js not-found.tsx and error.tsx |
| Children, keys, and refs | Partial | Partial | n/a | Documented semantics |
| Events | Supported | Supported | n/a | React 18 parity |
| Forms and inputs | Supported | Supported | n/a | React 18 parity |
| Portals | Supported | Supported | n/a | SSR-safe |
| Suspense | Supported | Supported | n/a | SSR-safe |
| React 19 actions and forms | Supported | Partial | n/a | Source semantics |
| React 19.2: useId, useEffectEvent, Activity, ViewTransition | Supported | n/a | n/a | Source semantics |
| Custom elements | Supported | n/a | n/a | React 19 semantics |
| Component tiers: client APIs, conflicts and higher-order components | Supported | n/a | n/a | Krate semantics |
| Server actions | Supported | n/a | Supported | Progressive enhancement |
| Data cache and revalidation | Supported | n/a | Supported | next/cache parity |
| Compiled React packages | n/a | Supported | Supported | Documented compat |
| shadcn-style dashboard | Supported | n/a | n/a | Stable |
| Next.js app corpus | Supported | n/a | n/a | Migration-ready |
| Local components in islands | n/a | Supported | n/a | Supported |
| npm React hook libraries (zustand, react-hook-form, SWR, TanStack Query, ...) | n/a | Supported | Supported | Run as Preact islands |
| CSS-in-JS (styled-components, emotion, MUI) | n/a | Partial | Partial | Client-only islands |
| Island hydration control (client:visible, client:idle) | n/a | Supported | n/a | Astro-style directives |
| SSR island hydration | n/a | Supported | Supported | Hydration-safe |
| Supported Preact versions | n/a | Supported | n/a | 10.20 through current |
| Browser differential corpus | Supported | n/a | n/a | No P0/P1 divergence |
| React 19 reference app | Supported | n/a | n/a | Same browser suite as React 18 |
| Serverless and Node deployment | n/a | n/a | Supported | Vercel, Netlify, Cloudflare and Node hosting |
| Metadata routes and ImageResponse | Supported | n/a | Supported | Next.js sitemap.ts, robots.ts, manifest.ts, opengraph-image and twitter-image |
| Code highlighting (build, request time, browser) | Supported | Supported | Supported | chroma parity |

## Krate Native

Krate Native is verified on Linux (GTK 3 and WebKitGTK 4.1 through purego, under
Xvfb, with a private D-Bus session and fake desktop services). The macOS and
Windows platform layers compile for their targets and have unit tests for the
parts that do not need a window, but no test has driven a real WKWebView or
WebView2 window yet, so those columns say Untested rather than Supported. The
CI `native` job runs the GUI tests on macOS and Windows without blocking; a
green run there is what moves a cell.

| Surface | Linux | macOS | Windows | v1 target |
|---|---|---|---|---|
| Native: window and custom scheme | Supported | Partial | Supported | Krate Native v1 |
| Native: streaming responses | Supported | Supported | Supported | Krate Native v1 |
| Native: host calls, streams and signals | Supported | Supported | Supported | Krate Native v1 |
| Native: Go host code | Supported | Supported | Supported | Krate Native v1 |
| Native: TypeScript host code on QuickJS | Supported | Supported | Supported | Krate Native v1 |
| Native: Node, Bun and Deno host children | Supported | Partial | Partial | Krate Native v1 |
| Native: SDK data areas (kv, db, fs, net, os) | Supported | Partial | Partial | Krate Native v1 |
| Native: SDK windows | Partial | Partial | Partial | Krate Native v1 |
| Native: requests are bound to the webview they came from | Supported | Supported | Supported | Krate Native v1 |
| Native: views and split panes | Supported | Supported | Supported | Krate Native v1 |
| Native: print, PDF, find, zoom and capture of a view | Supported | Untested | Untested | Krate Native v1 |
| Native: downloads started by the page | Supported | Untested | Untested | Krate Native v1 |
| Native: cookies and site data | Supported | Untested | Untested | Krate Native v1 |
| Native: remote views cannot reach the app | Supported | Untested | Untested | Krate Native v1 |
| Native: remote storage partitions | Supported | Untested | Untested | Krate Native v1 |
| Native: request rules for remote views | Supported | Untested | Untested | Krate Native v1 |
| Native: remote user agent, proxy, certificates and logins | Supported | Untested | Untested | Krate Native v1 |
| Native: remote message channel | Supported | Untested | Untested | Krate Native v1 |
| Native: shared web process, release and background mode | Supported | Untested | Untested | Krate Native v1 |
| Native: lean pages and the dev Memory panel | Supported | Supported | Supported | Krate Native v1 |
| Native: SDK dialogs, clipboard and menus | Supported | Untested | Untested | Krate Native v1 |
| Native: rich clipboard formats | Supported | Untested | Untested | Krate Native round 4 |
| Native: webview context menu | Supported | Untested | Untested | Krate Native round 4 |
| Native: debugging in krate native dev | Supported | Untested | Untested | Krate Native round 4 |
| Native: SDK notifications | Supported | Partial | Partial | Krate Native v1 |
| Native: camera, microphone, screen capture and location permissions | Supported | Untested | Untested | Krate Native v1 |
| Native: media permissions derived from the UI, usage descriptions and capabilities | Supported | Untested | Untested | Krate Native v1 |
| Native: operating system consent for camera, microphone, screen and location | Partial | Untested | Untested | Krate Native v1 |
| Native: screen and window capture sources | Partial | Untested | Untested | Krate Native v1 |
| Native: SDK tray and global shortcuts | Partial | Partial | Partial | Krate Native v1 |
| Native: SDK app lifecycle | Supported | Partial | Partial | Krate Native v1 |
| Native: file associations | Partial | Untested | Untested | Krate Native v1 |
| Native: opening files | Supported | Untested | Untested | Krate Native v1 |
| Native: dropped file paths | Supported | Untested | Untested | Krate Native v1 |
| Native: release as a single executable | Supported | Supported | Supported | Krate Native v1 |
| Native: prebuilt shell for apps without Go | Supported | Partial | Partial | Krate Native v1 |
| Native: krate native doctor | Supported | Supported | Supported | Krate Native v1 |
| Native: krate native test and the app driver | Supported | Untested | Untested | Krate Native v1 |
| Native: migration guides and the worked Electron port | Supported | Untested | Untested | Krate Native v1 |
| Native: plugins | Supported | Supported | Supported | Krate Native v1 |
| Native: packaging formats | Partial | Supported | Supported | Krate Native v1 |
| Native: signing and notarization | Partial | Partial | Partial | Krate Native v1 |
| Native: macOS universal apps | Supported | Untested | Supported | Krate Native round 4 |
| Native: Flatpak | Partial | Planned | Planned | Krate Native round 4 |
| Native: Snap | Partial | Planned | Planned | Krate Native round 4 |
| Native: Mac App Store | Planned | Untested | Planned | Krate Native round 4 |
| Native: Microsoft Store | Supported | Supported | Untested | Krate Native round 4 |
| Native: updater | Supported | Supported | Supported | Krate Native v1 |
| Native: app icon badge and progress | Supported | Untested | Untested | Krate Native round 3 |
| Native: displays | Supported | Untested | Untested | Krate Native round 3 |
| Native: power, idle and sleep blockers | Supported | Untested | Untested | Krate Native round 3 |
| Native: launch at login | Supported | Untested | Untested | Krate Native round 3 |
| Native: system theme | Supported | Untested | Untested | Krate Native round 3 |
| Native: crash reports | Supported | Untested | Untested | Krate Native round 3 |
| Native: size, start-up and memory budgets | Supported | Planned | Planned | Krate Native v1 |
| Native: custom title bars, drag regions and transparency | Supported | Untested | Untested | Krate Native v1 |
| Native: Mica, Acrylic, vibrancy, caption overlay and traffic lights | n/a | Untested | Untested | Krate Native v1 |
| Native: tabs, sheets, document state, share menu, dock, thumbnail buttons and jump lists | n/a | Untested | Untested | Krate Native v1 |
| Native: worker processes | Supported | Untested | Untested | Krate Native v1 |
| Native: message ports between views, windows and workers | Supported | Untested | Untested | Krate Native v1 |
| Native: offscreen rendering | Supported | Untested | Untested | Krate Native v1 |
| Native: remote debugging and automation | Supported | Untested | Untested | Krate Native v1 |
| Native: browser extensions | n/a | Untested | Untested | Krate Native v1 |
| Native: accessibility of the shell's own UI | Supported | Untested | Untested | Krate Native v1 |

## Details

### Hooks: state and reducers

**Source:** Supported - **Islands:** Supported - **SSR:** n/a

useState and useReducer lower to signals with React render snapshots (handlers and effects read the committed state, functional updaters the latest), Object.is bail-out, lazy initializers (inline or named), useReducer init, a stable dispatch, and React's update lanes (discrete events commit in a microtask, other updates in a later task).

Evidence: `TestReactUseReducerBuild`, `TestRewriteReactUseReducer`, `TestRewriteReactNamespaceUseReducer`, `TestUseReducerLazyInitIsPreserved`, `TestLazyUseStateInitializerRunsOnce`, `TestStateLoweringUsesReactSemantics`, `browser:/react-effects/`

### Hooks: effects and memo

**Source:** Supported - **Islands:** Supported - **SSR:** n/a

Effects run in React's commit order (insertion, layout before paint, passive after paint or synchronously after discrete events; children before parents; cleanups before re-runs; parent-first unmount) with Object.is deps, and setups/cleanups read the state and props of their own commit. useMemo/useCallback honour deps synchronously. A component in one conditional branch unmounts and remounts with fresh state; one stateful component type in both branches of a ternary (or chain, or early returns) keeps its instance and switches its props. Effects without deps re-run on the component's own state/props changes only; each divergence has a KR2xxx diagnostic.

Known limits:

- A same-type pair that cannot be lowered to one element (render-prop children, a differing ref or spread, a wrapper element built by each branch) restarts the component on the flip and warns with KR8360.
- A stateless component or an external island in both branches is not merged: uncontrolled DOM state inside it resets on the flip.

Diagnostics: `KR8360`

Evidence: `TestRewriteReactEffectDepsAndMemo`, `TestRewriteReactEffectEmptyDepsMountOnce`, `TestEffectPhasesLowerToCreateEffectDeps`, `TestEffectWithoutDepsSynthesizesStateAndProps`, `TestHydrationDeclaresEffectOwners`, `TestFreshDepsReRunEveryRender`, `TestHookInLoopOrConditionWarns`, `TestSameTypeBranchesBuildOneInstance`, `TestBuildSameTypeBranchesOneInstance`, `browser:/react-effects/`, `browser:/react-branches/`

### Context and refs

**Source:** Supported - **Islands:** Supported - **SSR:** n/a

Context resolves at the reader's position like React: Provider, React 19 <Ctx value>, Consumer render props, object defaults, nested and self-nesting providers, and providers around props.children; constant values fold at build time and state-driven values stay reactive. Object and callback refs (with React 19 cleanup functions), ref as a prop, forwardRef, createRef, useRef, and useImperativeHandle with deps attach before layout effects and detach on unmount.

Evidence: `TestUseContextTranspile`, `TestCrossComponentDynamicContextTranspile`, `TestContextSelfNestingProviders`, `TestContextReact19ProviderAndConsumer`, `TestContextChildrenSeeComponentProvider`, `TestReactForwardRefCallback`, `TestReactCreateRefBinds`, `TestRefBindingsAndEffectOrder`, `browser:/react-events/`

### Transitions and deferred values

**Source:** Supported - **Islands:** Supported - **SSR:** n/a

useTransition and startTransition support async actions: isPending stays true until the action, its awaited work and overlapping transitions settle, and a transition keeps revealed Suspense content. useDeferredValue (with React 19 initialValue) catches up in a later task; useSyncExternalStore follows the store by Object.is and unsubscribes on teardown. Updates after an await are ordinary updates, as in React 19.

Evidence: `TestRewriteReactUseTransitionLowered`, `TestTransitionStarterLowering`, `TestDeferredValueAndExternalStoreLowering`, `TestRuntimeNeedsSuspense`, `browser:/react-suspense/`, `fixture:examples/src/pages/react-suspense.tsx`

### Class components

**Source:** Supported - **Islands:** n/a - **SSR:** n/a

Lowered onto a runtime instance with React 18 ordering: batched setState with callbacks, forceUpdate, componentDidMount/DidUpdate (props and state, children first)/WillUnmount on branch removal, shouldComponentUpdate, PureComponent, getDerivedStateFromProps, getSnapshotBeforeUpdate, defaultProps, contextType, accessors, private members, and instance refs. UNSAFE_componentWillMount, legacy context and other statics are rejected with KR3xxx diagnostics.

Evidence: `TestClassComponentLowering`, `TestClassComponentLoweringEmit`, `TestClassLoweringIssueNamesMember`, `browser:/react-class/`, `fixture:examples/src/pages/react-class.tsx`

### Error boundaries

**Source:** Supported - **Islands:** Supported - **SSR:** Partial

Classes with getDerivedStateFromError/componentDidCatch catch errors from descendants' render, bindings, lifecycles and effects (not event handlers or async code), unmount the failed subtree, and recover on reset. A throw while the page is built renders the boundary fallback (KR3009). Uncaught errors unmount only the failing component. Each Preact island has its own error boundary and marks a fallback placeholder when rendering fails.

Known limits:

- Build-time boundary fallback for a browser-only island subtree is client-only; the island error boundary handles the client render failure.
- Island error boundaries catch Preact render errors, not event-handler or asynchronous errors.

Diagnostics: `KR3008`, `KR3009`

Evidence: `TestBoundaryBuildTimeFallback`, `TestBuildTimeThrowWithoutBoundaryFails`, `TestClassBoundaryChildrenBranch`, `browser:/react-class/`, `browser:/react-demo/`

### Per-segment not-found and error pages

**Source:** Supported - **Islands:** n/a - **SSR:** Supported

404.tsx or not-found.tsx and error.tsx or 500.tsx in any pages directory are that segment's pages. The nearest not-found page answers an unmatched URL, a closed dynamic route's unknown param and notFound() (status 404, inside the segment's layouts); the nearest error page answers an uncaught server error (status 500, sanitized message plus a digest) and, as a client error boundary around the segment's children, render, effect and reactive errors, with reset() and a hand-over to the next boundary up when an error page itself fails. The router swaps either page in place and keeps shared layouts. Pages are noindex and stay out of the sitemap and the route list.

Known limits:

- A page that throws while it is prerendered still fails the build (KR3010); the segment error page is not baked in as a build-time fallback.
- Segment pages are always prerendered; request-time rendering modes on them are ignored with KR9003.
- In the server-rendered error page, error.message and error.digest are filled into the HTML before hydration; text derived from them at render time appears once the page hydrates.

Diagnostics: `KR9001`, `KR9002`, `KR9003`

Evidence: `TestLookupSegmentPageURLWalk`, `TestLookupSegmentPageRouteWalk`, `TestValidateSegmentPages`, `TestBuildWritesSegmentPagesAndManifest`, `TestSegmentPagesServe`, `TestSegmentPagesServeDev`, `TestWatchSegmentPages`, `TestNestedNotFoundBecomesSegmentPage`, `TestErrorBoundaryBecomesSegmentErrorPage`, `TestSegmentPagesBrowser`, `TestServerlessNodeParity`, `TestPlatformDeploysKeepSegmentPages`, `TestMigratedNextAppBrowser`, `browser:/r18/boundary_reset_state/`, `browser:/r18/boundary_fallback_throws/`

### Children, keys, and refs

**Source:** Partial - **Islands:** Partial - **SSR:** n/a

Static Children helpers and cloneElement fold at build time. Lists reconcile by key with React semantics: rows keep DOM, input and focus state, component state, effects and refs across reorder, insert and removal (minimal moves), unkeyed lists match by index, items update in place, duplicate keys warn, and a changed key on a single element remounts it. Hydration adopts server-rendered rows by key. Missing, duplicate and partial keys are diagnosed (KR6001-KR6003). Refs in lists attach per item and detach when an item is removed.

Known limits:

- Some dynamic Children helpers and cloneElement calls cannot be folded at build time and use the compat runtime.
- List key inference is conservative for computed keys; rows without a stable key may be adopted by position.

Diagnostics: `KR6001`, `KR6002`, `KR6003`

Evidence: `TestReactChildrenMapSSR`, `TestReactCloneElementFold`, `TestReactKeyNotEmitted`, `TestEventHandlersUseReactPropNames`, `TestKeyedListKeyExtraction`, `TestKeyedRemount`, `TestListKeyDiagnostics`, `TestPlanMapCall`, `browser:/react-keys/`

### Events

**Source:** Supported - **Islands:** Supported - **SSR:** n/a

React 17+ root delegation with capture then bubble order, native listener interleaving, stopPropagation, the synthetic event surface, React onChange semantics, focus/blur bubbling, per-element enter/leave, media and composition events. Unknown or lowercase event props are diagnosed (KR5001, KR5002).

Known limits:

- Island event handlers run on Preact's event system, so Krate's source-level event diagnostics do not apply inside a compiled package.
- Controlled pointer/focus state in a compiled package is the package's responsibility; Krate only bridges page signals and callbacks.

Diagnostics: `KR5001`, `KR5002`

Evidence: `TestEventDelegationBubbles`, `TestNonBubblingEventUsesDirectListener`, `TestCaptureEventUsesCaptureListener`, `TestEventHandlersUseReactPropNames`, `TestEventAndRefDiagnostics`, `browser:/react-events/`, `browser:/react-demo/`

### Forms and inputs

**Source:** Supported - **Islands:** Supported - **SSR:** n/a

Controlled text, select (single and multiple), checkbox, radio group, textarea, number and range inputs restore to state after onChange; uncontrolled defaultValue/defaultChecked only set defaults; form reset and value={undefined} follow React. Form diagnostics: KR5004 to KR5007. React 19 form actions (action/formAction functions, reset after success) run in the browser gate. No-JS server action posts reach the server and redirect back; the returned state is carried in a short-lived flash cookie that useActionState reads on the next page, but a pre-rendered page is not re-rendered with it without script.

Known limits:

- Controlled controls inside a compiled package use Preact's controlled-input behavior; Krate's form diagnostics apply to source components only.
- Server action forms remain source-level; a compiled package receives the action runtime but not Krate's form lowering.

Diagnostics: `KR5004`, `KR5005`, `KR5006`, `KR5007`

Evidence: `TestFormValueUsesPropertyBinding`, `TestFormControlBindings`, `TestFormDiagnostics`, `browser:/react-forms/`, `browser:/react-events/`, `TestReactActionFormsCompile`, `browser:/react-actions/`, `browser:/react-demo/`

### Portals

**Source:** Supported - **Islands:** Supported - **SSR:** n/a

createPortal and Portal mount into document.body or a resolved container after hydration, follow conditional branches, unmount with their component, and bubble React events to the portal's React ancestors. Like React, portal content is not part of the server-rendered HTML.

Evidence: `TestPortalRendersAndMounts`, `TestCreatePortalLowersToAnchor`, `TestCreatePortalDynamicContainer`

### Suspense

**Source:** Supported - **Islands:** Supported - **SSR:** n/a

Boundaries show their fallback while use(promise), a resource or a lazy component inside is pending, reveal siblings together with React's 300ms reveal throttle, and nest independently (innermost wins). Content that cannot resolve at build time ships the fallback and hydrates into the content. A non-transition update that suspends hides the content in place and shows the fallback; a transition keeps the revealed content. lazy() (static import or a loader resolving to a local component) shows the fallback until its separately loaded hydration chunk or loader is ready. Matches React in the browser differential suspense fixtures.

Evidence: `TestBuildSuspenseBoundaryStaticFallback`, `TestBuildSuspenseBoundaryRuntimePrimary`, `TestSuspenseUseRendersFallbackAndTemplatesContent`, `TestNestedSuspenseInnermostWins`, `TestLazyComponentGetsHydrationChunk`, `TestLazyLowersToImportAndMarker`, `TestUsePromiseLowersToGetter`, `browser:/react-suspense/`, `fixture:examples/src/pages/react-suspense.tsx`

### React 19 actions and forms

**Source:** Supported - **Islands:** Partial - **SSR:** n/a

Source useActionState (queued, previous state over the action transport, permalink no-JS fallback), useFormStatus (form-scoped), useOptimistic (revert on settle), requestFormReset, and function form actions are lowered and run in the browser gate. Compiled packages get the same useActionState/useOptimistic runtime; their useFormStatus is idle. use() and async startTransition are tracked under Suspense and transitions.

Known limits:

- Compiled packages get useActionState and useOptimistic through the compat runtime; useFormStatus is idle inside an island.
- Use() and async transitions in compiled packages follow Preact compat behavior, not the source-component transition lowering.
- React 19 action hooks run in source components; a compiled island gets the compat runtime subset (useFormStatus is idle).

Diagnostics: `KR7001`

Evidence: `TestLowerUseActionState`, `TestReactBrowserDifferential`, `TestLowerUseFormStatusDestructured`, `TestLowerUseOptimistic`, `TestReactActionFormsCompile`, `browser:/react-actions/`

### React 19.2: useId, useEffectEvent, Activity, ViewTransition

**Source:** Supported - **Islands:** n/a - **SSR:** n/a

useId gives every component instance (children, nested children, lists, repeated components, conditional branches, layouts, rows added at runtime) an id that is the same in the static HTML and the hydration code, so htmlFor and id pairs match. useEffectEvent lowers to a stable function that reads the latest props and state and does not subscribe the effect that calls it. <Activity mode> renders its children in a wrapper that hides them while keeping state, cleans their effects up while hidden and runs them again when visible. <ViewTransition> names its element child for the router's view transition support (name and default class). The experimental names (unstable_Activity, unstable_ViewTransition, unstable_useEffectEvent, SuspenseList) are accepted; props with no effect warn (KR8031).

Diagnostics: `KR8031`

Evidence: `TestUseIdInChildComponents`, `TestUseIdInHydratedChildMatchesHTML`, `TestUseIdInDynamicListRowsIsNumberedPerRow`, `TestUseEffectEventIsLowered`, `TestActivityRendersChildrenAndHidesThem`, `TestActivityUnsupportedPropWarns`, `TestViewTransitionNamesItsChild`, `TestExperimentalReactNamesRenderTheirChildren`, `TestRewriteReact19Aliases`, `TestReact19Browser`

### Custom elements

**Source:** Supported - **Islands:** n/a - **SSR:** n/a

Strings and numbers render as attributes, true as an empty attribute and false is omitted. Objects, arrays and functions render no attribute on the server and are set as properties on the client, also when the element class is defined after the page loads. Components that render such props hydrate so the property is set.

Evidence: `TestCustomElementPropsFollowReact19`, `TestCustomElementAttrs`, `TestReact19Browser`

### Component tiers: client APIs, conflicts and higher-order components

**Source:** Supported - **Islands:** n/a - **SSR:** n/a

A server, static or runtime component that uses state, effects or event handlers fails the build (KR7110) with the fix, instead of shipping a dead page. Two tier sources that disagree for one component fail (KR7115). A tier decorator on an HOC-wrapped default export classifies the component and the HOC is applied, also when it is declared in another module; an HOC that cannot be inlined fails (KR8030).

Diagnostics: `KR7110`, `KR7115`, `KR8030`

Evidence: `TestBuildTimeTierRejectsClientAPIs`, `TestConflictingTiersFail`, `TestTierConflictRule`, `TestTierDecoratorOnHOCDefaultExport`, `TestHOCFromAnotherModuleIsApplied`, `TestUnresolvableHOCFailsTheBuild`

### Server actions

**Source:** Supported - **Islands:** n/a - **SSR:** Supported

File-level "use server" modules compile to hashed POST endpoints with client proxies, bound arguments, no-JS form posts, SSR forms, registry validation, origin checks, body limits, and no-store responses. Enhanced forms return results without reloading; errors use a {message, digest} envelope; actions read headers() and cookies() and can redirect(). Inline "use server" functions are a coded build error (KR7001).

Evidence: `TestPrepareActionsBuildsManifestAndBundle`, `TestRewriteActionImports`, `TestSameOriginActionRequest`, `TestActionSidecarTransport`, `TestActionHandlerSecurity`, `TestInlineUseServerIsCodedError`, `fixture:examples/src/pages/react-actions.tsx`, `browser:/react-actions/`

### Data cache and revalidation

**Source:** Supported - **Islands:** n/a - **SSR:** Supported

@krate/runtime/cache provides revalidatePath, revalidateTag, unstable_cache and unstable_noStore; server fetch honors next.tags/next.revalidate/cache options; pages accept export const revalidate and export const dynamic. Revalidation is applied across the SSR renderer workers, Go region cache, QuickJS and Node API data caches before an action or API route responds; static (SSG) targets are reported as skipped (KR8603).

Known limits:

- Caches are in memory per server process; multiple krate serve instances do not share them.
- Static (SSG) pages cannot be revalidated at runtime; revalidatePath reports them as skipped.
- Tags are recorded for data read during the page render; region renders and Go components do not record tags.

Diagnostics: `KR8601`, `KR8602`, `KR8603`

Evidence: `TestSegmentExportsRenderMode`, `TestDataCacheUsageInference`, `TestRevalidateHostAuth`, `TestRevalidateHostResolvesPathsAndFansOut`, `TestSSRInternalProxyLockdown`, `TestRevalidateTagAndPathE2E`, `TestRevalidateFansOutToWorkersE2E`, `TestActionRevalidationReadYourWritesE2E`, `TestAPIRouteRevalidationAndDataCacheE2E`, `TestQuickJSRouteUsesHostCache`

### Compiled React packages

**Source:** n/a - **Islands:** Supported - **SSR:** Supported

Radix and other compiled packages run as Preact islands with live props, callbacks, portals, controlled primitives, per-island entries bundled with shared modules, server-rendered initial markup, hydration, and an island error boundary. Known Preact limits: an uncontrolled Radix Tooltip closes right after it opens, and a layer opened inside another layer's portal content dismisses the outer layer; both have documented workarounds.

Evidence: `TestIslandSignalPropBridge`, `TestIslandLocalComponent`, `TestIslandSSRWithHooks`, `TestIslandSSREscapesTextProps`, `TestIslandSSRUnknownPropFallsBackClientOnly`, `TestIslandSSRModuleFailureFallsBackClientOnly`, `TestIslandSharedSignalAcrossIslands`, `TestIslandSSRArrayObjectProps`, `TestIslandLocalNamedImport`, `TestIslandPerIslandChunks`, `TestIslandChunksMinified`, `browser:/react-demo/`

### shadcn-style dashboard

**Source:** Supported - **Islands:** n/a - **SSR:** n/a

Copied shadcn source components, cva/cn folding, cards, controlled filters, and derived lists run in the browser corpus.

Evidence: `TestShadcnButtonFoldsVariantsAndSpread`, `browser:/react-dashboard/`

### Next.js app corpus

**Source:** Supported - **Islands:** n/a - **SSR:** n/a

The benchmark Next App Router fixture is the migration-shaped corpus. It is source-corpus coverage today, not a browser-run Krate app.

Evidence: `fixture:packages/benchmark/fixtures/next`

### Local components in islands

**Source:** n/a - **Islands:** Supported - **SSR:** n/a

Imported local React-compatible modules can be used as tags inside a compiled island and are bundled with it. Same-file Krate functions are diagnosed because they cannot cross the Krate/Preact runtime boundary.

Diagnostics: `KR8007`

Evidence: `TestIslandLocalComponent`, `browser:/react-demo/`, `TestIslandSameFileLocalWarning`

### npm React hook libraries (zustand, react-hook-form, SWR, TanStack Query, ...)

**Source:** n/a - **Islands:** Supported - **SSR:** Supported

A user module that calls a hook of an npm package built on React (or a store hook made by its factory, or a custom hook wrapping one) is bundled for Preact (react resolves to preact/compat, CommonJS require of react is bridged), rendered on the server and hydrated in place. The islands of all pages are bundled together with code splitting, so a store module is one store for the whole site and its state survives client-side navigation, forward and back; a leaving island unmounts and releases its subscriptions. A provider a layout wraps around its children (a query client, a Redux store, a theme) is lifted into the islands, one instance for every island of the page, with nested layouts composing; a provider that cannot be lifted is reported (KR8015, KR8016). Pages rendered per request (SSR, ISR) answer with the islands' build-time markup and hydrate. Checked by hand in Chromium against zustand 5.0.16, TanStack Query 5.105.3, react-redux 9.3.1 with Redux Toolkit 2.14.0 and styled-components 6.6.0 (ThemeProvider), and earlier against valtio 2, jotai 2, react-hook-form 7, SWR 2, TanStack Table 8, react-i18next 15 and usehooks-ts 3; the repository tests use stand-in packages with the same import shapes and behavior.

Known limits:

- A layout, template or server-rendered page cannot host these hooks; the build fails with KR8013.
- A layout provider whose props read the layout's params, a signal or a state setter, or that renders markup of its own, cannot reach the islands (KR8015, KR8016).
- State inside a component resets when its island unmounts on navigation; module-level state is kept.
- A page that is itself such an island cannot take route props (KR8013) or use Krate-only APIs in the same module.
- An island in a page rendered per request is server-rendered from build-time props; a prop known only per request makes it client-only.
- Only Chromium was run for the browser check.

Diagnostics: `KR8013`, `KR8015`, `KR8016`

Evidence: `TestReactHookLibraryPageIsland`, `TestReactHookLibraryComponentIslands`, `TestReactHookLibraryPageForms`, `TestReactLibraryValueInPageMakesIsland`, `TestReactIslandModulesAreNotKrateSource`, `TestReactHookLibraryInLayoutDiagnostic`, `TestPreactCompatChunkHasDefaultExport`, `TestBrowserReactLibraryIslands`, `TestLayoutProvidersReachIslands`, `TestIslandModulesAreSharedAcrossPages`, `TestLayoutProviderDiagnostics`, `TestIslandChunksSurviveDiskCache`, `TestPerRequestPageIslandStubs`

### CSS-in-JS (styled-components, emotion, MUI)

**Source:** n/a - **Islands:** Partial - **SSR:** Partial

Components built with styled-components, @emotion/styled, the emotion css prop (with the jsxImportSource pragma) or MUI run as Preact islands and are styled in the browser. They are client-only because the styles are created while rendering, so the server HTML holds an empty placeholder (KR8008 notice). A css prop without emotion's runtime is reported (KR8014). A ThemeProvider in a layout reaches them. Checked by hand in Chromium against styled-components 6, @emotion/styled and @emotion/react 11 and MUI 9.

Known limits:

- No server-rendered markup or styles: content appears when the island mounts.
- A project-wide jsxImportSource for emotion in tsconfig is not supported; use the per-file pragma.

Diagnostics: `KR8008`, `KR8014`

Evidence: `TestReactCSSInJSIsland`, `TestBrowserReactLibraryIslands`

### Island hydration control (client:visible, client:idle)

**Source:** n/a - **Islands:** Supported - **SSR:** n/a

client:visible and client:idle on an island's root tag defer mounting until the island is near the viewport or the browser is idle. The server markup stays; the chunk is neither preloaded nor fetched before then, and the directive never reaches the component. client:load is the default.

Diagnostics: `KR8012`

Evidence: `TestIslandClientDirectives`, `TestClientDirectiveOnNonIslandWarns`, `TestBrowserReactLibraryIslands`

### SSR island hydration

**Source:** n/a - **Islands:** Supported - **SSR:** Supported

Islands with build-time-known props are server-rendered with Preact and hydrated in place. The island entries of all pages are bundled together, so modules pages share are shared files. Unknown props, browser-only components, and portals fall back to client-only with a warning. A page rendered per request (SSR, ISR, streaming) answers with the placeholders and build-time markup of its islands instead of running them in the Node renderer; request-time props for an island are not supported.

Diagnostics: `KR8008`, `KR8009`

Evidence: `TestIslandSignalPropBridge`, `TestIslandSSRWithHooks`, `TestIslandSSREscapesTextProps`, `TestIslandSSRUnknownPropFallsBackClientOnly`, `TestIslandSSRModuleFailureFallsBackClientOnly`, `TestIslandSharedSignalAcrossIslands`, `TestIslandSSRArrayObjectProps`, `TestIslandLocalNamedImport`, `TestIslandPerIslandChunks`, `TestIslandChunksMinified`, `browser:/react-demo/`, `TestIslandBundleFailureUsesKR8009`, `TestPerRequestPageIslandStubs`, `TestIslandChunksSurviveDiskCache`, `TestBrowserReactLibraryIslands`

### Supported Preact versions

**Source:** n/a - **Islands:** Supported - **SSR:** n/a

The island build warns below 10.20. The conformance jobs exercise the minimum, an intermediate version, and the workspace version.

Evidence: `ci:preact-conformance`

### Browser differential corpus

**Source:** Supported - **Islands:** n/a - **SSR:** n/a

124 source-component fixtures (React 18 corpus plus the React 19 reference app) run in real React 19 and in Krate in Chromium, Firefox, and WebKit; DOM, form state, focus, and effect/lifecycle/ref ordering are compared after every step. Known divergences are tracked with a severity, and KRATE_REACT_EXIT=1 fails while P0/P1 entries remain.

Known limits:

- Four P2 render-count/useId formatting differences remain documented in testdata/reactbrowser/divergences.json.

Evidence: `TestReactBrowserDifferential`, `TestReactBrowserCorpus`, `ci:browser-conformance`

### React 19 reference app

**Source:** Supported - **Islands:** n/a - **SSR:** n/a

Fixtures for useActionState, useOptimistic, useFormStatus, form and button actions with auto-reset, use(promise) and use(context), ref as a prop, ref cleanup callbacks, async transitions, useDeferredValue initial values, document metadata, and server actions under the real server.

Evidence: `TestReactBrowserDifferential`, `fixture:packages/compiler/internal/build/testdata/reactbrowser/r19/app_reference.tsx`

### Serverless and Node deployment

**Source:** n/a - **Islands:** n/a - **SSR:** Supported

krate deploy bundles the portable server handler (SSR, ISR, streaming, async server components, JS/TS API routes, server actions, middleware, redirects, rewrites, headers) into one self-contained module for --target node, vercel, netlify and cloudflare. Per-segment not-found and error pages and middleware on prerendered pages work on every target. A request matrix gives the same status, security and cache headers, redirects and bodies as krate start; Go API routes, Go components and sidecars are refused at deploy time.

Known limits:

- ISR, region and data caches are per instance on serverless targets; revalidatePath/revalidateTag only reach the instance that handled the request (reported under skipped). Pass an isrStore to createKrateHandler to share the page cache.
- With middleware, Vercel and Netlify send every page request to the function, which serves prerendered HTML from the bundle (bundle size grows with static pages); assets keep going to the CDN. A public file without an extension is treated as a page there.
- Legacy (req, res) API handlers, Go API routes, Go components, the API sidecar and community plugin serve hooks are not supported on serverless targets.
- Cloudflare bundles fail at build time for Node built-ins outside nodejs_compat (fs, vm, net, child_process).

Evidence: `TestServerlessNodeParity`, `TestVercelServerOutput`, `TestNetlifyServerOutput`, `TestCloudflareServerOutput`, `TestServerBundleHasNoSecrets`, `TestServerlessRefusesGoAndSidecars`, `TestCloudflareRefusesNodeBuiltins`, `TestServerlessNodeBrowser`, `TestPlatformDeploysKeepSegmentPages`, `TestNetlifyAndVercelWithoutMiddlewareStayStaticFirst`

### Metadata routes and ImageResponse

**Source:** Supported - **Islands:** n/a - **SSR:** Supported

sitemap.ts, robots.ts and manifest.ts build sitemap.xml, robots.txt and manifest.webmanifest; opengraph-image and twitter-image files (including per-segment and per-generateStaticParams ones) build real PNGs from ImageResponse JSX with a pure Go renderer (flex layout, text and fonts, colours, gradients, images), and feed og:image and twitter:image. Anything the renderer cannot draw is a build error (KR7131). A file wins over a plugin (KR7133). Per-request ImageResponse works in Node API routes under krate start only; Workers and serverless functions have no renderer. krate migrate nextjs maps these files natively. Not supported: CSS grid, transform, filter, SVG, icon and apple-icon files, opengraph-image.alt.txt.

Evidence: `TestMetadataRouteFilesBuild`, `TestImageResponseUnsupportedFailsTheBuild`, `TestMetadataRoutePluginOverrideIsSaid`, `TestImageResponsePerRequestOnTheHost`, `TestMetadataRoutesMigrateNatively`

### Code highlighting (build, request time, browser)

**Source:** Supported - **Islands:** Supported - **SSR:** Supported

Code is highlighted by chroma at build time, by the same lexers in the Node renderer for request-time code, and by the same lexers loaded on demand for code that changes in the browser. The lexers are exported from the binary's own chroma and run token for token (the markup is identical), in light and dark from one stylesheet, with a size, line and time limit that leaves text plain instead of stalling.

Known limits:

- Request-time highlighting runs in the Node renderer; serverless deploy targets write plain text.
- The browser and request-time highlighters ship 16 languages; other languages are highlighted at build time only.
- A Markdown fence that names a language outside the shipped set leaves the snippet plain.

Diagnostics: `KR7150`, `KR7151`

Evidence: `TestParityCorpus`, `TestTranspiledRegexesMatchRegexp2`, `TestPatternOverrides`, `TestAdversarialInputs`, `TestRuleProfile`, `TestThemeCSSMatchesChromaPerMode`, `TestCodeThemeBrowser`, `TestDynamicCodeBrowser`, `TestSSRCodeHighlightingE2E`, `TestDynamicCodeShipsHighlighterOnlyWhereUsed`, `ci:code highlighting size budget`

### Native: window and custom scheme

**Linux:** Supported - **macOS:** Partial - **Windows:** Supported

The shell opens a window around the system webview (WebKitGTK 4.1 through purego on Linux, WKWebView on macOS, WebView2 on Windows) and serves the built UI from an in-process custom scheme with a content security policy. Navigation away from the app origin is blocked, paths cannot escape the asset directory, and browser mode serves the same app on a loopback port behind a session token.

Known limits:

- Linux GUI tests need a display and WebKitGTK 2.40 or newer (KRATE_NATIVE_GUI=1); CI runs them under Xvfb.
- WebView2 on Windows needs the Evergreen runtime installed; krate native doctor reports it.
- macOS and Windows: the CI native job runs the platform GUI tests on both (macOS TestScenario and TestTwoWindows, Windows TestGUIRoundTrip and TestGUITwoWindows: page load, request body echo, incremental streaming, secure context), TestGUIShell and TestNativeE2EGUI through a real WKWebView and WebView2. Blocking an in-window navigation to another origin has a real-window test on Windows (TestGUIRoundTrip) but none on macOS, where the navigation delegate is covered only by reading the code.

Evidence: `TestGUIPage`, `TestGUINavigationLock`, `TestGUIShell`, `TestGUITwoWindows`, `TestStaticResolution`, `TestStaticRejectsTraversal`, `TestDefaultCSPShape`, `TestBrowserMode`, `TestNativeE2EGUI`

### Native: streaming responses

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

Responses are streamed to the webview as the handler writes them: headers go out first, body chunks are flushed as they come, a handler panic before the first byte becomes a 500, and closing the reader cancels the handler's context. Server-sent streams from host code use the same path.

Known limits:

- macOS and Windows: incremental delivery through a real WKWebView and WebView2 is checked by the platform GUI tests (at least 3 reads spread over 100 ms), and the response writers (macOS task writer: panic before the header, cancel when the task stops; Windows: flush, abort, block buffer) have unit tests that run on both. A reader closed by the page cancelling a real request is driven through a real window on Linux only (TestGUIRequestAbort).

Evidence: `TestServeStreamsChunks`, `TestServeCancelsWhenReaderCloses`, `TestServePanicBeforeHeadersIs500`, `TestStreamValuesThenEnd`, `TestStreamError`, `TestGUIRequestAbort`

### Native: host calls, streams and signals

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

The page calls host functions by id over the scheme: unary calls, server streams and signals (a value with change notifications that reconnects with the current value). Calls are cancellable, errors map to stable codes, concurrency is bounded, and permissions gate which SDK ids a page may reach.

Known limits:

- The scheme package is pure Go and identical on every OS; its tests run on Linux, macOS and Windows in CI, and TestGUIShell sends calls, streams and signals from a real WKWebView and WebView2 to host code.

Evidence: `TestInvoke`, `TestInvokeStreamAndSubscribe`, `TestSignalsReconnectGetsCurrentValue`, `TestBatchFastResultArrivesBeforeSlow`, `TestCancelByID`, `TestErrorMapping`, `TestConcurrencyIsBounded`, `TestEveryIDIsRegisteredAndGated`

### Native: Go host code

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

Go host code (functions, streams and signals in .go files next to the pages) runs in a child process under krate native dev and krate native build, and is linked into the single executable in a release build. The child restarts after a crash and reconnects subscriptions.

Known limits:

- Needs the Go toolchain on the build machine (krate native doctor reports it).
- macOS and Windows: the child process lifecycle, crash restart and reconnect (gohost tests) run on both in CI, TestGUIShell replaces the Go host binary and restarts the child in a real window, and TestNativeReleaseE2EGUI runs a release executable with Go host code. TestNativeReleaseExample (the browser variant) runs on Linux only. On Windows a running Go host binary cannot be replaced, so a Go edit under krate native dev reports that.

Evidence: `TestChildLifecycle`, `TestChildCrashAndRestart`, `TestRunInProcess`, `TestNativeReleaseExample`, `TestNativeReleaseGoOnlyHasNoQuickJSAndShakesTray`, `fixture:examples/native-tasks`

### Native: TypeScript host code on QuickJS

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

TypeScript host code (*.host.ts files and @host functions) is bundled to one script and runs in the embedded QuickJS engine with no external runtime. Stack traces map back to the TypeScript sources, a watchdog stops runaway synchronous code, and the globals follow the published runtime compatibility ledger.

Known limits:

- QuickJS is not Node: only the globals in the runtime compatibility ledger exist; code that needs more selects a node, bun or deno host.
- macOS and Windows: the embedded QuickJS engine is pure Go and its tests (calls, timers, source-mapped stacks, the watchdog, the compatibility ledger) run on both in CI; TestNativeReleaseTSOnlyRuns builds and runs a TypeScript-only release executable there.

Evidence: `TestCalls`, `TestSourceMappedStack`, `TestWatchdog`, `TestTimers`, `TestCompatLedger`, `TestNativeReleaseTSOnlyRuns`, `fixture:examples/native-tasks`

### Native: Node, Bun and Deno host children

**Linux:** Supported - **macOS:** Partial - **Windows:** Partial

TypeScript host code can run as a child process of the shell under Node, Bun or Deno (native.host.runtime, or native.host.islands for single packages). Deno gets permission flags derived from the app's permissions (including its own unix socket, which recent Deno versions treat as network access); a release build compiles the child into an executable (Node SEA, bun build --compile, deno compile).

Known limits:

- Linux: Node, Bun and Deno children are run end to end through the real krate-shell in a window (TestNativeHostChildGUI: host call, host signal, stream, thrown error, binary and unicode arguments). Node and Bun release builds are verified; the Deno release command line (deno compile with plain --allow-net for unix targets) is unit tested only, and the Bun and Node compile tests skip when the tool is missing.
- Deno must be recent enough to take unix:<path> in --allow-net (verified with 2.9.8); older versions reject the entry, and the dev command line is not adapted to them.
- macOS: Node children run in CI (TestHostChildNode, TestNativeHostChildNodeEndToEnd and the hostchild tests on macos-latest) over a Unix socket. TestNativeHostChildGUI also runs Node, Bun and Deno through the real shell there; its first passing run on macos-latest is not confirmed yet.
- Windows: Node, Bun and Deno cannot use AF_UNIX there, so the shell uses loopback TCP with a random token. The transport tests and the hostchild tests run on windows-latest, TestHostChildNode starts a Node child there, and TestNativeHostChildGUI runs the three runtimes through the real shell; its first passing run there is not confirmed yet.
- arm64: Bun and Deno publish no Windows on ARM builds, so the windows-11-arm CI job skips those two (KRATE_SKIP_RUNTIMES) and runs Node only; Linux arm64 and macOS run all three.

Evidence: `TestHostChildNode`, `TestNativeHostChildNodeEndToEnd`, `TestNativeDevReloadsHostChild`, `TestDenoFlags`, `TestCommand`, `TestBuildReleaseNodeSEA`, `TestBuildReleaseBun`, `TestBuildReleaseDeno`, `TestReleaseBunCompiled`, `TestReleaseNodeSEA`, `TestTCPHandshake`, `TestParseRoundTrip`, `TestFor`, `TestChildTCPTransport`, `TestRunTCPToken`, `TestServeParentTCP`, `TestRemoteTCPSendsToken`, `TestNativeHostChildGUI`, `TestExpandSocketArgs`, `TestDenoCompileNetFlagPerTarget`

### Native: SDK data areas (kv, db, fs, net, os)

**Linux:** Supported - **macOS:** Partial - **Windows:** Partial

The key-value store, SQLite database, scoped file access, HTTP client with secrets, and OS information, directories, secrets and openExternal are available to the page and to Go and TypeScript host code, each behind its permission. Two processes can share a database and a store.

Known limits:

- Linux secrets use the freedesktop Secret Service over D-Bus (verified against a fake service); without a provider os.secret answers unsupported.
- macOS and Windows: the key-value store, SQLite, scoped files, HTTP client and OS information tests run on both in CI. os.secret (Keychain on macOS, Credential Manager on Windows) and os.openExternal have no test on either: only the Linux Secret Service is exercised.

Evidence: `TestGoAPI`, `TestTwoProcesses`, `TestWALAndPragmas`, `TestTraversalIsDenied`, `TestSymlinkEscapeIsDenied`, `TestClientTimeouts`, `TestSecretNotForwardedAcrossHosts`, `TestSecretServiceRoundTrip`, `TestInfo`, `TestUIDefaultsAndDenials`

### Native: SDK windows

**Linux:** Partial - **macOS:** Partial - **Windows:** Partial

Extra windows on routes, title and size, fullscreen, maximize, minimize, frameless windows, close-requested interception and per-window permission scopes bound to the webview, not to anything the page sends. A window can hold several webviews (native-views).

Known limits:

- Per-window permission scopes are enforced against the native webview a request came from (see native-view-identity). In browser mode the page's own x-krate-window header is the identity, so a script there can name another window's scope.
- Wayland: a client cannot place windows or keep them above others, so SetPosition and SetAlwaysOnTop are accepted and ignored there and the capability report says unsupported. The GUI test packages also run under weston (GDK_BACKEND=wayland, weston nested on Xvfb); TestWaylandPlacementIsIgnored checks the ignored placement. Window position and always-on-top were verified on X11 only.
- Fullscreen, maximize and minimize are verified with openbox on Xvfb (KRATE_NATIVE_WM=1), not on a full desktop environment.
- macOS and Windows: window creation, title and size, two windows and per-window scopes bound to the webview run in CI (platform GUI tests, TestGUIScopedWindowCannotForgeAWiderScope). Fullscreen, maximize, minimize, frameless windows, window position, always-on-top and close-requested interception have no test on either OS.

Evidence: `TestGUIWindowControls`, `TestGUIWindowOps`, `TestGUICloseRequested`, `TestGUITwoWindows`, `TestWindowOps`, `TestSecondWindowOnARoute`, `TestWindowOpenScopes`, `TestWindowScopeNarrowsCalls`, `TestWaylandPlacementIsIgnored`

### Native: requests are bound to the webview they came from

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

The platform layer tags every request on the app scheme with the webview that made it (WebKitGTK webkit_uri_scheme_request_get_web_view, the webView of WKURLSchemeHandler, one WebView2 WebResourceRequested handler per view) and the shell maps that view to its window or view id and permission scope. The x-krate-window header that page script can set is ignored in a native window, so a scoped page cannot claim a wider scope. A request from a webview the shell cannot place gets a scope that allows nothing.

Known limits:

- Browser mode (krate native dev --browser) has no native webviews, so the page's x-krate-window header is the identity there and a script can send another window's id; scopes in browser mode only catch mistakes.
- A scope limits what a page may ask the host to do; it does not isolate the page. Every window and view shares one origin, so pages of different scopes can read each other's localStorage and cookies.
- macOS and Windows: TestGUIViews (platform layer) and TestGUIScopedWindowCannotForgeAWiderScope (shell) run in CI against a real WKWebView and WebView2 and check that requests are tagged with the view they came from and that the page header is ignored. On Windows the identity is the handler object registered for the view, not a pointer comparison of the sender.

Evidence: `TestIdentifyOverridesTheWindowHeader`, `TestIdentify`, `TestViewContext`, `TestScopedWindowCannotForgeAWiderScope`, `TestViewScopeIsBoundNatively`, `TestGUIScopedWindowCannotForgeAWiderScope`, `TestGUIViewIdentity`, `TestViewScopes`, `TestViewWindowMapping`, `TestGUIViews`

### Native: views and split panes

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

A window can hold several webviews laid out as a tree of horizontal and vertical splits with draggable dividers, minimum pane sizes and ratios that can be read back after a drag. Views are added, removed, focused, navigated and given their own permission scope; they report added, navigated, removed and closed events and can post messages to each other. All views share the app's one web process; an extra view costs about what a page costs (about 1 MB of web process memory on the WebKitGTK 2.54 sysroot, about 40 MB with software-GL compositing on the CI runner). Layout arithmetic is pure Go and tested on every OS; the real-window view tests and the native-views example run in CI on Linux, macOS and Windows.

Known limits:

- Divider drags on Linux are verified by calling gtk_paned_set_position, which is what the handle does, not by moving a real pointer.
- macOS (nested NSSplitView, Auto Layout minimum sizes) and Windows (several WebView2 controllers in one HWND, dividers hit-tested in the window procedure) run TestGUIViews and TestNativeE2EViewsGUI in the CI native job, with the layout and drag arithmetic unit-tested on every OS. Divider drags on those two are not driven with a real pointer; the Windows splitter hit-testing and DPI scaling have unit tests (TestSplitterHitAndCursor, TestSplitterDrag, TestSplitterDPIScales). The WebView2 NavigationCompleted and WindowCloseRequested handler GUIDs are written from memory of WebView2.h; no platform test asserts the navigated or closed view events on Windows or macOS.
- On macOS a page's window.close() does not remove a secondary view (no closed event for it); Linux and Windows do.
- Release applies to a whole window, not to one view. While a window is released, adding, removing or re-laying out its views fails.
- A divider drag fires no event; read the ratios with layout().
- Messages are JSON up to 1 MiB, not queued for a page that has not loaded, and carry the sender's view id only when the sender is a page.

Evidence: `TestNativeE2EViewsGUI`, `TestArrangeTwoPanes`, `TestArrangeNestedCoversBounds`, `TestSplitSizesMinimums`, `TestMinSizePropagates`, `TestHitDividerAndDrag`, `TestValidate`, `TestSplitLeafAndRemove`, `TestRatiosAndMins`, `TestSplitterHitAndCursor`, `TestSplitterDrag`, `TestSplitterDPIScales`, `TestViewOps`, `TestViewEventsAreHeldUntilRegistered`, `TestGUIViewsTwoPane`, `TestGUIViewsNestedThreePane`, `TestGUIViewsReleaseRestore`, `TestGUIViewClosedByItsPage`, `TestGUIViewsMemory`, `TestViewMessagingEventsAndLayout`, `TestGUIViews`, `fixture:examples/native-views/src/pages/index.tsx`

### Native: print, PDF, find, zoom and capture of a view

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Per view (a window's primary view or any added view): the system print dialog, PDF output with page size, margins, orientation and backgrounds, find in page with match counts and stepping, a zoom factor with the Ctrl or Cmd plus, minus and 0 keys kept working, and a PNG capture of the view. WebKitGTK uses GtkPrintOperation, WebKitFindController, the zoom level and webkit_web_view_get_snapshot; WKWebView printOperationWithPrintInfo, findString:withConfiguration:, pageZoom and takeSnapshot; WebView2 ShowPrintUI, PrintToPdf, put_ZoomFactor and CapturePreview.

Known limits:

- The print dialog test on Linux closes the GTK dialog programmatically; macOS and Windows open it only with KRATE_NATIVE_PRINT_DIALOG=1 because a test cannot close a system dialog.
- The PDF page size and orientation are asserted on Linux only. macOS and Windows tests check for a PDF.
- WebKitGTK reports the match count but not the highlighted index (Krate follows it as you step); WKWebView reports only found or not, and WebView2 has no find interface in the WebView2.h Krate builds against, so macOS counts in the visible text and Windows runs window.find and counts the same way.
- WebView2 PrintToPdf takes portrait paper dimensions with an orientation, as GTK does; that reading of the settings is untested on Windows.
- macOS and Windows: untested. The code is written against the platform APIs, builds for both, and the same GUI tests run in CI on macos-latest and windows-latest.

Evidence: `TestGUIPageZoom`, `TestGUIPageFind`, `TestGUIPageCapture`, `TestGUIPagePrintPDF`, `TestGUIPagePrintDialog`, `TestNativeE2EPageGUI`, `TestPageZoomPerView`, `TestPagePrintFindCapture`, `TestPageNeedsPermission`, `TestPDFOptionsResolve`, `TestZoomSteps`, `TestEncodePNGFromBGRA`, `TestFindTracker`, `TestFindScripts`, `fixture:examples/native-page/src/pages/index.tsx`

### Native: downloads started by the page

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Downloads from a link with the download attribute, a blob URL or an attachment response get a destination (the native save dialog by default, or a configured folder with free file names), report started, progress, completed, cancelled and failed events with the final path, and can be cancelled. WebKitGTK uses the download signals, WKWebView WKDownload and WebView2 DownloadStarting. WebKit cannot download the app's custom scheme, so those downloads are fetched from the app handler by the shell and report the same events.

Known limits:

- A download of a page of the app origin is requested once more than the page made it, and the file name comes from the link's download attribute only when the link is in the DOM at the time.
- Downloads need the downloads area to be linked (the app imports @krate/app/downloads); otherwise the engine's default applies, and downloads of the app's own scheme do nothing.
- Windows hands the app origin to WebView2's own download path, which has not been run.
- macOS and Windows: untested. The same GUI tests run in CI on macos-latest and windows-latest.

Evidence: `TestGUIPageDownloadCompletes`, `TestGUIPageDownloadDeclined`, `TestGUIPageDownloadCancel`, `TestGUIPageDownloadAttachmentAndBlob`, `TestGUIPageDownloads`, `TestSchemeDownloadCompletes`, `TestSchemeDownloadNames`, `TestSchemeDownloadFailures`, `TestSchemeDownloadCancel`, `TestUniqueName`, `TestSafeFileName`, `TestDownloadsPolicy`, `TestDownloadsEventsListCancel`, `TestNativeE2EPageGUI`

### Native: cookies and site data

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

List, get, set and delete cookies by URL, and clear cookies, the cache and storage. WebKitGTK uses WebKitCookieManager and WebKitWebsiteDataManager (cookies persist in the app data directory), WKWebView WKHTTPCookieStore and WKWebsiteDataStore, WebView2 ICoreWebView2CookieManager and ClearBrowsingData.

Known limits:

- WebKit keeps no cookies for the app's custom scheme (document.cookie is inert on krate://app and the jar refuses a single-label host such as app), so on Linux and macOS cookie calls for the app origin answer unsupported; pass the URL of an http(s) site. On Windows the app origin is https://app.localhost and has cookies.
- An older WebView2 runtime without ICoreWebView2Profile2 can only clear cookies.
- macOS and Windows: untested. The same GUI tests run in CI on macos-latest and windows-latest.

Evidence: `TestGUIPageCookies`, `TestCookies`, `TestCookieDomainMatches`, `TestCookieOrigin`, `TestNativeE2EPageGUI`

### Native: remote views cannot reach the app

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Remote content (web pages) lives in views and windows created as remote (@krate/app/remote). A remote view has its own web context, configuration or profile with no handler for the app scheme and no bridge, so fetches, iframes, images, window.open and navigations to the app fail; the shell gives requests from a remote view the empty scope; navigation is limited to the start origin plus an allowlist, with everything else opened in the system browser or dropped; permissions are denied and downloads cancelled.

Known limits:

- Windows resolves https://app.localhost to the loopback address, so that host is refused as a remote URL, allowed origin and navigation target.
- Subframes follow the allowlist but never open the system browser. On WebKitGTK the policy callback does not tell main frames from frames, so a refused frame navigation of a remote page is reported with the same verdict as a top-level one.
- Opening needs a WebView2 runtime with ICoreWebView2Environment10 (profiles) on Windows.
- macOS and Windows: untested. The same GUI tests (gui_remote_test.go) run in CI on macos-latest and windows-latest.

Evidence: `TestGUIRemoteBasics`, `TestGUIRemoteCannotReachApp`, `TestGUIRemoteNavigationRules`, `TestGUIRemoteRedirectOffOrigin`, `TestGUIRemoteViewInAppWindow`, `TestRemoteDecide`, `TestRemoteDecideModes`, `TestRemoteResolveErrors`, `TestRemoteViewsHaveNoScope`, `TestRemoteNavigateAndPost`, `TestRemoteNeedsPermission`, `TestRemoteExternalOpenIsRateLimited`, `TestNativeE2ERemoteGUI`

### Native: remote storage partitions

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Remote content gets its own storage partition: the default one (persistent, never the app's), named persistent ones (persist:name) and in-memory ones (name). Cookies, local storage, caches, logins and certificate decisions are not shared between partitions or with the app, and a partition can be cleared on its own.

Known limits:

- WebKitGTK in-memory partitions use a temporary directory removed at exit rather than WebKit's ephemeral managers, whose certificate exceptions leak between partitions.
- macOS before 14 has no named persistent data stores: persist:name answers unsupported and the default partition is in-memory.
- Windows clears a profile through an open view of it.
- macOS and Windows: untested. The same GUI tests (gui_remote_test.go) run in CI on macos-latest and windows-latest.

Evidence: `TestGUIRemotePartitions`, `TestPartitions`, `TestProfileName`, `TestNativeE2ERemoteGUI`

### Native: request rules for remote views

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Declarative request rules per partition (block, redirect, add headers, with URL match patterns) and a request stream, with capability reporting. WebKitGTK blocks with a user content filter and rewrites main-frame navigations; WKWebView with a content rule list and navigation decisions; WebView2 with WebResourceRequested on the remote view.

Known limits:

- WebKitGTK and WKWebView cannot redirect or add headers to subresources, and WKWebView does not observe them; capabilities() reports the level per feature.
- Block rules on WebKit follow the content-blocker regular expression subset.
- macOS and Windows: untested. The same GUI tests (gui_remote_test.go) run in CI on macos-latest and windows-latest.

Evidence: `TestGUIRemoteInterception`, `TestMatchURLPattern`, `TestEvalRules`, `TestRuleValidation`, `TestContentBlockerJSON`, `TestRemoteRulesAndNetwork`, `TestRemoteMessagesEventsAndRequests`

### Native: remote user agent, proxy, certificates and logins

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

A user agent per partition or view, a proxy per partition (WebKitGTK, macOS 14), certificate errors that end the load unless the app accepts the host for that partition, HTTP basic and digest logins answered by the app, and client certificates from PEM files (Linux).

Known limits:

- Windows has no per-partition proxy (WebView2 takes --proxy-server for the whole environment) and no client certificates from files; macOS has no client certificates (they need a keychain identity). Those calls answer unsupported.
- macOS reports every certificate error as untrusted.
- macOS and Windows: untested. The same GUI tests (gui_remote_test.go) run in CI on macos-latest and windows-latest.

Evidence: `TestGUIRemoteUserAgent`, `TestGUIRemoteProxy`, `TestGUIRemoteTLS`, `TestGUIRemoteAuth`, `TestGUIRemoteAuthCancelled`, `TestGUIRemoteClientCertificate`, `TestRemoteCertificatesAndAuth`, `TestProxyValidate`, `TestCertErrorNames`, `TestParseChallenge`

### Native: remote message channel

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

The app and a remote page talk only through an opt-in channel: the page calls krateRemote.postMessage, the app gets the engine-reported origin; the app posts to a target origin and the page receives a message event with the app's origin. Messages carry a per-view token, so a frame that never ran the view's script cannot forge one.

Known limits:

- macOS and Windows: untested. The same GUI tests (gui_remote_test.go) run in CI on macos-latest and windows-latest.

Evidence: `TestGUIRemoteMessages`, `TestParseRemoteMessage`, `TestScripts`, `TestNativeE2ERemoteGUI`

### Native: shared web process, release and background mode

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

All windows of an app, and all webviews of a window, share one web process (related web views on Linux, one WebView2 environment on Windows). A window can release its webview (hide with release) and get it back on the same route and scroll position; native.window.keepAlive keeps a tray app running with no window; hidden and minimized pages get a krate:memory-pressure event.

Known limits:

- macOS and Windows: the pure-Go parts (view state, park on close, quit when empty) run in CI, and one web process per app is not asserted on either OS. No test releases and restores a webview on macOS or Windows, and none tests keepAlive or the memory-pressure event there; the Linux GUI tests are the only run. On macOS the single-web-process switch is a private WebKit selector guarded by respondsToSelector; on Windows ICoreWebView2_19 memory targets need a recent runtime and are skipped without it.
- A hidden page that is not released keeps its web process memory; only the page's own handlers of krate:memory-pressure and the shell's heap trim run. Release is what returns the web process memory.
- The network process of WebKitGTK stays alive while the app runs, so a released Linux app still holds it.
- Release destroys every webview of a window and restores them all with their routes and layout ratios; there is no per-view release.

Evidence: `TestGUISharedWebProcess`, `TestGUIReleaseRestore`, `TestGUIKeepAlive`, `TestGUINavigateReleased`, `TestGUIMemoryPressureOnHide`, `TestGUIWindowsBackground`, `TestParseViewState`, `TestParkOnClose`, `TestQuitWhenEmpty`, `TestGUIViewsReleaseRestore`, `TestMemorySelectors`, `TestLeversAreGuardable`

### Native: lean pages and the dev Memory panel

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

Native builds ship no script on a page without client code and do not preload lazy chunks; krate native check and native builds warn (KR7270 to KR7274) about oversized images, large inlined data: URLs, web fonts, compositing CSS effects and long lists without windowing; krate native dev has a Memory panel with PSS, private dirty, DOM nodes and the JS heap where the engine exposes it.

Known limits:

- The checks are static: sizes set from CSS classes, values chosen at run time and data fetched later are not seen. The web font check is a heuristic.
- WebKit has no JS heap size API, so the Memory panel shows the heap as unavailable on Linux and macOS and points at the web process row.
- macOS and Windows: the checks are build-time and platform independent (their tests run on both in CI); TestGUIZMemoryPanel runs the dev Memory panel in the real webview on both. Per-process numbers on macOS and Windows come from the memory report of the CI native job, which is informational (see native-budgets).

Evidence: `TestNativePagesLoadOnlyWhatTheyUse`, `TestLeanOversizedImage`, `TestLeanDataURIThreshold`, `TestLeanWebFonts`, `TestLeanCompositing`, `TestLeanWillChangeEverywhere`, `TestLeanLongLists`, `TestMemoryBrowserMode`, `TestGUIZMemoryPanel`, `TestNativeBuildWritesNoPrecompressedAssets`, `TestNativeTargetDisablesViewTransitionsByDefault`, `TestNativeDocsSiteE2E`, `TestGUIRequestBodyStress`

### Native: SDK dialogs, clipboard and menus

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Open, save, folder and message dialogs, clipboard text, PNG image and file list, and the menu bar and context menus with click events delivered to Go and to the page.

Known limits:

- A native dialog cannot be clicked headlessly: the tests check that it is created with the right options and answer it programmatically.
- Windows menu accelerators are shown but not active; Windows message boxes do not support custom button labels.
- macOS and Windows: untested. No test creates a native dialog, reads or writes the clipboard or builds a menu there; the SDK logic tests (TestDialogRoundTrip, TestMenuEvents) use a fake platform. The macOS menu accelerator parsing (TestAccelKeyEquivalent) is unit tested.

Evidence: `TestGUIDialogs`, `TestGUIClipboard`, `TestGUIMenus`, `TestDialogsAreCreatedWithTheRightOptions`, `TestClipboardThroughTheSDK`, `TestMenuClickReachesGoAndTheUI`, `TestDialogRoundTrip`, `TestMenuEvents`

### Native: rich clipboard formats

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

clipboard.write puts text, HTML and RTF on the clipboard together; readHtml, readRtf, formats, readFormat and writeFormat read and write by MIME name (UTIs on macOS, registered formats with the CF_HTML header on Windows).

Known limits:

- macOS and Windows: untested until the CI GUI tests (TestGUIClipboardRich) pass there.
- HTML is stored as given on Linux, without a charset declaration.

Evidence: `TestGUIClipboardRich`, `TestCFHTMLOffsets`, `TestDecodeCFHTMLFromOtherProducers`, `TestDecodeClipboardText`, `TestFormatNameMaps`, `TestClipboardRichFormats`

### Native: webview context menu

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

native.window.contextMenu and menu.setContextMenu disable, replace (custom: event with kind, link, image, selection, spelling; answered with menu items) or extend the engine's context menu per app, window or view; built-in krate: actions for editing, spelling and Inspect Element.

Known limits:

- Spelling suggestions and Add to Dictionary are not exercised by any test: no spelling dictionary is available to the Linux test environment.
- macOS and Windows: untested until TestGUIContextMenu passes in CI. Windows suggestions and Add to Dictionary are best effort.
- Inspect Element from a custom menu opens the inspector without selecting the element on WebKitGTK.

Evidence: `TestGUIContextMenuCustom`, `TestGUIContextMenuNone`, `TestGUIContextMenuExtend`, `TestGUIContextCommands`, `TestContextMenuActionValues`, `TestContextMenuSDK`, `TestManifestContextMenu`, `TestParseContextHit`

### Native: debugging in krate native dev

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

krate native dev --debug-go builds the Go host with -N -l and runs it under a headless Delve; breakpoints in src/lib/*.go hit and a Go edit restarts it on the same address. --debug-host starts node, bun and deno host children with --inspect. --debug-config vscode writes attach configurations.

Known limits:

- Needs dlv installed; the integration tests skip without it unless KRATE_REQUIRE_E2E is set.
- A debugger has to attach again after a Go edit; breakpoints are not carried over.
- macOS and Windows: untested.

Evidence: `TestNativeDebugGoHostGUI`, `TestNativeDebugGoHostRestart`, `TestChildWrap`, `TestDlvArgs`, `TestDlvDetachKillsTheProgram`, `TestFindDlvMissingHintsAtInstall`, `TestMergeLaunchJSON`, `TestResolveDebugAddr`

### Native: SDK notifications

**Linux:** Supported - **macOS:** Partial - **Windows:** Partial

Desktop notifications with actions. On Linux they use org.freedesktop.Notifications over D-Bus; on macOS UNUserNotificationCenter; on Windows, WinRT toasts.

Known limits:

- Verified against a real dunst daemon on a private session bus (text, icon, urgency, replacement by id, close from the app and from the daemon with the NotificationClosed reason, default action and action buttons, expiry, daemon gone) and against a fake service for protocol edge cases. Other daemons (GNOME Shell, KDE Plasma) were not run. Without a service that owns org.freedesktop.Notifications the call returns ErrUnsupported.
- macOS: TestNotifyUnbundled and TestNotifyBundled run in CI on macos-latest (the unbundled process gets the ErrUnsupported that names the fix; the generated app bundle takes the real UNUserNotificationCenter path), and the bundle planning is unit tested. That a notification banner appears and that clicking it or its actions reaches the app are manual checks. It needs an app bundle with a bundle identifier; krate native dev runs the shell from a generated <appId>.dev bundle under .krate/dev. The first notification asks for permission and a refusal is a permission_denied error. Icon and Timeout are ignored, and critical urgency is only time-sensitive (the critical level needs an Apple entitlement).
- Windows: the toast XML, ids and registry values are unit tested, TestToastXMLAcceptedByWinRT and TestToastDelegateInterfaces call the real WinRT on windows-latest, and TestGUIToast shows a toast there (it skips on a machine with notifications disabled). Clicking a toast and seeing the banner are manual checks. An unpackaged app registers its AppUserModelID under HKCU; a packaged app uses its package identity.
- Windows: a toast click uses protocol activation with a per-app URL scheme (HKCU registration for an unpackaged app, NSIS and MSIX registration too), not a COM activator. With the app running, the new process hands the click to it (WM_COPYDATA); with no instance, the click is kept and delivered when the page subscribes (the event that started the app). Unit tests cover the URL, the payload, the registry values and the manifest; the Windows CI job relaunches the test binary with the URL to prove both paths and checks the registration (first run pending), but a real click on a real toast, Action Center after a reboot and the packaged (MSIX) path are not confirmed. Windows has no per-toast timeout or urgency levels, and Show succeeding does not prove a banner was drawn (Focus assist and per-app settings can suppress it).

Evidence: `TestNotifyOverDBus`, `TestNotifyWithoutServiceIsUnsupported`, `TestNotificationAndTrayWithFakeDesktopServices`, `TestBuildToastXML`, `TestBuildToastXMLVariants`, `TestPinterfaceIID`, `TestAUMIDFor`, `TestToastXMLAcceptedByWinRT`, `TestToastDelegateInterfaces`, `TestGUIToast`, `TestPlatformBackendClose`, `TestNotifyUnbundled`, `TestNotifyBundled`, `TestBundleProblem`, `TestNotifyEvent`, `TestPlatformNotifyAvailability`, `TestPrepareMacDevBundle`, `TestRealDunstShowsNotification`, `TestRealDunstUrgencyLevels`, `TestRealDunstReplaceByID`, `TestRealDunstClosedByApp`, `TestRealDunstClosedByDaemon`, `TestRealDunstExpires`, `TestRealDunstDefaultAction`, `TestRealDunstActionButton`, `TestRealDunstGoneIsUnsupported`, `TestRealNoDaemonOnARealBus`, `TestToastXMLProtocolActivation`, `TestPlatformNotifyLaunchEventWaitsForSubscriber`, `TestPublishRetainedWaitsForTheFirstSubscriber`, `TestActivationPayloadRoundTrip`, `TestSchemeIsStableAndValid`, `TestURIRoundTrip`, `TestGUIToastActivationFromASecondProcess`, `TestGUIToastProtocolRegistration`

### Native: camera, microphone, screen capture and location permissions

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

The page's getUserMedia, getDisplayMedia and geolocation requests go to the SDK instead of the engine's own policy. A request is denied when its window or view scope lacks media or location; otherwise a remembered decision, the request stream (onRequest in TypeScript, media.OnRequest in Go), the native.media policy (allow, deny or a native prompt) and the operating system's consent decide. Decisions are remembered per app (stored) and per view. The UI half is @krate/app/media.

Known limits:

- Linux: verified in a real WebKitGTK 2.54 with the mock capture devices (KRATE_NATIVE_MOCK_CAPTURE): a camera or microphone request reaches the handler with the right kinds and view, allow yields a stream, deny yields NotAllowedError, no handler denies, geolocation deny is PERMISSION_DENIED, and a getDisplayMedia request arrives as kind screen. Real cameras and microphones were not used; no CI runner has one. An allowed geolocation request fails with POSITION_UNAVAILABLE without GeoClue, which the tests accept.
- macOS: implemented as a WKUIDelegate (media capture on macOS 12+, a private method for display capture, geolocation where WebKit calls it) and not yet run on a Mac. A runner has no camera, so the test calls the delegate methods of the real web view with its own blocks. Notification and clipboard requests have no public delegate.
- Windows: implemented with PermissionRequested under a deferral and ScreenCaptureStarting (ICoreWebView2_27, absent from older runtimes, which leaves screen capture ungated), with SavesInProfile off so the runtime never answers from its profile. Untested on Windows; the test runs Chromium's fake devices. getUserMedia with audio and video raises two requests there.
- Pointer lock, website data access and the other WebKitGTK request types, and the sensor, download, file and MIDI kinds of WebView2, are left to the engine.

Evidence: `TestGUIPermissionCameraAllowAndDeny`, `TestGUIPermissionMicrophoneAndBoth`, `TestGUIPermissionNoHandlerDenies`, `TestGUIPermissionGeolocation`, `TestGUIPermissionScreenCapture`, `TestScopeWithoutPermissionDenies`, `TestPromptRemembersAnswers`, `TestViewScopedDecisionWinsOverApp`, `TestStorePersistsAppDecisionsOnly`, `TestHandlerStreamDecides`, `TestHandlerTimeoutDenies`, `TestMediaHandlerIsInstalledAndPolicyApplies`, `TestMediaPromptAndRememberedDecisions`, `TestMediaScopeAndPermissionGates`, `TestMediaRequestHandlerStream`, `TestNativeE2EMediaGUI`, `TestGUIPermissionDelegate`, `TestCaptureKinds`, `TestGUIPermissionCamera`, `TestPermissionKindOf`

### Native: media permissions derived from the UI, usage descriptions and capabilities

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

The build scans the built UI for getUserMedia, getDisplayMedia and navigator.geolocation and grants media and location in a derived build (reported as KR7210 against native.permissions), and in dev for an app without a permission list. Packaging writes the macOS usage descriptions and device entitlements and the MSIX device capabilities from what the UI uses, and the build reports KR7290 when a macOS usage description is missing.

Known limits:

- The scan is textual: a call whose API name is built at run time is not found, and a library that mentions getUserMedia asks for the permission. The Info.plist, entitlements and MSIX output are unit-tested as text; no signed macOS app or MSIX package was run.

Evidence: `TestScanUses`, `TestScanDirUsesReportsFirstSite`, `TestDevUsePermissions`, `TestNativeMediaMeta`, `TestMediaUsageDiagnostics`, `TestAddUsesJoinsTheDerivedPermissions`, `TestNativeMediaDerivedPermissions`, `TestNativeMediaValidation`, `TestInfoPlistUsageDescriptions`, `TestMacEntitlementsForDevices`, `TestMSIXDeviceCapabilities`, `TestApplyAppJSON`

### Native: operating system consent for camera, microphone, screen and location

**Linux:** Partial - **macOS:** Untested - **Windows:** Untested

An allowed request is checked against the operating system: AVCaptureDevice, CGPreflightScreenCaptureAccess and CLLocationManager on macOS, the capability consent store on Windows (read only), and the Camera portal under Flatpak or Snap on Linux. media.osStatus reads the state and media.osRequest shows the system prompt where one exists. A refusal denies the request.

Known limits:

- Linux: outside a sandbox there is no consent switch for the camera or microphone, and the screen and location go through the ScreenCast portal and GeoClue inside WebKitGTK; only the Camera portal is driven by the SDK, and only against a fake portal in the tests.
- macOS: screen recording reads as granted or not yet granted, and a grant takes effect after a restart. Windows: an unpackaged app cannot be shown a consent prompt, so osRequest only reports the state. Neither was run on its OS.

Evidence: `TestOSConsent`, `TestMediaOSConsentIDs`, `TestPortalCameraGrantAndDeny`, `TestPortalOutsideASandboxHasNoSwitch`, `TestPortalErrorsAndTimeout`, `TestPrivacyMappings`, `TestConsentStatus`, `TestGUIPrivacyAndSources`

### Native: screen and window capture sources

**Linux:** Partial - **macOS:** Untested - **Windows:** Untested

media.sources lists capturable screens and windows with PNG thumbnails on macOS (CoreGraphics) and Windows (EnumDisplayMonitors, EnumWindows, GDI). No engine accepts a source chosen by the app, so media.chooseSource returns unsupported everywhere and getDisplayMedia always shows the system picker; the SDK can only allow or deny the capture request.

Known limits:

- Linux has no list (media.sources returns unsupported, so the row is partial): WebKitGTK reaches the screen through the ScreenCast portal and PipeWire, whose picker is the only list. ScreenCaptureKit is not used on macOS. Window titles and thumbnails on macOS need the Screen Recording permission. The listing code was not run on macOS or Windows.

Evidence: `TestMediaSources`, `TestWindowSources`, `TestScaleKeepsAspectAndColor`, `TestPNGRoundTrip`, `TestBGRA`, `TestGUIPrivacyAndSources`

### Native: SDK tray and global shortcuts

**Linux:** Partial - **macOS:** Partial - **Windows:** Partial

A tray icon with a menu (StatusNotifierItem and dbusmenu on Linux with a legacy XEmbed tray fallback, NSStatusItem on macOS, Shell_NotifyIconW on Windows) and global keyboard shortcuts (XGrabKey or the portal on Linux, Carbon RegisterEventHotKey on macOS, RegisterHotKey on Windows).

Known limits:

- The tray uses the StatusNotifierItem protocol when a StatusNotifier host exists and falls back to the legacy XEmbed system tray (GtkStatusIcon) when only an XEmbed tray manager owns _NET_SYSTEM_TRAY_S<screen> (X11 only). Stock GNOME with neither returns ErrUnsupported with a hint to install the AppIndicator extension. Verified against stalonetray (XEmbed, with real clicks and menu selection through XTEST) and against the Ayatana application indicator service as a real StatusNotifierWatcher and host; KDE Plasma, GNOME Shell with the extension and other panels were not run.
- Linux shortcuts are verified on X11 (XGrabKey). On Wayland they need the org.freedesktop.portal.GlobalShortcuts portal, verified against a fake portal only (TestShortcutPortal), and return ErrUnsupported without it. No portal backend that implements GlobalShortcuts can run headless here: the GNOME backend needs a running gnome-shell/mutter session, the KDE backend needs KGlobalAccel in a Plasma session, the Hyprland and COSMIC backends are not packaged for Debian or Ubuntu, and xdg-desktop-portal-wlr does not implement the interface.
- macOS: TestTrayItemAndMenu and TestHotKeyRegisterUnregister run in CI on macos-latest (a real NSStatusItem with its menu, and Carbon hot keys registered and released). Clicking the real status item and pressing a real hot key are manual checks (platform/darwin/MANUAL_TEST.md). With a tray menu any click opens the menu and is reported as click; right-click and double-click are only reported without a menu. Radio items show a checkmark. Single-tone gray icons become template images.
- macOS shortcuts use Carbon RegisterEventHotKey (no Accessibility permission). Keys are ANSI key positions, so other layouts differ, and macOS 15 ignores hot keys whose only modifiers are Option and/or Shift. A combination that is already registered is an error. Pressing a real hot key and clicking the real status item are manual checks (platform/darwin/MANUAL_TEST.md).
- Windows: the key table, the NOTIFYICONDATAW layout, event decoding and icon scaling are unit tested, and on windows-latest TestGUITrayLifecycle adds, modifies and removes a tray icon (it skips when the runner desktop has no taskbar), TestGUIHotkeys registers hotkeys, and TestGUITrayTaskbarCreated plays an explorer restart (the icon is deleted behind the app's back, the real TaskbarCreated message is broadcast, and the icon must be modifiable again; first run pending). The icon's appearance, its menu behavior, a real explorer.exe restart and real key presses are manual checks. An elevated app is allowed to receive TaskbarCreated with ChangeWindowMessageFilterEx.
- Windows: a shortcut another app or Windows holds fails with an 'already registered by another app' error; many Win+ combinations are reserved by the system. The tray menu has no accelerators and one icon per app.

Evidence: `TestTrayOverDBus`, `TestTrayWithoutHostIsUnsupported`, `TestShortcutPortal`, `TestShortcutWaylandWithoutPortalIsUnsupported`, `TestGUIGlobalShortcut`, `TestShortcutAndSupports`, `TestVirtualKey`, `TestHotkeySpec`, `TestHotkeyError`, `TestHotkeyTable`, `TestNotifyIconDataLayout`, `TestTrayEventKind`, `TestPNGToIconBits`, `TestGUITrayLifecycle`, `TestGUIHotkeys`, `TestTrayItemAndMenu`, `TestHotKeyRegisterUnregister`, `TestToCarbon`, `TestCarbonStatusError`, `TestInspectTrayIcon`, `TestAccelKeyEquivalent`, `TestPlatformTrayIsUsed`, `TestParseCmdOrCtrl`, `TestRealStatusNotifierHost`, `TestRealXEmbedTray`, `TestRealNoTrayAtAll`, `TestTrayFallsBackToXEmbedWithoutAStatusNotifierHost`, `TestTrayPrefersStatusNotifierOverXEmbed`, `TestNoTrayAtAllNamesTheAppIndicatorExtension`, `TestGUITrayTaskbarCreated`

### Native: SDK app lifecycle

**Linux:** Supported - **macOS:** Partial - **Windows:** Partial

Single instance, deep links and the Linux .desktop entry that registers the app's URL schemes.

Known limits:

- URL scheme registration is a packaging step on macOS (Info.plist) and Windows (registry); the runtime code is shared.
- macOS and Windows: single-instance locking, stale socket replacement and URL extraction from arguments run in CI on both. No test registers a scheme with the OS or opens a deep link there; the Linux .desktop entry has a test only on its own platform.

Evidence: `TestSingleInstance`, `TestStaleSocketIsReplaced`, `TestURLsFromArgs`, `TestDesktopEntry`

### Native: file associations

**Linux:** Partial - **macOS:** Untested - **Windows:** Untested

native.fileAssociations is validated (extensions, media types, roles, icons) and packaged as macOS CFBundleDocumentTypes with exported UTIs, MSIX file type associations, NSIS ProgID and OpenWithProgids registry entries removed again on uninstall, and a Linux MimeType with a shared-mime-info file in the deb, rpm and AppImage.

Known limits:

- Only the generated files are checked: no package was installed on a desktop, so Launch Services, the Windows registry and update-mime-database never read them. The NSIS script was not compiled with makensis and the MSIX was not installed.
- An installed app is not made the default handler of a type another program owns, and the viewer role has no Windows equivalent.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestValidateNative`, `TestNativeFileAssociationsLoad`, `TestFileTypesDefaults`, `TestInfoPlistDocumentTypes`, `TestMacBundleHasTypeIcons`, `TestAppxManifestFileTypes`, `TestMSIXShipsTypeLogos`, `TestNSISFileAssociations`, `TestSharedMimeXML`, `TestDesktopFileMimeTypes`, `TestLinuxPackagesCarryTheMimeFile`, `TestAppImageCarriesTheMimeFile`, `TestRequireLinksAnAreaTheCodeNeverReaches`, `TestDesktopEntryFileTypes`

### Native: opening files

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Files on the command line of the first launch, handed over by a second launch through the single-instance lock, or sent by macOS to the app delegate reach app.onOpenFile and openedFiles (page and Go) with absolute paths, raise the window, and are granted to the fs scope like a dialog pick.

Known limits:

- On Linux a first launch with a file and a second launch handing over another were run through the real shell and a real webview (TestNativeE2EFilesGUI); no desktop environment launched the app from a double click.
- macOS: the delegate method is called by the test, not by Launch Services. Windows and Linux file-manager launches were not tried.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestFilesFromArgs`, `TestFileURLPaths`, `TestLaunchFilesAreOpenedAndGranted`, `TestOnOpenFileReplaysThenStreams`, `TestSecondInstanceOpensItsFiles`, `TestSystemOpenedURLsReachOnOpenURL`, `TestIsAppDir`, `TestDropLaunchServicesArgs`, `TestPathQueueHoldsUntilAHandlerIsSet`, `TestNativeE2EFilesGUI`, `TestGUIDelegateOpensFilesAndURLs`

### Native: dropped file paths

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Files dropped from the desktop onto a view are reported with absolute paths (window.onFileDrop, Go window.OnFileDrop) and granted to the fs scope, while the page keeps its HTML5 drop events: GTK drag signals on Linux, a WKWebView subclass reading the dragging pasteboard on macOS, postMessageWithAdditionalObjects and ICoreWebView2File on Windows.

Known limits:

- Linux: a real drag between two GTK windows with synthesized pointer events (xdotool, needs a window manager) delivers the path and the page's drop event. In that setup WebKitGTK offers no File objects to the page with or without the watcher, so File contents were not checked. Without xdotool the real drag test skips; the signal-level tests use plain GTK widgets.
- macOS: the drop path is checked with a fake NSDraggingInfo and the class registration; a real Finder drag was not tried.
- Windows: the injected script runs against the real WebView2 with a script-made File (which has no path); the COM walk that reads paths is tested with Go fakes of the WebView2.h layouts. A drag from Explorer was not tried and needs a WebView2 Runtime with ICoreWebView2WebMessageReceivedEventArgs2.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestGUIFileDropFromARealDrag`, `TestGUIFileDropHoverThenDrop`, `TestGUIFileDropWithoutHoverData`, `TestGUIFileDropForgetsAHoverThatLeft`, `TestGUIFileDropIgnoresWhatIsNotAFile`, `TestGUIFileDropWatchesTheRealView`, `TestParseURIList`, `TestPathsFromURIs`, `TestFileDropsReachTheViewAndAreGranted`, `TestInjectFileDropGoesThroughThePlatformHandler`, `TestNativeE2EFilesGUI`, `TestGUIWebViewWatchesDrops`, `TestGUIDropReadsFilePathsFromThePasteboard`, `TestDropShimInNode`, `TestParseDropMsg`, `TestDroppedFilesReadsPathsFromAdditionalObjects`, `TestGUISyntheticDropReachesTheHost`

### Native: release as a single executable

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

krate native build --release links the shell, the UI, the host code and only the SDK areas the app uses into one executable per target (CGO_ENABLED=0, stripped, with a report of linked and left-out areas). Cross-compiling to the other OSes is covered; an executable built on its own OS runs in the CI native job on macOS and Windows.

Known limits:

- TestNativeReleaseE2EGUI builds a release executable on macos-latest and windows-latest and drives it in a real window, and the release tests that need no window run there too. Cross-compiling to another OS is checked for the executable format only; such an executable is never launched.
- Needs Go on the build machine; without it (native.release.compile auto) the build falls back to the prebuilt-shell layout.

Evidence: `TestNativeReleaseDerivesPermissionsAndReport`, `TestNativeReleaseTSOnlyRuns`, `TestNativeReleaseGoOnlyHasNoQuickJSAndShakesTray`, `TestNativeReleaseCrossCompiles`, `TestNativeReleaseWindowsFlagsAndTargets`, `TestNativeReleaseExample`, `TestNativeReleaseE2EGUI`

### Native: prebuilt shell for apps without Go

**Linux:** Supported - **macOS:** Partial - **Windows:** Partial

The @krate/core-<platform> packages carry krate-shell next to krate. krate native build then writes a runnable dist-native/<os>-<arch>/<productName> with app/ beside it for a TypeScript-only app, with no Go toolchain, and krate native package turns that into tar.gz, deb and the other formats. The shell is found in order: --shell, $KRATE_SHELL, next to the krate executable, a build from a Krate checkout.

Known limits:

- The Linux test uses a stand-in shell executable placed next to a copy of the test krate binary and a PATH with no go command; it proves the layout and packaging, not a launch.
- The packages for all seven platforms are built by scripts/build-platform-packages.mjs and checked by pnpm check:release; the npm install of them (scripts/smoke-install.mjs) runs in the release workflow.
- Alpine (musl): the @krate/core-linux-x64-musl shell is linked with musl's loader (/lib/ld-musl-x86_64.so.1) instead of glibc's, so it starts and `krate-shell --doctor` runs; the install smoke test checks this on node:alpine in CI. A window needs WebKitGTK 4.1 from the distribution, which was not run on Alpine.
- macOS and Windows: the lookup order, layout and packaging tests run in CI on both with a stand-in shell, and the install smoke test installs the @krate/core-<platform> package on macos-latest and windows-latest and runs its krate-shell --doctor. A dist-native build with the prebuilt shell is not launched there.

Evidence: `TestFindPrebuiltShellOrder`, `TestLocateNativeShellOrder`, `TestAssembleShellDist`, `TestNativePrebuiltShellWithoutGo`, `TestDiscoverPrefersReport`

### Native: krate native doctor

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

Reports the Go toolchain, the shell binary and its path, the system webview (WebKitGTK 4.1 version and library paths and display on Linux, the OS version on macOS, the WebView2 runtime version on Windows, through krate-shell --doctor), the packaging and signing tools, and the host runtime the config selects. Exits 1 when something the config needs is missing.

Known limits:

- The report logic is tested with injected probes for all three OSes. The real probes run in CI: TestBinaryDoctor runs krate-shell --doctor on all three OSes and the install smoke test runs it from the installed platform package on macos-latest and windows-latest. No test checks what the macOS and Windows probes report for a missing or too old webview.

Evidence: `TestHealthyTypeScriptOnlyLinuxWithoutGo`, `TestGoRequiredForGoHostAndCompileAlways`, `TestWebviewMissingIsRequired`, `TestHostRuntimes`, `TestPackagingToolsPerOS`, `TestNativeDoctorCommand`, `TestBinaryDoctor`, `TestProbeShape`

### Native: krate native test and the app driver

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

krate native test builds the app like native dev, bundles the project's *.native.test.ts files with esbuild and runs them with node --test. The @krate/app/driver module launches the real shell (--dev) per test file with isolated data directories and drives pages over the dev control channel: evaluate, click, fill, press, text, waitFor, url, errors, calls of host functions and SDK ids, several windows and views, and injected tray, menu and shortcut events for app logic behind native events. Input is DOM-level; real operating-system input is out of scope.

Known limits:

- Clicks, typing and key presses are synthesized DOM events (isTrusted is false); native dialogs, the real tray icon, menu bars and notifications cannot be operated, only their events injected.
- page.screenshot() is not available: the platform layer has no webview capture API yet.
- No --browser mode for tests; they drive the native shell.
- The macOS and Windows runs happen only in the CI native job (a desktop session is needed); the per-OS data-directory isolation (HOME on macOS, APPDATA and LOCALAPPDATA on Windows) has not been tried by hand.

Evidence: `TestNativeTestCommandGUI`, `TestFindNativeTests`, `TestBundleNativeTests`, `TestRunNativeTestRunsNodeWithEnvironment`, `TestNativeTestFlags`, `TestNativeTestListAndDiagnostics`, `TestServeControlDriverCommands`, `TestHostInjectMenuAndViews`, `TestHostInjectShortcut`, `fixture:examples/native-tasks/tests/tasks.native.test.ts`, `fixture:examples/native-views/tests/views.native.test.ts`

### Native: migration guides and the worked Electron port

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Migration guides from Electron and from Tauri v2 map concepts, APIs, security, packaging, updates, testing and debugging to Krate Native. examples/native-from-electron is a small Electron app (electron/, pinned, not run by CI) and its port: host functions and a host signal for ipcMain and webContents.send, an application menu, a tray, dialogs, scoped file reads and writes, a notification, a kv settings store and a second window. krate native test drives the port through the real shell.

Known limits:

- The Electron original is a reference and is not built or run in CI.
- Native dialogs and notification banners cannot be clicked headlessly: the test calls the code behind the dialogs and accepts 'sent' or 'unsupported' for the notification and tray (Linux without a notification service or tray host answers unsupported).
- The macOS and Windows runs happen only in the CI native job.

Evidence: `TestNativeFromElectronGUI`, `fixture:examples/native-from-electron/tests/port.native.test.ts`

### Native: plugins

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

A plugin is a Go package plus a krate-plugin.json (name, Go import path, permissions with descriptions, ops, minimum Krate). native.plugins enables local directories, npm packages and Go modules; ops are callable as plugin/<name>.<op> (lowerCamel op names like the SDK ids, bound to Go names by capitalizing or an explicit go field) with generated codecs and a typed krate/plugin/<name> TypeScript module (calls, streams, signals). Plugins run inside the shell process with a context (lifecycle hooks, Dispatch, platform capabilities, data directory), cannot register ids outside their namespace, and their permissions (plugin:<name>.<permission>) are derived from the code, enforced per window, listed by krate native check and shaken per op in release builds. An app with plugins runs under a per-app shell built with go (cached by plugin set, sources and Krate version) in dev and in builds; krate native plugin init scaffolds one.

Known limits:

- The plugin runtime and the build are exercised on all three OSes: the shell GUI test (TestGUIPlugins), the compiler's plugin tests (the CI native job runs -run Plugin on macos-latest and windows-latest, including TestNativePluginDevBuild and TestNativePluginGUI), which build a per-app shell and check a granted call, a call denied to a window scoped to nothing and the lifecycle hooks, and TestNativeE2EPluginGUI, which runs examples/native-plugin in a window. The system-info example's own tests run on all three; its macOS CPU load (ps) and Windows (GetSystemTimes) readings are checked for shape, not for accuracy.
- Plugins are trusted Go code in the shell process; the namespace and permissions limit what a page can reach through them, not what the plugin does.
- Plugin ops are callable from pages; TypeScript host code reaches them only by id (__krateHost.call). A go: plugin needs network or a populated module cache for the first build.
- Editing a local plugin's Go code restarts native dev instead of hot-reloading it.

Evidence: `TestInstallEnforcesNamespace`, `TestInstallRegistersGatedOps`, `TestSignalGate`, `TestLifecycle`, `TestParseManifestErrors`, `TestResolveDiagnostics`, `TestNativePluginsKey`, `TestPluginNamedImports`, `TestNativePluginDevBuild`, `TestNativePluginReleaseShakesOps`, `TestNativePluginReleaseWithGoHostCode`, `TestNativePluginScaffoldBuilds`, `TestNativePluginInit`, `TestGoRequiredForPlugins`, `TestGUIPlugins`, `TestNativePluginGUI`

### Native: packaging formats

**Linux:** Partial - **macOS:** Supported - **Windows:** Supported

krate native package builds .tar.gz, .deb, .rpm and AppImage on Linux, .app, .zip and .dmg on macOS, and .zip, NSIS installer and MSIX on Windows, from a release executable or the prebuilt-shell layout, with icons, desktop entries and update files. The Windows installer checks for the WebView2 Runtime and, by native.bundle.windows.webview2, downloads or embeds Microsoft's bootstrapper or ships a Fixed Version runtime folder; the app shows a message box with the download link when the runtime is missing.

Known limits:

- On Linux the .deb, .rpm and AppImage of examples/native-tasks are built, and the .deb contents and AppImage were launched and checked to serve the first page (scripts/native-smoke.mjs; the CI native job installs the .deb with dpkg and runs the AppImage). The .rpm is checked for structure and not installed.
- macOS: the CI native job builds the .app, .zip and .dmg of examples/native-tasks, verifies and mounts the .dmg, copies the app out, checks its signature (ad hoc) and launches it (version, first page, window open for 8 seconds). Windows: it builds the .zip, the NSIS installer and the MSIX, runs the NSIS installer silently (registry entry, launch, uninstall) and installs the MSIX with Add-AppxPackage, signed with a test certificate. Dragging the .dmg to Applications, the installer UI and Start menu entries are manual checks; the NSIS installer needs makensis.
- arm64 packages are built for linux/arm64, windows/arm64 and darwin/arm64 (deb and rpm architecture, MSIX ProcessorArchitecture, AppImage ARCH, Windows resources) and checked by unit tests; the CI native job also runs on ubuntu-24.04-arm and windows-11-arm, where the packaging steps run natively. Those two jobs have not run yet, and makensis, the Windows SDK and appimagetool may be missing on the Windows on ARM image (each is skipped with a notice, not failed).
- WebView2: the registry check, the stand-in bootstrapper run and the message box are exercised by CI steps (scripts/native-smoke.mjs windows webview2, TestGUIRuntimeMissingMessageBox) that have not run on a real runner yet. The Fixed Version runtime loader path uses the runtime type value from open reimplementations of the loader and is not confirmed. An MSIX cannot declare the Evergreen runtime as a dependency (it is not a framework package), so an MSIX app relies on the runtime being present (Windows 11, current Edge) and on the message box.

Evidence: `TestLinuxTarGzAndDeb`, `TestRPM`, `TestAppImageNeedsTool`, `TestMacAppAndZip`, `TestMacDMGUsesHdiutilOnlyOnMac`, `TestWindowsZipAndNSIS`, `TestMSIXStructure`, `TestNativePackageEndToEnd`, `TestUpdateArtifactsAreSignedAndFeedWritten`, `TestNSISWebView2Download`, `TestNSISWebView2Embed`, `TestNSISWebView2NoneAndFixed`, `TestNSISRegistersTheToastScheme`, `TestMSIXDeclaresTheToastScheme`, `TestAddWebView2`, `TestGUIRuntimeMissingMessageBox`

### Native: signing and notarization

**Linux:** Partial - **macOS:** Partial - **Windows:** Partial

krate native sign and notarize drive codesign and notarytool on macOS, signtool or osslsigncode for Windows, and gpg for Linux packages, and rebuild the archives around the signed payload. CI signs for real with throwaway keys (gpg on Linux, a self-signed identity in a temporary keychain and an ad hoc signature on macOS, a self-signed code-signing certificate on Windows) and verifies with gpg --verify, codesign --verify --deep --strict, signtool verify /pa and Get-AuthenticodeSignature.

Known limits:

- Unit tests replace the signing tools with stand-ins (command lines, ordering and secret redaction). The CI native job also signs for real with throwaway certificates (scripts/native-smoke.mjs): macOS ad hoc and with a self-signed identity in a temporary keychain (codesign --verify --deep --strict), Windows with a self-signed Authenticode certificate (signtool verify /pa), Linux packages with a generated gpg key (gpg --verify, and a tampered copy must fail). Ad hoc signing and the Windows test certificate ran green on macos-latest and windows-latest; the temporary-keychain identity, signtool verify /pa and the gpg step are new and their first CI run is pending.
- Notarization needs an Apple Developer account and stays manual, as do Developer ID identities, certificates from a CA (EV or OV Authenticode), timestamps from a paid authority and hardware or cloud signing services.

Evidence: `TestSignMacAppDeepSignsInnerFirst`, `TestSignMacAdHocAndEntitlements`, `TestSignMacOnlyOnMac`, `TestNotarize`, `TestSignWindows`, `TestSignLinuxGPG`, `TestPackageSignsBeforeArchiving`, `TestRedactCommand`, `TestSignLinuxArtifactsFindsEveryPackage`, `ci:native-smoke-sign`

### Native: macOS universal apps

**Linux:** Supported - **macOS:** Untested - **Windows:** Supported

krate native build --release --arch universal compiles darwin/arm64 and darwin/amd64 and package merges them in pure Go into one .app: the executable and every shared Mach-O file (Go host child, compiled Bun and Deno hosts, Node single executables) become fat binaries with FAT_MAGIC, one fat_arch per slice and 2^14 alignment. The update target is darwin-universal (zip, latest-darwin-universal.json), which the running app learns from a krate-dist.json marker. The Linux and Windows columns mean the merge runs there (a cross build from any OS).

Known limits:

- The fat Mach-O is written by Krate and read back with debug/macho on every OS (TestNativeUniversalMacApp merges real cross-compiled Go binaries). Running both slices (native, and x86_64 under Rosetta) and lipo -info are checked by that test on macOS only when KRATE_NATIVE_GUI=1 and lipo exist; that CI run is pending, so macOS stays untested.
- A Node host needs a node binary per architecture (KRATE_NODE_DARWIN_ARM64 / KRATE_NODE_DARWIN_AMD64); Bun and Deno cross-compile by themselves. The prebuilt-shell layout holds one platform, so a universal app needs the compiled-executable layout.

Evidence: `TestWriteFatLayout`, `TestWriteFatRejects`, `TestMergeUniversalMergesMachOAndChecksTheRest`, `TestMergeUniversalDiagnostics`, `TestPackageUniversalMacApp`, `TestUniversalUpdateFeedTarget`, `TestNativeUniversalMacApp`, `TestNativeBuildUniversalCLI`, `TestResolveDistTargets`, `TestDetect`

### Native: Flatpak

**Linux:** Partial - **macOS:** Planned - **Windows:** Planned

krate native package --format flatpak writes a flatpak-builder manifest on org.gnome.Platform (WebKitGTK 4.1) with finish-args derived from the app's permissions, a desktop file, AppStream metainfo (validated with appstreamcli when installed), icons named by the app id and a Flathub manifest that builds from a source archive with a sha256. With flatpak-builder installed it builds a .flatpak bundle. Inside the sandbox the shell opens links and sends notifications through the portals, uses the file chooser portal (GTK_USE_PORTAL), names its tray item below its own id and keeps data in the sandbox XDG directories.

Known limits:

- The manifest, metainfo and portal calls are tested without Flatpak (command lines, files, a fake D-Bus). Building the bundle and running the app inside a real sandbox needs flatpak-builder and the GNOME runtime and has not been done; the first CI run of that step is pending. appstreamcli validation of the generated metainfo has not run on any machine yet.
- Flathub prefers apps built from source; a prebuilt-binary submission is reviewed case by case, and screenshots must be added to the metainfo by hand.

Evidence: `TestFlatpakFinishArgsFollowPermissions`, `TestFlatpakManifestShape`, `TestAppStreamMetainfo`, `TestPackageFlatpakWithoutTools`, `TestPackageFlatpakBuildsBundle`, `TestPackageFlatpakFailures`, `TestNativePackageFlatpakSnapCLI`, `TestOpenURICallsThePortal`, `TestPortalBackendSendsAndRoutesActions`, `TestItemBusName`, `TestDirsFollowSandboxXDG`, `TestUpdaterRefusesInsideAStore`

### Native: Snap

**Linux:** Partial - **macOS:** Planned - **Windows:** Planned

krate native package --format snap writes snap/snapcraft.yaml (core24, gnome extension, libwebkit2gtk-4.1-0 staged, plugs derived from the app's permissions) with the app and icon, and builds the .snap with snapcraft when it is installed. Inside a snap the shell uses the OpenURI and notification portals, GTK_USE_PORTAL and the snap's XDG directories, and has no self-updater.

Known limits:

- The snapcraft.yaml is checked as text; building it with snapcraft and running the snap under strict confinement has not been done. The exact plugs a WebKitGTK app needs under strict confinement (for example for the tray) are unconfirmed.

Evidence: `TestSnapcraftYAML`, `TestSnapName`, `TestPackageSnap`, `TestNativePackageFlatpakSnapCLI`, `TestDetect`

### Native: Mac App Store

**Linux:** Planned - **macOS:** Untested - **Windows:** Planned

krate native package --target mas builds a sandboxed .app: com.apple.security.app-sandbox with network.client, user-selected files, camera, microphone and location entitlements derived from the app, inherit-only entitlements for helper executables, the embedded provisioning profile (checked against TEAMID.appId), codesign with a 3rd Party Mac Developer Application or Apple Distribution identity (ad hoc without one) and a productbuild .pkg signed with the installer identity. native.updater is removed and the running sandboxed app refuses updater calls.

Known limits:

- The entitlements, Info.plist, tool command lines and failure cases are unit tested on any OS. TestNativeMASSandboxGUI signs the example ad hoc with the sandbox entitlement and runs its GUI flows inside the container on macOS; that run is pending. Uploading to App Store Connect, a real distribution identity and App Store validation have not been tried.
- Access to a file the user picked lasts for that run; security-scoped bookmarks are not used.

Evidence: `TestMASEntitlementsFollowPermissions`, `TestEntitlementsPlistIsWellFormed`, `TestStripUpdaterJSON`, `TestPackageMASAdHocSignsWithSandbox`, `TestPackageMASRealIdentity`, `TestPackageMASRejects`, `TestPackageMASOffMacKeepsApp`, `TestNativeMASSandboxGUI`, `TestShortSocketDir`

### Native: Microsoft Store

**Linux:** Supported - **macOS:** Supported - **Windows:** Untested

krate native package --format msix-store builds the Store variant of the MSIX with the Partner Center identity (name, publisher id, publisher display name), no self-updater and a static check of the full-trust rules, plus a .msixupload; --arch universal --target windows/amd64 bundles x64 and arm64 into an .msixbundle (AppxBundleManifest.xml, block map, uncompressed packages) and its .msixupload. The Linux and macOS columns mean the package is built and checked there.

Known limits:

- The bundle manifest offsets are consistent with the zip layout (tested), but neither makeappx nor the Windows App Certification Kit nor Partner Center has opened a package; the Windows column stays untested until a runner does (makeappx unbundle, Add-AppxPackage of a signed copy).

Evidence: `TestStoreManifestUsesPartnerCenterIdentity`, `TestPackageStoreMSIX`, `TestStoreMetaValidation`, `TestCheckStoreMSIXFindsProblems`, `TestPackageStoreBundle`

### Native: updater

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

In-app updates: an Ed25519-signed feed, resumable verified downloads, signed delta updates (pure Go bsdiff patches for the install tree, macOS bundle and AppImage, with fallback to the full file), staged rollouts with halt/critical/min_version, channels, and an apply step that replaces the executable and app directory (Linux, AppImage, macOS bundle, Windows ordering) with rollback on failure and rollback of a version that fails its first start.

Known limits:

- The macOS bundle and Windows apply, delta and rollback steps are file-system logic tested on Linux (the delta and rollback tests run the macOS bundle and Windows wrapper layouts with GOOS overridden); replacing a running Windows executable has not been tried on Windows. scripts/native-smoke.mjs runs, for each OS, a real update 1.0.0 to 1.1.0 of an installed packaged app (TestSmokeUpdateInstalledApp, which must now be a delta), a confirmed healthy start, and a 1.1.0 build whose host exits early being rolled back on its second launch (TestSmokeRollbackState). The full update passed in the CI native job on Linux, macOS and Windows before deltas and rollback were added; the delta and rollback flows have run on Linux only so far.
- First-start protection does not see a crash before the shell's start-up check runs (a loader error, a panic in a package init), and the first update from a version built without it is not protected. An installer-based install (NSIS, MSIX, deb, rpm) does not take delta updates or rollbacks: the updater replaces an install directory, a bundle or an AppImage.

Evidence: `TestSignatureGoodBadWrongKey`, `TestCheck`, `TestDownloadVerifiesAndProgress`, `TestDownloadResumes`, `TestApplyLinuxReplacesExecutableAndAppDir`, `TestApplyRollsBackOnFailure`, `TestApplyAppImage`, `TestApplyMacBundle`, `TestApplyWindowsOrder`, `TestSmokeUpdateInstalledApp`, `TestDeltaUpdateEveryKind`, `TestDeltaFallsBackToFull`, `TestPatchRejectsHostilePaths`, `TestPatchSignatureBindsMeta`, `TestBsdiffRoundTrip`, `TestBucketIsStableAndSpread`, `TestRolloutIsMonotonic`, `TestCheckGatesOnRollout`, `TestDecideBoot`, `TestRollbackAfterFailedFirstStart`, `TestWatchdogTimeoutMakesNextLaunchRollBack`, `TestBootRollsBackAfterUnconfirmedStart`, `TestPackageWritesPatchesAndClientAppliesThem`, `TestSmokeRollbackState`

### Native: app icon badge and progress

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

krate/badge.* sets a badge (count or short text), a progress value (none, normal, indeterminate, error, paused) and an urgent flag on the app icon. Linux emits the Unity LauncherEntry D-Bus signal that GNOME Dash to Dock, KDE and Plank read; macOS uses NSDockTile with a progress bar in the tile; Windows uses ITaskbarList3 progress and an overlay icon the SDK draws.

Known limits:

- Linux launchers show numbers only (other text is refused as unsupported) and no indeterminate state; the signal was verified with a listener on a private D-Bus session, not in a real dock.
- macOS draws error and paused as normal progress.
- macOS and Windows: the same Go code is built for them and the CI GUI tests TestGUIBadgeAndProgress set and read back what the OS allows; untested until CI has run them.

Evidence: `TestBadgeLauncherEntryOverDBus`, `TestBadgeWithoutSessionBus`, `TestProgressValidate`, `TestBadgeImage`, `TestBGRAPremultiplied`, `TestTaskbarFlags`, `TestGUIBadgeAndProgress`

### Native: displays

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

krate/screen.* lists displays (bounds, work area, scale, primary, refresh rate), finds the display of a window, centers a window on a display and reports display changes. GDK monitors on Linux, NSScreen on macOS, EnumDisplayMonitors and GetDpiForMonitor on Windows.

Known limits:

- Linux was run against Xvfb, which has one monitor; multi-monitor layouts are covered by the pure geometry tests, not a real second monitor.
- Display change events are wired to GDK monitors-changed, NSApplicationDidChangeScreenParametersNotification and WM_DISPLAYCHANGE; only the Windows message path is driven by a test (TestGUIDisplays).
- macOS and Windows: untested until CI has run the GUI tests.

Evidence: `TestGUIDisplays`, `TestScreenOps`, `TestScreenPermissionAndUnsupported`, `TestDisplayForRect`, `TestCenterIn`, `TestTopLeftY`, `TestDisplayHelpers`

### Native: power, idle and sleep blockers

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

krate/power.* reports on-battery, idle time and lock state, delivers suspend, resume, lock, unlock, battery, AC, idle and active events, and holds sleep blockers. Linux uses logind, UPower, Mutter IdleMonitor or the ScreenSaver service, and the GNOME session manager or PowerManagement inhibitors; macOS NSWorkspace, IOKit and IOPMAssertion; Windows WM_POWERBROADCAST, WTS session notifications and SetThreadExecutionState.

Known limits:

- Linux was tested against fake services on private buses, never a real suspend or lock.
- logind's Inhibit returns a file descriptor, which the SDK's D-Bus client cannot receive, so it is not used.
- Battery and idle events come from sampling every three seconds while an event stream is open.
- macOS and Windows: untested until CI has run the GUI tests; a real suspend was never tried.

Evidence: `TestSampleFromServices`, `TestIdleFallsBackToLogind`, `TestBatteryFromSysfsWhenNoUPower`, `TestEventsFromLogindAndPolling`, `TestPreventSleepGnome`, `TestPreventSleepKDE`, `TestPreventSleepWithoutAnyService`, `TestTrackerBatteryEvents`, `TestTrackerIdleEvents`, `TestPowerThroughTheHost`, `TestGUIPowerMessages`

### Native: launch at login

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

krate/login.get and set register the app to start at login: an XDG autostart entry on Linux, SMAppService with a LaunchAgent fallback on macOS, the HKCU Run key on Windows.

Known limits:

- A packaged (MSIX) Windows app declares a startupTask extension instead; the calls answer unsupported because toggling a StartupTask from code is not implemented.
- macOS: SMAppService needs a signed bundle and was never run; the LaunchAgent fallback writes the plist but does not call launchctl, so it takes effect at the next login.
- macOS and Windows: untested until CI has run the temp-HOME and temp-registry-key round trips.

Evidence: `TestAutostartRoundTrip`, `TestAutostartNeedsAppID`, `TestAutostartEntry`, `TestLoginThroughTheHost`, `TestLaunchAgentRoundTrip`, `TestRunKeyRoundTrip`, `TestGUILoginItemAnswersCleanly`

### Native: system theme

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

krate/theme.* reads the system appearance (light or dark, high contrast), reports changes and makes native title bars follow it or a fixed choice: the desktop portal and gtk-application-prefer-dark-theme on Linux, NSApp.appearance on macOS, the Personalize registry key and DWMWA_USE_IMMERSIVE_DARK_MODE on Windows.

Known limits:

- Linux was tested against a fake portal and by reading back the GTK setting; whether a given desktop redraws its title bars is up to the window manager (client-side GTK decorations follow it).
- macOS observes AppleInterfaceThemeChangedNotification, not KVO on effectiveAppearance.
- macOS and Windows: untested until CI has run the GUI tests.

Evidence: `TestThemeFollowsThePortal`, `TestThemeFallsBackToGTKSettings`, `TestGUIWindowTheme`, `TestGUIThemeFollowsOverride`, `TestGUIThemeFollowsSettings`

### Native: crash reports

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

A Go panic or fatal error in the shell or the Go host is written to a JSON report under the app's log directory (runtime/debug.SetCrashOutput, collected on the next launch), and a crashed or killed webview process is reported as an event and a report; krate/crash.* lists, reads and deletes reports, and can reload a crashed view automatically (at most three times in 30 seconds). WebKitGTK web-process-terminated, WKWebView webViewWebContentProcessDidTerminate: and WebView2 ProcessFailed.

Known limits:

- A crash of the shell itself is reported on the next launch, not live.
- Nothing is uploaded; the app reads the reports and sends them where it likes.
- WKWebView does not say why its process ended, so macOS reports the reason as crashed.
- macOS and Windows: untested until CI has run the forced-crash tests.

Evidence: `TestRealCrashBecomesAReport`, `TestCollectSkipsLiveProcessesAndEmptyLogs`, `TestWriteListReadDelete`, `TestIDsCannotEscapeTheDirectory`, `TestLimiter`, `TestWebviewCrashEventsReportsAndAutoReload`, `TestGUIWebviewCrash`, `TestNativeE2ESystemGUI`

### Native: size, start-up and memory budgets

**Linux:** Supported - **macOS:** Planned - **Windows:** Planned

packages/native/budgets.json limits the stripped shell, Go-only, TypeScript-only and example release executables (bytes), the cold start of the example in a real window (ms) and its resident memory (KB). Limits are the measured value plus 25 percent. Any platform can have entries (linux-amd64 today); one with entries is enforced by the same measured-plus-headroom rule, one without only reports and prints a paste-ready BUDGET-RECORD block.

Known limits:

- Budgets exist for linux-amd64 only; the start-up limits were measured on a developer machine and CI applies KRATE_BUDGET_SCALE=3 until a hosted runner has been measured.
- darwin-arm64, windows-amd64, windows-arm64 and linux-arm64 run the same size, cold-start and memory tests in CI and report their measured values (BUDGET-RECORD-BEGIN/END in the log and the job summary) but have no recorded entries, so nothing is enforced there yet; the numbers have to be copied into packages/native/budgets.json after a review.

Evidence: `TestNativeBudgetSizes`, `TestNativeBudgetGUI`, `TestBudgetVerdictPerPlatform`, `TestEvalMemoryEnforcesRecordedEntries`, `TestBudgetRecordTextIsPasteable`

### Native: custom title bars, drag regions and transparency

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

native.window.titleBar hidden or none removes the system title bar; the page marks drag regions with data-krate-drag, -webkit-app-region or app-region (read from its style sheets, or by WebView2 itself) and a press starts the native move, a double press maximizes, and frameless windows keep resize edges (GTK draws them in the page, Windows hit-tests them, AppKit keeps its own). The window and webview can be transparent. The request that starts a move names no window: the shell finds it from the webview that sent it.

Known limits:

- Linux: the drag and resize tests press a real pointer with XTEST under openbox on X11; Wayland compositors decide where a dragged window goes and are untested. Transparency needs an RGBA visual and, to show the desktop, a compositing window manager.
- macOS and Windows: untested. The code compiles for them, the hit test, colors and style arithmetic are unit-tested on every OS, and GUI tests that read AppKit and Win32 state back run in the CI native job; nothing here has run on a Mac or a Windows PC by hand.
- Windows draws the caption buttons of a hidden title bar itself, over the extended frame, so the page must leave that corner clear; the WebView2 non-client region support needs a recent runtime and falls back to the page script without it.
- CSS regions are read from style sheets as text: nesting and @import are not followed, and the nearest marked ancestor wins rather than CSS specificity.

Evidence: `TestHandlerOps`, `TestHandlerNeedsAWindowWithChrome`, `TestInfo`, `TestWanted`, `TestScriptParsesAppRegionRules`, `TestHitTest`, `TestParseColor`, `TestChromeOptionsValidate`, `TestGUIChromeTitleBarStyles`, `TestGUIChromeRegionsFromScript`, `TestGUIChromeDragMovesTheWindow`, `TestGUIChromeDoublePressTogglesMaximize`, `TestGUIChromeEdgeResize`, `TestGUIChromeTransparent`, `TestNativeE2EChromeGUI`, `TestGUIChromeHiddenTitleBarHitTest`, `TestGUIChromeNoTitleBarHasNoButtons`, `TestGUIChromeClientAreaFillsTheWindow`, `TestGUIChromeNonClientRegionSupport`, `TestGUIChromeHiddenTitleBar`, `TestGUIChromeNoTitleBarHidesTheButtons`, `TestManifestChrome`, `TestNativeWindowChrome`, `TestChromeWindowOpenOptions`, `fixture:examples/native-chrome/src/pages/index.tsx`

### Native: Mica, Acrylic, vibrancy, caption overlay and traffic lights

**Linux:** n/a - **macOS:** Untested - **Windows:** Untested

Windows 11 22H2 system backdrops (Mica, Mica Alt, Acrylic) behind a transparent webview, the caption buttons Windows draws over a hidden title bar with Snap Layouts on the maximize button (WM_NCHITTEST returns HTMAXBUTTON) and their colors, and on macOS an NSVisualEffectView with any current material behind a transparent WKWebView and a movable traffic light position. Calls the platform lacks reject with unsupported.

Known limits:

- macOS and Windows: untested; see native-chrome-frame. The WebView2 vtables for ICoreWebView2Controller2 and ICoreWebView2Settings9 are generated from WebView2.h and checked against it.
- Backdrops need Windows 11 22H2 (build 22621); earlier builds reject them. A backdrop shows only where the page is transparent.
- Vibrancy attaches the effect view to the window's frame view, below the content view, which AppKit allows but does not document.

Evidence: `TestBackdropAndVibrancyTables`, `TestOverlayNormalize`, `TestBackdropAndCaptionColorBuilds`, `TestColorPacking`, `TestFrameMargins`, `TestNCHitInput`, `TestChromeVtables`, `TestGUIChromeBackdrop`, `TestGUIChromeTransparentWebView`, `TestGUIChromeOverlayNeedsHiddenTitleBar`, `TestGUIChromeVibrancy`, `TestGUIChromeVibrancyAtCreation`, `TestGUIChromeHiddenTitleBar`, `TestGUIChromeUnsupportedOnLinux`, `TestChromeUnsupportedWithoutPlatformSupport`

### Native: tabs, sheets, document state, share menu, dock, thumbnail buttons and jump lists

**Linux:** n/a - **macOS:** Untested - **Windows:** Untested

macOS: document-edited and represented-file title bar state, windows as sheets, native window tabs with merge, move and new-tab events, the share picker for text, URLs and files, and menu-bar-only apps (activation policy at run time, LSUIElement in Info.plist from native.bundle.macos.agent, dock icon show and hide). Windows: taskbar thumbnail toolbar buttons with click events and jump list tasks that relaunch the app with arguments.

Known limits:

- macOS and Windows: untested; see native-chrome-frame. The macOS tab test skips when the CI session does not merge windows into tabs, and the share picker is validated but not shown in CI.
- Thumbnail buttons need Explorer to create the taskbar button; without a shell session they stay pending. The jump list is committed through ICustomDestinationList and checked by its return codes, not by reading the shell's list back.
- A new-tab event is delivered through the window delegate's newWindowForTab: action, the way AppKit sends it from the tab bar's plus button.

Evidence: `TestInfoPlistAgent`, `TestMetadataAgentFromConfig`, `TestValidateThumbButtons`, `TestValidateJumpTasks`, `TestQuoteArgs`, `TestQuoteArgsRoundTrip`, `TestShareItemsValidate`, `TestThumbButtonLayout`, `TestMakeThumb`, `TestThumbIDs`, `TestSplitCommand`, `TestChromeSheetsTabsThumbs`, `TestChromeAppLevel`, `TestChromeEvents`, `TestChromeRejectsBadArguments`, `TestChromePermissions`, `TestGUIChromeDocumentState`, `TestGUIChromeTabbing`, `TestGUIChromeSheet`, `TestGUIChromeActivationPolicy`, `TestGUIChromeShareValidates`, `TestGUIChromeThumbButtons`, `TestGUIChromeJumpList`

### Native: worker processes

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Background worker processes declared in native.workers (a TypeScript module on Node, Bun or Deno, a Go main package, or a prebuilt executable) are started by name from a page or host code, with their own SDK endpoint whose permissions are narrowed per worker, stdout and stderr in the app log, a restart policy with backoff, crash and restart events, and two-way messages over the wire codec (binary, Date, Map, Set). The supervisor and policy are pure Go and tested on every OS; the shell tests run real Node and Go workers.

Known limits:

- macOS and Windows compile and share the portable supervisor and the host child transport (loopback TCP with a token for JavaScript workers on Windows); the worker tests run on Linux only.
- A release build ships worker scripts under host/workers next to the executable and needs Node, Bun or Deno on the user's machine for a TypeScript worker; a Go worker is compiled for the build machine's platform only.
- Permissions narrow the SDK ids a worker can call; a worker is still an ordinary process with the user's file and network access.

Evidence: `TestStartUnknownWorker`, `TestCrashRestartsOnFailure`, `TestNeverPolicyStaysStopped`, `TestGivesUpAfterMax`, `TestFailedRestartStartsAreRetried`, `TestMessagesBothWays`, `TestDecide`, `TestSpecValidate`, `TestWorkersNode`, `TestWorkerGo`, `TestParseManifestWorkers`, `TestNativeE2EProcsGUI`, `fixture:examples/native-procs/src/workers/echo.ts`

### Native: message ports between views, windows and workers

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

A channel has two ends; whoever creates it can hand each end to a window, view or worker, and messages (JSON, binary, Date, Map, Set) then flow directly between the ends without host code relaying them, in order, with a bounded queue (1024 messages, 16 MiB) and backpressure errors. An end can only be handed to a scope that has port:receive. The page object is a Port in @krate/app/port backed by the shell.

Known limits:

- The engines give two top-level webviews no way to share a real MessagePort, so a port is a shell-backed object, not a transferable MessagePort; inside one page the browser's MessageChannel is untouched.
- The pure broker is tested on every OS; the page-to-page and page-to-worker round trips run on Linux.

Evidence: `TestPostIsDeliveredInOrderToTheOtherEnd`, `TestOnlyTheHolderMayUseAnEnd`, `TestTransferNeedsAReceiver`, `TestQueuedMessagesFollowTheEnd`, `TestTransferEndsTheOldListener`, `TestCloseDrainsThenReportsClosed`, `TestBackpressureAndLimits`, `TestDropOwnerClosesItsChannels`, `TestWorkersNode`, `TestNativeE2EProcsGUI`, `fixture:examples/native-procs/src/pages/peer.tsx`

### Native: offscreen rendering

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

No system webview streams offscreen frames. The closest equivalent: a window that is never shown (a GtkOffscreenWindow on Linux, a window parked off every display on Windows and macOS) whose page renders normally, plus snapshots (webkit_web_view_get_snapshot, WKWebView takeSnapshot, WebView2 CapturePreview) delivered as PNG or raw premultiplied BGRA, one at a time at a requested rate up to 60 per second, with dropped frames counted instead of queued.

Known limits:

- This is polling, not a paint callback: frames are snapshots, so the rate depends on page complexity and engine; the measured Linux numbers are in docs native/processes.
- Windows and macOS are written and compiled; their capture and parked-window code runs only in the CI native job.
- An open offscreen window counts as a window: close it, or the app does not quit when the last visible window closes.

Evidence: `TestGUIOffscreenSnapshot`, `TestGUISnapshotOfAMappedWindowAndErrors`, `TestEncodePNGKeepsColorsAndUnpremultiplies`, `TestRawBGRARemovesRowPadding`, `TestPacing`, `TestStreamPacesAndCountsDroppedFrames`, `TestStreamStopsWithTheContextAndReportsCaptureErrors`, `TestSnapshotFromPNG`, `TestNativeE2EProcsGUI`

### Native: remote debugging and automation

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

An endpoint for tests and debugging, on in dev and with native.debug and never in a release build otherwise: WebView2 serves the Chrome DevTools Protocol (Playwright connectOverCDP, Puppeteer connect) and krate can send one CDP method or subscribe to an event in process; WebKitGTK serves the WebKit remote inspector over HTTP (WEBKIT_INSPECTOR_HTTP_SERVER), which is not CDP; WKWebView is inspectable from Safari's Develop menu. CDP answers unsupported where the engine does not speak it.

Known limits:

- Playwright and Puppeteer attach only to WebView2; they cannot drive WebKitGTK or WKWebView. Microsoft documents --remote-debugging-port with Edge WebDriver's DebuggerAddress for WebView2; attaching Playwright through connectOverCDP to the same port is standard Chromium CDP but was not run here.
- The WebView2 CDP command and event code is checked against WebView2.h and compiled; it runs only in the CI native job.

Evidence: `TestGUIOffscreenSnapshot`, `TestNativeE2EProcsGUI`, `TestProcsVtableSlots`

### Native: browser extensions

**Linux:** n/a - **macOS:** Untested - **Windows:** Untested

native.extensions packages unpacked extension folders loaded at start and by name: WebView2 through ICoreWebView2Profile7 AddBrowserExtension with AreBrowserExtensionsEnabled, WKWebView through WKWebExtensionController on macOS 15.4 and later. WebKitGTK has no extension API and answers unsupported.

Known limits:

- Linux is unsupported by the engine.
- WebView2 and macOS are written and compiled and never run by the author. On macOS the API's match patterns cover http, https and file pages, so a content script is not expected to run on the app's own krate:// pages.

Evidence: `TestProcsVtableSlots`, `TestNativeE2EProcsGUI`

### Native: accessibility of the shell's own UI

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

The UI the shell draws or configures is checked through each platform's accessibility API: window names in every title bar style, the webview reachable in native, hidden, frameless, transparent, split and remote windows, menus and context menus (roles, names, checked, disabled, accelerators), message and file dialogs, tray, notification, thumbnail button and jump list names, split-pane dividers, and offscreen windows kept out of the tree. Fixes: offscreen windows hidden (Linux AT-SPI, macOS AX), message dialog titles reach the accessible name, split panes and dividers named, tray and notification action and thumbnail button names never empty, Windows message box labels spelled out in the text, macOS alert default button, and drag regions no longer swallow presses on ARIA widgets and focusable elements.

Known limits:

- Linux is checked through AT-SPI on X11 with a private accessibility bus and keys synthesized by at-spi2-registryd (needs the at-spi2-core package and a window manager); Wayland and a real screen reader were not used.
- macOS is checked in process through NSAccessibility (no permission needed); the AX API tests skip unless the test process is trusted for accessibility, which CI runners are not. Written and compiled, run only in the CI native job.
- Windows is checked through UI Automation; written and compiled, run only in the CI native job. The caption buttons of a hidden title bar are reported, not required; the notification area, taskbar thumbnails and jump lists are not read by tests (names come from unit-tested helpers).
- Windows: split-pane dividers are mouse-only, frameless windows have no system menu, offscreen windows stay in the UI Automation tree marked offscreen, and a two-button message box cannot be closed with Esc.
- macOS: an alert is dismissed with Esc only through a button titled Cancel; the split divider is reachable by VoiceOver but not by a plain keyboard.
- No screen reader (Orca, VoiceOver, Narrator, NVDA, JAWS) was used on any platform; web content inside the webview is exposed by the engine and not tested here.

Evidence: `TestWindowIsNamedByItsTitle`, `TestWebviewReachableInEveryWindowConfiguration`, `TestOffscreenWindowIsNotExposed`, `TestAppMenuTree`, `TestAppMenuKeyboardAccess`, `TestContextMenuTreeAndEscape`, `TestMessageDialogTree`, `TestMessageDialogEscapeAndTabOrder`, `TestFileDialogIsNamedAndClosesWithEscape`, `TestSplitPanesAreReachableAndNamed`, `TestSplitDividerResizesFromTheKeyboard`, `TestFocusTraversalVisitsEveryView`, `TestRemoteViewIsReachable`, `TestItemPropsAlwaysNameTheIcon`, `TestMenuItemsCarryLabelsStatesAndShortcuts`, `TestTrayLabel`, `TestThumbButtonName`, `TestActionLabel`, `TestDragRegionLeavesControlsAlone`, `TestMessageBoxTextNamesTheMapping`, `TestGUIAccessibilityWindowIsNamedInEveryChrome`, `TestGUIAccessibilityOffscreenWindowIsHidden`, `TestGUIAccessibilityMenuBar`, `TestGUIAccessibilityTrayButtonIsNamed`, `TestGUIAccessibilitySplitViewRole`, `TestGUIAccessibilityAXWindows`, `TestGUIUIAWindowAndWebviewInEveryChrome`, `TestGUIUIAMenuBarIsNamedAndReachable`, `TestGUIUIAMessageBoxIsNamedAndShowsTheMapping`, `TestGUIUIAPopupMenuItemsAndEscape`

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
