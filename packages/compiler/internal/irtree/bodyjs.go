package irtree

import (
	"sort"
	"strings"
	"unicode"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// bodyJS is one hydration-scope fragment taken from a component body, tagged
// with its position in the body so fragments collected by different passes
// (plain declarations, folded locals, statements) are emitted in source order.
type bodyJS struct {
	ord int
	js  string
}

// bodyOrder numbers a component body's statements and declarators in source
// order (a pre-order walk, so a compound statement precedes its contents).
type bodyOrder struct {
	ords      map[any]int
	firstDecl map[string]int
	next      int
}

func newBodyOrder(body []ast.Stmt) *bodyOrder {
	o := &bodyOrder{ords: make(map[any]int), firstDecl: make(map[string]int)}
	o.stmts(body)
	return o
}

func (o *bodyOrder) stmts(stmts []ast.Stmt) {
	for _, s := range stmts {
		o.stmt(s)
	}
}

func (o *bodyOrder) stmt(stmt ast.Stmt) {
	if stmt == nil {
		return
	}
	o.ords[stmt] = o.next
	o.next++
	switch s := stmt.(type) {
	case *ast.VarStmt:
		for _, d := range s.Decls {
			o.ords[d] = o.next
			for _, name := range varDeclBindingNames(d) {
				if _, seen := o.firstDecl[name]; !seen {
					o.firstDecl[name] = o.next
				}
			}
			o.next++
		}
	case *ast.BlockStmt:
		o.stmts(s.Body)
	case *ast.IfStmt:
		o.stmts(s.Consequent)
		o.stmts(s.Alternate)
	}
}

// of returns the ordinal of a statement or declarator, or -1 when unknown.
func (o *bodyOrder) of(node any) int {
	if n, ok := o.ords[node]; ok {
		return n
	}
	return -1
}

// ofName returns the ordinal of the first declaration of name, or -1.
func (o *bodyOrder) ofName(name string) int {
	if n, ok := o.firstDecl[name]; ok {
		return n
	}
	return -1
}

// mergeBodyJS merges fragment groups into source order. Fragments with the
// same ordinal keep their group order.
func mergeBodyJS(groups ...[]bodyJS) []string {
	var all []bodyJS
	for _, g := range groups {
		all = append(all, g...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].ord < all[j].ord })
	return bodyJSStrings(all)
}

func bodyJSStrings(frags []bodyJS) []string {
	if len(frags) == 0 {
		return nil
	}
	out := make([]string, len(frags))
	for i, f := range frags {
		out[i] = f.js
	}
	return out
}

