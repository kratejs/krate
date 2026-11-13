package diag

// Go host bindings: UI imports of exported Go functions and signals
// (KR718x, part of the KR71xx server range).
const (
	// CodeGoBindSignature: an imported Go function has a signature the host bridge cannot call.
	CodeGoBindSignature Code = "KR7180"
	// CodeGoBindType: a parameter, result or field type has no wire mapping.
	CodeGoBindType Code = "KR7181"
	// CodeGoBindLoad: the Go package could not be loaded or type-checked.
	CodeGoBindLoad Code = "KR7182"
	// CodeGoBindImport: a UI import of a .go file names something that is not callable host code.
	CodeGoBindImport Code = "KR7183"
	// CodeGoBindMarshaler: a type has a custom JSON or text marshaler.
	CodeGoBindMarshaler Code = "KR7184"
	// CodeGoBindModule: a .go import cannot be given a module id or output location.
	CodeGoBindModule Code = "KR7185"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeGoBindSignature,
			Title: "Go function signature cannot be a host call",
			Explanation: "A UI file imports an exported Go function and calls it as an async function. The bridge " +
				"supports an optional first parameter `ctx krate.Ctx`, then the arguments, and one of these " +
				"result shapes: nothing, `error`, `T`, `(T, error)`, `<-chan T` or `(<-chan T, error)`. A channel " +
				"result makes the call a stream. Variadic functions, generic functions, a first parameter of " +
				"`context.Context` (use `krate.Ctx`), `krate.Ctx` anywhere but first, send-only channels, " +
				"and more than one non-error result are rejected. Change the signature, or wrap the function in " +
				"one that has a supported shape.",
		},
		CodeInfo{
			Code:  CodeGoBindType,
			Title: "Go type has no wire mapping",
			Explanation: "Values cross the host boundary through generated encoders without reflection, so every " +
				"parameter, result and field type must map to a wire type. Supported: bool, string, all int, uint " +
				"and float kinds, `time.Time` (a `Date`), `[]byte` (a `Uint8Array`), structs, slices and arrays, " +
				"`map[string]T` (also string-kinded named keys), pointers (`T | null`) and named types of these. " +
				"Rejected: `any` and other interfaces, channels and funcs inside values, complex numbers, " +
				"`uintptr`, unsafe pointers, maps with non-string keys, embedded pointers, and unexported named " +
				"types (the generated glue lives in another package and cannot name them). The message gives the " +
				"path to the offending parameter or field. Replace the type with a concrete one.",
		},
		CodeInfo{
			Code:  CodeGoBindLoad,
			Title: "Go package could not be loaded",
			Explanation: "The compiler type-checks the Go package behind a `.go` import with the Go toolchain " +
				"(`go list -export`) and the standard `go/types` package. It failed: the `go` command is missing, " +
				"the project has no `go.mod`, the package has compile errors, or it is `package main`. Fix the " +
				"error shown, and check that `go build ./...` works in the project.",
		},
		CodeInfo{
			Code:  CodeGoBindImport,
			Title: "Import from a .go file is not host code",
			Explanation: "In a native build a UI import from a `.go` file must name either a Go component (a " +
				"function in a `*.server.go` / `*.runtime.go` file or marked `//krate:component`) or exported host " +
				"code: a function with a supported signature, or a package-level variable created with " +
				"`krate.NewSignal`. Types, constants, other variables, methods and unexported or missing names " +
				"cannot be imported. Export a function that returns the value instead.",
		},
		CodeInfo{
			Code:  CodeGoBindMarshaler,
			Title: "Go type has a custom marshaler",
			Explanation: "A type with `MarshalJSON`, `UnmarshalJSON`, `MarshalText` or `UnmarshalText` encodes itself " +
				"in a way the generated encoders cannot know, and they do not call encoding/json. Types such as " +
				"`big.Int`, `netip.Addr` or `uuid.UUID` fall in this group. Convert the value to a plain type " +
				"(a string, a number or a struct) in the exported function.",
		},
		CodeInfo{
			Code:  CodeGoBindModule,
			Title: "Go import has no valid module id or output location",
			Explanation: "Each `.go` file imported by the UI gets a module id: its path relative to the project root " +
				"without the extension (`src/lib/tasks.go` becomes `src/lib/tasks`), and host calls are named " +
				"`<module id>.<Name>`. The file must exist inside the project, be a non-test `.go` file, and " +
				"belong to the project's Go module, and the generated output directory must be inside that " +
				"module so the glue can import the package.",
		},
	)
}
