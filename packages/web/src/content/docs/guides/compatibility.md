---
title: Compatibility Matrix
order: 6
description: The public Krate compatibility matrix for React source components, Preact islands, SSR, the browser conformance corpus, and Krate Native per operating system.
---

# Compatibility Matrix

This is the support contract for React compatibility. A surface is only marked
supported or stable when the referenced tests pass. Known divergences are
listed instead of silently hidden.

Last verified: **2026-11-11**. See the [roadmap](/roadmap/) for phase
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
| Native: window and custom scheme | Supported | Untested | Untested | Krate Native v1 |
| Native: streaming responses | Supported | Untested | Untested | Krate Native v1 |
| Native: host calls, streams and signals | Supported | Untested | Untested | Krate Native v1 |
| Native: Go host code | Supported | Untested | Untested | Krate Native v1 |
| Native: TypeScript host code on QuickJS | Supported | Untested | Untested | Krate Native v1 |
| Native: Node, Bun and Deno host children | Partial | Untested | Untested | Krate Native v1 |
| Native: SDK data areas (kv, db, fs, net, os) | Supported | Untested | Untested | Krate Native v1 |
| Native: SDK windows | Partial | Untested | Untested | Krate Native v1 |
| Native: requests are bound to the webview they came from | Supported | Untested | Untested | Krate Native v1 |
| Native: views and split panes | Supported | Supported | Supported | Krate Native v1 |
| Native: shared web process, release and background mode | Supported | Untested | Untested | Krate Native v1 |
| Native: lean pages and the dev Memory panel | Supported | Untested | Untested | Krate Native v1 |
| Native: SDK dialogs, clipboard and menus | Supported | Untested | Untested | Krate Native v1 |
| Native: SDK notifications | Supported | Untested | Untested | Krate Native v1 |
| Native: camera, microphone, screen capture and location permissions | Supported | Untested | Untested | Krate Native v1 |
| Native: media permissions derived from the UI, usage descriptions and capabilities | Supported | Untested | Untested | Krate Native v1 |
| Native: operating system consent for camera, microphone, screen and location | Partial | Untested | Untested | Krate Native v1 |
| Native: screen and window capture sources | Partial | Untested | Untested | Krate Native v1 |
| Native: SDK tray and global shortcuts | Partial | Untested | Untested | Krate Native v1 |
| Native: SDK app lifecycle | Supported | Untested | Untested | Krate Native v1 |
| Native: release as a single executable | Supported | Untested | Untested | Krate Native v1 |
| Native: prebuilt shell for apps without Go | Supported | Untested | Untested | Krate Native v1 |
| Native: krate native doctor | Supported | Untested | Untested | Krate Native v1 |
| Native: plugins | Supported | Untested | Untested | Krate Native v1 |
| Native: packaging formats | Partial | Untested | Untested | Krate Native v1 |
| Native: signing and notarization | Partial | Untested | Untested | Krate Native v1 |
| Native: updater | Supported | Untested | Untested | Krate Native v1 |
| Native: size, start-up and memory budgets | Supported | Planned | Planned | Krate Native v1 |

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

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

The shell opens a window around the system webview (WebKitGTK 4.1 through purego on Linux, WKWebView on macOS, WebView2 on Windows) and serves the built UI from an in-process custom scheme with a content security policy. Navigation away from the app origin is blocked, paths cannot escape the asset directory, and browser mode serves the same app on a loopback port behind a session token.

Known limits:

- Linux GUI tests need a display and WebKitGTK 2.40 or newer (KRATE_NATIVE_GUI=1); CI runs them under Xvfb.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.
- WebView2 on Windows needs the Evergreen runtime installed; krate native doctor reports it.

Evidence: `TestGUIPage`, `TestGUINavigationLock`, `TestGUIShell`, `TestGUITwoWindows`, `TestStaticResolution`, `TestStaticRejectsTraversal`, `TestDefaultCSPShape`, `TestBrowserMode`, `TestNativeE2EGUI`

### Native: streaming responses

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Responses are streamed to the webview as the handler writes them: headers go out first, body chunks are flushed as they come, a handler panic before the first byte becomes a 500, and closing the reader cancels the handler's context. Server-sent streams from host code use the same path.

