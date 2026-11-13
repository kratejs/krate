package annotator

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func firstClass(t *testing.T, src string) *ast.ClassDecl {
	t.Helper()
	prog := parseProg(t, src)
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.ClassDecl:
			return s
		case *ast.ExportStmt:
			if c, ok := s.Declaration.(*ast.ClassDecl); ok {
				return c
			}
		}
	}
	t.Fatalf("no class declaration parsed from %q", src)
	return nil
}

func TestSynthesizeClassComponentLowersSupported(t *testing.T) {
	class := firstClass(t, `
		class Counter extends Component {
			state = { n: 0 };
			render() {
				return <button onClick={() => this.setState({ n: this.state.n + 1 })}>{this.state.n}</button>;
			}
		}
	`)
	fn, ok := synthesizeClassComponent(class)
	if !ok || fn == nil {
		t.Fatal("expected a supported class to lower")
	}
	if fn.Name != "Counter" {
		t.Errorf("lowered function name = %q, want Counter", fn.Name)
	}
}
