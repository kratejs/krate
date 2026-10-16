package irtree

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/csssignals"
	"github.com/kratejs/krate/packages/compiler/internal/sigutil"
)

// dropVarDecls removes `var <name>=…` declarations whose name is a scope handle
// (they are compile-time state, not runtime locals).
func dropVarDecls(decls []string, names map[string]scopeRef) []string {
	if len(decls) == 0 || len(names) == 0 {
		return decls
	}
	out := make([]string, 0, len(decls))
	for _, d := range decls {
		drop := false
		for name := range names {
			if strings.HasPrefix(d, "var "+name+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, d)
		}
	}
	return out
}

// cloneScopeMap copies a context-scope map (nil-safe) for nested scoping.
func cloneScopeMap(m map[string]scopeRef) map[string]scopeRef {
	out := make(map[string]scopeRef, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// isCSSFactoryCall reports whether an expression is a call to one of the CSS
// scope factories.
func isCSSFactoryCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return false
	}
	switch id.Name {
	case "createCSSChoice", "createCSSToggle", "createCSSFlags", "createCSSGroup", "createCSSRange", "createCSSStack":
		return true
	}
	return false
}

// hasScopeHelperSpread reports whether a component's returned JSX contains a
// scope-helper spread (`{...x.trigger(...)}` etc.). Such components must be
// built through the slot tree (not SSR-evaluated) so the spread is rewritten.
func hasScopeHelperSpread(fn *ast.FnDecl) bool {
	ret := findReturnStmt(fn.Body)
	if ret == nil || ret.Value == nil {
		return false
	}
	return exprHasScopeSpread(ret.Value)
}

func exprHasScopeSpread(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.JSXElement:
		if e == nil {
			return false
		}
		for _, attr := range e.Opening.Attributes {
			if attr != nil && attr.Spread && isScopeHelperExpr(attr.Value) {
				return true
			}
		}
		for _, child := range e.Children {
			if childHasScopeSpread(child) {
				return true
			}
		}
	case *ast.JSXFragment:
		for _, child := range e.Children {
			if childHasScopeSpread(child) {
				return true
			}
		}
	case *ast.TypeAssertion:
		return exprHasScopeSpread(e.Expr)
	case *ast.ConditionalExpr:
		return exprHasScopeSpread(e.Consequent) || exprHasScopeSpread(e.Alternate)
	case *ast.BinaryExpr:
		return exprHasScopeSpread(e.Left) || exprHasScopeSpread(e.Right)
	}
	return false
}

func childHasScopeSpread(child ast.JSXChild) bool {
	switch c := child.(type) {
	case *ast.JSXElementChild:
		return exprHasScopeSpread(c.Element)
	case *ast.JSXFragmentChild:
		return exprHasScopeSpread(c.Fragment)
	case *ast.JSXExprContainer:
		return exprHasScopeSpread(c.Expression)
	}
	return false
}

// isScopeHelperExpr reports whether an expression is `X.trigger(...)` /
// `.panel(...)` / `.close()` / `.current()` (an unresolved check; the object may
// be a local or `props.<name>`).
func isScopeHelperExpr(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return false
	}
	prop, ok := mem.Property.(*ast.Identifier)
	if !ok {
		return false
	}
	switch prop.Name {
	case "trigger", "panel", "close", "current":
		return true
	}
	return false
}

// scopeRef identifies a CSS scope owned by some component instance: the analyzer
// that declared it, the scope itself, and the owning instance token (used for
// controller ids). It is what a scope handle resolves to at build time.
type scopeRef struct {
	analyzer *csssignals.Analyzer
	scope    *csssignals.Scope
	token    string
}

// providedScopeContext returns the context name and the local scope-handle
// variable a component provides via `<Ctx.Provider value={local}>` in its
// returned JSX (searching nested elements/fragments).
func providedScopeContext(fn *ast.FnDecl) (string, string, bool) {
	ret := findReturnStmt(fn.Body)
	if ret == nil || ret.Value == nil {
		return "", "", false
	}
	return findProviderContext(ret.Value)
}

