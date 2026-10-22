package annotator

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// Synthetic bindings of a lowered class component.
const (
	// classStateName / classSetterName are the committed-state signal: render
	// reads it, the runtime instance writes it when an update commits.
	classStateName  = "__krate_class_state"
	classSetterName = "__krate_class_setState"
	// classThisName is the runtime instance (packages/runtime/src/class-component.ts).
	classThisName     = "__krate_this"
	classDefaultsName = "__krate_class_defaults"
	classContextName  = "__krate_class_context"
	classInitName     = "__krate_class_init"
	classRuntimeFn    = "__krateClass"
)

// synthesizeClassComponent lowers a React class component into a function
// component driven by the runtime class instance:
//
//	function Counter(props) {
//	  const __krate_class_defaults = <static defaultProps>;
//	  const __krate_class_context = <static contextType>.useContext();
//	  const [__krate_class_state, __krate_class_setState] = createSignal(<initial state>);
//	  var __krate_this = __krateClass(props, __krate_class_state, __krate_class_setState, {
//	    name, pure, defaultProps, context, getDerivedStateFromProps, ..., componentDidMount: ...
//	  });
//	  var __krate_member_<method> = (...) => { ... };   // methods, accessors, fields
//	  ...constructor statements...
//	  ...render body...
//	}
//
// render() and the methods and getters it reaches read `this.state` from the
// committed-state signal and `this.props` from the (runtime-gated) props
// object, so the build folds the initial render and bindings update when an
// update commits. Lifecycles, handlers and other methods use the instance, as
// in React. It returns ok=false for classes analyzeClass rejects; the reason is
// reported by collectClassRejections.
func synthesizeClassComponent(class *ast.ClassDecl) (*ast.FnDecl, bool) {
	if class == nil || class.Name == "" || !isReactClassBase(class.SuperClass) {
		return nil, false
	}
	m := analyzeClass(class)
	if m.issue != nil {
		return nil, false
	}
	l := newClassLowerer(m)
	return &ast.FnDecl{
		Position: class.Position,
		Name:     class.Name,
		Params:   []*ast.Param{{Name: "props"}},
		Body:     l.body(),
	}, true
}

type thisCtx int

const (
	// ctxRender: render() and the members it reaches (committed state signal).
	ctxRender thisCtx = iota
	// ctxInit: field initializers and the constructor (props parameter).
	ctxInit
	// ctxInstance: lifecycles, handlers and other methods (runtime instance).
	ctxInstance
)

type classLowerer struct {
	m           *classModel
	defaultKeys map[string]bool
	inlining    map[string]bool
}

func newClassLowerer(m *classModel) *classLowerer {
	l := &classLowerer{m: m, defaultKeys: map[string]bool{}, inlining: map[string]bool{}}
	if obj, ok := m.defaultProps.(*ast.ObjectExpr); ok {
		for _, p := range obj.Properties {
			if p != nil && !p.Spread && !p.Computed && p.Key != "" {
				l.defaultKeys[p.Key] = true
			}
		}
	}
	return l
}

func ident(name string) *ast.Identifier { return &ast.Identifier{Name: name} }

func member(obj ast.Expr, prop string) *ast.MemberExpr {
	return &ast.MemberExpr{Object: obj, Property: ident(prop)}
}

func call(callee ast.Expr, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Callee: callee, Args: args}
}

func varDecl(kind ast.VarKind, name string, init ast.Expr) *ast.VarStmt {
	return &ast.VarStmt{Kind: kind, Decls: []*ast.VarDecl{{Name: name, Init: init}}}
}

func (l *classLowerer) body() []ast.Stmt {
	m := l.m
	var out []ast.Stmt
	if m.defaultProps != nil {
		out = append(out, varDecl(ast.VarConst, classDefaultsName, l.lowerExpr(m.defaultProps, ctxInit)))
	}
	if m.contextType != nil {
		out = append(out, varDecl(ast.VarConst, classContextName,
			call(member(l.lowerExpr(m.contextType, ctxInit), "useContext"))))
	}
	out = append(out, l.stateSignal()...)
	out = append(out, varDecl(ast.VarVar, classThisName, call(ident(classRuntimeFn),
		ident("props"), ident(classStateName), ident(classSetterName), l.spec())))
	out = append(out, l.memberDecls()...)
	if m.ctor != nil {
		out = append(out, l.constructorStmts()...)
	}
	render := wrapRenderReturns(inlineRenderLocals(m.render.Body))
	out = append(out, l.lowerStmts(render, ctxRender)...)
	return out
}

