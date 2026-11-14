package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

func testParse(t *testing.T, name, src string) {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := New(tokens)
	p.Filename = "test.tsx"
	_ = p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("FAIL %s: %v", name, e)
		}
	} else {
		t.Logf("OK   %s", name)
	}
}
