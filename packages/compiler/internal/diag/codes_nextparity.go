package diag

// Next.js-parity features: access-denied pages, metadata and shared caches.
const (
	// CodeStaticAccessDenied: forbidden() or unauthorized() in a statically
	// built page or in a layout, where the call can never run per request.
	CodeStaticAccessDenied Code = "KR8604"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeStaticAccessDenied,
			Title: "forbidden() or unauthorized() never runs per request",
			Explanation: "forbidden() and unauthorized() answer one request with a 403 or 401 page, so they only work in code " +
				"that runs on every request: a page rendered with ssr, isr or streaming, an async server component, a " +
				"server action or an API route. This call sits in a statically built page, or in a layout (layouts are " +
				"baked into the shell at build time), so it never runs and the page is served to everyone. Make the page " +
				"request-time (`export const dynamic = \"force-dynamic\"`), or call the function from an async server " +
				"component that the page renders.",
		},
	)
}
