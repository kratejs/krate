---
title: Compatibility Matrix
order: 6
description: The public Krate compatibility matrix for React source components, Preact islands, SSR, and the browser conformance corpus.
---

# Compatibility Matrix

This is the support contract for React compatibility. A surface is only marked
stable when the referenced corpus and browser tests pass. Known divergences are
listed instead of silently hidden.

Last verified: **2026-11-05**. See the [roadmap](/roadmap/) for phase
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
| Error boundaries | Beta | Beta | Partial | Source and island |
| Per-segment not-found and error pages | Beta | n/a | Beta | Next.js not-found.tsx and error.tsx |
| Children, keys, and refs | Partial | Partial | n/a | Documented semantics |
| Events | Beta | Beta | n/a | React 18 parity |
| Forms and inputs | Beta | Beta | n/a | React 18 parity |
| Portals | Beta | Beta | n/a | SSR-safe |
| Suspense | Beta | Beta | n/a | SSR-safe |
| React 19 actions and forms | Beta | Partial | n/a | Source semantics |
| React 19.2: useId, useEffectEvent, Activity, ViewTransition | Beta | n/a | n/a | Source semantics |
| Custom elements | Beta | n/a | n/a | React 19 semantics |
| Component tiers: client APIs, conflicts and higher-order components | Beta | n/a | n/a | Krate semantics |
| Server actions | Beta | n/a | Beta | Progressive enhancement |
| Data cache and revalidation | Beta | n/a | Beta | next/cache parity |
| Compiled React packages | n/a | Beta | Beta | Documented compat |
| shadcn-style dashboard | Beta | n/a | n/a | Stable |
| Next.js app corpus | Beta | n/a | n/a | Migration-ready |
| Local components in islands | n/a | Beta | n/a | Supported |
| npm React hook libraries (zustand, react-hook-form, SWR, TanStack Query, ...) | n/a | Partial | Beta | Run as Preact islands |
| CSS-in-JS (styled-components, emotion, MUI) | n/a | Partial | Partial | Client-only islands |
| Island hydration control (client:visible, client:idle) | n/a | Beta | n/a | Astro-style directives |
| SSR island hydration | n/a | Beta | Beta | Hydration-safe |
| Supported Preact versions | n/a | Beta | n/a | 10.20 through current |
| Browser differential corpus | Beta | n/a | n/a | No P0/P1 divergence |
| React 19 reference app | Beta | n/a | n/a | Same browser suite as React 18 |
| Serverless and Node deployment | n/a | n/a | Beta | Vercel, Netlify, Cloudflare and Node hosting |

## Details

### Hooks: state and reducers

**Source:** Beta - **Islands:** Beta - **SSR:** n/a

useState and useReducer lower to signals with React render snapshots (handlers and effects read the committed state, functional updaters the latest), Object.is bail-out, lazy initializers (inline or named), useReducer init, a stable dispatch, and React's update lanes (discrete events commit in a microtask, other updates in a later task).

Evidence: `TestReactUseReducerBuild`, `TestRewriteReactUseReducer`, `TestRewriteReactNamespaceUseReducer`, `TestUseReducerLazyInitIsPreserved`, `TestLazyUseStateInitializerRunsOnce`, `TestStateLoweringUsesReactSemantics`, `browser:/react-effects/`

### Hooks: effects and memo

**Source:** Beta - **Islands:** Beta - **SSR:** n/a

Effects run in React's commit order (insertion, layout before paint, passive after paint or synchronously after discrete events; children before parents; cleanups before re-runs; parent-first unmount) with Object.is deps, and setups/cleanups read the state and props of their own commit. useMemo/useCallback honour deps synchronously. Conditional branches unmount and remount components with fresh state. Effects without deps re-run on the component's own state/props changes only; each divergence has a KR2xxx diagnostic.

Evidence: `TestRewriteReactEffectDepsAndMemo`, `TestRewriteReactEffectEmptyDepsMountOnce`, `TestEffectPhasesLowerToCreateEffectDeps`, `TestEffectWithoutDepsSynthesizesStateAndProps`, `TestHydrationDeclaresEffectOwners`, `TestFreshDepsReRunEveryRender`, `TestHookInLoopOrConditionWarns`, `browser:/react-effects/`

### Context and refs

**Source:** Beta - **Islands:** Beta - **SSR:** n/a

