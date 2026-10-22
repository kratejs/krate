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
	}
}

// buildComponentScoped processes fn's body for node inside its own component
// context. isPageRoot marks the entry component, which has no call site: its
// locals are folded for build-time evaluation but not emitted, so it can still
// be promoted to the static tier.
func (b *builder) buildComponentScoped(fn *ast.FnDecl, node *ComponentNode, isPageRoot bool) componentResult {
	b.pushComponentCtx()
	defer b.popComponentCtx()
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
	node.PreSignalVars = bodyJSStrings(body.pre)
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
		if _, exists := b.localProps[name]; !exists {
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
				b.localProps[key] = jsvalue.FromLegacy(v)
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
		node.PreSignalVars = mergeBodyJS(body.pre, preLocals)
		node.ExtraVars = append(useIdVars, mergeBodyJS(body.post, postLocals)...)
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
	fold := collectLocalVars(fn.Body, b.sigMap(), b.localProps, useIdSequence(node.ID))
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
	for name, init := range fold.inits {
		if init == nil || fold.changed[name] {
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
	// Source order, not alphabetical: a local may reference an earlier local
	// (e.g. `basePath` derives from `themeOptions`).
	for _, name := range fold.order {
		if _, ok := fold.locals[name]; !ok {
			continue
		}
		if declared[name] {
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
	}
	b.contextValues = childContext
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
	node.Handlers = res.handlers
	node.AttrBindings = res.attrs
	node.RefBindings = res.refs
	// Props registrations for child components run in this component's scope
	// (their values reference its signals), before child IIFEs read them.
	if len(res.propsRegs) > 0 {
		node.ExtraVars = append(node.ExtraVars, res.propsRegs...)
	}
	// Local and module-level functions referenced by handlers, effects, memos
	// and dynamic list slot expressions must be in hydration scope.
	for _, fnDecl := range b.collectReferencedFunctions(node, fn.Body) {
		if fnJS := renderStmtJS(fnDecl, b.sigMap()); fnJS != "" {
			node.ExtraVars = append(node.ExtraVars, fnJS)
		}
	}
	// Module-level constants referenced from hydration code are declared
	// before the signals and pre-signal vars.
	if decls := b.referencedModuleConstVars(node); len(decls) > 0 {
		node.PreSignalVars = append(decls, node.PreSignalVars...)
	}
	if needsNoClientJS(node, fn) && !res.forceClient && !b.rendersLiveProps(node) {
		node.Tier = TierStatic
	}
}

// needsNoClientJS reports whether a client component has nothing to hydrate
// (props in, JSX out) and can be SSR-evaluated at build time with zero client
// JS: layout components, presentational components, any function that only
// composes JSX from its props. A ref passed to a child component needs this
// component's hydration scope to create/bind the ref object, and a component
// that provides a CSS scope via context renders its call-site children
// through the slot tree, so both stay client.
func needsNoClientJS(node *ComponentNode, fn *ast.FnDecl) bool {
	hasComponentRef := false
	if ret := findReturnStmt(fn.Body); ret != nil {
		hasComponentRef = hasRefAttrExpr(ret.Value)
	}
	_, _, providesScope := providedScopeContext(fn)
	return len(node.Signals) == 0 && len(node.Effects) == 0 &&
		len(node.Memos) == 0 && len(node.ExtraVars) == 0 &&
		len(node.PreSignalVars) == 0 &&
		len(node.Handlers) == 0 && len(node.AttrBindings) == 0 &&
		len(node.RefBindings) == 0 && !hasComponentRef && !providesScope
}
