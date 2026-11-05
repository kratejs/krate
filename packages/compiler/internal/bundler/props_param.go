package bundler

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// The build-time renderer treats a component's single parameter as the props
// object only when it is called `props` (call-site attributes become bindings
// under that name). Any other name, `function Box(p)`, would otherwise see
// every prop as undefined as soon as the page also reads a module constant.
// normalizePropsParam gives such a parameter the conventional name, so the one
// convention holds however the author spells it.

// normalizePropsParam renames the lone plain parameter of each top-level
// component (a capitalised function or arrow const) to `props`. A component
// that already uses the name `props` for something else is left alone.
func normalizePropsParam(prog *ast.Program) {
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Declaration != nil {
			stmt = exp.Declaration
		}
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if isComponentName(s.Name) && (len(s.Params) != 1 || s.Params[0] == nil || s.Params[0].Name != s.Name) {
				renamePropsParam(s, s.Params)
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d == nil || !isComponentName(d.Name) {
					continue
				}
				if arrow, ok := d.Init.(*ast.ArrowFn); ok {
					renamePropsParam(arrow, arrow.Params)
				}
			}
		}
	}
}

func isComponentName(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// renamePropsParam renames params[0] to `props` throughout fn when that is the
// only parameter and a plain identifier.
func renamePropsParam(fn any, params []*ast.Param) {
	if len(params) != 1 {
		return
	}
	p := params[0]
	if p == nil || p.Name == "" || p.Name == "props" || p.Pattern != "" || p.Target != nil || p.IsRest {
		return
	}
	// `Layout(children)` receives the children directly, and keeps doing so.
	if p.Name == "children" || strings.ContainsAny(p.Name, "{[.") {
		return
	}
	if usesName(fn, "props") {
		return
	}
	var wrapped ast.Stmt
	switch f := fn.(type) {
	case *ast.FnDecl:
		wrapped = f
	case *ast.ArrowFn:
		// Wrap the arrow in a statement so the program-level renamer reaches it.
		wrapped = &ast.ExprStmt{Expression: f}
	}
	applyRenames(&ast.Program{Body: []ast.Stmt{wrapped}}, map[string]string{p.Name: "props"})
}

// usesName reports whether the function declares or references an identifier
// called name anywhere inside it.
func usesName(fn any, name string) bool {
	found := false
	check := func(n any, _ bool) bool {
		switch x := n.(type) {
		case *ast.Identifier:
			if x.Name == name {
				found = true
			}
		case *ast.FnDecl:
			if x.Name == name {
				found = true
			}
			for _, p := range x.Params {
				if paramMentions(p, name) {
					found = true
				}
			}
		case *ast.ArrowFn:
			for _, p := range x.Params {
				if paramMentions(p, name) {
					found = true
				}
			}
		case *ast.VarStmt:
			for _, d := range x.Decls {
				if d != nil && declMentions(d, name) {
					found = true
				}
			}
		}
		return !found
	}
	switch f := fn.(type) {
	case *ast.FnDecl:
		for _, p := range f.Params {
			if paramMentions(p, name) {
				return true
			}
		}
		visitStmts(f.Body, false, check)
	case *ast.ArrowFn:
		for _, p := range f.Params {
			if paramMentions(p, name) {
				return true
			}
		}
		visitStmts(f.Body, false, check)
	}
	return found
}

func paramMentions(p *ast.Param, name string) bool {
	if p == nil {
		return false
	}
	return p.Name == name || strings.Contains(p.Pattern, name)
}

func declMentions(d *ast.VarDecl, name string) bool {
	if d.Name == name || d.RestName == name {
		return true
	}
	for _, n := range d.Names {
		if n == name {
			return true
		}
	}
	return false
}
