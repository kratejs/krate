package bundler

import (
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/htmlspec"
)

// reactNames maps a React API name to the krate runtime identifier that
// replaces it. Every target is an existing global in the shared runtime chunk
// (createSignal, createEffect, createMemo, useRef, useCallback, forwardRef,
// createContext, h) - React compatibility is a pure compiler concern and adds
// no runtime code.
var reactNames = map[string]string{
	"useState":           "createSignal",
	"useEffect":          "createEffect",
	"useLayoutEffect":    "createEffect",
	"useInsertionEffect": "createEffect",
	"useMemo":            "createMemo",
	"useReducer":         "createReducer",
	"useRef":             "useRef",
	"useCallback":        "useCallback",
	"forwardRef":         "forwardRef",
	"createContext":      "createContext",
	"createElement":      "h",
	// useId is lowered by the IR builder to a per-instance build-time literal;
	// the marker identifier is resolved there (it has the instance context).
	"useId": "__krate_useId",
	// React DOM's requestFormReset is a runtime global (runtime/src/actions.ts).
	"requestFormReset": "requestFormReset",
}

// structuralReactAPIs are React APIs lowered structurally (identity/passthrough
// or member rewrite) rather than a simple rename.
var structuralReactAPIs = map[string]bool{
	"memo":       true,
	"lazy":       true,
	"useContext": true,
}

// compatReactNames are React APIs that cannot be lowered to a plain krate
// primitive but ARE provided by the `@krate/runtime/react` compatibility runtime. When
// one is imported or used via `React.<name>`, the rewriter renames it to a bare
// identifier and synthesizes an import from `@krate/runtime/react`, so the code
// resolves instead of dangling after the `react` import is removed. (APIs with
// true re-render semantics are the runtime's responsibility; unsupported ones
// surface diagnostics.)
var compatReactNames = map[string]bool{
	"Children":             true,
	"cloneElement":         true,
	"isValidElement":       true,
	"createRef":            true,
	"createPortal":         true,
	"flushSync":            true,
	"useSyncExternalStore": true,
	"useTransition":        true,
	"useDeferredValue":     true,
	"useOptimistic":        true,
	"useActionState":       true,
	"useFormStatus":        true,
	"useImperativeHandle":  true,
	"useDebugValue":        true,
	"use":                  true,
	"startTransition":      true,
	"Component":            true,
	"PureComponent":        true,
	"StrictMode":           true,
	"Profiler":             true,
}

// reactiveFactories are React APIs whose first destructured binding is a value
// getter in Krate terms. Bare reads of those bindings become calls.
var reactiveFactories = map[string]bool{
	"useState":       true,
	"useMemo":        true,
	"useReducer":     true,
	"useActionState": true,
	"useOptimistic":  true,
}

// reactAPI reports whether name is a known React API (rename, structural, or
// compat-runtime provided).
func reactAPI(name string) bool {
	if _, ok := reactNames[name]; ok {
		return true
	}
	return structuralReactAPIs[name] || compatReactNames[name]
}

// scopes

// scope tracks per-function reactive getter bindings for the bare-read pass so
// `count` (React style) becomes `count()` while shadowed names are untouched.
type scope struct {
	parent  *scope
	getters map[string]bool
	shadow  map[string]bool
}

func newScope(parent *scope) *scope {
	return &scope{parent: parent, getters: map[string]bool{}, shadow: map[string]bool{}}
}

func (s *scope) declareGetter(name string) {
	if name != "" {
		s.getters[name] = true
	}
}

// declareLocal marks a name as a non-getter local so it shadows any getter of
// the same name in an enclosing scope.
func (s *scope) declareLocal(name string) {
	if name != "" && !s.getters[name] {
		s.shadow[name] = true
	}
}

func (s *scope) isGetter(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.getters[name] {
			return true
		}
		if cur.shadow[name] {
			return false
		}
	}
	return false
}

// context

type reactCtx struct {
	imports map[string]string // local name -> React API name
	alias   string            // namespace/default alias for `React`
	// domAlias is the namespace/default alias for `react-dom` (ReactDOM).
	domAlias string
	// fragmentNames holds local bindings (including the React alias) that
	// resolve to `React.Fragment`, so `<Fragment>` / `<React.Fragment>` can be
	// lowered to a real JSX fragment.
	fragmentNames map[string]bool
	// jsxCall maps a local binding imported from `react/jsx-runtime` (jsx,
	// jsxs, jsxDEV) to its kind, so compiled React output - which calls
	// `jsx(type, props)` instead of writing JSX - is lowered to Krate elements.
	jsxCall map[string]string
	// jsxNS is a namespace import of `react/jsx-runtime` (e.g.
	// `import * as jsxRuntime`), whose `jsx`/`jsxs`/`jsxDEV` members are lowered.
	jsxNS string
	// compat holds the React API names that must be imported from `@krate/runtime/react`
	// because they resolve to the compatibility runtime rather than a krate
	// primitive.
	compat map[string]bool
	// formStatusSeq numbers lowered useFormStatus() mount refs per module and
	// formStatusPos records their positions for diagnostics.
	formStatusSeq int
	formStatusPos []ast.Pos
	// diags collects non-fatal diagnostics (positions only; the bundler adds
	// the file).
	diags []diag.Diagnostic
}

// localAPI resolves an identifier bound by a React named import to its React
// API name.
func (c *reactCtx) localAPI(name string) (string, bool) {
	api, ok := c.imports[name]
	return api, ok
}