Context resolves at the reader's position like React: Provider, React 19 <Ctx value>, Consumer render props, object defaults, nested and self-nesting providers, and providers around props.children; constant values fold at build time and state-driven values stay reactive. Object and callback refs (with React 19 cleanup functions), ref as a prop, forwardRef, createRef, useRef, and useImperativeHandle with deps attach before layout effects and detach on unmount.

Evidence: `TestUseContextTranspile`, `TestCrossComponentDynamicContextTranspile`, `TestContextSelfNestingProviders`, `TestContextReact19ProviderAndConsumer`, `TestContextChildrenSeeComponentProvider`, `TestReactForwardRefCallback`, `TestReactCreateRefBinds`, `TestRefBindingsAndEffectOrder`, `browser:/react-events/`

### Transitions and deferred values

**Source:** Beta - **Islands:** Beta - **SSR:** n/a

useTransition and startTransition support async actions: isPending stays true until the action, its awaited work and overlapping transitions settle, and a transition keeps revealed Suspense content. useDeferredValue (with React 19 initialValue) catches up in a later task; useSyncExternalStore follows the store by Object.is and unsubscribes on teardown. Updates after an await are ordinary updates, as in React 19.

Evidence: `TestRewriteReactUseTransitionLowered`, `TestTransitionStarterLowering`, `TestDeferredValueAndExternalStoreLowering`, `TestRuntimeNeedsSuspense`, `browser:/react-suspense/`, `fixture:examples/src/pages/react-suspense.tsx`

### Class components

**Source:** Beta - **Islands:** n/a - **SSR:** n/a

Lowered onto a runtime instance with React 18 ordering: batched setState with callbacks, forceUpdate, componentDidMount/DidUpdate (props and state, children first)/WillUnmount on branch removal, shouldComponentUpdate, PureComponent, getDerivedStateFromProps, getSnapshotBeforeUpdate, defaultProps, contextType, accessors, private members, and instance refs. UNSAFE_componentWillMount, legacy context and other statics are rejected with KR3xxx diagnostics.

Evidence: `TestClassComponentLowering`, `TestClassComponentLoweringEmit`, `TestClassLoweringIssueNamesMember`, `browser:/react-class/`, `fixture:examples/src/pages/react-class.tsx`

### Error boundaries

**Source:** Beta - **Islands:** Beta - **SSR:** Partial

Classes with getDerivedStateFromError/componentDidCatch catch errors from descendants' render, bindings, lifecycles and effects (not event handlers or async code), unmount the failed subtree, and recover on reset. A throw while the page is built renders the boundary fallback (KR3009). Uncaught errors unmount only the failing component. Each Preact island has its own error boundary and marks a fallback placeholder when rendering fails.

Known limits:

- Build-time boundary fallback for a browser-only island subtree is client-only; the island error boundary handles the client render failure.
- Island error boundaries catch Preact render errors, not event-handler or asynchronous errors.

Diagnostics: `KR3008`, `KR3009`

Evidence: `TestBoundaryBuildTimeFallback`, `TestBuildTimeThrowWithoutBoundaryFails`, `TestClassBoundaryChildrenBranch`, `browser:/react-class/`, `browser:/react-demo/`

### Per-segment not-found and error pages

**Source:** Beta - **Islands:** n/a - **SSR:** Beta

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

**Source:** Beta - **Islands:** Beta - **SSR:** n/a

React 17+ root delegation with capture then bubble order, native listener interleaving, stopPropagation, the synthetic event surface, React onChange semantics, focus/blur bubbling, per-element enter/leave, media and composition events. Unknown or lowercase event props are diagnosed (KR5001, KR5002).

Known limits:

- Island event handlers run on Preact's event system, so Krate's source-level event diagnostics do not apply inside a compiled package.
- Controlled pointer/focus state in a compiled package is the package's responsibility; Krate only bridges page signals and callbacks.

Diagnostics: `KR5001`, `KR5002`

Evidence: `TestEventDelegationBubbles`, `TestNonBubblingEventUsesDirectListener`, `TestCaptureEventUsesCaptureListener`, `TestEventHandlersUseReactPropNames`, `TestEventAndRefDiagnostics`, `browser:/react-events/`, `browser:/react-demo/`

### Forms and inputs

