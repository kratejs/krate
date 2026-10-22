package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

func mustExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	e := jsvalue.ParseExprSource(src)
	if e == nil {
		t.Fatalf("parse %q", src)
	}
	return e
}

func TestEvalConstValueTyped(t *testing.T) {
	props := valueMap{
		"count": jsvalue.Number(0),
		"q":     jsvalue.String("0"),
		"title": jsvalue.String("<b>"),
	}
	items, _ := jsvalue.ParseLiteralSource(`[{n:"a",on:true},{n:"b",on:false}]`)
	props["items"] = items
	signals := map[string]ast.Expr{
		"open": mustExpr(t, "false"),
		"n":    mustExpr(t, "count + 2"),
	}
	cases := []struct{ src, want string }{
		{`count && "x"`, `0`},
		{`q && "x"`, `'x'`},
		{`props.count`, `0`},
		{`props.missing`, `undefined`},
		{`open() ? "open" : "closed"`, `'closed'`},
		{`n() * 2`, `4`},
		{`"5" + "3"`, `'53'`},
		{`items.filter(i => i.on).map(i => i.n)`, `['a']`},
		{`cn("a", count && "b", q && "c", {d: true, e: false})`, `'a c d'`},
		{`typeof title`, `'string'`},
	}
	for _, c := range cases {
		got, ok := jsvalue.JSSource(evalConstValue(mustExpr(t, c.src), signals, props))
		if !ok || got != c.want {
			t.Errorf("%s = %s (ok=%v), want %s", c.src, got, ok, c.want)
		}
	}
	if v := evalConstValue(mustExpr(t, "unknownThing + 1"), signals, props); !v.IsUnknown() {
		t.Errorf("unresolved identifier must be Unknown, got %#v", v)
	}
	if !operandLeaks(mustExpr(t, "unknownThing"), signals, props) || operandLeaks(mustExpr(t, "props.count"), signals, props) {
		t.Error("operandLeaks must follow typed knownness")
	}
}

func TestLegacyConstString(t *testing.T) {
	cases := []struct {
		v    jsvalue.Value
		want string
	}{
		{jsvalue.String("0"), "0"},
		{jsvalue.Number(-0.5), "-0.5"},
		{jsvalue.Bool(false), "false"},
		{jsvalue.Null(), ""},
		{jsvalue.Undefined(), "undefined"},
		{jsvalue.NewArray(jsvalue.String("a")), "['a']"},
	}
	for _, c := range cases {
		if got := legacyConstString(nil, c.v); got != c.want {
			t.Errorf("legacyConstString(%#v) = %q, want %q", c.v, got, c.want)
		}
	}
	if got := legacyConstString(&ast.Identifier{Name: "x"}, jsvalue.Dynamic("x")); got != "x" {
		t.Errorf("unresolved identifier keeps its name, got %q", got)
	}
}

func TestTypedModuleConsts(t *testing.T) {
	inits := map[string]ast.Expr{
		"A":   mustExpr(t, "2"),
		"B":   mustExpr(t, "A * 3"),
		"S":   mustExpr(t, `"0"`),
		"FN":  mustExpr(t, "(x) => x"),
		"DYN": mustExpr(t, "fetchSomething()"),
	}
	got := typedModuleConsts(inits, nil)
	if v := got["B"]; v.Kind() != jsvalue.KindNumber || v.NumberValue() != 6 {
		t.Errorf("B = %#v", v)
	}
	if v := got["S"]; v.Kind() != jsvalue.KindString {
		t.Errorf("string const must stay a string, got %#v", v)
	}
	if _, ok := got["FN"]; ok {
		t.Error("functions are not folded module constants")
	}
	if _, ok := got["DYN"]; ok {
		t.Error("unknown values are not folded module constants")
	}
}

func TestOmitAttrValue(t *testing.T) {
	cases := []struct {
		name string
		v    jsvalue.Value
		omit bool
	}{
		{"title", jsvalue.Null(), true},
		{"id", jsvalue.Undefined(), true},
		{"lang", jsvalue.Bool(false), true},
		{"data-x", jsvalue.Bool(false), false},
		{"aria-hidden", jsvalue.Bool(false), false},
		{"draggable", jsvalue.Bool(false), false},
		{"src", jsvalue.String(""), true},
		{"alt", jsvalue.String(""), false},
		{"title", jsvalue.String("null"), false},
	}
	for _, c := range cases {
		if got := OmitAttrValue(c.name, c.v); got != c.omit {
			t.Errorf("OmitAttrValue(%s, %#v) = %v, want %v", c.name, c.v, got, c.omit)
		}
	}
}
