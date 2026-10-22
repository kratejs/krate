package annotator

import (
	"strings"
	"testing"
)

func TestClassLoweringIssueNamesMember(t *testing.T) {
	cases := []struct {
		name, src, member, reason string
		line                      int
	}{
		{"shouldComponentUpdate", "class C extends Component {\n  shouldComponentUpdate() { return false; }\n  render() { return <div />; }\n}", "shouldComponentUpdate", "shouldComponentUpdate is not supported", 2},
		{"getSnapshotBeforeUpdate", "class C extends Component {\n  render() { return <div />; }\n  getSnapshotBeforeUpdate() { return null; }\n}", "getSnapshotBeforeUpdate", "getSnapshotBeforeUpdate", 3},
		{"getDerivedStateFromProps", "class C extends Component {\n  static getDerivedStateFromProps(p) { return null; }\n  render() { return <div />; }\n}", "getDerivedStateFromProps", "getDerivedStateFromProps", 2},
		{"error boundary", "class C extends Component {\n  componentDidCatch(e) {}\n  render() { return <div />; }\n}", "componentDidCatch", "error boundary", 2},
		{"static member", "class C extends Component {\n  static helper() { return 1; }\n  render() { return <div />; }\n}", "helper", "static member helper", 2},
		{"private member", "class C extends Component {\n  #secret = 1;\n  render() { return <div />; }\n}", "#secret", "private member #secret", 2},
		{"unknown this", "class C extends Component {\n  render() {\n    return <div>{this.other}</div>;\n  }\n}", "this.other", "this.other is not a field", 3},
		{"no render", "class C extends Component {\n  componentDidMount() {}\n}", "", "no render()", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := classLoweringIssue(firstClass(t, tc.src))
			if issue == nil {
				t.Fatal("expected an issue")
			}
			if issue.member != tc.member || !strings.Contains(issue.reason, tc.reason) || issue.hint == "" {
				t.Errorf("issue = %+v", issue)
			}
			if issue.pos.Line != tc.line {
				t.Errorf("line = %d, want %d", issue.pos.Line, tc.line)
			}
		})
	}
	ok := firstClass(t, `class C extends Component {
  state = { n: 0 };
  static defaultProps = { a: 1 };
  inc = () => this.setState({ n: this.state.n + 1 });
  constructor(props) { super(props); this.timer = 0; }
  componentDidMount() { this.timer = 1; }
  render() { return <button onClick={this.inc}>{this.state.n}{this.props.a}</button>; }
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
