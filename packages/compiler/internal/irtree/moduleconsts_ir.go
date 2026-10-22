package irtree

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// referencedModuleConstVars returns `var NAME=<init>` declarations for
// module-level constants used by client-only code (handlers, effects, memos,
// attribute bindings, signal factories). Those expressions are emitted into a
// per-component hydration IIFE where module bindings are not in scope.
//
// A module evaluates once, so a constant whose initializer runs code (a call
// or `new`, e.g. `const store = createStore(0)`) is created once per page
// through __krateModule and shared by every component that uses it. Top-level
// statements of the entry module that touch those constants (`window.__store
// = store`) run once the same way.
func (b *builder) referencedModuleConstVars(node *ComponentNode) []string {
	if node == nil || len(b.moduleConstInits) == 0 {
		return nil
	}

	var sources []string
	for _, h := range node.Handlers {
		sources = append(sources, h.Body)
	}
	for _, r := range node.RefBindings {
		sources = append(sources, r.Callback, r.Target)
	}
	sources = append(sources, node.Effects...)
	sources = append(sources, node.Memos...)
	for _, attr := range node.AttrBindings {
		sources = append(sources, attr.ExprSource)
	}
	for _, sig := range node.Signals {
		sources = append(sources, sig.FactoryJS, sig.RawInit)
	}
	sources = append(sources, node.ExtraVars...)
	sources = append(sources, node.PreSignalVars...)

	used := make(map[string]bool)
	var queue []string
	mark := func(src string) {
		scanJSIdents(src, func(name string) {
			if _, ok := b.moduleConstInits[name]; ok && !used[name] {
				used[name] = true
				queue = append(queue, name)
			}
		})
	}
	for _, src := range sources {
		if src != "" {
			mark(src)
		}
	}

	// Follow constants referenced by other constants' initializers and by the
	// module statements that run with them.
	stmtJS := make([]string, len(b.moduleStmts))
	for i, st := range b.moduleStmts {
		stmtJS[i] = renderStmtJS(st, nil)
	}
	replay := make([]bool, len(b.moduleStmts))
	for {
		for len(queue) > 0 {
			name := queue[0]
			queue = queue[1:]
			if init := b.moduleConstInits[name]; init != nil {
				mark(generateExprJS(init, nil))
			}
		}
		grew := false
		for i, js := range stmtJS {
			if replay[i] || js == "" {
				continue
			}
			for ref := range jsIdentRefs(js) {
				if used[ref] {
					replay[i] = true
					break
				}
			}
			if replay[i] {
				mark(js)
				grew = true
			}
		}
		if !grew || len(queue) == 0 {
			break
		}
	}

	if len(used) == 0 {
		return nil
	}
	decls := make([]string, 0, len(used))
	for _, name := range b.moduleConstOrder(used) {
		init := b.moduleConstInits[name]
		js := generateExprJS(init, nil)
		if js == "" {
			continue
		}
		if runsCode(init) {
			js = "__krateModule(" + strconv.Quote(name) + ",function(){return " + js + ";})"
		}
		decls = append(decls, "var "+name+"="+js)
	}
	for i, js := range stmtJS {
		if replay[i] {
			decls = append(decls, "__krateModule("+strconv.Quote("#stmt"+strconv.Itoa(i))+",function(){"+js+"})")
		}
	}
	return decls
}

// moduleConstOrder sorts the used constants so each one follows the
// constants its initializer reads (alphabetical among independent ones).
func (b *builder) moduleConstOrder(used map[string]bool) []string {
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []string
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(string)
	visit = func(name string) {
		if state[name] != 0 {
			return
		}
		state[name] = 1
		if init := b.moduleConstInits[name]; init != nil {
			deps := make([]string, 0)
			for ref := range jsIdentRefs(generateExprJS(init, nil)) {
				if used[ref] && ref != name {
					deps = append(deps, ref)
				}
			}
			sort.Strings(deps)
			for _, d := range deps {
				visit(d)
			}
		}
		state[name] = 2
		out = append(out, name)
	}
	for _, name := range names {
		visit(name)
	}
	return out
}

