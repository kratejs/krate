package irtree

import (
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Module side effects in the browser.
//
// Top-level setup code of a client module that configures a package
// (`extend({ OrbitControls })`, `dayjs.extend(plugin)`,
// `THREE.ColorManagement.enabled = false`) has to run in the browser before
// the page hydrates, as it would when the module loads. The build lists those
// statements, rooted in a binding imported from a package, in module
// dependency order (BuildOptions.ClientModuleStmts); the page script runs each
// once per page load (__krateModule) ahead of the component scopes, and the
// per-component replay of module statements leaves them out. The module
// bindings and functions they read are declared with them.

// ClientModuleStmt is one top-level statement of a client module.
type ClientModuleStmt struct {
	Stmt ast.Stmt
	// Key identifies the statement for __krateModule (stable per module path
	// and position).
	Key  string
	File string
	// Bindings are the names the module imports from other modules or
	// declares at top level; a statement reading one that cannot be brought
	// into the page script is reported.
	Bindings map[string]bool
}

func clientStmtKeys(stmts []ClientModuleStmt) map[ast.Stmt]string {
	if len(stmts) == 0 {
		return nil
	}
	out := make(map[ast.Stmt]string, len(stmts))
	for _, s := range stmts {
		out[s.Stmt] = s.Key
	}
	return out
}

// SideEffectRoot returns the identifier a setup statement starts from: the
// root of the callee of a call or `new` (`dayjs` in `dayjs.extend(p)`), or of
// the target of a member assignment (`THREE` in
// `THREE.ColorManagement.enabled = false`). It is "" for anything else.
func SideEffectRoot(stmt ast.Stmt) string {
	if !IsClientSideEffect(stmt) {
		return ""
	}
	e := unparen(stmt.(*ast.ExprStmt).Expression)
	switch v := e.(type) {
	case *ast.CallExpr:
		e = v.Callee
	case *ast.NewExpr:
		e = v.Callee
	case *ast.BinaryExpr:
		e = v.Left
	}
	for {
		switch v := unparen(e).(type) {
		case *ast.Identifier:
			return v.Name
		case *ast.MemberExpr:
			e = v.Object
		case *ast.CallExpr:
			e = v.Callee
		default:
			return ""
		}
	}
}

// IsClientSideEffect reports whether a top-level statement is setup code to
// replay in the browser: a call, a `new` expression, or an assignment to a
// member (`obj.prop = v`).
func IsClientSideEffect(stmt ast.Stmt) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok || es.Expression == nil {
		return false
	}
	e := es.Expression
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.Expr
	}
	switch v := e.(type) {
	case *ast.CallExpr, *ast.NewExpr:
		return true
	case *ast.BinaryExpr:
		if _, member := v.Left.(*ast.MemberExpr); member && isAssignOp(v.Op) {
			return true
		}
	}
	return false
}

// moduleInitBlock renders the page's module setup statements with the
// declarations they need, each guarded so a throw is reported and does not
// stop hydration. It returns nil when there are none.
func (b *builder) moduleInitBlock() []string {
	if len(b.clientStmts) == 0 {
		return nil
	}
	type item struct {
		key, js string
	}
	var items []item
	for _, cs := range b.clientStmts {
		js := strings.TrimRight(strings.TrimSpace(renderStmtJS(cs.Stmt, nil)), ";")
		if js == "" {
			continue
		}
		if missing := b.unresolvedBindings(cs, js); len(missing) > 0 {
			b.warnModuleStmt(cs, missing)
			continue
		}
		items = append(items, item{cs.Key, js})
	}
	if len(items) == 0 {
		return nil
	}
	// The statements' code is scanned like a component's, which declares the
	// module bindings and helper functions it reads.
	node := &ComponentNode{}
	for _, it := range items {
		node.ExtraVars = append(node.ExtraVars, it.js)
	}
	var out []string
	for _, decl := range b.referencedModuleConstVars(node) {
		out = append(out, guardJS(decl))
	}
	for _, fn := range b.collectReferencedFunctions(node, nil) {
		if js := b.reactifier().renderFnDeclJS(fn, nil); js != "" {
			out = append(out, js)
		}
	}
	for _, it := range items {
		out = append(out, guardJS("__krateModule("+strconv.Quote(it.key)+",function(){"+it.js+";})"))
	}
	return out
}

func guardJS(js string) string {
	return "try{" + js + ";}catch(e){__krateScopeErr(e);}"
}

// unresolvedBindings lists the module bindings a statement reads that the
// page script cannot declare: not a module constant, function or vendored
// import.
func (b *builder) unresolvedBindings(cs ClientModuleStmt, js string) []string {
	var out []string
	seen := map[string]bool{}
	scanJSIdents(js, func(name string) {
		if seen[name] || !cs.Bindings[name] {
			return
		}
		seen[name] = true
		if b.isModuleBinding(name) || b.functions[name] != nil || b.vendorLocals[name] {
			return
		}
		out = append(out, name)
	})
	sort.Strings(out)
	return out
}

func (b *builder) warnModuleStmt(cs ClientModuleStmt, missing []string) {
	pos := cs.Stmt.Pos()
	d := diag.New(cs.File, pos.Line, pos.Col,
		"this top-level statement does not run in the browser: "+strings.Join(missing, ", ")+" cannot be brought into the page script",
		"import the value from a package (it is then bundled for the browser) or declare it as a top-level const or function; or move the statement into a component (for example in onMount)",
		"").WithCode(diag.CodeUnbundledImport)
	d.Severity = diag.SeverityWarning
	b.warnings = append(b.warnings, d)
}
