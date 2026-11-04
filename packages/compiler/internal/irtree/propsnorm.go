package irtree

import (
	"strings"

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
	// key is the top-level props key the binding reads.
	key  string
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
//
// Two source shapes are lowered: a destructured first parameter
// (`function C({ a, b = 1, ...rest })`) and a top-level destructuring of a
// plain props parameter (`const { a, ...rest } = props`). A rest element that
// is only spread onto elements (`{...rest}`) or read by member (`rest.x`)
// becomes a trailing rest parameter whose Pattern lists the excluded keys,
// which the call-site rest machinery (restProps) understands.
func normalizePropsFn(fn *ast.FnDecl) *ast.FnDecl {
	if fn == nil || !isComponentName(fn.Name) || len(fn.Params) != 1 || fn.Generator || fn.Params[0] == nil {
		return nil
	}
	p := fn.Params[0]
	if p.IsRest || p.Default != nil {
		return nil
	}
	propsName := normalizedPropsParam
	var pattern *ast.ObjectExpr
	body := fn.Body
	switch {
	case p.Target != nil:
		obj, ok := p.Target.(*ast.ObjectExpr)
		if !ok {
			return nil
		}
		pattern = obj
	case p.Name != "" && p.Name != "{...}" && p.Name != "[...]" && p.Pattern == "":
		propsName = p.Name
		obj, idx := propsDestructureStmt(body, propsName)
		if obj == nil {
			return nil
		}
		pattern = obj
		body = append(append([]ast.Stmt{}, body[:idx]...), body[idx+1:]...)
	default:
		return nil
	}
	locals, restName, ok := propsLocals(pattern, &ast.Identifier{Name: propsName})
	if !ok || (len(locals) == 0 && restName == "") {
		return nil
	}
	for _, l := range locals {
		if bindingScopeDeclares(blockBindingNames(body), l.name) || l.name == propsName || l.name == restName {
			return nil
		}
	}
	if restName != "" && (bindingScopeDeclares(blockBindingNames(body), restName) || !restUsesAllowed(body, restName)) {
		return nil
	}
	if assignsAny(body, bindingsOf(locals)) {
		return nil
	}
	if restName != "" && restName == propsName {
		// `{ a, ...props }`: the rest keeps working under another name.
		s := &listParamSubstituter{param: propsName, elem: &ast.Identifier{Name: "__rest"}}
		body = s.subStmts(body)
		restName = "__rest"
	}
	for _, l := range locals {
		s := &listParamSubstituter{param: l.name, elem: l.read}
		body = s.subStmts(body)
	}
	if restName != "" {
		s := &listParamSubstituter{param: restName, elem: &ast.Identifier{Name: propsName}, keepSpread: true}
		body = s.subStmts(body)
	}
	if bodyMentionsAny(body, locals) {
		return nil
	}
	cp := *fn
	cp.Params = []*ast.Param{{Position: p.Position, Name: propsName}}
	if restName != "" {
		keys := make([]string, 0, len(locals))
		for _, l := range locals {
			keys = append(keys, l.key)
		}
		cp.Params = append(cp.Params, &ast.Param{Position: p.Position, Name: restName, IsRest: true, Pattern: "{" + strings.Join(keys, ",") + "}"})
	}
	cp.Body = body
	return &cp
}

// propsDestructureStmt finds a top-level `const { ... } = <param>` statement
// and returns its pattern and index.
func propsDestructureStmt(body []ast.Stmt, param string) (*ast.ObjectExpr, int) {
	for i, st := range body {
		vs, ok := st.(*ast.VarStmt)
		if !ok || len(vs.Decls) != 1 {
			continue
		}
		d := vs.Decls[0]
		if d == nil || !d.IsDestructuring || d.Target == nil {
			continue
		}
		init, ok := d.Init.(*ast.Identifier)
		if !ok || init.Name != param {
			continue
		}
		if obj, ok := d.Target.(*ast.ObjectExpr); ok {
			return obj, i
		}
	}
	return nil, -1
}

// restUsesAllowed reports whether every use of the rest binding is a JSX
// spread (`{...rest}`) or a member read (`rest.x`), the forms the call-site
// rest machinery resolves.
func restUsesAllowed(body []ast.Stmt, rest string) bool {
	allowed := map[*ast.Identifier]bool{}
	total := 0
	WalkAST(body, func(n any) bool {
		switch x := n.(type) {
		case *ast.JSXAttr:
			if x.Spread {
				if id, ok := x.Value.(*ast.Identifier); ok && id.Name == rest {
					allowed[id] = true
				}
			}
		case *ast.MemberExpr:
			if id, ok := x.Object.(*ast.Identifier); ok && id.Name == rest && !x.Computed {
				allowed[id] = true
			}
		case *ast.Identifier:
			if x.Name == rest {
				total++
				if !allowed[x] {
					total += 1000
				}
			}
		}
		return true
	})
	return total < 1000
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
// bindings, plus the name of a trailing rest element. Computed keys and array
// patterns are rejected.
func propsLocals(obj *ast.ObjectExpr, base ast.Expr) ([]propsLocal, string, bool) {
	var out []propsLocal
	rest := ""
	for i, prop := range obj.Properties {
		if prop == nil {
			return nil, "", false
		}
		if prop.Spread {
			id, ok := prop.Value.(*ast.Identifier)
			if !ok || i != len(obj.Properties)-1 || base == nil {
				return nil, "", false
			}
			rest = id.Name
			continue
		}
		if prop.Computed || prop.Method || prop.Key == "" || prop.KeyNumeric {
			return nil, "", false
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
			out = append(out, propsLocal{name: t.Name, key: prop.Key, read: read})
		case *ast.ObjectExpr:
			inner, innerRest, ok := propsLocals(t, read)
			if !ok || innerRest != "" {
				return nil, "", false
			}
			// A nested pattern consumes the whole top-level key.
			for j := range inner {
				inner[j].key = prop.Key
			}
			out = append(out, inner...)
		default:
			return nil, "", false
		}
	}
	return out, rest, true
}
