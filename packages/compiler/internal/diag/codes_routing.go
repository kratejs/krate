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
		CodeInfo{CodeSegmentRequestMode, "Error page ignores request-time rendering",
			"An error page (error.tsx, 500.tsx) answers while the app is failing, so it is prerendered once and never depends on the renderer. A request-time mode (ssr, isr, streaming, runtime components, async server components) on it is ignored. Read request data in the page's client code. A not-found, forbidden or unauthorized page (404.tsx, 403.tsx, 401.tsx) can render per request instead."},
	)
}
