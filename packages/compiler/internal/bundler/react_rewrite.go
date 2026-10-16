package bundler

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
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
}

// structuralReactAPIs are React APIs lowered structurally (identity/passthrough
// or member rewrite) rather than a simple rename. React APIs that cannot be
// lowered cleanly at compile time (useReducer, useId, Children, cloneElement,
// isValidElement, Fragment-as-tag) are intentionally deferred to the
// shadcn/radix milestone instead of being shimmed in the runtime.
var structuralReactAPIs = map[string]bool{
	"memo":       true,
	"lazy":       true,
	"useContext": true,
}

// reactiveFactories are React APIs whose first destructured binding is a value
// getter in Krate terms. Bare reads of those bindings become calls.
var reactiveFactories = map[string]bool{
	"useState":   true,
	"useMemo":    true,
	"useReducer": true,
}

// reactAPI reports whether name is a known React API (rename or structural).
func reactAPI(name string) bool {
	if _, ok := reactNames[name]; ok {
		return true
	}
	return structuralReactAPIs[name]
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
	// fragmentNames holds local bindings (including the React alias) that
	// resolve to `React.Fragment`, so `<Fragment>` / `<React.Fragment>` can be
	// lowered to a real JSX fragment.
	fragmentNames map[string]bool
}

// localAPI resolves an identifier bound by a React named import to its React
// API name.
func (c *reactCtx) localAPI(name string) (string, bool) {
	api, ok := c.imports[name]
	return api, ok
}

// memberAPI reports the React API name when mem is `React.<prop>`.
func (c *reactCtx) memberAPI(mem *ast.MemberExpr) (string, bool) {
	if mem.Computed {
		return "", false
	}
	objID, ok := mem.Object.(*ast.Identifier)
	if !ok || c.alias == "" || objID.Name != c.alias {
		return "", false
	}
	propID, ok := mem.Property.(*ast.Identifier)
	if !ok {
		return "", false
	}
	if reactAPI(propID.Name) {
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
func RewriteReact(prog *ast.Program) {
	ctx := &reactCtx{imports: map[string]string{}, fragmentNames: map[string]bool{}}
	var toRemove []int

	for i, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		src := strings.Trim(imp.Source, "\"'")
		if src != "react" {
			continue
		}
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
	}
	if ctx.alias != "" {
		ctx.fragmentNames[ctx.alias] = true
	}

	for i := len(toRemove) - 1; i >= 0; i-- {
		idx := toRemove[i]
		prog.Body = append(prog.Body[:idx], prog.Body[idx+1:]...)
	}

	ctx.rewriteStmts(prog.Body, newScope(nil))
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
	api, isReactAPI := c.callAPI(e.Callee)

	// `memo`/`lazy` are identity wrappers; `useCallback` yields its function.
	switch api {
	case "memo", "lazy", "useCallback":
		if isReactAPI && len(e.Args) >= 1 {
			return c.rewriteExpr(e.Args[0], sc)
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
		}
	} else {
		e.Callee = c.rewriteExprCtx(e.Callee, sc, false)
	}
	for i, arg := range e.Args {
		e.Args[i] = c.rewriteExpr(arg, sc)
	}
	return e
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

// unitlessStyleProps are CSS properties whose numeric values must not get a px
// suffix (matching React's unitless allowlist).
var unitlessStyleProps = map[string]bool{
	"animation-iteration-count": true, "aspect-ratio": true, "border-image-outset": true,
	"border-image-slice": true, "border-image-width": true, "box-flex": true,
	"box-flex-group": true, "box-ordinal-group": true, "column-count": true,
	"columns": true, "flex": true, "flex-grow": true, "flex-positive": true,
	"flex-shrink": true, "flex-negative": true, "flex-order": true, "grid-area": true,
	"grid-column": true, "grid-column-end": true, "grid-column-span": true,
	"grid-column-start": true, "grid-row": true, "grid-row-end": true,
	"grid-row-span": true, "grid-row-start": true, "font-weight": true,
	"line-clamp": true, "line-height": true, "opacity": true, "order": true,
	"orphans": true, "tab-size": true, "widows": true, "z-index": true, "zoom": true,
	"fill-opacity": true, "flood-opacity": true, "stop-opacity": true,
	"stroke-dasharray": true, "stroke-dashoffset": true, "stroke-miterlimit": true,
	"stroke-opacity": true, "stroke-width": true,
}

// styleObjectToCSS folds a literal `style={{ ... }}` object into a CSS string,
// converting camelCase keys to kebab-case and adding `px` to bare numeric
// dimension values. Returns ok=false when the object is not fully literal.
func styleObjectToCSS(value ast.Expr) (string, bool) {
	obj, ok := value.(*ast.ObjectExpr)
	if !ok {
		return "", false
	}
	var parts []string
	for _, prop := range obj.Properties {
		if prop == nil || prop.Spread || prop.Key == "" {
			return "", false
		}
		lit, ok := prop.Value.(*ast.Literal)
		if !ok {
			return "", false
		}
		key := camelToKebab(prop.Key)
		val := lit.Value
		if lit.Kind == ast.NumberLit && !unitlessStyleProps[key] {
			val += "px"
		}
		parts = append(parts, key+":"+val)
	}
	return strings.Join(parts, ";"), true
}

// camelToKebab converts a camelCase CSS property name to its kebab-case form.
func camelToKebab(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
