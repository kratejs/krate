package bundler

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// React Suspense, lazy, use() and transition lowering.
//
//   - `const X = lazy(() => import("./X"))` at module level becomes
//     `import X from "./X"` plus a `const __krate_lazy__X = "./X"` marker. The
//     component is rendered at build time like any import; the marker makes
//     the IR builder put its hydration code into a separately loaded chunk.
//   - `const v = <expr using use(p)>` becomes `const v = __krateUse(() => expr)`
//     with each `use(p)` replaced by `__krateRead(p)`; `v` is a getter. A
//     `use(Ctx)` of a context lowers to `Ctx.useContext()` like useContext.
//   - `const [isPending, start] = useTransition()` becomes a pending signal
//     and `__krateTransitionStarter(setPending)` (async actions keep it true).
//   - `const d = useDeferredValue(v, init?)` becomes
//     `createDeferredValue(() => v, init?)` and
//     `const s = useSyncExternalStore(sub, get, server?)` becomes
//     `createExternalStore(sub, get, server?)`; both are getters.
//   - `<React.Suspense>` (or an aliased import) becomes `<Suspense>`, the
//     built-in boundary; `<StrictMode>` and `<Profiler>` become fragments.

// lazyMarkerPrefix names the module-level marker constant recorded for every
// lowered lazy component (`const __krate_lazy__<Local> = "<spec>"`).
const lazyMarkerPrefix = "__krate_lazy__"

// LazyMarkerPrefix is exported for the annotator, which collects the markers.
const LazyMarkerPrefix = lazyMarkerPrefix

// LazyLoaderPrefix names the module-level loader kept for a lazy() whose
// loader is not a static import (`lazy(() => delay(ms, { default: X }))`).
const LazyLoaderPrefix = "__krate_lazyload__"

// LazyLocalSpec prefixes the marker value of such a lazy component: the rest
// is the local component the loader resolves to.
const LazyLocalSpec = "fn:"

// suspenseState is the Suspense-related part of a reactCtx.
type suspenseState struct {
	suspense map[string]bool // local bindings of React.Suspense
	contexts map[string]bool // module-level createContext bindings
	reported map[*ast.CallExpr]bool
}

func newSuspenseState() *suspenseState {
	return &suspenseState{suspense: map[string]bool{}, contexts: map[string]bool{}, reported: map[*ast.CallExpr]bool{}}
}

func (c *reactCtx) report(pos ast.Pos, sev diag.Severity, code diag.Code, msg, hint string) {
	d := diag.New(c.file, pos.Line, pos.Col, msg, hint, sourceLineOf(c.src, pos.Line)).WithCode(code)
	d.Severity = sev
	c.diags = append(c.diags, d)
}

// RewriteReactDiagnostics runs the React lowering without a file name and
// returns its diagnostics (tests).
func RewriteReactDiagnostics(prog *ast.Program) []diag.Diagnostic {
	return RewriteReactDiag(prog, "", "")
}

// splitReactDiags returns the warnings of the React lowering and fails with
// its errors (Suspense/lazy/use() misuse) as positioned diagnostics.
func splitReactDiags(diags []diag.Diagnostic) ([]diag.Diagnostic, error) {
	var warnings []diag.Diagnostic
	var errs []error
	for _, d := range diags {
		if d.IsWarning() {
			warnings = append(warnings, d)
		} else {
			errs = append(errs, d)
		}
	}
	if len(errs) > 0 {
		return nil, &parseError{summary: diag.FormatDiagnostics(errs), errs: errs}
	}
	return warnings, nil
}

// collectModuleContexts records module-level `const X = createContext(...)`
// bindings so `use(X)` is known to be a context read.
func (c *reactCtx) collectModuleContexts(prog *ast.Program) {
	for _, stmt := range prog.Body {
		vs, _ := moduleVarStmt(stmt)
		if vs == nil {
			continue
		}
		for _, d := range vs.Decls {
			call, ok := d.Init.(*ast.CallExpr)
			if !ok || d.Name == "" {
				continue
			}
			if api, ok := c.callAPI(call.Callee); ok && api == "createContext" {
				c.susp.contexts[d.Name] = true
			} else if id, ok := call.Callee.(*ast.Identifier); ok && (id.Name == "createContext" || id.Name == "createScope") {
				c.susp.contexts[d.Name] = true
			}
		}
	}
}

