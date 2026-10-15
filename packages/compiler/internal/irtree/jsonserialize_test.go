package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func TestSerializeJSON(t *testing.T) {
	str := &ast.Literal{Kind: ast.StringLit, Value: "hi"}
	num := &ast.Literal{Kind: ast.NumberLit, Value: "3"}
	tru := &ast.Literal{Kind: ast.BoolLit, Value: "true"}
	neg := &ast.UnaryExpr{Op: "-", Arg: num}
	arr := &ast.ArrayExpr{Elements: []ast.Expr{str, num, tru}}
	obj := &ast.ObjectExpr{Properties: []*ast.ObjectProp{{Key: "a", Value: num}, {Key: "b", Value: str}}}

	cases := []struct {
		e    ast.Expr
		want string
	}{
		{str, `"hi"`},
		{num, "3"},
		{tru, "true"},
		{neg, "-3"},
		{arr, `["hi",3,true]`},
		{obj, `{"a":3,"b":"hi"}`},
		{&ast.Literal{Kind: ast.NullLit}, "null"},
	}
	for _, c := range cases {
		if got := SerializeJSON(c.e, nil); got != c.want {
			t.Errorf("SerializeJSON = %q, want %q", got, c.want)
		}
	}

	// Non-literal nodes go through resolve; valid JSON is embedded as-is,
	// otherwise it becomes a JSON string.
	id := &ast.Identifier{Name: "x"}
	if got := SerializeJSON(id, func(ast.Expr) (string, bool) { return "42", true }); got != "42" {
		t.Errorf("resolved number = %q, want 42", got)
	}
	if got := SerializeJSON(id, func(ast.Expr) (string, bool) { return "text", true }); got != `"text"` {
		t.Errorf("resolved text = %q, want %q", got, `"text"`)
	}
	if got := SerializeJSON(id, func(ast.Expr) (string, bool) { return "", false }); got != "null" {
		t.Errorf("unresolved = %q, want null", got)
	}
}
