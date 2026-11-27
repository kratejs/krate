package annotator

import (
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// contextBindings returns the names bound to createContext() (and, with
// scopes, createScope()) in any bundled module, plus renamed imports of them.
func contextBindings(modules []ModuleSource, scopes bool) map[string]bool {
	contexts := map[string]bool{}
	for _, m := range modules {
		if m.Program == nil {
			continue
		}
		for _, stmt := range m.Program.Body {
			vs, ok := stmt.(*ast.VarStmt)
			if !ok {
				if exp, isExp := stmt.(*ast.ExportStmt); isExp {
					vs, ok = exp.Declaration.(*ast.VarStmt)
				}
			}
			if !ok || vs == nil {
				continue
			}
			for _, d := range vs.Decls {
				if call, ok := d.Init.(*ast.CallExpr); ok && d.Name != "" {
					if id, ok := call.Callee.(*ast.Identifier); ok && (id.Name == "createContext" || (scopes && id.Name == "createScope")) {
						contexts[d.Name] = true
					}
				}
			}
		}
	}
	// Renamed imports of a context are contexts too.
	for _, m := range modules {
		if m.Program == nil {
			continue
		}
		for _, stmt := range m.Program.Body {
			if imp, ok := stmt.(*ast.ImportStmt); ok {
				for _, n := range imp.Named {
					if n.Local != "" && contexts[n.Remote] {
						contexts[n.Local] = true
					}
				}
			}
		}
	}
	return contexts
}

// lowerContextLocals makes a component's context locals live getters.
//
// `const density = Ctx.useContext()` reads the nearest provider value, which
// can change. The hydration code runs once, so a plain local would freeze (or
// hold the provider's getter prop itself). The binding becomes a derived getter
// and every read of it a call, the same shape the React lowering produces:
//
//	const density = Ctx.useContext()   ->   const density = krateDerived(() => Ctx.useContext())
//	"cx-" + density                    ->   "cx-" + density()
//
// A destructuring pattern reads its members through one shared getter, so
// `const { count, setCount } = Ctx.useContext()` follows the provider value
// too. A name that an inner scope rebinds is left alone.
func lowerContextLocals(ann *irtree.Annotations, modules []ModuleSource) {
	// A scope handle is compile-time state and stays a plain local.
	contexts := contextBindings(modules, false)
	if len(contexts) == 0 {
		return
	}
	done := map[*ast.FnDecl]*ast.FnDecl{}
	for fnKey, fn := range ann.Functions {
		if fn == nil {
			continue
		}
		if cp, ok := done[fn]; ok {
			if cp != fn {
				ann.Functions[fnKey] = cp
			}
			continue
		}
		done[fn] = fn
		if cp := lowerContextLocalsIn(fn, contexts); cp != nil {
			done[fn] = cp
			ann.Functions[fnKey] = cp
		}
	}
}

// contextReadCtx reports `X.useContext()` / `X.use()` on a known context X.
func contextReadCtx(init ast.Expr, contexts map[string]bool) bool {
	call, ok := init.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return false
	}
	obj, ok := mem.Object.(*ast.Identifier)
	prop, ok2 := mem.Property.(*ast.Identifier)
	return ok && ok2 && contexts[obj.Name] && (prop.Name == "useContext" || prop.Name == "use")
}

func isComponentFnName(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return name != "" && unicode.IsUpper(r)
}

// patternBindingNames returns the names a destructuring declarator binds.
func patternBindingNames(d *ast.VarDecl) []string {
	var out []string
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch x := e.(type) {
		case *ast.Identifier:
			out = append(out, x.Name)
		case *ast.BinaryExpr:
			if x.Op == "=" {
				walk(x.Left)
			}
		case *ast.UnaryExpr:
			if x.Op == "..." {
				walk(x.Arg)
			}
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				if p == nil {
					continue
				}
				if p.Value != nil {
					walk(p.Value)
				} else if p.Key != "" {
					out = append(out, p.Key)
				}
			}
		case *ast.ArrayExpr:
			for _, el := range x.Elements {
				walk(el)
			}
		}
	}
	if d.Target == nil {
		return append(append([]string(nil), d.Names...), restNames(d)...)
	}
	walk(d.Target)
	return out
}