// wrapRenderReturns turns `return this.props.children` (or any non-JSX value)
// into `return <>{...}</>`: the IR builds a fragment's children as slots, which
// keeps the children region (and an error boundary's children branch) intact.
func wrapRenderReturns(stmts []ast.Stmt) []ast.Stmt {
	out := make([]ast.Stmt, len(stmts))
	for i, s := range stmts {
		switch v := s.(type) {
		case *ast.ReturnStmt:
			if v.Value != nil && needsFragmentWrap(v.Value) {
				c := *v
				c.Value = &ast.JSXFragment{Position: v.Position, Children: []ast.JSXChild{&ast.JSXExprContainer{Expression: v.Value}}}
				out[i] = &c
				continue
			}
		case *ast.IfStmt:
			c := *v
			c.Consequent = wrapRenderReturns(v.Consequent)
			c.Alternate = wrapRenderReturns(v.Alternate)
			out[i] = &c
			continue
		case *ast.BlockStmt:
			c := *v
			c.Body = wrapRenderReturns(v.Body)
			out[i] = &c
			continue
		}
		out[i] = s
	}
	return out
}

func needsFragmentWrap(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.JSXElement, *ast.JSXFragment:
		return false
	case *ast.Literal:
		return false
	case *ast.ConditionalExpr:
		return needsFragmentWrap(x.Consequent) || needsFragmentWrap(x.Alternate)
	case *ast.TypeAssertion:
		return needsFragmentWrap(x.Expr)
	}
	return true
}

// stateSignal declares the committed-state signal. With
// getDerivedStateFromProps the initial state includes the derived state, so
// the build renders what React's first render shows.
func (l *classLowerer) stateSignal() []ast.Stmt {
	m := l.m
	var init ast.Expr = &ast.ObjectExpr{}
	if m.stateInit != nil {
		init = l.lowerExpr(m.stateInit, ctxInit)
	}
	var out []ast.Stmt
	if m.gdsfp != nil {
		out = append(out, varDecl(ast.VarConst, classInitName, init))
		// The props parameter is dropped and its uses read the component's
		// props directly, which the build resolves from the call site.
		fn := l.staticFn(m.gdsfp, true)
		var args []ast.Expr
		switch {
		case len(fn.Params) == 0:
		case fn.Params[0] != nil && fn.Params[0].Name == "props" && fn.Params[0].Pattern == "":
			fn.Params = fn.Params[1:]
			args = append(args, ident(classInitName))
		default:
			args = append(args, ident("props"), ident(classInitName))
		}
		init = call(member(ident("Object"), "assign"), &ast.ObjectExpr{}, ident(classInitName), call(fn, args...))
	}
	out = append(out, &ast.VarStmt{Kind: ast.VarConst, Decls: []*ast.VarDecl{{
		IsDestructuring: true,
		Names:           []string{classStateName, classSetterName},
		Init:            call(ident("createSignal"), init),
	}}})
	return out
}

