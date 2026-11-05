---
title: Diagnostic Codes
description: Every stable KRxxxx diagnostic code the Krate compiler reports, with an explanation.
order: 5
---

<!-- Generated from internal/diag by TestDiagnosticsDocUpToDate. Do not edit by hand; run
     KRATE_DIAG_DOCS_UPDATE=1 go test ./internal/diag from packages/compiler. -->

# Diagnostic Codes

Krate diagnostics that point at a known compatibility limit carry a stable
code such as `KR2001`. The code appears in the message
(`file.tsx:3:5: [KR2001] ...`), never changes meaning once released,
and is listed here with the reason it is reported and what to do instead.

Codes are grouped by area:

| Range | Area |
|-------|------|
| `KR1xxx` | syntax and parsing |
| `KR2xxx` | React effects, dependency arrays and hooks |
| `KR3xxx` | class components and error boundaries |
| `KR4xxx` | Suspense, lazy, use() and transitions |
| `KR5xxx` | events, forms and refs |
| `KR6xxx` | lists and keys |
| `KR7xxx` | React 19 actions and server actions |
| `KR8xxx` | islands, compiled packages and general React compatibility |
| `KR9xxx` | routing, segment pages and rendering errors |

## KR1xxx: syntax and parsing

No codes are registered in this range yet.

## KR2xxx: React effects, dependency arrays and hooks

### KR2001

**Dependency array is not statically resolvable**

The dependency list is a binding the compiler cannot resolve to an array literal in the same component (for example a parameter or an imported value). It is compared element by element (Object.is) whenever it is re-read, but Krate components run once, so a captured array never changes and the hook runs only on mount. Write the array inline, e.g. useEffect(fn, [a, b]), or bind it with const deps = [a, b] in the component body.

### KR2002

**Effect without a dependency array**

React runs such an effect after every render. Krate has no re-renders, so the effect re-runs after every commit that changes the component's own state (useState/useReducer) or its props, which is when React would have re-rendered it because of its own state or props. Re-renders caused only by a parent or a context are not modelled. Pass an explicit dependency array to make the trigger explicit, or [] to run once.

### KR2003

**Async effect callback**

An async function returns a Promise, not a cleanup function, so its cleanup is ignored (React warns about this too). Declare an async function inside the effect and call it, returning a synchronous cleanup.

### KR2004

**useCallback without a dependency array**

React returns a new function on every render when the dependency array is omitted. Krate components run once, so the callback keeps one identity for the component's lifetime; effects or memos that list it as a dependency do not re-run. Pass the dependency array the callback reads.

### KR2005

**useMemo without a dependency array**

React recomputes such a memo on every render. Krate recomputes it when a signal it reads changes, which matches whenever the value depends only on state and props. Pass the dependency array to make recomputation explicit.

### KR2006

**Dependency argument is not an array**

The second argument of useEffect/useLayoutEffect/useInsertionEffect/useMemo/useCallback must be an array (React warns otherwise). The value is ignored and the hook is treated as having no dependency array. Pass [a, b] or omit the argument.

### KR2008

**Hook called inside a condition or loop**

React requires hooks to be called in the same order on every render (the Rules of Hooks), so a hook inside an if, switch or loop is undefined behaviour in React (it throws when the hook count changes). Krate runs the component once and would bind whatever the first run created. Call the hook unconditionally at the top level and branch on its result, or move the repeated or conditional part into a child component that calls the hook.

## KR3xxx: class components and error boundaries

### KR3001

**Class component has no render method**

A React class component must define render(). Without it Krate cannot lower the class and the component renders nothing. Add render() returning JSX, or convert the class to a function component.

### KR3002

**Unknown instance property on a class component**

The class reads this.<name>, but <name> is not a field, method, accessor, props, state, context, setState or forceUpdate, and nothing assigns it. Lowered classes have no React base-class internals (this.refs, this.updater, this.isMounted). Declare the property as a field or method, or read it from props or state.

### KR3003

**Unsupported static class member**

Only defaultProps, propTypes, displayName, contextType, getDerivedStateFromProps and getDerivedStateFromError are lowered. Other static fields, methods and static blocks have no instance equivalent after lowering; move them to module scope next to the component.

### KR3004

**Computed class member name**

