package irtree_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func lit(v string) ast.Expr { return &ast.Literal{Kind: ast.StringLit, Value: v} }

func TestMergeSpreadAttrsReactOrder(t *testing.T) {
	spread := &ast.JSXAttr{Spread: true, Value: &ast.Identifier{Name: "p"}}
	unknown := &ast.JSXAttr{Spread: true, Value: &ast.Identifier{Name: "q"}}
	attrs := []*ast.JSXAttr{
		{Name: "id", Value: lit("explicit-before")},
		spread,
		{Name: "title", Value: lit("explicit-after")},
		unknown,
	}
	out, children := irtree.MergeSpreadAttrs(attrs, func(a *ast.JSXAttr) irtree.SpreadExpansion {
		if a != spread {
			return irtree.SpreadExpansion{}
		}
		return irtree.SpreadExpansion{Resolved: true, ForwardsChildren: true, Attrs: []*ast.JSXAttr{
			{Name: "title", Value: lit("spread")},
			{Name: "id", Value: lit("spread")},
			{Name: "lang", Value: lit("spread")},
		}}
	})
	var got []string
	for _, a := range out {
		if a.Spread {
			got = append(got, "...")
			continue
		}
		got = append(got, a.Name+"="+a.Value.(*ast.Literal).Value)
	}
	want := "id=spread title=explicit-after lang=spread ..."
	if strings.Join(got, " ") != want || !children {
		t.Fatalf("got %q (children %v), want %q", strings.Join(got, " "), children, want)
	}
}

func TestMergeSpreadAttrsNoSpreadUnchanged(t *testing.T) {
	attrs := []*ast.JSXAttr{{Name: "a", Value: lit("1")}, {Name: "a", Value: lit("2")}}
	out, _ := irtree.MergeSpreadAttrs(attrs, nil)
	if len(out) != 2 || &out[0] != &attrs[0] {
		t.Fatalf("attributes without spreads must be returned as-is")
	}
}
