package scene3d

import (
	"math"
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// AST construction helpers for the generated code. Every string in the output
// is an ast.Literal, which the printers quote, so user text is never spliced
// into JavaScript source.

func ident(name string) *ast.Identifier { return &ast.Identifier{Name: name} }

func strLit(s string) ast.Expr { return &ast.Literal{Kind: ast.StringLit, Value: s} }

func boolLit(b bool) ast.Expr {
	if b {
		return &ast.Literal{Kind: ast.BoolLit, Value: "true"}
	}
	return &ast.Literal{Kind: ast.BoolLit, Value: "false"}
}

func numberText(text string, negative bool) ast.Expr {
	lit := &ast.Literal{Kind: ast.NumberLit, Value: text}
	if negative {
		return &ast.UnaryExpr{Op: "-", Arg: lit}
	}
	return lit
}

// numLit emits a float64 as a JavaScript number literal.
func numLit(n float64) ast.Expr {
	if n == 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return numberText("0", false)
	}
	return numberText(formatNumber(math.Abs(n)), n < 0)
}

// sigLit emits n rounded to six significant digits (colors, light vectors).
func sigLit(n float64) ast.Expr {
	r, _ := strconv.ParseFloat(strconv.FormatFloat(n, 'g', 6, 64), 64)
	return numLit(r)
}

// f32Lit emits n as the shortest decimal that round-trips a float32, with
// values too small to matter snapped to zero.
func f32Lit(n float64) ast.Expr {
	if math.Abs(n) < 1e-9 {
		return numberText("0", false)
	}
	f := float32(n)
	return numberText(strconv.FormatFloat(math.Abs(float64(f)), 'g', -1, 32), f < 0)
}

func arrOf(elems ...ast.Expr) *ast.ArrayExpr { return &ast.ArrayExpr{Elements: elems} }

func numArr(vs ...float64) ast.Expr {
	out := make([]ast.Expr, len(vs))
	for i, v := range vs {
		out[i] = numLit(v)
	}
	return arrOf(out...)
}

func sigArr(vs ...float64) ast.Expr {
	out := make([]ast.Expr, len(vs))
	for i, v := range vs {
		out[i] = sigLit(v)
	}
	return arrOf(out...)
}

func matArr(m mat4) ast.Expr {
	out := make([]ast.Expr, 16)
	for i, v := range m {
		out[i] = f32Lit(v)
	}
	return arrOf(out...)
}

func member(obj ast.Expr, prop string) ast.Expr {
	return &ast.MemberExpr{Object: obj, Property: ident(prop)}
}

func call(callee ast.Expr, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Callee: callee, Args: args}
}

func method(obj, name string, args ...ast.Expr) *ast.CallExpr {
	return call(member(ident(obj), name), args...)
}

func kv(key string, v ast.Expr) *ast.ObjectProp { return &ast.ObjectProp{Key: key, Value: v} }

func obj(props ...*ast.ObjectProp) *ast.ObjectExpr { return &ast.ObjectExpr{Properties: props} }

func exprStmt(e ast.Expr) ast.Stmt { return &ast.ExprStmt{Expression: e} }

func constStmt(name string, init ast.Expr) ast.Stmt {
	return &ast.VarStmt{Kind: ast.VarConst, Decls: []*ast.VarDecl{{Name: name, Init: init}}}
}

func arrowBlock(params []string, body []ast.Stmt) *ast.ArrowFn {
	ps := make([]*ast.Param, len(params))
	for i, p := range params {
		ps[i] = &ast.Param{Name: p}
	}
	return &ast.ArrowFn{Params: ps, Body: body}
}

// effect wraps statements in `__k3d.effect(() => { ... })`.
func effect(body ...ast.Stmt) ast.Stmt {
	return exprStmt(method(k3dName, "effect", arrowBlock(nil, body)))
}
