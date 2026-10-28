package diag

// Async server components and RSC boundaries (KR71xx, part of the KR7xxx
// server range).
const (
	// CodeAsyncLayout: a layout is an async function.
	CodeAsyncLayout Code = "KR7101"
	// CodeBuildTimeAwait: `await` reached the build-time evaluator.
	CodeBuildTimeAwait Code = "KR7102"
	// CodeServerComponentStaticOutput: server components with output "static".
	CodeServerComponentStaticOutput Code = "KR7103"
	// CodeServerComponentStreaming: a static page with server components streams.
	CodeServerComponentStreaming Code = "KR7104"
	// CodeServerComponentProps: a server component region needs build-time props.
	CodeServerComponentProps Code = "KR7105"
	// CodeClientAPIInServer: hooks, signals or event handlers in a server tree.
	CodeClientAPIInServer Code = "KR7110"
	// CodeClientPropNotSerializable: a function prop crosses to a client component.
	CodeClientPropNotSerializable Code = "KR7111"
	// CodeServerOnlyInClient: a client boundary imports a server-only module.
	CodeServerOnlyInClient Code = "KR7112"
	// CodeAsyncClientComponent: an async component in a "use client" module.
	CodeAsyncClientComponent Code = "KR7113"
	// CodeClientBoundaryExports: a "use client" module's exports cannot be listed.
	CodeClientBoundaryExports Code = "KR7114"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeAsyncLayout,
			Title: "Async layouts are not supported",
			Explanation: "Layouts are baked into the static page shell at build time, so they cannot await " +
				"data. Keep the layout synchronous and move the data fetching into an async child " +
				"component (a server component), which renders per request.",
		},
		CodeInfo{
			Code:  CodeBuildTimeAwait,
			Title: "`await` cannot run at build time",
			Explanation: "Only async components (server components) can await: they render per request in the " +
				"SSR sidecar. Code that Krate evaluates at build time, such as module-level constants a " +
				"static component renders or a non-async component body, cannot await a Promise. Mark the " +
				"component `async` or compute the value without awaiting.",
		},
		CodeInfo{
			Code:  CodeServerComponentStaticOutput,
			Title: "Server components need request-time rendering",
			Explanation: "An async component renders per request, but `output: \"static\"` disables the SSR " +
				"sidecar, so the page cannot render it. Remove `output: \"static\"` or make the component " +
				"synchronous.",
		},
		CodeInfo{
			Code:  CodeServerComponentStreaming,
			Title: "Page with server components is rendered per request",
			Explanation: "A page that contains an async component cannot be fully baked at build time. Krate " +
				"bakes the rest of the page as a static shell and renders each server component per request " +
				"(streaming mode). This is informational.",
		},
		CodeInfo{
			Code:  CodeServerComponentProps,
			Title: "Server component props must be known at build time in a static page",
			Explanation: "A server component used from a synchronous page renders as its own request-time " +
				"region, so its props are fixed when the page shell is built: they must be literals, " +
				"module constants or route params, and it cannot receive children. Make the page itself " +
				"`async` (the whole page then renders per request) or pass constant props.",
		},
		CodeInfo{
			Code:  CodeClientAPIInServer,
			Title: "Client-only API used in a server component",
			Explanation: "Components rendered by a server component (an async component) render once per request " +
				"on the server and ship no JavaScript, so hooks such as useState/useEffect, Krate signals and " +
				"event handlers cannot work there. Move the interactive part into a module that starts with " +
				"`\"use client\"`; it is server-rendered and then hydrated in the browser.",
		},
		CodeInfo{
			Code:  CodeClientPropNotSerializable,
			Title: "Function prop passed from a server component to a client component",
			Explanation: "Props crossing from a server component to a `\"use client\"` component are serialized " +
				"to JSON. Functions (other than server actions), class instances and symbols cannot be sent. " +
				"Define the handler inside the client component, or pass a server action. The same check runs " +
				"at request time for values computed while rendering.",
		},
		CodeInfo{
			Code:  CodeServerOnlyInClient,
			Title: "Client component imports a server-only module",
			Explanation: "A module that imports `server-only` must never reach the browser, but a " +
				"`\"use client\"` module (directly or through its imports) imports it, so it would be bundled " +
				"into the client chunk. Import it from a server component instead and pass the data as props.",
		},
		CodeInfo{
			Code:  CodeAsyncClientComponent,
			Title: "Async component in a client module",
			Explanation: "Components in a `\"use client\"` module run in the browser and cannot be async. " +
				"Move the async component to a module without the directive (it becomes a server component).",
		},
		CodeInfo{
			Code:  CodeClientBoundaryExports,
			Title: "Client module exports cannot be listed",
			Explanation: "Krate wraps each export of a `\"use client\"` module in a client boundary, so it must " +
				"know the export names. `export * from` cannot be enumerated: list the re-exported names " +
				"explicitly (`export { A, B } from \"./x\"`).",
		},
	)
}