func findProviderContext(expr ast.Expr) (string, string, bool) {
	switch e := expr.(type) {
	case *ast.JSXElement:
		if e == nil || e.Opening == nil {
			return "", "", false
		}
		if strings.HasSuffix(e.Opening.Name, ".Provider") {
			ctx := strings.TrimSuffix(e.Opening.Name, ".Provider")
			for _, attr := range e.Opening.Attributes {
				if attr == nil || attr.Name != "value" || attr.Value == nil {
					continue
				}
				if id, ok := attr.Value.(*ast.Identifier); ok {
					return ctx, id.Name, true
				}
			}
		}
		for _, child := range e.Children {
			if ctx, local, ok := providerContextInChild(child); ok {
				return ctx, local, true
			}
		}
	case *ast.JSXFragment:
		for _, child := range e.Children {
			if ctx, local, ok := providerContextInChild(child); ok {
				return ctx, local, true
			}
		}
	case *ast.TypeAssertion:
		return findProviderContext(e.Expr)
	case *ast.ConditionalExpr:
		if ctx, local, ok := findProviderContext(e.Consequent); ok {
			return ctx, local, true
		}
		return findProviderContext(e.Alternate)
	case *ast.BinaryExpr:
		if ctx, local, ok := findProviderContext(e.Left); ok {
			return ctx, local, true
		}
		return findProviderContext(e.Right)
	}
	return "", "", false
}

func providerContextInChild(child ast.JSXChild) (string, string, bool) {
	switch c := child.(type) {
	case *ast.JSXElementChild:
		return findProviderContext(c.Element)
	case *ast.JSXFragmentChild:
		return findProviderContext(c.Fragment)
	}
	return "", "", false
}

// contextReadName returns the context identifier of a `Ctx.useContext()` read
// (the lowered form of `useContext(Ctx)`), or "".
func contextReadName(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return ""
	}
	prop, ok := mem.Property.(*ast.Identifier)
	if !ok || (prop.Name != "useContext" && prop.Name != "use") {
		return ""
	}
	if id, ok := mem.Object.(*ast.Identifier); ok {
		return id.Name
	}
	return ""
}

// registerContextReads maps `const s = Ctx.useContext()` locals to the scope
// handle currently provided for that context, so `s.trigger(...)` resolves.
func (b *builder) registerContextReads(fn *ast.FnDecl) {
	if b.contextScopes == nil {
		return
	}
	var walk func([]ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.VarStmt:
				for _, decl := range s.Decls {
					if decl.IsDestructuring || decl.Name == "" || decl.Init == nil {
						continue
					}
					ctx := contextReadName(decl.Init)
					if ctx == "" {
						continue
					}
					if ref, ok := b.contextScopes[ctx]; ok {
						b.scopeHandles[decl.Name] = ref
					}
				}
			case *ast.BlockStmt:
				walk(s.Body)
			case *ast.IfStmt:
				walk(s.Consequent)
				walk(s.Alternate)
			case *ast.ForStmt:
				walk(s.Body)
			case *ast.WhileStmt:
				walk(s.Body)
			case *ast.DoWhileStmt:
				walk(s.Body)
			case *ast.TryStmt:
				walk(s.Body)
				if s.Catch != nil {
					walk(s.Catch.Body)
				}
				walk(s.Finally)
			case *ast.SwitchStmt:
				for _, c := range s.Cases {
					walk(c.Body)
				}
			}
		}
	}
	walk(fn.Body)
}

// registerScopeLocals records the scope handles declared in a component body so
// spread helpers on their names resolve during the component's own build.
func (b *builder) registerScopeLocals(fn *ast.FnDecl, analyzer *csssignals.Analyzer, token string) {
	if analyzer == nil {
		return
	}
	for _, d := range sigutil.Find(fn.Body, true) {
		if d.CSSKind == sigutil.CSSKindNone {
			continue
		}
		if s := analyzer.ScopeByVar(d.Name); s != nil {
			b.scopeHandles[d.Name] = scopeRef{analyzer: analyzer, scope: s, token: token}
		}
	}
}

// resolveScopeExpr resolves an identifier or `props.<name>` to its scope handle.
func (b *builder) resolveScopeExpr(expr ast.Expr) (scopeRef, bool) {
	if expr == nil || b.scopeHandles == nil {
		return scopeRef{}, false
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		ref, ok := b.scopeHandles[e.Name]
		return ref, ok
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" && !e.Computed {
			if prop, ok := e.Property.(*ast.Identifier); ok {
				ref, ok := b.scopeHandles["props."+prop.Name]
				return ref, ok
			}
		}
	case *ast.TypeAssertion:
		return b.resolveScopeExpr(e.Expr)
	}
	return scopeRef{}, false
}

