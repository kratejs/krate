package irtree

import (
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Component call sites.
//
// buildComponentSlot lowers `<Child .../>` in one of four ways: a runtime-tier
// placeholder rendered at request time, a signal-less component inlined with
// its signal-valued props substituted, a build-time evaluated (SSREval)
// component, or a full client component built by buildComponentNode.

// buildComponentSlot builds the slot for a component element.
func (b *builder) buildComponentSlot(el *ast.JSXElement, parentID string) []SlotNode {
	if el == nil || el.Opening == nil {
		return nil
	}
	childName := el.Opening.Name
	childFn := b.functions[childName]
	if childFn == nil {
		return nil
	}
	el = b.expandComponentSpreads(el)
	childID := joinSlotID(parentID, childName)

	// Scope root: resolve its option universe from the call site (an explicit
	// literal `options` array, or the descendant `value` literals) before it is
	// built, so its controller inputs match the parts.
	if b.declaresCSSTScope(childFn) {
		if spec := b.collectScopeSpec(el); len(spec.options) > 0 {
			b.setScopeOverride(childFn, spec)
			if spec.initial != "" {
				if b.instanceInitial == nil {
					b.instanceInitial = make(map[string]string)
				}
				b.instanceInitial[b.cssTokenFor(childID, childName)] = spec.initial
			}
		}
	}
	provider := b.providedScope(childFn, childID, childName)

	// Runtime-tier components are NOT rendered at build time. The emitter
	// produces a krate-id placeholder + props entry so the serve-time renderer
	// evaluates them (via QuickJS/streaming) at request time with fresh values.
	if b.ann.ComponentTiers[childName] == TierRuntime {
		return b.buildRuntimeComponentSlot(el, childFn, childID)
	}

	childSignals := b.collectSignalDecls(childFn)
	attrs := extractPropsAST(el)
	// <Toggle>{(on, flip) => <button/>}</Toggle>: a function as the only child
	// is the component's `children` prop, called with the component's values.
	if fn := functionChild(el.Children); fn != nil {
		attrs["children"] = fn
		bare := *el
		bare.Children = nil
		el = &bare
	}

	// Recursion guard: expanding a component that is already being expanded
	// with the same props can never terminate (see enterExpansion).
	leave, ok := b.enterExpansion(childFn.Name, el, attrs)
	if !ok {
		return []SlotNode{&StaticHTML{HTML: ""}}
	}
	defer leave()

	b.addContextProps(attrs)
	b.inlineJSXConstAttrs(attrs)
	// CSS signal components must go through buildComponentNode so their
	// analyzer is installed and triggers/panels are rewritten. A panels-only
	// component has no signals/handlers and would otherwise take the SSREval
	// shortcut, which bypasses the interception and breaks the panels. The same
	// applies to components using first-class scope-handle spreads, and to
	// components handed a scope handle, which they may pass further down.
	if len(childSignals) == 0 && !b.cssSignalsOK(childFn) && !hasScopeHelperSpread(childFn) && !b.passesScopeHandle(attrs) && !hasComponentChildren(el.Children) && !b.isLazyComponent(childName) {
		if !b.componentNeedsClient(childFn, attrs) {
			// A signal-less component that renders a parent's signal value
			// (e.g. <Display value={display()}/>) must stay reactive, so its
			// return JSX is inlined with the props substituted by their
			// call-site expressions.
			if b.componentHasSignalProps(attrs) {
				// Inlining substitutes `props.X` reads only. A parameter that
				// stays destructured would keep the first value, so the
				// component takes the client path, which warns about it.
				if len(childFn.Params) == 0 || childFn.Params[0] == nil || childFn.Params[0].Pattern == "" {
					return b.inlineSignalPropsComponent(el, childFn, childID, attrs)
				}
			} else {
				return b.buildSSREvalComponentSlot(el, childFn, childID, attrs)
			}
		}
	}
	return b.buildClientComponentSlot(el, childFn, childID, attrs, provider)
}

// passesScopeHandle reports whether a call-site attribute is a CSS scope
// handle (or `props.<name>` holding one).
func (b *builder) passesScopeHandle(attrs map[string]ast.Expr) bool {
	for _, expr := range attrs {
		if _, ok := b.buildTimeScope(expr); ok {
			return true
		}
	}
	return false
}

// expandComponentSpreads resolves the spread attributes of a component call
// (`<Inner {...props} count={n} />`) to the attributes they carry, so they
// reach the child. Spreads whose value is not known at build time cannot be
// passed and are reported.
func (b *builder) expandComponentSpreads(el *ast.JSXElement) *ast.JSXElement {
	hasSpread := false
	for _, a := range el.Opening.Attributes {
		if a != nil && a.Spread {
			hasSpread = true
			break
		}
	}
	if !hasSpread {
		return el
	}
	expanded, forwardsChildren := b.expandForwardedSpreadAttrs(el.Opening.Attributes)
	cp := *el
	opening := *el.Opening
	opening.Attributes = expanded
	cp.Opening = &opening
	if forwardsChildren && len(cp.Children) == 0 {
		cp.Children = b.callSiteChildren
	}
	if b.locateOnly == 0 {
		for _, a := range expanded {
			if a != nil && a.Spread && a.Value != nil && !isScopeHelperExpr(a.Value) {
				b.warnAt(a.Position, "spread {..."+generateExprJS(a.Value, nil)+"} on <"+el.Opening.Name+"> has no build-time attribute list; its props are not passed",
					"spread a props or rest parameter, or pass the props explicitly (e.g. label={x.label})")
			}
		}
	}
	return &cp
}

// scopeProvider is the CSS scope a context-providing component makes active
// for its call-site children.
type scopeProvider struct {
	ctx string
	ref scopeRef
	ok  bool
}

// providedScope reports the scope a component provides when its return wraps
// a scope handle in `<Ctx.Provider value={handle}>`, so descendant
// `Ctx.useContext()` reads in the call-site children resolve.
func (b *builder) providedScope(childFn *ast.FnDecl, childID SlotID, childName string) scopeProvider {
	ctxName, localVar, ok := providedScopeContext(childFn)
	if !ok {
		return scopeProvider{}
	}
	a := b.cssAnalyzer(childFn)
	if a == nil || !a.OK() {
		return scopeProvider{}
	}
	sc := a.ScopeByVar(localVar)
	if sc == nil {
		return scopeProvider{}
	}
	return scopeProvider{
		ctx: ctxName,
		ref: scopeRef{analyzer: a, scope: sc, token: b.cssTokenFor(childID, childName)},
		ok:  true,
	}
}

// buildRuntimeComponentSlot builds the request-time placeholder of a
// runtime-tier component.
func (b *builder) buildRuntimeComponentSlot(el *ast.JSXElement, childFn *ast.FnDecl, childID SlotID) []SlotNode {
	childName := el.Opening.Name
	instanceIdx := b.instanceCounts[childName]
	b.instanceCounts[childName]++
	id := SlotID(string(childID) + "_c" + itoa(instanceIdx))
	attrs := extractPropsAST(el)
	if childFn.Async {
		b.checkServerComponentCall(el, attrs)
	}
	props := make(map[string]any, len(attrs))
	for name, expr := range attrs {
		props[name] = evalConstWithSignals(expr, b.sigMap(), b.localProps)
	}
	return []SlotNode{&ComponentSlot{
		ID: id,
		Component: &ComponentNode{
			ID:              id,
			Name:            childName,
			Tier:            TierRuntime,
			Props:           attrs,
			RuntimeProps:    props,
			ServerComponent: childFn.Async,
			SourceFile:      b.ann.ComponentSources[childName],
			Line:            childFn.Position.Line,
		},
	}}
}

// addContextProps passes the active dynamic context values into the child as
// synthetic getter props, so its hydration code reads the live value through
// the props registry (the parent's signal names are not in scope in the
// child's IIFE).
func (b *builder) addContextProps(attrs map[string]ast.Expr) {
	for ctx, val := range b.contextValues {
		attrs[contextPropPrefix+ctx] = &ast.ArrowFn{
			Expression: true,
			Body:       []ast.Stmt{&ast.ReturnStmt{Value: val}},
		}
	}
}

// buildSSREvalComponentSlot marks a pure prop-driven component for SSR
// evaluation at emit time. The call-site prop bindings let the emitter
// evaluate the component's return with those values. Instances are
// disambiguated (_cN) so CSS-choice radio names stay unique.
func (b *builder) buildSSREvalComponentSlot(el *ast.JSXElement, childFn *ast.FnDecl, childID SlotID, attrs map[string]ast.Expr) []SlotNode {
	childName := el.Opening.Name
	instanceIdx := b.instanceCounts[childName]
	b.instanceCounts[childName]++
	childID = SlotID(string(childID) + "_c" + itoa(instanceIdx))

	b.checkCSSSignalPropReads(el, childFn, attrs)
	bindings := b.buildPropBindings(extractParamNames(childFn), attrs, childFn)
	var useIDs []string
	nextID := b.useIDSequence(childID)
	childNode := &ComponentNode{
		ID:              childID,
		Name:            childName,
		Tier:            TierStatic,
		Fn:              childFn,
		SSREvalBindings: bindings,
		SSREvalAttrs:    attrs,
		LocalVars: b.foldLocalConsts(childFn, bindings, func() string {
			id := nextID()
			useIDs = append(useIDs, id)
			return id
		}),
		IsSSREval:        true,
		SSREvalCaller:    callerScope(jsxAttrExprs(attrs), b.localProps),
		CallSiteChildren: el.Children,
		RestProps:        restProps(childFn, attrs),
		RestPropsName:    fnRestParamName(childFn),
		ContextValues:    b.activeContextMarkers(),
	}
	childNode.UseIDs = useIDs

	// The call-site children may contain interactive components that need
	// their own hydration even though this component has no signals.
	savedCS := b.callSiteChildren
	b.callSiteChildren = el.Children
	// Fully static call-site text is kept raw so SSREval can chroma-highlight
	// <SyntaxHighlight> content.
	if text, ok := b.resolveCallSiteChildrenText(); ok {
		childNode.CallSiteChildrenText = text
	}
	restoreExpansion := b.callerExpansion()
	restoreCSS := b.enterCSSChildren(childFn)
	childNode.Children = b.buildCallSiteChildSlots(el.Children, string(childID))
	b.buildRepeatedChildrenCopies(childFn.Name, childFn, &childNode.Children, &childNode.ChildCopyStarts, func() []SlotNode {
		return b.buildCallSiteChildSlots(el.Children, string(childID))
	})
	restoreCSS()
	restoreExpansion()
	b.callSiteChildren = savedCS

	if ret := findReturnStmt(childFn.Body); ret != nil && ret.Value != nil {
		childNode.ReturnSlots = b.discoverReturnSlots(childFn, childID, attrs, ret.Value)
	}
	// Components in a JSX prop are evaluated first (ResolveJSXProps), so their
	// slots lead the queue the emitter matches by name.
	if propSlots := b.buildJSXPropComponentSlots(childFn, attrs, string(childID)); len(propSlots) > 0 {
		childNode.ReturnSlots = append(propSlots, childNode.ReturnSlots...)
	}
	return []SlotNode{&ComponentSlot{ID: childID, Component: childNode}}
}

// buildJSXPropComponentSlots builds, in the caller's scope, the client
// components written inside JSX-valued call-site props (`slot={<Probe />}`).
// SSREval renders the prop markup itself, which cannot run a component that
// holds state, so the emitter emits these through the tree path instead and
// they keep their hydration markers like call-site children.
func (b *builder) buildJSXPropComponentSlots(childFn *ast.FnDecl, attrs map[string]ast.Expr, parentID string) []SlotNode {
	names := make([]string, 0, len(attrs))
	for name, expr := range attrs {
		if expr != nil && !b.isFuncReference(expr) && containsJSXNode(expr) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []SlotNode
	for _, name := range names {
		var els []*ast.JSXElement
		WalkAST(attrs[name], func(n any) bool {
			el, ok := n.(*ast.JSXElement)
			if !ok {
				return true
			}
			if el.Opening != nil && el.Opening.Name != "" && el.Opening.Name[0] >= 'A' && el.Opening.Name[0] <= 'Z' {
				els = append(els, el)
				return false
			}
			return true
		})
		before := len(out)
		for _, el := range els {
			restoreExpansion := b.callerExpansion()
			for _, slot := range b.buildSlotNodes(el, parentID) {
				if cs, ok := slot.(*ComponentSlot); ok && cs.Component != nil && cs.Component.Tier == TierClient {
					out = append(out, slot)
				}
			}
			restoreExpansion()
		}
		if len(out) > before {
			if n := countPropReads(childFn, name); n > 1 {
				b.warnCompat(attrs[name].Pos(), diag.CodeJSXPropRenderedTwice,
					"the `"+name+"` prop holds a component with state and <"+childFn.Name+"> renders it "+itoa(n)+" times; only one copy hydrates",
					"render the prop in one place, or pass one element for each place")
			}
		}
	}
	return out
}

// countPropReads counts the reads of the prop called name in fn's body,
// either as a bare (destructured) name or as props.name.
func countPropReads(fn *ast.FnDecl, name string) int {
	n := 0
	var visit func(node any) bool
	visit = func(node any) bool {
		switch e := node.(type) {
		case *ast.Identifier:
			if e.Name == name {
				n++
			}
		case *ast.MemberExpr:
			if obj, ok := e.Object.(*ast.Identifier); ok && obj.Name == "props" {
				if prop, ok := e.Property.(*ast.Identifier); ok && !e.Computed && prop.Name == name {
					n++
				}
				return false
			}
			WalkAST(e.Object, visit)
			if e.Computed {
				WalkAST(e.Property, visit)
			}
			return false
		}
		return true
	}
	for _, stmt := range fn.Body {
		WalkAST(stmt, visit)
	}
	return n
}

// useIDRow numbers the useId() calls of one keyed list row.
type useIDRow struct {
	prefix string
	n      int
}

// useIDSequence returns the useId() generator for the component with the
// given slot ID: the row sequence inside a keyed list row, else the
// component's own sequence.
func (b *builder) useIDSequence(id SlotID) func() string {
	row := b.useIDRow
	if row == nil {
		return useIdSequence(id)
	}
	return func() string {
		n := row.n
		row.n++
		return "krate-" + idSafe(row.prefix) + "-u" + itoa(n)
	}
}

// idSafe percent-encodes every byte of an id part outside [A-Za-z0-9_.:$-],
// so a list key such as "a b" still makes a valid id. The runtime
// (runtime/src/use-id.ts) applies the same rule.
func idSafe(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '.', c == ':', c == '$', c == '-':
			b.WriteByte(c)
		default:
			b.WriteString("%")
			b.WriteString(strings.ToUpper(strconv.FormatInt(int64(c)+0x100, 16)[1:]))
		}
	}
	return b.String()
}

