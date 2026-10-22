package bundler

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// React effect, memo and callback lowering.
//
// useEffect/useLayoutEffect/useInsertionEffect(fn, deps) lower to
// createEffectDeps(fn, () => deps[, phase]). The runtime re-reads the deps
// thunk whenever a signal it reads changes and re-runs fn in the next commit
// when an element changed by Object.is (see packages/runtime/src/effects.ts).
// useMemo(fn, deps) lowers to createMemoDeps(fn, () => deps) and
// useCallback(fn, deps) to createCallbackDeps(fn, () => deps); both bind a
// getter whose value is recomputed synchronously when the deps change.

// effectPhases maps an effect hook to the runtime phase argument ("" means
// the default passive phase).
var effectPhases = map[string]string{
	"useEffect":          "",
	"useLayoutEffect":    "layout",
	"useInsertionEffect": "insertion",
}

// stateFactories are the lowered calls whose first binding is component state
// (a re-render trigger in React).
var stateFactories = map[string]bool{
	"createSignal":   true,
	"createReducer":  true,
	"useActionState": true,
	"useOptimistic":  true,
}

// hookFrame is a function body being rewritten (a component or custom hook).
type hookFrame struct {
	sc     *scope
	params []*ast.Param
	body   []ast.Stmt
	// kind and propsName are set for component and custom hook bodies in
	// React modules (react_derived.go); propsName is a component's
	// whole-props parameter.
	kind      frameKind
	propsName string
	// noDeps are synthesized dependency arrays of effects without a
	// dependency array, filled once the whole body has been rewritten.
	noDeps []*ast.ArrayExpr
	// guards is the stack of conditions and loops enclosing the statement
	// being rewritten, within this function.
	guards []string
}

// guarded rewrites fn's statements as being inside a condition or loop.
func (c *reactCtx) guarded(kind string, fn func()) {
	f := c.frame()
	if f == nil {
		fn()
		return
	}
	f.guards = append(f.guards, kind)
	defer func() { f.guards = f.guards[:len(f.guards)-1] }()
	fn()
}

// checkHookPlacement reports a React hook called inside a condition or loop
// (the Rules of Hooks). React's own use() may be called conditionally.
func (c *reactCtx) checkHookPlacement(e *ast.CallExpr, api string) {
	if api == "use" || !strings.HasPrefix(api, "use") {
		return
	}
	f := c.frame()
	if f == nil || len(f.guards) == 0 {
		return
	}
	where := "a condition"
	for _, g := range f.guards {
		if g == "loop" {
			where = "a loop"
		}
	}
	c.warn(e.Position, diag.CodeHookPlacement,
		api+" is called inside "+where+"; React requires hooks to run in the same order on every render",
		"call "+api+" unconditionally at the top level of the component and branch on its result, "+
			"or move the repeated part into a child component that calls the hook")
}

// lowerStateHook prepares useState(init) for createSignal: a lazy
// initializer (an inline function or a binding holding one) is called once,
// and the signal gets React state semantics (krateHookState).
func (c *reactCtx) lowerStateHook(e *ast.CallExpr) {
	if len(e.Args) == 0 {
		e.Args = []ast.Expr{&ast.Identifier{Name: "undefined"}}
	}
	e.Args[0] = c.lazyInit(e.Args[0])
	e.Args = append(e.Args[:1], &ast.Identifier{Name: "krateHookState"})
}

// lowerReducerHook lowers useReducer(reducer, arg, init) to
// createReducer(reducer, init(arg), krateHookState).
func (c *reactCtx) lowerReducerHook(e *ast.CallExpr) {
	if len(e.Args) < 2 {
		return
	}
	initial := e.Args[1]
	if len(e.Args) >= 3 && !isNoDeps(e.Args[2]) {
		initial = &ast.CallExpr{Position: e.Args[2].Pos(), Callee: e.Args[2], Args: []ast.Expr{e.Args[1]}}
	}
	e.Args = []ast.Expr{e.Args[0], initial, &ast.Identifier{Name: "krateHookState"}}
}