func restNames(d *ast.VarDecl) []string {
	if d.RestName != "" {
		return []string{d.RestName}
	}
	return nil
}

// lowerContextLocalsIn returns a copy of fn with its context locals lowered,
// or nil when there is nothing to lower.
func lowerContextLocalsIn(fn *ast.FnDecl, contexts map[string]bool) *ast.FnDecl {
	if !isComponentFnName(fn.Name) || fn.Async || fn.Generator {
		return nil
	}
	nested := nestedBindings(fn.Body)
	lowered := map[string]bool{}
	for _, stmt := range fn.Body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok || vs.Kind != ast.VarConst {
			continue
		}
		for _, d := range vs.Decls {
			if d == nil || !contextReadCtx(d.Init, contexts) {
				continue
			}
			names := patternBindingNames(d)
			if !d.IsDestructuring && d.Name != "" {
				names = []string{d.Name}
			}
			ok := len(names) > 0
			for _, n := range names {
				if nested[n] || lowered[n] {
					ok = false
				}
			}
			if !ok {
				continue
			}
			for _, n := range names {
				lowered[n] = true
			}
		}
	}
	if len(lowered) == 0 {
		return nil
	}
	sub := func(e ast.Expr, recurse func(ast.Expr) ast.Expr) ast.Expr {
		if id, ok := e.(*ast.Identifier); ok {
			if lowered[id.Name] {
				return &ast.CallExpr{Position: id.Position, Callee: id}
			}
			return e
		}
		return recurse(e)
	}
	derived := func(pos ast.Pos, body ast.Expr) *ast.CallExpr {
		return &ast.CallExpr{Position: pos, Callee: &ast.Identifier{Position: pos, Name: "krateDerived"}, Args: []ast.Expr{
			&ast.ArrowFn{Position: pos, Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Position: pos, Value: body}}},
		}}
	}
	newBody := make([]ast.Stmt, 0, len(fn.Body))
	tmp := 0
	for _, stmt := range fn.Body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			newBody = append(newBody, walkStmt(stmt, sub))
			continue
		}
		vc := *vs
		vc.Decls = nil
		for _, d := range vs.Decls {
			if d == nil {
				continue
			}
			if !contextReadCtx(d.Init, contexts) || vs.Kind != ast.VarConst {
				dc := *d
				dc.Init = walkExpr(d.Init, sub)
				vc.Decls = append(vc.Decls, &dc)
				continue
			}
			if !d.IsDestructuring {
				if !lowered[d.Name] {
					vc.Decls = append(vc.Decls, d)
					continue
				}
				dc := *d
				dc.Init = derived(vs.Position, d.Init)
				vc.Decls = append(vc.Decls, &dc)
				continue
			}
			names := patternBindingNames(d)
			if len(names) == 0 || !lowered[names[0]] {
				vc.Decls = append(vc.Decls, d)
				continue
			}
			tmp++
			shared := "__krdctx" + strconv.Itoa(tmp)
			vc.Decls = append(vc.Decls, &ast.VarDecl{Name: shared, Init: derived(vs.Position, d.Init)})
			for _, name := range names {
				inner := &ast.VarStmt{Position: vs.Position, Kind: ast.VarConst, Decls: []*ast.VarDecl{{
					Target:          d.Target,
					Names:           d.Names,
					IsDestructuring: true,
					Pattern:         d.Pattern,
					RestName:        d.RestName,
					Init:            &ast.CallExpr{Callee: &ast.Identifier{Name: shared}},
				}}}
				arrow := &ast.ArrowFn{Position: vs.Position, Body: []ast.Stmt{inner, &ast.ReturnStmt{Value: &ast.Identifier{Name: name}}}}
				vc.Decls = append(vc.Decls, &ast.VarDecl{Name: name, Init: &ast.CallExpr{Callee: &ast.Identifier{Name: "krateDerived"}, Args: []ast.Expr{arrow}}})
			}
		}
		newBody = append(newBody, &vc)
	}
	fc := *fn
	fc.Body = newBody
	return &fc
}