// spec builds the ClassSpec object passed to the runtime.
func (l *classLowerer) spec() ast.Expr {
	m := l.m
	obj := &ast.ObjectExpr{}
	add := func(key string, v ast.Expr) {
		obj.Properties = append(obj.Properties, &ast.ObjectProp{Key: key, Value: v})
	}
	var name ast.Expr = &ast.Literal{Kind: ast.StringLit, Value: m.name}
	if m.displayName != nil {
		name = l.lowerExpr(m.displayName, ctxInit)
	}
	add("name", name)
	if m.pure {
		add("pure", &ast.Literal{Kind: ast.BoolLit, Value: "true"})
	}
	if m.defaultProps != nil {
		add("defaultProps", ident(classDefaultsName))
	}
	if m.contextType != nil {
		add("context", &ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: ident(classContextName)}}})
	}
	if m.gdsfp != nil {
		add("getDerivedStateFromProps", l.staticFn(m.gdsfp, false))
	}
	if m.gdsfe != nil {
		add("getDerivedStateFromError", l.staticFn(m.gdsfe, false))
	}
	if exposed := l.exposed(); len(exposed.Properties) > 0 {
		add("expose", &ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: exposed}}})
	}
	for _, src := range lifecycleOrder {
		mem := m.lifecycles[src]
		if mem == nil {
			continue
		}
		lc := instanceLifecycles[src]
		// Call through the local so the method is defined by the time the
		// runtime invokes it (the spec is built before the member locals).
		var params []*ast.Param
		var args []ast.Expr
		for i := 0; i < lc.arity; i++ {
			n := "__krate_a" + itoa(i)
			params = append(params, &ast.Param{Name: n})
			args = append(args, ident(n))
		}
		add(lc.key, &ast.ArrowFn{Params: params, Expression: true,
			Body: []ast.Stmt{&ast.ReturnStmt{Value: call(ident(classMemberLocal(src)), args...)}}})
	}
	return obj
}

// exposed lists the public methods and function-valued fields, which React
// code reaches through an instance ref (`this.child.current.focus()`).
func (l *classLowerer) exposed() *ast.ObjectExpr {
	obj := &ast.ObjectExpr{}
	add := func(name string) {
		if strings.HasPrefix(name, "#") {
			return
		}
		obj.Properties = append(obj.Properties, &ast.ObjectProp{Key: name, Value: ident(classMemberLocal(name))})
	}
	for _, mem := range l.m.methods {
		add(mem.Name)
	}
	for _, mem := range l.m.fields {
		if _, ok := mem.Init.(*ast.ArrowFn); ok {
			add(mem.Name)
		}
	}
	return obj
}

