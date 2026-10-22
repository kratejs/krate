package irtree

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func TestScanJSIdents(t *testing.T) {
	cases := []struct {
		code string
		want []string
	}{
		{`a.b + "c d" + 'e'`, []string{"a"}},
		{"`t ${d + x.y} e` + f", []string{"d", "f", "x"}},
		{"`outer ${`inner ${q}`} z`", []string{"q"}},
		{`/* skip */ g?.h // tail`, []string{"g"}},
		{`i?.(j) + k?.[l]`, []string{"i", "j", "k", "l"}},
		{`x = /a"b/g.test(s) ? m : n / 2`, []string{"m", "n", "s", "x"}},
		{`__krate_props["3J.TOCNav.TOCNav_c0"]={a:1}`, []string{"__krate_props", "a"}},
		{`{...rest, p: 1.5e3}`, []string{"p", "rest"}},
	}
	for _, tc := range cases {
		got := make([]string, 0)
		for name := range jsIdentRefs(tc.code) {
			got = append(got, name)
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("jsIdentRefs(%q) = %v, want %v", tc.code, got, tc.want)
		}
	}
	if codeContainsIdent(`alert("Toggle theme")`, "Toggle") {
		t.Error("an identifier inside a string literal is not a reference")
	}
	if !codeContainsIdent(`h(Toggle,{})`, "Toggle") {
		t.Error("a bare identifier is a reference")
	}
}

func TestBindingNames(t *testing.T) {
	prog := parseInternal(t, `function f({a, b: c, ...rest}, [d, e = 1], g) {
  const h = 1, [i, j] = k;
  var l;
  if (x) { var m = 2; let n = 3; }
  function o() {}
}`)
	fn := prog.Body[0].(*ast.FnDecl)
	got := functionScopeNames(fn.Params, fn.Body)
	sort.Strings(got)
	want := []string{"a", "c", "d", "e", "g", "h", "i", "j", "l", "m", "o", "rest"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("functionScopeNames = %v, want %v", got, want)
	}
}

func TestSetterNameForEmpty(t *testing.T) {
	if got := setterNameFor(""); got != "" {
		t.Errorf("setterNameFor(\"\") = %q", got)
	}
	if got := setterNameFor("count"); got != "setCount" {
		t.Errorf("setterNameFor(count) = %q", got)
	}
}

func TestLowerRenderReturnShapes(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		want  string // rendered JS of the lowered value
		issue string // substring of the issue message
	}{
		{"single return untouched", `return <a/>;`, `h('a',null)`, ""},
		{"guard", `if (l) return <s/>; return <m/>;`, `(l?h('s',null):h('m',null))`, ""},
		{"null guard", `if (!data) return null; const x = data.y; return <m/>;`, `(!data?null:h('m',null))`, ""},
		{"bare return", `if (!ok) return; return <m/>;`, `(!ok?null:h('m',null))`, ""},
		{"else-if", `if (a) { return <x/>; } else if (b) return <y/>; return <z/>;`, `(a?h('x',null):(b?h('y',null):h('z',null)))`, ""},
		{"if-else both return", `if (a) return <x/>; else return <y/>;`, `(a?h('x',null):h('y',null))`, ""},
		{"nested block", `{ if (a) { { return <x/>; } } } return <y/>;`, `(a?h('x',null):h('y',null))`, ""},
		{"falls off the end", `if (a) return <x/>;`, `(a?h('x',null):null)`, ""},
		{"switch", `switch (k) { case 1: return <a/>; case 2: case 3: return <b/>; default: return <c/>; }`,
			`((k === 1)?h('a',null):(((k === 2) || (k === 3))?h('b',null):h('c',null)))`, ""},
		{"switch break falls through", `switch (k) { case 1: return <a/>; case 2: break; } return <z/>;`,
			`((k === 1)?h('a',null):((k === 2)?h('z',null):h('z',null)))`, ""},
		{"statement in branch", `if (a) { log(); return <x/>; } return <y/>;`, "", "statement inside an early-return branch"},
		{"return in loop", `for (const i of xs) { return <x/>; } return <y/>;`, "", "inside a loop"},
		{"switch case with statements", `switch (k) { case 1: log(); return <a/>; } return <z/>;`, "", "switch case"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseInternal(t, "function C() {\n"+tc.body+"\n}")
			fn := prog.Body[0].(*ast.FnDecl)
			ret, issue := lowerRenderReturn(fn.Body)
			if tc.issue != "" {
				if issue == nil || !strings.Contains(issue.msg, tc.issue) || issue.hint == "" {
					t.Fatalf("issue = %+v, want %q", issue, tc.issue)
				}
				return
			}
			if issue != nil {
				t.Fatalf("unexpected issue: %+v", issue)
			}
			if ret == nil {
				t.Fatal("no render return")
			}
			if got := generateExprJS(ret.Value, nil); got != tc.want {
				t.Errorf("lowered = %s\nwant      %s", got, tc.want)
			}
		})
	}
}

func parseInternal(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}
