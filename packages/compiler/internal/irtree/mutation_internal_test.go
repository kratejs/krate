package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseBody(t *testing.T, src string) []ast.Stmt {
	t.Helper()
	p := parser.New(lexer.New("function C(){" + src + "}").Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse %q: %v", src, errs)
	}
	return prog.Body[0].(*ast.FnDecl).Body
}

func TestCollectLocalVarsTracksMutations(t *testing.T) {
	cases := []struct {
		name string
		body string
		// want maps a local to its expected folded value ("?" means no
		// build-time value).
		want map[string]string
	}{
		{"push folds", `const a = [1]; a.push(2, 3);`, map[string]string{"a": "[1,2,3]"}},
		{"push unknown", `const a = [1]; a.push(Math.random());`, map[string]string{"a": "?"}},
		{"other mutator", `const a = [2, 1]; a.sort();`, map[string]string{"a": "?"}},
		{"pure method keeps value", `const a = [1]; a.map(x => x);`, map[string]string{"a": "[1]"}},
		{"member write folds", `const o = {n: 1}; o.n = 2; o["m"] = "x";`, map[string]string{"o": "{n:2,m:'x'}"}},
		{"nested member write", `const o = {i: {n: 1}}; o.i.n = 2;`, map[string]string{"o": "?"}},
		{"alias", `const p = {n: 1}; const q = p; q.n = 2;`, map[string]string{"p": "?", "q": "?"}},
		{"contained alias", `const p = [1]; const box = [p]; box[0].push(2);`, map[string]string{"p": "?", "box": "?"}},
		{"rebinding alias keeps original", `const p = {n: 1}; let q = p; q = {n: 2};`, map[string]string{"p": "{n:1}"}},
		{"alias by reassignment", `const p = {n: 1}; let q = {}; q = p; q.n = 2;`, map[string]string{"p": "?"}},
		{"map mutation", `const m = new Map(); m.set("k", 1);`, map[string]string{"m": "?"}},
		{"delete", `const o = {n: 1}; delete o.n;`, map[string]string{"o": "?"}},
		{"update", `let n = 1; n++; n--; n++;`, map[string]string{"n": "2"}},
		{"compound", `let s = "a"; s += "b";`, map[string]string{"s": "ab"}},
		{"unknown rhs", `let s = "a"; s = Math.random();`, map[string]string{"s": "?"}},
		{"unknown branch", `let s = "a"; if (Math.random()) { s = "b"; }`, map[string]string{"s": "?"}},
		{"known branch", `let s = "a"; if (true) { s = "b"; }`, map[string]string{"s": "b"}},
		{"loop", `let s = 0; for (let i = 0; i < 3; i++) { s += i; }`, map[string]string{"s": "?"}},
		{"callback", `const a = []; const b = [1]; b.forEach(x => a.push(x));`, map[string]string{"a": "?", "b": "[1]"}},
		{"local helper", `const a = []; function add() { a.push(1); } add();`, map[string]string{"a": "?"}},
		{"helper param", `const a = []; const add = (xs) => { xs.push(1); }; add(a);`, map[string]string{"a": "?"}},
		{"helper not called", `const a = [1]; const add = () => { a.push(1); };`, map[string]string{"a": "[1]"}},
		{"effect runs later", `const a = [1]; createEffect(() => { a.push(2); });`, map[string]string{"a": "[1]"}},
		{"string method", `const s = "ab"; s.trim();`, map[string]string{"s": "ab"}},
		{"in initializer", `const a = [1]; const n = a.push(2);`, map[string]string{"a": "?", "n": "?"}},
		{"Object.assign", `const o = {}; Object.assign(o, {a: 1});`, map[string]string{"o": "?"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fold := collectLocalVars(parseBody(t, tc.body), nil, nil, nil)
			for name, want := range tc.want {
				v, ok := fold.locals[name]
				if !ok {
					t.Fatalf("%s: local not collected", name)
				}
				got := "?"
				if !v.ContainsUnknown() {
					got = valueJSLiteral(v)
					if v.TypeOf() == "string" {
						got = v.ToString()
					}
				}
				if got != want {
					t.Errorf("%s = %s, want %s", name, got, want)
				}
			}
		})
	}
}