Class members named with a computed key ([expr]) cannot be resolved at build time. Give the member a plain name.

### KR3005

**componentWillMount is not supported**

UNSAFE_componentWillMount (and componentWillMount) runs before the first render, which Krate performs at build time. Initialize state in the constructor or a state field, and move side effects to componentDidMount. componentWillReceiveProps and componentWillUpdate (and their UNSAFE_ forms) are supported.

### KR3006

**Legacy class context is not supported**

static contextTypes and childContextTypes (the pre-16.3 context API) are not lowered. Use static contextType = MyContext and this.context, or a function component with useContext.

### KR3007

**this inside a static lifecycle**

getDerivedStateFromProps and getDerivedStateFromError are static: React calls them without an instance and Krate lowers them to plain functions. Use their parameters instead of this.

### KR3008

**Error boundary without getDerivedStateFromError**

A class with componentDidCatch but no static getDerivedStateFromError catches errors, but React has no state update to render a fallback with: the failed subtree is unmounted and its last DOM stays on the page until componentDidCatch calls setState. Add getDerivedStateFromError returning the fallback state.

### KR3009

**Error boundary rendered its fallback at build time**

A component inside an error boundary threw while the page was built. Krate rendered the boundary's fallback (the state returned by getDerivedStateFromError) instead of failing the build, and calls componentDidCatch when the page hydrates. Fix the error, or keep the fallback if it is expected.

### KR3010

**Component threw while the page was built**

A component threw (a throw statement ran) while Krate rendered the page at build time, and no error boundary encloses it, so the build fails. Fix the error, guard it with a condition, or wrap the component in an error boundary to render a fallback instead.

### KR3011

**Class component is not lowered**

Krate lowers class components declared at module top level (`class X extends Component`, exported or not). A class expression or a class declared inside a function is not lowered and renders nothing. Move the declaration to module scope or convert it to a function component.

## KR4xxx: Suspense, lazy, use() and transitions

### KR4001

**lazy() loader is not a static import()**

Krate code-splits lazy components at build time, so the loader must be `() => import("./Component")` with a string literal path (optionally `.then((m) => ({ default: m.Named }))`). Computed paths and other loaders cannot be resolved.

### KR4002

**lazy() is not declared at module level**

Declare lazy components once at the top level of a module (`const Chart = lazy(() => import("./Chart"))`). React re-creates a lazy component declared inside a component on every render, and Krate cannot split it.

### KR4003

**use(promise) is not assigned to a variable**

Krate lowers use(promise) to a reactive value, so it must initialize a declaration in the component body: `const user = use(userPromise)`. The call may be conditional inside that initializer. use(Context) works anywhere.

### KR4004

**use(promise) result is destructured**

The value of use(promise) is reactive and is read through its variable. Assign it first and read fields from it: `const user = use(p); user.name`.

### KR4005

**<Profiler onRender> is never called**

Krate components do not re-render, so there are no commit timings to report. <Profiler> renders its children; measure with the browser performance tools instead.

### KR4006

**lazy() of a package is not code-split**

Only local modules are split into separately loaded hydration chunks. A lazy package import is compiled like a static import of that package.

### KR4007

**lazy() module has no default component export**

The module passed to lazy() must `export default` a function component, or the loader must map a named export: `import("./X").then((m) => ({ default: m.Named }))`.

### KR4008

**Component suspends outside a <Suspense> boundary**

A component reads use(promise) or a resource but no <Suspense> encloses it, so there is no fallback to show while it loads and its bindings stay empty until then. Wrap it in <Suspense fallback={...}>.

## KR5xxx: events, forms and refs

### KR5001

**Unknown event handler prop**

React ignores on* props on built-in elements that are not React DOM events (and warns). Krate still attaches a native listener for the lowercased event name. Fix the spelling, or listen for a custom DOM event with ref.current.addEventListener in an effect.

### KR5002

**Lowercase event handler prop**

React event props are camelCase (onClick, onChange). A lowercase prop such as onclick is not an event handler in React and the function is never called. Use the camelCase name. `krate fix` rewrites unambiguous lowercase handlers; string-valued inline handlers are left alone.

### KR5003

**String ref**

String refs (ref="name") were removed in React 19 and are ignored. Use useRef or createRef and pass the object (ref={inputRef}), or pass a callback ref.

