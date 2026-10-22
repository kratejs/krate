package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Render-phase mutation analysis.
//
// Build-time folding of component locals must never keep a value that a later
// statement in the component body changes before it is read. The walker below
// finds the names a statement may change while the body runs: assignment and
// update targets, receivers of non-pure method calls, and the names a local
// helper changes when the body calls it. Function values that run later
// (handlers, effects, functions stored in variables) are not part of the
// render phase and are skipped; callbacks passed directly to a call are
// assumed to run synchronously and are included.

type mutationKind int

const (
	// mutContents changes the contents of the value bound to the name (a
	// member write or a mutating method call). Primitive values cannot change
	// this way.
	mutContents mutationKind = iota + 1
	// mutAssign rebinds the name itself.
	mutAssign
)

// mutationSet maps a name to the strongest change a statement makes to it.
type mutationSet map[string]mutationKind

func (m mutationSet) add(name string, kind mutationKind) {
	if name == "" {
		return
	}
	if m[name] < kind {
		m[name] = kind
	}
}

// pureMethods are methods that never change their receiver. Any other method
// called on a local is assumed to mutate it.
var pureMethods = map[string]bool{
	"at": true, "charAt": true, "charCodeAt": true, "codePointAt": true,
	"concat": true, "endsWith": true, "entries": true, "every": true,
	"filter": true, "find": true, "findIndex": true, "findLast": true,
	"findLastIndex": true, "flat": true, "flatMap": true, "forEach": true,
	"get": true, "has": true, "hasOwnProperty": true, "includes": true,
	"indexOf": true, "join": true, "keys": true, "lastIndexOf": true,
	"localeCompare": true, "map": true, "match": true, "matchAll": true,
	"normalize": true, "padEnd": true, "padStart": true, "reduce": true,
	"reduceRight": true, "repeat": true, "replace": true, "replaceAll": true,
	"search": true, "slice": true, "some": true, "split": true,
	"startsWith": true, "substring": true, "toFixed": true, "toJSON": true,
	"toLocaleString": true, "toLowerCase": true, "toReversed": true,
	"toSorted": true, "toSpliced": true, "toString": true, "toUpperCase": true,
	"trim": true, "trimEnd": true, "trimStart": true, "valueOf": true,
	"with": true, "getTime": true, "toISOString": true, "test": true,
	"exec": true, "call": true, "apply": true, "bind": true,
}

// deferredCallees are body-level calls whose function arguments run after
// the render phase (effects, lifecycle callbacks) or are memo getters.
var deferredCallees = map[string]bool{
	"createEffect": true, "createEffectDeps": true, "onMount": true,
	"onCleanup": true, "createMemo": true, "createMemoDeps": true,
	"krateDerived": true,
}

// mutationWalker collects a mutationSet for render-phase code.
type mutationWalker struct {
	fns      map[string]*ast.ArrowFn
	fnMuts   map[string]mutationSet
	visiting map[string]bool
	out      mutationSet
}

// newMutationWalker prepares a walker for a component body; the body's local
// helper functions are indexed so calls to them contribute their mutations.
func newMutationWalker(body []ast.Stmt) *mutationWalker {
	w := &mutationWalker{
		fns:      make(map[string]*ast.ArrowFn),
		fnMuts:   make(map[string]mutationSet),
		visiting: make(map[string]bool),
	}
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			w.fns[s.Name] = &ast.ArrowFn{Params: s.Params, Body: s.Body, Function: true}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if fn, ok := d.Init.(*ast.ArrowFn); ok && !d.IsDestructuring && d.Name != "" {
					w.fns[d.Name] = fn
				}
			}
		}
	}
	return w
}

// stmtMutations returns the names stmt may change while it runs.
func (w *mutationWalker) stmtMutations(stmt ast.Stmt) mutationSet {
	return w.collect(func() { w.stmt(stmt) })
}

// exprMutations returns the names expr may change while it is evaluated.
func (w *mutationWalker) exprMutations(expr ast.Expr) mutationSet {
	return w.collect(func() { w.expr(expr) })
}

func (w *mutationWalker) collect(walk func()) mutationSet {
	saved := w.out
	w.out = make(mutationSet)
	walk()
	got := w.out
	w.out = saved
	return got
}

func (w *mutationWalker) stmts(stmts []ast.Stmt) {
	for _, s := range stmts {
		w.stmt(s)
	}
}

