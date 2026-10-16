package build

import (
	"github.com/kratejs/krate/packages/compiler/ast"
)

// substituteParamBindings rewrites dynamic-route param reads to their concrete
// values before build-time inlining. This lets a `[slug].tsx` page resolve its
// collection entry statically:
//
//	const post = getCollection('blog').find((p) => p.slug === params.slug);
//
// With params {slug: "hello-collections"}, `params.slug` becomes the literal
// `"hello-collections"`, so the find() call folds to the matching entry.
//
// Recognized forms: `params.<name>`, `params?.<name>`, `props.params.<name>`,
// and `props.params?.<name>`. Params not present in the map are left untouched.
func substituteParamBindings(prog *ast.Program, params map[string]string) {
	if prog == nil || len(params) == 0 {
		return
	}
	for _, stmt := range prog.Body {
		subParamStmt(stmt, params)
	}
}

func subParamStmt(stmt ast.Stmt, params map[string]string) {
	switch s := stmt.(type) {
	case *ast.ExportStmt:
		if s.Declaration != nil {
			subParamStmt(s.Declaration, params)
		}
	case *ast.FnDecl:
		for _, inner := range s.Body {
			subParamStmt(inner, params)
		}
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if d.Init != nil {
				d.Init = subParamExpr(d.Init, params)
			}
		}
	case *ast.ReturnStmt:
		if s.Value != nil {
			s.Value = subParamExpr(s.Value, params)
		}
	case *ast.ExprStmt:
		if s.Expression != nil {
			s.Expression = subParamExpr(s.Expression, params)
		}
	case *ast.IfStmt:
		if s.Test != nil {
			s.Test = subParamExpr(s.Test, params)
		}
		for _, inner := range s.Consequent {
			subParamStmt(inner, params)
		}
		for _, inner := range s.Alternate {
			subParamStmt(inner, params)
		}
	case *ast.BlockStmt:
		for _, inner := range s.Body {
			subParamStmt(inner, params)
		}
	case *ast.ForStmt:
		if s.Test != nil {
			s.Test = subParamExpr(s.Test, params)
		}
		for _, inner := range s.Body {
			subParamStmt(inner, params)
		}
	}
}

func subParamExpr(expr ast.Expr, params map[string]string) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.MemberExpr:
		if lit := paramReadLiteral(e, params); lit != nil {
			return lit
		}
		e.Object = subParamExpr(e.Object, params)
		return e
	case *ast.CallExpr:
		e.Callee = subParamExpr(e.Callee, params)
		for i, arg := range e.Args {
			e.Args[i] = subParamExpr(arg, params)
		}
		return e
	case *ast.ArrowFn:
		for _, inner := range e.Body {
			subParamStmt(inner, params)
		}
		return e
	case *ast.BinaryExpr:
		e.Left = subParamExpr(e.Left, params)
		e.Right = subParamExpr(e.Right, params)
		return e
	case *ast.ConditionalExpr:
		e.Test = subParamExpr(e.Test, params)
		e.Consequent = subParamExpr(e.Consequent, params)
		e.Alternate = subParamExpr(e.Alternate, params)
		return e
	case *ast.UnaryExpr:
		e.Arg = subParamExpr(e.Arg, params)
		return e
	case *ast.TemplateExpr:
		for i, part := range e.Parts {
			e.Parts[i] = subParamExpr(part, params)
		}
		return e
	case *ast.ArrayExpr:
		for i, el := range e.Elements {
			e.Elements[i] = subParamExpr(el, params)
		}
		return e
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if prop.Value != nil {
				prop.Value = subParamExpr(prop.Value, params)
			}
		}
		return e
	case *ast.JSXElement:
		for _, attr := range e.Opening.Attributes {
			if attr.Spread || attr.Value == nil {
				continue
			}
			attr.Value = subParamExpr(attr.Value, params)
		}
		subParamJSXChildren(e.Children, params)
		return e
	case *ast.JSXFragment:
		subParamJSXChildren(e.Children, params)
		return e
	case *ast.TypeAssertion:
		e.Expr = subParamExpr(e.Expr, params)
		return e
	}
	return expr
}

func subParamJSXChildren(children []ast.JSXChild, params map[string]string) {
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			c.Expression = subParamExpr(c.Expression, params)
		case *ast.JSXElementChild:
			subParamExpr(c.Element, params)
		case *ast.JSXFragmentChild:
			subParamJSXChildren(c.Fragment.Children, params)
		}
	}
}

// paramReadLiteral returns a string literal when expr is a param read with a
// known concrete value, else nil. Handles `params.x`, `params?.x`,
// `props.params.x`, and `props.params?.x`.
func paramReadLiteral(expr *ast.MemberExpr, params map[string]string) ast.Expr {
	prop, ok := expr.Property.(*ast.Identifier)
	if !ok {
		return nil
	}
	val, ok := params[prop.Name]
	if !ok {
		return nil
	}
	if isParamsAccess(expr.Object) {
		return &ast.Literal{Kind: ast.StringLit, Value: val}
	}
	return nil
}

// isParamsAccess reports whether expr is the `params` binding or `props.params`.
func isParamsAccess(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Identifier:
		return e.Name == "params"
	case *ast.MemberExpr:
		if prop, ok := e.Property.(*ast.Identifier); ok && prop.Name == "params" {
			if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
				return true
			}
		}
	}
	return false
}