### KR5004

**Controlled form field without onChange**

A form field with value (or checked) is controlled: after every edit the DOM is restored to the prop, so without an onChange handler it is read-only (React warns). Add onChange, use defaultValue/defaultChecked for an uncontrolled field, or mark it readOnly.

### KR5005

**Both value and defaultValue**

A form field must be either controlled (value/checked) or uncontrolled (defaultValue/defaultChecked), not both. The default is ignored; remove one of the props.

### KR5006

**Form field switches between controlled and uncontrolled**

value={cond ? x : undefined} makes the field uncontrolled while the expression is undefined and controlled otherwise; React warns when it switches. Use an empty string (or false for checked) instead of undefined so the field stays controlled.

### KR5007

**onChange on a contentEditable element**

React's onChange only fires for input, textarea and select. A contentEditable element never receives it; listen with onInput instead.

### KR5008

**ref.current read during render**

React shows the ref's value as of the latest render, so a later state update reveals a mutated ref.current. Krate components render once and bind only state, so a ref read in JSX is shown once and never refreshed. Keep values the UI shows in state, or read the ref in an effect or event handler.

## KR6xxx: lists and keys

### KR6001

**Each child in a list should have a unique key prop**

A list whose items can change at runtime (its array reads a signal, state or a runtime value) returns elements without a key, so rows are matched by index. When items are reordered, inserted or removed, row state (input values, focus, component state, effects) stays with the position instead of following the item, exactly as in React. Add a stable, unique key from the item, for example <li key={item.id}>. Lists over build-time constants are not reported.

### KR6002

**Encountered two children with the same key**

Two items of a list resolve to the same key at build time. Keys must be unique among siblings so each row keeps its identity. The runtime also logs a console error when it sees duplicates; it keeps the first occurrence on its existing row and renders each duplicate as a separate row. Use an id that is unique per item.

### KR6003

**List callback keys only some of its elements**

A map callback returns a keyed element on some paths and an unkeyed one on others (for example a ternary with a key on one branch). Unkeyed rows fall back to their index, which mixes identities. Put the same key on every returned element.

## KR7xxx: React 19 actions and server actions

### KR7001

**Inline "use server" function**

React allows a "use server" directive at the top of an async function inside a component. Krate only supports file-level server actions: move the function into a module whose first statement is "use server", export it, and import it where the form uses it.

### KR7002

**useFormStatus() component does not render an element**

Krate finds the form a useFormStatus() call belongs to from the DOM element the component returns. When the component returns another component, a string, or nothing, the status cannot be connected and always reads as idle. Return a DOM element (for example the submit <button>).

### KR7003

**useActionState initial state is not known at build time**

A form whose action is a useActionState dispatch around a server action still posts to the action URL without JavaScript. The previous state for that submission is the initial state, encoded into the URL at build time; when it cannot be evaluated the server receives null.

### KR7004

**"use server" module exports a non-function**

Every export of a "use server" module becomes a public endpoint and must be an async function.

### KR7005

**"use server" module re-exports another module**

Re-exports cannot be bundled as server actions. Import the function and re-declare it as an export.

### KR7006

**Imported server action does not exist**

The import names an export that the "use server" module does not declare.

### KR7101

**Async layouts are not supported**

Layouts are baked into the static page shell at build time, so they cannot await data. Keep the layout synchronous and move the data fetching into an async child component (a server component), which renders per request.

### KR7102

**`await` cannot run at build time**

Only async components (server components) can await: they render per request in the SSR sidecar. Code that Krate evaluates at build time, such as module-level constants a static component renders or a non-async component body, cannot await a Promise. Mark the component `async` or compute the value without awaiting.

### KR7103

**Server components need request-time rendering**

An async component renders per request, but `output: "static"` disables the SSR sidecar, so the page cannot render it. Remove `output: "static"` or make the component synchronous.

### KR7104

**Page with server components is rendered per request**

A page that contains an async component cannot be fully baked at build time. Krate bakes the rest of the page as a static shell and renders each server component per request (streaming mode). This is informational.

### KR7105

**Server component props must be known at build time in a static page**

A server component used from a synchronous page renders as its own request-time region, so its props are fixed when the page shell is built: they must be literals, module constants or route params, and it cannot receive children. Make the page itself `async` (the whole page then renders per request) or pass constant props.

