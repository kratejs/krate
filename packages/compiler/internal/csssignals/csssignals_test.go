package csssignals

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return p.ParseProgram()
}

func componentOf(t *testing.T, src string) *ast.FnDecl {
	t.Helper()
	prog := parseProg(t, src)
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			return s
		case *ast.ExportStmt:
			if fn, ok := s.Declaration.(*ast.FnDecl); ok {
				return fn
			}
		}
	}
	t.Fatal("no component function found")
	return nil
}

func analyze(t *testing.T, src string) *Analyzer {
	t.Helper()
	fn := componentOf(t, src)
	next := 0
	return Analyze(fn.Name, fn.Body, func(_, _ string) int {
		i := next
		next++
		return i
	})
}

const tabSrc = `export default function Tabs() {
	const [tab, setTab] = createCSSChoice('overview');
	return (
		<div class="tabs">
			<button onClick={() => setTab('overview')}>Overview</button>
			<button onClick={() => setTab('features')}>Features</button>
			<div showIf={tab() === 'overview'}>Overview content</div>
			<div showIf={tab() === 'features'}>Features content</div>
		</div>
	);
}`

func TestAnalyzeChoiceOptions(t *testing.T) {
	a := analyze(t, tabSrc)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	s := a.Scopes()[0]
	if s.Kind != KindChoice {
		t.Errorf("kind = %v", s.Kind)
	}
	if len(s.Options) != 2 || s.Options[0] != "overview" || s.Options[1] != "features" {
		t.Errorf("options = %v", s.Options)
	}
	if s.Class != ClassPrefix+"0" {
		t.Errorf("class = %q", s.Class)
	}
}

// CSS generation

func TestStylesheetChoiceWellFormed(t *testing.T) {
	css := Stylesheet(analyze(t, tabSrc).Scopes(), nil)
	if strings.Contains(css, "display:none},") {
		t.Errorf("base rule joined to the next by a dangling comma:\n%s", css)
	}
	if !strings.Contains(css, ".krc0-r-overview:checked") {
		t.Errorf("missing radio rule:\n%s", css)
	}
	if !strings.Contains(css, "display:contents") {
		t.Errorf("panels should be toggled via display:contents wrapper:\n%s", css)
	}
}