// useIdSequence returns the useId() literal generator of a component
// instance: the first call gets the base ID, later calls a numeric suffix.
func useIdSequence(id SlotID) func() string {
	base := "krate-" + strings.ReplaceAll(string(id), ".", "-")
	n := 0
	return func() string {
		i := n
		n++
		if i == 0 {
			return base
		}
		return base + "-" + itoa(i)
	}
}

// discoverReturnSlots builds a build-time evaluated component's OWN return
// JSX to find interactive client components in it (e.g. a signal-less demo
// wrapper returning a <DropdownMenu>); those must be emitted through the tree
// path or they would be SSR'd as flat static HTML with no hydration. The walk
// runs with the child's params as local props in a scoped context, so the
// child's internal bindings never leak into the caller's signature; only the
// props registrations of the client components found are kept, since they
// are emitted with them in the caller's scope.
func (b *builder) discoverReturnSlots(childFn *ast.FnDecl, childID SlotID, attrs map[string]ast.Expr, ret ast.Expr) []SlotNode {
	b.pushComponentCtx()
	b.pendingHandlers, b.pendingAttrs, b.pendingRefs = nil, nil, nil
	b.localProps = b.propBindingsToLocalProps(childFn, attrs, b.localProps)
	b.propKeys = make(map[string]bool, len(attrs))
	for name := range attrs {
		b.propKeys[name] = true
	}
	b.restProps = b.restPropsFor(fnRestParamName(childFn), childFn, attrs)
	b.liveProps, b.livePropsParam = nil, ""
	b.localFnBody = childFn.Body
	b.locateOnly++
	slots := b.buildSlotNodes(ret, string(childID))
	b.locateOnly--
	regs := b.pendingPropsRegs
	b.popComponentCtx()
	b.pendingPropsRegs = regs
	return slots
}