### KR7110

**Client-only API used in a server component**

Server, static and runtime components, and the components an async component renders, render once (at build time or per request) and ship no JavaScript, so hooks such as useState/useEffect, Krate signals, class state and lifecycles, and event handlers cannot work there; they used to build and leave a page that did nothing. Remove the server tier from the component (the `// @server` comment or decorator, the `.server` file name or the config entry) to make it a client component, or move the interactive part into a client child component (one with no tier, or marked `@client`, or a module that starts with `"use client"` under an async component) and render that from the server component; it is server-rendered and then hydrated in the browser.

### KR7111

**Function prop passed from a server component to a client component**

Props crossing from a server component to a `"use client"` component are serialized to JSON. Functions (other than server actions), class instances and symbols cannot be sent. Define the handler inside the client component, or pass a server action. The same check runs at request time for values computed while rendering.

### KR7112

**Client component imports a server-only module**

A module that imports `server-only` must never reach the browser, but a `"use client"` module (directly or through its imports) imports it, so it would be bundled into the client chunk. Import it from a server component instead and pass the data as props.

### KR7113

**Async component in a client module**

Components in a `"use client"` module run in the browser and cannot be async. Move the async component to a module without the directive (it becomes a server component).

### KR7114

**Client module exports cannot be listed**

Krate wraps each export of a `"use client"` module in a client boundary, so it must know the export names. `export * from` cannot be enumerated: list the re-exported names explicitly (`export { A, B } from "./x"`).

### KR7115

**Tier decorator cannot apply here, or two tiers conflict**

`@server`, `@static`, `@runtime` and `@client` go on a function, class or const declaration, once per declaration. An async component always renders per request, so only `@runtime` fits it. A runtime component is bundled per file, so `@runtime` must be on the default export or the only exported component of its file. A component also has one tier: the leading `// @tier` comment, the `.server`/`.runtime`/`.static` file name, the `serverComponents`, `runtimeComponents`, `serverDirs` and `runtimeDirs` config entries, a decorator and a `"@tier"` string at the top of a function body must agree. A decorator on a component that is not the file's default export or only exported component may override the file's tier (`@client` in a `// @server` file); on the default export it must match. See "Tiers that disagree" in Component Tiers.

### KR7116

**One name imported from two modules**

Two modules export the same name and a page imports it from both, for example `import { Card as A } from "./a"` and `import { Card as B } from "./b"`. Modules on a page share one namespace, so Krate gives one of the exports a private name and rewrites the imports to match, and this is not reported. It is reported when neither module can be renamed because something reads export names from source: a re-export (`export { Card } from`, `export *`, or an import followed by an export), a namespace import (`import * as ns`), an `import()`, or a "use client" or "use server" module. Both imports then resolve to one declaration and the page shows the wrong output. Rename one of the exports. Private names and default exports do not have this problem. This is a warning.

### KR7120

**API route file in the pages directory**

API routes are served from src/api, not from the pages directory. A `.ts` or `.js` file under src/pages/api used to build into an empty page while /api/... answered 502. Move the file to src/api/. This is a warning.

### KR7121

**Prerendered endpoint failed**

A file such as `src/pages/data.json.ts` is a prerendered endpoint: its `GET` export runs once at build time and the response body is written to the output as a file (`data.json`). The build fails when the module has no `GET` export, `GET` throws, answers a non-2xx status, or the name has a dynamic segment. Endpoints run on Node (or the configured runtime) with the project's environment. For a response that depends on the request, use an API route in `src/api`.

### KR7122

**Page metadata could not be evaluated at build time**

A static page's `export const metadata` and `generateMetadata` (and those of the layouts above it) run at build time, once per `generateStaticParams` entry, and become the document `<title>`, description, canonical link, Open Graph and Twitter tags. This one could not run: it threw, it needs something the build does not have (a browser global, a service that is down), or the module could not be loaded. The segment's tags were left out and the page still builds. Fix the error, or render the page per request (`export const dynamic = "force-dynamic"`, or `revalidate = N`) so `generateMetadata` runs with the request. This is a warning.

### KR7123

**Sass, Less and Stylus files are not compiled**

