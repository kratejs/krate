package bundler

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// React 19 action hooks are lowered to signals plus runtime helpers from
// packages/runtime/src/actions.ts (window globals in the runtime chunk):
//
//	const [s, act, pending] = useActionState(fn, init, permalink)
//	  const [s, set] = createSignal(init); const [pending, setP] = createSignal(false);
//	  const act = __krateActionState(fn, () => s(), set, setP, init, permalink);
//
//	const [o, add] = useOptimistic(base, update)
//	  const [o, setO] = createSignal(base);
//	  const add = __krateOptimistic(() => base, setO, update);
//
//	const { pending } = useFormStatus()
//	  const [pending, setPending] = createSignal(false);
//	  const __krate_fs_ref_N = (el) => __krateFormStatusMount(el, { pending: setPending });
//	  plus ref={__krate_fs_ref_N} on the component's root element.
//
// The IR builder recognizes __krateActionState to render the no-JS form target.

const formStatusRefPrefix = "__krate_fs_ref_"

func synthIdent(name string) *ast.Identifier { return &ast.Identifier{Name: name} }

func synthCall(name string, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Callee: synthIdent(name), Args: args}
}

func synthLit(kind ast.LitKind, value string) *ast.Literal {
	return &ast.Literal{Kind: kind, Value: value}
}

func falseLit() ast.Expr { return synthLit(ast.BoolLit, "false") }
func nullLit() ast.Expr  { return synthLit(ast.NullLit, "null") }

// thunk returns `() => expr`.
func thunk(expr ast.Expr) *ast.ArrowFn {
	return &ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: expr}}}
}

// cloneLiteralExpr copies literal-shaped expressions so one source expression
// can appear twice in the lowered output without aliasing AST nodes.
func cloneLiteralExpr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.Literal:
		return &ast.Literal{Position: e.Position, Kind: e.Kind, Value: e.Value}
	case *ast.Identifier:
		return &ast.Identifier{Position: e.Position, Name: e.Name}
	case *ast.CallExpr:
		if len(e.Args) == 0 {
			return &ast.CallExpr{Position: e.Position, Callee: cloneLiteralExpr(e.Callee)}
		}
	case *ast.ObjectExpr:
		out := &ast.ObjectExpr{Position: e.Position}
		for _, p := range e.Properties {
			if p == nil {
				continue
			}
			cp := *p
			cp.Value = cloneLiteralExpr(p.Value)
			out.Properties = append(out.Properties, &cp)
		}
		return out
	case *ast.ArrayExpr:
		out := &ast.ArrayExpr{Position: e.Position}
		for _, el := range e.Elements {
			out.Elements = append(out.Elements, cloneLiteralExpr(el))
		}
		return out
	case *ast.MemberExpr:
		return &ast.MemberExpr{Position: e.Position, Object: cloneLiteralExpr(e.Object), Property: e.Property, Computed: e.Computed, Optional: e.Optional}
	}
	return expr
}

// pairDecl builds `[a, b] = init` with the names, source pattern and
// structured target every consumer of destructuring declarations reads.
func pairDecl(a, b string, init ast.Expr) *ast.VarDecl {
	return &ast.VarDecl{
		IsDestructuring: true,
		Names:           []string{a, b},
		Pattern:         "[" + a + ", " + b + "]",
		Target:          &ast.ArrayExpr{Elements: []ast.Expr{synthIdent(a), synthIdent(b)}},
		Init:            init,
	}
}

func destructured(decl *ast.VarDecl, i int) string {
	if decl == nil || i >= len(decl.Names) {
		return ""
	}
	return decl.Names[i]
}

// lowerActionHook lowers a single-declarator `const ... = useActionState(...)`,
// `useOptimistic(...)` or `useFormStatus()` statement in place. It reports
// whether the statement was handled.
func (c *reactCtx) lowerActionHook(v *ast.VarStmt, sc *scope) bool {
	if len(v.Decls) != 1 || v.Decls[0] == nil {
		return false
	}
	decl := v.Decls[0]
	call, ok := decl.Init.(*ast.CallExpr)
	if !ok {
		return false
	}
	api, ok := c.callAPI(call.Callee)
	if !ok {
		return false
	}
	switch api {
	case "useActionState":
		return c.lowerUseActionState(v, decl, call, sc)
	case "useOptimistic":
		return c.lowerUseOptimistic(v, decl, call, sc)
	case "useFormStatus":
		return c.lowerUseFormStatus(v, decl, sc)
	}
	return false
}