// buildClientComponentSlot builds a component through buildComponentNode with
// the call-site inputs installed, then its call-site children in the caller's
// scope, and registers its props for hydration.
func (b *builder) buildClientComponentSlot(el *ast.JSXElement, childFn *ast.FnDecl, childID SlotID, attrs map[string]ast.Expr, provider scopeProvider) []SlotNode {
	b.warnSuspendOutsideBoundary(childFn, el.Position)
	// An error boundary whose subtree threw at build time renders its fallback
	// (boundary_build.go); the instance id is the one newComponentNode assigns.
	instance := SlotID(string(joinSlotID(string(childID), childFn.Name)) + "_c" + itoa(b.instanceCounts[childFn.Name]))
	if fallback, ok := b.boundaryFallback(childFn, instance); ok {
		childFn = fallback
		bare := *el
		bare.Children = nil
		el = &bare
	}
	// A lowered class remounts when its key changes; the runtime instance
	// reads the key through a reserved prop.
	if b.ann.IsLoweredClass(childFn.Name) {
		for _, a := range el.Opening.Attributes {
			if a != nil && !a.Spread && a.Name == "key" && a.Value != nil {
				attrs["__krate_key"] = a.Value
			}
		}
	}
	b.pushComponentCtx()
	b.installCallSiteInputs(el, childFn, attrs)
	live := b.liveProps
	lc, lazy := b.ann.LazyComponents[el.Opening.Name]
	if lazy {
		// Consumed by finishClientComponent for this node (children finish
		// first and have other functions).
		b.pendingLazy, b.pendingLazyFn = &lc, childFn
	}
	outerPropSlots := b.propExprSlots
	b.propExprSlots = nil
	childNode := b.buildComponentNode(childFn, string(childID))
	propReaders := b.propExprSlots
	b.propExprSlots = outerPropSlots
	b.popComponentCtx()
	childNode.ContextValues = b.activeContextMarkers()
	b.warnSnapshotProps(el, childFn, childNode, live)

	childNode.Props = attrs
	if lazy {
		childNode.Lazy = true
	}
	childNode.CallSiteChildren = el.Children
	childNode.CallSiteSlots = b.buildComponentCallSiteSlots(el, childFn, childID, provider, childNode.childrenContext)
	b.buildRepeatedChildrenCopies(childFn.Name, childFn, &childNode.CallSiteSlots, &childNode.CallSiteCopyStarts, func() []SlotNode {
		return b.buildComponentCallSiteSlots(el, childFn, childID, provider, childNode.childrenContext)
	})
	if len(propReaders) > 0 {
		if propSlots := b.buildJSXPropComponentSlots(childFn, attrs, string(childID)); len(propSlots) > 0 {
			for _, s := range propReaders {
				s.PropSlots = propSlots
			}
		}
	}
	b.completeSSREvalBindings(childNode, childFn, attrs)
	b.registerChildProps(childNode, childFn)
	b.checkScopePropReads(el, childFn, childNode, attrs)

	// buildTextSlot resolves signals only, so TextSlot initials that read
	// props.X are updated from the resolved values here.
	updateTextSlotInitials(childNode.Children, childNode.Signals)
	return []SlotNode{&ComponentSlot{ID: childID, Component: childNode}}
}

