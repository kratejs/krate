package diag

// Native (desktop) target: host runtimes other than the embedded QuickJS one,
// islands and scriptc (KR7230 to KR7239).
const (
	// CodeHostNodeBuiltin: host code on QuickJS imports a Node built-in module.
	CodeHostNodeBuiltin Code = "KR7230"
	// CodeHostIslandConflict: one host export reaches islands of different runtimes.
	CodeHostIslandConflict Code = "KR7231"
	// CodeHostIslandUnused: an islands entry matches no host code.
	CodeHostIslandUnused Code = "KR7232"
	// CodeRuntimeUnavailable: the runtime a host child needs is missing or the wrong version.
	CodeRuntimeUnavailable Code = "KR7233"
	// CodeRuntimeRelease: compiling a host child into an executable failed.
	CodeRuntimeRelease Code = "KR7234"
	// CodeScriptcUnsupported: scriptc rejects host code.
	CodeScriptcUnsupported Code = "KR7235"
	// CodeScriptcToolchain: scriptc or its linker toolchain is missing.
	CodeScriptcToolchain Code = "KR7236"
	// CodeScriptcRuntime: scriptc rejects the Krate host child runtime itself.
	CodeScriptcRuntime Code = "KR7237"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeHostNodeBuiltin,
			Title: "Host code imports a Node built-in on the QuickJS runtime",
			Explanation: "Host code runs on an embedded QuickJS runtime by default. It has the ECMAScript built-ins " +
				"and a small set of web APIs, but no Node built-in modules, so an import of `node:fs`, `node:path` " +
				"or any other built-in cannot work and the build refuses it. Run the host code on a full runtime " +
				"with `native.host.runtime` set to `\"node\"`, `\"bun\"` or `\"deno\"`, or run only the module (or " +
				"package) that needs the built-in there with `native.host.islands`, for example " +
				"`{ \"sharp\": \"node\" }`. Go host functions are the third option.",
		},
		CodeInfo{
			Code:  CodeHostIslandConflict,
			Title: "Host export reaches islands of different runtimes",
			Explanation: "`native.host.islands` assigns a package or path to a runtime, and a registered host export " +
				"runs wherever the modules it imports run. When one export imports modules assigned to two " +
				"different runtimes there is no process that can run it. Split the export so that each part " +
				"imports one island, or move both modules to the same runtime.",
		},
		CodeInfo{
			Code:  CodeHostIslandUnused,
			Title: "An islands entry matches no host code",
			Explanation: "An entry of `native.host.islands` names a package or path that no registered host export " +
				"imports, so it changes nothing. Package names must match the import specifier's package " +
				"(`sharp`, `@scope/name`); paths are relative to the project root and may use `*` and `**`.",
		},
		CodeInfo{
			Code:  CodeRuntimeUnavailable,
			Title: "Host runtime not found or wrong version",
			Explanation: "A host child needs its runtime (Node, Bun or Deno): on PATH for development, or to build the " +
				"executable that ships with the app. The runtime was not found, or its version does not match " +
				"`native.host.version`. Install it, point `KRATE_NODE`, `KRATE_BUN` or `KRATE_DENO` at a binary, " +
				"or change the pinned version. Release builds require `native.host.bundle` to be true.",
		},
		CodeInfo{
			Code:  CodeRuntimeRelease,
			Title: "Compiling the host child failed",
			Explanation: "A release build turns each host child into a self-contained executable: a Node single " +
				"executable application, `bun build --compile`, or `deno compile`. The tool failed; its output is " +
				"in the message. Cross-compiling needs the target's runtime, which the tool downloads or you " +
				"provide with `KRATE_NODE_<OS>_<ARCH>`.",
		},
		CodeInfo{
			Code:  CodeScriptcUnsupported,
			Title: "scriptc cannot compile this host code",
			Explanation: "With `native.host.runtime` set to `\"scriptc\"` the host code is compiled to a native " +
				"executable that has no JavaScript engine, so code scriptc rejects fails the build; there is no " +
				"fallback to an interpreter. The message carries scriptc's own diagnostic code (SC####) and hint. " +
				"Rewrite the code as the hint suggests, move it to a Go host function, or run it on QuickJS or " +
				"Node. `krate native check --scriptc` reports these without building.",
		},
		CodeInfo{
			Code:  CodeScriptcToolchain,
			Title: "scriptc or its toolchain is missing",
			Explanation: "scriptc is not on PATH, or the linker it needs to build an executable is missing: clang on " +
				"macOS and glibc Linux, zig on Windows and musl Linux and for cross-compiling. Install scriptc " +
				"(`npm install -g scriptc`, Node 24 or later) and the linker, or use another host runtime.",
		},
		CodeInfo{
			Code:  CodeScriptcRuntime,
			Title: "scriptc rejects the Krate host child runtime",
			Explanation: "The host code compiled, but scriptc rejected a line of the Krate host child runtime " +
				"(packages/native/hostchild) that serves the call protocol. This is a limitation of the installed " +
				"scriptc version, not of your code. Update scriptc, or use another host runtime.",
		},
	)
}