**Source:** Beta - **Islands:** Beta - **SSR:** n/a

Controlled text, select (single and multiple), checkbox, radio group, textarea, number and range inputs restore to state after onChange; uncontrolled defaultValue/defaultChecked only set defaults; form reset and value={undefined} follow React. Form diagnostics: KR5004 to KR5007. React 19 form actions (action/formAction functions, reset after success) run in the browser gate. No-JS server action posts reach the server and redirect back; the returned state is carried in a short-lived flash cookie that useActionState reads on the next page, but a pre-rendered page is not re-rendered with it without script.

Known limits:

- Controlled controls inside a compiled package use Preact's controlled-input behavior; Krate's form diagnostics apply to source components only.
- Server action forms remain source-level; a compiled package receives the action runtime but not Krate's form lowering.

Diagnostics: `KR5004`, `KR5005`, `KR5006`, `KR5007`

Evidence: `TestFormValueUsesPropertyBinding`, `TestFormControlBindings`, `TestFormDiagnostics`, `browser:/react-forms/`, `browser:/react-events/`, `TestReactActionFormsCompile`, `browser:/react-actions/`, `browser:/react-demo/`

### Portals

**Source:** Beta - **Islands:** Beta - **SSR:** n/a

createPortal and Portal mount into document.body or a resolved container after hydration, follow conditional branches, unmount with their component, and bubble React events to the portal's React ancestors. Like React, portal content is not part of the server-rendered HTML.

Evidence: `TestPortalRendersAndMounts`, `TestCreatePortalLowersToAnchor`, `TestCreatePortalDynamicContainer`

### Suspense

**Source:** Beta - **Islands:** Beta - **SSR:** n/a

Boundaries show their fallback while use(promise), a resource or a lazy component inside is pending, reveal siblings together with React's 300ms reveal throttle, and nest independently (innermost wins). Content that cannot resolve at build time ships the fallback and hydrates into the content. A non-transition update that suspends hides the content in place and shows the fallback; a transition keeps the revealed content. lazy() (static import or a loader resolving to a local component) shows the fallback until its separately loaded hydration chunk or loader is ready. Matches React in the browser differential suspense fixtures.

Evidence: `TestBuildSuspenseBoundaryStaticFallback`, `TestBuildSuspenseBoundaryRuntimePrimary`, `TestSuspenseUseRendersFallbackAndTemplatesContent`, `TestNestedSuspenseInnermostWins`, `TestLazyComponentGetsHydrationChunk`, `TestLazyLowersToImportAndMarker`, `TestUsePromiseLowersToGetter`, `browser:/react-suspense/`, `fixture:examples/src/pages/react-suspense.tsx`

### React 19 actions and forms

**Source:** Beta - **Islands:** Partial - **SSR:** n/a

Source useActionState (queued, previous state over the action transport, permalink no-JS fallback), useFormStatus (form-scoped), useOptimistic (revert on settle), requestFormReset, and function form actions are lowered and run in the browser gate. Compiled packages get the same useActionState/useOptimistic runtime; their useFormStatus is idle. use() and async startTransition are tracked under Suspense and transitions.

Known limits:

- Compiled packages get useActionState and useOptimistic through the compat runtime; useFormStatus is idle inside an island.
- Use() and async transitions in compiled packages follow Preact compat behavior, not the source-component transition lowering.
- React 19 action hooks run in source components; a compiled island gets the compat runtime subset (useFormStatus is idle).

Diagnostics: `KR7001`

Evidence: `TestLowerUseActionState`, `TestReactBrowserDifferential`, `TestLowerUseFormStatusDestructured`, `TestLowerUseOptimistic`, `TestReactActionFormsCompile`, `browser:/react-actions/`

### React 19.2: useId, useEffectEvent, Activity, ViewTransition

**Source:** Beta - **Islands:** n/a - **SSR:** n/a

useId gives every component instance (children, nested children, lists, repeated components, conditional branches, layouts, rows added at runtime) an id that is the same in the static HTML and the hydration code, so htmlFor and id pairs match. useEffectEvent lowers to a stable function that reads the latest props and state and does not subscribe the effect that calls it. <Activity mode> renders its children in a wrapper that hides them while keeping state, cleans their effects up while hidden and runs them again when visible. <ViewTransition> names its element child for the router's view transition support (name and default class). The experimental names (unstable_Activity, unstable_ViewTransition, unstable_useEffectEvent, SuspenseList) are accepted; props with no effect warn (KR8031).

