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
// detection and speculative type-argument rescans. It compares parse time for
// an input and one four times larger (deeper nesting, longer chains) instead
// of using a wall-clock limit, so it holds under -race and coverage on slow
// runners. Linear parsing gives a ratio near 4; quadratic gives near 16.
func TestNestedParensAreLinear(t *testing.T) {
	build := func(k int) string {
		var b strings.Builder
		for i := 0; i < 10; i++ {
			b.WriteString("x = " + strings.Repeat("(", 250*k) + "1" + strings.Repeat(")", 250*k) + ";\n")
		}
		b.WriteString("y = " + strings.Repeat("(a) + ", 5000*k) + "1;\n")
		b.WriteString("z = a" + strings.Repeat(" < a", 300*k) + ";\n")
		return b.String()
	}
	parse := func(src string) time.Duration {
		best := time.Duration(1<<63 - 1)
		for r := 0; r < 3; r++ {
			start := time.Now()
			p := newParser(src)
			p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatalf("unexpected errors: %v", p.Errors()[0])
			}
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	small, large := parse(build(1)), parse(build(4))
	if ratio := float64(large) / float64(small); ratio > 10 {
		t.Fatalf("parse time grew %.1fx for a 4x larger input (%v vs %v); expected linear scaling", ratio, small, large)
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