// installCallSiteInputs sets the child's call-site inputs in the active
// context: resolved prop values (props.X reads and bare param identifiers
// evaluate to their SSR initial), function-valued props, what a rest or
// whole-props parameter collects, call-site children, scope-valued props, and
// whether call-site component children force the child onto the client.
func (b *builder) installCallSiteInputs(el *ast.JSXElement, childFn *ast.FnDecl, attrs map[string]ast.Expr) {
	parentProps := b.localProps
	b.localProps = make(valueMap, len(attrs))
	// Constant provider values above the call site stay visible to the child.
	for k, v := range parentProps {
		if strings.HasPrefix(k, contextMarkerPrefix) {
			b.localProps[k] = v
		}
	}
	var funcProps map[string]bool
	// Evaluated in the caller's scope, before the child's signals replace it.
	var live map[string]bool
	for name, expr := range attrs {
		if !strings.HasPrefix(name, contextPropPrefix) && b.isSignalDerivedProp(expr) {
			if live == nil {
				live = make(map[string]bool)
			}
			live[name] = true
		}
	}
	b.liveProps, b.livePropsParam = live, ""
	if live != nil {
		b.livePropsParam = plainPropsParamName(childFn)
	}
	for name, expr := range attrs {
		if b.isFuncReference(expr) {
			if funcProps == nil {
				funcProps = make(map[string]bool)
			}
			funcProps[name] = true
			// Function props have no const value; keep them absent from the
			// child's localProps so a `var X = props.<fn>` alias isn't folded
			// to a function-name string.
			continue
		}
		b.localProps[name] = b.resolveBindingValue(expr, parentProps)
	}
	// An omitted destructured prop takes its pattern default (`{ step = 2 }`)
	// or undefined, rather than leaving the bare name unresolved.
	if len(childFn.Params) > 0 && childFn.Params[0] != nil && childFn.Params[0].Pattern != "" {
		rest := fnRestParamName(childFn)
		for _, name := range destructuredParamNames(childFn.Params[0].Pattern) {
			if _, given := attrs[name]; given || name == rest {
				continue
			}
			b.localProps[name] = b.paramDefaultValue(childFn, name)
		}
		b.bindNestedParamPattern(childFn, attrs, funcProps)
	}
	b.localFuncProps = funcProps
	b.propKeys = make(map[string]bool, len(attrs))
	b.jsxProps = nil
	for name, expr := range attrs {
		b.propKeys[name] = true
		if !b.isFuncReference(expr) && containsJSXNode(expr) {
			if b.jsxProps == nil {
				b.jsxProps = make(map[string]jsxProp)
			}
			b.jsxProps[name] = jsxProp{expr: expr, scope: callerScope([]ast.Expr{expr}, parentProps)}
		}
	}

	switch {
	case fnRestParamName(childFn) != "":
		// A rest parameter collects every call-site attr not bound by a
		// named/destructured parameter.
		bound := boundParamSet(childFn)
		rest := make(map[string]ast.Expr)
		for name, expr := range attrs {
			if !bound[name] {
				rest[name] = expr
			}
		}
		b.restProps = map[string]map[string]ast.Expr{fnRestParamName(childFn): rest}
	case plainPropsParamName(childFn) != "":
		// A whole-props parameter (`function Btn(props)`) collects every
		// call-site attribute, so `<button {...props}>` forwards them all.
		b.restProps = map[string]map[string]ast.Expr{plainPropsParamName(childFn): callSiteAttrs(attrs)}
	default:
		b.restProps = nil
	}
	b.callSiteChildren = el.Children

	// Scope-valued props (e.g. scope={tab}) flow into the child build so its
	// `props.scope.trigger(...)` spreads resolve to the parent's handle.
	scopeProps := make(map[string]scopeRef)
	for name, expr := range attrs {
		if ref, ok := b.resolveScopeExpr(expr); ok {
			scopeProps["props."+name] = ref
			scopeProps[name] = ref
		}
	}
	b.pendingScopeProps = scopeProps
	b.forceClient = hasComponentChildren(el.Children)
}