// helperCall matches `X.trigger(v)` / `X.panel(v)` / `X.close()` / `X.current()`
// against a scope handle, returning the helper name, the scope, and the option
// argument (nil when absent).
func (b *builder) helperCall(expr ast.Expr) (string, scopeRef, ast.Expr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", scopeRef{}, nil, false
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return "", scopeRef{}, nil, false
	}
	prop, ok := mem.Property.(*ast.Identifier)
	if !ok {
		return "", scopeRef{}, nil, false
	}
	switch prop.Name {
	case "trigger", "panel", "close", "current":
	default:
		return "", scopeRef{}, nil, false
	}
	ref, ok := b.resolveScopeExpr(mem.Object)
	if !ok {
		return "", scopeRef{}, nil, false
	}
	var arg ast.Expr
	if len(call.Args) > 0 {
		arg = call.Args[0]
	}
	return prop.Name, ref, arg, true
}

// rewriteScopeSpread turns an element carrying a scope-helper spread
// (`<button {...tab.trigger('a')}>`) into the zero-JS trigger/panel/close
// markup. Returns nil when the element has no such spread.
func (b *builder) rewriteScopeSpread(el *ast.JSXElement) ast.Expr {
	if el == nil || el.Opening == nil {
		return nil
	}
	for i, attr := range el.Opening.Attributes {
		if attr == nil || !attr.Spread || attr.Value == nil || !isScopeHelperExpr(attr.Value) {
			continue
		}
		name, ref, arg, ok := b.helperCall(attr.Value)
		if !ok {
			// A scope helper (`.trigger/.panel/.close/.current`) whose handle is
			// not resolvable — usually a part used outside its `<Provider>`.
			b.cssErrs = append(b.cssErrs, fmt.Errorf(
				"CSS scope helper spread at line %d is not bound to a scope handle: is there a `<Ctx.Provider value={scope}>` above it, or is the handle passed as a prop?",
				el.Position.Line))
			continue
		}
		clone := shallowCloneJSX(el)
		// Remove the consumed spread by index (attrs are deep-cloned, so pointer
		// identity no longer matches the original).
		attrs := clone.Opening.Attributes
		clone.Opening.Attributes = append(append([]*ast.JSXAttr(nil), attrs[:i]...), attrs[i+1:]...)

		savedToken := b.cssToken
		b.cssToken = ref.token
		defer func() { b.cssToken = savedToken }()

		switch name {
		case "trigger":
			opt := b.scopeOption(arg, "on")
			return b.buildCSSTriggerAt(clone, csssignals.Trigger{Scope: ref.scope, Option: opt}, "")
		case "close":
			// A toggle's controller is the single checkbox (option "on"); other
			// scopes close via their "closed" sentinel.
			opt := ref.scope.Sentinel
			if ref.scope.Kind == csssignals.KindToggle {
				opt = "on"
			}
			return b.buildCSSTriggerAt(clone, csssignals.Trigger{Scope: ref.scope, Option: opt}, "")
		case "panel":
			if ref.scope.Kind == csssignals.KindToggle {
				negated := literalBoolIsFalse(arg)
				return wrapWithClassOnly([]ast.JSXChild{&ast.JSXElementChild{Element: clone}}, ref.scope.PanelWrapperClass("on", negated))
			}
			opt := b.scopeOption(arg, "")
			return wrapWithClassOnly([]ast.JSXChild{&ast.JSXElementChild{Element: clone}}, ref.scope.PanelWrapperClass(opt, false))
		case "current":
			clone.Children = []ast.JSXChild{}
			clone.Opening.Attributes = append(clone.Opening.Attributes, strJSXAttr("class", "krc-live"))
			ref.scope.LiveText = true
			return clone
		}
	}
	return nil
}

// scopeOption reads the helper's option argument: a literal, a `props.<name>`
// resolved from the component's call-site bindings, else the fallback ("on" for
// toggles).
func (b *builder) scopeOption(arg ast.Expr, fallback string) string {
	if arg != nil {
		if ta, ok := arg.(*ast.TypeAssertion); ok {
			return b.scopeOption(ta.Expr, fallback)
		}
		if lit, ok := arg.(*ast.Literal); ok {
			switch lit.Kind {
			case ast.StringLit, ast.NumberLit:
				return lit.Value
			}
		}
		if mem, ok := arg.(*ast.MemberExpr); ok && !mem.Computed {
			if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == "props" {
				if prop, ok := mem.Property.(*ast.Identifier); ok {
					if v, ok := b.localProps[prop.Name]; ok && v != "" {
						return v
					}
				}
			}
		}
	}
	return fallback
}

// literalBoolIsFalse reports whether an expression is the literal `false`.
func literalBoolIsFalse(expr ast.Expr) bool {
	lit, ok := expr.(*ast.Literal)
	return ok && lit.Kind == ast.BoolLit && lit.Value == "false"
}