// compatMember rewrites `React.<CompatAPI>` (e.g. `React.Children`) to a bare
// identifier provided by the `@krate/runtime/react` compatibility runtime, so nested
// member use (`React.Children.map`) keeps working after `React` is removed.
func (c *reactCtx) compatMember(mem *ast.MemberExpr) (ast.Expr, bool) {
	if mem.Computed {
		return nil, false
	}
	objID, ok := mem.Object.(*ast.Identifier)
	if !ok {
		return nil, false
	}
	propID, ok := mem.Property.(*ast.Identifier)
	if !ok {
		return nil, false
	}
	isReactMember := c.alias != "" && objID.Name == c.alias && compatReactNames[propID.Name]
	isDOMMember := c.domAlias != "" && objID.Name == c.domAlias && reactDOMAPI(propID.Name)
	if !isReactMember && !isDOMMember {
		return nil, false
	}
	c.compat[propID.Name] = true
	return &ast.Identifier{Position: mem.Position, Name: propID.Name}, true
}

// memberAPI reports the React API name when mem is `React.<prop>`.
func (c *reactCtx) memberAPI(mem *ast.MemberExpr) (string, bool) {
	if mem.Computed {
		return "", false
	}
	objID, ok := mem.Object.(*ast.Identifier)
	if !ok {
		return "", false
	}
	propID, ok := mem.Property.(*ast.Identifier)
	if !ok {
		return "", false
	}
	if c.alias != "" && objID.Name == c.alias && reactAPI(propID.Name) {
		return propID.Name, true
	}
	if c.domAlias != "" && objID.Name == c.domAlias && reactDOMAPI(propID.Name) {
		return propID.Name, true
	}
	return "", false
}

// callAPI resolves a call callee to the React API being invoked, if any.
func (c *reactCtx) callAPI(callee ast.Expr) (string, bool) {
	switch e := callee.(type) {
	case *ast.Identifier:
		return c.localAPI(e.Name)
	case *ast.MemberExpr:
		return c.memberAPI(e)
	}
	return "", false
}

// entry point

// RewriteReact transpiles React source to krate equivalents. It runs on every
// module (no opt-in): React imports and `React.*` member calls are mapped to
// existing krate runtime primitives, and bare reads of hook values are
// auto-called so unmodified React (`{count}`, `count + 1`) behaves correctly.
//
// It returns non-fatal diagnostics carrying positions but no file name.
func RewriteReact(prog *ast.Program) []diag.Diagnostic {
	ctx := &reactCtx{imports: map[string]string{}, fragmentNames: map[string]bool{}, jsxCall: map[string]string{}, compat: map[string]bool{}}
	var toRemove []int

	for i, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		src := strings.Trim(imp.Source, "\"'")
		switch src {
		case "react":
			toRemove = append(toRemove, i)
			if imp.Default != "" {
				ctx.alias = imp.Default
			}
			if imp.Namespace != "" {
				ctx.alias = imp.Namespace
			}
			for _, named := range imp.Named {
				local := named.Local
				if local == "" {
					local = named.Remote
				}
				if named.Remote == "default" || named.Remote == "*" || named.Remote == "React" {
					ctx.alias = local
					continue
				}
				if reactAPI(named.Remote) {
					ctx.imports[local] = named.Remote
				}
				if named.Remote == "Fragment" {
					ctx.fragmentNames[local] = true
				}
			}
		case "react-dom", "react-dom/client":
			// Krate does not ship react-dom. The DOM entry points React source uses
			// most often (portals and sync flushing) map to the compat runtime.
			toRemove = append(toRemove, i)
			if imp.Default != "" {
				ctx.domAlias = imp.Default
			}
			if imp.Namespace != "" {
				ctx.domAlias = imp.Namespace
			}
			for _, named := range imp.Named {
				local := named.Local
				if local == "" {
					local = named.Remote
				}
				if named.Remote == "default" || named.Remote == "*" || named.Remote == "ReactDOM" {
					ctx.domAlias = local
					continue
				}
				if reactDOMAPI(named.Remote) {
					ctx.imports[local] = named.Remote
				}
			}
		case "react/jsx-runtime", "react/jsx-dev-runtime":
			// Compiled React output (esbuild/tsc) calls `jsx`/`jsxs`/`jsxDEV`
			// from the automatic JSX runtime instead of emitting JSX. Lower those
			// calls to real JSX so the element pipeline can render them.
			toRemove = append(toRemove, i)
			if imp.Namespace != "" {
				ctx.jsxNS = imp.Namespace
			}
			if imp.Default != "" {
				ctx.jsxCall[imp.Default] = "jsx"
			}
			for _, named := range imp.Named {
				local := named.Local
				if local == "" {
					local = named.Remote
				}
				switch named.Remote {
				case "jsx", "jsxs", "jsxDEV":
					ctx.jsxCall[local] = named.Remote
				case "Fragment":
					ctx.fragmentNames[local] = true
				}
			}
		default:
			continue
		}
	}
	if ctx.alias != "" {
		ctx.fragmentNames[ctx.alias] = true
	}

	for i := len(toRemove) - 1; i >= 0; i-- {
		idx := toRemove[i]
		prog.Body = append(prog.Body[:idx], prog.Body[idx+1:]...)
	}

	ctx.rewriteStmts(prog.Body, newScope(nil))

	// React APIs that resolve to the compatibility runtime are pulled in from
	// `@krate/runtime/react` so they stay defined after the `react` import is removed.
	if len(ctx.compat) > 0 {
		names := make([]string, 0, len(ctx.compat))
		for name := range ctx.compat {
			names = append(names, name)
		}
		sort.Strings(names)
		named := make([]ast.NamedImport, 0, len(names))
		for _, name := range names {
			named = append(named, ast.NamedImport{Local: name, Remote: name})
		}
		imp := &ast.ImportStmt{Source: `"@krate/runtime/react"`, Named: named}
		prog.Body = append([]ast.Stmt{imp}, prog.Body...)
	}
	return ctx.diags
}

