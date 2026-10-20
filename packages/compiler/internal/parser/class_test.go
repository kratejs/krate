package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// TestParseClassMembers verifies class declarations are retained with their
// members (previously class bodies were discarded entirely), which is the
// prerequisite for lowering React class components.
func TestParseClassMembers(t *testing.T) {
	src := `
		class Counter extends React.Component {
			state = { count: 0 };
			#secret = 1;
			static displayName = "Counter";
			constructor(props: Props) {
				super(props);
				this.state = { count: 0 };
			}
			componentDidMount(this: Counter) {}
			render() { return this.state.count; }
		}
	`
	p := New(lexer.New(src).Tokenize())
	p.Filename = "test.tsx"
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs[0])
	}
	var decl *ast.ClassDecl
	for _, stmt := range prog.Body {
		if c, ok := stmt.(*ast.ClassDecl); ok {
			decl = c
		}
	}
	if decl == nil {
		t.Fatal("class declaration was not retained")
	}
	if decl.Name != "Counter" {
		t.Errorf("class name = %q, want Counter", decl.Name)
	}
	want := map[string]ast.ClassMemberKind{
		"state":             ast.ClassField,
		"#secret":           ast.ClassField,
		"displayName":       ast.ClassField,
		"constructor":       ast.ClassConstructor,
		"componentDidMount": ast.ClassMethod,
		"render":            ast.ClassMethod,
	}
	got := map[string]ast.ClassMemberKind{}
	for _, m := range decl.Members {
		got[m.Name] = m.Kind
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("member %q kind = %v, want %v (members: %v)", name, got[name], kind, got)
		}
	}
}