// runsCode reports whether an initializer calls or constructs something
// outside a nested function, so evaluating it twice would make two values.
func runsCode(expr ast.Expr) bool {
	found := false
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		if e == nil || found {
			return
		}
		switch x := e.(type) {
		case *ast.CallExpr, *ast.NewExpr, *ast.TaggedTemplateExpr:
			found = true
		case *ast.MemberExpr:
			walk(x.Object)
			if x.Computed {
				walk(x.Property)
			}
		case *ast.BinaryExpr:
			walk(x.Left)
			walk(x.Right)
		case *ast.UnaryExpr:
			walk(x.Arg)
		case *ast.ConditionalExpr:
			walk(x.Test)
			walk(x.Consequent)
			walk(x.Alternate)
		case *ast.ArrayExpr:
			for _, el := range x.Elements {
				walk(el)
			}
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				if p != nil && !p.Method && !p.Getter && !p.Setter {
					walk(p.Value)
				}
			}
		case *ast.TemplateExpr:
			for _, p := range x.Parts {
				walk(p)
			}
		case *ast.TypeAssertion:
			walk(x.Expr)
		case *ast.ParenExpr:
			walk(x.Expr)
		}
	}
	walk(expr)
	return found
}

// collectModuleStmts returns the entry module's top-level statements that run
// code when the module loads (expression statements, branches, loops), as
// opposed to declarations, imports and exports.
func collectModuleStmts(prog *ast.Program) []ast.Stmt {
	if prog == nil {
		return nil
	}
	var out []ast.Stmt
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.ExprStmt:
			// Directive prologues ("use client") are not code.
			if _, ok := s.Expression.(*ast.Literal); ok {
				continue
			}
			out = append(out, s)
		case *ast.IfStmt, *ast.BlockStmt, *ast.ForStmt, *ast.ForInStmt, *ast.WhileStmt, *ast.TryStmt:
			out = append(out, s)
		}
	}
	return out
}

// checkUnbundledImports reports (KR8302) a binding imported by name from a
// local module that the component's hydration code uses but the compiler
// could not copy into the page script: it is neither a function nor a
// constant the bundler collected (for example a class, a `let`, or a value
// re-exported through a namespace).
func (b *builder) checkUnbundledImports(node *ComponentNode) {
	if b.entryProg == nil || node == nil || b.locateOnly > 0 {
		return
	}
	type origin struct {
		imp  *ast.ImportStmt
		name string
	}
	imported := map[string]origin{}
	for _, stmt := range b.entryProg.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok || !localCodeModule(imp.Source) {
			continue
		}
		for _, n := range imp.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			if local != "" {
				imported[local] = origin{imp: imp, name: local}
			}
		}
	}
	if len(imported) == 0 {
		return
	}
	var sources []string
	for _, h := range node.Handlers {
		sources = append(sources, h.Body)
	}
	sources = append(sources, node.Effects...)
	sources = append(sources, node.Memos...)
	for _, attr := range node.AttrBindings {
		sources = append(sources, attr.ExprSource)
	}
	for _, sig := range node.Signals {
		sources = append(sources, sig.FactoryJS, sig.RawInit)
	}
	sources = append(sources, node.ExtraVars...)
	sources = append(sources, node.PreSignalVars...)
	missing := map[string]bool{}
	for _, src := range sources {
		scanJSIdents(src, func(name string) {
			o, ok := imported[name]
			if !ok || missing[name] {
				return
			}
			if _, fn := b.functions[name]; fn {
				return
			}
			if _, c := b.moduleConstInits[name]; c {
				return
			}
			if _, act := b.actionURLs[name]; act {
				return
			}
			if _, goc := b.goComponents[name]; goc {
				return
			}
			if r := name[0]; r >= 'A' && r <= 'Z' {
				// Components and classes rendered through the slot tree.
				return
			}
			missing[name] = true
			src := strings.Trim(o.imp.Source, "\"'`")
			b.warnCodeAt(o.imp.Position, diag.CodeUnbundledImport,
				"`"+name+"` imported from "+strconv.Quote(src)+" is used by hydration code of <"+node.Name+"> but could not be bundled into the page script; it is undefined in the browser",
				"export it from "+strconv.Quote(src)+" as a function declaration or a const, or define it in this module")
		})
	}
}

// localCodeModule reports a relative import of a script module (not a
// stylesheet, JSON or asset import).
func localCodeModule(source string) bool {
	src := strings.Trim(source, "\"'`")
	if !strings.HasPrefix(src, "./") && !strings.HasPrefix(src, "../") {
		return false
	}
	switch strings.ToLower(filepath.Ext(src)) {
	case "", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".mts":
		return true
	}
	return false
}
