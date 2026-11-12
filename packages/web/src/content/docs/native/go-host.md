---
title: Go Host Code
order: 4
description: Call exported Go functions, streams and signals from the UI of a Krate Native app - signatures, type mapping, generated files and the Go host binary.
---

# Go Host Code

> **Preview.** Part of [Krate Native](/docs/native/), which is under construction.

A `.go` file is always [host code](/docs/native/host-code/). The UI imports its
exported functions and signals by name and calls them like async functions:

```tsx
import { List, Add, Watch, Count } from "../lib/tasks.go";

const tasks = await List();
const t = await Add("write docs");
for await (const n of Watch()) { /* one value per tick */ }
<p>Open: {Count()}</p>
```

```go
package lib

import "github.com/kratejs/krate/packages/native/krate"

var Count = krate.NewSignal(0)

func List() []Task                       { /* ... */ }
func Add(title string) (Task, error)     { /* ... */ }
func Watch(ctx krate.Ctx) <-chan int     { /* ... */ }
```

Name the imports: `import * as t from "./tasks.go"` and `export *` hide the names
the compiler has to bind (`KR7201`).

## Signatures

A function takes an optional first parameter `krate.Ctx`, then its arguments, and
returns nothing, `error`, `T`, `(T, error)`, `<-chan T` or `(<-chan T, error)`.
A channel result is a stream (`AsyncIterable<T>`). An exported package-level
`*krate.Signal[T]` variable is a host signal: in the UI it is a function that
returns the current value and is tracked like a runtime signal. Variadic and
generic functions are rejected. Values map to JavaScript as: `bool`, `string`;
numbers; structs as objects (`json` tags are honored); slices as arrays;
`map[string]T` as records; `[]byte` as `Uint8Array`; `time.Time` as `Date`;
pointers as `T | null`. A `*krate.Error` keeps its code in the rejected promise.

The compiler type-checks the package and reports problems with the codes
`KR7180` to `KR7185` (see [Diagnostic codes](/docs/reference/diagnostics/)).

## What the build generates

All generated files are under `.krate/native/` (do not commit them):

| Path | Content |
|------|---------|
| `glue/<package dir>/glue.go` | `Register(*scheme.Registry)` for the package: decode, call, encode, without reflection |
| `gostubs/<module id>.go.js` | The module the UI bundle imports in place of the `.go` file |
| `types/<module id>.go.d.ts` | Types for the editor |
| `gohost/main.go` | The program that serves the glue |

The module id is the path of the `.go` file from the project root, without its
extension: `src/lib/tasks.go` has the id `src/lib/tasks`, and `List` is called
as `src/lib/tasks.List`. The glue lives inside your Go module, so it can import
the package and its internal packages. The stubs replace the `.go` file in the
UI bundle only; no Go code or Go type reaches the webview. A Go component
(`*.server.go`, `*.runtime.go`, `//krate:component`) imported from the same file
keeps its web behavior.

## The Go host binary

When the UI imports any host function or signal, `krate native build` generates

```go
package main

import (
	glue0 "<module>/.krate/native/glue/src/lib"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/scheme"
)

func main() {
	gohost.Serve(func(r *scheme.Registry) {
		glue0.Register(r)
	})
}
```

and compiles it in your module with `go build -trimpath` to
`app/gohost/krate-gohost` (`.exe` on Windows). The shell starts it as a child
process and routes calls to it. The project's `go.mod` must require
`github.com/kratejs/krate/packages/native`; without it the build stops with
`KR7202` and tells you to run `go get github.com/kratejs/krate/packages/native`.
A compile error is `KR7203` with the output of the Go command. `--target`
cross-compiles the binary.

## Debugging

`krate native dev --debug-go` builds the Go host without optimization or
`-trimpath` and runs it under Delve, so breakpoints in your `src/lib/*.go` files
hit; see [Debugging](/docs/native/dev/#debugging) for attaching VS Code, GoLand
or `dlv`, and for what happens to the session when you edit Go code.
