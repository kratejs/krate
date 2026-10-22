package build

import (
	"os"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// maxBoundaryPasses bounds re-renders when a fallback itself throws and the
// error moves to an outer boundary.
const maxBoundaryPasses = 4

// applyBoundaryFallbacks re-renders the page while error boundaries report
// that their subtree threw during the build (renderer.BuildThrow): each pass
// adds the reporting boundaries to opts.BoundaryThrows so they render their
// getDerivedStateFromError fallback, and prints a KR3009 warning per caught
// error instead of failing the build.
func applyBoundaryFallbacks(page string, tree *irtree.ComponentTree, res *renderer.EmitResult, opts irtree.BuildOptions,
	rebuild func(irtree.BuildOptions) (*irtree.ComponentTree, *renderer.EmitResult)) (*irtree.ComponentTree, *renderer.EmitResult) {
	if len(res.BoundaryThrows) == 0 {
		return tree, res
	}
	caught := map[irtree.SlotID]irtree.BoundaryThrow{}
	for k, v := range opts.BoundaryThrows {
		caught[k] = v
	}
	var warnings []diag.Diagnostic
	for pass := 0; pass < maxBoundaryPasses && len(res.BoundaryThrows) > 0; pass++ {
		progress := false
		for _, t := range res.BoundaryThrows {
			if _, seen := caught[t.BoundaryID]; seen {
				continue
			}
			caught[t.BoundaryID] = t
			progress = true
			warnings = append(warnings, boundaryWarning(page, t))
		}
		if !progress {
			break
		}
		opts.BoundaryThrows = caught
		tree, res = rebuild(opts)
	}
	printTreeWarnings(os.Stderr, warnings)
	return tree, res
}

func boundaryWarning(page string, t irtree.BoundaryThrow) diag.Diagnostic {
	msg := "a component inside <" + t.Boundary + "> threw while the page was built"
	if t.Message != "" {
		msg += ": " + t.Message
	}
	msg += "; <" + t.Boundary + "> renders its error fallback"
	d := diag.New(page, t.Line, t.Col, msg,
		"fix the error, or keep the fallback if it is expected; componentDidCatch receives the error when the page hydrates", "").
		WithCode(diag.CodeBoundaryBuildFallback)
	d.Severity = diag.SeverityWarning
	return d
}
