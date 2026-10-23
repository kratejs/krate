package renderer

import "testing"

// TestClassComponentLoweringEmit checks the lowered class output end to end:
// the build folds render (derived state, getters, destructured state,
// defaultProps), and the hydration scopes register with the owner tree with
// the parent, creator and conditional-branch links the runtime needs.
func TestClassComponentLoweringEmit(t *testing.T) {
	src := `import { Component, PureComponent } from "react";
class Child extends Component {
  static defaultProps = { color: "blue" };
  componentDidUpdate(prevProps) { console.log(prevProps.n, this.props.n); }
  render() { return <span class={this.props.color}>{this.props.n}</span>; }
}
class Parent extends PureComponent {
  state = { n: 1, show: true };
  static getDerivedStateFromProps(props, state) { return { start: props.start, doubled: state.n * 2 }; }
  get label() { return "n=" + this.state.n; }
  shouldComponentUpdate(nextProps, nextState) { return nextState.n < 10; }
  inc = () => this.setState(s => ({ n: s.n + 1 }));
  render() {
    const { n, show } = this.state;
    return <div>
      <button onClick={this.inc}>{this.label}</button>
      <p>{this.state.start}:{this.state.doubled}:{n}</p>
      {show ? <Child n={n} /> : null}
    </div>;
  }
}
export default function App() { return <main><Parent start="s" /></main>; }`
	tree, html, js := buildTreeReact(t, src)
	if len(tree.Errors) > 0 {
		t.Fatalf("errors: %v", tree.Errors)
	}
	mustContain(t, "html", html, "n=1", "s", "2", `class="blue"`)
	mustContain(t, "js", js,
		`__krateOwnF("App_c0"`,
		`__krateOwnF("1.Parent.Parent_c0",{p:"App_c0",n:"Parent",k:"App_c0"}`,
		`n:"Child",c:"`, `b:"c",k:"1.Parent.Parent_c0"`,
		"(__krate_this = __krateClass(((typeof props === 'undefined')?null:props), __krate_class_state, __krate_class_setState, {name:'Parent',pure:true",
		"getDerivedStateFromProps:", "shouldComponentUpdate:", "defaultProps:__krate_class_defaults",
		"__krate_this.setState(",
		// Destructured state is read from the signal in each binding.
		"__krate_class_state().n",
	)
	mustNotContain(t, "js", js, "(function(){\nvar props")
}

// TestClassBoundaryChildrenBranch checks that children rendered through an
// error boundary's `return this.props.children` render and are linked to the
// boundary's children branch.
func TestClassBoundaryChildrenBranch(t *testing.T) {
	src := `import { Component, useState } from "react";
class Boundary extends Component {
  state = { error: null };
  static getDerivedStateFromError(error) { return { error: error.message }; }
  render() {
    if (this.state.error) return <p>failed: {this.state.error}</p>;
    return this.props.children;
  }
}
function Kid() { const [n, setN] = useState(0); return <b onClick={() => setN(n + 1)}>{n}</b>; }
export default function App() { return <main><Boundary><Kid /></Boundary></main>; }`
	tree, html, js := buildTreeReact(t, src)
	if len(tree.Errors) > 0 {
		t.Fatalf("errors: %v", tree.Errors)
	}
	mustContain(t, "html", html, "<b ", "failed:")
	mustContain(t, "js", js, `n:"Kid",c:"`, `b:"a"`, "getDerivedStateFromError:")
}
