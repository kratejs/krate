package bundler

import (
	"crypto/sha256"
	"encoding/hex"
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

// isolateDecoratorTemps gives the temporaries esbuild generates for decorators
// (`_init`, `_m_dec`, `_a`) names private to this file. They sit at module level
// under the same names in every file, and client code shares one scope, so a
// class would otherwise read another module's decorator state.
func isolateDecoratorTemps(prog *ast.Program, src string) {
	temps := map[string]bool{}
	lowered := false
	for _, stmt := range prog.Body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			switch {
			case d == nil:
			case d.Name == "__decoratorStart" || d.Name == "__decorateClass":
				lowered = true
			case d.Init == nil && strings.HasPrefix(d.Name, "_") && !strings.HasPrefix(d.Name, "__"):
				temps[d.Name] = true
			}
		}
	}
	if !lowered || len(temps) == 0 {
		return
	}
	sum := sha256.Sum256([]byte(src))
	renameBindings(prog, func(name string) bool { return temps[name] }, "$d"+hex.EncodeToString(sum[:3]))
}

// renameReservedBindings renames user-declared bindings that collide with
// runtime-reserved names throughout the module. Names bound by an import from
// the runtime or a package are left alone (`import { h } from "@krate/runtime"`
// is the runtime itself), as are names bound only by a destructuring pattern
// kept as source text.
//
// The public surface of a module does not change: an exported binding keeps its
// export name (`export const krateTheme` becomes `const krateTheme$u` plus
// `export { krateTheme$u as krateTheme }`), and an import from a project file
// keeps the name it asks for while its local binding is renamed
// (`import { h as h$u }`).
func renameReservedBindings(prog *ast.Program) {
	declared := renamableNames(prog, reservedRuntimeName)
	if _, isAction := isUseServerDirective(prog.Body); isAction {
		// Actions are addressed by export name and run no hydration code.
		for _, stmt := range prog.Body {
			if exp, ok := stmt.(*ast.ExportStmt); ok && !exp.Default {
				for _, name := range declaredNames(exp.Declaration) {
					delete(declared, name)
				}
				for _, spec := range exp.Named {
					delete(declared, spec.Local)
				}
			}
		}
	}
	imported := reservedImportLocals(prog)
	if len(declared) == 0 && len(imported) == 0 {
		return
	}
	names := make(map[string]string, len(declared)+len(imported))
	for name := range declared {
		names[name] = name + reservedSuffix
	}
	for name := range imported {
		names[name] = name + reservedSuffix
	}

	// Collect what applyRenames cannot express while names are still original.
	type split struct {
		at    int
		pairs []ast.NamedExport
	}
	var splits []split
	for i, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || exp.Default || exp.ReexportSource != "" {
			continue
		}
		var pairs []ast.NamedExport
		for _, name := range declaredNames(exp.Declaration) {
			if to, ok := names[name]; ok {
				pairs = append(pairs, ast.NamedExport{Local: to, Remote: name})
			}
		}
		if len(pairs) > 0 {
			splits = append(splits, split{at: i, pairs: pairs})
		}
		for j, spec := range exp.Named {
			if to, ok := names[spec.Local]; ok {
				exp.Named[j].Local = to
			}
		}
	}

	applyRenames(prog, names)

	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		if to, ok := names[imp.Default]; ok && imported[imp.Default] {
			imp.Default = to
		}
		if to, ok := names[imp.Namespace]; ok && imported[imp.Namespace] {
			imp.Namespace = to
		}
		for i, spec := range imp.Named {
			local := spec.Local
			if local == "" {
				local = spec.Remote
			}
			if to, ok := names[local]; ok && imported[local] {
				imp.Named[i] = ast.NamedImport{Local: to, Remote: spec.Remote}
			}
		}
	}
	// Insert the export lists after their declarations, last first so earlier
	// indexes stay valid.
	for k := len(splits) - 1; k >= 0; k-- {
		sp := splits[k]
		exp := prog.Body[sp.at].(*ast.ExportStmt)
		decl := exp.Declaration
		list := &ast.ExportStmt{Position: exp.Position, Named: sp.pairs}
		body := make([]ast.Stmt, 0, len(prog.Body)+1)
		body = append(body, prog.Body[:sp.at]...)
		body = append(body, decl, list)
		body = append(body, prog.Body[sp.at+1:]...)
		prog.Body = body
	}
}

// reservedImportLocals returns the reserved-name bindings the module imports
// from files of the project (relative or alias paths). Runtime and package
// imports are not included: they are the runtime globals themselves.
func reservedImportLocals(prog *ast.Program) map[string]bool {
	out := map[string]bool{}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok || !isProjectSpecifier(strings.Trim(imp.Source, "\"'`")) {
			continue
		}
		add := func(name string) {
			if name != "" && reservedRuntimeName(name) {
				out[name] = true
			}
		}
		add(imp.Default)
		add(imp.Namespace)
		for _, spec := range imp.Named {
			if spec.Local != "" {
				add(spec.Local)
			} else {
				add(spec.Remote)
			}
		}
	}
	return out
}

// isProjectSpecifier reports whether an import specifier names a file of the
// project: a relative path or the conventional "@/" and "~/" aliases.
func isProjectSpecifier(src string) bool {
	return strings.HasPrefix(src, ".") || strings.HasPrefix(src, "/") ||
		strings.HasPrefix(src, "@/") || strings.HasPrefix(src, "~/")
}

// renameBindings appends suffix to every binding whose name matches, and to
// every reference resolving to it, throughout the module.
func renameBindings(prog *ast.Program, match func(string) bool, suffix string) {
	declared := renamableNames(prog, match)
	if len(declared) == 0 {
		return
	}
	names := make(map[string]string, len(declared))
	for name := range declared {
		names[name] = name + suffix
	}
	applyRenames(prog, names)
}

// renamableNames returns the bindings of prog that match. A name bound by an
// import or kept as source text in a destructuring pattern is left out.
func renamableNames(prog *ast.Program, match func(string) bool) map[string]bool {
	declared := map[string]bool{}
	skip := map[string]bool{}
	note := func(name string) {
		if name != "" && match(name) {
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
	return declared
}

// applyRenames renames bindings and the references resolving to them.
func applyRenames(prog *ast.Program, names map[string]string) {
	rn := func(name string) string {
		if to, ok := names[name]; ok {
			return to
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
		case *ast.ExportStmt:
			x.Local = rn(x.Local)
		case *ast.JSXElement:
			if x.Opening != nil {
				x.Opening.Name = renameJSXTag(x.Opening.Name, rn)
			}
			if x.Closing != nil {
				x.Closing.Name = renameJSXTag(x.Closing.Name, rn)
			}
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				if p == nil || !p.Shorthand {
					continue
				}
				// `{ h }` keeps its key: `{ h: h$u }`.
				if id, ok := p.Value.(*ast.Identifier); ok {
					if _, renamed := names[id.Name]; renamed {
						p.Shorthand = false
					}
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

// renameJSXTag renames the binding a tag refers to: `Row` or the `Row` of
// `Row.Cell`. A lowercase tag is an HTML element, not a reference.
func renameJSXTag(tag string, rn func(string) string) string {
	if tag == "" || tag[0] < 'A' || tag[0] > 'Z' {
		return tag
	}
	head, rest, dotted := strings.Cut(tag, ".")
	if dotted {
		return rn(head) + "." + rest
	}
	return rn(head)
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
