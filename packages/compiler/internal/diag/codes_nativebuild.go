package diag

// Native (desktop) target: assembling the app directory (KR7200 to KR7209).
const (
	// CodeNativeAppMissing: the UI uses host code but @krate/app is not installed.
	CodeNativeAppMissing Code = "KR7200"
	// CodeNativeGoNamespace: a namespace import or `export *` of a .go file.
	CodeNativeGoNamespace Code = "KR7201"
	// CodeNativeGoModule: the project's Go module cannot build the Go host.
	CodeNativeGoModule Code = "KR7202"
	// CodeNativeGoBuild: compiling the Go host binary failed.
	CodeNativeGoBuild Code = "KR7203"
	// CodeNativeTarget: an unknown --target platform.
	CodeNativeTarget Code = "KR7204"
	// CodeNativeInit: `krate native init` cannot write its files.
	CodeNativeInit Code = "KR7205"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeNativeAppMissing,
			Title: "@krate/app is not installed",
			Explanation: "A native app's UI calls host code through generated stubs, and the stubs import " +
				"`@krate/app` (the host call client). The package is not resolvable from the project. Install it " +
				"next to `@krate/core`: `npm i @krate/app`.",
		},
		CodeInfo{
			Code:  CodeNativeGoNamespace,
			Title: "Namespace import of a .go file",
			Explanation: "The compiler generates a binding for each name a UI file imports from a `.go` file, so " +
				"it has to see the names. `import * as tasks from \"./tasks.go\"` and `export * from \"./tasks.go\"` " +
				"hide them. Import the functions and signals by name: `import { List, Count } from \"./tasks.go\"`.",
		},
		CodeInfo{
			Code:  CodeNativeGoModule,
			Title: "Go module cannot build the Go host",
			Explanation: "The Go host binary is built inside the project's own Go module, next to the generated " +
				"glue, so the module has to exist (`go.mod` at the project root) and require " +
				"`github.com/kratejs/krate/packages/native`. Run `go get github.com/kratejs/krate/packages/native` " +
				"in the project, or `krate native init --go` in an empty directory to get a working `go.mod`.",
		},
		CodeInfo{
			Code:  CodeNativeGoBuild,
			Title: "Go host failed to compile",
			Explanation: "`go build` failed for the generated Go host program. The output of the Go command is " +
				"shown. The packages the UI imports must compile for the target platform; check build constraints " +
				"and cgo use when you build with `--target` for another operating system.",
		},
		CodeInfo{
			Code:  CodeNativeTarget,
			Title: "Unknown native build target",
			Explanation: "`krate native build --target` takes `<os>/<arch>` where os is windows, linux or darwin " +
				"(macos is accepted) and arch is amd64 or arm64, for example `--target linux/arm64`.",
		},
		CodeInfo{
			Code:  CodeNativeInit,
			Title: "krate native init cannot write its files",
			Explanation: "`krate native init` creates a small app in a directory. It refuses to overwrite an " +
				"existing file, so run it in an empty directory or remove the files it names.",
		},
	)
}
