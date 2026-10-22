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

No codes are registered in this range yet.

## KR4xxx: Suspense, lazy, use() and transitions

No codes are registered in this range yet.

## KR5xxx: events, forms and refs

### KR5001

**Unknown event handler prop**

React ignores on* props on built-in elements that are not React DOM events (and warns). Krate still attaches a native listener for the lowercased event name. Fix the spelling, or listen for a custom DOM event with ref.current.addEventListener in an effect.

### KR5002

**Lowercase event handler prop**

React event props are camelCase (onClick, onChange). A lowercase prop such as onclick is not an event handler in React and the function is never called. Use the camelCase name.

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

## See also

- [React Compatibility](/docs/guides/react-compatibility/)
- [Compatibility matrix](/docs/guides/compatibility/)
