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

func TestPrintSimpleProgram(t *testing.T) {
	out := roundTrip(t, `import { createSignal } from '@krate/runtime';
const x = 1;
function add(a, b) { return a + b; }
`)
	if !strings.Contains(out, "import { createSignal } from") {
		t.Errorf("missing import: %q", out)
	}
	if !strings.Contains(out, "const x = 1;") {
		t.Errorf("missing const: %q", out)
	}
	if !strings.Contains(out, "function add(a, b)") {
		t.Errorf("missing function: %q", out)
	}
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

func TestPrintArrowAndObject(t *testing.T) {
	src := `const f = (x) => x * 2;
const o = { a: 1, b: 'two', c };
const items = arr.map((it) => it.id);
`
	out := roundTrip(t, src)
	if !strings.Contains(out, "=> x * 2") {
		t.Errorf("missing arrow: %q", out)
	}
	if !strings.Contains(out, "a: 1") {
		t.Errorf("missing object prop: %q", out)
	}
}

func TestPrintControlFlow(t *testing.T) {
	roundTrip(t, `if (a) { b(); } else if (c) { d(); } else { e(); }
for (let i = 0; i < 10; i++) { x += i; }
while (x) { x--; }
try { f(); } catch (e) { g(e); } finally { h(); }
`)
}

func TestPrintPrecedence(t *testing.T) {
	out := roundTrip(t, `const y = (a + b) * c;`)
	if !strings.Contains(out, "(a + b) * c") {
		t.Errorf("expected parenthesized grouping, got %q", out)
	}
}

func TestPrintStringEscapesRoundTrip(t *testing.T) {
	// Decoded values must be re-escaped so the printed source reparses to the
	// same runtime string (backslash, quote, newline, tab).
	out := roundTrip(t, `const s = "a\\b \"q\" \n\tend";`)
	if !strings.Contains(out, `\\`) {
		t.Errorf("expected escaped backslash in output: %q", out)
	}
	if !strings.Contains(out, `\n`) || !strings.Contains(out, `\t`) {
		t.Errorf("expected escaped newline/tab in output: %q", out)
	}
}

func TestDroppedTypesCounter(t *testing.T) {
	p := parser.New(lexer.New(`interface Foo { a: number }
type Bar = string;
export function f(x: Foo): number { return 1; }
`).Tokenize())
	p.ParseProgram()
	if p.DroppedTypes() == 0 {
		t.Fatal("expected DroppedTypes > 0 for typed source")
	}
}