// lifecycleOrder fixes the spec property order so output is deterministic.
var lifecycleOrder = []string{
	"shouldComponentUpdate", "getSnapshotBeforeUpdate",
	"componentDidMount", "componentDidUpdate", "componentWillUnmount", "componentDidCatch",
	"componentWillReceiveProps", "UNSAFE_componentWillReceiveProps",
	"componentWillUpdate", "UNSAFE_componentWillUpdate",
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// staticFn lowers a static lifecycle to an arrow function. With propsParam the
// first parameter is renamed to `props`, so the build resolves the call-site
// props when it folds the initial derived state.
func (l *classLowerer) staticFn(mem *ast.ClassMember, propsParam bool) *ast.ArrowFn {
	params := mem.Params
	body := mem.Body
	if propsParam && len(params) > 0 && params[0] != nil && params[0].Pattern == "" && params[0].Name != "" && params[0].Name != "props" {
		from := params[0].Name
		body = walkStmts(body, renameIdent(from, "props"))
		cp := *params[0]
		cp.Name = "props"
		params = append([]*ast.Param{&cp}, params[1:]...)
	}
	return &ast.ArrowFn{Position: mem.Position, Params: params, Async: mem.Async, Body: body}
}

// renameIdent renames free uses of from to to (arrow parameters that shadow
// from stop the rename).
func renameIdent(from, to string) exprFn {
	var f exprFn
	f = func(e ast.Expr, recurse func(ast.Expr) ast.Expr) ast.Expr {
		switch x := e.(type) {
		case *ast.Identifier:
			if x.Name == from {
				c := *x
				c.Name = to
				return &c
			}
			return e
		case *ast.ArrowFn:
			for _, p := range x.Params {
				if p != nil && p.Name == from {
					return e
				}
			}
		}
		return recurse(e)
	}
	return f
}

// memberDecls declares methods (and lifecycles) first, as they live on the
// prototype in React, then accessors, then fields in source order, then
// instance properties that are only ever assigned.
func (l *classLowerer) memberDecls() []ast.Stmt {
	m := l.m
	var out []ast.Stmt
	declared := map[string]bool{}
	method := func(name string, mem *ast.ClassMember, ctx thisCtx) {
		declared[name] = true
		out = append(out, varDecl(ast.VarVar, classMemberLocal(name), l.methodFn(mem, ctx)))
	}
	for _, mem := range m.methods {
		ctx := ctxInstance
		if m.renderMembers[mem.Name] {
			ctx = ctxRender
		}
		method(mem.Name, mem, ctx)
	}
	for _, src := range lifecycleOrder {
		if mem := m.lifecycles[src]; mem != nil {
			method(src, mem, ctxInstance)
		}
	}
	for _, name := range sortedKeys(m.getters) {
		ctx := ctxInstance
		if m.renderMembers[name] {
			ctx = ctxRender
		}
		out = append(out, varDecl(ast.VarVar, classAccessorLocal("get", name), l.methodFn(m.getters[name], ctx)))
	}
	for _, name := range sortedKeys(m.setters) {
		out = append(out, varDecl(ast.VarVar, classAccessorLocal("set", name), l.methodFn(m.setters[name], ctxInstance)))
	}
	for _, mem := range m.fields {
		declared[mem.Name] = true
		var init ast.Expr
		if mem.Init != nil {
			ctx := ctxInit
			if _, ok := mem.Init.(*ast.ArrowFn); ok {
				ctx = ctxInstance
			}
			init = l.lowerExpr(mem.Init, ctx)
		}
		out = append(out, varDecl(ast.VarVar, classMemberLocal(mem.Name), init))
	}
	for _, name := range sortedKeys(m.members) {
		if declared[name] {
			continue
		}
		out = append(out, &ast.VarStmt{Kind: ast.VarVar, Decls: []*ast.VarDecl{{Name: classMemberLocal(name)}}})
	}
	return out
}

func sortedKeys[V any](mp map[string]V) []string {
	keys := make([]string, 0, len(mp))
	for k := range mp {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// methodFn lowers a method body to an arrow function (lexical this is the
// instance). Generator methods stay function expressions.
func (l *classLowerer) methodFn(mem *ast.ClassMember, ctx thisCtx) ast.Expr {
	return &ast.ArrowFn{
		Position:  mem.Position,
		Params:    walkParams(mem.Params, l.fn(ctx)),
		Async:     mem.Async,
		Generator: mem.Generator,
		Function:  mem.Generator,
		Body:      l.lowerStmts(mem.Body, ctx),
	}
}

// constructorStmts lowers the constructor body minus super(props), the state
// initializer, and `this.x = this.x.bind(this)` (methods are already bound
// locals).
func (l *classLowerer) constructorStmts() []ast.Stmt {
	body := l.m.ctor.Body
	if l.m.ctorProps != "" {
		body = walkStmts(body, renameIdent(l.m.ctorProps, "props"))
	}
	var out []ast.Stmt
	for _, stmt := range body {
		if isSuperCall(stmt) || thisStateAssignment(stmt) != nil || isSelfBind(stmt) {
			continue
		}
		out = append(out, l.lowerStmts([]ast.Stmt{stmt}, ctxInit)...)
	}
	return out
}

func isSuperCall(stmt ast.Stmt) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	c, ok := es.Expression.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := c.Callee.(*ast.Identifier)
	return ok && id.Name == "super"
}

// isSelfBind matches `this.x = this.x.bind(this)`.
func isSelfBind(stmt ast.Stmt) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	assign, ok := es.Expression.(*ast.BinaryExpr)
	if !ok || assign.Op != "=" {
		return false
	}
	left, _, ok := thisMember(assign.Left)
	if !ok {
		return false
	}
	c, ok := assign.Right.(*ast.CallExpr)
	if !ok || len(c.Args) != 1 {
		return false
	}
	if _, ok := c.Args[0].(*ast.ThisExpr); !ok {
		return false
	}
	mem, ok := c.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return false
	}
	if p, ok := mem.Property.(*ast.Identifier); !ok || p.Name != "bind" {
		return false
	}
	inner, _, ok := thisMember(mem.Object)
	return ok && inner == left
}

