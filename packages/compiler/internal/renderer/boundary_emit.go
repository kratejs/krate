package renderer

import (
	"errors"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Build-time error boundaries.
//
// A throw statement that runs while a component is rendered at build time
// produces a BuildThrow diagnostic (KR3010). When the component sits inside a
// lowered error boundary, the boundary's emit claims it: the error is removed
// from the build errors and recorded on the output, and the build re-renders
// the page with the boundary in its fallback state (irtree BoundaryThrows).

// BuildThrow is the diagnostic for a value thrown during build-time rendering.
type BuildThrow struct {
	diag.Diagnostic
	Name    string
	Message string
}

// Unwrap exposes the diagnostic to diag.FromError.
func (t BuildThrow) Unwrap() error { return t.Diagnostic }

func newBuildThrow(t *jsvalue.ThrownError) BuildThrow {
	msg := "a component threw while the page was built"
	if t.Message != "" {
		msg += ": " + t.Message
	}
	d := diag.Diagnostic{Line: t.Line, Col: t.Col, Message: msg,
		Hint: "fix the error, guard the throw with a condition, or wrap the component in an error boundary (a class with static getDerivedStateFromError) to render its fallback instead"}
	return BuildThrow{Diagnostic: d.WithCode(diag.CodeBuildTimeThrow), Name: t.Name, Message: t.Message}
}

// claimBoundaryThrows removes the build throws raised since index from
// e.errs and records the first for the boundary node.
func (e *Emitter) claimBoundaryThrows(node *irtree.ComponentNode, from int, out *SlotOutput) {
	if from > len(e.errs) {
		return
	}
	kept := e.errs[:from:from]
	var first *BuildThrow
	for _, err := range e.errs[from:] {
		var bt BuildThrow
		if errors.As(err, &bt) {
			if first == nil {
				first = &bt
			}
			continue
		}
		kept = append(kept, err)
	}
	if first == nil {
		return
	}
	e.errs = kept
	out.BoundaryThrows = append(out.BoundaryThrows, irtree.BoundaryThrow{
		BoundaryID: node.ID,
		Boundary:   node.Name,
		Name:       first.Name,
		Message:    first.Message,
		Line:       first.Line,
		Col:        first.Col,
	})
}
