package irtree

import "sort"

// BoundaryThrow records that a component inside the error boundary
// BoundaryID threw while the page was built. A rebuild with it in
// BuildOptions.BoundaryThrows renders that boundary in its fallback state.
type BoundaryThrow struct {
	BoundaryID SlotID
	// Boundary is the boundary component's name.
	Boundary string
	// Name and Message describe the thrown error; Line/Col locate the throw.
	Name      string
	Message   string
	Line, Col int
}

// reportClassNotes adds the non-blocking warnings of every lowered class
// component the page rendered (Annotations.ClassNotes), once per class.
func (b *builder) reportClassNotes() {
	if b.ann == nil || len(b.ann.ClassNotes) == 0 {
		return
	}
	names := make([]string, 0, len(b.ann.ClassNotes))
	for name := range b.ann.ClassNotes {
		if b.instanceCounts[name] > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b.warnings = append(b.warnings, b.ann.ClassNotes[name]...)
	}
}
