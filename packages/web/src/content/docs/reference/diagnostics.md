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

### KR8301

**Render value keeps its first value**

React recomputes every value in a component body on each render. Krate compiles a body value that reads state, props, context or a custom hook result into a derived value that is recomputed when its inputs change. This value could not be compiled that way: it is reassigned inside a closure (an event handler or effect), or by a body statement that also writes another variable, or it is read between the statements that build it. It keeps the value it had when the component hydrated. Compute it in a single expression (const label = n > 1 ? "items" : "item"), or keep the changing value in state.

### KR8302

**Imported value is not available to hydration code**

Hydration code (an event handler, effect, derived value or a module-level initializer) uses a binding imported from another module, and the compiler could not copy that module's declaration into the page's hydration script: the import is a package, a namespace or default import, a class, or a declaration with side effects. At runtime the name would be undefined. Move the helper into a local module and import it by name, or define it in the component's module.

### KR8601

**Unsupported page segment export value**

Krate reads the Next.js-style page exports `export const revalidate` (a constant number of seconds, 0 for per-request rendering, or false to cache indefinitely) and `export const dynamic` ("auto", "force-dynamic", "force-static" or "error") at build time. The value could not be read statically or is not one of the supported values, so it is ignored. Write a literal value.

### KR8602

**Static page reads revalidating data**

The page reads data with cache tags, a revalidate window or `cache: "no-store"`, but it is rendered statically (it is pinned with `dynamic = "force-static"`, `revalidate = false`, or the project uses `output: "static"`). Static HTML is baked once at build time: revalidateTag, revalidatePath and revalidate windows cannot refresh it. Remove the static pin or export `revalidate = N` so the page is served with ISR.

### KR8603

**revalidatePath targets a static page**

A revalidatePath() call names a route that is rendered statically (SSG). The page's HTML is a file baked at build time, so revalidation cannot rebuild it: at runtime the call reports the route under `skipped` and logs a warning. Make the target page ISR (`export const revalidate = N`) or SSR (`export const dynamic = "force-dynamic"`), or rebuild the site to refresh it.

## See also

- [React Compatibility](/docs/guides/react-compatibility/)
- [Compatibility matrix](/docs/guides/compatibility/)