func (l *classLowerer) lowerStmts(stmts []ast.Stmt, ctx thisCtx) []ast.Stmt {
	return walkStmts(stmts, l.fn(ctx))
}

func (l *classLowerer) lowerExpr(e ast.Expr, ctx thisCtx) ast.Expr {
	return walkExpr(e, l.fn(ctx))
}

// fn returns the `this` rewriter for ctx.
func (l *classLowerer) fn(ctx thisCtx) exprFn {
	var f exprFn
	f = func(e ast.Expr, recurse func(ast.Expr) ast.Expr) ast.Expr {
		switch x := e.(type) {
		case *ast.ArrowFn:
			if x.Function {
				// A function expression binds its own `this`.
				return e
			}
		case *ast.ClassExpr:
			return e
		case *ast.ThisExpr:
			return ident(classThisName)
		case *ast.MemberExpr:
			if ctx != ctxInstance && !x.Computed {
				if inner, _, ok := thisMember(x.Object); ok && inner == "props" {
					if p, ok := x.Property.(*ast.Identifier); ok && l.defaultKeys[p.Name] {
						read := member(ident("props"), p.Name)
						return &ast.ConditionalExpr{
							Test:       &ast.BinaryExpr{Left: read, Op: "===", Right: ident("undefined")},
							Consequent: member(ident(classDefaultsName), p.Name),
							Alternate:  member(ident("props"), p.Name),
						}
					}
				}
			}
			if name, _, ok := thisMember(x); ok {
				return l.thisRef(name, ctx)
			}
		case *ast.BinaryExpr:
			if isAssignOp(x.Op) {
				if name, _, ok := thisMember(x.Left); ok && l.m.setters[name] != nil {
					value := walkExpr(x.Right, f)
					if x.Op != "=" {
						value = &ast.BinaryExpr{Left: l.thisRef(name, ctx), Op: x.Op[:len(x.Op)-1], Right: value}
					}
					return call(ident(classAccessorLocal("set", name)), value)
				}
			}
		case *ast.UnaryExpr:
			if x.Op == "++" || x.Op == "--" {
				if name, _, ok := thisMember(x.Arg); ok && l.m.setters[name] != nil {
					op := "+"
					if x.Op == "--" {
						op = "-"
					}
					next := &ast.BinaryExpr{Left: l.thisRef(name, ctx), Op: op, Right: &ast.Literal{Kind: ast.NumberLit, Value: "1"}}
					return call(ident(classAccessorLocal("set", name)), next)
				}
			}
		}
		return recurse(e)
	}
	return f
}

// thisRef lowers `this.<name>` in ctx.
func (l *classLowerer) thisRef(name string, ctx thisCtx) ast.Expr {
	m := l.m
	inst := func() ast.Expr { return member(ident(classThisName), name) }
	switch name {
	case "state":
		if ctx == ctxInstance {
			return inst()
		}
		return call(ident(classStateName))
	case "props":
		if ctx == ctxInstance {
			return inst()
		}
		return ident("props")
	case "context":
		if ctx != ctxInstance && m.contextType != nil {
			return ident(classContextName)
		}
		return inst()
	case "setState", "forceUpdate":
		return inst()
	}
	if g := m.getters[name]; g != nil {
		// A single-return getter read during render is inlined, so the build
		// folds it and each binding recomputes it.
		if ctx == ctxRender && !l.inlining[name] && len(g.Body) == 1 {
			if ret, ok := g.Body[0].(*ast.ReturnStmt); ok && ret.Value != nil {
				l.inlining[name] = true
				v := l.lowerExpr(ret.Value, ctxRender)
				delete(l.inlining, name)
				return v
			}
		}
		return call(ident(classAccessorLocal("get", name)))
	}
	if m.members[name] || instanceLifecycleName(name) {
		return ident(classMemberLocal(name))
	}
	if m.setters[name] != nil {
		// A write-only accessor reads as undefined.
		return ident("undefined")
	}
	return inst()
}