// buildComponentCallSiteSlots builds a client component's call-site children
// (in the caller's scope) with the component's call-site children and any
// scope it provides active.
func (b *builder) buildComponentCallSiteSlots(el *ast.JSXElement, childFn *ast.FnDecl, childID SlotID, provider scopeProvider, overlay *contextOverlay) []SlotNode {
	savedCallSite, savedScopes := b.callSiteChildren, b.contextScopes
	defer func() { b.callSiteChildren, b.contextScopes = savedCallSite, savedScopes }()
	defer b.enterCSSChildren(childFn)()
	defer b.callerExpansion()()
	defer b.applyChildrenContext(overlay)()
	b.callSiteChildren = el.Children
	if provider.ok {
		b.contextScopes = cloneScopeMap(savedScopes)
		b.contextScopes[provider.ctx] = provider.ref
	}
	return b.buildCallSiteChildSlots(el.Children, string(childID))
}

// maxChildrenCopies caps how many instances of the call-site children one
// component renders; further placeholders are reported (KR8360).
const maxChildrenCopies = 8

// buildRepeatedChildrenCopies builds one extra copy of the call-site children
// for every further `{children}` placeholder in the component's body, so each
// rendered instance gets its own hydration keys (React mounts children rendered
// twice as two instances). build makes one copy. The copies are appended to
// *slots and their start offsets recorded in *starts. Children with nothing to
// hydrate render identically each time and need no copy. A placeholder that
// renders more than once from a single place (inside a loop) or beyond the cap
// cannot get its own copy and is reported.
func (b *builder) buildRepeatedChildrenCopies(name string, fn *ast.FnDecl, slots *[]SlotNode, starts *[]int, build func() []SlotNode) {
	refs := childrenPlaceholders(fn)
	if len(refs) == 0 || (len(refs) < 2 && !refs[0].loop) {
		return
	}
	hydrates := false
	for _, s := range *slots {
		if h, static := s.(*StaticHTML); !static || strings.Contains(h.HTML, "data-k") {
			hydrates = true
			break
		}
	}
	if !hydrates {
		return
	}
	for _, r := range refs {
		if r.loop {
			b.warnChildrenRepeated(fn.Name, r.pos, "children with client state are rendered once per iteration in <"+name+">",
				"each copy needs its own instance: render a component per item with its own state, or move the state up out of the children")
			break
		}
	}
	if len(refs) > maxChildrenCopies {
		b.warnChildrenRepeated(fn.Name, refs[maxChildrenCopies].pos, "children with client state are rendered "+itoa(len(refs))+" times in <"+name+">, more than the "+itoa(maxChildrenCopies)+" copies that get their own instance",
			"each copy needs its own instance: render a component per copy, or move the state up out of the children")
	}
	for i := 1; i < min(len(refs), maxChildrenCopies); i++ {
		*starts = append(*starts, len(*slots))
		*slots = append(*slots, build()...)
	}
}

