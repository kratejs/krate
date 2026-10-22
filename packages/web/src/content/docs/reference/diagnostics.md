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

No codes are registered in this range yet.

## KR3xxx: class components and error boundaries

No codes are registered in this range yet.

## KR4xxx: Suspense, lazy, use() and transitions

No codes are registered in this range yet.

## KR5xxx: events, forms and refs

No codes are registered in this range yet.

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