// reactDOMAPI reports the react-dom exports Krate lowers or provides.
func reactDOMAPI(name string) bool {
	switch name {
	case "createPortal", "flushSync", "useFormStatus", "requestFormReset":
		return true
	}
	return false
}

func (c *reactCtx) rewriteStmts(stmts []ast.Stmt, sc *scope) {
	for _, stmt := range stmts {
		c.rewriteStmt(stmt, sc)
	}
}

func (c *reactCtx) rewriteStmt(stmt ast.Stmt, sc *scope) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		s.Value = c.rewriteExpr(s.Value, sc)
	case *ast.VarStmt:
		c.rewriteVarStmt(s, sc)
	case *ast.ExprStmt:
		s.Expression = c.rewriteExpr(s.Expression, sc)
	case *ast.FnDecl:
		child := c.fnScope(s.Params, sc)
		c.rewriteStmts(s.Body, child)
		c.injectFormStatusRefs(s.Body)
	case *ast.ExportStmt:
		if s.Declaration != nil {
			c.rewriteStmt(s.Declaration, sc)
		}
	case *ast.IfStmt:
		s.Test = c.rewriteExpr(s.Test, sc)
		c.rewriteStmts(s.Consequent, sc)
		c.rewriteStmts(s.Alternate, sc)
	case *ast.BlockStmt:
		c.rewriteStmts(s.Body, newScope(sc))
	case *ast.ForStmt:
		if s.Init != nil {
			c.rewriteStmt(s.Init, sc)
		}
		if s.Test != nil {
			s.Test = c.rewriteExpr(s.Test, sc)
		}
		if s.Update != nil {
			s.Update = c.rewriteExpr(s.Update, sc)
		}
		c.rewriteStmts(s.Body, newScope(sc))
	case *ast.ForInStmt:
		if s.Left != nil {
			s.Left = c.rewriteExpr(s.Left, sc)
		}
		if s.Right != nil {
			s.Right = c.rewriteExpr(s.Right, sc)
		}
		c.rewriteStmts(s.Body, newScope(sc))
	case *ast.WhileStmt:
		if s.Test != nil {
			s.Test = c.rewriteExpr(s.Test, sc)
		}
		c.rewriteStmts(s.Body, newScope(sc))
	case *ast.DoWhileStmt:
		c.rewriteStmts(s.Body, newScope(sc))
		if s.Test != nil {
			s.Test = c.rewriteExpr(s.Test, sc)
		}
	case *ast.SwitchStmt:
		if s.Discriminant != nil {
			s.Discriminant = c.rewriteExpr(s.Discriminant, sc)
		}
		for _, cs := range s.Cases {
			if cs.Test != nil {
				cs.Test = c.rewriteExpr(cs.Test, sc)
			}
			c.rewriteStmts(cs.Body, sc)
		}
	case *ast.TryStmt:
		c.rewriteStmts(s.Body, sc)
		if s.Catch != nil {
			catchScope := newScope(sc)
			catchScope.declareLocal(s.Catch.Param)
			c.rewriteStmts(s.Catch.Body, catchScope)
		}
		c.rewriteStmts(s.Finally, sc)
	case *ast.ThrowStmt:
		s.Value = c.rewriteExpr(s.Value, sc)
	}
}

// fnScope builds a child scope declared with the function's parameter names so
// parameters shadow enclosing getters.
func (c *reactCtx) fnScope(params []*ast.Param, parent *scope) *scope {
	child := newScope(parent)
	for _, p := range params {
		if p == nil {
			continue
		}
		if p.Name != "" && p.Name != "{...}" {
			child.declareLocal(p.Name)
		}
	}
	return child
}