// lazyInit calls a function-valued useState initializer.
func (c *reactCtx) lazyInit(arg ast.Expr) ast.Expr {
	switch x := arg.(type) {
	case *ast.ArrowFn:
		if len(x.Params) == 0 && !x.Async && !x.Generator {
			return &ast.CallExpr{Position: x.Position, Callee: x}
		}
	case *ast.Identifier:
		if c.fnNames[x.Name] {
			return &ast.CallExpr{Position: x.Position, Callee: x}
		}
	}
	return arg
}

// functionNames collects bindings that hold functions anywhere in body:
// function declarations and const bindings initialized with a function.
func functionNames(body []ast.Stmt) map[string]bool {
	out := map[string]bool{}
	var walk func([]ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.FnDecl:
				if s.Name != "" {
					out[s.Name] = true
				}
				walk(s.Body)
			case *ast.ExportStmt:
				if s.Declaration != nil {
					walk([]ast.Stmt{s.Declaration})
				}
			case *ast.VarStmt:
				if s.Kind != ast.VarConst {
					continue
				}
				for _, d := range s.Decls {
					if d == nil || d.IsDestructuring || d.Name == "" {
						continue
					}
					if fn, ok := d.Init.(*ast.ArrowFn); ok {
						out[d.Name] = true
						walk(fn.Body)
					}
				}
			case *ast.BlockStmt:
				walk(s.Body)
			}
		}
	}
	walk(body)
	return out
}

// recordFresh remembers a local whose value is a new object or function on
// every React render (an inline arrow, object or array literal, or a nested
// function declaration), so a dependency array naming it re-runs its hook
// after every render.
func (c *reactCtx) recordFresh(sc *scope, name string) {
	if c.freshLocals == nil {
		c.freshLocals = map[*scope]map[string]bool{}
	}
	m := c.freshLocals[sc]
	if m == nil {
		m = map[string]bool{}
		c.freshLocals[sc] = m
	}
	m[name] = true
}

func (c *reactCtx) isFreshLocal(sc *scope, name string) bool {
	for cur := sc; cur != nil; cur = cur.parent {
		if c.freshLocals[cur][name] {
			return true
		}
		if cur.getters[name] || cur.shadow[name] {
			return false
		}
	}
	return false
}

// isFreshValue reports expressions that create a new identity each time
// they are evaluated.
func isFreshValue(e ast.Expr) bool {
	switch e.(type) {
	case *ast.ArrowFn, *ast.ObjectExpr, *ast.ArrayExpr:
		return true
	}
	return false
}

// freshDeps replaces dependency elements that change identity on every
// React render with the component's render triggers, so the hook re-runs
// after every render, as React's comparison would.
func (c *reactCtx) freshDeps(arr *ast.ArrayExpr, sc *scope) {
	f := c.frame()
	if f == nil {
		return
	}
	for i, el := range arr.Elements {
		fresh := isFreshValue(el)
		if id, ok := el.(*ast.Identifier); ok && c.isFreshLocal(sc, id.Name) {
			fresh = true
		}
		if !fresh {
			continue
		}
		triggers := &ast.ArrayExpr{Position: el.Pos()}
		f.noDeps = append(f.noDeps, triggers)
		arr.Elements[i] = &ast.UnaryExpr{Op: "...", Arg: triggers}
	}
}

// enterFn pushes a function frame; the returned func pops it and fills the
// synthesized dependency arrays of its no-deps effects.
func (c *reactCtx) enterFn(sc *scope, params []*ast.Param, body []ast.Stmt) func() {
	f := &hookFrame{sc: sc, params: params, body: body}
	c.frames = append(c.frames, f)
	return func() {
		c.frames = c.frames[:len(c.frames)-1]
		if len(f.noDeps) == 0 {
			return
		}
		elems := renderTriggers(f)
		for _, arr := range f.noDeps {
			arr.Elements = append([]ast.Expr(nil), elems...)
		}
	}
}

