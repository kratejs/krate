package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
	"github.com/kratejs/krate/packages/compiler/internal/sigutil"
)

// State hooks.
//
// A component can get its state from a project function instead of creating
// it in its own body:
//
//	export function useToggle(initial = false) {
//	  const [on, setOn] = createSignal(initial);
//	  return { on, toggle: () => setOn(!on()) };
//	}
//	const { on, toggle } = useToggle();
//
// Such a function (same module or imported, transitively) is a state hook.
// A component that calls one cannot be rendered once at build time, so it
// takes the client path: the call is replayed in the hydration scope (the
// hook and the helpers it uses are declared there), the getters the call
// returns are reactive in the signal table, and their initial value in the
// server HTML is the hook's own initial value when it can be computed.

// stateCreators are the runtime calls that give a function state of its own.
var stateCreators = map[string]bool{
	"createSignal": true, "createMemo": true, "createEffect": true,
	"createEffectDeps": true, "createMemoDeps": true, "createResource": true,
	"createReducer": true, "createDeferredValue": true, "createExternalStore": true,
	"createEffectEvent": true, "onMount": true, "onCleanup": true,
}

// fnCreatesState reports whether fn creates reactive state in its own body or
// calls a project function that does. Nested function bodies run later and do
// not count.
func (b *builder) fnCreatesState(fn *ast.FnDecl) bool {
	if fn == nil || fn.Async {
		return false
	}
	if v, ok := b.stateFns[fn]; ok {
		return v
	}
	if b.stateFns == nil {
		b.stateFns = make(map[*ast.FnDecl]bool)
	}
	// A recursive call cannot add state the first pass has not seen.
	b.stateFns[fn] = false
	found := false
	for _, stmt := range fn.Body {
		WalkAST(stmt, func(n any) bool {
			if found {
				return false
			}
			switch c := n.(type) {
			case *ast.ArrowFn, *ast.FnDecl:
				return false
			case *ast.CallExpr:
				if id, ok := c.Callee.(*ast.Identifier); ok {
					if stateCreators[id.Name] {
						found = true
						return false
					}
					if callee := b.functions[id.Name]; callee != nil && callee != fn && !isComponentName(id.Name) && b.fnCreatesState(callee) {
						found = true
						return false
					}
				}
			}
			return true
		})
		if found {
			break
		}
	}
	b.stateFns[fn] = found
	return found
}

// inUseClient reports whether the component called name lives in a "use
// client" module, or is rendered by one: the whole client module graph runs
// in the browser, so such a component takes the client path and whatever the
// build cannot evaluate is bound there. A fully folded one still ships no
// JavaScript (finishClientComponent promotes it to the static tier).
func (b *builder) inUseClient(name string) bool {
	return b.useClientDepth > 0 || b.ann.UseClient[name]
}

// stateHookCall returns the call and function of expr when it calls a state
// hook.
func (b *builder) stateHookCall(expr ast.Expr) (*ast.CallExpr, *ast.FnDecl) {
	call, ok := unparen(expr).(*ast.CallExpr)
	if !ok {
		return nil, nil
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok || isComponentName(id.Name) || stateCreators[id.Name] {
		return nil, nil
	}
	fn := b.functions[id.Name]
	if fn == nil || !b.fnCreatesState(fn) {
		return nil, nil
	}
	return call, fn
}

// callsStateHook reports whether a component body calls a state hook at its
// top level.
func (b *builder) callsStateHook(body []ast.Stmt) bool {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d != nil && d.Init != nil {
					init := d.Init
					// A React-source component's hook call is wrapped as
					// krateHook(useThing()) by the bundler.
					if derivedCallee(init) == "krateHook" {
						init = init.(*ast.CallExpr).Args[0]
					}
					if call, _ := b.stateHookCall(init); call != nil {
						return true
					}
				}
			}
		case *ast.ExprStmt:
			if call, _ := b.stateHookCall(s.Expression); call != nil {
				return true
			}
		}
	}
	return false
}