Krate compiles CSS (including CSS modules and Tailwind) but has no Sass, Less or Stylus compiler, so importing a `.scss`, `.sass`, `.less` or `.styl` file would ship no styles at all. Compile the file to CSS first (for example with the `sass` CLI in a prebuild script) and import the `.css` result, or port the styles to CSS or Tailwind. This is an error.

### KR7124

**Name is not exported by @krate/runtime**

A named import from `@krate/runtime` (or one of its subpaths) asks for something the package does not export. The import is removed from the page, so the name would be undefined in the browser and fail with a ReferenceError. Check the spelling against the runtime API reference; names from other frameworks (`signal`, `usePathname`) have Krate equivalents such as `createSignal` and `getNavigationState`. This is an error.

### KR7125

**tsconfig baseUrl and paths disagree**

`compilerOptions.paths` targets resolve against `baseUrl`, in tsc and in Krate. A `baseUrl` of `./src` together with `"@/*": ["./src/*"]` points at `src/src`, so every `@/` import fails with "cannot resolve import ... (matched tsconfig path alias)". Krate 0.4 ignored `baseUrl` for aliases, so a project scaffolded then still has it. `krate fix --apply` sets `baseUrl` to `"."` when every target repeats the baseUrl directory, exists under the project root and does not exist under `baseUrl`. Otherwise edit `tsconfig.json` by hand. This is not a build diagnostic: it is reported by `krate fix`.

### KR7126

**Imported asset is used where its URL cannot be inlined**

`import logo from "./logo.png"` binds the file's hashed URL. The compiler replaces reads of the binding with that URL, in the module that imports the file and in every module that imports it through `export { logo }`, `export default logo`, `export { default as logo } from "./logo.png"`, `export *` or `export * as ns` and `ns.logo`. A read it cannot reach, such as a computed property key (`{ [logo]: 1 }`) or `ns.default` of a namespace that is passed on whole, has no value in the browser script: it is a ReferenceError there, or renders as the bare name. Use the binding as a plain value in JSX or a component, or import the file with a `?url` suffix. This is a warning.

### KR7130

**Metadata route could not be built**

A `sitemap.ts`, `robots.ts`, `manifest.ts`, `opengraph-image` or `twitter-image` file under the pages directory runs once at build time and its result becomes `sitemap.xml`, `robots.txt`, `manifest.webmanifest` or a PNG. The file threw, returned a value of the wrong shape (a sitemap entry without a `url`, a robots file without `rules`), or could not be loaded. Fix the error named in the message. This is an error: the file would otherwise silently produce nothing.

### KR7131

**ImageResponse uses something the renderer cannot draw**

ImageResponse draws a documented JSX subset with a built-in renderer (no headless browser): flexbox layout, text, colours, gradients, borders, box shadows, opacity, overflow hidden and images. Anything else (an element such as `<svg>`, a style such as `transform` or `filter`, a unit such as `vh`, a character the font has no glyph for) is listed here instead of being drawn wrong. Use the supported subset, or pass a font with `options.fonts` for other characters. This is an error.

### KR7132

**Image route in a dynamic segment has no params to build with**

An `opengraph-image` or `twitter-image` file inside a dynamic segment (`blog/[slug]/`) is built once per entry of the segment page's `generateStaticParams`. This one has no matching page or no `generateStaticParams`, so no image was built and no `og:image` tag is emitted for it. Export `generateStaticParams` from the segment's page. This is a warning.

### KR7133

**A metadata file replaces a plugin or config output**

`sitemap.ts` or `robots.ts` defines the file, so the sitemap or robots plugin (and the `robots` and `seo` config that feed it) did not write its own. The metadata file wins. Remove one of them to silence this warning. This is a warning.

### KR7150

**Unknown code theme**

`markdown.codeTheme` names a syntax theme that Krate does not ship, so code is highlighted with the default theme for that colour mode instead (`github` for light, `github-dark` for dark). Use a chroma style name such as `github`, `github-dark`, `monokai`, `dracula` or `nord`. Give one name to use it in both modes, or `{ light: "github", dark: "github-dark" }` to pick a theme per mode. This is a warning.

### KR7151

**Language cannot be highlighted after the build**