func moduleVarStmt(stmt ast.Stmt) (*ast.VarStmt, bool) {
	switch s := stmt.(type) {
	case *ast.VarStmt:
		return s, false
	case *ast.ExportStmt:
		if v, ok := s.Declaration.(*ast.VarStmt); ok {
			return v, true
		}
	}
	return nil, false
}

// lazy

// lowerLazyDecls replaces module-level lazy() declarations with imports.
func (c *reactCtx) lowerLazyDecls(prog *ast.Program) {
	out := make([]ast.Stmt, 0, len(prog.Body))
	for _, stmt := range prog.Body {
		vs, exported := moduleVarStmt(stmt)
		if vs == nil || !c.hasLazyDecl(vs) {
			out = append(out, stmt)
			continue
		}
		var keep []*ast.VarDecl
		for _, d := range vs.Decls {
			call := c.lazyCall(d.Init)
			if call == nil {
				keep = append(keep, d)
				continue
			}
			spec, remote, ok := lazyImportSpec(call)
			if !ok && !d.IsDestructuring && d.Name != "" {
				if impl := lazyLocalImpl(call); impl != "" {
					// The loader runs on the client when the component first
					// renders; it must resolve to { default: impl }.
					out = append(out,
						&ast.VarStmt{Position: call.Position, Kind: ast.VarConst, Decls: []*ast.VarDecl{{
							Name: LazyLoaderPrefix + d.Name,
							Init: call.Args[0],
						}}},
						&ast.VarStmt{Position: call.Position, Kind: ast.VarConst, Decls: []*ast.VarDecl{{
							Name: lazyMarkerPrefix + d.Name,
							Init: &ast.Literal{Kind: ast.StringLit, Value: LazyLocalSpec + impl},
						}}})
					continue
				}
			}
			if !ok || d.IsDestructuring || d.Name == "" {
				c.susp.reported[call] = true
				c.report(call.Position, diag.SeverityError, diag.CodeLazyLoader,
					"lazy() needs a loader of the form () => import(\"./Component\")",
					"pass a static import with a string literal path, e.g. const Chart = lazy(() => import(\"./Chart\"))")
				keep = append(keep, d)
				continue
			}
			imp := &ast.ImportStmt{Position: call.Position, Source: strconv.Quote(spec)}
			if remote == "default" {
				imp.Default = d.Name
			} else {
				imp.Named = []ast.NamedImport{{Local: d.Name, Remote: remote}}
			}
			out = append(out, imp)
			if isRelativeSpec(spec) {
				out = append(out, &ast.VarStmt{Position: call.Position, Kind: ast.VarConst, Decls: []*ast.VarDecl{{
					Name: lazyMarkerPrefix + d.Name,
					Init: &ast.Literal{Kind: ast.StringLit, Value: spec},
				}}})
			} else {
				c.report(call.Position, diag.SeverityWarning, diag.CodeLazyPackage,
					"lazy() of package "+strconv.Quote(spec)+" is not code-split; it is compiled like a static import",
					"only local modules (\"./X\") get a separately loaded hydration chunk")
			}
			if exported {
				out = append(out, &ast.ExportStmt{Position: call.Position, Named: []ast.NamedExport{{Local: d.Name, Remote: d.Name}}})
			}
		}
		if len(keep) > 0 {
			vs.Decls = keep
			out = append(out, stmt)
		}
	}
	prog.Body = out
}

func (c *reactCtx) hasLazyDecl(vs *ast.VarStmt) bool {
	for _, d := range vs.Decls {
		if c.lazyCall(d.Init) != nil {
			return true
		}
	}
	return false
}

// lazyCall returns init when it is a call to React's lazy.
func (c *reactCtx) lazyCall(init ast.Expr) *ast.CallExpr {
	call, ok := init.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if api, ok := c.callAPI(call.Callee); ok && api == "lazy" {
		return call
	}
	return nil
}

