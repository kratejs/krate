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
	if node == nil || (len(b.moduleConstInits) == 0 && len(b.moduleDeclOnly) == 0 && len(b.moduleNS) == 0) {
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
	// Runtime list bindings evaluate their array and row render in this scope.
	sources = append(sources, listBindingSources(node.Children)...)
	sources = append(sources, listBindingSources(node.CallSiteSlots)...)

	// Members read through namespace imports (`ns.add`) decide which exports
	// the namespace object carries.
	b.nsUse = b.namespaceMembers(sources)

	used := make(map[string]bool)
	var queue []string
	mark := func(src string) {
		scanJSIdents(src, func(name string) {
			if b.isModuleBinding(name) && !used[name] {
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
			mark(b.moduleInitJS(name))
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
	// A module-level let/var is mutable state: every component must read and
	// write the same variable, so it (and what its initializer reads) is
	// declared once at the top of the page script instead of per component.
	b.hoistSharedModuleVars(used)
	decls := make([]string, 0, len(used))
	for _, name := range b.moduleConstOrder(used) {
		if b.moduleLets[name] {
			continue
		}
		if decl := b.moduleVarDecl(name); decl != "" {
			decls = append(decls, decl)
		}
	}
	for i, js := range stmtJS {
		if replay[i] {
			decls = append(decls, "__krateModule("+strconv.Quote("#stmt"+strconv.Itoa(i))+",function(){"+js+"})")
		}
	}
	return decls
}

// moduleVarDecl returns the `var NAME=<init>` declaration of a module binding,
// created once per page through __krateModule when the initializer runs code.
func (b *builder) moduleVarDecl(name string) string {
	if b.moduleDeclOnly[name] && b.moduleConstInits[name] == nil {
		return "var " + name
	}
	init := b.moduleConstInits[name]
	js := b.moduleInitJS(name)
	if js == "" {
		return ""
	}
	if init != nil && runsCode(init) {
		js = "__krateModule(" + strconv.Quote(name) + ",function(){return " + js + ";})"
	}
	return "var " + name + "=" + js
}

// hoistSharedModuleVars records, in dependency order, the declarations of the
// used module-level let/var bindings and of the constants their initializers
// read, for emission at the top of the page script.
func (b *builder) hoistSharedModuleVars(used map[string]bool) {
	closure := map[string]bool{}
	var add func(name string)
	add = func(name string) {
		if closure[name] || !used[name] {
			return
		}
		closure[name] = true
		for ref := range jsIdentRefs(b.moduleInitJS(name)) {
			add(ref)
		}
	}
	for name := range used {
		if b.moduleLets[name] {
			add(name)
		}
	}
	if len(closure) == 0 {
		return
	}
	if b.sharedSeen == nil {
		b.sharedSeen = map[string]bool{}
	}
	for _, name := range b.moduleConstOrder(closure) {
		if b.sharedSeen[name] {
			continue
		}
		if decl := b.moduleVarDecl(name); decl != "" {
			b.sharedSeen[name] = true
			b.sharedModuleVars = append(b.sharedModuleVars, decl)
		}
	}
}

// collectModuleLets returns the names declared by module-level `let` and
// `var` statements with an initializer.
func collectModuleLets(prog *ast.Program) map[string]bool {
	out := map[string]bool{}
	if prog == nil {
		return out
	}
	for _, stmt := range prog.Body {
		var vs *ast.VarStmt
		switch s := stmt.(type) {
		case *ast.VarStmt:
			vs = s
		case *ast.ExportStmt:
			if v, ok := s.Declaration.(*ast.VarStmt); ok {
				vs = v
			}
		}
		if vs == nil || (vs.Kind != ast.VarLet && vs.Kind != ast.VarVar) {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name != "" && decl.Init != nil {
				out[decl.Name] = true
			}
		}
	}
	return out
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
		if js := b.moduleInitJS(name); js != "" {
			deps := make([]string, 0)
			for ref := range b.moduleInitRefs(name, js) {
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

// moduleInitRefs returns the identifiers a module binding's initializer
// reads. An initializer with an AST is walked, because the text scan counts
// object keys as references: `const cfg = {label, fmt}` would then depend on
// the bindings `const { label, fmt } = cfg` creates and the order would loop.
func (b *builder) moduleInitRefs(name, js string) map[string]bool {
	init := b.moduleConstInits[name]
	if init == nil {
		return jsIdentRefs(js)
	}
	refs := map[string]bool{}
	WalkAST(init, func(n any) bool {
		if id, ok := n.(*ast.Identifier); ok {
			refs[id.Name] = true
		}
		return true
	})
	return refs
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
		if imp.Default != "" {
			imported[imp.Default] = origin{imp: imp, name: imp.Default}
		}
		if imp.Namespace != "" {
			imported[imp.Namespace] = origin{imp: imp, name: imp.Namespace}
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
	sources = append(sources, listBindingSources(node.Children)...)
	sources = append(sources, listBindingSources(node.CallSiteSlots)...)
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
			if b.isModuleBinding(name) {
				return
			}
			if _, act := b.actionURLs[name]; act {
				return
			}
			if _, goc := b.goComponents[name]; goc {
				return
			}
			if b.ann.UsedComponents[name] {
				// Components rendered through the slot tree.
				return
			}
			missing[name] = true
			src := strings.Trim(o.imp.Source, "\"'`")
			b.warnCodeAt(o.imp.Position, diag.CodeUnbundledImport,
				"`"+name+"` imported from "+strconv.Quote(src)+" is used by hydration code of <"+node.Name+"> but could not be bundled into the page script; it is undefined in the browser",
				"export it from "+strconv.Quote(src)+" as a function declaration, class or const (not a destructured binding), or define it in this module")
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

// isModuleBinding reports whether name is a module-level value the hydration
// scope can declare.
func (b *builder) isModuleBinding(name string) bool {
	if _, ok := b.moduleConstInits[name]; ok {
		return true
	}
	return b.moduleDeclOnly[name] || b.moduleNS[name] != nil
}

// moduleInitJS renders the initializer of a module binding as JS: its
// expression, or for a namespace import an object of the members read from it.
func (b *builder) moduleInitJS(name string) string {
	if init := b.moduleConstInits[name]; init != nil {
		return generateExprJS(init, nil)
	}
	exports := b.moduleNS[name]
	if exports == nil {
		return ""
	}
	use := b.nsUse[name]
	keys := make([]string, 0, len(exports))
	for k := range exports {
		if use == nil || use[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, strconv.Quote(k)+":"+exports[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// namespaceMembers finds, per namespace-import binding, the members read as
// `ns.member`. A nil entry means the whole namespace is needed (bracket access
// or the bare identifier used as a value).
func (b *builder) namespaceMembers(sources []string) map[string]map[string]bool {
	if len(b.moduleNS) == 0 {
		return nil
	}
	out := map[string]map[string]bool{}
	whole := map[string]bool{}
	for _, src := range sources {
		for ns := range b.moduleNS {
			if !strings.Contains(src, ns) || strings.HasPrefix(src, "var "+ns+"=") {
				continue
			}
			for i := 0; ; {
				j := strings.Index(src[i:], ns)
				if j < 0 {
					break
				}
				start, end := i+j, i+j+len(ns)
				i = end
				if start > 0 && isIdentByte(src[start-1]) || start > 0 && src[start-1] == '.' {
					continue
				}
				if end < len(src) && isIdentByte(src[end]) {
					continue
				}
				if end < len(src) && src[end] == '.' {
					k := end + 1
					m := k
					for m < len(src) && isIdentByte(src[m]) {
						m++
					}
					if m > k {
						if out[ns] == nil {
							out[ns] = map[string]bool{}
						}
						out[ns][src[k:m]] = true
						continue
					}
				}
				whole[ns] = true
			}
		}
	}
	for ns := range whole {
		delete(out, ns)
	}
	for ns := range b.moduleNS {
		if out[ns] == nil && !whole[ns] {
			out[ns] = map[string]bool{}
		}
	}
	for ns := range whole {
		out[ns] = nil
	}
	return out
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// JSIdentRefs returns the free identifier names a JS fragment references.
func JSIdentRefs(code string) map[string]bool { return jsIdentRefs(code) }
