package build

import (
	"strings"
	"testing"
)

// TestAnnotateGoCalls verifies __krateGoId is injected immediately after a
// Go-component tag name (not mid-name, not on other tags).
func TestAnnotateGoCalls(t *testing.T) {
	src := "import { Badge } from \"../components/badge.server.go\";\n\n" +
		"export default function Page() {\n" +
		"\treturn <main className=\"x\"><Badge label=\"Hi\" /><Clock zone=\"UTC\">kid</Clock></main>;\n" +
		"}\n"
	out := annotateGoCalls(src, "page.tsx", map[string]bool{"Badge": true, "Clock": true})

	if !strings.Contains(out, "<Badge __krateGoId=\"gc4_") {
		t.Fatalf("Badge not annotated after tag name:\n%s", out)
	}
	if !strings.Contains(out, "<Clock __krateGoId=\"gc4_") {
		t.Fatalf("Clock not annotated after tag name:\n%s", out)
	}
	if strings.Contains(out, "<main __krateGoId") {
		t.Fatalf("non-Go tag was annotated:\n%s", out)
	}
	// The annotated source must still be valid TSX.
	if parseProgram("page.tsx", out) == nil {
		t.Fatalf("annotated source failed to parse:\n%s", out)
	}
	// Re-running annotation must not double-inject (already idempotent per call
	// only if ids differ; ensure the tag name check is exact).
	if strings.Count(out, "__krateGoId") != 2 {
		t.Fatalf("expected exactly 2 annotations:\n%s", out)
	}
}

// TestAnnotateGoCallsNoLocals is a no-op when the module has no Go components.
func TestAnnotateGoCallsNoLocals(t *testing.T) {
	src := "export default function P() { return <div>hi</div>; }"
	if got := annotateGoCalls(src, "p.tsx", nil); got != src {
		t.Fatalf("expected unchanged source, got:\n%s", got)
	}
}
