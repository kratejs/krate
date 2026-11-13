package diag

// Native (desktop) target: functions evaluated during the build and baked into
// the prerendered UI (KR7240 to KR7249).
const (
	// CodeBakeArgs: a build function is called with arguments that are not literals.
	CodeBakeArgs Code = "KR7240"
	// CodeBakeRef: a build function is used without being called.
	CodeBakeRef Code = "KR7241"
	// CodeBakeEval: evaluating a build function failed.
	CodeBakeEval Code = "KR7242"
	// CodeBakeValue: the result of a build function cannot be baked.
	CodeBakeValue Code = "KR7243"
	// CodeBakePlacement: a build marker is on something that cannot be a build function.
	CodeBakePlacement Code = "KR7244"
	// CodeBakeGo: the Go evaluator for //krate:build functions could not run.
	CodeBakeGo Code = "KR7245"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeBakeArgs,
			Title: "Build function called with non-literal arguments",
			Explanation: "A function marked `@build` (TypeScript) or `//krate:build` (Go) runs once during the native " +
				"build and its result is baked into the app, so the compiler has to know every argument tuple while " +
				"it builds. Only literal arguments are supported: strings, numbers, booleans, null, and arrays and " +
				"objects of those. Each distinct call with literal arguments gets its own baked value. Pass a " +
				"literal, or drop `@build` and call the function as an ordinary host function at run time.",
		},
		CodeInfo{
			Code:  CodeBakeRef,
			Title: "Build function used without being called",
			Explanation: "A build function is replaced by its baked values, which are looked up by the literal " +
				"arguments of each call. Passing the function around, aliasing it, importing it as a namespace or " +
				"re-exporting it hides the calls from the compiler. Call it directly where its value is needed: " +
				"`const v = appVersion()`.",
		},
		CodeInfo{
			Code:  CodeBakeEval,
			Title: "Build function failed",
			Explanation: "A build function threw, rejected, never settled or returned an error while the native build " +
				"ran it. TypeScript build functions run in a bare JavaScript VM without fetch, timers, the file " +
				"system or locale-dependent methods; imports they use must be plain code. Fix the error shown, or " +
				"remove the build marker if the function needs those capabilities at run time.",
		},
		CodeInfo{
			Code:  CodeBakeValue,
			Title: "Build function result cannot be baked",
			Explanation: "The value a build function returned is embedded in the app, so it has to be plain data: " +
				"strings, finite numbers, booleans, null, arrays and plain objects, at most 8 MiB of JSON, nested at " +
				"most 512 levels, with arrays of at most 1,000,000 elements. NaN, Infinity, cycles, functions, " +
				"symbols, bigint, Date, Map, Set, binary data, class instances and unsettled promises are refused. " +
				"Convert the value to plain data inside the function.",
		},
		CodeInfo{
			Code:  CodeBakePlacement,
			Title: "Build marker on something that cannot be baked",
			Explanation: "`@build` and `//krate:build` apply to ordinary functions that return one value. They " +
				"cannot mark components, generators or streams, signals, or functions that return a channel.",
		},
		CodeInfo{
			Code:  CodeBakeGo,
			Title: "Go build functions could not be evaluated",
			Explanation: "`//krate:build` functions are evaluated by compiling and running a small generated Go program " +
				"in the project's Go module, so the Go toolchain has to be installed and the host packages must " +
				"compile. The output of the Go command is shown.",
		},
	)
}