// lazyImportSpec extracts the module specifier and export name from
// `lazy(() => import("./X"))` or
// `lazy(() => import("./X").then((m) => ({ default: m.Named })))`.
func lazyImportSpec(call *ast.CallExpr) (spec, remote string, ok bool) {
	if len(call.Args) != 1 {
		return "", "", false
	}
	arrow, ok := call.Args[0].(*ast.ArrowFn)
	if !ok || len(arrow.Params) != 0 {
		return "", "", false
	}
	body := arrowReturn(arrow)
	if body == nil {
		return "", "", false
	}
	if spec, ok := dynamicImportSpec(body); ok {
		return spec, "default", true
	}
	// import("./X").then((m) => ({ default: m.Named }))
	then, ok := body.(*ast.CallExpr)
	if !ok || len(then.Args) != 1 {
		return "", "", false
	}
	mem, ok := then.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return "", "", false
	}
	if prop, ok := mem.Property.(*ast.Identifier); !ok || prop.Name != "then" {
		return "", "", false
	}
	spec, ok = dynamicImportSpec(mem.Object)
	if !ok {
		return "", "", false
	}
	mapper, ok := then.Args[0].(*ast.ArrowFn)
	if !ok || len(mapper.Params) != 1 || mapper.Params[0] == nil {
		return "", "", false
	}
	obj, ok := arrowReturn(mapper).(*ast.ObjectExpr)
	if !ok || len(obj.Properties) != 1 || obj.Properties[0] == nil || obj.Properties[0].Key != "default" {
		return "", "", false
	}
	val, ok := obj.Properties[0].Value.(*ast.MemberExpr)
	if !ok || val.Computed {
		return "", "", false
	}
	base, ok := val.Object.(*ast.Identifier)
	if !ok || base.Name != mapper.Params[0].Name {
		return "", "", false
	}
	name, ok := val.Property.(*ast.Identifier)
	if !ok {
		return "", "", false
	}
	return spec, name.Name, true
}

// lazyLocalImpl returns the component a non-import lazy loader resolves to:
// the identifier in a `{ default: Component }` object returned (possibly
// through a helper such as delay(ms, value)) by `() => ...`.
func lazyLocalImpl(call *ast.CallExpr) string {
	if len(call.Args) != 1 {
		return ""
	}
	arrow, ok := call.Args[0].(*ast.ArrowFn)
	if !ok || len(arrow.Params) != 0 {
		return ""
	}
	body := arrowReturn(arrow)
	if body == nil {
		return ""
	}
	impl := ""
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		switch x := e.(type) {
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				if p == nil {
					continue
				}
				if p.Key == "default" {
					if id, ok := p.Value.(*ast.Identifier); ok && impl == "" {
						impl = id.Name
					}
				}
				walk(p.Value)
			}
		case *ast.CallExpr:
			walk(x.Callee)
			for _, a := range x.Args {
				walk(a)
			}
		case *ast.MemberExpr:
			walk(x.Object)
		case *ast.NewExpr:
			for _, a := range x.Args {
				walk(a)
			}
		case *ast.ArrowFn:
			if r := arrowReturn(x); r != nil {
				walk(r)
			}
		}
	}
	walk(body)
	return impl
}

func dynamicImportSpec(expr ast.Expr) (string, bool) {
	di, ok := expr.(*ast.DynamicImport)
	if !ok {
		return "", false
	}
	switch a := di.Arg.(type) {
	case *ast.Literal:
		if a.Kind == ast.StringLit && a.Value != "" {
			return a.Value, true
		}
	case *ast.TemplateExpr:
		if len(a.Parts) == 1 {
			if lit, ok := a.Parts[0].(*ast.Literal); ok && lit.Kind == ast.StringLit && lit.Value != "" {
				return lit.Value, true
			}
		}
	}
	return "", false
}

// arrowReturn returns the value of a single-expression arrow (either the
// expression form or a body that is exactly one return statement).
func arrowReturn(arrow *ast.ArrowFn) ast.Expr {
	if arrow == nil || len(arrow.Body) != 1 {
		return nil
	}
	if ret, ok := arrow.Body[0].(*ast.ReturnStmt); ok {
		return ret.Value
	}
	if es, ok := arrow.Body[0].(*ast.ExprStmt); ok && arrow.Expression {
		return es.Expression
	}
	return nil
}

func isRelativeSpec(spec string) bool {
	return strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || strings.HasPrefix(spec, "/")
}

// declarations

