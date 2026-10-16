package build

import (
	"github.com/kratejs/krate/packages/compiler/ast"
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
// element with the object literal's properties.
func flattenElementSpreads(el *ast.JSXElement, consts map[string]*ast.ObjectExpr) {
	out := make([]*ast.JSXAttr, 0, len(el.Opening.Attributes))
	for _, attr := range el.Opening.Attributes {
		if !attr.Spread {
			out = append(out, attr)
			continue
		}
		obj := resolveSpreadObject(attr.Value, consts)
		if obj == nil {
			out = append(out, attr)
			continue
		}
		for _, prop := range obj.Properties {
			if prop.Spread || prop.Value == nil {
				continue
			}
			out = append(out, &ast.JSXAttr{
				Position: attr.Position,
				Name:     prop.Key,
				Value:    prop.Value,
			})
		}
	}
	el.Opening.Attributes = out
}

// resolveSpreadObject resolves a spread attribute value to an object literal
// when statically known.
func resolveSpreadObject(expr ast.Expr, consts map[string]*ast.ObjectExpr) *ast.ObjectExpr {
	switch v := expr.(type) {
	case *ast.ObjectExpr:
		return v
	case *ast.Identifier:
		return consts[v.Name]
	}
	return nil
}