// rewriteVarStmt registers reactive-getter bindings (React hooks and krate
// factories) in the scope, then rewrites the initializers so React-style reads
// inside them become calls.
func (c *reactCtx) rewriteVarStmt(v *ast.VarStmt, sc *scope) {
	if c.lowerActionHook(v, sc) {
		return
	}
	// Lower `const [pending, start] = useTransition()` to a Krate signal plus a
	// starter function. The compat call would otherwise leave both bindings
	// undefined in the hydration scope.
	if len(v.Decls) == 1 {
		decl := v.Decls[0]
		if decl != nil && decl.IsDestructuring && len(decl.Names) >= 2 {
			if call, ok := decl.Init.(*ast.CallExpr); ok {
				if api, ok := c.callAPI(call.Callee); ok && api == "useTransition" {
					pending, start := decl.Names[0], decl.Names[1]
					setter := "__krate_transition_" + pending
					boolLit := func(b bool) ast.Expr {
						val := "false"
						if b {
							val = "true"
						}
						return &ast.Literal{Kind: ast.BoolLit, Value: val}
					}
					makeCall := func(name string, args ...ast.Expr) *ast.CallExpr {
						return &ast.CallExpr{Callee: &ast.Identifier{Name: name}, Args: args}
					}
					v.Decls = []*ast.VarDecl{
						{IsDestructuring: true, Names: []string{pending, setter}, Init: makeCall("createSignal", boolLit(false))},
						{Name: start, Init: &ast.ArrowFn{Params: []*ast.Param{{Name: "fn"}}, Body: []ast.Stmt{
							&ast.ExprStmt{Expression: makeCall(setter, boolLit(true))},
							&ast.ExprStmt{Expression: makeCall("fn")},
							&ast.ExprStmt{Expression: makeCall(setter, boolLit(false))},
						}}},
					}
					sc.declareGetter(pending)
					sc.declareLocal(setter)
					sc.declareLocal(start)
					return
				}
			}
		}
	}
	for _, decl := range v.Decls {
		if decl == nil {
			continue
		}
		if decl.Init != nil {
			if c.isReactiveFactory(decl.Init) {
				if decl.IsDestructuring && len(decl.Names) >= 1 {
					sc.declareGetter(decl.Names[0])
					for _, n := range decl.Names[1:] {
						sc.declareLocal(n)
					}
				} else if decl.Name != "" {
					sc.declareGetter(decl.Name)
				}
			} else if !decl.IsDestructuring {
				sc.declareLocal(decl.Name)
			}
		}
		if !decl.IsDestructuring {
			if decl.Name != "" {
				sc.declareLocal(decl.Name)
			}
		} else {
			for _, n := range decl.Names {
				if !sc.getters[n] {
					sc.declareLocal(n)
				}
			}
		}
		decl.Init = c.rewriteExpr(decl.Init, sc)
	}
}

// isReactiveFactory reports whether an initializer is a reactive factory whose
// first binding is a getter (React useState/useMemo or krate createSignal/
// createMemo).
func (c *reactCtx) isReactiveFactory(init ast.Expr) bool {
	call, ok := init.(*ast.CallExpr)
	if !ok {
		return false
	}
	if api, ok := c.callAPI(call.Callee); ok {
		return reactiveFactories[api]
	}
	if id, ok := call.Callee.(*ast.Identifier); ok {
		switch id.Name {
		case "createSignal", "createMemo", "createReducer":
			return true
		}
	}
	return false
}

// expression rewriting

// rewriteExpr rewrites an expression in value position: bare reads of reactive
// getters become calls.
func (c *reactCtx) rewriteExpr(expr ast.Expr, sc *scope) ast.Expr {
	return c.rewriteExprCtx(expr, sc, true)
}

func (c *reactCtx) rewriteExprCtx(expr ast.Expr, sc *scope, valuePos bool) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.Identifier:
		return c.rewriteIdentifier(e, sc, valuePos)

	case *ast.CallExpr:
		return c.rewriteCall(e, sc)

	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if prop == nil {
				continue
			}
			prop.Value = c.rewriteExpr(prop.Value, sc)
		}
	case *ast.ArrayExpr:
		for i, elem := range e.Elements {
			e.Elements[i] = c.rewriteExpr(elem, sc)
		}
	case *ast.MemberExpr:
		if id, ok := c.compatMember(e); ok {
			return id
		}
		e.Object = c.rewriteExpr(e.Object, sc)
		if e.Computed {
			e.Property = c.rewriteExpr(e.Property, sc)
		}
	case *ast.BinaryExpr:
		e.Left = c.rewriteExpr(e.Left, sc)
		e.Right = c.rewriteExpr(e.Right, sc)
	case *ast.UnaryExpr:
		e.Arg = c.rewriteExpr(e.Arg, sc)
	case *ast.ConditionalExpr:
		e.Test = c.rewriteExpr(e.Test, sc)
		e.Consequent = c.rewriteExpr(e.Consequent, sc)
		e.Alternate = c.rewriteExpr(e.Alternate, sc)
	case *ast.TypeAssertion:
		e.Expr = c.rewriteExpr(e.Expr, sc)
	case *ast.ArrowFn:
		child := c.fnScope(e.Params, sc)
		for _, p := range e.Params {
			if p != nil && p.Default != nil {
				p.Default = c.rewriteExpr(p.Default, sc)
			}
		}
		c.rewriteStmts(e.Body, child)
		c.injectFormStatusRefs(e.Body)
	case *ast.TemplateExpr:
		for i, part := range e.Parts {
			e.Parts[i] = c.rewriteExpr(part, sc)
		}
	case *ast.NewExpr:
		e.Callee = c.rewriteExpr(e.Callee, sc)
		for i, arg := range e.Args {
			e.Args[i] = c.rewriteExpr(arg, sc)
		}
	case *ast.AwaitExpr:
		e.Arg = c.rewriteExpr(e.Arg, sc)
	case *ast.DynamicImport:
		e.Arg = c.rewriteExpr(e.Arg, sc)
	case *ast.JSXElement:
		if c.isFragmentTag(e.Opening.Name) {
			return c.lowerFragment(e, sc)
		}
		c.rewriteJSXElement(e, sc)
	case *ast.JSXFragment:
		for i, child := range e.Children {
			e.Children[i] = c.rewriteJSXChild(child, sc)
		}
	}

	return expr
}

