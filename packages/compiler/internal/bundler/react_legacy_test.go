package bundler

import (
	"strings"
	"testing"
)

func TestLegacyFindDOMNodeReported(t *testing.T) {
	_, diags := rewriteDiag(t, `import { Component } from "react";
import { findDOMNode } from "react-dom";
import * as ReactDOM from "react-dom";
export default class Legacy extends Component {
  componentDidMount() {
    findDOMNode(this);
    ReactDOM.render(null, document.body);
  }
  render() { return <div>legacy</div>; }
}`)
	var apis []string
	for _, d := range diags {
		if d.Code == "KR8001" {
			apis = append(apis, d.Message)
			if d.Hint == "" {
				t.Errorf("KR8001 needs a hint: %+v", d)
			}
		}
	}
	if len(apis) != 2 || !strings.Contains(apis[0], "findDOMNode") || !strings.Contains(apis[1], "render") {
		t.Fatalf("want findDOMNode and render reported, got %v", apis)
	}
	if d := findDiag(t, diags, "KR8001"); d.Line != 6 {
		t.Errorf("findDOMNode should be reported at its call (line 6), got %d", d.Line)
	}
}

func TestLegacyContextReportedOncePerClass(t *testing.T) {
	_, diags := rewriteDiag(t, `import { Component } from "react";
class Child extends Component {
  static contextTypes = { color: () => null };
  render() { return <span>{this.context.color}</span>; }
}
export default class Parent extends Component {
  static childContextTypes = { color: () => null };
  getChildContext() { return { color: "purple" }; }
  render() { return <Child />; }
}`)
	n := 0
	for _, d := range diags {
		if d.Code == "KR8005" {
			n++
			if !strings.Contains(d.Hint, "createContext") {
				t.Errorf("hint should point at createContext: %q", d.Hint)
			}
		}
	}
	if n != 2 {
		t.Fatalf("want one KR8005 per class, got %d: %v", n, diags)
	}
}

func TestModernContextNotReported(t *testing.T) {
	_, diags := rewriteDiag(t, `import { Component, createContext } from "react";
const Theme = createContext("light");
export default class Label extends Component {
  static contextType = Theme;
  getChildContextValue() { return 1; }
  render() { return <span>{String(this.context)}</span>; }
}`)
	for _, d := range diags {
		if d.Code == "KR8005" || d.Code == "KR8001" {
			t.Fatalf("unexpected %s: %s", d.Code, d.Message)
		}
	}
}