Diagnostics: `KR8031`

Evidence: `TestUseIdInChildComponents`, `TestUseIdInHydratedChildMatchesHTML`, `TestUseIdInDynamicListRowsIsNumberedPerRow`, `TestUseEffectEventIsLowered`, `TestActivityRendersChildrenAndHidesThem`, `TestActivityUnsupportedPropWarns`, `TestViewTransitionNamesItsChild`, `TestReact19Browser`

### Custom elements

**Source:** Beta - **Islands:** n/a - **SSR:** n/a

Strings and numbers render as attributes, true as an empty attribute and false is omitted. Objects, arrays and functions render no attribute on the server and are set as properties on the client, also when the element class is defined after the page loads. Components that render such props hydrate so the property is set.

Evidence: `TestCustomElementPropsFollowReact19`, `TestCustomElementAttrs`, `TestReact19Browser`

### Component tiers: client APIs, conflicts and higher-order components

**Source:** Beta - **Islands:** n/a - **SSR:** n/a

A server, static or runtime component that uses state, effects or event handlers fails the build (KR7110) with the fix, instead of shipping a dead page. Two tier sources that disagree for one component fail (KR7115). A tier decorator on an HOC-wrapped default export classifies the component and the HOC is applied, also when it is declared in another module; an HOC that cannot be inlined fails (KR8030).

Diagnostics: `KR7110`, `KR7115`, `KR8030`

Evidence: `TestBuildTimeTierRejectsClientAPIs`, `TestConflictingTiersFail`, `TestTierConflictRule`, `TestTierDecoratorOnHOCDefaultExport`, `TestHOCFromAnotherModuleIsApplied`, `TestUnresolvableHOCFailsTheBuild`

### Server actions

**Source:** Beta - **Islands:** n/a - **SSR:** Beta

File-level "use server" modules compile to hashed POST endpoints with client proxies, bound arguments, no-JS form posts, SSR forms, registry validation, origin checks, body limits, and no-store responses. Enhanced forms return results without reloading; errors use a {message, digest} envelope; actions read headers() and cookies() and can redirect(). Inline "use server" functions are a coded build error (KR7001).

Evidence: `TestPrepareActionsBuildsManifestAndBundle`, `TestRewriteActionImports`, `TestSameOriginActionRequest`, `TestActionSidecarTransport`, `TestActionHandlerSecurity`, `TestInlineUseServerIsCodedError`, `fixture:examples/src/pages/react-actions.tsx`, `browser:/react-actions/`

### Data cache and revalidation

**Source:** Beta - **Islands:** n/a - **SSR:** Beta

@krate/runtime/cache provides revalidatePath, revalidateTag, unstable_cache and unstable_noStore; server fetch honors next.tags/next.revalidate/cache options; pages accept export const revalidate and export const dynamic. Revalidation is applied across the SSR renderer workers, Go region cache, QuickJS and Node API data caches before an action or API route responds; static (SSG) targets are reported as skipped (KR8603).

Known limits:

- Caches are in memory per server process; multiple krate serve instances do not share them.
- Static (SSG) pages cannot be revalidated at runtime; revalidatePath reports them as skipped.
- Tags are recorded for data read during the page render; region renders and Go components do not record tags.

Diagnostics: `KR8601`, `KR8602`, `KR8603`

Evidence: `TestSegmentExportsRenderMode`, `TestDataCacheUsageInference`, `TestRevalidateHostAuth`, `TestRevalidateHostResolvesPathsAndFansOut`, `TestSSRInternalProxyLockdown`, `TestRevalidateTagAndPathE2E`, `TestRevalidateFansOutToWorkersE2E`, `TestActionRevalidationReadYourWritesE2E`, `TestAPIRouteRevalidationAndDataCacheE2E`, `TestQuickJSRouteUsesHostCache`

### Compiled React packages

**Source:** n/a - **Islands:** Beta - **SSR:** Beta

Radix and other compiled packages run as Preact islands with live props, callbacks, portals, controlled primitives, per-island chunks, server-rendered initial markup, hydration, and an island error boundary.

