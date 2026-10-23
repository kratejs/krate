package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

func TestEvalPureWithJSBuiltins(t *testing.T) {
	b := &builder{evalJS: jsruntime.EvaluateExpr}
	b.localProps = valueMap{}
	b.localSignals = map[string]ast.Expr{}
	for _, src := range []string{
		`new Array(3).fill(7)`,
		`Number.isInteger(4)`,
		`Math.max(2, 7)`,
	} {
		expr := parseTestExpr(t, src)
		if !b.pureJSExpr(expr) {
			t.Errorf("%s: pureJSExpr = false", src)
			continue
		}
		v, ok := b.evalPureWithJS(expr)
		if !ok {
			t.Errorf("%s: fallback failed", src)
			continue
		}
		t.Logf("%s = %s", src, valueJSLiteral(v))
	}
}

func TestPureJSRejectsImpureExpressions(t *testing.T) {
	b := &builder{evalJS: jsruntime.EvaluateExpr}
	b.localProps = valueMap{}
	b.localSignals = map[string]ast.Expr{}
	for _, src := range []string{
		`Date.now()`,
		`Math.random()`,
		`fetch("https://example.com")`,
		`process.env.HOME`,
		`new Date()`,
		`"x".toLocaleUpperCase()`,
		`Math["random"]()`,
		`globalThis["Math"].random()`,
		`props["constructor"]("return 1")`,
		`Object["constructor"]["constructor"]("return 1")()`,
		`Math["ra" + "ndom"]()`,
	} {
		if b.pureJSExpr(parseTestExpr(t, src)) {
			t.Errorf("%s must not be treated as pure", src)
		}
	}
}
