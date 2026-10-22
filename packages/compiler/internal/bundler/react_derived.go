package bundler

import (
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Render-scope values of React source components.
//
// React re-runs a component body on every render, so a value computed there
// (`const total = rows.reduce(...)`, `const label = n > 1 ? "items" : "item"`)
// is always current. Krate runs the body once. In a module that imports React,
// a top-level body binding of a component or custom hook whose initializer
// reads reactive state (a state/memo getter, `props.x`, a context value, a
// custom hook result, or another such binding) is lowered to a lazily
// evaluated derived getter:
//
//	const value = Number(raw)   ->   const value = krateDerived(() => Number(raw()))
//
// and the binding joins the scope's getters, so every later read (JSX, handlers,
// effects, other derived values) becomes `value()`. A `let` built up by later
// body statements (`let cls = "a"; if (on) cls += " b";`) is folded into one
// derived computation together with those statements. Values that read
// nothing reactive are left untouched, so static code costs nothing.
//
// Custom hooks follow the same rule for their result: a hook returning a
// value that reads reactive state returns a derived getter, and the call site
// normalizes any hook result with krateHook(), so `const [on, toggle] =
// useToggle()` and `const counter = useCounter()` read the current result.
// Context reads (`useContext(Ctx)`) are derived values too: user code sees the
// provided value, never the getter that carries it.

// frameKind classifies the function a body belongs to.
type frameKind int

const (
	frameOther frameKind = iota
	frameComponent
	frameHook
)

// frameKindOf classifies a function by its name: PascalCase is a component,
// use + uppercase is a custom hook.
func frameKindOf(name string) frameKind {
	if name == "" {
		return frameOther
	}
	if isCustomHookName(name) {
		return frameHook
	}
	r, _ := utf8.DecodeRuneInString(name)
	if unicode.IsUpper(r) {
		return frameComponent
	}
	return frameOther
}

// builtinHooks are hook names provided by React or the Krate runtime; a call to
// any other use + uppercase function is a custom hook.
var builtinHooks = map[string]bool{
	"useState": true, "useEffect": true, "useLayoutEffect": true,
	"useInsertionEffect": true, "useMemo": true, "useReducer": true,
	"useRef": true, "useCallback": true, "useId": true, "useContext": true,
	"useSyncExternalStore": true, "useTransition": true, "useDeferredValue": true,
	"useOptimistic": true, "useActionState": true, "useFormStatus": true,
	"useImperativeHandle": true, "useDebugValue": true,
}

func isCustomHookName(name string) bool {
	rest, ok := cutPrefix(name, "use")
	if !ok || rest == "" || builtinHooks[name] {
		return false
	}
	if _, internal := cutPrefix(rest, "__krate"); internal {
		return false
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return unicode.IsUpper(r)
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

// noteFnName remembers the binding name of a function expression initializer
// (`const Item = (props) => ...`, `const Display = memo(function () {...})`),
// so its body is classified as a component or hook.
func (c *reactCtx) noteFnName(name string, init ast.Expr) {
	if name == "" || init == nil {
		return
	}
	if c.fnNames == nil {
		c.fnNames = map[*ast.ArrowFn]string{}
	}
	switch e := init.(type) {
	case *ast.ArrowFn:
		c.fnNames[e] = name
	case *ast.CallExpr:
		api, ok := c.callAPI(e.Callee)
		if !ok {
			if id, isID := e.Callee.(*ast.Identifier); isID {
				api, ok = id.Name, id.Name == "memo" || id.Name == "forwardRef"
			}
		}
		if ok && (api == "memo" || api == "forwardRef") && len(e.Args) >= 1 {
			c.noteFnName(name, e.Args[0])
		}
	}
}

// fnName returns the name a function expression is known by.
func (c *reactCtx) fnName(e *ast.ArrowFn) string {
	if n, ok := c.fnNames[e]; ok {
		return n
	}
	return e.Name
}

// renderFrame returns the innermost component or hook frame.
func (c *reactCtx) renderFrame() *hookFrame {
	for i := len(c.frames) - 1; i >= 0; i-- {
		if c.frames[i].kind != frameOther {
			return c.frames[i]
		}
	}
	return nil
}

// isPropsRead reports whether name is the enclosing component's whole-props
// parameter (not shadowed by a nested binding).
func (c *reactCtx) isPropsRead(sc *scope, name string) bool {
	f := c.renderFrame()
	if f == nil || f.propsName == "" || name != f.propsName {
		return false
	}
	for cur := sc; cur != nil; cur = cur.parent {
		if cur == f.sc {
			return true
		}
		if cur.shadow[name] || cur.getters[name] {
			return false
		}
	}
	return false
}

// rewriteFnBody rewrites a function body. Component and custom hook bodies in
// React modules get the render-scope lowering; any other body is rewritten
// statement by statement.
func (c *reactCtx) rewriteFnBody(body *[]ast.Stmt, sc *scope, kind frameKind, params []*ast.Param) {
	f := c.frame()
	if !c.react || kind == frameOther || f == nil {
		c.rewriteStmts(*body, sc)
		return
	}
	f.kind = kind
	if kind == frameComponent && len(params) >= 1 && plainParam(params[0]) {
		f.propsName = params[0].Name
	}
	stmts := *body
	plan := planRenderBody(stmts)
	drop := map[ast.Stmt]bool{}
	for i, stmt := range stmts {
		if g := plan.member[i]; g != nil {
			// The statement is part of a value's computation: inside the
			// derived function the value is a plain local.
			gs := newScope(sc)
			gs.declareLocal(g.name)
			r0 := c.reads
			c.rewriteStmt(stmt, gs)
			g.reads += c.reads - r0
			g.stmts = append(g.stmts, stmt)
			if i == g.last {
				c.finishRenderGroup(g, sc, drop)
			}
			continue
		}
		switch s := stmt.(type) {
		case *ast.VarStmt:
			c.rewriteRenderVarStmt(s, sc, plan)
		case *ast.ReturnStmt:
			if kind == frameHook {
				c.rewriteHookReturn(s, sc)
			} else {
				c.rewriteStmt(s, sc)
			}
		default:
			c.rewriteStmt(stmt, sc)
		}
	}
	if len(drop) > 0 {
		kept := stmts[:0:0]
		for _, s := range stmts {
			if !drop[s] {
				kept = append(kept, s)
			}
		}
		stmts = kept
	}
	*body = stmts
	f.body = stmts
}

// renderGroup is a body binding together with the later body statements that
// assign or mutate it.
type renderGroup struct {
	name  string
	stmt  *ast.VarStmt
	decl  *ast.VarDecl
	last  int
	valid bool
	// pending is set once the declaration was rewritten; reads counts the
	// reactive reads of the initializer and the member statements.
	pending bool
	reads   int
	stmts   []ast.Stmt
}

type renderPlan struct {
	groups map[*ast.VarDecl]*renderGroup
	member map[int]*renderGroup
	// closureWritten holds bindings assigned inside a nested function (an
	// event handler or effect); those keep their declared value.
	closureWritten map[string]bool
}

// planRenderBody finds, for each top-level `let`/`const` binding, the later
// top-level statements that write it, and whether they can move into the
// binding's derived computation: they may write nothing else, contain no
// hook call or return, and nothing else may read the binding while it is
// being built.
func planRenderBody(body []ast.Stmt) *renderPlan {
	plan := &renderPlan{groups: map[*ast.VarDecl]*renderGroup{}, member: map[int]*renderGroup{}, closureWritten: map[string]bool{}}
	visitStmts(body, false, func(n any, inFn bool) bool {
		if inFn {
			for name := range exprWrites(n, true) {
				plan.closureWritten[name] = true
			}
		}
		return true
	})
	for i, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d == nil || d.IsDestructuring || d.Name == "" {
				continue
			}
			g := &renderGroup{name: d.Name, stmt: vs, decl: d, last: -1, valid: true}
			members := map[int]bool{}
			for j := i + 1; j < len(body); j++ {
				switch body[j].(type) {
				case *ast.VarStmt, *ast.FnDecl, *ast.ClassDecl, *ast.ReturnStmt, *ast.ImportStmt, *ast.ExportStmt:
					continue
				}
				writes, local := stmtWrites(body[j])
				if !writes[d.Name] || local[d.Name] {
					continue
				}
				members[j] = true
				g.last = j
				for w := range writes {
					if w != d.Name && !local[w] {
						g.valid = false
					}
				}
				if stmtCallsHookOrReturns(body[j]) {
					g.valid = false
				}
			}
			if len(members) == 0 {
				continue
			}
			for j := i + 1; j < g.last; j++ {
				if !members[j] && stmtRefersTo(body[j], d.Name) {
					g.valid = false
				}
			}
			if plan.closureWritten[d.Name] {
				g.valid = false
			}
			plan.groups[d] = g
			if !g.valid {
				continue
			}
			for j := range members {
				plan.member[j] = g
			}
		}
	}
	return plan
}

// mutatingMethods are methods that modify their receiver in place.
var mutatingMethods = map[string]bool{
	"push": true, "pop": true, "shift": true, "unshift": true, "splice": true,
	"sort": true, "reverse": true, "fill": true, "copyWithin": true,
	"set": true, "add": true, "delete": true, "clear": true,
}

// stmtWrites returns the root identifiers a statement assigns or mutates
// outside nested functions, and the names the statement declares itself (loop
// variables, block locals, catch parameters).
func stmtWrites(stmt ast.Stmt) (writes, local map[string]bool) {
	writes, local = map[string]bool{}, map[string]bool{}
	visitStmt(stmt, false, func(n any, inFn bool) bool {
		if inFn {
			return false
		}
		switch s := n.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				for _, name := range declBindingNames(d) {
					local[name] = true
				}
			}
		case *ast.ForInStmt:
			if s.Keyword != "" {
				for _, name := range exprBindingNames(s.Left) {
					local[name] = true
				}
			} else {
				for _, name := range exprBindingNames(s.Left) {
					writes[name] = true
				}
			}
		case *ast.TryStmt:
			if s.Catch != nil && s.Catch.Param != "" {
				if s.Catch.ParamTarget != nil {
					for _, name := range exprBindingNames(s.Catch.ParamTarget) {
						local[name] = true
					}
				} else {
					local[s.Catch.Param] = true
				}
			}
		}
		for name := range exprWrites(n, false) {
			writes[name] = true
		}
		return true
	})
	return writes, local
}