Code that is highlighted in the browser (a `<Code>` whose text changes at run time) or at request time is coloured by a small tokenizer that runs chroma's lexers exactly, and it ships only for a fixed list of languages. The language used here is not on it, so the code is shown as plain text. Build-time highlighting is unaffected and supports every chroma language. Use one of the listed languages (`markdown.codeHighlight` docs), or make the code known at build time. This is a warning.

## KR8xxx: islands, compiled packages and general React compatibility

### KR8001

**React API removed in React 19**

The code uses an API that React 19 removed: findDOMNode, ReactDOM.render, ReactDOM.hydrate, unmountComponentAtNode, createFactory or propTypes checking. Krate follows React 19 semantics, so these have no equivalent. Use a ref instead of findDOMNode, and let Krate mount pages instead of calling render or hydrate yourself.

### KR8002

**React API not supported in source components**

A React API used in a source component has no lowering to Krate signals, so the component would render differently from React. Rewrite the component with a supported pattern (see the React compatibility guide), or move it into a compiled React package so it runs as an island with full React semantics.

### KR8003

**Component renders nothing because it could not be compiled**

Krate could not compile a component (for example an unresolved component name or an unsupported class feature) and emitted an empty placeholder where React would render content. Fix the reported cause; the placeholder is never a valid substitute for the component's output.

### KR8004

**Context consumer child is not a single render function**

`<Ctx.Consumer>` renders the result of calling its child with the nearest provider value, so its only child must be one function such as `{(value) => <span>{value}</span>}`. Krate inlines that function at build time; it also needs a plain parameter (or a `{ a, b }` pattern without renames or defaults). Read the value with `useContext(Ctx)` in a component when the render function is more involved.

### KR8005

**Legacy context API**

A class component uses legacy context (`contextTypes`, `childContextTypes` or `getChildContext`). React 19 removed it, and Krate does not pass legacy context: `this.context` is empty. Create a context with `createContext`, provide it with `<Ctx value={...}>` and read it with `static contextType = Ctx` or `useContext(Ctx)`.

### KR8006

**Portal container cannot be resolved**

`createPortal(children, container)` moves its children into a DOM node after hydration. Krate resolves `document.body`, `document.getElementById("id")` and `document.querySelector("selector")` with literal arguments, and any other expression in the hydration scope of a client component; elsewhere the content mounts into `document.body`, and a container that is not an element when the page hydrates keeps the content unmounted. Make sure the container exists before the component hydrates (for example a `<div id="modal-root">` in the layout).

### KR8007

**Same-file component inside an island**

An external package island contains a component declared in the page module. The page component belongs to the Krate runtime and cannot run inside the island's Preact tree. Move the component into its own module and import it, or render it outside the island.

### KR8008

**Island stays client-only**

The island has no build-time props, uses a browser-only API, or could not be rendered by Preact during the build. Its placeholder is emitted without server-rendered markup and the client mounts it after the island chunk loads. Pass build-time initial values or keep the island client-only intentionally.

### KR8009

**Island asset could not be built**

Krate could not bundle the island's external package, local module, Preact dependency, or generated chunk. Check the reported resolver error and install the missing package (Preact 10.20 or newer) in the project.

### KR8010

**createPortal is not in a child position**

Krate lowers `createPortal(children, container)` when it is a child (`{createPortal(...)}`) or a branch of a conditional child (`{open && createPortal(...)}`, `{open ? createPortal(...) : null}`). Anywhere else the call is left as is and fails in the browser because `createPortal` is not a global. Render the portal as a child, or use `<Portal container={...}>`.

### KR8011

**Expression has no build-time value and no client binding**

The compiler could not compute the value of this expression while building the page (it calls a function it cannot see into, reads an unsupported built-in, or depends on data that only exists at runtime) and the component has no client code that could fill it in, so the page renders it as empty text. Move the value into component state or an effect, compute it from values the build can see, or make the component interactive so the browser evaluates it.

### KR8012

**Client directive on a component that is not an island**

`client:visible`, `client:idle` and `client:load` choose when an island mounts, and only apply to the root tag of a compiled package component (or a module that uses a React library) rendered as an island. On a Krate component or an HTML element they do nothing; Krate hydrates those with the page. Remove the directive, or put it on the package component itself.

### KR8013

**React library hook used where no island can run it**

