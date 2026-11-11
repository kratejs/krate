package native

import (
	"strings"
	"unicode"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// site is a function-like declaration: a function declaration or a const bound
// to an arrow or function expression.
type site struct {
	name      string
	params    []*ast.Param
	body      []ast.Stmt
	pos       ast.Pos
	async     bool
	exprBody  bool
	top       bool
	exported  bool
	decorator string   // tier or "host" from a decorator, "" for none
	chain     []*frame // enclosing function scopes, outermost first
}

// frame holds the names declared directly in one function.
type frame struct {
	names map[string]bool
}

// collectSites lists the top-level function sites of a module and, below each,
// the nested ones.
func collectSites(prog *ast.Program) []*site {
	var out []*site
	add := func(s *site) {
		out = append(out, s)
		out = append(out, nestedSites(s, nil)...)
	}
	for _, stmt := range prog.Body {
		exported := false
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			if exp.Declaration == nil {
				continue
			}
			exported = true
			stmt = exp.Declaration
			if es, ok := stmt.(*ast.ExprStmt); ok && exp.Default {
				if fn, ok := es.Expression.(*ast.ArrowFn); ok {
					add(arrowSite("default", fn, exp.Position, "", true))
				}
				continue
			}
		}
		switch s := stmt.(type) {
		case *ast.FnDecl:
			add(&site{name: s.Name, params: s.Params, body: s.Body, pos: s.Position, async: s.Async, top: true, exported: exported || s.Export, decorator: s.Tier})
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d == nil || d.Name == "" {
					continue
				}
				if fn, ok := unparen(d.Init).(*ast.ArrowFn); ok {
					add(arrowSite(d.Name, fn, s.Position, d.Tier, exported))
				}
			}
		}
	}
	return out
}

func arrowSite(name string, fn *ast.ArrowFn, pos ast.Pos, decorator string, exported bool) *site {
	return &site{name: name, params: fn.Params, body: fn.Body, pos: pos, async: fn.Async, exprBody: fn.Expression, top: true, exported: exported, decorator: decorator}
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.Expr
	}
}

// nestedSites finds the functions declared inside parent, with their enclosing
// scopes recorded in chain.
func nestedSites(parent *site, chain []*frame) []*site {
	frm := &frame{names: declaredNames(parent.params, parent.body, false)}
	chain = append(chain[:len(chain):len(chain)], frm)
	var out []*site
	names := map[*ast.ArrowFn]string{}
	bundler.WalkStmts(parent.body, func(n any, _ bool) bool {
		var child *site
		switch x := n.(type) {
		case *ast.VarStmt:
			for _, d := range x.Decls {
				if d != nil {
					if fn, ok := unparen(d.Init).(*ast.ArrowFn); ok {
						names[fn] = d.Name
					}
				}
			}
		case *ast.FnDecl:
			child = &site{name: x.Name, params: x.Params, body: x.Body, pos: x.Position, async: x.Async}
		case *ast.ArrowFn:
			name := names[x]
			if name == "" {
				name = x.Name
			}
			child = &site{name: name, params: x.Params, body: x.Body, pos: x.Position, async: x.Async, exprBody: x.Expression}
		}
		if child == nil {
			return true
		}
		child.chain = chain
		out = append(out, child)
		out = append(out, nestedSites(child, chain)...)
		return false
	})
	return out
}