// exprWrites returns the names one node writes: assignment and update
// targets, plus the receiver of a mutating method call or member assignment.
// With rebindOnly set only rebinding writes count (used for closures, where
// mutating an object does not stale a binding).
func exprWrites(n any, rebindOnly bool) map[string]bool {
	out := map[string]bool{}
	switch e := n.(type) {
	case *ast.BinaryExpr:
		if !assignOps[e.Op] {
			break
		}
		switch t := unparen(e.Left).(type) {
		case *ast.Identifier:
			out[t.Name] = true
		case *ast.MemberExpr:
			if !rebindOnly {
				if root := rootIdent(t); root != "" {
					out[root] = true
				}
			}
		case *ast.ArrayExpr, *ast.ObjectExpr:
			for _, name := range exprBindingNames(t) {
				out[name] = true
			}
		}
	case *ast.UnaryExpr:
		if e.Op != "++" && e.Op != "--" {
			break
		}
		switch t := unparen(e.Arg).(type) {
		case *ast.Identifier:
			out[t.Name] = true
		case *ast.MemberExpr:
			if !rebindOnly {
				if root := rootIdent(t); root != "" {
					out[root] = true
				}
			}
		}
	case *ast.CallExpr:
		if rebindOnly {
			break
		}
		if m, ok := e.Callee.(*ast.MemberExpr); ok && !m.Computed {
			if p, ok := m.Property.(*ast.Identifier); ok && mutatingMethods[p.Name] {
				if root := rootIdent(m.Object); root != "" {
					out[root] = true
				}
			}
		}
	}
	return out
}