// collectExtraVarJS walks a component body's top-level declarations and
// statements and returns the ones the hydration scope must replay, split into
// pre (fragments that read NO signal getters, safe to emit before the signal
// declarations) and post (fragments that read signals and so must follow the
// signal declarations). Both groups are in source order. Statements other
// than declarations (expression statements, branches, loops, switch, try) are
// kept so a mutation such as `cache.set(k, v)` runs before the bindings that
// read the local. Hook calls (effects, memos, signals) are collected by their
// own passes and render early-return guards are skipped. A reassignment of a
// parameter (`className = cn(className, "x")`) is folded into the parameter's
// build-time value instead, since parameters are not hydration-scope bindings.
func (b *builder) collectExtraVarJS(body []ast.Stmt, order *bodyOrder, params map[string]bool) (pre, post []bodyJS) {
	add := func(node any, js string, reactive bool) {
		// The emitter terminates every fragment itself.
		js = strings.TrimSuffix(js, ";")
		if js == "" {
			return
		}
		frag := bodyJS{ord: order.of(node), js: js}
		if reactive {
			post = append(post, frag)
		} else {
			pre = append(pre, frag)
		}
	}
	for idx, stmt := range body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if js, ok := b.extraVarDeclJS(decl); ok {
					add(decl, js, decl.Init != nil && b.referencesSignal(decl.Init))
				}
			}
		case *ast.BlockStmt:
			p, po := b.collectExtraVarJS(s.Body, order, params)
			pre = append(pre, p...)
			post = append(post, po...)
		case *ast.IfStmt:
			// Render early-return guards (`if (!items || items.length === 0)
			// return <span/>`) are a render-time decision already captured by
			// the SSR slot output. Re-emitting them at hydration as a top-level
			// statement that can `return` would abort the component IIFE BEFORE
			// its refs/effects register. Skip them here.
			if ifEndsInReturn(s) {
				continue
			}
			reactive := b.referencesSignal(s.Test) || stmtsReferenceSignal(s.Consequent, b) || stmtsReferenceSignal(s.Alternate, b)
			js := renderStmtJS(s, b.sigMap())
			if isThrowGuard(s) {
				// Re-check the guard when its signals change, recomputing the
				// locals it reads (throwguard.go).
				withDeps, live := b.throwGuardWithDeps(body, idx, js)
				if reactive || live {
					js = throwGuardJS(withDeps)
					reactive = true
				}
			}
			add(s, js, reactive)
		case *ast.ExprStmt:
			if !isReplayedBodyExpr(s.Expression) || params[assignedIdent(s.Expression)] {
				continue
			}
			// A custom hook may read the component's signals through
			// closures, so it runs after the signal declarations.
			add(s, renderStmtJS(s, b.sigMap()), isCustomHookCall(s.Expression) || stmtReferenceSignal(s, b))
		case *ast.ForStmt, *ast.ForInStmt, *ast.WhileStmt, *ast.DoWhileStmt:
			add(s, renderStmtJS(s, b.sigMap()), stmtReferenceSignal(s, b))
		case *ast.SwitchStmt, *ast.TryStmt, *ast.LabeledStmt:
			// A `return` here would end the hydration IIFE early.
			if stmtHasReturn(s) {
				continue
			}
			add(s, renderStmtJS(s, b.sigMap()), stmtReferenceSignal(s, b))
		}
	}
	return pre, post
}

// extraVarDeclJS renders one top-level declarator for the hydration scope, or
// reports false when another pass owns it (signals, resources, useId, context
// reads, scope handles) or the builder emits it as a folded local (props-
// derived and destructured declarators).
func (b *builder) extraVarDeclJS(decl *ast.VarDecl) (string, bool) {
	if decl.Init == nil {
		// `let timer;` has no value to fold, but handlers and effects may
		// assign it, so it must still be declared in the hydration scope.
		if !decl.IsDestructuring && decl.Name != "" && decl.Name != "children" {
			return "var " + decl.Name, true
		}
		return "", false
	}
	// useId() is resolved to a per-instance literal by the caller
	// (collectLocalVars) and emitted from there.
	if isUseIdCall(decl.Init) {
		return "", false
	}
	// A context read folds to its default value (SSR) and is emitted as a
	// folded local; `var x=Ctx.useContext()` would reference an undefined
	// context object at hydration time.
	if isContextRead(decl.Init) {
		return "", false
	}
	// A first-class scope handle is compile-time state, never a runtime local.
	if isCSSFactoryCall(decl.Init) {
		return "", false
	}
	if call, ok := decl.Init.(*ast.CallExpr); ok {
		if id, ok := call.Callee.(*ast.Identifier); ok {
			// createMemo stays an extra var so the named getter (const doubled
			// = createMemo(...)) is declared with its real name, letting slot
			// bindings reference it as () => doubled(). Signals/resources/
			// effects are handled by their own collection passes.
			switch id.Name {
			case "createSignal", "createResource", "createEffect":
				return "", false
			}
		}
	}
	if decl.IsDestructuring || referencesProps(decl.Init) {
		return "", false
	}
	if derivedCallee(decl.Init) != "" {
		return "var " + decl.Name + "=" + generateExprJS(b.derivedDeclInit(decl.Init), b.sigMap()), true
	}
	return "var " + decl.Name + "=" + generateExprJS(decl.Init, b.sigMap()), true
}

// isReplayedBodyExpr reports whether a body-level expression statement must
// run in the hydration scope. Hook and lifecycle calls are owned by their own
// collection passes, and value-only statements (directives, bare reads) have
// no effect.
func isReplayedBodyExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case nil, *ast.Literal, *ast.Identifier, *ast.TemplateExpr, *ast.ArrowFn:
		return false
	case *ast.CallExpr:
		if isUseIdCall(e) || isContextRead(e) || isCSSFactoryCall(e) {
			return false
		}
		if id, ok := e.Callee.(*ast.Identifier); ok {
			// A custom hook (`useTitle("x")`) is ordinary code that runs once
			// per instance; only the runtime's own hooks have dedicated passes.
			return !isHookCallee(id.Name) || isCustomHookCallee(id.Name)
		}
	}
	return true
}