func (c *reactCtx) lowerUseActionState(v *ast.VarStmt, decl *ast.VarDecl, call *ast.CallExpr, sc *scope) bool {
	state, dispatch := destructured(decl, 0), destructured(decl, 1)
	if !decl.IsDestructuring || state == "" || dispatch == "" || len(call.Args) == 0 {
		return false
	}
	pending := destructured(decl, 2)
	if pending == "" {
		pending = "__krate_as_pending_" + state
	}
	setState := "__krate_as_set_" + state
	setPending := "__krate_as_setp_" + state

	action := c.rewriteExpr(call.Args[0], sc)
	var initial ast.Expr = synthIdent("undefined")
	if len(call.Args) >= 2 {
		initial = c.rewriteExpr(call.Args[1], sc)
	}
	args := []ast.Expr{
		action,
		thunk(synthCall(state)),
		synthIdent(setState),
		synthIdent(setPending),
		cloneLiteralExpr(initial),
	}
	if len(call.Args) >= 3 {
		args = append(args, c.rewriteExpr(call.Args[2], sc))
	}
	v.Decls = []*ast.VarDecl{
		pairDecl(state, setState, synthCall("createSignal", initial)),
		pairDecl(pending, setPending, synthCall("createSignal", falseLit())),
		{Name: dispatch, Init: &ast.CallExpr{Position: call.Position, Callee: synthIdent("__krateActionState"), Args: args}},
	}
	sc.declareGetter(state)
	sc.declareGetter(pending)
	sc.declareLocal(setState)
	sc.declareLocal(setPending)
	sc.declareLocal(dispatch)
	return true
}

func (c *reactCtx) lowerUseOptimistic(v *ast.VarStmt, decl *ast.VarDecl, call *ast.CallExpr, sc *scope) bool {
	value := destructured(decl, 0)
	if !decl.IsDestructuring || value == "" || len(call.Args) == 0 {
		return false
	}
	add := destructured(decl, 1)
	if add == "" {
		add = "__krate_opt_add_" + value
	}
	setter := "__krate_opt_set_" + value
	base := c.rewriteExpr(call.Args[0], sc)
	args := []ast.Expr{thunk(cloneLiteralExpr(base)), synthIdent(setter)}
	if len(call.Args) >= 2 {
		args = append(args, c.rewriteExpr(call.Args[1], sc))
	}
	v.Decls = []*ast.VarDecl{
		pairDecl(value, setter, synthCall("createSignal", base)),
		{Name: add, Init: &ast.CallExpr{Position: call.Position, Callee: synthIdent("__krateOptimistic"), Args: args}},
	}
	sc.declareGetter(value)
	sc.declareLocal(setter)
	sc.declareLocal(add)
	return true
}

// formStatusIdle returns the idle value of one useFormStatus field.
func formStatusIdle(field string) ast.Expr {
	if field == "pending" {
		return falseLit()
	}
	return nullLit()
}

func (c *reactCtx) lowerUseFormStatus(v *ast.VarStmt, decl *ast.VarDecl, sc *scope) bool {
	c.formStatusSeq++
	ref := formStatusRefPrefix + strconv.Itoa(c.formStatusSeq)
	setters := &ast.ObjectExpr{}
	var decls []*ast.VarDecl
	switch {
	case !decl.IsDestructuring && decl.Name != "":
		setter := "__krate_fs_set_" + decl.Name
		idle := &ast.ObjectExpr{}
		for _, f := range []string{"pending", "data", "method", "action"} {
			idle.Properties = append(idle.Properties, &ast.ObjectProp{Key: f, Value: formStatusIdle(f)})
		}
		decls = append(decls, pairDecl(decl.Name, setter, synthCall("createSignal", idle)))
		setters.Properties = append(setters.Properties, &ast.ObjectProp{Key: "status", Value: synthIdent(setter)})
		sc.declareGetter(decl.Name)
		sc.declareLocal(setter)
	case decl.IsDestructuring:
		obj, ok := decl.Target.(*ast.ObjectExpr)
		if !ok {
			return false
		}
		for _, p := range obj.Properties {
			if p == nil || p.Spread || p.Computed {
				return false
			}
			local, ok := p.Value.(*ast.Identifier)
			if !ok {
				return false
			}
			setter := "__krate_fs_" + p.Key + "_" + local.Name
			decls = append(decls, pairDecl(local.Name, setter, synthCall("createSignal", formStatusIdle(p.Key))))
			setters.Properties = append(setters.Properties, &ast.ObjectProp{Key: p.Key, Value: synthIdent(setter)})
			sc.declareGetter(local.Name)
			sc.declareLocal(setter)
		}
	default:
		return false
	}
	mount := &ast.ArrowFn{
		Params:     []*ast.Param{{Name: "el"}},
		Expression: true,
		Body:       []ast.Stmt{&ast.ReturnStmt{Value: synthCall("__krateFormStatusMount", synthIdent("el"), setters)}},
	}
	decls = append(decls, &ast.VarDecl{Name: ref, Init: mount})
	sc.declareLocal(ref)
	v.Decls = decls
	c.formStatusPos = append(c.formStatusPos, v.Position)
	return true
}

