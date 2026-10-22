package irtree

import (
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
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
	childID := joinSlotID(parentID, childName)

	// Scope root: resolve its option universe from the call site (an explicit
	// literal `options` array, or the descendant `value` literals) before it is
	// built, so its controller inputs match the parts.
	if b.declaresCSSTScope(childFn) {
		if spec := b.collectScopeSpec(el); len(spec.options) > 0 {
			b.setScopeOverride(childFn, spec)
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

	// Recursion guard: expanding a component that is already being expanded
	// with the same props can never terminate (see enterExpansion).
	leave, ok := b.enterExpansion(childFn.Name, el, attrs)
	if !ok {
		return []SlotNode{&StaticHTML{HTML: ""}}
	}
	defer leave()

	b.addContextProps(attrs)
	// CSS signal components must go through buildComponentNode so their
	// analyzer is installed and triggers/panels are rewritten. A panels-only
	// component has no signals/handlers and would otherwise take the SSREval
	// shortcut, which bypasses the interception and breaks the panels. The same
	// applies to components using first-class scope-handle spreads.
	if len(childSignals) == 0 && !b.cssSignalsOK(childFn) && !hasScopeHelperSpread(childFn) && !hasComponentChildren(el.Children) && !b.isLazyComponent(childName) {
		if !b.componentNeedsClient(childFn, attrs) {
			// A signal-less component that renders a parent's signal value
			// (e.g. <Display value={display()}/>) must stay reactive, so its
			// return JSX is inlined with the props substituted by their
			// call-site expressions.
			if b.componentHasSignalProps(attrs) {
				return b.inlineSignalPropsComponent(el, childFn, childID, attrs)
			}
			return b.buildSSREvalComponentSlot(el, childFn, childID, attrs)
		}
	}
	return b.buildClientComponentSlot(el, childFn, childID, attrs, provider)
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
	props := make(map[string]any, len(attrs))
	for name, expr := range attrs {
		props[name] = evalConstWithSignals(expr, b.sigMap(), b.localProps)
	}
	return []SlotNode{&ComponentSlot{
		ID: id,
		Component: &ComponentNode{
			ID:           id,
			Name:         childName,
			Tier:         TierRuntime,
			Props:        attrs,
			RuntimeProps: props,
			SourceFile:   b.ann.ComponentSources[childName],
			Line:         childFn.Position.Line,
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

	bindings := b.buildPropBindings(extractParamNames(childFn), attrs, childFn)
	childNode := &ComponentNode{
		ID:               childID,
		Name:             childName,
		Tier:             TierStatic,
		Fn:               childFn,
		SSREvalBindings:  bindings,
		SSREvalAttrs:     attrs,
		LocalVars:        b.foldLocalConsts(childFn, bindings, useIdSequence(childID)),
		IsSSREval:        true,
		CallSiteChildren: el.Children,
		RestProps:        restProps(childFn, attrs),
		RestPropsName:    fnRestParamName(childFn),
		ContextValues:    b.activeContextMarkers(),
	}

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
	childNode.Children = b.buildCallSiteChildSlots(el.Children, string(childID))
	restoreExpansion()
	b.callSiteChildren = savedCS

	if ret := findReturnStmt(childFn.Body); ret != nil && ret.Value != nil {
		childNode.ReturnSlots = b.discoverReturnSlots(childFn, childID, attrs, ret.Value)
	}
	return []SlotNode{&ComponentSlot{ID: childID, Component: childNode}}
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
	childNode := b.buildComponentNode(childFn, string(childID))
	b.popComponentCtx()
	childNode.ContextValues = b.activeContextMarkers()
	b.warnSnapshotProps(el, childFn, childNode, live)

	childNode.Props = attrs
	if lazy {
		childNode.Lazy = true
	}
	childNode.CallSiteChildren = el.Children
	childNode.CallSiteSlots = b.buildComponentCallSiteSlots(el, childID, provider, childNode.childrenContext)
	b.completeSSREvalBindings(childNode, childFn, attrs)
	b.registerChildProps(childNode, childFn)

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
	}
	b.localFuncProps = funcProps

	switch {
	case fnRestParamName(childFn) != "":
		// A rest parameter collects every call-site attr not bound by a
		// named/destructured parameter.
		bound := make(map[string]bool)
		for _, n := range extractParamNames(childFn) {
			bound[n] = true
		}
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
func (b *builder) buildComponentCallSiteSlots(el *ast.JSXElement, childID SlotID, provider scopeProvider, overlay *contextOverlay) []SlotNode {
	savedCallSite, savedScopes := b.callSiteChildren, b.contextScopes
	defer func() { b.callSiteChildren, b.contextScopes = savedCallSite, savedScopes }()
	defer b.callerExpansion()()
	defer b.applyChildrenContext(overlay)()
	b.callSiteChildren = el.Children
	if provider.ok {
		b.contextScopes = cloneScopeMap(savedScopes)
		b.contextScopes[provider.ctx] = provider.ref
	}
	return b.buildCallSiteChildSlots(el.Children, string(childID))
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
		len(childNode.ExtraVars) > 0 ||
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
	if len(live) == 0 || b.locateOnly > 0 || childNode == nil || childNode.Tier != TierClient {
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
	reads := func(code string) bool {
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
	}
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