Known limits:

- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.
- The Windows response writer (TestResponseWriterFlushStreams, TestBlockBufStreaming) and the macOS task writer have pure-Go unit tests; only the Linux path has been driven through a real webview.

Evidence: `TestServeStreamsChunks`, `TestServeCancelsWhenReaderCloses`, `TestServePanicBeforeHeadersIs500`, `TestStreamValuesThenEnd`, `TestStreamError`, `TestGUIRequestAbort`

### Native: host calls, streams and signals

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

The page calls host functions by id over the scheme: unary calls, server streams and signals (a value with change notifications that reconnects with the current value). Calls are cancellable, errors map to stable codes, concurrency is bounded, and permissions gate which SDK ids a page may reach.

Known limits:

- The scheme package is pure Go and identical on every OS; the Linux run is the one verified here. macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestInvoke`, `TestInvokeStreamAndSubscribe`, `TestSignalsReconnectGetsCurrentValue`, `TestBatchFastResultArrivesBeforeSlow`, `TestCancelByID`, `TestErrorMapping`, `TestConcurrencyIsBounded`, `TestEveryIDIsRegisteredAndGated`

### Native: Go host code

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Go host code (functions, streams and signals in .go files next to the pages) runs in a child process under krate native dev and krate native build, and is linked into the single executable in a release build. The child restarts after a crash and reconnects subscriptions.

Known limits:

- Needs the Go toolchain on the build machine (krate native doctor reports it).
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestChildLifecycle`, `TestChildCrashAndRestart`, `TestRunInProcess`, `TestNativeReleaseExample`, `TestNativeReleaseGoOnlyHasNoQuickJSAndShakesTray`, `fixture:examples/native-tasks`

### Native: TypeScript host code on QuickJS

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

TypeScript host code (*.host.ts files and @host functions) is bundled to one script and runs in the embedded QuickJS engine with no external runtime. Stack traces map back to the TypeScript sources, a watchdog stops runaway synchronous code, and the globals follow the published runtime compatibility ledger.

Known limits:

- QuickJS is not Node: only the globals in the runtime compatibility ledger exist; code that needs more selects a node, bun or deno host.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestCalls`, `TestSourceMappedStack`, `TestWatchdog`, `TestTimers`, `TestCompatLedger`, `TestNativeReleaseTSOnlyRuns`, `fixture:examples/native-tasks`

### Native: Node, Bun and Deno host children

**Linux:** Partial - **macOS:** Untested - **Windows:** Untested

TypeScript host code can run as a child process of the shell under Node, Bun or Deno (native.host.runtime, or native.host.islands for single packages). Deno gets permission flags derived from the app's permissions; a release build compiles the child into an executable (Node SEA, bun build --compile, deno compile).

Known limits:

- Node children are verified end to end on Linux. Bun and Deno command lines, flags and release builds are unit tested with stand-in executables; a real Deno child has not been run by any test, and the Bun and Node compile tests skip when the tool is missing.
- macOS: untested. The same Go code is built for it (cross-compile checks in CI) and the CI native job runs go test on it, but no result of a run against a real WKWebView window has been confirmed.
- Windows: untested. Node, Bun and Deno cannot use AF_UNIX there, so the shell uses loopback TCP with a random token (tests run that transport on every OS, including Linux with Node and Bun, and the hostchild tests run on the Windows CI job). A real Windows run of the shell with a host child, and of Deno on any OS, has not been confirmed.

Evidence: `TestHostChildNode`, `TestNativeHostChildNodeEndToEnd`, `TestNativeDevReloadsHostChild`, `TestDenoFlags`, `TestCommand`, `TestBuildReleaseNodeSEA`, `TestBuildReleaseBun`, `TestBuildReleaseDeno`, `TestReleaseBunCompiled`, `TestReleaseNodeSEA`, `TestTCPHandshake`, `TestParseRoundTrip`, `TestFor`, `TestChildTCPTransport`, `TestRunTCPToken`, `TestServeParentTCP`, `TestRemoteTCPSendsToken`

### Native: SDK data areas (kv, db, fs, net, os)

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

The key-value store, SQLite database, scoped file access, HTTP client with secrets, and OS information, directories, secrets and openExternal are available to the page and to Go and TypeScript host code, each behind its permission. Two processes can share a database and a store.

Known limits:

- Linux secrets use the freedesktop Secret Service over D-Bus (verified against a fake service); without a provider os.secret answers unsupported.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestGoAPI`, `TestTwoProcesses`, `TestWALAndPragmas`, `TestTraversalIsDenied`, `TestSymlinkEscapeIsDenied`, `TestClientTimeouts`, `TestSecretNotForwardedAcrossHosts`, `TestSecretServiceRoundTrip`, `TestInfo`, `TestUIDefaultsAndDenials`