// injectFormStatusRefs attaches the lowered useFormStatus mount refs declared
// in a component body to the element each `return` renders, so the runtime
// can find the form that contains the component.
func (c *reactCtx) injectFormStatusRefs(body []ast.Stmt) {
	var refs []string
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d != nil && strings.HasPrefix(d.Name, formStatusRefPrefix) {
				refs = append(refs, d.Name)
			}
		}
	}
	if len(refs) == 0 {
		return
	}
	injected := false
	var visit func(stmts []ast.Stmt)
	visit = func(stmts []ast.Stmt) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ReturnStmt:
				if attachRefs(s.Value, refs) {
					injected = true
				}
			case *ast.IfStmt:
				visit(s.Consequent)
				visit(s.Alternate)
			case *ast.BlockStmt:
				visit(s.Body)
			}
		}
	}
	visit(body)
	if !injected {
		pos := ast.Pos{}
		if n := len(c.formStatusPos); n > 0 {
			pos = c.formStatusPos[n-1]
		}
		c.warn(pos, diag.CodeFormStatusNoElement,
			"useFormStatus() needs the component to return a DOM element; its status stays idle",
			"return an element such as <button> or <span> (not a component or a value) from the component that calls useFormStatus()")
	}
}

// attachRefs adds the mount refs to the root DOM element of a returned value.
func attachRefs(expr ast.Expr, refs []string) bool {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return attachRefs(e.Expr, refs)
	case *ast.ConditionalExpr:
		a := attachRefs(e.Consequent, refs)
		b := attachRefs(e.Alternate, refs)
		return a || b
	case *ast.BinaryExpr:
		if e.Op == "&&" || e.Op == "||" || e.Op == "??" {
			return attachRefs(e.Right, refs)
		}
	case *ast.JSXFragment:
		for _, ch := range e.Children {
			if el, ok := ch.(*ast.JSXElementChild); ok && el.Element != nil {
				if attachRefs(el.Element, refs) {
					return true
				}
			}
		}
	case *ast.JSXElement:
		if e.Opening == nil || e.Opening.Name == "" {
			return false
		}
		first := e.Opening.Name[0]
		if first < 'a' || first > 'z' || strings.Contains(e.Opening.Name, ".") {
			return false
		}
		addRefAttr(e.Opening, refs)
		return true
	}
	return false
}

// addRefAttr sets ref={mount} on an element, composing with an existing ref
// through the runtime's __krateAssignRef (callback or {current} object).
func addRefAttr(op *ast.JSXOpening, refs []string) {
	var existing *ast.JSXAttr
	for _, a := range op.Attributes {
		if a != nil && !a.Spread && a.Name == "ref" {
			existing = a
		}
	}
	if existing == nil && len(refs) == 1 {
		op.Attributes = append(op.Attributes, &ast.JSXAttr{Name: "ref", Value: synthIdent(refs[0])})
		return
	}
	var body []ast.Stmt
	for _, r := range refs {
		body = append(body, &ast.ExprStmt{Expression: synthCall(r, synthIdent("el"))})
	}
	if existing != nil && existing.Value != nil {
		body = append(body, &ast.ExprStmt{Expression: synthCall("__krateAssignRef", existing.Value, synthIdent("el"))})
	}
	fn := &ast.ArrowFn{Params: []*ast.Param{{Name: "el"}}, Body: body}
	if existing != nil {
		existing.Value = fn
		return
	}
	op.Attributes = append(op.Attributes, &ast.JSXAttr{Name: "ref", Value: fn})
}