func (c *reactCtx) frame() *hookFrame {
	if len(c.frames) == 0 {
		return nil
	}
	return c.frames[len(c.frames)-1]
}

// renderTriggers lists what makes React re-render the frame's component: its
// own state getters (called, so the deps thunk subscribes to them) and its
// props (read live through krateDepsOf, which also accepts a plain value).
func renderTriggers(f *hookFrame) []ast.Expr {
	var out []ast.Expr
	for _, stmt := range f.body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl == nil || !decl.IsDestructuring || len(decl.Names) == 0 {
				continue
			}
			call, ok := decl.Init.(*ast.CallExpr)
			if !ok {
				continue
			}
			id, ok := call.Callee.(*ast.Identifier)
			if !ok || !stateFactories[id.Name] {
				continue
			}
			name := decl.Names[0]
			if name == "" || !f.sc.getters[name] {
				continue
			}
			out = append(out, &ast.CallExpr{Callee: &ast.Identifier{Name: name}})
		}
	}
	for _, p := range f.params {
		if p == nil || p.IsRest {
			continue
		}
		if p.Target != nil {
			for _, n := range patternNames(p.Target) {
				out = append(out, &ast.Identifier{Name: n})
			}
			continue
		}
		if p.Name != "" && p.Name != "{...}" {
			out = append(out, &ast.UnaryExpr{Op: "...", Arg: &ast.CallExpr{
				Callee: &ast.Identifier{Name: "krateDepsOf"},
				Args:   []ast.Expr{&ast.Identifier{Name: p.Name}},
			}})
		}
	}
	return out
}

// patternNames returns the names bound by a destructuring pattern, skipping
// rest elements (a fresh object or array that never compares equal).
func patternNames(e ast.Expr) []string {
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
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				if p == nil || p.Spread {
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
				if u, ok := el.(*ast.UnaryExpr); ok && u.Op == "..." {
					continue
				}
				walk(el)
			}
		}
	}
	walk(e)
	return out
}

// warn records a coded warning at pos.
func (c *reactCtx) warn(pos ast.Pos, code diag.Code, msg, hint string) {
	d := diag.New(c.file, pos.Line, pos.Col, msg, hint, sourceLineOf(c.src, pos.Line)).WithCode(code)
	d.Severity = diag.SeverityWarning
	c.diags = append(c.diags, d)
}

func sourceLineOf(src string, line int) string {
	if src == "" || line <= 0 {
		return ""
	}
	lines := strings.SplitN(src, "\n", line+1)
	if len(lines) < line {
		return ""
	}
	return strings.TrimRight(lines[line-1], "\r")
}

// recordArrayConst remembers `const name = [ ... ]` so a dependency argument
// naming it can be inlined.
func (c *reactCtx) recordArrayConst(sc *scope, name string, arr *ast.ArrayExpr) {
	if c.arrayConsts == nil {
		c.arrayConsts = map[*scope]map[string]*ast.ArrayExpr{}
	}
	m := c.arrayConsts[sc]
	if m == nil {
		m = map[string]*ast.ArrayExpr{}
		c.arrayConsts[sc] = m
	}
	m[name] = arr
}

// lookupArrayConst resolves name to a recorded array literal, honouring
// shadowing by inner declarations.
func (c *reactCtx) lookupArrayConst(sc *scope, name string) *ast.ArrayExpr {
	for cur := sc; cur != nil; cur = cur.parent {
		if arr := c.arrayConsts[cur][name]; arr != nil {
			return arr
		}
		if cur.getters[name] || cur.shadow[name] {
			return nil
		}
	}
	return nil
}

