package bundler

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// Runtime-reserved names.
//
// Hydration code is emitted into the scope of the user's own functions and
// calls runtime globals by name: `h(...)` builds list rows and JSX values,
// `kbind*` binds slots, `createSignal`/`krateDerived`/... are the lowering
// targets. A user binding with one of those names (`hits.map(h => <li>{h}</li>)`)
// would shadow the runtime inside generated code. renameReservedBindings gives
// every such user binding, and every reference resolving to it, a private
// name before any lowering runs, so generated references always reach the
// runtime.

// reservedRuntimeName reports whether generated hydration code may reference
// name as a runtime global.
func reservedRuntimeName(name string) bool {
	switch name {
	case "h", "findSlot", "refreshSlots", "createSignal", "createEffect", "createEffectDeps",
		"createMemo", "createMemoDeps", "createCallbackDeps", "createReducer", "createResource",
		"untrack", "onCleanup", "onMount", "useRef", "useCallback", "forwardRef", "createContext":
		return true
	}
	return strings.HasPrefix(name, "kbind") || strings.HasPrefix(name, "krate") || strings.HasPrefix(name, "__k")
}

// reservedSuffix is appended to a renamed user binding.
const reservedSuffix = "$u"

// renameReservedBindings renames user-declared bindings that collide with
// runtime-reserved names throughout the module. Names bound by an import are
// left alone (`import { h } from "@krate/runtime"` is the runtime itself), as
// are names bound only by a destructuring pattern kept as source text.
func renameReservedBindings(prog *ast.Program) {
	declared := map[string]bool{}
	skip := map[string]bool{}
	note := func(name string) {
		if name != "" && reservedRuntimeName(name) {
			declared[name] = true
		}
	}
	for _, stmt := range prog.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			for _, n := range imp.Named {
				local := n.Local
				if local == "" {
					local = n.Remote
				}
				skip[local] = true
			}
			skip[imp.Default] = true
			skip[imp.Namespace] = true
		}
	}
	noteParams := func(params []*ast.Param) {
		for _, p := range params {
			if p == nil {
				continue
			}
			if p.Target != nil {
				for _, n := range exprBindingNames(p.Target) {
					note(n)
				}
			} else if p.Pattern != "" {
				for _, n := range patternTextNames(p.Pattern) {
					skip[n] = true
				}
			} else {
				note(p.Name)
			}
		}
	}
	visitStmts(prog.Body, false, func(n any, _ bool) bool {
		switch x := n.(type) {
		case *ast.FnDecl:
			note(x.Name)
			noteParams(x.Params)
		case *ast.ArrowFn:
			note(x.Name)
			noteParams(x.Params)
		case *ast.ClassDecl:
			note(x.Name)
		case *ast.VarStmt:
			for _, d := range x.Decls {
				if d == nil {
					continue
				}
				if d.IsDestructuring && d.Target == nil {
					for _, name := range append(append([]string(nil), d.Names...), d.RestName) {
						skip[name] = true
					}
					continue
				}
				for _, name := range declBindingNames(d) {
					note(name)
				}
			}
		case *ast.ForInStmt:
			if x.Keyword != "" {
				for _, name := range exprBindingNames(x.Left) {
					note(name)
				}
			}
		case *ast.TryStmt:
			if x.Catch != nil {
				if x.Catch.ParamTarget != nil {
					for _, name := range exprBindingNames(x.Catch.ParamTarget) {
						note(name)
					}
				} else {
					note(x.Catch.Param)
				}
			}
		}
		return true
	})
	for name := range skip {
		delete(declared, name)
	}
	if len(declared) == 0 {
		return
	}
	rn := func(name string) string {
		if declared[name] {
			return name + reservedSuffix
		}
		return name
	}
	renameParams := func(params []*ast.Param) {
		for _, p := range params {
			if p != nil && p.Target == nil && p.Pattern == "" {
				p.Name = rn(p.Name)
			}
		}
	}
	visitStmts(prog.Body, false, func(n any, _ bool) bool {
		switch x := n.(type) {
		case *ast.Identifier:
			x.Name = rn(x.Name)
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				if p == nil || !p.Shorthand {
					continue
				}
				// `{ h }` keeps its key: `{ h: h$u }`.
				if id, ok := p.Value.(*ast.Identifier); ok && declared[id.Name] {
					p.Shorthand = false
				}
			}
		case *ast.FnDecl:
			x.Name = rn(x.Name)
			renameParams(x.Params)
		case *ast.ArrowFn:
			x.Name = rn(x.Name)
			renameParams(x.Params)
		case *ast.ClassDecl:
			x.Name = rn(x.Name)
		case *ast.VarStmt:
			for _, d := range x.Decls {
				if d == nil {
					continue
				}
				d.Name = rn(d.Name)
				for i, name := range d.Names {
					d.Names[i] = rn(name)
				}
				d.RestName = rn(d.RestName)
			}
		case *ast.TryStmt:
			if x.Catch != nil && x.Catch.ParamTarget == nil {
				x.Catch.Param = rn(x.Catch.Param)
			}
			if x.Catch != nil && x.Catch.ParamTarget != nil {
				visitExpr(x.Catch.ParamTarget, false, func(n any, _ bool) bool {
					if id, ok := n.(*ast.Identifier); ok {
						id.Name = rn(id.Name)
					}
					return true
				})
			}
		}
		return true
	})
	// Patterns kept as Target expressions were renamed by the walk; refresh
	// their source text so every consumer sees the same names.
	visitStmts(prog.Body, false, func(n any, _ bool) bool {
		switch x := n.(type) {
		case *ast.FnDecl:
			visitParamTargets(x.Params, rn)
		case *ast.ArrowFn:
			visitParamTargets(x.Params, rn)
		}
		return true
	})
}

// visitParamTargets renames identifiers inside destructuring parameters (the
// generic walk only visits parameter defaults).
func visitParamTargets(params []*ast.Param, rn func(string) string) {
	for _, p := range params {
		if p == nil || p.Target == nil {
			continue
		}
		visitExpr(p.Target, false, func(n any, _ bool) bool {
			if id, ok := n.(*ast.Identifier); ok {
				id.Name = rn(id.Name)
			}
			return true
		})
	}
}

// patternTextNames extracts identifier-like words from a pattern's source
// text (over-approximating is fine: the names are only skipped).
func patternTextNames(pattern string) []string {
	var out []string
	word := strings.Builder{}
	flush := func() {
		if word.Len() > 0 {
			out = append(out, word.String())
			word.Reset()
		}
	}
	for _, r := range pattern {
		if r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			word.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}