// lowerTransitionVarStmt lowers `const [isPending, start] = useTransition()`.
func (c *reactCtx) lowerTransitionVarStmt(v *ast.VarStmt, sc *scope) bool {
	if len(v.Decls) != 1 {
		return false
	}
	decl := v.Decls[0]
	if decl == nil || !decl.IsDestructuring || len(decl.Names) < 2 {
		return false
	}
	call, ok := decl.Init.(*ast.CallExpr)
	if !ok {
		return false
	}
	if api, ok := c.callAPI(call.Callee); !ok || api != "useTransition" {
		return false
	}
	pending, start := decl.Names[0], decl.Names[1]
	setter := "__krate_transition_" + pending
	v.Decls = []*ast.VarDecl{
		{IsDestructuring: true, Names: []string{pending, setter}, Pattern: "[" + pending + ", " + setter + "]",
			Init: callNamed("createSignal", &ast.Literal{Kind: ast.BoolLit, Value: "false"})},
		// The setter is read through a closure: the hydration scope may declare
		// the starter before the signal.
		{Name: start, Init: callNamed("__krateTransitionStarter", &ast.ArrowFn{
			Params:     []*ast.Param{{Name: "v"}},
			Expression: true,
			Body:       []ast.Stmt{&ast.ReturnStmt{Value: callNamed(setter, &ast.Identifier{Name: "v"})}},
		})},
	}
	sc.declareGetter(pending)
	sc.declareLocal(setter)
	sc.declareLocal(start)
	return true
}

func callNamed(name string, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Callee: &ast.Identifier{Name: name}, Args: args}
}

func thunk(expr ast.Expr) *ast.ArrowFn {
	return &ast.ArrowFn{Expression: true, Body: []ast.Stmt{&ast.ReturnStmt{Value: expr}}}
}

// lowerSuspenseDecl lowers one declaration initialized by use(),
// useDeferredValue or useSyncExternalStore. It reports whether it handled the
// declaration (including its scope bookkeeping).
func (c *reactCtx) lowerSuspenseDecl(decl *ast.VarDecl, sc *scope) bool {
	if decl == nil || decl.Init == nil {
		return false
	}
	if call, ok := decl.Init.(*ast.CallExpr); ok {
		if api, ok := c.callAPI(call.Callee); ok && !decl.IsDestructuring && decl.Name != "" {
			switch api {
			case "useDeferredValue":
				if len(call.Args) == 0 {
					return false
				}
				args := []ast.Expr{thunk(c.rewriteExpr(call.Args[0], sc))}
				if len(call.Args) > 1 {
					args = append(args, c.rewriteExpr(call.Args[1], sc))
				}
				decl.Init = &ast.CallExpr{Position: call.Position, Callee: &ast.Identifier{Name: "createDeferredValue"}, Args: args}
				sc.declareGetter(decl.Name)
				return true
			case "useSyncExternalStore":
				args := make([]ast.Expr, 0, len(call.Args))
				for _, a := range call.Args {
					args = append(args, c.rewriteExpr(a, sc))
				}
				decl.Init = &ast.CallExpr{Position: call.Position, Callee: &ast.Identifier{Name: "createExternalStore"}, Args: args}
				sc.declareGetter(decl.Name)
				return true
			}
		}
	}
	uses := c.collectUses(decl.Init)
	if len(uses) == 0 {
		return false
	}
	allContexts := true
	for _, u := range uses {
		if len(u.Args) != 1 || !c.isContextLike(u.Args[0]) {
			allContexts = false
		}
	}
	if allContexts {
		// Context reads only: rewriteCall lowers them to Ctx.useContext().
		return false
	}
	for _, u := range uses {
		c.susp.reported[u] = true
	}
	if decl.IsDestructuring || decl.Name == "" {
		c.report(uses[0].Position, diag.SeverityError, diag.CodeUseDestructure,
			"the result of use(promise) cannot be destructured",
			"assign it to a variable and read fields from it: const user = use(userPromise); user.name")
		return true
	}
	for _, u := range uses {
		u.Callee = &ast.Identifier{Name: "__krateRead"}
	}
	inner := c.rewriteExpr(decl.Init, sc)
	decl.Init = &ast.CallExpr{Position: uses[0].Position, Callee: &ast.Identifier{Name: "__krateUse"}, Args: []ast.Expr{thunk(inner)}}
	sc.declareGetter(decl.Name)
	return true
}

