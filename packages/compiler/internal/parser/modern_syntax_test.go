package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// TestModernSyntaxParses guards modern operators and private members that
// compiled libraries emit. These previously aborted the parse; they must now
// parse cleanly (Krate only executes the parts it understands).
func TestModernSyntaxParses(t *testing.T) {
	tests := []struct{ name, src string }{
		{"exp-op", "const x = 2 ** 10;"},
		{"exp-assign", "x **= 2;"},
		{"nullish", "const y = a ?? b;"},
		{"nullish-assign", "x ??= 1;"},
		{"logical-assign", "x &&= 1;"},
		{"or-assign", "x ||= 1;"},
		{"shl-assign", "x <<= 1;"},
		{"shr-assign", "x >>= 1;"},
		{"shl", "const x = a << b;"},
		{"shr", "const x = a >> b;"},
		{"ushr", "const x = a >>> b;"},
		{"private-member", "const z = obj.#value;"},
		{"private-field", "class C { #x = 1; }"},
		{"spread-call", "run(...args);"},
		{"export-alias", "const a = 1; export { a as b };"},
		{"this-param", "function f(this: any, x: string) { return x; }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(lexer.New(tt.src).Tokenize())
			p.Filename = "test.tsx"
			_ = p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("%s: unexpected parse error: %v", tt.name, errs[0])
			}
		})
	}
}