// rewriteIdentifier auto-calls reactive getters in value position and renames
// React named-import identifiers to their krate targets.
func (c *reactCtx) rewriteIdentifier(e *ast.Identifier, sc *scope, valuePos bool) ast.Expr {
	if api, ok := c.localAPI(e.Name); ok {
		if target, ok := reactNames[api]; ok {
			e.Name = target
			return e
		}
		if compatReactNames[api] {
			c.compat[api] = true
			e.Name = api
			return e
		}
		// Structural API used as a value (e.g. `memo`) - leave the local name;
		// call sites are handled by rewriteCall.
		return e
	}
	if valuePos && sc.isGetter(e.Name) {
		return &ast.CallExpr{Position: e.Position, Callee: e}
	}
	return e
}

// rewriteCall applies React hook lowerings and recursively rewrites arguments.
func (c *reactCtx) rewriteCall(e *ast.CallExpr, sc *scope) ast.Expr {
	// Compiled React output calls jsx()/jsxs()/jsxDEV() instead of writing JSX;
	// lower those to real JSX elements before any other handling.
	if c.jsxKind(e.Callee) {
		if lowered, ok := c.lowerJSXCall(e, sc); ok {
			return lowered
		}
	}

	api, isReactAPI := c.callAPI(e.Callee)

	// React runs an effect with an empty dependency array exactly once. Lower
	// that case to onMount (with cleanup registration) instead of createEffect,
	// which would re-run it whenever any signal it reads changes.
	if isReactAPI && (api == "useEffect" || api == "useLayoutEffect" || api == "useInsertionEffect") && len(e.Args) >= 2 {
		if deps, ok := e.Args[1].(*ast.ArrayExpr); ok && len(deps.Elements) == 0 {
			fn := c.rewriteExpr(e.Args[0], sc)
			cleanup := &ast.VarStmt{Decls: []*ast.VarDecl{{Name: "__krate_effect_cleanup", Init: &ast.CallExpr{Callee: fn}}}}
			guard := &ast.IfStmt{
				Test: &ast.BinaryExpr{
					Left:  &ast.UnaryExpr{Op: "typeof", Arg: &ast.Identifier{Name: "__krate_effect_cleanup"}},
					Op:    "===",
					Right: &ast.Literal{Kind: ast.StringLit, Value: "function"},
				},
				Consequent: []ast.Stmt{&ast.ExprStmt{Expression: &ast.CallExpr{
					Callee: &ast.Identifier{Name: "onCleanup"},
					Args:   []ast.Expr{&ast.Identifier{Name: "__krate_effect_cleanup"}},
				}}},
			}
			return &ast.CallExpr{
				Position: e.Position,
				Callee:   &ast.Identifier{Name: "onMount"},
				Args: []ast.Expr{&ast.ArrowFn{
					Position: e.Position,
					Body:     []ast.Stmt{cleanup, guard},
				}},
			}
		}
		// Non-empty dependency arrays: createEffectDeps compares the deps and
		// only runs the callback when one changed. The deps thunk re-reads the
		// signal getters so the effect subscribes to them.
		if _, ok := e.Args[1].(*ast.ArrayExpr); ok {
			fn := c.rewriteExpr(e.Args[0], sc)
			deps := c.rewriteExpr(e.Args[1], sc)
			thunk := &ast.ArrowFn{Position: e.Position, Body: []ast.Stmt{&ast.ReturnStmt{Value: deps}}}
			return &ast.CallExpr{Position: e.Position, Callee: &ast.Identifier{Name: "createEffectDeps"}, Args: []ast.Expr{fn, thunk}}
		}
	}

	if isReactAPI && api == "useMemo" && len(e.Args) >= 2 {
		if _, ok := e.Args[1].(*ast.ArrayExpr); ok {
			fn := c.rewriteExpr(e.Args[0], sc)
			deps := c.rewriteExpr(e.Args[1], sc)
			thunk := &ast.ArrowFn{Position: e.Position, Body: []ast.Stmt{&ast.ReturnStmt{Value: deps}}}
			return &ast.CallExpr{Position: e.Position, Callee: &ast.Identifier{Name: "createMemoDeps"}, Args: []ast.Expr{fn, thunk}}
		}
	}

	// `memo`/`lazy` are identity wrappers; `useCallback` yields its function.
	switch api {
	case "memo", "lazy", "useCallback":
		if isReactAPI && len(e.Args) >= 1 {
			return c.rewriteExpr(e.Args[0], sc)
		}
	case "forwardRef":
		// forwardRef((props, ref) => ...) is lowered to (props) => ... with
		// reads of `ref` rewritten to `props.ref`. The call site then passes
		// `ref` as a normal prop and the compiler binds it live at the element,
		// rather than const-folding the ref object into an HTML attribute.
		if isReactAPI && len(e.Args) == 1 {
			if arrow, ok := e.Args[0].(*ast.ArrowFn); ok {
				lowerForwardRefArrow(arrow)
			}
			return c.rewriteExpr(e.Args[0], sc)
		}
	case "createRef":
		// createRef() is a stable { current: null } object.
		if isReactAPI {
			return &ast.ObjectExpr{
				Position: e.Position,
				Properties: []*ast.ObjectProp{{
					Key:   "current",
					Value: &ast.Literal{Kind: ast.NullLit, Value: "null"},
				}},
			}
		}
	case "useRef":
		if isReactAPI {
			var initVal ast.Expr = &ast.Literal{Kind: ast.NullLit, Value: "null"}
			if len(e.Args) >= 1 {
				initVal = c.rewriteExpr(e.Args[0], sc)
			}
			return &ast.ObjectExpr{
				Position:   e.Position,
				Properties: []*ast.ObjectProp{{Key: "current", Value: initVal}},
			}
		}
	case "useContext":
		if isReactAPI && len(e.Args) >= 1 {
			// useContext(Ctx) -> Ctx.useContext()
			obj := c.rewriteExpr(e.Args[0], sc)
			return &ast.CallExpr{
				Position: e.Position,
				Callee: &ast.MemberExpr{
					Object:   obj,
					Property: &ast.Identifier{Name: "useContext"},
				},
			}
		}
	}

	// Rename a React callee (`useState(...)` or `React.useState(...)`) to its
	// krate target by replacing the whole callee expression with an identifier.
	if isReactAPI {
		if target, ok := reactNames[api]; ok {
			e.Callee = &ast.Identifier{Name: target}
		} else if compatReactNames[api] {
			c.compat[api] = true
			e.Callee = &ast.Identifier{Name: api}
		}
	} else {
		e.Callee = c.rewriteExprCtx(e.Callee, sc, false)
	}
	for i, arg := range e.Args {
		e.Args[i] = c.rewriteExpr(arg, sc)
	}
	return e
}