// declaredNames collects the names a function declares: its parameters and the
// bindings in its body. deep also descends into nested functions, which is the
// lenient reading used for a host function's own locals.
func declaredNames(params []*ast.Param, body []ast.Stmt, deep bool) map[string]bool {
	out := map[string]bool{}
	addParams := func(ps []*ast.Param) {
		for _, p := range ps {
			if p == nil {
				continue
			}
			if p.Target != nil {
				patternNames(p.Target, out)
			} else if p.Name != "" && !strings.HasPrefix(p.Name, "{") {
				out[p.Name] = true
			}
		}
	}
	addParams(params)
	bundler.WalkStmts(body, func(n any, inFn bool) bool {
		if inFn && !deep {
			return false
		}
		switch x := n.(type) {
		case *ast.VarStmt:
			for _, d := range x.Decls {
				if d == nil {
					continue
				}
				if d.Name != "" {
					out[d.Name] = true
				}
				for _, nm := range d.Names {
					out[nm] = true
				}
				if d.RestName != "" {
					out[d.RestName] = true
				}
				patternNames(d.Target, out)
			}
		case *ast.FnDecl:
			if x.Name != "" {
				out[x.Name] = true
			}
			if deep {
				addParams(x.Params)
			}
		case *ast.ArrowFn:
			if deep {
				addParams(x.Params)
			}
		case *ast.ClassDecl:
			if x.Name != "" {
				out[x.Name] = true
			}
		case *ast.TryStmt:
			if x.Catch != nil {
				if x.Catch.ParamTarget != nil {
					patternNames(x.Catch.ParamTarget, out)
				} else if x.Catch.Param != "" {
					out[x.Catch.Param] = true
				}
			}
		case *ast.ForInStmt:
			if x.Keyword != "" {
				patternNames(x.Left, out)
			}
		}
		return true
	})
	return out
}

// patternNames adds the names bound by a destructuring pattern.
func patternNames(e ast.Expr, out map[string]bool) {
	switch x := e.(type) {
	case nil:
	case *ast.Identifier:
		out[x.Name] = true
	case *ast.ObjectExpr:
		for _, p := range x.Properties {
			if p == nil {
				continue
			}
			if p.Value == nil {
				if p.Key != "" {
					out[p.Key] = true
				}
				continue
			}
			patternNames(p.Value, out)
		}
	case *ast.ArrayExpr:
		for _, el := range x.Elements {
			patternNames(el, out)
		}
	case *ast.BinaryExpr:
		if x.Op == "=" {
			patternNames(x.Left, out)
		}
	case *ast.UnaryExpr:
		if x.Op == "..." {
			patternNames(x.Arg, out)
		}
	}
}

// isComponent reports whether a function renders UI: it returns JSX, or it is
// named like a component and uses JSX anywhere.
func isComponent(s *site) bool {
	returns, anyJSX := false, false
	bundler.WalkStmts(s.body, func(n any, inFn bool) bool {
		switch x := n.(type) {
		case *ast.JSXElement, *ast.JSXFragment:
			anyJSX = true
		case *ast.ReturnStmt:
			if !inFn && exprIsJSX(x.Value) {
				returns = true
			}
		case *ast.ExprStmt:
			if !inFn && s.exprBody && exprIsJSX(x.Expression) {
				returns = true
			}
		}
		return true
	})
	if returns {
		return true
	}
	return anyJSX && isPascal(s.name)
}

func exprIsJSX(e ast.Expr) bool {
	switch x := unparen(e).(type) {
	case *ast.JSXElement, *ast.JSXFragment:
		return true
	case *ast.ConditionalExpr:
		return exprIsJSX(x.Consequent) || exprIsJSX(x.Alternate)
	case *ast.BinaryExpr:
		if x.Op == "&&" || x.Op == "||" || x.Op == "??" {
			return exprIsJSX(x.Left) || exprIsJSX(x.Right)
		}
	}
	return false
}

func isPascal(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

// bodyMarker reads a leading string directive of a body: "host" for "@host",
// a tier name for "@server" and friends, "" for none.
func bodyMarker(body []ast.Stmt) string {
	if len(body) > 0 {
		if es, ok := body[0].(*ast.ExprStmt); ok {
			if lit, ok := es.Expression.(*ast.Literal); ok && lit.Kind == ast.StringLit &&
				strings.Trim(strings.TrimSpace(lit.Value), `"'`) == "@host" {
				return "host"
			}
		}
	}
	return bundler.BodyDirectiveTier(body)
}

// hasUseServer reports a leading "use server" string in body.
func hasUseServer(body []ast.Stmt) bool {
	if len(body) == 0 {
		return false
	}
	es, ok := body[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	lit, ok := es.Expression.(*ast.Literal)
	return ok && lit.Kind == ast.StringLit && strings.Trim(strings.TrimSpace(lit.Value), `"'`) == "use server"
}