// assignedIdent returns the identifier an assignment or update expression
// writes (`x` in `x = 1`, `x += 1`, `x++`), or "".
func assignedIdent(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if id, ok := e.Left.(*ast.Identifier); ok && isAssignOp(e.Op) {
			return id.Name
		}
	case *ast.UnaryExpr:
		if id, ok := e.Arg.(*ast.Identifier); ok && (e.Op == "++" || e.Op == "--") {
			return id.Name
		}
	}
	return ""
}

// paramBindingSet returns the component parameter names that the body does
// not redeclare.
func paramBindingSet(fn *ast.FnDecl) map[string]bool {
	params := make(map[string]bool)
	for _, n := range extractParamNames(fn) {
		params[n] = true
	}
	for _, n := range blockBindingNames(fn.Body) {
		delete(params, n)
	}
	return params
}

// isHookCallee reports whether a call to name is a hook or reactive primitive
// whose body-level call is collected (or deliberately dropped) by a dedicated
// pass rather than replayed verbatim.
func isHookCallee(name string) bool {
	switch name {
	case "createEffect", "createEffectDeps", "onMount", "onCleanup",
		"createMemo", "createMemoDeps", "createSignal", "createResource",
		"createReducer", "createContext":
		return true
	}
	if rest, ok := strings.CutPrefix(name, "use"); ok && rest != "" {
		return unicode.IsUpper(rune(rest[0]))
	}
	return false
}

// runtimeHooks are the `use*` hooks the runtime (and the React compatibility
// layer) provides; their body-level calls are collected or dropped by
// dedicated passes.
var runtimeHooks = map[string]bool{
	"useState": true, "useEffect": true, "useLayoutEffect": true,
	"useInsertionEffect": true, "useMemo": true, "useReducer": true,
	"useRef": true, "useCallback": true, "useId": true, "useContext": true,
	"useSyncExternalStore": true, "useTransition": true, "useDeferredValue": true,
	"useOptimistic": true, "useActionState": true, "useFormStatus": true,
	"useImperativeHandle": true, "useDebugValue": true,
}

// isCustomHookCallee reports whether name is a `use*` hook defined by the
// application rather than the runtime.
func isCustomHookCallee(name string) bool {
	if runtimeHooks[name] || strings.HasPrefix(name, "use__krate") {
		return false
	}
	rest, ok := strings.CutPrefix(name, "use")
	return ok && rest != "" && unicode.IsUpper(rune(rest[0]))
}

func isCustomHookCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Callee.(*ast.Identifier)
	return ok && isCustomHookCallee(id.Name)
}

// stmtHasReturn reports whether stmt contains a `return` that would exit the
// enclosing function (nested function bodies are not inspected).
func stmtHasReturn(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		return stmtsHaveReturn(s.Body)
	case *ast.IfStmt:
		return stmtsHaveReturn(s.Consequent) || stmtsHaveReturn(s.Alternate)
	case *ast.ForStmt:
		return stmtsHaveReturn(s.Body)
	case *ast.ForInStmt:
		return stmtsHaveReturn(s.Body)
	case *ast.WhileStmt:
		return stmtsHaveReturn(s.Body)
	case *ast.DoWhileStmt:
		return stmtsHaveReturn(s.Body)
	case *ast.SwitchStmt:
		for _, c := range s.Cases {
			if stmtsHaveReturn(c.Body) {
				return true
			}
		}
	case *ast.TryStmt:
		if stmtsHaveReturn(s.Body) || stmtsHaveReturn(s.Finally) {
			return true
		}
		return s.Catch != nil && stmtsHaveReturn(s.Catch.Body)
	case *ast.LabeledStmt:
		return stmtHasReturn(s.Body)
	}
	return false
}

func stmtsHaveReturn(stmts []ast.Stmt) bool {
	for _, s := range stmts {
		if stmtHasReturn(s) {
			return true
		}
	}
	return false
}