// markCallable records that the getter name may hold a function value, so
// calling it invokes that value: `name(args)` lowers to `name()(args)`. For a
// useCallback getter (always a function) a zero-argument call is invoked too;
// for a useMemo getter `name()` stays a plain read, since Krate-style code
// reads memo values that way.
func (c *reactCtx) markCallable(sc *scope, name, api string) {
	if c.callables == nil {
		c.callables = map[*scope]map[string]string{}
	}
	m := c.callables[sc]
	if m == nil {
		m = map[string]string{}
		c.callables[sc] = m
	}
	m[name] = api
}

// callableGetterCall reports whether call invokes the value of a useMemo or
// useCallback getter rather than reading it.
func (c *reactCtx) callableGetterCall(sc *scope, name string, call *ast.CallExpr) bool {
	for cur := sc; cur != nil; cur = cur.parent {
		if cur.getters[name] {
			switch c.callables[cur][name] {
			case "useCallback":
				return true
			case "useMemo":
				return len(call.Args) > 0
			case "derived":
				// A derived render value or hook result: React source calling
				// it invokes the value.
				return true
			}
			return false
		}
		if cur.shadow[name] {
			return false
		}
	}
	return false
}

// hasDepsArg reports whether a hook call passes a usable dependency argument.
func hasDepsArg(e *ast.CallExpr) bool {
	if len(e.Args) < 2 || isNoDeps(e.Args[1]) {
		return false
	}
	return !isNonArrayLiteral(e.Args[1])
}

// isNoDeps reports a dependency argument React treats as omitted
// (undefined or null: the hook runs after every render).
func isNoDeps(e ast.Expr) bool {
	switch x := e.(type) {
	case nil:
		return true
	case *ast.Identifier:
		return x.Name == "undefined" || x.Name == "null"
	case *ast.Literal:
		return x.Kind == ast.NullLit
	}
	return false
}

// isNonArrayLiteral reports dependency arguments that can never be an array.
func isNonArrayLiteral(e ast.Expr) bool {
	if isNoDeps(e) {
		return false
	}
	switch e.(type) {
	case *ast.Literal, *ast.ObjectExpr, *ast.ArrowFn, *ast.TemplateExpr:
		return true
	}
	return false
}

// depsThunk builds `() => deps` for a hook's dependency argument. It inlines
// a local const array literal; any other expression is wrapped as is and
// compared at run time. ok is false when the call has no usable dependency
// argument.
func (c *reactCtx) depsThunk(e *ast.CallExpr, api string, sc *scope) (ast.Expr, bool) {
	if len(e.Args) >= 2 && isNonArrayLiteral(e.Args[1]) {
		c.warn(e.Args[1].Pos(), diag.CodeDepsNotArray,
			api+" dependency argument is not an array; it is ignored",
			"pass an array such as [a, b], or omit the argument")
		return nil, false
	}
	if !hasDepsArg(e) {
		return nil, false
	}
	arg := e.Args[1]
	var deps ast.Expr
	switch x := arg.(type) {
	case *ast.ArrayExpr:
		c.freshDeps(x, sc)
		deps = c.rewriteExpr(x, sc)
	case *ast.Identifier:
		if arr := c.lookupArrayConst(sc, x.Name); arr != nil {
			deps = &ast.ArrayExpr{Position: arr.Position, Elements: append([]ast.Expr(nil), arr.Elements...)}
			break
		}
		c.warn(x.Position, diag.CodeDepsUnresolved,
			api+" dependency array `"+x.Name+"` is not an array literal in this component; it is compared at run time but a captured array never changes",
			"write the dependencies inline, e.g. "+api+"(fn, [a, b]), or declare `const "+x.Name+" = [a, b]` in the component body")
		deps = c.rewriteExpr(x, sc)
	default:
		// Member reads, conditionals, calls: evaluated inside the thunk, so
		// signal reads stay live and the runtime compares the result.
		deps = c.rewriteExpr(arg, sc)
	}
	return &ast.ArrowFn{Position: e.Position, Body: []ast.Stmt{&ast.ReturnStmt{Value: deps}}}, true
}