// collectUses returns the use() calls in expr, not descending into nested
// functions (a use() inside a callback is not part of this render).
func (c *reactCtx) collectUses(expr ast.Expr) []*ast.CallExpr {
	var out []*ast.CallExpr
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		switch x := e.(type) {
		case *ast.CallExpr:
			if api, ok := c.callAPI(x.Callee); ok && api == "use" {
				out = append(out, x)
			}
			walk(x.Callee)
			for _, a := range x.Args {
				walk(a)
			}
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
		case *ast.TypeAssertion:
			walk(x.Expr)
		case *ast.AwaitExpr:
			walk(x.Arg)
		case *ast.ArrayExpr:
			for _, el := range x.Elements {
				walk(el)
			}
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				if p != nil {
					walk(p.Value)
				}
			}
		case *ast.TemplateExpr:
			for _, p := range x.Parts {
				walk(p)
			}
		}
	}
	walk(expr)
	return out
}

// isContextLike reports whether a use() argument is a context: a module-level
// createContext binding, or a name that reads as one (FooContext, FooCtx).
func (c *reactCtx) isContextLike(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Identifier:
		return c.susp.contexts[e.Name] || strings.HasSuffix(e.Name, "Context") || strings.HasSuffix(e.Name, "Ctx")
	case *ast.MemberExpr:
		if prop, ok := e.Property.(*ast.Identifier); ok && !e.Computed {
			return strings.HasSuffix(prop.Name, "Context")
		}
	}
	return false
}

// rewriteUseCall lowers a use() call outside a declaration initializer:
// context reads become Ctx.useContext(); anything else is reported.
func (c *reactCtx) rewriteUseCall(e *ast.CallExpr, sc *scope) (ast.Expr, bool) {
	if len(e.Args) == 1 && c.isContextLike(e.Args[0]) {
		return &ast.CallExpr{
			Position: e.Position,
			Callee:   &ast.MemberExpr{Object: c.rewriteExpr(e.Args[0], sc), Property: &ast.Identifier{Name: "useContext"}},
		}, true
	}
	if !c.susp.reported[e] {
		c.susp.reported[e] = true
		c.report(e.Position, diag.SeverityError, diag.CodeUsePlacement,
			"use(promise) must initialize a variable in the component body",
			"write const value = use(promise) and read value in JSX; the call may be conditional inside that initializer")
	}
	return nil, false
}

// reportStrayLazy reports a lazy() call left after module-level lowering.
func (c *reactCtx) reportStrayLazy(e *ast.CallExpr) {
	if c.susp.reported[e] {
		return
	}
	c.susp.reported[e] = true
	c.report(e.Position, diag.SeverityError, diag.CodeLazyPlacement,
		"lazy() must be declared at the top level of a module",
		"move it out of the component: const Chart = lazy(() => import(\"./Chart\"))")
}

// JSX

// lowerSuspenseJSX renames React.Suspense to the built-in <Suspense> and
// turns StrictMode/Profiler into fragments. It returns a replacement for
// fragment-like tags.
func (c *reactCtx) lowerSuspenseJSX(el *ast.JSXElement, sc *scope) (ast.Expr, bool) {
	if el.Opening == nil {
		return nil, false
	}
	name := el.Opening.Name
	api := c.jsxTagAPI(name)
	switch api {
	case "Suspense":
		if name != "Suspense" {
			el.Opening.Name = "Suspense"
			if el.Closing != nil {
				el.Closing.Name = "Suspense"
			}
		}
	case "StrictMode", "Profiler":
		if api == "Profiler" {
			for _, attr := range el.Opening.Attributes {
				if attr != nil && attr.Name == "onRender" {
					c.report(attr.Position, diag.SeverityWarning, diag.CodeProfilerOnRender,
						"<Profiler onRender> is never called: Krate components do not re-render",
						"remove onRender, or measure with the browser performance tools")
				}
			}
		}
		return c.lowerFragment(el, sc), true
	}
	return nil, false
}

// jsxTagAPI resolves a JSX tag to Suspense/StrictMode/Profiler when it names
// the React export (imported binding or React.X member).
func (c *reactCtx) jsxTagAPI(name string) string {
	if c.susp.suspense[name] {
		return "Suspense"
	}
	if api, ok := c.imports[name]; ok && (api == "StrictMode" || api == "Profiler") {
		return api
	}
	if i := strings.LastIndex(name, "."); i > 0 && c.alias != "" && name[:i] == c.alias {
		switch prop := name[i+1:]; prop {
		case "Suspense", "StrictMode", "Profiler":
			return prop
		}
	}
	return ""
}