A component calls a hook of an npm React library (zustand, react-hook-form, swr, @tanstack/react-query, ...). Those hooks need React's render model, which Krate's lowered components do not have, so Krate runs the module that calls them as a Preact island. That is not possible for a layout or template, or for a server or runtime tier component. Move the component into its own module and render it from a page or client component, or replace the library with createSignal / createResource.

### KR8014

**css prop is emotion's, not a DOM attribute**

A `css={...}` prop on a DOM element is understood only by emotion's JSX runtime. In a module that does not use it Krate renders `css` as a plain attribute, so the styles are lost. Add `/** @jsxImportSource @emotion/react */` to the top of the file so the module runs as a Preact island with emotion, or use CSS modules, Tailwind or a stylesheet.

### KR8015

**Layout provider cannot reach the islands of its pages**

A layout wraps `{children}` in a component of a React library (a `QueryClientProvider`, a `ThemeProvider`, a Redux `Provider`). Islands run in Preact, so the layout cannot give them context. Krate lifts such wrappers into a module every island of the page renders inside, but only when their props can be computed without the layout's own state: constants, imports, module-level values, and `useState(() => value)` or `useMemo(() => value, [])` with no setter in use. A prop that reads the layout's `params`, a signal or a state setter cannot be lifted. Build the value from constants, or wrap the island's own module in the provider.

### KR8016

**Layout provider renders markup around its children**

A component a layout wraps around `{children}` renders elements of its own, so it is not a context provider that can be moved into the islands: Krate renders the page content, and an island cannot sit inside markup that belongs to the layout. The layout keeps the component as it was and the islands of its pages do not see its context. Split the component into a provider that renders only its children and the markup, or put the provider inside the island's own module.

### KR8030

**Higher-order component cannot be resolved at build time**

`const Page = withX(Inner)` (or `export default withX(Inner)`) is compiled by inlining the component function that `withX` returns, with its parameters replaced by the arguments. That works when `withX` is a function in the project whose body holds only that returned component function. A wrapper imported from a package, or one with other statements around the returned function, cannot be inlined, so the component would render nothing; the build fails instead. Declare the wrapper in your source as `function withX(C) { return function Wrapped(props) { return <C {...props} /> } }`, or write the wrapped component out by hand.

### KR8031

**Prop of Activity, ViewTransition or SuspenseList is not supported**

`<Activity>` supports `mode="visible"` and `mode="hidden"`. `<ViewTransition>` supports `name`, `default`, `enter`, `exit`, `update` and `share` as strings, and ignores the event callbacks (`onEnter`, `onExit`, `onUpdate`, `onShare`). `<SuspenseList>` renders its children and ignores `revealOrder` and `tail`. Other props have no effect and are reported once per element. This is a warning: the children always render.

### KR8301

**Render value keeps its first value**

React recomputes every value in a component body on each render. Krate compiles a body value that reads state, props, context or a custom hook result into a derived value that is recomputed when its inputs change. This value could not be compiled that way: it is reassigned inside a closure (an event handler or effect), or by a body statement that also writes another variable, or it is read between the statements that build it. It keeps the value it had when the component hydrated. Compute it in a single expression (const label = n > 1 ? "items" : "item"), or keep the changing value in state.

### KR8302

**Imported value is not available to hydration code**

Hydration code (an event handler, effect, derived value, text binding or a module-level initializer) uses a binding imported from another local module, and the compiler could not copy that module's declaration into the page's hydration script. Functions, classes, constants, lowered namespaces, namespace imports and JSON, ?raw and ?url imports are copied; npm packages are bundled for the browser. A binding created by a destructuring export, or one that only a side effect initializes, cannot be copied, so at runtime the name would be undefined. Export it as a function, class or plain const, or define it in the component's module.

### KR8310

**Environment variable is not exposed to client code**

Client code and statically rendered pages cannot read process.env or import.meta.env values: they are build and server-time only and are never written into HTML or JavaScript. The read yields undefined. Name the variable with the KRATE_PUBLIC_ prefix (KRATE_PUBLIC_API_URL) to inline its value at build time, or read it in a server component, API route or server action instead. import.meta.env.MODE, DEV, PROD, BASE_URL and SSR are always available.

### KR8601

**Unsupported page segment export value**

Krate reads the Next.js-style page exports `export const revalidate` (a constant number of seconds, 0 for per-request rendering, or false to cache indefinitely) and `export const dynamic` ("auto", "force-dynamic", "force-static" or "error") at build time. The value could not be read statically or is not one of the supported values, so it is ignored. Write a literal value.