// jsxKind reports whether callee is a react/jsx-runtime element factory (either
// a named import of jsx/jsxs/jsxDEV or a member of a jsx-runtime namespace).
func (c *reactCtx) jsxKind(callee ast.Expr) bool {
	switch e := callee.(type) {
	case *ast.Identifier:
		_, ok := c.jsxCall[e.Name]
		return ok
	case *ast.MemberExpr:
		if e.Computed || c.jsxNS == "" {
			return false
		}
		obj, ok := e.Object.(*ast.Identifier)
		if !ok || obj.Name != c.jsxNS {
			return false
		}
		prop, ok := e.Property.(*ast.Identifier)
		if !ok {
			return false
		}
		switch prop.Name {
		case "jsx", "jsxs", "jsxDEV":
			return true
		}
	}
	return false
}

// lowerJSXCall converts `jsx(type, props[, key])` into a JSX element (or
// fragment). It returns ok=false when the type is dynamic or the props are not
// an object literal, leaving the call for the runtime shim.
func (c *reactCtx) lowerJSXCall(call *ast.CallExpr, sc *scope) (ast.Expr, bool) {
	if len(call.Args) < 2 {
		return nil, false
	}
	typeExpr := call.Args[0]
	propsExpr := call.Args[1]

	if id, ok := typeExpr.(*ast.Identifier); ok && c.fragmentNames[id.Name] {
		return &ast.JSXFragment{Position: call.Position, Children: c.jsxChildrenFromProps(propsExpr, sc)}, true
	}

	name, ok := jsxTypeName(typeExpr)
	if !ok {
		return nil, false
	}

	obj, ok := propsExpr.(*ast.ObjectExpr)
	if !ok {
		return nil, false
	}

	attrs := make([]*ast.JSXAttr, 0, len(obj.Properties))
	var children ast.Expr
	hasChildren := false
	for _, prop := range obj.Properties {
		if prop == nil {
			continue
		}
		if prop.Spread {
			attrs = append(attrs, &ast.JSXAttr{Spread: true, Value: c.rewriteExpr(prop.Value, sc)})
			continue
		}
		switch prop.Key {
		case "children":
			children = prop.Value
			hasChildren = true
		case "key":
			// React reconciliation key; Krate drops it (position/marker identity).
		default:
			attrs = append(attrs, &ast.JSXAttr{Name: prop.Key, Value: c.rewriteExpr(prop.Value, sc)})
		}
	}

	el := &ast.JSXElement{
		Position: call.Position,
		Opening:  &ast.JSXOpening{Name: name, Attributes: attrs, SelfClosing: !hasChildren},
	}
	if hasChildren {
		el.Children = c.jsxChildrenFromExpr(children, sc)
	}
	return el, true
}

// jsxChildrenFromProps extracts the `children` prop of a jsx() props object.
func (c *reactCtx) jsxChildrenFromProps(props ast.Expr, sc *scope) []ast.JSXChild {
	obj, ok := props.(*ast.ObjectExpr)
	if !ok {
		return nil
	}
	for _, prop := range obj.Properties {
		if prop != nil && !prop.Spread && prop.Key == "children" {
			return c.jsxChildrenFromExpr(prop.Value, sc)
		}
	}
	return nil
}

// jsxChildrenFromExpr converts a `children` value (a single node or an array)
// into JSX children.
func (c *reactCtx) jsxChildrenFromExpr(expr ast.Expr, sc *scope) []ast.JSXChild {
	if expr == nil {
		return nil
	}
	if arr, ok := expr.(*ast.ArrayExpr); ok {
		out := make([]ast.JSXChild, 0, len(arr.Elements))
		for _, el := range arr.Elements {
			out = append(out, c.jsxChildFromExpr(el, sc)...)
		}
		return out
	}
	return c.jsxChildFromExpr(expr, sc)
}

func (c *reactCtx) jsxChildFromExpr(expr ast.Expr, sc *scope) []ast.JSXChild {
	switch e := expr.(type) {
	case *ast.JSXElement:
		return []ast.JSXChild{&ast.JSXElementChild{Element: e}}
	case *ast.JSXFragment:
		return []ast.JSXChild{&ast.JSXFragmentChild{Fragment: e}}
	case *ast.ArrayExpr:
		var out []ast.JSXChild
		for _, el := range e.Elements {
			out = append(out, c.jsxChildFromExpr(el, sc)...)
		}
		return out
	default:
		return []ast.JSXChild{&ast.JSXExprContainer{Expression: c.rewriteExpr(expr, sc)}}
	}
}