// warnChildrenRepeated reports KR8323 at a position in component fn, which
// may live in another file than the one being built.
func (b *builder) warnChildrenRepeated(fn string, pos ast.Pos, msg, hint string) {
	if b.locateOnly > 0 {
		return
	}
	file, raw := "", ""
	if b.ann != nil {
		file, raw = b.ann.ComponentSources[fn], b.ann.ComponentRaw[fn]
		if file == "" {
			file = b.ann.SourceFile
		}
	}
	d := diag.New(file, pos.Line, pos.Col, msg, hint, sourceLine(raw, pos.Line))
	d.Severity = diag.SeverityWarning
	d = d.WithCode(diag.CodeChildrenRepeated)
	for _, w := range b.warnings {
		if w.Code == d.Code && w.File == d.File && w.Line == d.Line && w.Col == d.Col {
			return
		}
	}
	b.warnings = append(b.warnings, d)
}

// childrenRef is one `{children}` placeholder in a component body.
type childrenRef struct {
	pos  ast.Pos
	loop bool // rendered once per iteration of a loop or array callback
}

var iterationMethods = map[string]bool{"map": true, "flatMap": true, "forEach": true, "reduce": true, "filter": true}

// childrenPlaceholders lists the `{children}` / `{props.children}` containers
// in fn's body in source order.
func childrenPlaceholders(fn *ast.FnDecl) []childrenRef {
	inLoop := map[*ast.JSXExprContainer]bool{}
	markLoop := func(node any) {
		WalkAST(node, func(n any) bool {
			if ec, ok := n.(*ast.JSXExprContainer); ok {
				inLoop[ec] = true
			}
			return true
		})
	}
	var refs []childrenRef
	WalkAST(fn.Body, func(n any) bool {
		switch t := n.(type) {
		case *ast.ForStmt, *ast.ForInStmt, *ast.WhileStmt, *ast.DoWhileStmt:
			markLoop(t)
		case *ast.CallExpr:
			if m, ok := t.Callee.(*ast.MemberExpr); ok {
				if id, ok := m.Property.(*ast.Identifier); ok && iterationMethods[id.Name] {
					markLoop(t.Args)
				}
			}
		}
		return true
	})
	// The placeholder expression carries no position; the element or fragment
	// holding it does.
	addRefs := func(pos ast.Pos, children []ast.JSXChild) {
		for _, c := range children {
			if ec, ok := c.(*ast.JSXExprContainer); ok && ec.Expression != nil && isChildrenPlaceholderExpr(ec.Expression) {
				refs = append(refs, childrenRef{pos: pos, loop: inLoop[ec]})
			}
		}
	}
	WalkAST(fn.Body, func(n any) bool {
		switch t := n.(type) {
		case *ast.JSXElement:
			addRefs(t.Position, t.Children)
		case *ast.JSXFragment:
			addRefs(t.Position, t.Children)
		}
		return true
	})
	return refs
}

// completeSSREvalBindings gives an auto-promoted SSR-evaluated component the
// prop bindings (from the call-site attrs and its parameter names) and folded
// locals that {props.X} reads resolve against at build time.
func (b *builder) completeSSREvalBindings(childNode *ComponentNode, childFn *ast.FnDecl, attrs map[string]ast.Expr) {
	if !childNode.IsSSREval {
		return
	}
	if childNode.SSREvalBindings == nil {
		childNode.SSREvalBindings = b.buildPropBindings(extractParamNames(childFn), attrs, childFn)
	}
	if len(childNode.LocalVars) == 0 {
		return
	}
	if childNode.SSREvalBindings == nil {
		childNode.SSREvalBindings = make(map[string]jsvalue.Value, len(childNode.LocalVars))
	}
	for name, val := range childNode.LocalVars {
		if _, exists := childNode.SSREvalBindings[name]; !exists {
			childNode.SSREvalBindings[name] = val
		}
	}
}

