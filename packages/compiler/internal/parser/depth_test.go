package parser

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// TestParseDepthGuard verifies pathologically nested input is rejected with a
// diagnostic instead of exhausting the goroutine stack.
func TestParseDepthGuard(t *testing.T) {
	const n = 40000
	src := "const x = " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) + ";"
	l := lexer.New(src)
	p := New(l.Tokenize())
	_ = p.ParseProgram() // must return, not crash

	found := false
	for _, e := range p.Errors() {
		if strings.Contains(e.Error(), "nesting too deep") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a nesting-depth error, got: %v", p.Errors())
	}
}