var assignOps = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "**=": true,
	"<<=": true, ">>=": true, ">>>=": true, "&=": true, "|=": true, "^=": true,
	"&&=": true, "||=": true, "??=": true,
}

func unparen(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.Expr
		case *ast.TypeAssertion:
			e = x.Expr
		default:
			return e
		}
	}
}

// rootIdent returns the identifier a member chain starts from (`a` in a.b[c]).
func rootIdent(e ast.Expr) string {
	for {
		switch x := unparen(e).(type) {
		case *ast.Identifier:
			return x.Name
		case *ast.MemberExpr:
			e = x.Object
		default:
			return ""
		}
	}
}

// exprBindingNames returns the identifiers of a pattern expression.
func exprBindingNames(e ast.Expr) []string {
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
	walk(e)
	return out
}

// declBindingNames returns every name a declarator binds, including rest
// elements of a destructuring pattern.
func declBindingNames(d *ast.VarDecl) []string {
	if d == nil {
		return nil
	}
	if !d.IsDestructuring {
		if d.Name == "" {
			return nil
		}
		return []string{d.Name}
	}
	if d.Target != nil {
		return exprBindingNames(d.Target)
	}
	names := append([]string(nil), d.Names...)
	if d.RestName != "" {
		names = append(names, d.RestName)
	}
	return names
}

