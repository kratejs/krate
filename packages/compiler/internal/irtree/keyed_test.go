package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func parseMapCall(t *testing.T, src string) *ast.CallExpr {
	t.Helper()
	prog := parseExprAsProgram(src)
	if prog == nil || len(prog.Body) != 1 {
		t.Fatalf("parse %q", src)
	}
	es, ok := prog.Body[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("not an expression: %q", src)
	}
	call, ok := es.Expression.(*ast.CallExpr)
	if !ok {
		t.Fatalf("not a call: %q", src)
	}
	return call
}

func TestPlanMapCall(t *testing.T) {
	cases := []struct {
		src      string
		keyJS    string
		accessor bool
		holes    bool
		element  bool
	}{
		{`xs.map(x => <li key={x.id}>{x.name}</li>)`, "(x)=>x.id", true, false, true},
		{`xs.map((x, i) => <li key={i}>{x}</li>)`, "(x, i)=>i", true, false, true},
		{"xs.map(x => <li key={`k${x.id}`}/>)", "(x)=>`k${x.id}`", true, false, true},
		{`xs.map(x => x.ok ? <a key={x.id}/> : <b key={x.id}/>)`, "(x)=>(x.ok?x.id:x.id)", true, false, true},
		{`xs.map(x => x.ok ? <a key={x.id}/> : <b/>)`, "(x)=>(x.ok?x.id:undefined)", true, true, true},
		{`xs.map(x => { const id = x.id; return <li key={id}/>; })`, "(x)=>{\n  const id=x.id;\n  return id;\n}", true, false, true},
		{`xs.map(({ id }) => <li key={id}/>)`, "({id})=>id", false, false, true},
		{`xs.map(x => x.label)`, "", true, false, false},
		{`xs.map(renderRow)`, "", false, false, false},
	}
	for _, tc := range cases {
		plan := planMapCall(parseMapCall(t, tc.src))
		if plan == nil {
			t.Errorf("%s: no plan", tc.src)
			continue
		}
		if plan.KeyJS != tc.keyJS || plan.Accessor != tc.accessor || plan.KeyHoles != tc.holes || plan.ReturnsElement != tc.element {
			t.Errorf("%s: key=%q accessor=%v holes=%v element=%v", tc.src, plan.KeyJS, plan.Accessor, plan.KeyHoles, plan.ReturnsElement)
		}
	}
}

func TestRowRenderIsReactive(t *testing.T) {
	plan := planMapCall(parseMapCall(t, `xs.map(x => <li key={x.id} class={x.cls} onClick={() => pick(x.id)}>{x.name}<Row item={x} /></li>)`))
	want := "(x)=>h('li',{class:()=>x().cls,onClick:()=>pick(x().id)},()=>x().name,h(Row, {get item() {\n  return x();\n}}))"
	if plan.RenderJS != want {
		t.Errorf("render:\n got %s\nwant %s", plan.RenderJS, want)
	}
}

func TestAssignsIdent(t *testing.T) {
	cases := map[string]bool{
		"label = 'x';":       true,
		"label += 'x';":      true,
		"label++;":           true,
		"--label;":           true,
		"if (label == 'x')":  false,
		"label => label":     false,
		"obj.label = 1;":     false,
		"const labels = 1;":  false,
		"f(label === other)": false,
	}
	for js, want := range cases {
		if got := assignsIdent(js, "label"); got != want {
			t.Errorf("assignsIdent(%q) = %v, want %v", js, got, want)
		}
	}
}
