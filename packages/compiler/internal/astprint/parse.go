package astprint

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// Parse lexes and parses TypeScript/JSX source, returning the program, the
// parse errors, and the number of type-only constructs the parser discarded.
// A non-zero dropped count means the program cannot reproduce the source: it
// is safe to inspect, but printing it would drop type information.
func Parse(src string) (*ast.Program, []error, int) {
	p := parser.New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	return prog, p.Errors(), p.DroppedTypes()
}

// CycleLossy reports whether printing prog and reparsing would lose constructs
// relative to the original source. It is a convenience for tooling: the source
// was parsed with the same parser, so `dropped` is the authoritative signal.
func CycleLossy(dropped int) bool { return dropped > 0 }
