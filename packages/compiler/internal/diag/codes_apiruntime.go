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
			"modules each route or middleware bundles and finds globals that runtime lacks, such as " +
			"`Buffer`, `__dirname`, `process.cwd`, `TransformStream` or `WebSocket`. Only global " +
			"references count: an import or local variable of the same name, a `typeof X` check and code " +
			"guarded by one are not reported. When `ssr.apiRuntime` is unset, an API route whose bundled " +
			"modules use such a global is served by the Node sidecar instead, and the build prints one line " +
			"naming the first use. The diagnostic is therefore a warning for a use that the build cannot " +
			"move (a route whose scan is incomplete, middleware) and an error when the embedded runtime " +
			"was requested explicitly (`ssr.apiRuntime: \"quickjs\"`, or `export const runtime = \"edge\"` " +
			"in the route), because the route would fail at run time with a `NotSupportedError` carrying " +
			"the same code. Middleware has no automatic move: select `ssr.middlewareRuntime: \"node\"`. " +
			"In a dependency of an explicitly embedded route the report stays a warning, once per package " +
			"and API, since libraries often detect features before using them. A route that imports a " +
			"`node:` builtin, or exports `runtime = \"nodejs\"`, is served by Node and is not scanned. The " +
			"full list is in the [API runtime compatibility](/docs/reference/api-runtime-compat/) reference.",
	})
}