func (w *mutationWalker) stmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		w.expr(s.Expression)
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if _, isFn := d.Init.(*ast.ArrowFn); !isFn {
				w.expr(d.Init)
			}
		}
	case *ast.ReturnStmt:
		w.expr(s.Value)
	case *ast.ThrowStmt:
		w.expr(s.Value)
	case *ast.IfStmt:
		w.expr(s.Test)
		w.stmts(s.Consequent)
		w.stmts(s.Alternate)
	case *ast.BlockStmt:
		w.stmts(s.Body)
	case *ast.ForStmt:
		if s.Init != nil {
			w.stmt(s.Init)
		}
		w.expr(s.Test)
		w.expr(s.Update)
		w.stmts(s.Body)
	case *ast.ForInStmt:
		if s.Keyword == "" {
			w.target(s.Left, mutAssign)
		}
		w.expr(s.Right)
		w.stmts(s.Body)
	case *ast.WhileStmt:
		w.expr(s.Test)
		w.stmts(s.Body)
	case *ast.DoWhileStmt:
		w.stmts(s.Body)
		w.expr(s.Test)
	case *ast.SwitchStmt:
		w.expr(s.Discriminant)
		for _, c := range s.Cases {
			w.expr(c.Test)
			w.stmts(c.Body)
		}
	case *ast.TryStmt:
		w.stmts(s.Body)
		if s.Catch != nil {
			w.stmts(s.Catch.Body)
		}
		w.stmts(s.Finally)
	case *ast.LabeledStmt:
		w.stmt(s.Body)
	case *ast.WithStmt:
		w.expr(s.Object)
		w.stmts(s.Body)
	}
}

func isAssignOp(op string) bool {
	switch op {
	case "=", "+=", "-=", "*=", "/=", "%=", "**=", "<<=", ">>=", ">>>=",
		"&=", "|=", "^=", "&&=", "||=", "??=":
		return true
	}
	return false
}

func (w *mutationWalker) expr(expr ast.Expr) {
	switch e := expr.(type) {
	case nil:
	case *ast.BinaryExpr:
		if isAssignOp(e.Op) {
			w.target(e.Left, mutAssign)
		} else {
			w.expr(e.Left)
		}
		w.expr(e.Right)
	case *ast.UnaryExpr:
		switch e.Op {
		case "++", "--":
			w.target(e.Arg, mutAssign)
		case "delete":
			w.target(e.Arg, mutContents)
		default:
			w.expr(e.Arg)
		}
	case *ast.CallExpr:
		w.call(e.Callee, e.Args)
	case *ast.NewExpr:
		w.expr(e.Callee)
		w.args(e.Args)
	case *ast.MemberExpr:
		w.expr(e.Object)
		if e.Computed {
			w.expr(e.Property)
		}
	case *ast.ConditionalExpr:
		w.expr(e.Test)
		w.expr(e.Consequent)
		w.expr(e.Alternate)
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			w.expr(p)
		}
	case *ast.TaggedTemplateExpr:
		w.expr(e.Tag)
		if e.Quasi != nil {
			w.expr(e.Quasi)
		}
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			w.expr(el)
		}
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p.Computed {
				w.expr(p.KeyExpr)
			}
			if _, isFn := p.Value.(*ast.ArrowFn); !isFn {
				w.expr(p.Value)
			}
		}
	case *ast.ParenExpr:
		w.expr(e.Expr)
	case *ast.TypeAssertion:
		w.expr(e.Expr)
	case *ast.AwaitExpr:
		w.expr(e.Arg)
	case *ast.DynamicImport:
		w.expr(e.Arg)
	}
}

// args walks call arguments. Function arguments are walked as if invoked,
// since array helpers and similar APIs call them synchronously.
func (w *mutationWalker) args(args []ast.Expr) {
	for _, a := range args {
		if fn, ok := a.(*ast.ArrowFn); ok {
			w.invoke(fn)
			continue
		}
		w.expr(a)
	}
}

func (w *mutationWalker) invoke(fn *ast.ArrowFn) {
	inner := w.collect(func() { w.stmts(fn.Body) })
	shadow := make(map[string]bool)
	for _, n := range functionScopeNames(fn.Params, fn.Body) {
		shadow[n] = true
	}
	for name, kind := range inner {
		if !shadow[name] {
			w.out.add(name, kind)
		}
	}
}

