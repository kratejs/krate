package diag

// KR9xxx: routing, segment pages and rendering errors.
const (
	CodeSegmentConflict    Code = "KR9001"
	CodeSegmentMisplaced   Code = "KR9002"
	CodeSegmentRequestMode Code = "KR9003"
)

func init() {
	Register(
		CodeInfo{CodeSegmentConflict, "Conflicting segment pages",
			"Two files define the same role for one URL directory: 404.tsx and not-found.tsx, error.tsx and 500.tsx, or the same file name in two route groups that resolve to one directory. Krate cannot tell which one to use. Keep one file per role in a directory."},
		CodeInfo{CodeSegmentMisplaced, "Segment page in an unsupported directory",
			"404.tsx, not-found.tsx, error.tsx and 500.tsx are per-directory segment pages. They cannot live inside a parallel-route @slot or an intercepting (.)route directory, because those render inside their parent's layout. Move the file to the parent route directory."},
		CodeInfo{CodeSegmentRequestMode, "Segment page ignores request-time rendering",
			"Segment pages (404.tsx, not-found.tsx, error.tsx, 500.tsx) are prerendered once and written to the output; the server picks the right file per request. A request-time mode (ssr, isr, streaming, runtime components, async server components) on such a page is ignored. Read request data in the page's client code, or link to a normal route."},
	)
}