Evidence: `TestIslandSignalPropBridge`, `TestIslandLocalComponent`, `TestIslandSSRWithHooks`, `TestIslandSSREscapesTextProps`, `TestIslandSSRUnknownPropFallsBackClientOnly`, `TestIslandSSRModuleFailureFallsBackClientOnly`, `TestIslandSharedSignalAcrossIslands`, `TestIslandSSRArrayObjectProps`, `TestIslandLocalNamedImport`, `TestIslandPerIslandChunks`, `TestIslandChunksMinified`, `browser:/react-demo/`

### shadcn-style dashboard

**Source:** Beta - **Islands:** n/a - **SSR:** n/a

Copied shadcn source components, cva/cn folding, cards, controlled filters, and derived lists run in the browser corpus.

Evidence: `TestShadcnButtonFoldsVariantsAndSpread`, `browser:/react-dashboard/`

### Next.js app corpus

**Source:** Beta - **Islands:** n/a - **SSR:** n/a

The benchmark Next App Router fixture is the migration-shaped corpus. It is source-corpus coverage today, not a browser-run Krate app.

Evidence: `fixture:packages/benchmark/fixtures/next`

### Local components in islands

**Source:** n/a - **Islands:** Beta - **SSR:** n/a

Imported local React-compatible modules can be used as tags inside a compiled island and are bundled with it. Same-file Krate functions are diagnosed because they cannot cross the Krate/Preact runtime boundary.

Diagnostics: `KR8007`

Evidence: `TestIslandLocalComponent`, `browser:/react-demo/`, `TestIslandSameFileLocalWarning`

### npm React hook libraries (zustand, react-hook-form, SWR, TanStack Query, ...)

**Source:** n/a - **Islands:** Partial - **SSR:** Beta

A user module that calls a hook of an npm package built on React (or a store hook made by its factory, or a custom hook wrapping one) is bundled for Preact (react resolves to preact/compat, CommonJS require of react is bridged), rendered on the server and hydrated in place. The islands of one page share one chunk, so a store module is one store. Checked by hand in Chromium against zustand 5, valtio 2, jotai 2, react-redux 9 with Redux Toolkit 2, react-hook-form 7, SWR 2, TanStack Query 5, TanStack Table 8, react-i18next 15 and usehooks-ts 3; the repository tests use stand-in packages with the same import shapes.

Known limits:

- A layout, template or server-rendered page cannot host these hooks; the build fails with KR8013.
- Providers wrapped around children in a layout do not reach islands in pages; put the provider in the island module.
- Store state is per page: it does not survive client-side navigation.
- A page that is itself such an island cannot take route props (KR8013) or use Krate-only APIs in the same module.
- Pages rendered per request (dynamic, ISR, SSR) were not verified with these islands.
- Only Chromium was run for the browser check.

Diagnostics: `KR8013`

Evidence: `TestReactHookLibraryPageIsland`, `TestReactHookLibraryComponentIslands`, `TestReactHookLibraryPageForms`, `TestReactLibraryValueInPageMakesIsland`, `TestReactIslandModulesAreNotKrateSource`, `TestReactHookLibraryInLayoutDiagnostic`, `TestPreactCompatChunkHasDefaultExport`, `TestBrowserReactLibraryIslands`

### CSS-in-JS (styled-components, emotion, MUI)

**Source:** n/a - **Islands:** Partial - **SSR:** Partial

Components built with styled-components, @emotion/styled, the emotion css prop (with the jsxImportSource pragma) or MUI run as Preact islands and are styled in the browser. They are client-only because the styles are created while rendering, so the server HTML holds an empty placeholder (KR8008 notice). A css prop without emotion's runtime is reported (KR8014). Checked by hand in Chromium against styled-components 6, @emotion/styled and @emotion/react 11 and MUI 9.

Known limits:

- No server-rendered markup or styles: content appears when the island mounts.
- A project-wide jsxImportSource for emotion in tsconfig is not supported; use the per-file pragma.
- A ThemeProvider around the whole app in a layout does not reach islands.

Diagnostics: `KR8008`, `KR8014`

Evidence: `TestReactCSSInJSIsland`, `TestBrowserReactLibraryIslands`

### Island hydration control (client:visible, client:idle)

**Source:** n/a - **Islands:** Beta - **SSR:** n/a