### KR8602

**Static page reads revalidating data**

The page reads data with cache tags, a revalidate window or `cache: "no-store"`, but it is rendered statically (it is pinned with `dynamic = "force-static"`, `revalidate = false`, or the project uses `output: "static"`). Static HTML is baked once at build time: revalidateTag, revalidatePath and revalidate windows cannot refresh it. Remove the static pin or export `revalidate = N` so the page is served with ISR.

### KR8603

**revalidatePath targets a static page**

A revalidatePath() call names a route that is rendered statically (SSG). The page's HTML is a file baked at build time, so revalidation cannot rebuild it: at runtime the call reports the route under `skipped` and logs a warning. Make the target page ISR (`export const revalidate = N`) or SSR (`export const dynamic = "force-dynamic"`), or rebuild the site to refresh it.

### KR8604

**forbidden() or unauthorized() never runs per request**

forbidden() and unauthorized() answer one request with a 403 or 401 page, so they only work in code that runs on every request: a page rendered with ssr, isr or streaming, an async server component, a server action or an API route. This call sits in a statically built page, or in a layout (layouts are baked into the shell at build time), so it never runs and the page is served to everyone. Make the page request-time (`export const dynamic = "force-dynamic"`), or call the function from an async server component that the page renders.

### KR8801

**Image source cannot be used**

An `<Image>` is optimised while the site is built, so its src must name an image file that exists (a path starting with / is relative to the public directory) and can be decoded. Krate reports the problem as a build error instead of shipping a broken image. Fix the path or the file.

### KR8802

**Remote image host is not allowed**

A remote `<Image src="https://...">` is only passed through when its host matches images.remotePatterns in krate.config.ts. Add the host to the allow list, or download the file into the public directory.

### KR8803

**Image prop must be known at build time**

An `<Image>` generates its variants during the build, so src, width, height, quality and priority must be literals (or consts initialised with literals) by the time the page is rendered. A value that only exists in the browser cannot be used. alt, sizes and className may be expressions.

### KR8804

**Image has no alt text**

An `<Image>` without an alt attribute is announced by screen readers by its file name. Add a description, or alt="" when the image is purely decorative. This is a warning.

### KR8810

**Content collection is shipped to the browser**

A `getCollection("name")` chain is normally evaluated while the site is built and only its result is rendered. When the chain depends on a value that only exists in the browser (a signal, an event), the collection has to be evaluated there, so its entries are copied into the page script. Krate ships only the fields the page mentions (the body and rendered html only when referenced), but every entry in the collection, drafts included, is still included. Filter with build-time values, or compute the list on the server. This is a warning.

### KR8820

**Component option value must be a literal**

Tabs, Accordion and the other zero-JS components turn each part's value into CSS while the site is built, so every `value` on their parts must be a string literal in the source. A value read from data (for example `value={item.id}` inside `items.map(...)`) cannot be enumerated: the panels for that value get no show/hide rules, defaultValue is ignored and every panel stays visible. Write the values out, or use `<Select>`/a `createSignal` based component for data-driven options. This is a warning.

## KR9xxx: routing, segment pages and rendering errors

### KR9001

**Conflicting segment pages**

Two files define the same role for one URL directory: 404.tsx and not-found.tsx, error.tsx and 500.tsx, or the same file name in two route groups that resolve to one directory. Krate cannot tell which one to use. Keep one file per role in a directory.

### KR9002

**Segment page in an unsupported directory**

404.tsx, not-found.tsx, error.tsx and 500.tsx are per-directory segment pages. They cannot live inside a parallel-route @slot or an intercepting (.)route directory, because those render inside their parent's layout. Move the file to the parent route directory.

### KR9003

**Segment page ignores request-time rendering**

Segment pages (404.tsx, not-found.tsx, error.tsx, 500.tsx) are prerendered once and written to the output; the server picks the right file per request. A request-time mode (ssr, isr, streaming, runtime components, async server components) on such a page is ignored. Read request data in the page's client code, or link to a normal route.

## See also

- [React Compatibility](/docs/guides/react-compatibility/)
- [Compatibility matrix](/docs/guides/compatibility/)