### Native: SDK windows

**Linux:** Partial - **macOS:** Untested - **Windows:** Untested

Extra windows on routes, title and size, fullscreen, maximize, minimize, frameless windows, close-requested interception and per-window permission scopes bound to the webview, not to anything the page sends. A window can hold several webviews (native-views).

Known limits:

- Per-window permission scopes are enforced against the native webview a request came from (see native-view-identity). In browser mode the page's own x-krate-window header is the identity, so a script there can name another window's scope.
- Wayland: a client cannot place windows or keep them above others, so SetPosition and SetAlwaysOnTop are accepted and ignored there and the capability report says unsupported. The GUI test packages also run under weston (GDK_BACKEND=wayland, weston nested on Xvfb); TestWaylandPlacementIsIgnored checks the ignored placement. Window position and always-on-top were verified on X11 only.
- Fullscreen, maximize and minimize are verified with openbox on Xvfb (KRATE_NATIVE_WM=1), not on a full desktop environment.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestGUIWindowControls`, `TestGUIWindowOps`, `TestGUICloseRequested`, `TestGUITwoWindows`, `TestWindowOps`, `TestSecondWindowOnARoute`, `TestWindowOpenScopes`, `TestWindowScopeNarrowsCalls`, `TestWaylandPlacementIsIgnored`

### Native: requests are bound to the webview they came from

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

The platform layer tags every request on the app scheme with the webview that made it (WebKitGTK webkit_uri_scheme_request_get_web_view, the webView of WKURLSchemeHandler, one WebView2 WebResourceRequested handler per view) and the shell maps that view to its window or view id and permission scope. The x-krate-window header that page script can set is ignored in a native window, so a scoped page cannot claim a wider scope. A request from a webview the shell cannot place gets a scope that allows nothing.

Known limits:

- Browser mode (krate native dev --browser) has no native webviews, so the page's x-krate-window header is the identity there and a script can send another window's id; scopes in browser mode only catch mistakes.
- A scope limits what a page may ask the host to do; it does not isolate the page. Every window and view shares one origin, so pages of different scopes can read each other's localStorage and cookies.
- macOS and Windows: untested. The code is compiled for both (cross-compile checks) and the request tagging is written against the documented APIs, but no result of a run against a real WKWebView or WebView2 has been confirmed. On Windows the identity is the handler object registered for the view, not a pointer comparison of the sender.

Evidence: `TestIdentifyOverridesTheWindowHeader`, `TestIdentify`, `TestViewContext`, `TestScopedWindowCannotForgeAWiderScope`, `TestViewScopeIsBoundNatively`, `TestGUIScopedWindowCannotForgeAWiderScope`, `TestGUIViewIdentity`, `TestViewScopes`, `TestViewWindowMapping`, `TestGUIViews`

### Native: views and split panes

**Linux:** Supported - **macOS:** Supported - **Windows:** Supported

A window can hold several webviews laid out as a tree of horizontal and vertical splits with draggable dividers, minimum pane sizes and ratios that can be read back after a drag. Views are added, removed, focused, navigated and given their own permission scope; they report added, navigated, removed and closed events and can post messages to each other. All views share the app's one web process; an extra view costs about what a page costs (about 1 MB of web process memory on the WebKitGTK 2.54 sysroot, about 40 MB with software-GL compositing on the CI runner). Layout arithmetic is pure Go and tested on every OS; the real-window view tests and the native-views example run in CI on Linux, macOS and Windows.

Known limits:

- Divider drags on Linux are verified by calling gtk_paned_set_position, which is what the handle does, not by moving a real pointer.
- macOS (nested NSSplitView, Auto Layout minimum sizes) and Windows (several WebView2 controllers in one HWND, dividers hit-tested in the window procedure) are untested: compiled, with the layout and drag arithmetic unit-tested, but never run. The WebView2 NavigationCompleted and WindowCloseRequested handler GUIDs are written from memory of WebView2.h and unverified.
- On macOS a page's window.close() does not remove a secondary view (no closed event for it); Linux and Windows do.
- Release applies to a whole window, not to one view. While a window is released, adding, removing or re-laying out its views fails.
- A divider drag fires no event; read the ratios with layout().
- Messages are JSON up to 1 MiB, not queued for a page that has not loaded, and carry the sender's view id only when the sender is a page.

Evidence: `TestNativeE2EViewsGUI`, `TestArrangeTwoPanes`, `TestArrangeNestedCoversBounds`, `TestSplitSizesMinimums`, `TestMinSizePropagates`, `TestHitDividerAndDrag`, `TestValidate`, `TestSplitLeafAndRemove`, `TestRatiosAndMins`, `TestSplitterHitAndCursor`, `TestSplitterDrag`, `TestSplitterDPIScales`, `TestViewOps`, `TestViewEventsAreHeldUntilRegistered`, `TestGUIViewsTwoPane`, `TestGUIViewsNestedThreePane`, `TestGUIViewsReleaseRestore`, `TestGUIViewClosedByItsPage`, `TestGUIViewsMemory`, `TestViewMessagingEventsAndLayout`, `TestGUIViews`, `fixture:examples/native-views/src/pages/index.tsx`

### Native: shared web process, release and background mode

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

All windows of an app, and all webviews of a window, share one web process (related web views on Linux, one WebView2 environment on Windows). A window can release its webview (hide with release) and get it back on the same route and scroll position; native.window.keepAlive keeps a tray app running with no window; hidden and minimized pages get a krate:memory-pressure event.

Known limits:

- macOS and Windows: untested. The code builds for both and the Windows vtable slots are checked against WebView2.h, but no run against a real WKWebView or WebView2 window has been confirmed. On macOS the single-web-process switch is a private WebKit selector guarded by respondsToSelector; on Windows ICoreWebView2_19 memory targets need a recent runtime and are skipped without it.
- A hidden page that is not released keeps its web process memory; only the page's own handlers of krate:memory-pressure and the shell's heap trim run. Release is what returns the web process memory.
- The network process of WebKitGTK stays alive while the app runs, so a released Linux app still holds it.
- Release destroys every webview of a window and restores them all with their routes and layout ratios; there is no per-view release.

Evidence: `TestGUISharedWebProcess`, `TestGUIReleaseRestore`, `TestGUIKeepAlive`, `TestGUINavigateReleased`, `TestGUIMemoryPressureOnHide`, `TestGUIWindowsBackground`, `TestParseViewState`, `TestParkOnClose`, `TestQuitWhenEmpty`, `TestGUIViewsReleaseRestore`, `TestMemorySelectors`, `TestLeversAreGuardable`

### Native: lean pages and the dev Memory panel

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Native builds ship no script on a page without client code and do not preload lazy chunks; krate native check and native builds warn (KR7270 to KR7274) about oversized images, large inlined data: URLs, web fonts, compositing CSS effects and long lists without windowing; krate native dev has a Memory panel with PSS, private dirty, DOM nodes and the JS heap where the engine exposes it.

Known limits:

- The checks are static: sizes set from CSS classes, values chosen at run time and data fetched later are not seen. The web font check is a heuristic.
- WebKit has no JS heap size API, so the Memory panel shows the heap as unavailable on Linux and macOS and points at the web process row.
- macOS and Windows: the panel and the checks are platform independent, but only the Linux webview has run them.

Evidence: `TestNativePagesLoadOnlyWhatTheyUse`, `TestLeanOversizedImage`, `TestLeanDataURIThreshold`, `TestLeanWebFonts`, `TestLeanCompositing`, `TestLeanWillChangeEverywhere`, `TestLeanLongLists`, `TestMemoryBrowserMode`, `TestGUIZMemoryPanel`, `TestNativeBuildWritesNoPrecompressedAssets`, `TestNativeTargetDisablesViewTransitionsByDefault`, `TestNativeDocsSiteE2E`, `TestGUIRequestBodyStress`

### Native: SDK dialogs, clipboard and menus

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Open, save, folder and message dialogs, clipboard text, PNG image and file list, and the menu bar and context menus with click events delivered to Go and to the page.

Known limits:

- A native dialog cannot be clicked headlessly: the tests check that it is created with the right options and answer it programmatically.
- Windows menu accelerators are shown but not active; Windows message boxes do not support custom button labels.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestGUIDialogs`, `TestGUIClipboard`, `TestGUIMenus`, `TestDialogsAreCreatedWithTheRightOptions`, `TestClipboardThroughTheSDK`, `TestMenuClickReachesGoAndTheUI`, `TestDialogRoundTrip`, `TestMenuEvents`