// scopeSpec is a call-site option universe for a compound scope-root component,
// inferred from the literal `value`s of its namespaced parts.
type scopeSpec struct {
	options []string
	initial string
}

// cssTokenFor derives the per-instance CSS-signal token exactly as
// buildComponentNode does (join the component name again, append the instance
// suffix), so call-site trigger labels point their `for` at the same ids the
// injected controller inputs use.
func (b *builder) cssTokenFor(parentID SlotID, name string) string {
	idx := b.instanceCounts[name]
	id := joinSlotID(string(parentID), name)
	id = SlotID(string(id) + "_c" + itoa(idx))
	return csssignals.SanitizeToken(string(id))
}

// declaresCSSTScope reports whether a component body declares a CSS scope
// (without analyzing it, so an option override can be set first).
func (b *builder) declaresCSSTScope(fn *ast.FnDecl) bool {
	if fn == nil {
		return false
	}
	for _, d := range sigutil.Find(fn.Body, true) {
		if d.CSSKind != sigutil.CSSKindNone {
			return true
		}
	}
	return false
}

// setScopeOverride installs the call-site option universe for a scope-root
// component before it is built, so controllers/classes reflect the parts.
func (b *builder) setScopeOverride(fn *ast.FnDecl, spec scopeSpec) {
	if b.scopeOverrides == nil {
		b.scopeOverrides = make(map[*ast.FnDecl]scopeSpec)
	}
	b.scopeOverrides[fn] = spec
}

// literalAttr returns the literal string value of an attribute, if present.
func literalAttr(el *ast.JSXElement, name string) (string, bool) {
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Name != name || attr.Value == nil {
			continue
		}
		if lit, ok := attr.Value.(*ast.Literal); ok && lit.Kind == ast.StringLit {
			return lit.Value, true
		}
	}
	return "", false
}

// collectScopeSpec determines a scope root's option universe and initial value
// from its call site: an explicit literal `options` array prop, or every
// descendant element's literal `value`, plus a literal `defaultValue`/`value`.
func (b *builder) collectScopeSpec(el *ast.JSXElement) scopeSpec {
	var options []string
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Name != "options" || attr.Value == nil {
			continue
		}
		if arr, ok := attr.Value.(*ast.ArrayExpr); ok {
			for _, item := range arr.Elements {
				if lit, ok := item.(*ast.Literal); ok && (lit.Kind == ast.StringLit || lit.Kind == ast.NumberLit) {
					options = append(options, lit.Value)
				}
			}
		}
	}
	if len(options) == 0 {
		options = collectValueLiterals(el.Children)
	}
	initial := ""
	if v, ok := literalAttr(el, "defaultValue"); ok {
		initial = v
	} else if v, ok := literalAttr(el, "value"); ok {
		initial = v
	}
	return scopeSpec{options: options, initial: initial}
}

// collectValueLiterals gathers the distinct literal `value` attributes of every
// descendant element (any component or intrinsic tag), in source order.
func collectValueLiterals(children []ast.JSXChild) []string {
	seen := make(map[string]bool)
	var options []string
	var walk func([]ast.JSXChild)
	walk = func(cs []ast.JSXChild) {
		for _, child := range cs {
			switch c := child.(type) {
			case *ast.JSXElementChild:
				if c.Element == nil || c.Element.Opening == nil {
					continue
				}
				// Only component tags (e.g. <Tabs.Trigger value="a">) contribute
				// options; an intrinsic <input value="…"> must not.
				name := c.Element.Opening.Name
				if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
					if v, ok := literalAttr(c.Element, "value"); ok && !seen[v] {
						seen[v] = true
						options = append(options, v)
					}
				}
				walk(c.Element.Children)
			case *ast.JSXFragmentChild:
				if c.Fragment != nil {
					walk(c.Fragment.Children)
				}
			}
		}
	}
	walk(children)
	return options
}

// rewritePartChildren returns a copy of children with every namespaced part of
// part markup never shares mutable AST with the source component. Without this,
// appendClass on one instance would leak into every instance.
func deepCloneAttrs(attrs []*ast.JSXAttr) []*ast.JSXAttr {
	out := make([]*ast.JSXAttr, 0, len(attrs))
	for _, a := range attrs {
		if a == nil {
			continue
		}
		clone := *a
		if lit, ok := a.Value.(*ast.Literal); ok {
			l := *lit
			clone.Value = &l
		}
		out = append(out, &clone)
	}
	return out
}
