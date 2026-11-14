package renderer

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// Test helpers

func parseProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func fullPipeline(t *testing.T, src string) (*EmitResult, string) {
	t.Helper()
	prog := parseProg(t, src)
	cfg := &config.Config{}
	ann := annotator.Annotate(prog, cfg, "test.tsx", src)
	tree := irtree.Build(prog, ann)
	emitter := NewEmitter()
	result := emitter.Emit(tree)
	EmitMeta(tree, result)
	hydrationJS := GenerateNewHydrationJS(result)
	return result, hydrationJS
}

func TestEscapeJSString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"it's", "it\\'s"},
		{"line\nbreak", "line\\nbreak"},
		{"back\\slash", "back\\\\slash"},
	}
	for _, tt := range tests {
		got := escapeJSString(tt.input)
		if got != tt.expected {
			t.Errorf("escapeJSString(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