### Native: SDK notifications

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Desktop notifications with actions. On Linux they use org.freedesktop.Notifications over D-Bus; on macOS UNUserNotificationCenter; on Windows, WinRT toasts.

Known limits:

- Verified against a real dunst daemon on a private session bus (text, icon, urgency, replacement by id, close from the app and from the daemon with the NotificationClosed reason, default action and action buttons, expiry, daemon gone) and against a fake service for protocol edge cases. Other daemons (GNOME Shell, KDE Plasma) were not run. Without a service that owns org.freedesktop.Notifications the call returns ErrUnsupported.
- macOS: implemented, not yet run on a Mac (untested). It needs an app bundle with a bundle identifier; krate native dev runs the shell from a generated <appId>.dev bundle under .krate/dev. A process outside a bundle gets an ErrUnsupported error that names the fix. The first notification asks for permission and a refusal is a permission_denied error. Icon and Timeout are ignored, and critical urgency is only time-sensitive (the critical level needs an Apple entitlement).
- Windows: untested. Toasts use WinRT through hand-written COM bindings; the toast XML, ids and registry values are unit tested on Linux, and a Windows CI job shows a toast, but nothing here has been confirmed on a real desktop. An unpackaged app registers its AppUserModelID under HKCU; a packaged app uses its package identity.
- Windows: clicking a toast after the app has quit does nothing (COM activation of a closed app is not implemented). Windows has no per-toast timeout or urgency levels, and Show succeeding does not prove a banner was drawn (Focus assist and per-app settings can suppress it).