// stmtCallsHookOrReturns reports a statement that cannot move into a derived
// computation: it calls a hook or returns from the component.
func stmtCallsHookOrReturns(stmt ast.Stmt) bool {
	found := false
	visitStmt(stmt, false, func(n any, inFn bool) bool {
		if found || inFn {
			return false
		}
		switch x := n.(type) {
		case *ast.ReturnStmt:
			found = true
		case *ast.CallExpr:
			if id, ok := x.Callee.(*ast.Identifier); ok {
				if builtinHooks[id.Name] || isCustomHookName(id.Name) {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// stmtRefersTo reports whether a statement mentions name anywhere, nested
// functions included.
func stmtRefersTo(stmt ast.Stmt, name string) bool {
	found := false
	visitStmt(stmt, false, func(n any, _ bool) bool {
		if id, ok := n.(*ast.Identifier); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// derivableInit reports whether an initializer may be recomputed lazily: it is
// not itself a function or class, does not suspend, and does not write
// state or call a hook (those must run exactly once).
func (c *reactCtx) derivableInit(init ast.Expr) bool {
	switch unparen(init).(type) {
	case *ast.ArrowFn, *ast.ClassExpr, *ast.JSXElement, *ast.JSXFragment:
		return false
	}
	ok := true
	visitExpr(init, false, func(n any, inFn bool) bool {
		if !ok {
			return false
		}
		switch x := n.(type) {
		case *ast.AwaitExpr:
			if !inFn {
				ok = false
			}
		case *ast.CallExpr:
			id, isID := x.Callee.(*ast.Identifier)
			if !isID || inFn {
				break
			}
			if c.setters[id.Name] || isCustomHookName(id.Name) || builtinHooks[id.Name] {
				ok = false
			}
			switch id.Name {
			case "createSignal", "createEffect", "createEffectDeps", "createMemo", "createMemoDeps",
				"createCallbackDeps", "createReducer", "createResource", "onMount", "onCleanup":
				ok = false
			}
		}
		return ok
	})
	return ok
}

// derivedCall wraps expr in krateDerived(() => expr).
func derivedCall(expr ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{
		Position: expr.Pos(),
		Callee:   &ast.Identifier{Position: expr.Pos(), Name: "krateDerived"},
		Args: []ast.Expr{&ast.ArrowFn{
			Position:   expr.Pos(),
			Expression: true,
			Body:       []ast.Stmt{&ast.ReturnStmt{Position: expr.Pos(), Value: expr}},
		}},
	}
}

// tempName returns a fresh module-unique binding name.
func (c *reactCtx) tempName() string {
	c.tmp++
	return "__krd" + strconv.Itoa(c.tmp)
}

// declareDerived makes name a derived getter in sc. Calling it in React
// source (`toggle()`) invokes the value, so calls lower to `toggle()()`.
func (c *reactCtx) declareDerived(sc *scope, name string) {
	sc.declareGetter(name)
	c.markCallable(sc, name, "derived")
}

// rewriteRenderVarStmt rewrites a top-level declaration of a component or
// hook body, lowering render-dependent bindings to derived getters.
func (c *reactCtx) rewriteRenderVarStmt(v *ast.VarStmt, sc *scope, plan *renderPlan) {
	var out []*ast.VarDecl
	for _, d := range v.Decls {
		if d == nil {
			continue
		}
		out = append(out, c.rewriteRenderDecl(v, d, sc, plan)...)
	}
	v.Decls = out
}

func (c *reactCtx) rewriteRenderDecl(v *ast.VarStmt, d *ast.VarDecl, sc *scope, plan *renderPlan) []*ast.VarDecl {
	one := &ast.VarStmt{Position: v.Position, Kind: v.Kind, Decls: []*ast.VarDecl{d}}
	if d.Init == nil {
		c.rewriteVarStmt(one, sc)
		if g := plan.groups[d]; g != nil && g.valid {
			g.pending = true
		}
		return one.Decls
	}
	if call, ok := d.Init.(*ast.CallExpr); ok {
		if api, isAPI := c.callAPI(call.Callee); isAPI && api == "useContext" {
			// The provided value, recomputed when the provider's value changes.
			d.Init = c.rewriteExpr(d.Init, sc)
			return c.bindDerived(v, d, derivedCall(d.Init), sc)
		}
		if id, isID := call.Callee.(*ast.Identifier); isID && isCustomHookName(id.Name) {
			if _, isAPI := c.localAPI(id.Name); !isAPI {
				d.Init = c.rewriteExpr(d.Init, sc)
				wrapped := &ast.CallExpr{Position: call.Position, Callee: &ast.Identifier{Position: call.Position, Name: "krateHook"}, Args: []ast.Expr{d.Init}}
				return c.bindDerived(v, d, wrapped, sc)
			}
		}
	}
	if c.isReactiveFactory(d.Init) || c.isHookInit(d.Init) {
		c.rewriteVarStmt(one, sc)
		return one.Decls
	}
	r0 := c.reads
	c.rewriteVarStmt(one, sc)
	reads := c.reads - r0
	if len(one.Decls) != 1 || one.Decls[0] != d {
		return one.Decls
	}
	if g := plan.groups[d]; g != nil {
		if g.valid {
			g.pending = true
			g.reads = reads
		} else if reads > 0 {
			c.warnStale(v, d.Name, "is also written by a body statement that writes other variables, or read while it is being built")
		}
		return one.Decls
	}
	if reads == 0 || !c.derivableInit(d.Init) {
		return one.Decls
	}
	for _, name := range declBindingNames(d) {
		if plan.closureWritten[name] {
			c.warnStale(v, name, "is reassigned inside a function")
			return one.Decls
		}
	}
	return c.bindDerived(v, d, derivedCall(d.Init), sc)
}

// isHookInit reports a React hook call that owns its binding (useRef,
// useCallback, useId, ...): those run once and are never derived.
func (c *reactCtx) isHookInit(init ast.Expr) bool {
	call, ok := init.(*ast.CallExpr)
	if !ok {
		return false
	}
	if _, ok := c.callAPI(call.Callee); ok {
		return true
	}
	if id, ok := call.Callee.(*ast.Identifier); ok {
		return builtinHooks[id.Name]
	}
	return false
}

// bindDerived binds d to the getter expression getter: a plain binding
// becomes the getter itself; a destructuring pattern reads its members
// through a temporary getter, one derived binding per name.
func (c *reactCtx) bindDerived(v *ast.VarStmt, d *ast.VarDecl, getter *ast.CallExpr, sc *scope) []*ast.VarDecl {
	if !d.IsDestructuring {
		d.Init = getter
		c.declareDerived(sc, d.Name)
		return []*ast.VarDecl{d}
	}
	tmp := c.tempName()
	sc.declareLocal(tmp)
	out := []*ast.VarDecl{{Name: tmp, Init: getter}}
	for _, name := range declBindingNames(d) {
		inner := &ast.VarStmt{Position: v.Position, Kind: ast.VarConst, Decls: []*ast.VarDecl{{
			Target:          d.Target,
			Names:           d.Names,
			IsDestructuring: true,
			Pattern:         d.Pattern,
			RestName:        d.RestName,
			Init:            &ast.CallExpr{Callee: &ast.Identifier{Name: tmp}},
		}}}
		fn := &ast.ArrowFn{Position: v.Position, Body: []ast.Stmt{inner, &ast.ReturnStmt{Value: &ast.Identifier{Name: name}}}}
		out = append(out, &ast.VarDecl{Name: name, Init: &ast.CallExpr{Callee: &ast.Identifier{Name: "krateDerived"}, Args: []ast.Expr{fn}}})
		c.declareDerived(sc, name)
	}
	return out
}

// finishRenderGroup lowers a binding and the statements that build it into
// one derived computation once its last statement was rewritten.
func (c *reactCtx) finishRenderGroup(g *renderGroup, sc *scope, drop map[ast.Stmt]bool) {
	if !g.pending {
		return
	}
	if g.reads == 0 || (g.decl.Init != nil && !c.derivableInit(g.decl.Init)) {
		return
	}
	for _, s := range g.stmts {
		drop[s] = true
	}
	local := &ast.VarStmt{Position: g.stmt.Position, Kind: ast.VarLet, Decls: []*ast.VarDecl{{Name: g.name, Init: g.decl.Init}}}
	body := append([]ast.Stmt{local}, g.stmts...)
	body = append(body, &ast.ReturnStmt{Value: &ast.Identifier{Name: g.name}})
	g.decl.Init = &ast.CallExpr{
		Position: g.stmt.Position,
		Callee:   &ast.Identifier{Position: g.stmt.Position, Name: "krateDerived"},
		Args:     []ast.Expr{&ast.ArrowFn{Position: g.stmt.Position, Body: body}},
	}
	c.declareDerived(sc, g.name)
}

// rewriteHookReturn makes a custom hook's top-level return value a derived
// getter when it reads reactive state, so the caller sees later values.
func (c *reactCtx) rewriteHookReturn(s *ast.ReturnStmt, sc *scope) {
	if s.Value == nil {
		return
	}
	r0 := c.reads
	s.Value = c.rewriteExpr(s.Value, sc)
	if c.reads > r0 && c.derivableInit(s.Value) {
		s.Value = derivedCall(s.Value)
	}
}

func (c *reactCtx) warnStale(v *ast.VarStmt, name, why string) {
	c.warn(v.Position, diag.CodeStaleRenderValue,
		"`"+name+"` is computed from state or props but "+why+"; it keeps its first value after hydration",
		"compute it in one expression, e.g. const "+name+" = cond ? a : b, or keep it in state")
}