// registerChildProps hoists a client child's props into the __krate_props
// registry when its hydration code reads `props`. The values may reference
// THIS component's signals (e.g. checked={checked1()}), so the registration
// is built in the parent's scope and the child binds `props` to it, instead
// of inlining values that reference out-of-scope identifiers into the child
// IIFE. Even with no call-site props an empty {} registration keeps `props`
// defined for handlers like props.onOpenChange.
func (b *builder) registerChildProps(childNode *ComponentNode, childFn *ast.FnDecl) {
	if childNode.Tier != TierClient {
		return
	}
	hasHydration := len(childNode.Handlers) > 0 || len(childNode.Effects) > 0 || len(childNode.Memos) > 0 ||
		len(childNode.Signals) > 0 || len(childNode.RefBindings) > 0 || len(childNode.AttrBindings) > 0 ||
		len(b.contextValues) > 0 ||
		// use()/deferred/external-store getters (`use(props.promise)`).
		(childFn != nil && bodyUsesReactiveFactory(childFn.Body)) ||
		// Replayed locals derived from live props (`var hits=...props.query`).
		(childFn != nil && plainPropsParamName(childFn) != "" && hydrationReadsAny(childNode, []string{plainPropsParamName(childFn)})) ||
		// Replayed body statements (a render-time throw guard) read props too.
		len(childNode.ExtraVars) > 0 || len(childNode.PreSignalVars) > 0 ||
		// Bindings of getter-registered props (`{props.title}`).
		(childFn != nil && plainPropsParamName(childFn) != "" && slotsReadProps(childNode.Children, plainPropsParamName(childFn)))
	if !hasHydration {
		return
	}
	readsProps := handlersOrLocalsReferenceProps(childNode.Handlers, childFn.Body) ||
		// Forwarded spread props are bound as `props.<name>` reads.
		attrBindingsReferenceProps(childNode.AttrBindings) ||
		compiledRefsProps(childNode.Effects) || compiledRefsProps(childNode.Memos) ||
		signalsReferenceProps(childNode.Signals) || len(childNode.FuncPropAliases) > 0 ||
		// Dynamic context is read through synthetic props (`props.__krate_ctx_*`).
		len(b.contextValues) > 0 ||
		// A child that reads props only inside rendered JSX (e.g. a list
		// binding `props.items.map(...)`) still needs the registry.
		bodyReferencesProps(childFn.Body)
	// A destructured or differently named props parameter
	// (`function Child({ label })`) is not a hydration-scope binding on its
	// own: when the emitted hydration code reads one of its names, bind it
	// from the registry object.
	paramJS, paramNames := propsParamBindingJS(childFn)
	readsParam := paramJS != "" && hydrationReadsAny(childNode, paramNames)
	if !readsProps && !readsParam {
		return
	}
	b.pendingPropsRegs = append(b.pendingPropsRegs, b.buildChildPropsRegDecl(string(childNode.ID), childNode.Props))
	decl := "var props=__krate_props[" + strconv.Quote(string(childNode.ID)) + "]"
	if readsParam {
		// Kept in the same fragment so the params are bound before signal
		// initializers that may read them.
		decl += ";" + paramJS
	}
	childNode.ExtraVars = append([]string{decl}, childNode.ExtraVars...)
}

// propsParamBindingJS returns the declaration that binds a component's first
// parameter from the hydration `props` object, with the names it binds. It
// is "" for a plain `props` parameter (already the registry binding), a rest
// parameter, or no parameter.
func propsParamBindingJS(fn *ast.FnDecl) (string, []string) {
	if fn == nil || len(fn.Params) == 0 || fn.Params[0] == nil {
		return "", nil
	}
	p := fn.Params[0]
	if p.IsRest {
		return "", nil
	}
	if p.Pattern != "" {
		names := destructuredParamNames(p.Pattern)
		if len(names) == 0 {
			return "", nil
		}
		return "var " + p.Pattern + "=props", names
	}
	if p.Name == "" || p.Name == "props" || p.Name == "{...}" || p.Name == "[...]" {
		return "", nil
	}
	return "var " + p.Name + "=props", []string{p.Name}
}

// warnSnapshotProps warns when a signal-derived prop reaches a hydrating
// child through a destructured parameter: the destructured local is read
// once, so the child never sees later values. Only `props.<name>` reads of a
// plain props parameter go through the live getter.
func (b *builder) warnSnapshotProps(el *ast.JSXElement, childFn *ast.FnDecl, childNode *ComponentNode, live map[string]bool) {
	if len(live) == 0 || b.locateOnly > 0 || childNode == nil {
		return
	}
	if len(childFn.Params) == 0 || childFn.Params[0] == nil || childFn.Params[0].Pattern == "" {
		return
	}
	var stale []string
	for _, name := range destructuredParamNames(childFn.Params[0].Pattern) {
		if live[name] {
			stale = append(stale, name)
		}
	}
	if len(stale) == 0 {
		return
	}
	sort.Strings(stale)
	key := childFn.Name + ":" + strings.Join(stale, ",")
	if b.snapshotPropWarned[key] {
		return
	}
	if b.snapshotPropWarned == nil {
		b.snapshotPropWarned = make(map[string]bool)
	}
	b.snapshotPropWarned[key] = true
	b.warnAt(el.Position,
		"<"+el.Opening.Name+"> receives signal-derived prop(s) "+strings.Join(stale, ", ")+
			" through a destructured parameter; the child only sees the initial value",
		"declare the parameter as `props` and read `props."+stale[0]+"` to keep it reactive, or pass an accessor ("+
			stale[0]+"={() => ...}) and call it in the child")
}