func (w *mutationWalker) call(callee ast.Expr, args []ast.Expr) {
	switch c := callee.(type) {
	case *ast.ArrowFn:
		w.invoke(c)
		w.args(args)
		return
	case *ast.ParenExpr:
		if fn, ok := c.Expr.(*ast.ArrowFn); ok {
			w.invoke(fn)
			w.args(args)
			return
		}
	case *ast.Identifier:
		if deferredCallees[c.Name] {
			return
		}
		if fn := w.fns[c.Name]; fn != nil {
			w.localCall(c.Name, fn, args)
		}
		w.args(args)
		return
	case *ast.MemberExpr:
		method := ""
		if id, ok := c.Property.(*ast.Identifier); ok && !c.Computed {
			method = id.Name
		}
		root := rootIdentName(c.Object)
		if root == "Object" && method == "assign" && len(args) > 0 {
			w.out.add(rootIdentName(args[0]), mutContents)
		} else if !pureMethods[method] {
			w.out.add(root, mutContents)
		}
		w.expr(c.Object)
		if c.Computed {
			w.expr(c.Property)
		}
		w.args(args)
		return
	}
	w.expr(callee)
	w.args(args)
}

// localCall adds what a call to a local helper changes: the enclosing names
// its body changes, plus the contents of arguments bound to parameters whose
// contents it changes.
func (w *mutationWalker) localCall(name string, fn *ast.ArrowFn, args []ast.Expr) {
	muts, ok := w.fnMuts[name]
	if !ok {
		if w.visiting[name] {
			return
		}
		w.visiting[name] = true
		muts = w.collect(func() { w.stmts(fn.Body) })
		w.visiting[name] = false
		w.fnMuts[name] = muts
	}
	params := make(map[string]int, len(fn.Params))
	for i, p := range fn.Params {
		if p.Name != "" && p.Pattern == "" && !p.IsRest {
			params[p.Name] = i
		}
	}
	shadow := make(map[string]bool)
	for _, n := range functionScopeNames(fn.Params, fn.Body) {
		shadow[n] = true
	}
	for n, kind := range muts {
		if i, isParam := params[n]; isParam {
			if kind == mutContents && i < len(args) {
				w.out.add(rootIdentName(args[i]), mutContents)
			}
			continue
		}
		if !shadow[n] {
			w.out.add(n, kind)
		}
	}
}

// target records an assignment-like write to e.
func (w *mutationWalker) target(e ast.Expr, kind mutationKind) {
	switch t := e.(type) {
	case *ast.Identifier:
		w.out.add(t.Name, kind)
	case *ast.MemberExpr:
		w.out.add(rootIdentName(t.Object), mutContents)
		w.expr(t.Object)
		if t.Computed {
			w.expr(t.Property)
		}
	case *ast.ParenExpr:
		w.target(t.Expr, kind)
	case *ast.TypeAssertion:
		w.target(t.Expr, kind)
	case *ast.ArrayExpr, *ast.ObjectExpr:
		for _, n := range exprPatternNames(t) {
			w.out.add(n, mutAssign)
		}
	}
}

// RenderMutatedNames returns the names a function body may change while it
// runs (reassignments and content mutations, see mutationWalker). Callers
// folding a body-level binding at build time must not fold these.
func RenderMutatedNames(body []ast.Stmt) map[string]bool {
	w := newMutationWalker(body)
	out := make(map[string]bool)
	for _, s := range body {
		for name := range w.stmtMutations(s) {
			out[name] = true
		}
	}
	return out
}

// rootIdentName returns the identifier at the base of a member chain
// (`a` for `a.b[c].d`), or "" when the base is not a plain identifier.
func rootIdentName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.Identifier:
			return x.Name
		case *ast.MemberExpr:
			e = x.Object
		case *ast.ParenExpr:
			e = x.Expr
		case *ast.TypeAssertion:
			e = x.Expr
		default:
			return ""
		}
	}
}

// isPrimitiveValue reports whether v is a known primitive, whose contents no
// statement can change.
func isPrimitiveValue(v jsvalue.Value) bool {
	return !v.IsUnknown() && !v.IsArray() && !v.IsObject() && !v.IsFunction() && !v.IsJSX()
}
