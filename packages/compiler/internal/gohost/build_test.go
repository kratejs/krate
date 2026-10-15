package gohost

import "testing"

// TestBuildDiagnostics verifies Go compiler output is parsed into structured
// diagnostics with generated paths remapped to source.
func TestBuildDiagnostics(t *testing.T) {
	srcMap := map[string]string{
		"/p/.krate/goapi/components/ui/card.go": "/p/src/components/ui/card.go",
	}
	diags := buildDiagnostics("/p/.krate/goapi/components/ui/card.go:5:2: undefined: Foo", srcMap)
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %v", diags)
	}
	d := diags[0]
	if d.File != "/p/src/components/ui/card.go" || d.Line != 5 || d.Col != 2 {
		t.Errorf("diagnostic = %+v", d)
	}
}