client:visible and client:idle on an island's root tag defer mounting until the island is near the viewport or the browser is idle. The server markup stays; the chunk is neither preloaded nor fetched before then, and the directive never reaches the component. client:load is the default.

Diagnostics: `KR8012`

Evidence: `TestIslandClientDirectives`, `TestClientDirectiveOnNonIslandWarns`, `TestBrowserReactLibraryIslands`

### SSR island hydration

**Source:** n/a - **Islands:** Beta - **SSR:** Beta

Islands with build-time-known props are server-rendered with Preact and hydrated in place. Each island has its own chunk; unknown props, browser-only components, and portals fall back to client-only with a warning. Request-time SSR and streaming island regions remain separate work.

Diagnostics: `KR8008`, `KR8009`

Evidence: `TestIslandSignalPropBridge`, `TestIslandSSRWithHooks`, `TestIslandSSREscapesTextProps`, `TestIslandSSRUnknownPropFallsBackClientOnly`, `TestIslandSSRModuleFailureFallsBackClientOnly`, `TestIslandSharedSignalAcrossIslands`, `TestIslandSSRArrayObjectProps`, `TestIslandLocalNamedImport`, `TestIslandPerIslandChunks`, `TestIslandChunksMinified`, `browser:/react-demo/`, `TestIslandBundleFailureUsesKR8009`

### Supported Preact versions

**Source:** n/a - **Islands:** Beta - **SSR:** n/a

The island build warns below 10.20. The conformance jobs exercise the minimum, an intermediate version, and the workspace version.

Evidence: `ci:preact-conformance`

### Browser differential corpus

**Source:** Beta - **Islands:** n/a - **SSR:** n/a

124 source-component fixtures (React 18 corpus plus the React 19 reference app) run in real React 19 and in Krate in Chromium, Firefox, and WebKit; DOM, form state, focus, and effect/lifecycle/ref ordering are compared after every step. Known divergences are tracked with a severity, and KRATE_REACT_EXIT=1 fails while P0/P1 entries remain.

Known limits:

- Four P2 render-count/useId formatting differences remain documented in testdata/reactbrowser/divergences.json.

Evidence: `TestReactBrowserDifferential`, `TestReactBrowserCorpus`, `ci:browser-conformance`

### React 19 reference app

**Source:** Beta - **Islands:** n/a - **SSR:** n/a

Fixtures for useActionState, useOptimistic, useFormStatus, form and button actions with auto-reset, use(promise) and use(context), ref as a prop, ref cleanup callbacks, async transitions, useDeferredValue initial values, document metadata, and server actions under the real server.

Evidence: `TestReactBrowserDifferential`, `fixture:packages/compiler/internal/build/testdata/reactbrowser/r19/app_reference.tsx`

### Serverless and Node deployment

**Source:** n/a - **Islands:** n/a - **SSR:** Beta

krate deploy bundles the portable server handler (SSR, ISR, streaming, async server components, JS/TS API routes, server actions, middleware, redirects, rewrites, headers) into one self-contained module for --target node, vercel, netlify and cloudflare. Per-segment not-found and error pages and middleware on prerendered pages work on every target. A request matrix gives the same status, security and cache headers, redirects and bodies as krate start; Go API routes, Go components and sidecars are refused at deploy time.

Known limits:

- ISR, region and data caches are per instance on serverless targets; revalidatePath/revalidateTag only reach the instance that handled the request (reported under skipped). Pass an isrStore to createKrateHandler to share the page cache.
- With middleware, Vercel and Netlify send every page request to the function, which serves prerendered HTML from the bundle (bundle size grows with static pages); assets keep going to the CDN. A public file without an extension is treated as a page there.
- Legacy (req, res) API handlers, Go API routes, Go components, the API sidecar and community plugin serve hooks are not supported on serverless targets.
- Cloudflare bundles fail at build time for Node built-ins outside nodejs_compat (fs, vm, net, child_process).

Evidence: `TestServerlessNodeParity`, `TestVercelServerOutput`, `TestNetlifyServerOutput`, `TestCloudflareServerOutput`, `TestServerBundleHasNoSecrets`, `TestServerlessRefusesGoAndSidecars`, `TestCloudflareRefusesNodeBuiltins`, `TestServerlessNodeBrowser`, `TestPlatformDeploysKeepSegmentPages`, `TestNetlifyAndVercelWithoutMiddlewareStayStaticFirst`

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