Evidence: `TestNotifyOverDBus`, `TestNotifyWithoutServiceIsUnsupported`, `TestNotificationAndTrayWithFakeDesktopServices`, `TestBuildToastXML`, `TestBuildToastXMLVariants`, `TestPinterfaceIID`, `TestAUMIDFor`, `TestToastXMLAcceptedByWinRT`, `TestToastDelegateInterfaces`, `TestGUIToast`, `TestPlatformBackendClose`, `TestNotifyUnbundled`, `TestNotifyBundled`, `TestBundleProblem`, `TestNotifyEvent`, `TestPlatformNotifyAvailability`, `TestPrepareMacDevBundle`, `TestRealDunstShowsNotification`, `TestRealDunstUrgencyLevels`, `TestRealDunstReplaceByID`, `TestRealDunstClosedByApp`, `TestRealDunstClosedByDaemon`, `TestRealDunstExpires`, `TestRealDunstDefaultAction`, `TestRealDunstActionButton`, `TestRealDunstGoneIsUnsupported`, `TestRealNoDaemonOnARealBus`

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

**Linux:** Partial - **macOS:** Untested - **Windows:** Untested

A tray icon with a menu (StatusNotifierItem and dbusmenu on Linux with a legacy XEmbed tray fallback, NSStatusItem on macOS, Shell_NotifyIconW on Windows) and global keyboard shortcuts (XGrabKey or the portal on Linux, Carbon RegisterEventHotKey on macOS, RegisterHotKey on Windows).

Known limits:

- The tray uses the StatusNotifierItem protocol when a StatusNotifier host exists and falls back to the legacy XEmbed system tray (GtkStatusIcon) when only an XEmbed tray manager owns _NET_SYSTEM_TRAY_S<screen> (X11 only). Stock GNOME with neither returns ErrUnsupported with a hint to install the AppIndicator extension. Verified against stalonetray (XEmbed, with real clicks and menu selection through XTEST) and against the Ayatana application indicator service as a real StatusNotifierWatcher and host; KDE Plasma, GNOME Shell with the extension and other panels were not run.
- Linux shortcuts are verified on X11 (XGrabKey). On Wayland they need the org.freedesktop.portal.GlobalShortcuts portal, verified against a fake portal only (TestShortcutPortal), and return ErrUnsupported without it. No portal backend that implements GlobalShortcuts can run headless here: the GNOME backend needs a running gnome-shell/mutter session, the KDE backend needs KGlobalAccel in a Plasma session, the Hyprland and COSMIC backends are not packaged for Debian or Ubuntu, and xdg-desktop-portal-wlr does not implement the interface.
- macOS: implemented, not yet run on a Mac (untested). With a tray menu any click opens the menu and is reported as click; right-click and double-click are only reported without a menu. Radio items show a checkmark. Single-tone gray icons become template images.
- macOS shortcuts use Carbon RegisterEventHotKey (no Accessibility permission). Keys are ANSI key positions, so other layouts differ, and macOS 15 ignores hot keys whose only modifiers are Option and/or Shift. A combination that is already registered is an error. Pressing a real hot key and clicking the real status item are manual checks (platform/darwin/MANUAL_TEST.md).
- Windows: untested. The key table, the NOTIFYICONDATAW layout, event decoding and icon scaling are unit tested on Linux, and a Windows CI job adds, modifies and removes a tray icon and registers hotkeys, but the icon, its menu behavior, the explorer-restart re-add and real key presses have not been confirmed on a real desktop.
- Windows: a shortcut another app or Windows holds fails with an 'already registered by another app' error; many Win+ combinations are reserved by the system. The tray menu has no accelerators and one icon per app.

Evidence: `TestTrayOverDBus`, `TestTrayWithoutHostIsUnsupported`, `TestShortcutPortal`, `TestShortcutWaylandWithoutPortalIsUnsupported`, `TestGUIGlobalShortcut`, `TestShortcutAndSupports`, `TestVirtualKey`, `TestHotkeySpec`, `TestHotkeyError`, `TestHotkeyTable`, `TestNotifyIconDataLayout`, `TestTrayEventKind`, `TestPNGToIconBits`, `TestGUITrayLifecycle`, `TestGUIHotkeys`, `TestTrayItemAndMenu`, `TestHotKeyRegisterUnregister`, `TestToCarbon`, `TestCarbonStatusError`, `TestInspectTrayIcon`, `TestAccelKeyEquivalent`, `TestPlatformTrayIsUsed`, `TestParseCmdOrCtrl`, `TestRealStatusNotifierHost`, `TestRealXEmbedTray`, `TestRealNoTrayAtAll`, `TestTrayFallsBackToXEmbedWithoutAStatusNotifierHost`, `TestTrayPrefersStatusNotifierOverXEmbed`, `TestNoTrayAtAllNamesTheAppIndicatorExtension`

### Native: SDK app lifecycle

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Single instance, deep links and the Linux .desktop entry that registers the app's URL schemes.

Known limits:

- URL scheme registration is a packaging step on macOS (Info.plist) and Windows (registry); the runtime code is shared.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestSingleInstance`, `TestStaleSocketIsReplaced`, `TestURLsFromArgs`, `TestDesktopEntry`

### Native: release as a single executable

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

krate native build --release links the shell, the UI, the host code and only the SDK areas the app uses into one executable per target (CGO_ENABLED=0, stripped, with a report of linked and left-out areas). Cross-compiling to the other OSes is covered; running those executables is not.

Known limits:

- The macOS and Windows executables are cross-compiled from Linux and checked for their format, never launched.
- Needs Go on the build machine; without it (native.release.compile auto) the build falls back to the prebuilt-shell layout.

Evidence: `TestNativeReleaseDerivesPermissionsAndReport`, `TestNativeReleaseTSOnlyRuns`, `TestNativeReleaseGoOnlyHasNoQuickJSAndShakesTray`, `TestNativeReleaseCrossCompiles`, `TestNativeReleaseWindowsFlagsAndTargets`, `TestNativeReleaseExample`, `TestNativeReleaseE2EGUI`

### Native: prebuilt shell for apps without Go

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

The @krate/core-<platform> packages carry krate-shell next to krate. krate native build then writes a runnable dist-native/<os>-<arch>/<productName> with app/ beside it for a TypeScript-only app, with no Go toolchain, and krate native package turns that into tar.gz, deb and the other formats. The shell is found in order: --shell, $KRATE_SHELL, next to the krate executable, a build from a Krate checkout.

Known limits:

- The Linux test uses a stand-in shell executable placed next to a copy of the test krate binary and a PATH with no go command; it proves the layout and packaging, not a launch.
- The packages for all seven platforms are built by scripts/build-platform-packages.mjs and checked by pnpm check:release; the npm install of them (scripts/smoke-install.mjs) runs in the release workflow.
- Alpine (musl): the @krate/core-linux-x64-musl shell is linked with musl's loader (/lib/ld-musl-x86_64.so.1) instead of glibc's, so it starts and `krate-shell --doctor` runs; the install smoke test checks this on node:alpine in CI. A window needs WebKitGTK 4.1 from the distribution, which was not run on Alpine.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestFindPrebuiltShellOrder`, `TestLocateNativeShellOrder`, `TestAssembleShellDist`, `TestNativePrebuiltShellWithoutGo`, `TestDiscoverPrefersReport`

### Native: krate native doctor

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

Reports the Go toolchain, the shell binary and its path, the system webview (WebKitGTK 4.1 version and library paths and display on Linux, the OS version on macOS, the WebView2 runtime version on Windows, through krate-shell --doctor), the packaging and signing tools, and the host runtime the config selects. Exits 1 when something the config needs is missing.

Known limits:

- The report logic is tested with injected probes for all three OSes; the real probes ran only on Linux. The macOS version and Windows registry probes compile (go vet per GOOS) but have not run.

Evidence: `TestHealthyTypeScriptOnlyLinuxWithoutGo`, `TestGoRequiredForGoHostAndCompileAlways`, `TestWebviewMissingIsRequired`, `TestHostRuntimes`, `TestPackagingToolsPerOS`, `TestNativeDoctorCommand`, `TestBinaryDoctor`, `TestProbeShape`

### Native: plugins

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

A plugin is a Go package plus a krate-plugin.json (name, Go import path, permissions with descriptions, ops, minimum Krate). native.plugins enables local directories, npm packages and Go modules; ops are callable as plugin/<name>.<op> (lowerCamel op names like the SDK ids, bound to Go names by capitalizing or an explicit go field) with generated codecs and a typed krate/plugin/<name> TypeScript module (calls, streams, signals). Plugins run inside the shell process with a context (lifecycle hooks, Dispatch, platform capabilities, data directory), cannot register ids outside their namespace, and their permissions (plugin:<name>.<permission>) are derived from the code, enforced per window, listed by krate native check and shaken per op in release builds. An app with plugins runs under a per-app shell built with go (cached by plugin set, sources and Krate version) in dev and in builds; krate native plugin init scaffolds one.

Known limits:

- The plugin runtime and the build are exercised on Linux (the GUI test runs the shell in a real WebKitGTK window and checks a granted call, a call denied to a window scoped to nothing, and the lifecycle hooks). The macOS and Windows code paths that plugins use (the platform layer, the per-app shell build) compile but have not run; the system-info example's macOS CPU load (ps) and Windows (GetSystemTimes) readings are untested.
- Plugins are trusted Go code in the shell process; the namespace and permissions limit what a page can reach through them, not what the plugin does.
- Plugin ops are callable from pages; TypeScript host code reaches them only by id (__krateHost.call). A go: plugin needs network or a populated module cache for the first build.
- Editing a local plugin's Go code restarts native dev instead of hot-reloading it.

Evidence: `TestInstallEnforcesNamespace`, `TestInstallRegistersGatedOps`, `TestSignalGate`, `TestLifecycle`, `TestParseManifestErrors`, `TestResolveDiagnostics`, `TestNativePluginsKey`, `TestPluginNamedImports`, `TestNativePluginDevBuild`, `TestNativePluginReleaseShakesOps`, `TestNativePluginReleaseWithGoHostCode`, `TestNativePluginScaffoldBuilds`, `TestNativePluginInit`, `TestGoRequiredForPlugins`, `TestGUIPlugins`, `TestNativePluginGUI`