// jsxTypeName resolves a jsx() type argument to a JSX tag name: a string
// literal ("div"), an identifier (Foo), or a dotted member (Foo.Bar).
func jsxTypeName(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.Identifier:
		return e.Name, true
	case *ast.Literal:
		if e.Kind == ast.StringLit {
			return e.Value, true
		}
	case *ast.MemberExpr:
		if e.Computed {
			return "", false
		}
		obj, ok := jsxTypeName(e.Object)
		if !ok {
			return "", false
		}
		prop, ok := e.Property.(*ast.Identifier)
		if !ok {
			return "", false
		}
		return obj + "." + prop.Name, true
	}
	return "", false
}

// isFragmentTag reports whether a JSX tag name resolves to React.Fragment:
// either a `<Fragment>` named import or `<React.Fragment>` (and any aliased
// namespace). Plain `<>...</>` is already a JSXFragment and never reaches here.
func (c *reactCtx) isFragmentTag(name string) bool {
	if c.fragmentNames[name] {
		return true
	}
	if i := strings.LastIndex(name, "."); i >= 0 {
		obj := name[:i]
		prop := name[i+1:]
		return prop == "Fragment" && c.fragmentNames[obj]
	}
	return false
}

// lowerFragment converts `<Fragment>...</Fragment>` / `<React.Fragment>` into a
// real JSX fragment, which the existing fragment pipeline already handles.
func (c *reactCtx) lowerFragment(el *ast.JSXElement, sc *scope) ast.Expr {
	for i, child := range el.Children {
		el.Children[i] = c.rewriteJSXChild(child, sc)
	}
	return &ast.JSXFragment{Position: el.Position, Children: el.Children}
}

func (c *reactCtx) rewriteJSXElement(el *ast.JSXElement, sc *scope) {
	if el.Opening != nil {
		kept := el.Opening.Attributes[:0]
		for _, attr := range el.Opening.Attributes {
			if attr == nil {
				continue
			}
			// React-only directives carry no DOM meaning; drop them so no
			// render path (static SSR, IR tree, hydration) can emit them.
			if attr.Name == "key" || attr.Name == "suppressHydrationWarning" {
				continue
			}
			if attr.Name == "style" {
				if css, ok := styleObjectToCSS(attr.Value); ok {
					attr.Value = &ast.Literal{Kind: ast.StringLit, Value: css}
					kept = append(kept, attr)
					continue
				}
			}
			if attr.Value != nil {
				attr.Value = c.rewriteExpr(attr.Value, sc)
			}
			kept = append(kept, attr)
		}
		el.Opening.Attributes = kept
	}
	for i, child := range el.Children {
		el.Children[i] = c.rewriteJSXChild(child, sc)
	}
}

func (c *reactCtx) rewriteJSXChild(child ast.JSXChild, sc *scope) ast.JSXChild {
	switch ch := child.(type) {
	case *ast.JSXExprContainer:
		ch.Expression = c.rewriteExpr(ch.Expression, sc)
		return ch
	case *ast.JSXElementChild:
		if el, ok := c.rewriteExpr(ch.Element, sc).(*ast.JSXElement); ok {
			ch.Element = el
		}
		return ch
	case *ast.JSXFragmentChild:
		if frag, ok := c.rewriteExpr(ch.Fragment, sc).(*ast.JSXFragment); ok {
			ch.Fragment = frag
		}
		return ch
	}
	return child
}

// style objects

// styleObjectToCSS folds a literal `style={{ ... }}` object into a CSS string
// using the shared htmlspec rules (hyphenation, vendor prefixes, unitless
// properties, custom properties, skipped null/boolean values). Returns
// ok=false when the object is not fully literal.
func styleObjectToCSS(value ast.Expr) (string, bool) {
	obj, ok := value.(*ast.ObjectExpr)
	if !ok {
		return "", false
	}
	entries := make([]htmlspec.StyleEntry, 0, len(obj.Properties))
	for _, prop := range obj.Properties {
		if prop == nil || prop.Spread || prop.Key == "" {
			return "", false
		}
		lit, ok := prop.Value.(*ast.Literal)
		if !ok {
			return "", false
		}
		v := htmlspec.StrVal(lit.Value)
		switch lit.Kind {
		case ast.NumberLit:
			v = htmlspec.Value{Kind: htmlspec.Number, Str: lit.Value}
		case ast.BoolLit:
			v = htmlspec.BoolVal(lit.Value == "true")
		case ast.NullLit:
			v = htmlspec.Value{Kind: htmlspec.Null}
		case ast.RegexpLit:
			return "", false
		}
		entries = append(entries, htmlspec.StyleEntry{Key: prop.Key, Value: v})
	}
	return htmlspec.Style(entries), true
}

// lowerForwardRefArrow rewrites forwardRef((props, ref) => body) into
// (props) => body[props.ref/ref]. It leaves the call to the compat runtime when
// either parameter is not a plain identifier (for example a destructured props
// pattern), since there is no named props object to read the ref from.
func lowerForwardRefArrow(arrow *ast.ArrowFn) bool {
	if arrow == nil || len(arrow.Params) < 2 {
		return false
	}
	propsParam, refParam := arrow.Params[0], arrow.Params[1]
	if !plainParam(propsParam) || !plainParam(refParam) {
		return false
	}
	arrow.Params = arrow.Params[:1]
	replaceRefIdentsStmts(arrow.Body, refParam.Name, propsParam.Name)
	return true
}

