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

// TestSynthesizeClassComponentRejectsUnsupported ensures classes outside the
// supported subset are left for the specific build diagnostic instead of being
// lowered incorrectly.
func TestSynthesizeClassComponentRejectsUnsupported(t *testing.T) {
	cases := map[string]string{
		"unknown this":  `class C extends Component { render() { return <div>{this.other}</div>; } }`,
		"static helper": `class C extends Component { static helper() { return 1; } render() { return <div />; } }`,
		"will mount":    `class C extends Component { componentWillMount() {} render() { return <div />; } }`,
	}
	for name, src := range cases {
		if _, ok := synthesizeClassComponent(firstClass(t, src)); ok {
			t.Errorf("%s: expected lowering to be rejected", name)
		}
	}
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

func TestSynthesizeClassComponentLowersDidUpdate(t *testing.T) {
	class := firstClass(t, `
		class Counter extends Component {
			state = { n: 0 };
			componentDidUpdate(prevProps, prevState) {
				if (prevState.n !== this.state.n) console.log(prevState.n);
			}
			render() { return <div>{this.state.n}</div>; }
		}
	`)
	if _, ok := synthesizeClassComponent(class); !ok {
		t.Fatal("expected componentDidUpdate to lower to a state-diff effect")
	}
}

func TestSynthesizeClassComponentLowersMembers(t *testing.T) {
	class := firstClass(t, `
		class Counter extends Component {
			state = { n: 0 };
			count = 0;
			increment = () => { this.setState({ n: this.state.n + 1 }); };
			step() { this.increment(); this.count = this.count + 1; }
			render() {
				return <button onClick={this.step}>{this.state.n}:{this.count}</button>;
			}
		}
	`)
	fn, ok := synthesizeClassComponent(class)
	if !ok || fn == nil {
		t.Fatal("expected helper methods, arrow fields, and instance fields to lower")
	}
	if fn.Name != "Counter" {
		t.Errorf("lowered function name = %q, want Counter", fn.Name)
	}
}
