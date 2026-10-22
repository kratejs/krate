package build

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// FlattenComponentSpreadAttrs rewrites JSX spread attributes that resolve to a
// statically-known object literal into plain attributes before the annotator /
// irtree stages consume call-site props.
// The docs plugin generates pages that call <DocsLayout {...docsProps}> where
// `const docsProps = {...}` is declared in the same page function. The irtree
// call-site prop extraction (extractPropsAST / buildPropBindings) skips spread
// attributes, so those props silently vanished and the theme layout rendered
// empty. This pass inlines the const object's properties onto the element so
// the rest of the pipeline sees exactly what `extractPropsAST` expects.
// Only spreads resolvable to an object literal - inline <Comp {...{a: 1}}> or
// a same-function `const o = {...}` - are flattened. Identifiers bound to
// anything else, member accesses, calls, etc. are left untouched so they keep
// the pre-existing (skip) behavior.
func (b *Builder) FlattenComponentSpreadAttrs(prog *ast.Program) {
	for _, stmt := range prog.Body {
		var fn *ast.FnDecl
		switch s := stmt.(type) {
		case *ast.FnDecl:
			fn = s
		case *ast.ExportStmt:
			if d, ok := s.Declaration.(*ast.FnDecl); ok {
				fn = d
			}
		}
		if fn == nil {
			continue
		}
		consts := collectConstObjects(fn.Body)
		// An object changed by the body (`o.x = 1`, `Object.assign(o, ...)`)
		// no longer matches its literal.
		for name := range irtree.RenderMutatedNames(fn.Body) {
			delete(consts, name)
		}
		for _, s := range fn.Body {
			flattenStmtSpreads(s, consts)
		}
	}
}

// collectConstObjects maps same-function const declarations whose initializer
// is an object literal to that literal, e.g. `const docsProps = {...}`.
func collectConstObjects(body []ast.Stmt) map[string]*ast.ObjectExpr {
	consts := make(map[string]*ast.ObjectExpr)
	for _, stmt := range body {
		var vs *ast.VarStmt
		if v, ok := stmt.(*ast.VarStmt); ok {
			vs = v
		} else if e, ok := stmt.(*ast.ExportStmt); ok {
			vs, _ = e.Declaration.(*ast.VarStmt)
		}
		if vs == nil {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name == "" || decl.Init == nil {
				continue
			}
			if obj, ok := decl.Init.(*ast.ObjectExpr); ok {
				consts[decl.Name] = obj
			}
		}
	}
	return consts
}

// flattenStmtSpreads recurses through statement constructs that can contain
// JSX and flattens resolvable spread attributes.
func flattenStmtSpreads(s ast.Stmt, consts map[string]*ast.ObjectExpr) {
	switch t := s.(type) {
	case *ast.VarStmt:
		for _, decl := range t.Decls {
			if decl.Init != nil {
				flattenExprSpreads(decl.Init, consts)
			}
		}
	case *ast.ExprStmt:
		flattenExprSpreads(t.Expression, consts)
	case *ast.ReturnStmt:
		flattenExprSpreads(t.Value, consts)
	case *ast.IfStmt:
		flattenExprSpreads(t.Test, consts)
		for _, c := range t.Consequent {
			flattenStmtSpreads(c, consts)
		}
		for _, c := range t.Alternate {
			flattenStmtSpreads(c, consts)
		}
	case *ast.BlockStmt:
		for _, c := range t.Body {
			flattenStmtSpreads(c, consts)
		}
	case *ast.ForStmt:
		if init, ok := t.Init.(*ast.VarStmt); ok {
			flattenStmtSpreads(init, consts)
		}
		flattenExprSpreads(t.Test, consts)
		flattenExprSpreads(t.Update, consts)
		for _, c := range t.Body {
			flattenStmtSpreads(c, consts)
		}
	case *ast.ExportStmt:
		if d, ok := t.Declaration.(*ast.FnDecl); ok {
			for _, c := range d.Body {
				flattenStmtSpreads(c, consts)
			}
		} else {
			flattenStmtSpreads(t.Declaration, consts)
		}
	}
}

// flattenExprSpreads recurses through expression constructs that can contain
// JSX elements and flattens resolvable spread attributes on them.
func flattenExprSpreads(e ast.Expr, consts map[string]*ast.ObjectExpr) {
	if e == nil {
		return
	}
	switch t := e.(type) {
	case *ast.JSXElement:
		flattenElementSpreads(t, consts)
		flattenChildrenSpreads(t.Children, consts)
	case *ast.JSXFragment:
		flattenChildrenSpreads(t.Children, consts)
	case *ast.BinaryExpr:
		flattenExprSpreads(t.Left, consts)
		flattenExprSpreads(t.Right, consts)
	case *ast.ConditionalExpr:
		flattenExprSpreads(t.Test, consts)
		flattenExprSpreads(t.Consequent, consts)
		flattenExprSpreads(t.Alternate, consts)
	case *ast.CallExpr:
		for _, arg := range t.Args {
			flattenExprSpreads(arg, consts)
		}
	case *ast.MemberExpr:
		flattenExprSpreads(t.Object, consts)
	case *ast.ArrayExpr:
		for _, el := range t.Elements {
			flattenExprSpreads(el, consts)
		}
	case *ast.TypeAssertion:
		flattenExprSpreads(t.Expr, consts)
	}
}

// flattenChildrenSpreads recurses JSX children so spread attributes inside
// nested elements are resolved too.
func flattenChildrenSpreads(children []ast.JSXChild, consts map[string]*ast.ObjectExpr) {
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			flattenExprSpreads(c.Element, consts)
		case *ast.JSXFragmentChild:
			flattenExprSpreads(c.Fragment, consts)
		case *ast.JSXExprContainer:
			flattenExprSpreads(c.Expression, consts)
		}
	}
}

// flattenElementSpreads replaces resolvable spread attributes on a single
// element with the object literal's properties, with React's ordering: a
// later attribute or spread overrides an earlier one of the same name.
func flattenElementSpreads(el *ast.JSXElement, consts map[string]*ast.ObjectExpr) {
	el.Opening.Attributes, _ = irtree.MergeSpreadAttrs(el.Opening.Attributes, func(attr *ast.JSXAttr) irtree.SpreadExpansion {
		obj := resolveSpreadObject(attr.Value, consts)
		if obj == nil {
			return irtree.SpreadExpansion{}
		}
		exp := irtree.SpreadExpansion{Resolved: true}
		for _, prop := range obj.Properties {
			exp.Attrs = append(exp.Attrs, &ast.JSXAttr{
				Position: attr.Position,
				Name:     prop.Key,
				Value:    prop.Value,
			})
		}
		return exp
	})
}

// resolveSpreadObject resolves a spread attribute value to an object literal
// when statically known. Literals with spread, computed, method or accessor
// members are not flattened: their keys or values are not plain attributes.
func resolveSpreadObject(expr ast.Expr, consts map[string]*ast.ObjectExpr) *ast.ObjectExpr {
	var obj *ast.ObjectExpr
	switch v := expr.(type) {
	case *ast.ObjectExpr:
		obj = v
	case *ast.Identifier:
		obj = consts[v.Name]
	}
	if obj == nil {
		return nil
	}
	for _, prop := range obj.Properties {
		if prop == nil || prop.Spread || prop.Computed || prop.Method || prop.Getter || prop.Setter || prop.Value == nil {
			return nil
		}
	}
	return obj
}
