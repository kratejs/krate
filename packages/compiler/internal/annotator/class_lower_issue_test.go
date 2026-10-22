package annotator

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestClassLoweringIssueNamesMember(t *testing.T) {
	cases := []struct {
		name, src, member, reason string
		code                      diag.Code
		line                      int
	}{
		{"static member", "class C extends Component {\n  static helper() { return 1; }\n  render() { return <div />; }\n}", "static helper", "static member helper", diag.CodeClassStaticMember, 2},
		{"static block", "class C extends Component {\n  static { init(); }\n  render() { return <div />; }\n}", "static {}", "static initialization blocks", diag.CodeClassStaticMember, 2},
		{"unknown this", "class C extends Component {\n  render() {\n    return <div>{this.other}</div>;\n  }\n}", "this.other", "this.other is not a field", diag.CodeClassUnknownThis, 3},
		{"string refs", "class C extends Component {\n  componentDidMount() { this.refs.x.focus(); }\n  render() { return <div />; }\n}", "this.refs", "this.refs is not a field", diag.CodeClassUnknownThis, 2},
		{"no render", "class C extends Component {\n  componentDidMount() {}\n}", "", "no render()", diag.CodeClassNoRender, 1},
		{"will mount", "class C extends Component {\n  UNSAFE_componentWillMount() {}\n  render() { return <div />; }\n}", "UNSAFE_componentWillMount", "UNSAFE_componentWillMount is not supported", diag.CodeClassWillMount, 2},
		{"legacy context", "class C extends Component {\n  static contextTypes = {};\n  render() { return <div />; }\n}", "static contextTypes", "legacy context", diag.CodeClassLegacyContext, 2},
		{"static this", "class C extends Component {\n  static getDerivedStateFromProps(p) {\n    return this.x;\n  }\n  render() { return <div />; }\n}", "static getDerivedStateFromProps", "`this` inside static", diag.CodeClassStaticThis, 3},
		{"computed", "class C extends Component {\n  [key]() {}\n  render() { return <div />; }\n}", "[key]", "computed member names", diag.CodeClassComputedMember, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := classLoweringIssue(firstClass(t, tc.src))
			if issue == nil {
				t.Fatal("expected an issue")
			}
			if issue.member != tc.member || !strings.Contains(issue.reason, tc.reason) || issue.hint == "" || issue.code != tc.code {
				t.Errorf("issue = %+v", issue)
			}
			if issue.pos.Line != tc.line {
				t.Errorf("line = %d, want %d", issue.pos.Line, tc.line)
			}
			if _, ok := diag.Lookup(issue.code); !ok {
				t.Errorf("code %s is not registered", issue.code)
			}
		})
	}
	ok := firstClass(t, `class C extends PureComponent {
  #secret = 1;
  static getDerivedStateFromProps(p, s) { return null; }
  static getDerivedStateFromError(e) { return { failed: true }; }
  static contextType = Theme;
  shouldComponentUpdate(np, ns) { return true; }
  getSnapshotBeforeUpdate() { return null; }
  componentDidCatch(e, info) {}
  UNSAFE_componentWillReceiveProps(np) {}
  get total() { return this.state.n + this.#secret; }
  state = { n: 0 };
  static defaultProps = { a: 1 };
  inc = () => this.setState({ n: this.state.n + 1 });
  constructor(props) { super(props); this.timer = 0; }
  componentDidMount() { this.timer = 1; this.interval = setInterval(this.inc, 10); }
  componentWillUnmount() { clearInterval(this.interval); }
  render() { return <button onClick={this.inc}>{this.state.n}{this.props.a}{this.total}{this.context}</button>; }
}`)
	if issue := classLoweringIssue(ok); issue != nil {
		t.Errorf("supported class rejected: %+v", issue)
	}
	// The pre-check and the lowering agree.
	if _, lowered := synthesizeClassComponent(ok); !lowered {
		t.Error("synthesizeClassComponent rejected a class classLoweringIssue accepts")
	}
	for _, tc := range cases {
		if _, lowered := synthesizeClassComponent(firstClass(t, tc.src)); lowered {
			t.Errorf("%s: synthesizeClassComponent lowered a class with an issue", tc.name)
		}
	}
}

func TestClassBoundaryWithoutDerivedStateIsNoted(t *testing.T) {
	m := analyzeClass(firstClass(t, "class B extends Component {\n  componentDidCatch(e) {}\n  render() { return <div />; }\n}"))
	if m.issue != nil || !m.isBoundary() {
		t.Fatalf("boundary should lower: issue=%+v", m.issue)
	}
	if len(m.notes) != 1 || m.notes[0].code != diag.CodeBoundaryNoDerivedState || m.notes[0].pos.Line != 2 {
		t.Errorf("notes = %+v", m.notes)
	}
}
