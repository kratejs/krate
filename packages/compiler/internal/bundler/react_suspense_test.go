package bundler

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// rewriteSrcDiags parses src, runs the React lowering, and returns the printed
// program and its diagnostics.
func rewriteSrcDiags(t *testing.T, src string) (string, []diag.Diagnostic) {
	t.Helper()
	prog := parseProg(t, src)
	diags := RewriteReactDiagnostics(prog)
	return astprint.Print(prog), diags
}

func TestLazyLowersToImportAndMarker(t *testing.T) {
	out, diags := rewriteSrcDiags(t, `
		import { lazy, Suspense } from 'react';
		const Chart = lazy(() => import('./Chart'));
		export default function Page() {
			return <Suspense fallback={<p>loading</p>}><Chart /></Suspense>;
		}
	`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !strings.Contains(out, `import Chart from "./Chart"`) {
		t.Errorf("expected a static default import:\n%s", out)
	}
	if !strings.Contains(out, `__krate_lazy__Chart = "./Chart"`) {
		t.Errorf("expected the lazy marker:\n%s", out)
	}
	if strings.Contains(out, "lazy(") || strings.Contains(out, "import(") {
		t.Errorf("lazy() call should be gone:\n%s", out)
	}
}