func plainParam(p *ast.Param) bool {
	return p != nil && p.Name != "" && p.Name != "{...}" && p.Name != "[...]" && p.Pattern == "" && !p.IsRest
}

func replaceRefIdentsStmts(stmts []ast.Stmt, name, propsName string) {
	for _, stmt := range stmts {
		replaceRefIdentsStmt(stmt, name, propsName)
	}
}

func replaceRefIdentsStmt(stmt ast.Stmt, name, propsName string) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		s.Expression = replaceRefIdentsExpr(s.Expression, name, propsName)
	case *ast.ReturnStmt:
		s.Value = replaceRefIdentsExpr(s.Value, name, propsName)
	case *ast.VarStmt:
		for _, d := range s.Decls {
			d.Init = replaceRefIdentsExpr(d.Init, name, propsName)
		}
	case *ast.IfStmt:
		s.Test = replaceRefIdentsExpr(s.Test, name, propsName)
		replaceRefIdentsStmts(s.Consequent, name, propsName)
		replaceRefIdentsStmts(s.Alternate, name, propsName)
	case *ast.BlockStmt:
		replaceRefIdentsStmts(s.Body, name, propsName)
	case *ast.ForStmt:
		if s.Init != nil {
			replaceRefIdentsStmt(s.Init, name, propsName)
		}
		s.Test = replaceRefIdentsExpr(s.Test, name, propsName)
		s.Update = replaceRefIdentsExpr(s.Update, name, propsName)
		replaceRefIdentsStmts(s.Body, name, propsName)
	}
}

func replaceRefIdentsExpr(expr ast.Expr, name, propsName string) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if e.Name == name {
			return &ast.MemberExpr{
				Position: e.Position,
				Object:   &ast.Identifier{Position: e.Position, Name: propsName},
				Property: &ast.Identifier{Position: e.Position, Name: "ref"},
			}
		}
	case *ast.MemberExpr:
		e.Object = replaceRefIdentsExpr(e.Object, name, propsName)
		if e.Computed {
			e.Property = replaceRefIdentsExpr(e.Property, name, propsName)
		}
	case *ast.CallExpr:
		e.Callee = replaceRefIdentsExpr(e.Callee, name, propsName)
		for i, a := range e.Args {
			e.Args[i] = replaceRefIdentsExpr(a, name, propsName)
		}
	case *ast.BinaryExpr:
		e.Left = replaceRefIdentsExpr(e.Left, name, propsName)
		e.Right = replaceRefIdentsExpr(e.Right, name, propsName)
	case *ast.UnaryExpr:
		e.Arg = replaceRefIdentsExpr(e.Arg, name, propsName)
	case *ast.ConditionalExpr:
		e.Test = replaceRefIdentsExpr(e.Test, name, propsName)
		e.Consequent = replaceRefIdentsExpr(e.Consequent, name, propsName)
		e.Alternate = replaceRefIdentsExpr(e.Alternate, name, propsName)
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p != nil {
				p.Value = replaceRefIdentsExpr(p.Value, name, propsName)
			}
		}
	case *ast.ArrayExpr:
		for i, el := range e.Elements {
			e.Elements[i] = replaceRefIdentsExpr(el, name, propsName)
		}
	case *ast.TemplateExpr:
		for i, p := range e.Parts {
			e.Parts[i] = replaceRefIdentsExpr(p, name, propsName)
		}
	case *ast.ArrowFn:
		for _, p := range e.Params {
			if p != nil && p.Name == name {
				return e
			}
		}
		replaceRefIdentsStmts(e.Body, name, propsName)
	case *ast.JSXElement:
		replaceRefIdentsJSXElement(e, name, propsName)
	case *ast.JSXFragment:
		for _, c := range e.Children {
			replaceRefIdentsJSXChild(c, name, propsName)
		}
	case *ast.NewExpr:
		e.Callee = replaceRefIdentsExpr(e.Callee, name, propsName)
		for i, a := range e.Args {
			e.Args[i] = replaceRefIdentsExpr(a, name, propsName)
		}
	case *ast.TypeAssertion:
		e.Expr = replaceRefIdentsExpr(e.Expr, name, propsName)
	case *ast.AwaitExpr:
		e.Arg = replaceRefIdentsExpr(e.Arg, name, propsName)
	case *ast.DynamicImport:
		e.Arg = replaceRefIdentsExpr(e.Arg, name, propsName)
	}
	return expr
}

func replaceRefIdentsJSXElement(el *ast.JSXElement, name, propsName string) {
	if el == nil {
		return
	}
	if el.Opening != nil {
		for _, a := range el.Opening.Attributes {
			if a != nil {
				a.Value = replaceRefIdentsExpr(a.Value, name, propsName)
			}
		}
	}
	for _, c := range el.Children {
		replaceRefIdentsJSXChild(c, name, propsName)
	}
}

func replaceRefIdentsJSXChild(child ast.JSXChild, name, propsName string) {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		c.Expression = replaceRefIdentsExpr(c.Expression, name, propsName)
	case *ast.JSXElementChild:
		replaceRefIdentsJSXElement(c.Element, name, propsName)
	case *ast.JSXFragmentChild:
		if c.Fragment != nil {
			for _, fc := range c.Fragment.Children {
				replaceRefIdentsJSXChild(fc, name, propsName)
			}
		}
	}
}
