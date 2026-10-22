package parser_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func newParser(src string) *parser.Parser {
	p := parser.New(lexer.New(src).Tokenize())
	p.SetSource(src)
	return p
}

// TestDeepInputsReportDepthError feeds ~200k-deep inputs of every recursive
// shape; each must produce a depth diagnostic with a hint, not a stack
// overflow.
func TestDeepInputsReportDepthError(t *testing.T) {
	const n = 200000
	inputs := map[string]string{
		"parens":      strings.Repeat("(", n) + "1" + strings.Repeat(")", n),
		"assign":      strings.Repeat("a=", n) + "1",
		"ternary":     strings.Repeat("a?b:", n) + "c",
		"exponent":    strings.Repeat("a**", n) + "b",
		"unary":       strings.Repeat("-", n) + "a",
		"arrows":      strings.Repeat("a=>", n) + "1",
		"arrays":      strings.Repeat("[", n) + strings.Repeat("]", n),
		"objects":     strings.Repeat("({a:", n) + "1" + strings.Repeat("})", n),
		"blocks":      strings.Repeat("{", n) + strings.Repeat("}", n),
		"jsx":         strings.Repeat("<a>", n) + strings.Repeat("</a>", n),
		"templates":   strings.Repeat("`${", n) + "1" + strings.Repeat("}`", n),
		"type-nested": "let x: " + strings.Repeat("A<", n) + "B" + strings.Repeat(">", n) + ";",
		"calls":       "f" + strings.Repeat("(g", n) + strings.Repeat(")", n),
	}
	for name, src := range inputs {
		t.Run(name, func(t *testing.T) {
			p := newParser(src)
			p.ParseProgram()
			found := false
			for _, e := range p.Errors() {
				if d, ok := e.(parser.Diagnostic); ok && strings.Contains(d.Message, "nesting too deep") && d.Hint != "" {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected a depth diagnostic, got %d errors (first: %v)", len(p.Errors()), firstErr(p.Errors()))
			}
		})
	}
}

func firstErr(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errs[0]
}

// TestNestedParensAreLinear guards against quadratic arrow-function
// detection: 50k nested parentheses (below the depth limit only when wrapped
// in separate statements) must parse quickly.
func TestNestedParensAreLinear(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("x = " + strings.Repeat("(", 1000) + "1" + strings.Repeat(")", 1000) + ";\n")
	}
	// One long flat chain of parenthesized operands as well.
	b.WriteString("y = " + strings.Repeat("(a) + ", 50000) + "1;\n")
	// Chained comparisons must not rescan speculative type arguments.
	b.WriteString("z = a" + strings.Repeat(" < a", 1500) + ";\n")
	src := b.String()
	start := time.Now()
	p := newParser(src)
	p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", p.Errors()[0])
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("parsing 50k nested/parenthesized operands took %v", d)
	}
}

func BenchmarkNestedParens(b *testing.B) {
	src := strings.Repeat("x = "+strings.Repeat("(", 1000)+"1"+strings.Repeat(")", 1000)+";\n", 20)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		newParser(src).ParseProgram()
	}
}

func BenchmarkArrowHeavy(b *testing.B) {
	src := strings.Repeat("const f = (a, b) => (c) => ({ a, b, c }); g((x) => x * 2, (y) => (y));\n", 500)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		newParser(src).ParseProgram()
	}
}
