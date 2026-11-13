package astprint

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// roundTrip parses src, prints it, re-parses the output, and fails if either
// parse reports errors.
func roundTrip(t *testing.T, src string) string {
	t.Helper()
	p1 := parser.New(lexer.New(src).Tokenize())
	prog := p1.ParseProgram()
	if errs := p1.Errors(); len(errs) > 0 {
		t.Fatalf("initial parse failed: %v", errs)
	}
	out := Print(prog)
	p2 := parser.New(lexer.New(out).Tokenize())
	p2.ParseProgram()
	if errs := p2.Errors(); len(errs) > 0 {
		t.Fatalf("reparse failed for output:\n%s\nerrors: %v", out, errs)
	}
	return out
}

func TestPrintJSXAndExpr(t *testing.T) {
	out := roundTrip(t, `export default function App() {
  return <div class="card"><h1>Hello</h1>{count() > 0 ? 'yes' : 'no'}</div>;
}
`)
	if !strings.Contains(out, `<div class="card">`) {
		t.Errorf("missing div: %q", out)
	}
	if !strings.Contains(out, "{count() > 0 ? ") {
		t.Errorf("missing conditional: %q", out)
	}
}
