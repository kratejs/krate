package parser

import (
	"testing"

	"krate-compiler/internal/lexer"
)

// testUnsupported ensures that parsing `src` produces at least one diagnostic.
// It guards constructs that Krate does not support and would otherwise be
// silently mangled or dropped, emitting incorrect or missing output. They must
// surface a clear error instead.
func testUnsupported(t *testing.T, name, src string) {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := New(tokens)
	p.Filename = "test.tsx"
	_ = p.ParseProgram()
	if errs := p.Errors(); len(errs) == 0 {
		t.Errorf("expected unsupported syntax %q to error, but it parsed cleanly", name)
	} else {
		t.Logf("OK   %s: %v", name, errs[0])
	}
}

func TestUnsupportedSyntax(t *testing.T) {
	tests := []struct{ name, src string }{
		// Operator mangling: these previously produced incorrect code.
		{"exp-op", "const x = 2 ** 10;"},
		{"nullish-assign", "x ??= 1;"},
		{"logical-assign", "x &&= 1;"},
		{"or-assign", "x ||= 1;"},
		{"shl-assign", "x <<= 1;"},
		{"shr-assign", "x >>= 1;"},

		// Unknown characters are unlexable and must error, not be skipped.
		{"unknown-char", "#private-field;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testUnsupported(t, tt.name, tt.src)
		})
	}
}