// hookVal describes one value a hook call returns.
type hookVal struct {
	getter bool          // a reactive getter the caller reads by calling it
	init   jsvalue.Value // the getter's value before any update
	fields map[string]hookVal
	elems  []hookVal
	known  bool // the shape of the value is known
}

// hookResult analyses a call to a state hook: which of the values it returns
// are reactive getters, and their initial values. Arguments are evaluated in
// the caller's scope. depth bounds hook-calls-hook chains.
func (b *builder) hookResult(call *ast.CallExpr, fn *ast.FnDecl, caller valueMap, depth int) hookVal {
	if depth > 6 {
		return hookVal{}
	}
	scope := make(valueMap)
	for i, p := range fn.Params {
		if p == nil || p.Name == "" || p.Pattern != "" {
			continue
		}
		switch {
		case i < len(call.Args) && call.Args[i] != nil:
			scope[p.Name] = evalConstValue(call.Args[i], b.sigMap(), caller)
		case p.Default != nil:
			scope[p.Name] = evalConstValue(p.Default, nil, scope)
		default:
			scope[p.Name] = jsvalue.Undefined()
		}
	}
	vals := map[string]hookVal{}
	setters := map[string]bool{}
	for _, d := range sigutil.Find(fn.Body, false) {
		if d.CSSKind != sigutil.CSSKindNone {
			continue
		}
		v := hookVal{getter: true, known: true, init: jsvalue.Dynamic("hook state")}
		if d.Initial != nil && !d.IsResource {
			v.init = evalConstValue(d.Initial, nil, scope)
		}
		vals[d.Name] = v
		if d.Setter != "" {
			setters[d.Setter] = true
		}
	}
	for name := range b.collectNamedMemos(fn.Body) {
		vals[name] = hookVal{getter: true, known: true, init: jsvalue.Dynamic("hook memo")}
	}
	// Results of hooks this hook calls.
	for _, stmt := range fn.Body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d == nil || d.Init == nil {
				continue
			}
			inner, innerFn := b.stateHookCall(d.Init)
			if inner == nil || innerFn == fn {
				continue
			}
			bindHookPattern(d, b.hookResult(inner, innerFn, scope, depth+1), vals)
		}
	}
	ret := hookReturn(fn.Body)
	if ret == nil {
		return hookVal{known: true}
	}
	return b.hookReturnVal(ret, vals, setters, scope, depth)
}

// hookReturn returns the expression of the last top-level return.
func hookReturn(body []ast.Stmt) ast.Expr {
	for i := len(body) - 1; i >= 0; i-- {
		if r, ok := body[i].(*ast.ReturnStmt); ok {
			return r.Value
		}
	}
	return nil
}

// hookReturnVal classifies a hook's returned expression.
func (b *builder) hookReturnVal(expr ast.Expr, vals map[string]hookVal, setters map[string]bool, scope valueMap, depth int) hookVal {
	switch e := unparen(expr).(type) {
	case *ast.TypeAssertion:
		return b.hookReturnVal(e.Expr, vals, setters, scope, depth)
	case *ast.Identifier:
		if v, ok := vals[e.Name]; ok {
			return v
		}
		return hookVal{known: true}
	case *ast.ObjectExpr:
		out := hookVal{known: true, fields: map[string]hookVal{}}
		for _, p := range e.Properties {
			if p == nil {
				continue
			}
			switch {
			case p.Spread:
				if id, ok := p.Value.(*ast.Identifier); ok {
					for k, v := range vals[id.Name].fields {
						out.fields[k] = v
					}
					continue
				}
				out.known = false
			case p.Computed || p.Getter || p.Setter:
				out.known = false
			case p.Shorthand:
				out.fields[p.Key] = vals[p.Key]
			case p.Value != nil:
				out.fields[p.Key] = b.hookReturnVal(p.Value, vals, setters, scope, depth)
			}
		}
		return out
	case *ast.ArrayExpr:
		out := hookVal{known: true}
		for _, el := range e.Elements {
			if el == nil {
				out.elems = append(out.elems, hookVal{known: true})
				continue
			}
			out.elems = append(out.elems, b.hookReturnVal(el, vals, setters, scope, depth))
		}
		return out
	case *ast.ArrowFn:
		// A function that reads hook state and writes none is a derived
		// getter (`doubled: () => n() * 2`); one that writes is an action.
		reads, writes := false, false
		for _, stmt := range e.Body {
			WalkAST(stmt, func(n any) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if id, ok := c.Callee.(*ast.Identifier); ok {
						if setters[id.Name] {
							writes = true
						}
						if v, ok := vals[id.Name]; ok && v.getter {
							reads = true
						}
					}
				}
				return true
			})
		}
		if reads && !writes {
			return hookVal{getter: true, known: true, init: jsvalue.Dynamic("hook derived")}
		}
		return hookVal{known: true}
	case *ast.CallExpr:
		if inner, innerFn := b.stateHookCall(e); inner != nil {
			return b.hookResult(inner, innerFn, scope, depth+1)
		}
	}
	return hookVal{}
}

