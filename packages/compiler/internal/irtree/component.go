package irtree

import (
	"maps"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/csssignals"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Component node construction.
//
// buildComponentNode runs in three steps:
//
//  1. newComponentNode allocates the node and its instance slot ID.
//  2. buildComponentScoped processes the body inside a pushed componentCtx:
//     signals and the signal table, hydration body fragments, props and
//     constants, folded locals, and the render return's slot tree.
//  3. finishClientComponent, back in the caller's context, attaches the
//     accumulated bindings and helper declarations and decides whether the
//     component can be promoted to the static tier.

// buildComponentNode builds the IR node for one instance of a component.
func (b *builder) buildComponentNode(fn *ast.FnDecl, parentID string) *ComponentNode {
	if fn == nil {
		return &ComponentNode{Name: "_nil", Tier: TierStatic}
	}
	node := b.newComponentNode(fn, parentID)
	res := b.buildComponentScoped(fn, node, parentID == "")
	if node.Tier == TierClient {
		b.finishClientComponent(node, fn, res)
	}
	return node
}

// componentResult is what the scoped build of a component hands to
// finishClientComponent once the caller's context is restored.
type componentResult struct {
	handlers    []HandlerDecl
	attrs       []AttrBinding
	refs        []RefBinding
	propsRegs   []string
	forceClient bool
}

// newComponentNode allocates the node for the next instance of fn. Instance
// disambiguation appends _cN so repeated uses of a component get distinct
// slot IDs.
func (b *builder) newComponentNode(fn *ast.FnDecl, parentID string) *ComponentNode {
	id := joinSlotID(parentID, fn.Name)
	tier := b.ann.ComponentTiers[fn.Name]
	if tier == TierUnknown {
		tier = TierClient
	}
	instanceIdx := b.instanceCounts[fn.Name]
	b.instanceCounts[fn.Name]++
	id = SlotID(string(id) + "_c" + itoa(instanceIdx))
	return &ComponentNode{
		ID:         id,
		Name:       fn.Name,
		Tier:       tier,
		Fn:         fn,
		SourceFile: b.ann.SourceFile,
		Line:       fn.Position.Line,
		Class:      b.ann.IsLoweredClass(fn.Name),
		Boundary:   b.ann.IsLoweredClass(fn.Name) && b.ann.ClassBoundaries[fn.Name],
	}
}

// buildComponentScoped processes fn's body for node inside its own component
// context. isPageRoot marks the entry component, which has no call site: its
// locals are folded for build-time evaluation but not emitted, so it can still
// be promoted to the static tier.
func (b *builder) buildComponentScoped(fn *ast.FnDecl, node *ComponentNode, isPageRoot bool) componentResult {
	b.pushComponentCtx()
	defer b.popComponentCtx()
	cssEnteredMark := len(b.cssEntered)
	defer func() {
		for len(b.cssEntered) > cssEnteredMark {
			n := len(b.cssEntered) - 1
			b.cssActive[b.cssEntered[n]]--
			b.cssEntered = b.cssEntered[:n]
		}
	}()
	// The caller's sibling counters survive the child's walk; otherwise an
	// element after a child component reuses an earlier sibling's slot ID.
	savedCounts := maps.Clone(b.elementCounts)
	defer func() { b.elementCounts = savedCounts }()

	// Scope handles are component-scoped: only this component's own
	// declarations and its scope-valued props are visible while building it.
	b.scopeHandles = make(map[string]scopeRef)
	b.currentNode = node

	// Signal declarations are collected against the caller's signal table;
	// everything after runs against this component's own.
	node.Signals = b.collectSignalDecls(fn)
	b.installSignalTable(node.Signals)
	node.BodyUses = b.collectBodySignalUses(fn.Body, node.Signals)
	b.signalSetters = make(map[string]bool, len(node.Signals))
	for _, sig := range node.Signals {
		if sig.SetterName != "" {
			b.signalSetters[sig.SetterName] = true
		}
	}

	body := b.collectHydrationBody(fn, node)
	b.enterComponentBody(fn)
	if node.Tier == TierClient {
		b.registerReactiveLocals(fn)
	}
	b.installComponentProps(fn, node)
	b.foldComponentLocals(fn, node, isPageRoot, body)
	b.buildComponentReturn(fn, node)

	return componentResult{
		handlers:    b.pendingHandlers,
		attrs:       b.pendingAttrs,
		refs:        b.pendingRefs,
		propsRegs:   b.pendingPropsRegs,
		forceClient: b.forceClient,
	}
}

// installSignalTable sets the component-local signal context used for body
// uses, effects, memos and extra vars. The table holds exactly this
// component's own signals: a parent's (or another component's) signal of the
// same name is not in scope inside this component's hydration IIFE, so it must
// never classify this component's identifiers as reactive. The walk starts at
// the component top level, where no nested binding shadows anything.
func (b *builder) installSignalTable(signals []SignalDecl) {
	caller := b.localSignals
	b.shadow = nil
	own := make(map[string]ast.Expr, len(signals))
	for _, sig := range signals {
		if sig.InitialExpr != nil {
			own[sig.Name] = sig.InitialExpr
		} else {
			own[sig.Name] = &ast.Literal{Kind: ast.NullLit, Value: "undefined"}
		}
	}
	// A dynamic `<Ctx.Provider value={expr}>` value is substituted for
	// `Ctx.useContext()` reads in descendants while they are built, so the
	// enclosing component's signals that expr reads stay resolvable (for the
	// SSR initial value) unless this component declares its own binding of the
	// same name. Hydration reads the value through the props registry.
	if len(b.contextValues) > 0 && caller != nil {
		ctxNames := make([]string, 0, len(b.contextValues))
		for ctx := range b.contextValues {
			ctxNames = append(ctxNames, ctx)
		}
		sort.Strings(ctxNames)
		for _, ctx := range ctxNames {
			scanJSIdents(generateExprJS(b.contextValues[ctx], nil), func(name string) {
				if _, mine := own[name]; mine {
					return
				}
				if init, ok := caller[name]; ok {
					own[name] = init
				}
			})
		}
	}
	b.localSignals = own
}

// hydrationBody holds a client component's body fragments for the hydration
// scope, tagged with their source position (see collectExtraVarJS).
type hydrationBody struct {
	order     *bodyOrder
	pre, post []bodyJS
}

// collectHydrationBody collects a client component's effects, memos and body
// fragments. The folded locals added by foldComponentLocals merge into the
// same source order the component body runs in.
func (b *builder) collectHydrationBody(fn *ast.FnDecl, node *ComponentNode) hydrationBody {
	body := hydrationBody{order: newBodyOrder(fn.Body)}
	if node.Tier != TierClient {
		return body
	}
	node.InstanceID = deriveInstanceID(string(node.ID))
	node.Effects = b.collectEffectJS(fn.Body)
	node.Memos = b.collectMemoJS(fn.Body)
	body.pre, body.post = b.collectExtraVarJS(fn.Body, body.order, paramBindingSet(fn))
	body.post = append(body.post, b.collectResourceJS(fn.Body, body.order)...)
	factoryPre, factoryPost := b.collectReactiveFactoryJS(fn.Body, body.order)
	body.pre = append(body.pre, factoryPre...)
	body.post = append(body.post, factoryPost...)
	body.pre, body.post = promoteTransitive(body.pre, body.post)
	node.PreSignalVars = mergeBodyJS(body.pre)
	node.ExtraVars = mergeBodyJS(body.post)
	return body
}

// enterComponentBody resets the per-walk counters and binding accumulators
// and records the body-level facts handler and ref resolution consult.
func (b *builder) enterComponentBody(fn *ast.FnDecl) {
	for k := range b.elementCounts {
		delete(b.elementCounts, k)
	}
	b.pendingHandlers = nil
	b.pendingAttrs = nil
	b.pendingRefs = nil
	b.pendingPropsRegs = nil
	b.localFnBody = fn.Body
	// Locals that are useRef {current:...} objects: `ref={myRef}` assigns
	// `.current` instead of clobbering the object.
	b.refObjectVars = collectRefObjectVars(fn.Body)
	// Locals holding functions: `ref={setRef}` is a callback ref that
	// kbindRef invokes with the node, NOT an assignment target (which would
	// overwrite the function variable with the element).
	b.refCallbackVars = collectFunctionRefVars(fn.Body)
}

// registerReactiveLocals makes named memos and resource getters reactive
// getters in the signal table. A memo maps to its arrow body expression so
// `{doubled()}` becomes a text binding with a computed SSR initial value; a
// resource getter maps to a sentinel so `{user.loading}` / `{user.state}`
// resolve their initial value during SSR while still binding at hydration.
func (b *builder) registerReactiveLocals(fn *ast.FnDecl) {
	if b.localSignals == nil {
		return
	}
	for name, arrow := range b.collectNamedMemos(fn.Body) {
		if bodyExpr := arrowBodyExpr(arrow); bodyExpr != nil {
			b.localSignals[name] = bodyExpr
		}
	}
	for _, name := range b.collectResourceNames(fn.Body) {
		b.localSignals[name] = resourceSentinel
	}
	b.registerFactoryLocals(fn.Body)
	// Derived render values and custom hook results (derived.go).
	for name, initial := range b.collectDerivedLocals(fn.Body) {
		b.localSignals[name] = initial
	}
}

// installComponentProps completes the local-prop scope the call site set up
// (buildComponentSlot populates b.localProps with resolved call-site values):
// parameter names and defaults, module-level constants and createContext
// defaults, each added only where no closer binding exists.
func (b *builder) installComponentProps(fn *ast.FnDecl, node *ComponentNode) {
	// An absent parameter folds to "undefined" (or its default), so an
	// omitted prop like `className` in `cn("base", className)` resolves as a
	// known undefined rather than leaking the identifier.
	b.localParamNames = make(map[string]bool)
	for _, name := range extractParamNames(fn) {
		b.localParamNames[name] = true
		if b.localProps == nil {
			b.localProps = make(valueMap)
		}
		// A function prop the caller passed replaces the default. It lives in
		// localFuncProps, not localProps, so absence here does not mean omitted.
		if _, exists := b.localProps[name]; !exists && !b.localFuncProps[name] {
			b.localProps[name] = b.paramDefaultValue(fn, name)
		}
	}
	if b.localFuncProps != nil {
		node.FuncProps = b.localFuncProps
	}
	// Module-level constants (const X = <literal>) resolve in JSX text such
	// as {hexNum} instead of leaking the identifier name.
	if len(b.moduleConsts) > 0 {
		if b.localProps == nil {
			b.localProps = make(valueMap)
		}
		for k, v := range b.moduleConsts {
			if _, exists := b.localProps[k]; !exists {
				b.localProps[k] = v
			}
		}
	}
	// Router hooks fold to the page's route.
	if len(b.routeHooks) > 0 {
		if b.localProps == nil {
			b.localProps = make(valueMap)
		}
		for k, v := range b.routeHooks {
			if _, exists := b.localProps[k]; !exists {
				b.localProps[k] = v
			}
		}
	}
	// `createContext(default)` bindings are seeded under a reserved key so
	// `Ctx.useContext()` folds to the default at build time (no Provider
	// exists during SSG).
	if len(b.contextDefaults) > 0 {
		if b.localProps == nil {
			b.localProps = make(valueMap)
		}
		for k, v := range b.contextDefaults {
			key := contextMarkerPrefix + k
			if _, exists := b.localProps[key]; !exists {
				b.localProps[key] = v
			}
		}
	}
	// A page root starts with a nil localProps map, but its locals still need
	// folding so slot initials resolve to their values.
	if b.localProps == nil {
		b.localProps = make(valueMap)
	}
}

// foldComponentLocals resolves the component's local variables to build-time
// values for SSR and emits the declarations the hydration scope needs. Locals
// that alias a function prop (`var onNavigate = props.onNavigate`) are live
// registry reads instead.
func (b *builder) foldComponentLocals(fn *ast.FnDecl, node *ComponentNode, isPageRoot bool, body hydrationBody) {
	client := node.Tier == TierClient
	fold := b.foldLocals(fn, node)
	var funcPropAliases []string
	if b.localFuncProps != nil && client {
		// Function-prop aliases leave the fold set so they aren't serialized
		// as the function's name string. Declaration order keeps the output
		// deterministic.
		for _, name := range fold.order {
			if _, ok := fold.locals[name]; !ok {
				continue
			}
			if prop := funcPropAliasOf(fn.Body, name, b.localFuncProps); prop != "" {
				funcPropAliases = append(funcPropAliases, name+"=props."+prop)
				delete(fold.locals, name)
			}
		}
	}
	// useId locals are referenced by SSR-resolved attributes and any client
	// bindings, so they are declared for every client component, including
	// the page root (whose ordinary locals stay unemitted).
	var useIdVars []string
	if client {
		declared := declaredLocalNames(node, fn)
		for _, name := range sortedKeys(fold.useIds) {
			if declared[name] {
				continue
			}
			useIdVars = append(useIdVars, "var "+name+"="+jsLiteralFor(fold.useIds[name]))
		}
		node.ExtraVars = append(node.ExtraVars, useIdVars...)
	}
	b.liveContextLocals(fold)
	var preLocals, postLocals []bodyJS
	if len(fold.locals) > 0 {
		b.bindFoldedLocals(fold)
		// Locals are declared for children only: collectExtraVarJS excludes
		// props-derived locals so a purely presentational root can be
		// promoted to the static tier.
		if client && !isPageRoot {
			preLocals, postLocals = b.localDecls(fn, node, fold, body.order)
		}
	}
	if client {
		// Destructured names without a build-time value are declared by
		// replaying the declaration, also in a page root (whose other locals
		// are baked into the HTML).
		pre, post := b.leakedDestructDecls(fold, body.order)
		preLocals = append(preLocals, pre...)
		postLocals = append(postLocals, post...)
	}
	if client {
		pre, post := promoteTransitive(append(append([]bodyJS(nil), body.pre...), preLocals...),
			append(append([]bodyJS(nil), body.post...), postLocals...))
		node.PreSignalVars = mergeBodyJS(pre)
		node.ExtraVars = append(useIdVars, mergeBodyJS(post)...)
		if len(funcPropAliases) > 0 {
			// The alias reads props.<funcProp>, emitted after the registry
			// read that binds `props`.
			node.FuncPropAliases = append(node.FuncPropAliases, funcPropAliases...)
		}
	}
}

// foldLocals runs collectLocalVars for the component and applies its results
// to the local-prop scope: changed locals replace same-named bindings, and
// locals whose initializer needs the cva/cn table are re-folded.
func (b *builder) foldLocals(fn *ast.FnDecl, node *ComponentNode) *localFold {
	// useId() resolves to a per-component-instance build-time literal, stable
	// across SSR and hydration.
	fold := collectLocalVarsWithBuilder(fn.Body, b.sigMap(), b.localProps, b.useIDSequence(node.ID), b)
	// A changed local replaces any same-named binding: with its final folded
	// value, or (when it has none) by removal so reads defer to hydration.
	for name := range fold.changed {
		if fold.leaked[name] {
			delete(b.localProps, name)
		} else {
			b.localProps[name] = fold.locals[name]
		}
	}
	// Re-fold locals whose initializer needs the cva/cn table (e.g. shadcn's
	// `const classes = cn(buttonVariants({ variant }), className)`), so JSX
	// attributes reference the class value instead of the variable name.
	destructured := destructNames(fold.destructs)
	for name, init := range fold.inits {
		if init == nil || fold.changed[name] || destructured[name] {
			continue
		}
		// Array/object/JSX locals may be mutated by loops or emitted as
		// runtime structures; folding them would skip those statements.
		switch init.(type) {
		case *ast.ArrayExpr, *ast.ObjectExpr, *ast.JSXElement, *ast.JSXFragment, *ast.ArrowFn:
			continue
		}
		if v := b.evalAttrConst(init); !v.ContainsUnknown() && !v.IsFunction() && !v.IsJSX() {
			fold.locals[name] = v
			if b.localProps == nil {
				b.localProps = make(valueMap)
			}
			b.localProps[name] = v
		}
	}
	if len(fold.locals) > 0 {
		node.LocalVars = make(map[string]jsvalue.Value, len(fold.locals))
		for name, val := range fold.locals {
			if !val.IsUnknown() {
				node.LocalVars[name] = val
			}
		}
	}
	if len(fold.callables) > 0 {
		if b.localProps == nil {
			b.localProps = make(valueMap, len(fold.callables))
		}
		for name, fn := range fold.callables {
			b.localProps[name] = fn
		}
	}
	return fold
}

// liveContextLocals turns dynamic context locals (`const v =
// Ctx.useContext()` under an active provider) into live reads of the getter
// prop instead of the context default folded at build time.
func (b *builder) liveContextLocals(fold *localFold) {
	for name, init := range fold.inits {
		if init == nil {
			continue
		}
		ctx, ok := contextReadTarget(init)
		if !ok {
			continue
		}
		if _, active := b.contextValues[ctx]; !active {
			continue
		}
		fold.inits[name] = &ast.MemberExpr{
			Object:   &ast.Identifier{Name: "props"},
			Property: &ast.Identifier{Name: contextPropPrefix + ctx},
		}
		fold.locals[name] = jsvalue.Dynamic("context " + ctx)
		fold.leaked[name] = true
	}
}

// bindFoldedLocals adds folded locals to the local-prop scope. Leaked locals
// have no known value and stay OUT of it, so guards referencing them fold as
// unknown (a ConditionalSlot is emitted) instead of dropping a branch.
func (b *builder) bindFoldedLocals(fold *localFold) {
	for k, v := range fold.locals {
		if fold.leaked[k] {
			continue
		}
		if _, exists := b.localProps[k]; !exists {
			b.localProps[k] = v
		}
	}
}

// localDecls returns the hydration declarations of a child component's
// folded locals in source order, split into those that may precede the
// signal declarations and those that read signals.
func (b *builder) localDecls(fn *ast.FnDecl, node *ComponentNode, fold *localFold, order *bodyOrder) (pre, post []bodyJS) {
	declared := declaredLocalNames(node, fn)
	replayed := destructNames(b.replayedDestructs(fold))
	// Source order, not alphabetical: a local may reference an earlier local
	// (e.g. `basePath` derives from `themeOptions`).
	for _, name := range fold.order {
		if _, ok := fold.locals[name]; !ok {
			continue
		}
		if declared[name] || replayed[name] {
			continue
		}
		init := fold.inits[name]
		frag := bodyJS{ord: order.ofName(name), js: b.localDeclJS(name, init, fold)}
		// Signal-referencing locals must be declared after the signal
		// declarations; everything else merges with the pre fragments.
		if init != nil && b.referencesSignal(init) {
			post = append(post, frag)
		} else {
			pre = append(pre, frag)
		}
	}
	return pre, post
}

// replayedDestructs returns the destructuring declarations the hydration
// scope evaluates as written: one with a name that has no build-time value or
// is changed later, or whose initializer reads props, another local or a
// reference. Names of the others are plain folded values.
func (b *builder) replayedDestructs(fold *localFold) []*ast.VarDecl {
	var out []*ast.VarDecl
	for _, d := range fold.destructs {
		replay := d.Init != nil && (referencesProps(d.Init) || referencesAnyLocal(d.Init, b.sigMap(), fold.locals) || isReferenceInit(d.Init))
		for _, n := range d.Names {
			if fold.leaked[n] || fold.changed[n] {
				replay = true
			}
		}
		if replay {
			out = append(out, d)
		}
	}
	return out
}

// destructNames returns the names bound by the declarations.
func destructNames(decls []*ast.VarDecl) map[string]bool {
	names := make(map[string]bool)
	for _, d := range decls {
		for _, n := range d.Names {
			names[n] = true
		}
	}
	return names
}

// leakedDestructDecls renders each replayed destructuring declaration as one
// `var {a, b: c} = init` fragment, split like localDecls into those that may
// precede the signal declarations and those that read signals.
func (b *builder) leakedDestructDecls(fold *localFold, order *bodyOrder) (pre, post []bodyJS) {
	for _, d := range b.replayedDestructs(fold) {
		pattern := d.Pattern
		if d.Target != nil {
			pattern = renderPattern(d.Target, b.sigMap())
		} else if pattern == "" {
			pattern = "[" + strings.Join(d.Names, ",") + "]"
		}
		frag := bodyJS{ord: order.of(d), js: "var " + pattern + "=" + generateExprJS(d.Init, b.sigMap())}
		if b.referencesSignal(d.Init) {
			post = append(post, frag)
		} else {
			pre = append(pre, frag)
		}
	}
	return pre, post
}

// localDeclJS renders one folded local's hydration declaration. A local is
// re-evaluated at runtime (`var items=props.items||[]`) when its value depends
// on props or another local, is a reference to a function/object binding, has
// no build-time value, or is changed later in the body (the replayed
// statements apply the changes). Reference initializers are rendered from the
// AST, since a folded string cannot be told apart from a string constant. A
// folded, non-reactive scalar (e.g. `const classes = cn(cva(...))`) is
// emitted as its value.
func (b *builder) localDeclJS(name string, init ast.Expr, fold *localFold) string {
	locals := fold.locals
	changed := fold.changed[name]
	if derivedCallee(init) != "" {
		// A derived getter is always re-created at hydration.
		return "var " + name + "=" + generateExprJS(b.derivedDeclInit(init), b.sigMap())
	}
	foldable := init != nil && !changed && !locals[name].IsUnknown() && !b.referencesSignal(init) && !isReferenceInit(init)
	if foldable {
		switch init.(type) {
		case *ast.ArrayExpr, *ast.ObjectExpr, *ast.JSXElement, *ast.JSXFragment, *ast.ArrowFn:
			foldable = false
		}
	}
	if init != nil && !foldable && (changed || referencesProps(init) || referencesAnyLocal(init, b.sigMap(), locals) || isReferenceInit(init) || locals[name].IsUnknown()) {
		if js := generateExprJS(init, b.sigMap()); js != "" {
			return "var " + name + "=" + js
		}
	}
	return "var " + name + "=" + valueJSLiteral(locals[name])
}

// buildComponentReturn lowers the render return (early-return guards become a
// conditional chain) and builds the component's slot tree. Handlers and
// bindings accumulate in the component context during the walk.
func (b *builder) buildComponentReturn(fn *ast.FnDecl, node *ComponentNode) {
	returnStmt, retIssue := lowerRenderReturn(fn.Body)
	if retIssue != nil && !b.returnIssueReported[fn] {
		if b.returnIssueReported == nil {
			b.returnIssueReported = make(map[*ast.FnDecl]bool)
		}
		b.returnIssueReported[fn] = true
		b.errorAt(retIssue.pos, "component <"+fn.Name+">: "+retIssue.msg, retIssue.hint)
	}
	if returnStmt == nil || returnStmt.Value == nil {
		return
	}
	retExpr := b.enterCSSScope(fn, node, returnStmt)

	// A context value this component received through its synthetic getter
	// prop is forwarded to its own children as a call of that getter (the
	// provider's names are not in scope in this component's hydration IIFE).
	// Function-valued props live in localFuncProps rather than localProps.
	childContext := cloneExprMap(b.contextValues)
	forward := func(name string) {
		if strings.HasPrefix(name, contextPropPrefix) {
			ctx := strings.TrimPrefix(name, contextPropPrefix)
			childContext[ctx] = &ast.CallExpr{Callee: &ast.MemberExpr{
				Object:   &ast.Identifier{Name: "props"},
				Property: &ast.Identifier{Name: name},
			}}
		}
	}
	for name := range b.localProps {
		forward(name)
	}
	for name := range b.localFuncProps {
		forward(name)
		// Build-time reads of the forwarded getter (`props.__krate_ctx_X()`
		// in this component's own JSX) evaluate to the provider's initial.
		ctx := strings.TrimPrefix(name, contextPropPrefix)
		if initial, ok := b.contextInitials[ctx]; ok && strings.HasPrefix(name, contextPropPrefix) {
			if _, exists := b.localProps[name]; !exists {
				b.localProps[name] = jsvalue.Func(name, func([]jsvalue.Value) jsvalue.Value { return initial })
			}
		}
	}
	b.contextValues = childContext
	b.enterContextEntry()
	node.Children = b.buildSlotNodes(retExpr, string(node.ID))
}

// enterCSSScope installs the component's CSS signal state for the walk of
// its return and returns the expression to build. CSS components render
// through the normal slot pipeline: the analyzer drives trigger/panel rewrites
// and the controller inputs are injected into the component's scope anchor.
func (b *builder) enterCSSScope(fn *ast.FnDecl, node *ComponentNode, returnStmt *ast.ReturnStmt) ast.Expr {
	analyzer := b.cssAnalyzer(fn)
	if analyzer != nil && analyzer.HaveAny() && !analyzer.OK() {
		// Hard error: no fallback. The declaration is not compiled to signals
		// either, so a broken component must fail the build.
		for _, reason := range analyzer.Errors() {
			b.collectCSSErr(fn.Name, returnStmt.Position.Line, reason)
		}
	}

	// Scope-valued props from the parent are visible to this component
	// whether or not it declares its own scope.
	for k, ref := range b.pendingScopeProps {
		b.scopeHandles[k] = ref
	}
	b.pendingScopeProps = nil
	// Context-provided scope handles (`const s = Ctx.useContext()`).
	b.registerContextReads(fn)
	// Scope handles are compile-time state: never emit them as runtime vars.
	node.PreSignalVars = dropVarDecls(node.PreSignalVars, b.scopeHandles)
	node.ExtraVars = dropVarDecls(node.ExtraVars, b.scopeHandles)

	retExpr := returnStmt.Value
	if analyzer == nil || !analyzer.OK() {
		return retExpr
	}
	b.cssSignals = analyzer
	b.cssToken = csssignals.SanitizeToken(string(node.ID))
	if b.cssActive == nil {
		b.cssActive = make(map[*ast.FnDecl]int)
	}
	b.cssActive[fn]++
	b.cssEntered = append(b.cssEntered, fn)
	for _, s := range analyzer.Scopes() {
		b.collectCSSScope(s)
	}
	b.registerScopeLocals(fn, analyzer, b.cssToken)
	for _, c := range analyzer.Conditions() {
		b.collectCSSCondition(c)
	}
	if analyzer.NeedsARIA() {
		b.cssNeedsARIA = true
	}
	// Inject the scope class + controller inputs into the component's root
	// (or a display:contents wrapper when there is no single intrinsic root),
	// on a copy so multiple instances don't accumulate them in the shared AST.
	return b.prepareCSSRoot(retExpr, analyzer.Scopes(), b.cssToken)
}

// finishClientComponent attaches the bindings a client component's walk
// accumulated, declares the local and module-level helpers its hydration code
// references, and promotes it to the static tier when it needs no client JS.
// It runs in the caller's context: helper declarations are rendered against
// the caller's signal table.
func (b *builder) finishClientComponent(node *ComponentNode, fn *ast.FnDecl, res componentResult) {
	if b.pendingLazy != nil && fn == b.pendingLazyFn {
		// A lazy() instance always hydrates: it suspends until its chunk or
		// loader is ready.
		node.Lazy = true
		if b.pendingLazy.Loader != "" {
			b.lazyLoaderGate(node, *b.pendingLazy)
		}
		b.pendingLazy, b.pendingLazyFn = nil, nil
	}
	node.Handlers = res.handlers
	node.AttrBindings = res.attrs
	node.RefBindings = res.refs
	// Props registrations for child components run in this component's scope
	// (their values reference its signals), before child IIFEs read them.
	if len(res.propsRegs) > 0 {
		node.ExtraVars = append(node.ExtraVars, res.propsRegs...)
	}
	// Local and module-level functions referenced by handlers, effects, memos
	// and dynamic list slot expressions must be in hydration scope, and so
	// must the module-level constants they (and the hydration code) read,
	// which are declared before the signals and pre-signal vars. A constant's
	// initializer can call an imported helper (`const store =
	// createStore(0)`), so both are resolved until nothing new is found.
	emittedFns := map[string]bool{}
	addFunctions := func() {
		for _, fnDecl := range b.collectReferencedFunctions(node, fn.Body) {
			if emittedFns[fnDecl.Name] {
				continue
			}
			emittedFns[fnDecl.Name] = true
			if fnJS := renderFnDeclJS(fnDecl, b.sigMap()); fnJS != "" {
				node.ExtraVars = append(node.ExtraVars, fnJS)
			}
		}
	}
	addFunctions()
	var constDecls []string
	for round := 0; round < 4; round++ {
		decls := b.referencedModuleConstVars(node)
		if len(decls) == len(constDecls) {
			break
		}
		base := node.PreSignalVars[len(constDecls):]
		node.PreSignalVars = append(append([]string(nil), decls...), base...)
		constDecls = decls
		addFunctions()
	}
	dropShadowedFunctions(node, constDecls)
	b.checkUnbundledImports(node)
	if needsNoClientJS(node, fn, b.localFuncProps) && !res.forceClient && !b.rendersLiveProps(node) && !node.Lazy {
		node.Tier = TierStatic
	}
}

// dropShadowedFunctions removes a function declaration from the component's
// extra vars when a module constant of the same name is declared as well. A
// capitalized arrow constant (`export const Add = (x) => ...`) is known both
// as a constant and as a function, and `var Add=` next to `function Add` in
// one block is a syntax error. The constant stays: it is the original code.
func dropShadowedFunctions(node *ComponentNode, constDecls []string) {
	if len(constDecls) == 0 || len(node.ExtraVars) == 0 {
		return
	}
	declared := map[string]bool{}
	for _, d := range constDecls {
		if rest, ok := strings.CutPrefix(d, "var "); ok {
			if i := strings.IndexAny(rest, "=;"); i > 0 {
				declared[strings.TrimSpace(rest[:i])] = true
			}
		}
	}
	kept := node.ExtraVars[:0:0]
	for _, v := range node.ExtraVars {
		if rest, ok := strings.CutPrefix(strings.TrimPrefix(v, "async "), "function"); ok {
			rest = strings.TrimPrefix(strings.TrimSpace(rest), "*")
			if i := strings.IndexByte(rest, '('); i > 0 && declared[strings.TrimSpace(rest[:i])] {
				continue
			}
		}
		kept = append(kept, v)
	}
	node.ExtraVars = kept
}

// needsNoClientJS reports whether a client component has nothing to hydrate
// (props in, JSX out) and can be SSR-evaluated at build time with zero client
// JS: layout components, presentational components, any function that only
// composes JSX from its props. A ref passed to a child component needs this
// component's hydration scope to create/bind the ref object, and a component
// that provides a CSS scope via context renders its call-site children
// through the slot tree, so both stay client.
//
// funcProps names the props that hold functions at this call site: build-time
// evaluation cannot call them, so a slot that reads one needs the client.
func needsNoClientJS(node *ComponentNode, fn *ast.FnDecl, funcProps map[string]bool) bool {
	hasComponentRef := false
	if ret := findReturnStmt(fn.Body); ret != nil {
		hasComponentRef = hasRefAttrExpr(ret.Value)
	}
	_, _, providesScope := providedScopeContext(fn)
	return !slotsHaveExpr(node.Children, paramBindingSet(fn), funcProps) &&
		len(node.Signals) == 0 && len(node.Effects) == 0 &&
		len(node.Memos) == 0 && onlyFoldedVars(node) &&
		len(node.Handlers) == 0 && len(node.AttrBindings) == 0 &&
		len(node.RefBindings) == 0 && !hasComponentRef && !providesScope
}

// onlyFoldedVars reports whether the component declares nothing the hydration
// scope needs: no body vars, or only vars whose value was folded at build time
// (they are baked into the HTML, so a hydration script would only repeat
// them) in a component without a hydrating child, which may read them. It
// drops the vars when it holds.
func onlyFoldedVars(node *ComponentNode) bool {
	if len(node.ExtraVars) == 0 && len(node.PreSignalVars) == 0 {
		return true
	}
	if hasClientChild(node.Children) {
		return false
	}
	for _, list := range [][]string{node.ExtraVars, node.PreSignalVars} {
		for _, v := range list {
			if !foldedVarDecl(node, v) {
				return false
			}
		}
	}
	node.ExtraVars, node.PreSignalVars = nil, nil
	return true
}

// foldedVarDecl reports whether v is a `var name=...` declaration of a local
// with a build-time value.
func foldedVarDecl(node *ComponentNode, v string) bool {
	v = strings.TrimSuffix(strings.TrimPrefix(v, PureVarBegin), PureVarEnd)
	rest, ok := strings.CutPrefix(v, "var ")
	if !ok {
		return false
	}
	end := 0
	for end < len(rest) && isIdentChar(rest[end]) {
		end++
	}
	if end == 0 || end == len(rest) || rest[end] != '=' {
		return false
	}
	_, folded := node.LocalVars[rest[:end]]
	return folded
}

// hasClientChild reports whether a hydrating component is nested anywhere in
// slots.
func hasClientChild(slots []SlotNode) bool {
	for _, s := range slots {
		switch v := s.(type) {
		case *ComponentSlot:
			if v.Component != nil && v.Component.Tier == TierClient {
				return true
			}
		case *ConditionalSlot:
			if hasClientChild(v.Consequent) || hasClientChild(v.Alternate) {
				return true
			}
		case *ListSlot:
			for _, it := range v.Items {
				if it != nil && hasClientChild(it.Contents) {
					return true
				}
			}
		case *SuspenseSlot:
			if hasClientChild(v.Fallback) || hasClientChild(v.Resolved) || (v.Primary != nil && v.Primary.Tier == TierClient) {
				return true
			}
		case *GoComponentSlot:
			if hasClientChild(v.Children) {
				return true
			}
		case *MetaSlot:
			if hasClientChild(v.Children) {
				return true
			}
		}
	}
	return false
}

// readsFuncProp reports whether src reads one of the function props, as a
// bare parameter or as `props.name`.
func readsFuncProp(src string, funcProps map[string]bool) bool {
	for name := range funcProps {
		for _, form := range []string{"props." + name, "props?." + name} {
			for from := 0; ; {
				i := strings.Index(src[from:], form)
				if i < 0 {
					break
				}
				end := from + i + len(form)
				if end >= len(src) || !isIdentChar(src[end]) {
					return true
				}
				from = end
			}
		}
	}
	return false
}

// slotsHaveExpr reports whether any slot (at any depth of the node's own
// tree) is an ExprSlot the build could not fold, which needs the component's
// hydration script to produce its value. An expression over the component's
// own props or route params stays static: the server renderer evaluates it
// with the call-site or route bindings.
func slotsHaveExpr(slots []SlotNode, params, funcProps map[string]bool) bool {
	for _, s := range slots {
		switch v := s.(type) {
		case *ExprSlot:
			usesParam := false
			funcRead := readsFuncProp(v.ExprSource, funcProps)
			scanJSIdents(v.ExprSource, func(n string) {
				if funcProps[n] {
					// A function prop cannot be called at build time.
					funcRead = true
				} else if params[n] {
					usesParam = true
				}
			})
			if funcRead {
				return true
			}
			if !usesParam {
				return true
			}
		case *ConditionalSlot:
			if slotsHaveExpr(v.Consequent, params, funcProps) || slotsHaveExpr(v.Alternate, params, funcProps) {
				return true
			}
		case *ListSlot:
			for _, it := range v.Items {
				if it != nil && slotsHaveExpr(it.Contents, params, funcProps) {
					return true
				}
			}
		case *SuspenseSlot:
			if slotsHaveExpr(v.Fallback, params, funcProps) || slotsHaveExpr(v.Resolved, params, funcProps) {
				return true
			}
		}
	}
	return false
}
