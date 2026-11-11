package diag

// Embedded API runtime (KR716x, part of the KR7xxx server range).
const (
	// CodeAPIRuntimeUnsupported: an API route or middleware references a Web
	// API or Node global the embedded QuickJS runtime does not provide.
	CodeAPIRuntimeUnsupported Code = "KR7160"
)

func init() {
	Register(CodeInfo{
		Code:  CodeAPIRuntimeUnsupported,
		Title: "API not supported by the embedded API runtime",
		Explanation: "API routes and middleware run in Krate's embedded QuickJS runtime unless `ssr.apiRuntime` " +
			"(or `ssr.middlewareRuntime`) selects `\"node\"`, `\"bun\"` or `\"deno\"`. The build scans the " +
			"modules each route or middleware bundles and reports globals that runtime lacks, such as " +
			"`TransformStream`, `WebSocket`, `Buffer` or `crypto.subtle.encrypt`, with the location of the " +
			"first use in each file. Calling one throws a `NotSupportedError` carrying the same code at run " +
			"time. Only global references count: an import or local variable of the same name, a " +
			"`typeof X` check and code guarded by one are not reported. In a dependency the warning is " +
			"reported once per package and API, since libraries often detect features before using them. " +
			"Follow the hint (most APIs have a portable alternative), or select the Node runtime. A route " +
			"that imports a `node:` builtin is served by Node automatically and is not scanned. The full " +
			"list is in the [API runtime compatibility](/docs/reference/api-runtime-compat/) reference. " +
			"This is a warning.",
	})
}