// rendersLiveProps reports whether the component being built binds a
// getter-registered prop in its rendered output (`{props.title}` where the
// caller passed a signal-derived value), so it must hydrate.
func (b *builder) rendersLiveProps(node *ComponentNode) bool {
	if len(b.liveProps) == 0 || b.livePropsParam == "" {
		return false
	}
	return slotsReadProps(node.Children, b.livePropsParam)
}

// slotsReadProps reports whether a component's own slots read the named
// props parameter.
func slotsReadProps(slots []SlotNode, param string) bool {
	return slotsReadAny(slots, func(code string) bool { return codeContainsIdent(code, param) })
}

// hydrationReadsAny reports whether the JS a component emits into its own
// hydration scope references any of names. Nested component instances have
// their own scopes and are not inspected.
func hydrationReadsAny(node *ComponentNode, names []string) bool {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	return hydrationCodeReads(node, func(code string) bool {
		if code == "" {
			return false
		}
		found := false
		scanJSIdents(code, func(name string) {
			if want[name] {
				found = true
			}
		})
		return found
	})
}

// hydrationCodeReads reports whether reads holds for any JS fragment the
// component emits into its own hydration scope.
func hydrationCodeReads(node *ComponentNode, reads func(code string) bool) bool {
	var codes []string
	for _, h := range node.Handlers {
		codes = append(codes, h.Body)
	}
	for _, s := range node.Signals {
		codes = append(codes, s.RawInit, s.FactoryJS, s.OptionsJS)
	}
	for _, a := range node.AttrBindings {
		codes = append(codes, a.ExprSource)
	}
	for _, r := range node.RefBindings {
		codes = append(codes, r.Target, r.Callback)
	}
	codes = append(codes, node.Effects...)
	codes = append(codes, node.Memos...)
	codes = append(codes, node.ExtraVars...)
	codes = append(codes, node.PreSignalVars...)
	codes = append(codes, node.FuncPropAliases...)
	codes = append(codes, node.BodyUses...)
	codes = append(codes, node.IslandBoot...)
	for _, c := range codes {
		if reads(c) {
			return true
		}
	}
	return slotsReadAny(node.Children, reads)
}

func slotsReadAny(slots []SlotNode, reads func(string) bool) bool {
	for _, s := range slots {
		switch v := s.(type) {
		case *ExprSlot:
			if reads(v.ExprSource) {
				return true
			}
		case *ConditionalSlot:
			if reads(v.TestJS) || reads(v.ExprSource) || slotsReadAny(v.Consequent, reads) || slotsReadAny(v.Alternate, reads) {
				return true
			}
		case *ListSlot:
			if reads(v.ExprSource) {
				return true
			}
			for _, it := range v.Items {
				if it != nil && slotsReadAny(it.Contents, reads) {
					return true
				}
			}
		case *SuspenseSlot:
			if slotsReadAny(v.Fallback, reads) || slotsReadAny(v.Resolved, reads) {
				return true
			}
		}
	}
	return false
}

// enterCSSChildren marks fn as an active CSS signal component while its
// call-site children are built, so a nested instance of the same component
// gets its own scope classes. The returned func undoes it.
func (b *builder) enterCSSChildren(fn *ast.FnDecl) func() {
	if a := b.cssAnalyzer(fn); a == nil || !a.OK() {
		return func() {}
	}
	if b.cssActive == nil {
		b.cssActive = make(map[*ast.FnDecl]int)
	}
	b.cssActive[fn]++
	return func() { b.cssActive[fn]-- }
}

// bindNestedParamPattern binds the names of a destructured parameter whose
// pattern renames or nests (`{ item: { id, tags: [first] }, idx }`) from the
// call-site values. A name inside such a pattern is not a prop key, so it is
// bound by destructuring the props object, not looked up as an attribute.
func (b *builder) bindNestedParamPattern(fn *ast.FnDecl, attrs map[string]ast.Expr, funcProps map[string]bool) {
	props := jsvalue.NewObject()
	for name := range attrs {
		if funcProps[name] {
			props.Set(name, jsvalue.Dynamic("function prop "+name))
		} else if v, ok := b.localProps[name]; ok {
			props.Set(name, v)
		}
	}
	for name, v := range b.nestedPatternValues(fn, props) {
		b.localProps[name] = v
	}
}

// nestedPatternValues destructures props with the first parameter's pattern
// and returns the value of every name it binds. It returns nil for a flat
// pattern (its names are prop keys) and for one the build cannot evaluate.
func (b *builder) nestedPatternValues(fn *ast.FnDecl, props jsvalue.Value) map[string]jsvalue.Value {
	if fn == nil || len(fn.Params) == 0 || fn.Params[0] == nil {
		return nil
	}
	pattern := strings.TrimSpace(fn.Params[0].Pattern)
	if len(pattern) < 2 || pattern[0] != '{' || !strings.ContainsAny(pattern[1:len(pattern)-1], ":[{") {
		return nil
	}
	scope := jsvalue.NewScope(nil)
	if r := b.constInterp(b.localProps).BindPattern(scope, fn.Params[0].Name, pattern, props); r.IsUnsupported() {
		return nil
	}
	rest := fnRestParamName(fn)
	out := map[string]jsvalue.Value{}
	for _, name := range destructuredParamNames(pattern) {
		if name == rest {
			continue
		}
		if v, ok := scope.Lookup(name); ok {
			out[name] = v
		}
	}
	return out
}