// lowerEffectHook lowers a React effect hook call.
func (c *reactCtx) lowerEffectHook(e *ast.CallExpr, api string, sc *scope) ast.Expr {
	if len(e.Args) == 0 {
		return &ast.Identifier{Position: e.Position, Name: "undefined"}
	}
	if arrow, ok := e.Args[0].(*ast.ArrowFn); ok && arrow.Async {
		c.warn(e.Args[0].Pos(), diag.CodeAsyncEffect,
			api+" callback is async; the Promise it returns is not a cleanup function",
			"call an async function inside the effect instead: "+api+"(() => { load(); return cleanup; }, deps)")
	}
	fn := c.rewriteExpr(e.Args[0], sc)
	thunk, ok := c.depsThunk(e, api, sc)
	if !ok {
		arr := &ast.ArrayExpr{Position: e.Position}
		if f := c.frame(); f != nil {
			f.noDeps = append(f.noDeps, arr)
		}
		c.warn(e.Position, diag.CodeEffectNoDeps,
			api+" has no dependency array; it re-runs after commits that change this component's state or props",
			"pass the dependencies explicitly, e.g. "+api+"(fn, [a, b]), or [] to run once after mount")
		thunk = &ast.ArrowFn{Position: e.Position, Body: []ast.Stmt{&ast.ReturnStmt{Value: arr}}}
	}
	args := []ast.Expr{fn, thunk}
	if phase := effectPhases[api]; phase != "" {
		args = append(args, &ast.Literal{Kind: ast.StringLit, Value: phase})
	}
	return &ast.CallExpr{Position: e.Position, Callee: &ast.Identifier{Name: "createEffectDeps"}, Args: args}
}

// lowerMemoHook lowers useMemo; ok is false when it has no dependency array
// (the caller then falls back to auto-tracked createMemo).
func (c *reactCtx) lowerMemoHook(e *ast.CallExpr, sc *scope) (ast.Expr, bool) {
	thunk, ok := c.depsThunk(e, "useMemo", sc)
	if !ok {
		if len(e.Args) == 1 {
			c.warn(e.Position, diag.CodeMemoNoDeps,
				"useMemo has no dependency array; it recomputes when a signal it reads changes",
				"pass the dependencies explicitly, e.g. useMemo(fn, [a, b])")
		}
		if len(e.Args) > 1 {
			e.Args = e.Args[:1]
		}
		return nil, false
	}
	fn := c.rewriteExpr(e.Args[0], sc)
	return &ast.CallExpr{Position: e.Position, Callee: &ast.Identifier{Name: "createMemoDeps"}, Args: []ast.Expr{fn, thunk}}, true
}

// lowerCallbackHook lowers useCallback. With a dependency array it becomes a
// getter whose identity changes only when the deps change; without one the
// function itself (one identity for the component's lifetime).
func (c *reactCtx) lowerCallbackHook(e *ast.CallExpr, sc *scope) ast.Expr {
	if len(e.Args) == 0 {
		return &ast.Identifier{Position: e.Position, Name: "undefined"}
	}
	thunk, ok := c.depsThunk(e, "useCallback", sc)
	if !ok {
		c.warn(e.Position, diag.CodeCallbackNoDeps,
			"useCallback has no dependency array; the callback keeps one identity for the component's lifetime",
			"pass the dependencies the callback reads, e.g. useCallback(fn, [a, b])")
		return c.rewriteExpr(e.Args[0], sc)
	}
	fn := c.rewriteExpr(e.Args[0], sc)
	return &ast.CallExpr{Position: e.Position, Callee: &ast.Identifier{Name: "createCallbackDeps"}, Args: []ast.Expr{fn, thunk}}
}

// isDepsGetterHook reports whether call is useMemo/useCallback with a
// dependency array, whose binding is a callable getter.
func (c *reactCtx) isDepsGetterHook(call *ast.CallExpr) (string, bool) {
	api, ok := c.callAPI(call.Callee)
	if !ok || (api != "useMemo" && api != "useCallback") {
		return "", false
	}
	return api, hasDepsArg(call)
}