### Native: packaging formats

**Linux:** Partial - **macOS:** Untested - **Windows:** Untested

krate native package builds .tar.gz, .deb, .rpm and AppImage on Linux, .app, .zip and .dmg on macOS, and .zip, NSIS installer and MSIX on Windows, from a release executable or the prebuilt-shell layout, with icons, desktop entries and update files.

Known limits:

- On Linux the .deb, .rpm and AppImage of examples/native-tasks are built, and the .deb contents and AppImage were launched and checked to serve the first page (scripts/native-smoke.mjs; the CI native job installs the .deb with dpkg and runs the AppImage). The .dmg needs hdiutil and the NSIS installer needs makensis, so those are exercised only in the macOS and Windows CI steps, which have not run yet.
- macOS and Windows packages are assembled by pure Go code on Linux and checked for structure; none has been installed or opened on its OS. The NSIS installer needs makensis.

Evidence: `TestLinuxTarGzAndDeb`, `TestRPM`, `TestAppImageNeedsTool`, `TestMacAppAndZip`, `TestMacDMGUsesHdiutilOnlyOnMac`, `TestWindowsZipAndNSIS`, `TestMSIXStructure`, `TestNativePackageEndToEnd`, `TestUpdateArtifactsAreSignedAndFeedWritten`

### Native: signing and notarization

**Linux:** Partial - **macOS:** Untested - **Windows:** Untested

krate native sign and notarize drive codesign and notarytool on macOS, signtool or osslsigncode for Windows, and gpg for Linux packages, and rebuild the archives around the signed payload.

Known limits:

- The signing tools are replaced by stand-ins in every test: command lines, ordering and secret redaction are checked, a real certificate and Apple notarization service never were.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestSignMacAppDeepSignsInnerFirst`, `TestSignMacAdHocAndEntitlements`, `TestSignMacOnlyOnMac`, `TestNotarize`, `TestSignWindows`, `TestSignLinuxGPG`, `TestPackageSignsBeforeArchiving`, `TestRedactCommand`

### Native: updater

**Linux:** Supported - **macOS:** Untested - **Windows:** Untested

In-app updates: an Ed25519-signed feed, resumable verified downloads, and an apply step that replaces the executable and app directory (Linux, AppImage, macOS bundle, Windows ordering) with rollback on failure.

Known limits:

- The macOS bundle and Windows apply steps are file-system logic tested on Linux; replacing a running Windows executable has not been tried on Windows. TestSmokeUpdateInstalledApp (skipped unless scripts/native-smoke.mjs sets its environment) runs the real updater against a signed local feed from 1.0.0 to 1.1.0 of an installed packaged app; it ran on Linux only.
- macOS and Windows: untested. The same Go code is built for them (cross-compile checks in CI) and the CI native job runs go test on both, but no result of a run against a real WKWebView or WebView2 window has been confirmed.

Evidence: `TestSignatureGoodBadWrongKey`, `TestCheck`, `TestDownloadVerifiesAndProgress`, `TestDownloadResumes`, `TestApplyLinuxReplacesExecutableAndAppDir`, `TestApplyRollsBackOnFailure`, `TestApplyAppImage`, `TestApplyMacBundle`, `TestApplyWindowsOrder`, `TestSmokeUpdateInstalledApp`

### Native: size, start-up and memory budgets

**Linux:** Supported - **macOS:** Planned - **Windows:** Planned

packages/native/budgets.json limits the stripped shell, Go-only, TypeScript-only and example release executables (bytes), the cold start of the example in a real window (ms) and its resident memory (KB). Limits are the measured value plus 25 percent.

Known limits:

- Budgets exist for linux-amd64 only; the start-up limits were measured on a developer machine and CI applies KRATE_BUDGET_SCALE=3 until a hosted runner has been measured.
- macOS and Windows have no recorded budgets (planned). The CI native job runs the memory profile there and prints a comparison with the linux-amd64 limits (scripts/native-budget-compare.mjs); it is informational until the numbers have been reviewed.

Evidence: `TestNativeBudgetSizes`, `TestNativeBudgetGUI`

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
