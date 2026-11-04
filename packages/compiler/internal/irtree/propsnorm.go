package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
)

// Destructured component props.
//
// A component declared `function Label({ value, tone = "x" })` reads its props
// once if the destructured locals are bound at hydration. React re-renders the
// body on every prop change, so the locals must stay live. normalizePropsFn
// rewrites such a function to the whole-props form (`function Label(props)`)
// with every use of a destructured local replaced by a `props.<key>` read
// (defaults become `props.k === undefined ? def : props.k`). The whole-props
// form already goes through the live-getter registry, so a changing call-site
// prop updates the child.

// normalizedPropsParam is the synthetic whole-props parameter name.
const normalizedPropsParam = "props"

// propsLocal is one binding of a destructured props parameter and the
// expression that reads it from the props object.
type propsLocal struct {
	name string
	read ast.Expr
}

// normalizeComponentFns returns fns with every destructured-props component
// rewritten to the whole-props form. Functions that cannot be rewritten
// safely keep their destructured parameter (and the snapshot warning).
func normalizeComponentFns(fns map[string]*ast.FnDecl) map[string]*ast.FnDecl {
	if len(fns) == 0 {
		return fns
	}
	out := make(map[string]*ast.FnDecl, len(fns))
	for name, fn := range fns {
		if nf := normalizePropsFn(fn); nf != nil {
			out[name] = nf
		} else {
			out[name] = fn
		}
	}
	return out
}

// normalizePropsFn returns the whole-props form of fn, or nil when fn does
// not take a destructured props parameter or cannot be rewritten.
func normalizePropsFn(fn *ast.FnDecl) *ast.FnDecl {
	if fn == nil || !isComponentName(fn.Name) || len(fn.Params) != 1 || fn.Generator {
		return nil
	}
	p := fn.Params[0]
	if p == nil || p.IsRest || p.Default != nil || p.Target == nil {
		return nil
	}
	obj, ok := p.Target.(*ast.ObjectExpr)
	if !ok {
		return nil
	}
	locals, ok := propsLocals(obj, &ast.Identifier{Name: normalizedPropsParam})
	if !ok || len(locals) == 0 {
		return nil
	}
	body := fn.Body
	for _, l := range locals {
		if bindingScopeDeclares(blockBindingNames(body), l.name) || l.name == normalizedPropsParam {
			return nil
		}
	}
	if assignsAny(body, bindingsOf(locals)) {
		return nil
	}
	for _, l := range locals {
		s := &listParamSubstituter{param: l.name, elem: l.read}
		body = s.subStmts(body)
	}
	if bodyMentionsAny(body, locals) {
		return nil
	}
	cp := *fn
	cp.Params = []*ast.Param{{Position: p.Position, Name: normalizedPropsParam}}
	cp.Body = body
	return &cp
}

func bindingsOf(locals []propsLocal) []propsBinding {
	out := make([]propsBinding, len(locals))
	for i, l := range locals {
		out[i] = propsBinding{local: l.name}
	}
	return out
}

// bodyMentionsAny reports whether the rewritten body still references a
// destructured local (an unmodeled statement kind, a JSX tag name).
func bodyMentionsAny(body []ast.Stmt, locals []propsLocal) bool {
	js := ""
	for _, s := range body {
		js += renderStmtJS(s, nil) + "\n"
	}
	for _, l := range locals {
		found := false
		scanJSIdents(js, func(name string) {
			if name == l.name {
				found = true
			}
		})
		if found {
			return true
		}
	}
	return false
}

// propsLocals flattens an object binding pattern read from base into its
// bindings. Rest elements, computed keys and array patterns are rejected.
func propsLocals(obj *ast.ObjectExpr, base ast.Expr) ([]propsLocal, bool) {
	var out []propsLocal
	for _, prop := range obj.Properties {
		if prop == nil || prop.Spread || prop.Computed || prop.Method || prop.Key == "" || prop.KeyNumeric {
			return nil, false
		}
		read := ast.Expr(&ast.MemberExpr{Object: cloneExpr(base), Property: &ast.Identifier{Name: prop.Key}})
		if prop.KeyQuoted || !isJSIdentifier(prop.Key) {
			read = &ast.MemberExpr{Object: cloneExpr(base), Property: &ast.Literal{Kind: ast.StringLit, Value: prop.Key}, Computed: true}
		}
		target := prop.Value
		var def ast.Expr
		if be, ok := target.(*ast.BinaryExpr); ok && be.Op == "=" {
			target, def = be.Left, be.Right
		}
		if def != nil {
			read = &ast.ParenExpr{Expr: &ast.ConditionalExpr{
				Test:       &ast.BinaryExpr{Left: read, Op: "===", Right: undefinedLit()},
				Consequent: def,
				Alternate:  cloneExpr(read),
			}}
		}
		switch t := target.(type) {
		case *ast.Identifier:
			out = append(out, propsLocal{name: t.Name, read: read})
		case *ast.ObjectExpr:
			inner, ok := propsLocals(t, read)
			if !ok {
				return nil, false
			}
			out = append(out, inner...)
		default:
			return nil, false
		}
	}
	return out, true
}
