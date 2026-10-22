package bundler

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func refDiagCodes(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, d := range RewriteReactDiag(parseProg(t, src), "page.tsx", src) {
		if strings.HasPrefix(string(d.Code), "KR5") {
			if d.Hint == "" || d.Line == 0 {
				t.Errorf("diagnostic needs a position and hint: %+v", d)
			}
			out = append(out, string(d.Code))
		}
	}
	return out
}

func TestStringRefDiagnosticInClassAndFunction(t *testing.T) {
	src := `import { Component } from "react";
export class Old extends Component {
  render() { return <input ref="name" />; }
}
export function F() { return <div ref='box' />; }`
	got := refDiagCodes(t, src)
	if strings.Join(got, ",") != string(diag.CodeStringRef)+","+string(diag.CodeStringRef) {
		t.Fatalf("want two KR5003, got %v", got)
	}
}

func TestRefReadInRenderDiagnostic(t *testing.T) {
	src := `import { useRef, useState, useEffect } from "react";
export default function App() {
  const clicks = useRef(0);
  const el = useRef(null);
  const [n, setN] = useState(0);
  useEffect(() => { console.log(el.current, clicks.current); });
  return <div ref={el} onClick={() => { clicks.current += 1; setN(clicks.current); }}>
    <p>{clicks.current}</p>
    <p title={String(clicks.current)}>{n}</p>
  </div>;
}`
	got := refDiagCodes(t, src)
	if len(got) != 1 || got[0] != string(diag.CodeRefReadInRender) {
		t.Fatalf("want one KR5008 (reported once per ref, handlers and effects exempt), got %v", got)
	}
}
