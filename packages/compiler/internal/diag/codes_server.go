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
	)
}
