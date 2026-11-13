// Package gobind generates the plumbing between UI code and Go host code in a
// Krate Native app. UI files import exported Go functions and signals from a
// .go file and call them like async functions:
//
//	import { List, Add, Watch, Count } from "../lib/tasks.go"
//
// The compiler sees both sides, so Generate emits everything: a Go glue
// package per user package, a JavaScript stub per imported file and a
// TypeScript declaration file per imported file. Nothing on the call path
// uses reflection or encoding/json.
//
// # Entry point
//
// Generate(root, imports, outDir) takes the project root (the directory with
// go.mod), one Import per .go file or package directory the UI imports with the
// names it imports, and the output directory (normally <root>/.krate/native).
// It returns a Result, diagnostics (codes KR7180 to KR7185), and an error only
// for I/O failures. With diagnostics, nothing is written and Result is nil.
//
// # What is generated
//
//	<outDir>/glue/<package dir>/glue.go   package glue, func Register(*scheme.Registry)
//	<outDir>/gostubs/<module id>.go.js    the module the UI bundle imports
//	<outDir>/types/<module id>.go.d.ts    TypeScript declarations
//
// The glue lives inside the user's Go module (go ignores directories that
// start with a dot for ./... patterns, but an explicit import path such as
// example.com/app/.krate/native/glue/src/lib works), so it can import the user
// package, including its internal packages, and the host module
// github.com/kratejs/krate/packages/native/{krate,wire,scheme}. The user's
// go.mod must require that module. Generated files are gofmt-formatted and
// deterministic. Stale files from earlier runs are not removed.
//
// # Module ids
//
// A module id is the path of the .go file relative to the project root,
// slash-separated, without the extension: src/lib/tasks.go has the id
// src/lib/tasks, and its function List is called as src/lib/tasks.List. A
// package directory import uses the directory path (src/lib) as the id. Two
// imports must not produce the same id. A file import may only name things
// declared in that file; a directory import may name anything in the package.
//
// # Host functions versus components
//
// A .go import can name a Go component (see internal/gotsgen: any exported
// function in a *.server.go or *.runtime.go file, in a file marked
// //krate:component, or a function marked with that directive). Those keep
// today's behavior; Generate only lists them in Module.Components. Every other
// imported name must be host code: an exported function with a supported
// signature or an exported package-level *krate.Signal[T] variable. Types,
// constants, other variables and missing or unexported names are KR7183.
//
// # Signatures
//
// A function takes an optional first parameter krate.Ctx, then arguments, and
// returns nothing, error, T, (T, error), <-chan T or (<-chan T, error). A
// channel result makes it a stream (AsyncIterable<T>). Variadic and generic
// functions are rejected (KR7180). The TypeScript form drops ctx and adds a
// trailing opts?: CallOptions parameter. Calls are not deduplicated unless the
// caller passes dedupe: true.
//
// # Type mapping
//
//	bool, string                      boolean, string
//	int*, uint*, float*               number (int64 and uint64 above 2^53 lose
//	                                  precision in JavaScript; they are allowed)
//	named types, aliases              resolved to the underlying type;
//	                                  time.Duration is a number of nanoseconds
//	time.Time                         Date (wire Time)
//	[]byte                            Uint8Array (wire Bytes, no base64)
//	struct                            object; json tags (name, omitempty, "-"),
//	                                  embedded struct flattening and name
//	                                  conflict rules follow encoding/json;
//	                                  unexported fields are skipped; omitzero
//	                                  and ",string" are ignored
//	slice, array                      array (a nil slice is sent as [])
//	map[string]T (string-kinded keys) Record<string, T> (keys are sorted)
//	*T                                T | null
//
// Rejected with KR7181: any and other interfaces, channels and funcs inside
// values, complex numbers, uintptr, unsafe pointers, maps with other key
// types, embedded pointers and unexported named types. Types with
// MarshalJSON, UnmarshalJSON, MarshalText or UnmarshalText methods are KR7184.
// Recursive types work: each named type gets one codec function.
//
// Decoding skips unknown object fields, leaves missing fields at their zero
// value, matches field names exactly (not case-insensitively), and rejects a
// wrong argument count with a krate.Error coded bad_args.
package gobind