// bindHookPattern binds the names of a declaration of a hook result to the
// values they receive.
func bindHookPattern(d *ast.VarDecl, v hookVal, out map[string]hookVal) {
	if !d.IsDestructuring {
		if d.Name != "" {
			out[d.Name] = v
		}
		return
	}
	switch t := d.Target.(type) {
	case *ast.ObjectExpr:
		for _, p := range t.Properties {
			if p == nil || p.Spread || p.Computed {
				continue
			}
			if id, ok := p.Value.(*ast.Identifier); ok {
				out[id.Name] = v.fields[p.Key]
			}
		}
	case *ast.ArrayExpr:
		for i, el := range t.Elements {
			if id, ok := el.(*ast.Identifier); ok && i < len(v.elems) {
				out[id.Name] = v.elems[i]
			}
		}
	}
}

// registerHookLocals makes the getters a component's hook calls return
// reactive getters in the signal table, with the hook's initial value as the
// value of the server HTML.
func (b *builder) registerHookLocals(fn *ast.FnDecl) {
	if b.localSignals == nil {
		return
	}
	for _, stmt := range fn.Body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d == nil || d.Init == nil {
				continue
			}
			call, hookFn := b.stateHookCall(d.Init)
			if call == nil {
				continue
			}
			res := b.hookResult(call, hookFn, b.localProps, 0)
			names := map[string]hookVal{}
			bindHookPattern(d, res, names)
			if !res.known {
				b.registerCalledNames(fn, d, names)
			}
			for name, v := range names {
				switch {
				case v.getter:
					b.localSignals[name] = exprFromValue(v.init)
				case !d.IsDestructuring && (len(v.fields) > 0 || len(v.elems) > 0):
					// `const t = useToggle()`: reads like `t.on()` stay live.
					b.localSignals[name] = &ast.Identifier{Name: "__krate_dynamic_context__"}
				}
			}
		}
	}
}

// registerCalledNames covers a hook whose result shape is not known: a
// destructured name the render calls without arguments is a getter.
func (b *builder) registerCalledNames(fn *ast.FnDecl, d *ast.VarDecl, names map[string]hookVal) {
	if !d.IsDestructuring {
		return
	}
	ret := findReturnStmt(fn.Body)
	if ret == nil || ret.Value == nil {
		return
	}
	called, bare := map[string]bool{}, map[string]bool{}
	WalkAST(ret.Value, func(n any) bool {
		switch e := n.(type) {
		case *ast.CallExpr:
			if id, ok := e.Callee.(*ast.Identifier); ok && len(e.Args) == 0 {
				called[id.Name] = true
			}
		case *ast.JSXAttr:
			if id, ok := e.Value.(*ast.Identifier); ok {
				bare[id.Name] = true
			}
		}
		return true
	})
	for _, name := range d.Names {
		if called[name] && !bare[name] {
			names[name] = hookVal{getter: true, known: true, init: jsvalue.Dynamic("hook state")}
		}
	}
}

// declaresLocalFn reports whether decl is a function declared in body.
func declaresLocalFn(body []ast.Stmt, decl *ast.FnDecl) bool {
	for _, stmt := range body {
		if fn, ok := stmt.(*ast.FnDecl); ok && fn == decl {
			return true
		}
	}
	return false
}
