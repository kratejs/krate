package irtree

import "fmt"

// GoRenderMarkerPrefix and GoRenderMarkerSuffix delimit the splice marker a
// build-time Go-component render leaves in the emitted HTML. The build replaces
// each marker with the host's rendered HTML (see gohost). Defining it here keeps
// the emitter and the build in sync (they both import irtree).
const (
	GoRenderMarkerPrefix = "<!--krate-gocomp:"
	GoRenderMarkerSuffix = "-->"
)

// GoRenderMarker returns the splice marker for a Go render request id.
func GoRenderMarker(id string) string {
	return GoRenderMarkerPrefix + id + GoRenderMarkerSuffix
}

// GoCallID is the stable, source-derived id for one Go-component call site. It
// keys the build-time baked HTML map that a page's SSR server bundle consults
// (see serverbundle.go): the same id is injected into the SSR page source as the
// `__krateGoId` prop, so a server-tier component renders its build-time HTML
// instead of re-running the Go host at request time. line/col are the JSX tag
// name token position; name disambiguates same-position calls in different files.
func GoCallID(line, col int, name string) string {
	return fmt.Sprintf("gc%d_%d_%s", line, col, name)
}

// GoRenderRequest is one build-time Go component render. Shared by the emitter
// (which collects them) and the Go host (which renders them).
type GoRenderRequest struct {
	ID     string // marker id (matches <!--krate-gocomp:ID-->)
	Name   string // Go component function name
	Props  string // JSON object string (includes children)
	CallID string // source-derived call-site id (see GoCallID), for SSR baking
}
