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

### KR2007

**Effect cleanup reads state**

In React a cleanup closes over the values of the render its effect ran for, so it sees the previous state. Krate state reads are live getters, so a cleanup sees the current (new) value. Copy the value into a local in the effect body and read that in the cleanup: const id = roomId; return () => leave(id).

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

### KR5008

**ref.current read during render**

React shows the ref's value as of the latest render, so a later state update reveals a mutated ref.current. Krate components render once and bind only state, so a ref read in JSX is shown once and never refreshed. Keep values the UI shows in state, or read the ref in an effect or event handler.

## KR6xxx: lists and keys

No codes are registered in this range yet.

## KR7xxx: React 19 actions and server actions

No codes are registered in this range yet.

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
