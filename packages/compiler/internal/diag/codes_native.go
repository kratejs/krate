package diag

// Native (desktop) target: host code placement and target restrictions
// (KR717x, part of the KR71xx server range).
const (
	// CodeHostConflict: two host-marking sources disagree about a module or function.
	CodeHostConflict Code = "KR7170"
	// CodeHostComponent: `@host` on a component.
	CodeHostComponent Code = "KR7171"
	// CodeHostCapture: an inline host function captures component state.
	CodeHostCapture Code = "KR7172"
	// CodeHostInWebBuild: a host marker in a web build.
	CodeHostInWebBuild Code = "KR7173"
	// CodeNativeServerRendering: SSR, ISR or streaming in a native build.
	CodeNativeServerRendering Code = "KR7174"
	// CodeNativeAPIRoute: a src/api route in a native build.
	CodeNativeAPIRoute Code = "KR7175"
	// CodeNativeMiddleware: middleware in a native build.
	CodeNativeMiddleware Code = "KR7176"
	// CodeNativeRuntimeComponent: a runtime component in a native build.
	CodeNativeRuntimeComponent Code = "KR7177"
	// CodeNativeServerAction: a server action in a native build.
	CodeNativeServerAction Code = "KR7178"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeHostConflict,
			Title: "Host code markers disagree",
			Explanation: "A native app decides what is host code (it runs in the native process, not the webview) " +
				"from several sources: a `@host` decorator or a `\"@host\"` string at the top of a function body, a " +
				"`// @host` comment on the first line of a file, a `.host.ts` / `.host.tsx` file name, the " +
				"`hostFunctions` and `hostDirs` config entries, and `.go` files, which are always host code. " +
				"Sources that name host code and something else for the same module conflict: a `.go` file cannot " +
				"also carry a tier, and a host module cannot carry `// @server`, `// @runtime`, `// @static` or " +
				"`// @client`, a tier file name, or a `serverComponents`, `runtimeComponents`, `serverDirs` or " +
				"`runtimeDirs` entry. A function-level marker overrides the file for that function only, so " +
				"`@client` on one function in a host file is allowed, but a decorator and a body string on one " +
				"function must name the same thing. Remove one of the conflicting sources.",
		},
		CodeInfo{
			Code:  CodeHostComponent,
			Title: "`@host` on a component",
			Explanation: "Host code runs in the native process and is called from the UI as an async function. A " +
				"component renders UI, so it cannot be host code. Keep the component in the webview and move the " +
				"native work into a plain function marked `@host` that the component calls. For work that only " +
				"needs to happen at build time, use `@server`.",
		},
		CodeInfo{
			Code:  CodeHostCapture,
			Title: "Inline host function captures component state",
			Explanation: "An `@host` function declared inside a component module is moved to the host process, so it " +
				"cannot see the component's local variables, signals or props. It may use its own parameters, " +
				"module-level imports, module-level host declarations and globals. Pass the value as an argument " +
				"instead. The check covers identifiers declared inside an enclosing function; module-level " +
				"non-host declarations are not checked.",
		},
		CodeInfo{
			Code:  CodeHostInWebBuild,
			Title: "Host code in a web build",
			Explanation: "`@host` markers, `.host.ts` files, `hostFunctions` and `hostDirs` only mean something in a " +
				"native build, where host code runs in the native process. A web build has no such process, so the " +
				"marker would be silently ignored and the code would run in the browser or not at all. Remove the " +
				"marker, or build with `krate native`.",
		},
		CodeInfo{
			Code:  CodeNativeServerRendering,
			Title: "Request-time rendering in a native build",
			Explanation: "A native build has no server: every route is prerendered. `ssr.streaming`, `ppr`, and page " +
				"exports such as `dynamic = \"force-dynamic\"`, `revalidate = N` or `config = { ssr / isr / streaming }` " +
				"ask for rendering per request. Remove them, and load per-request data from host code " +
				"(`@host` functions) at runtime.",
		},
		CodeInfo{
			Code:  CodeNativeAPIRoute,
			Title: "API route in a native build",
			Explanation: "A native app has no HTTP server, so routes under `src/api` have nothing to serve them. " +
				"Move the logic into host code (a `.go` file or an `@host` function) and call it from the UI.",
		},
		CodeInfo{
			Code:  CodeNativeMiddleware,
			Title: "Middleware in a native build",
			Explanation: "`middleware.ts` runs in front of a server's requests. A native app has no server, so it " +
				"would never run. Remove it, and do the check in host code or in the UI.",
		},
		CodeInfo{
			Code:  CodeNativeRuntimeComponent,
			Title: "Runtime component in a native build",
			Explanation: "`@runtime` components render per request in a server sidecar. A native build renders every " +
				"route once, ahead of time. Use `@server` for build-time rendering, or a client component that loads " +
				"its data from host code. This covers `@runtime`, `// @runtime`, `.runtime` files, and the " +
				"`runtimeComponents` and `runtimeDirs` config entries.",
		},
		CodeInfo{
			Code:  CodeNativeServerAction,
			Title: "Server action in a native build",
			Explanation: "`\"use server\"` actions are POSTed to a server. A native app has no server; call an " +
				"`@host` function (or a Go function) from the UI instead.",
		},
	)
}
