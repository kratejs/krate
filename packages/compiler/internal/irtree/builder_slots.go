package irtree

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/csssignals"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	"github.com/kratejs/krate/packages/compiler/internal/sigutil"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

// buildComponentNode

func (b *builder) buildComponentNode(fn *ast.FnDecl, parentID string) *ComponentNode {
	if fn == nil {
		return &ComponentNode{Name: "_nil", Tier: TierStatic}
	}

	id := joinSlotID(parentID, fn.Name)
	tier := b.ann.ComponentTiers[fn.Name]
	if tier == TierUnknown {
		tier = TierClient
	}

	// Instance disambiguation: append _cN to prevent slot ID collisions
	// when the same component appears multiple times.
	instanceIdx := b.instanceCounts[fn.Name]
	b.instanceCounts[fn.Name]++
	id = SlotID(string(id) + "_c" + itoa(instanceIdx))

	node := &ComponentNode{
		ID:         id,
		Name:       fn.Name,
		Tier:       tier,
		Fn:         fn,
		SourceFile: b.ann.SourceFile,
		Line:       fn.Position.Line,
	}

	node.Signals = b.collectSignalDecls(fn)
	node.BodyUses = b.collectBodySignalUses(fn.Body, node.Signals)

	// Set component-local signal context before collecting effects/memos/extra
	// vars: component's own signals take precedence over the global annotation
	// map so signal reads resolve to the correct initial value even with name
	// collisions across components - and signal-referencing classifications for
	// extra vars see the current component's signals, not just the parent's.
	savedLocalSignals := b.localSignals
	if len(node.Signals) > 0 {
		merged := make(map[string]ast.Expr, len(b.ann.Signals)+len(node.Signals))
		for k, v := range b.ann.Signals {
			merged[k] = v
		}
		for _, sig := range node.Signals {
			if sig.InitialExpr != nil {
				merged[sig.Name] = sig.InitialExpr
			}
		}
		b.localSignals = merged
	}

	// Client-only: effects, memos, extra vars
	if tier == TierClient {
		node.InstanceID = deriveInstanceID(string(id))
		node.Effects = b.collectEffectJS(fn.Body)
		node.Memos = b.collectMemoJS(fn.Body)
		node.PreSignalVars, node.ExtraVars = b.collectExtraVarJS(fn.Body)
		node.ExtraVars = append(node.ExtraVars, b.collectResourceJS(fn.Body)...)
	}

	// Reset element tag counters for fresh walk
	for k := range b.elementCounts {
		delete(b.elementCounts, k)
	}

	// Save parent's pending handlers so child component processing doesn't overwrite them
	savedHandlers := b.pendingHandlers
	b.pendingHandlers = nil

	// Save parent's pending attr bindings for the same reason
	savedAttrs := b.pendingAttrs
	b.pendingAttrs = nil

	// Save parent's pending ref bindings for the same reason
	savedRefs := b.pendingRefs
	b.pendingRefs = nil

	// Save parent's pending props registrations for the same reason
	savedPropsRegs := b.pendingPropsRegs
	b.pendingPropsRegs = nil

	// Set local function body context for handler resolution
	savedLocalFnBody := b.localFnBody
	b.localFnBody = fn.Body

	// Track which local variables are useRef {current:...} objects so a
	// `ref={myRef}` binding assigns `.current` instead of clobbering the object.
	savedRefObjectVars := b.refObjectVars
	b.refObjectVars = collectRefObjectVars(fn.Body)

	// Track which local names hold functions (named function declarations or
	// variables initialized to an arrow/function). `ref={setRef}` where setRef
	// is such a function is a callback ref - kbindRef(id, setRef) invokes it
	// with the node - NOT an assignment target (which would overwrite the
	// function variable with the element, breaking onMount readers).
	savedRefCallbackVars := b.refCallbackVars
	b.refCallbackVars = collectFunctionRefVars(fn.Body)

	// Named memos (const doubled = createMemo(() => ...)) are treated as local
	// reactive getters: mapping the getter to its arrow body expression lets
	// JSX text reads like {doubled()} resolve to a reactive text binding with a
	// computed SSR initial value (b.sigMap() is consulted for both reactivity
	// detection and initial evaluation).
	if tier == TierClient && b.localSignals != nil {
		for name, arrow := range b.collectNamedMemos(fn.Body) {
			if bodyExpr := arrowBodyExpr(arrow); bodyExpr != nil {
				b.localSignals[name] = bodyExpr
			}
		}
		// Resource getters (const [user, actions] = createResource(...)) are
		// registered as reactive getters with a sentinel initial expression so
		// reads like {user.loading} / {user.state} resolve their correct initial
		// value during SSR while still emitting live bindings at hydration.
		for _, name := range b.collectResourceNames(fn.Body) {
			b.localSignals[name] = resourceSentinel
		}
	}

	// Set component-local props: resolved call-site attribute values so that
	// prop reads (props.X and bare param identifiers) resolve during the walk.
	// buildComponentSlot populates b.localProps before invoking this method.
	savedLocalProps := b.localProps
	savedParamNames := b.localParamNames
	// Record the component's parameter names (including destructured members)
	// and fold any absent one to "undefined" (or its default), so an omitted
	// prop like `className` in `cn("base", className)` resolves as a known
	// undefined rather than leaking the identifier.
	b.localParamNames = make(map[string]bool)
	for _, name := range extractParamNames(fn) {
		b.localParamNames[name] = true
		if b.localProps == nil {
			b.localProps = make(map[string]string)
		}
		if _, exists := b.localProps[name]; !exists {
			b.localProps[name] = b.paramDefaultValue(fn, name)
		}
	}
	// The entry component is built with an empty parent ID and has no call
	// site; any non-empty parent ID means we are a child with call-site props.
	// The distinction matters below: root locals are folded for build-time
	// evaluation but must NOT force the root out of its static tier.
	isPageRoot := parentID == ""
	savedLocalFuncProps := b.localFuncProps
	if b.localFuncProps != nil {
		node.FuncProps = b.localFuncProps
	}

	// Fold module-level constants (const X = <literal>) into the component's
	// local-prop scope so JSX text referencing them (e.g. {hexNum}) resolves to
	// their value at build time instead of leaking the identifier name. Local
	// props/vars take precedence, so module consts are only added as defaults.
	if len(b.moduleConsts) > 0 {
		if b.localProps == nil {
			b.localProps = make(map[string]string)
		}
		for k, v := range b.moduleConsts {
			if _, exists := b.localProps[k]; !exists {
				b.localProps[k] = v
			}
		}
	}

	// Seed `createContext(default)` bindings under a reserved key so
	// `Ctx.useContext()` folds to the default at build time (no Provider exists
	// during SSG). Without this the local `const v = Ctx.useContext()` cannot be
	// folded and hydration would reference an undefined context object.
	if len(b.contextDefaults) > 0 {
		if b.localProps == nil {
			b.localProps = make(map[string]string)
		}
		for k, v := range b.contextDefaults {
			key := contextMarkerPrefix + k
			if _, exists := b.localProps[key]; !exists {
				b.localProps[key] = v
			}
		}
	}

	// Resolve component local variables (var lang = props.lang || "", etc.) to
	// build-time constants. Folded into localProps for SSR initial evaluation
	// and emitted as var declarations in the hydration bundle so bindings that
	// reference them don't throw ReferenceError. Locals that alias a function
	// prop (`var onNavigate = props.onNavigate`) cannot be const-folded (the
	// value is a live function reference, not a literal): they're emitted as a
	// runtime read of the __krate_props registry instead.
	var funcPropAliases []string
	// A page root starts with a nil localProps map, but its local variables
	// still need folding so slot initials resolve to their values rather than
	// leaking the identifier name into output (e.g. a root `const title = "..."`
	// rendered inside <Head><title>{title}</title></Head>).
	if b.localProps == nil {
		b.localProps = make(map[string]string)
	}
	// useId() resolves to a per-component-instance build-time literal. The
	// instance ID is stable across SSR and hydration, so `<Label htmlFor={id}>`
	// / `<Input id={id}>` agree without any runtime state (and without the
	// SSR/hydration divergence React's runtime counter risks). Multiple calls
	// within one instance get distinct suffixes.
	useIdBase := "krate-" + strings.ReplaceAll(string(id), ".", "-")
	useIdN := 0
	nextUseId := func() string {
		n := useIdN
		useIdN++
		if n == 0 {
			return useIdBase
		}
		return useIdBase + "-" + itoa(n)
	}
	locals, useIdLocals, localInits, localOrder, localLeaked := collectLocalVars(fn.Body, b.sigMap(), b.localProps, nextUseId)
	if b.localFuncProps != nil && tier == TierClient {
		// Remove function-prop aliases from the fold set so they aren't
		// serialized as the function's name string, and re-declare them as
		// live reads below.
		for name := range locals {
			if prop := funcPropAliasOf(fn.Body, name, b.localFuncProps); prop != "" {
				funcPropAliases = append(funcPropAliases, name+"=props."+prop)
				delete(locals, name)
			}
		}
	}
	// useId locals are referenced by SSR-resolved attributes and any client
	// bindings, so emit them as extra vars for every client component -
	// including the page root (whose ordinary locals are intentionally left
	// unemitted to allow static promotion).
	if tier == TierClient {
		declared := declaredLocalNames(node, fn)
		for _, name := range sortedKeys(useIdLocals) {
			if declared[name] {
				continue
			}
			node.ExtraVars = append(node.ExtraVars, "var "+name+"="+jsLiteralFor(useIdLocals[name]))
		}
	}
	if len(locals) > 0 {
		for k, v := range locals {
			// Leaked locals have no known value; leaving them OUT of localProps
			// makes guards/tests referencing them fold as unknown (so a
			// ConditionalSlot is emitted) instead of treating "" as a
			// definitive falsy value and dropping the branch.
			if localLeaked[k] {
				continue
			}
			if _, exists := b.localProps[k]; !exists {
				b.localProps[k] = v
			}
		}
		// Emit ExtraVars for children only. collectExtraVarJS already excludes
		// props-derived locals for every component specifically so a purely
		// presentational root can auto-promote to TierStatic; re-adding them
		// here for the root would block that promotion and flip the page to
		// client (losing build-time evaluability).
		if tier == TierClient && !isPageRoot {
			declared := declaredLocalNames(node, fn)
			// Emit locals in SOURCE (declaration) order, not alphabetical order:
			// a local may reference an earlier local (e.g. `basePath` derives
			// from `themeOptions`), and alphabetical emission would assign the
			// dependent before its dependency, yielding `undefined`.
			var preLocals []string
			for _, name := range localOrder {
				if _, ok := locals[name]; !ok {
					continue
				}
				if declared[name] {
					continue
				}
				// A local whose initializer reads props OR another local must be
				// re-evaluated at runtime (`var items=props.items||[]`) so its
				// dependencies have already been assigned. Folding is only safe
				// for build-time constants; emitting a folded array/object
				// literal there previously quoted the JS source into a string.
				init := localInits[name]
				decl := ""
				// Emit as a runtime expression when the value depends on props,
				// on another local, is a reference to a function/object binding,
				// or could not be folded at all (`locals[name] == ""` marks a
				// leaked/unresolvable value). Reference initializers must be
				// rendered from the AST (as `SomeIdent`) rather than the folded
				// string, which cannot be told apart from a string constant.
				if init != nil && (referencesProps(init) || referencesAnyLocal(init, b.sigMap(), locals) || isReferenceInit(init) || locals[name] == "") {
					if js := generateExprJS(init, b.sigMap()); js != "" {
						decl = "var " + name + "=" + js
					}
				}
				if decl == "" {
					decl = "var " + name + "=" + jsLiteralFor(locals[name])
				}
				// Signal-referencing locals must be declared after the signal
				// declarations; everything else goes BEFORE collectExtraVarJS's
				// pre-vars so a local that depends on a props-derived local
				// (emitted here) is ordered correctly.
				if init != nil && b.referencesSignal(init) {
					node.ExtraVars = append(node.ExtraVars, decl)
				} else {
					preLocals = append(preLocals, decl)
				}
			}
			if len(preLocals) > 0 {
				node.PreSignalVars = append(preLocals, node.PreSignalVars...)
			}
		}
	}
	if len(funcPropAliases) > 0 && tier == TierClient {
		// The alias reads props.<funcProp>, so the component must bind props to
		// the registry entry registered by its parent (emitted ahead of these in
		// newhydrate). Declared AFTER the registry read to preserve correctness.
		node.FuncPropAliases = append(node.FuncPropAliases, funcPropAliases...)
	}

	// Find return statement and build children
	// Handlers are accumulated in b.pendingHandlers during this walk.
	returnStmt := findReturnStmt(fn.Body)
	if returnStmt != nil && returnStmt.Value != nil {
		// CSS components render through the normal slot pipeline: the analyzer
		// drives trigger/panel rewrites and the controller inputs are injected
		// into the component's scope anchor. Set the component-local analyzer +
		// per-instance token for the duration of the walk.
		analyzer := b.cssAnalyzer(fn)
		if analyzer != nil && analyzer.HaveAny() && !analyzer.OK() {
			// Hard error: no fallback. The declaration is not compiled to
			// signals either, so a broken component must fail the build.
			for _, reason := range analyzer.Errors() {
				b.collectCSSErr(fn.Name, returnStmt.Position.Line, reason)
			}
		}

		savedChoice, savedToken := b.cssSignals, b.cssToken
		retExpr := returnStmt.Value
		if analyzer != nil && analyzer.OK() {
			b.cssSignals = analyzer
			b.cssToken = csssignals.SanitizeToken(string(id))
			for _, s := range analyzer.Scopes() {
				b.collectCSSScope(s)
			}
			for _, c := range analyzer.Conditions() {
				b.collectCSSCondition(c)
			}
			if analyzer.NeedsARIA() {
				b.cssNeedsARIA = true
			}
			// Inject the scope class + controller inputs into the component's
			// root (or a display:contents wrapper when there is no single
			// intrinsic root). Done on a copy so multiple instances don't
			// accumulate classes/inputs in the shared AST.
			retExpr = b.prepareCSSRoot(retExpr, analyzer.Scopes(), b.cssToken)
		}

		node.Children = b.buildSlotNodes(retExpr, string(id))

		b.cssSignals, b.cssToken = savedChoice, savedToken
	}

	// Restore context
	b.localFnBody = savedLocalFnBody
	b.localSignals = savedLocalSignals
	b.localProps = savedLocalProps
	b.localParamNames = savedParamNames
	b.localFuncProps = savedLocalFuncProps
	b.refObjectVars = savedRefObjectVars
	b.refCallbackVars = savedRefCallbackVars

	// Read accumulated handlers from slot building walk
	if tier == TierClient {
		node.Handlers = b.pendingHandlers
		node.AttrBindings = b.pendingAttrs
		node.RefBindings = b.pendingRefs
		// Props registrations for child components must run in this
		// component's scope (their values reference this component's
		// signals), and before child component IIFEs read the registry.
		if len(b.pendingPropsRegs) > 0 {
			node.ExtraVars = append(node.ExtraVars, b.pendingPropsRegs...)
		}
		// Scan handlers/effects/memos (and dynamic list slot expressions, which
		// may be nested in call-site children of signal-less wrappers) for local
		// and module-level function references, and include those function
		// declarations in extra vars so the hydration code has access to them.
		localFnDecls := b.collectReferencedFunctions(node, fn.Body)
		for _, fnDecl := range localFnDecls {
			fnJS := renderStmtJS(fnDecl, b.sigMap())
			if fnJS != "" {
				node.ExtraVars = append(node.ExtraVars, fnJS)
			}
		}

		// Auto-promote to TierStatic: if the component has no signals,
		// effects, memos, handlers, attr bindings, or extra vars, it is
		// purely static (props-in, JSX-out) and can be SSR-evaluated at
		// build time with zero client JS. This covers layout components
		// ({children} passthrough), presentational components, and any
		// function that only composes JSX from its props.
		if len(node.Signals) == 0 && len(node.Effects) == 0 &&
			len(node.Memos) == 0 && len(node.ExtraVars) == 0 &&
			len(node.PreSignalVars) == 0 &&
			len(node.Handlers) == 0 && len(node.AttrBindings) == 0 &&
			len(node.RefBindings) == 0 {
			node.Tier = TierStatic
		}
	}
	b.pendingHandlers = savedHandlers
	b.pendingAttrs = savedAttrs
	b.pendingRefs = savedRefs
	b.pendingPropsRegs = savedPropsRegs

	return node
}

// collectReferencedFunctions returns the function declarations a client
// component must have in scope at hydration time: local helper functions plus
// any module-level helper functions they (or the handlers/effects/memos)
// reference. Resolution is transitive so a local handler like
// `applyOperator() { ... compute(...) ... }` pulls in the module-level
// `compute` as well.
func (b *builder) collectReferencedFunctions(node *ComponentNode, body []ast.Stmt) []*ast.FnDecl {
	candidates := make(map[string]*ast.FnDecl)
	localFns := make(map[string]bool)
	for _, stmt := range body {
		if fn, ok := stmt.(*ast.FnDecl); ok {
			candidates[fn.Name] = fn
			localFns[fn.Name] = true
		}
	}
	// Module-level functions (non-component helpers declared at the top level
	// of a module) are also in scope and may be called by the local handlers.
	// Component functions are excluded: their names appear in slot-ID string
	// literals (e.g. "a.Button.Button_c0") which would otherwise false-match.
	for name, fn := range b.functions {
		if _, isLocal := candidates[name]; isLocal {
			continue
		}
		if b.ann.UsedComponents[name] {
			continue
		}
		candidates[name] = fn
	}
	if len(candidates) == 0 {
		return nil
	}

	referenced := make(map[string]bool)
	var queue []string
	scan := func(code string) {
		for name := range candidates {
			if !referenced[name] && codeContainsIdent(code, name) {
				referenced[name] = true
				queue = append(queue, name)
			}
		}
	}

	// Always emit the component's own local function declarations. Parts of the
	// body are emitted verbatim (e.g. a `for` loop that builds an element array
	// and references a local handler by name), which the reference scan below
	// cannot see; without this a local handler like `handleInputEvent` would be
	// undefined at hydration time. DCE's aggressive mode opts out of this and
	// relies solely on the reference scan.
	if !b.aggressiveDCE {
		for name := range localFns {
			if !referenced[name] {
				referenced[name] = true
				queue = append(queue, name)
			}
		}
	}

	for _, h := range node.Handlers {
		scan(h.Body)
	}
	for _, eff := range node.Effects {
		scan(eff)
	}
	for _, memo := range node.Memos {
		scan(memo)
	}
	// Attribute bindings (kbindAttr value expressions such as
	// `href={'?page=' + prevPage()}`) can call local helper functions that
	// must also be in scope at hydration time.
	for _, a := range node.AttrBindings {
		scan(a.ExprSource)
	}
	// Signal factory calls (e.g. createReducer's reducer function) may reference
	// local/module functions that must be hoisted into the hydration scope.
	for _, s := range node.Signals {
		if s.FactoryJS != "" {
			scan(s.FactoryJS)
		}
	}
	// Props registrations (__krate_props["<id>"]={onClick:clear,...}) reference
	// local functions from the parent scope; hoist them so the child's forwarded
	// handlers resolve at runtime.
	for _, ev := range node.ExtraVars {
		scan(ev)
	}
	// Dynamic list slot expressions (e.g. {items().map(x => <Item onSelect={fn}/>)})
	// reference local functions too; without hoisting them the runtime re-render
	// of the list would throw ReferenceError. The ListSlot may be nested inside
	// a signal-less child wrapper's call-site slots (e.g. <ToastViewport>{list}</ToastViewport>),
	// so walk the built slot tree transitively.
	var scanSlots func(slots []SlotNode)
	scanSlots = func(slots []SlotNode) {
		for _, child := range slots {
			switch s := child.(type) {
			case *ListSlot:
				scan(s.ExprSource)
			case *ComponentSlot:
				if s.Component != nil {
					scanSlots(s.Component.Children)
					scanSlots(s.Component.CallSiteSlots)
				}
			case *ConditionalSlot:
				scanSlots(s.Consequent)
				scanSlots(s.Alternate)
			}
		}
	}
	scanSlots(node.Children)
	scanSlots(node.CallSiteSlots)

	// Transitive closure: the bodies of referenced functions may reference
	// other local or module-level functions.
	for i := 0; i < len(queue); i++ {
		fn := candidates[queue[i]]
		if fn == nil {
			continue
		}
		scan(renderStmtJS(fn, b.sigMap()))
	}

	names := make([]string, 0, len(referenced))
	for name := range referenced {
		if _, ok := candidates[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := make([]*ast.FnDecl, 0, len(names))
	for _, name := range names {
		result = append(result, candidates[name])
	}
	return result
}

// codeContainsIdent reports whether code contains name as a standalone
// identifier (not a substring of a longer identifier). Used to detect local
// function references in generated JS like `var handler=handleClickOutside`.
func codeContainsIdent(code, name string) bool {
	if name == "" || code == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(code[start:], name)
		if idx < 0 {
			return false
		}
		i := start + idx
		beforeOK := i == 0 || !isIdentChar(code[i-1])
		afterOK := i+len(name) >= len(code) || !isIdentChar(code[i+len(name)])
		if beforeOK && afterOK {
			return true
		}
		start = i + len(name)
	}
}

// collectHandlerLocalFunctions scans handler bodies for local function references
// and returns the matching function declarations from the component body.
func collectHandlerLocalFunctions(handlers []HandlerDecl, body []ast.Stmt) []*ast.FnDecl {
	localFns := make(map[string]*ast.FnDecl)
	for _, stmt := range body {
		if fn, ok := stmt.(*ast.FnDecl); ok {
			localFns[fn.Name] = fn
		}
	}
	if len(localFns) == 0 {
		return nil
	}

	// Scan each handler body for local function references (calls AND bare
	// identifier references like onClick={handleClick}).
	referenced := make(map[string]bool)
	for _, h := range handlers {
		for name := range localFns {
			if codeContainsIdent(h.Body, name) {
				referenced[name] = true
			}
		}
	}

	var result []*ast.FnDecl
	for name := range referenced {
		result = append(result, localFns[name])
	}
	return result
}

// isIdentChar returns true if c is a valid JS identifier character.
func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$'
}

// buildSlotNodes - dispatch by expression type

func (b *builder) buildSlotNodes(expr ast.Expr, parentID string) []SlotNode {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.JSXElement:
		return b.buildJSXSlot(e, parentID)
	case *ast.JSXFragment:
		return b.buildFragmentSlots(e, parentID)
	case *ast.ConditionalExpr:
		if nodes := b.tryResolveConditional(e, parentID); len(nodes) > 0 {
			return nodes
		}
		return []SlotNode{b.buildConditionalSlot(e, parentID)}
	case *ast.BinaryExpr:
		if e.Op == "&&" || e.Op == "||" || e.Op == "??" {
			return []SlotNode{b.buildExprSlot(e, parentID)}
		}
		if slot := b.buildStaticTextSlot(e, parentID); slot != nil {
			return []SlotNode{slot}
		}
		return nil
	case *ast.CallExpr:
		return b.buildCallExprSlots(e, parentID)
	case *ast.Literal, *ast.Identifier, *ast.MemberExpr, *ast.TemplateExpr, *ast.UnaryExpr, *ast.ArrayExpr:
		if slot := b.buildStaticTextSlot(e, parentID); slot != nil {
			return []SlotNode{slot}
		}
		return nil
	case *ast.ArrowFn:
		bodyExpr := arrowBodyExpr(e)
		if bodyExpr != nil {
			return b.buildSlotNodes(bodyExpr, parentID)
		}
		return nil
	case *ast.TypeAssertion:
		return b.buildSlotNodes(e.Expr, parentID)
	default:
		return nil
	}
}

// buildCallExprSlots handles call expressions - detects .map() calls and falls back to ExprSlot.
func (b *builder) buildCallExprSlots(call *ast.CallExpr, parentID string) []SlotNode {
	if isMapCall(call) {
		return []SlotNode{b.buildListSlot(call, parentID)}
	}
	if b.referencesSignal(call) {
		return []SlotNode{b.buildExprSlot(call, parentID)}
	}
	if slot := b.buildStaticTextSlot(call, parentID); slot != nil {
		return []SlotNode{slot}
	}
	return nil
}

// buildJSXSlot - lowercase HTML or uppercase component

func (b *builder) buildJSXSlot(el *ast.JSXElement, parentID string) []SlotNode {
	// CSS interception. When the component currently being walked declares a
	// zero-JS CSS primitive, a trigger (a setter handler) becomes a `<label for>`
	// and a panel (a scope condition in showIf) becomes a wrapper-toggled
	// element. The rewrite returns an expression (a label element or a
	// display:contents wrapper), which is then built by the normal slot pipeline
	// so children nested components, lists, conditionals render as usual.
	// This runs before the showIf handling below, which the panel rewrite removes.
	if b.cssSignals != nil && b.cssSignals.OK() && isIntrinsicTag(el) {
		if rewritten := b.rewriteCSSSignalElement(el); rewritten != nil {
			return b.buildSlotNodes(rewritten, parentID)
		}
	}

	// `showIf`/`visibleIf` sugar: {test && <el/>}. Strip the attribute and route
	// the guard through the same static-fold / ConditionalSlot machinery used by
	// explicit `{test && <el/>}` expressions so reactivity inside either branch
	// (handlers, bindings, nested guards) keeps working.
	if test, stripped, ok := ShowIfExpr(el); ok {
		bin := &ast.BinaryExpr{Left: test, Op: "&&", Right: stripped}
		if nodes, resolved := b.tryResolveGuard(bin, parentID); resolved {
			return nodes
		}
		return []SlotNode{b.buildGuardSlot(bin, parentID)}
	}

	name := el.Opening.Name

	// Special components - route to metadata output
	switch name {
	case "Head", "head", "Script", "script", "Style", "style":
		return []SlotNode{b.buildMetaSlot(el, parentID)}
	case "Link":
		b.hasLinks = true
		return b.buildLinkSlots(el, parentID)
	case "SyntaxHighlight":
		return b.buildSyntaxHighlightSlots(el, parentID)
	case "Icon", "Image":
		return []SlotNode{&StaticHTML{HTML: ""}}
	case "Suspense":
		return []SlotNode{b.buildSuspenseSlot(el, parentID)}
	}

	// A tag name bound to a local variable (e.g. `const Comp = asChild ? Slot :
	// "button"` then `<Comp/>`) resolves to its concrete tag when the binding
	// folds at build time. This is the shadcn/ui `asChild` pattern.
	if alias := b.resolveTagAlias(name); alias != "" && alias != name {
		cloned := *el
		opening := *el.Opening
		opening.Name = alias
		cloned.Opening = &opening
		return b.buildSlotNodes(&cloned, parentID)
	}

	// Uppercase = component
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		if slots := b.buildComponentSlot(el, parentID); len(slots) > 0 {
			return slots
		}
		// Unknown component - render as empty span
		return []SlotNode{&StaticHTML{HTML: "<span></span>"}}
	}

	// Lowercase = HTML element
	return b.buildStaticElementSlots(el, parentID)
}

// buildSuspenseSlot builds a <Suspense fallback={...}><Primary/></Suspense>
// boundary. The fallback is always baked into the static shell inside suspense
// markers (emitter emits them). Whether the primary is ALSO baked (ModeStatic)
// or deferred to a request-time region render (ModeRegion) is decided by the
// boundary's contents:
//   - TierRuntime primaries (or runtime/server children) are deferred: they
//     carry fresh props/params per request and must render in the sidecar.
//   - Pure static primaries are fully resolved at build time (ModeStatic).
//   - A missing primary falls back to the fallback alone (ModeDefault).
func (b *builder) buildSuspenseSlot(el *ast.JSXElement, parentID string) *SuspenseSlot {
	_, fallbackExpr := findSuspenseFallback(el)
	id := joinSlotID(parentID, "suspense")

	slotID := b.assignSlotID(id)
	streamID := b.nextSuspenseStreamID(string(slotID))

	// Bake the fallback content. If absent, empty boundaries render nothing.
	var fallback []SlotNode
	if fallbackExpr != nil {
		fallback = b.buildSlotNodes(fallbackExpr, string(id))
	} else {
		fallback = []SlotNode{}
	}

	s := &SuspenseSlot{
		ID:       id,
		Fallback: fallback,
		StreamID: streamID,
		Mode:     SuspenseModeDefault,
	}

	// Resolve the primary region: a possible top-level runtime component plus
	// the mode and the buildable static content to bake when static.
	primary, mode, resolved := b.resolveSuspensePrimary(el.Children, string(id))
	s.Primary = primary
	s.Mode = mode
	s.Resolved = resolved

	return s
}

// nextSuspenseStreamID returns a stable, unique stream ID for a suspense
// boundary. It derives from the compact slot ID (which the emitter marks with)
// and a per-component counter so repeated builds of the same boundary reuse the
// same ID (deterministic output) while sibling boundaries stay distinct.
func (b *builder) nextSuspenseStreamID(slotID string) string {
	b.suspenseCount++
	return fmt.Sprintf("%s-%d", slotID, b.suspenseCount)
}

// resolveSuspensePrimary walks the suspense children and decides how to emit the
// boundary's primary region:
//   - If a direct TierRuntime child component is found, it becomes the Primary
//
// (region render) - the streaming boundary's whole point.
//   - Otherwise, if any direct or deeply-nested runtime component appears, the
//     boundary must still be a region (we defer those sub-regions), so choose
//     ModeRegion with no single top-level Primary.
//   - Otherwise everything is static: the buildable children are returned as
//     Resolved content (ModeStatic).
//
// The third return value holds the slot nodes to bake into the shell when the
// mode is static (the resolved primary content), or nil for region modes.
func (b *builder) resolveSuspensePrimary(children []ast.JSXChild, parentID string) (*ComponentNode, SuspenseMode, []SlotNode) {
	if len(children) == 0 {
		return nil, SuspenseModeDefault, nil
	}

	var directStatic []ast.JSXChild

	for _, c := range children {
		elc, ok := c.(*ast.JSXElementChild)
		if !ok {
			directStatic = append(directStatic, c)
			continue
		}
		name := elc.Element.Opening.Name
		fn := b.functions[name]
		if fn == nil {
			directStatic = append(directStatic, c)
			continue
		}
		if b.ann.ComponentTiers[name] == TierRuntime {
			childID := joinSlotID(parentID, name)
			attrs := extractPropsAST(elc.Element)
			props := make(map[string]any, len(attrs))
			for pn, pe := range attrs {
				props[pn] = evalConstWithSignals(pe, b.sigMap(), b.localProps)
			}
			node := &ComponentNode{
				ID:           childID,
				Name:         name,
				Tier:         TierRuntime,
				Props:        attrs,
				RuntimeProps: props,
				SourceFile:   b.ann.ComponentSources[name],
				Line:         fn.Position.Line,
			}
			// Build the remaining (static) children as resolved fallback-in-place
			// content so a shell without the runtime region still shows them.
			resolved := b.buildSuspenseResolved(directStatic, parentID)
			return node, SuspenseModeRegion, resolved
		}
		directStatic = append(directStatic, c)
	}

	// No direct runtime primary. If a runtime component is nested deeper inside
	// the boundary's static direct children, bake the resolved content anyway:
	// the static wrappers become part of the shell and each nested runtime
	// component is emitted as its own standalone region (region-<slotID>), which
	// the sidecar renders independently. A boundary cannot be resolved as an
	// anonymous whole (it has no component/bundle identity), so deferring the
	// whole boundary would leave nothing renderable at serve time.
	if b.childrenContainRuntime(directStatic) {
		resolved := b.buildSuspenseResolved(directStatic, parentID)
		return nil, SuspenseModeStatic, resolved
	}

	// Fully static boundary - bake the resolved primary content.
	resolved := b.buildSuspenseResolved(directStatic, parentID)
	return nil, SuspenseModeStatic, resolved
}

// buildSuspenseResolved builds a set of suspense children as static slot nodes
// to bake into the shell as the boundary's resolved (primary) content.
func (b *builder) buildSuspenseResolved(children []ast.JSXChild, parentID string) []SlotNode {
	var out []SlotNode
	for _, c := range children {
		switch ch := c.(type) {
		case *ast.JSXElementChild:
			out = append(out, b.buildSlotNodes(ch.Element, parentID)...)
		case *ast.JSXFragmentChild:
			out = append(out, b.buildFragmentSlots(ch.Fragment, parentID)...)
		case *ast.JSXExprContainer:
			out = append(out, b.buildExprContainerChildren(ch, parentID)...)
		case *ast.JSXText:
			if ch.Value != "" {
				out = append(out, &StaticHTML{HTML: ch.Value})
			}
		}
	}
	return out
}

// childrenContainRuntime reports whether any nested component within the given
// suspense children is a TierRuntime component (direct or deeply nested).
func (b *builder) childrenContainRuntime(children []ast.JSXChild) bool {
	for _, c := range children {
		switch ch := c.(type) {
		case *ast.JSXElementChild:
			if b.elementContainRuntime(ch.Element) {
				return true
			}
		case *ast.JSXFragmentChild:
			if b.fragmentContainRuntime(ch.Fragment) {
				return true
			}
		case *ast.JSXExprContainer:
			if b.exprContainRuntime(ch.Expression) {
				return true
			}
		}
	}
	return false
}

func (b *builder) elementContainRuntime(el *ast.JSXElement) bool {
	name := el.Opening.Name
	if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
		if fn := b.ann.Functions[name]; fn != nil {
			if b.ann.ComponentTiers[name] == TierRuntime {
				return true
			}
		}
	}
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			if b.elementContainRuntime(c.Element) {
				return true
			}
		case *ast.JSXFragmentChild:
			if b.fragmentContainRuntime(c.Fragment) {
				return true
			}
		case *ast.JSXExprContainer:
			if b.exprContainRuntime(c.Expression) {
				return true
			}
		}
	}
	return false
}

func (b *builder) fragmentContainRuntime(frag *ast.JSXFragment) bool {
	for _, child := range frag.Children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			if b.elementContainRuntime(c.Element) {
				return true
			}
		case *ast.JSXFragmentChild:
			if b.fragmentContainRuntime(c.Fragment) {
				return true
			}
		case *ast.JSXExprContainer:
			if b.exprContainRuntime(c.Expression) {
				return true
			}
		}
	}
	return false
}

func (b *builder) exprContainRuntime(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.JSXElement:
		return b.elementContainRuntime(e)
	case *ast.JSXFragment:
		return b.fragmentContainRuntime(e)
	case *ast.ConditionalExpr:
		return b.exprContainRuntime(e.Consequent) || b.exprContainRuntime(e.Alternate)
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			if b.exprContainRuntime(el) {
				return true
			}
		}
	}
	return false
}

// findSuspenseFallback extracts the JSX expression bound to the `fallback`
// prop of a <Suspense> element. Returns the JSXChild list if the fallback value
// is a JSX element/fragment, else nil. (Children of the expression container
// that build through buildSlotNodes handle nesting.)
func findSuspenseFallback(el *ast.JSXElement) (bool, ast.Expr) {
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Name != "fallback" || attr.Value == nil {
			continue
		}
		switch v := attr.Value.(type) {
		case *ast.JSXElement:
			return true, v
		case *ast.JSXFragment:
			return true, v
		default:
			return false, nil
		}
	}
	return false, nil
}

// buildLinkSlots - <Link> -> <a> with SPA navigation
// buildLinkSlots compiles <Link> into a real <a> element wired for client-side
// navigation (data-krate-link) with Next.js-style props:
// - prefetch (default true) -> data-prefetch (hover + viewport prefetch)
// - replace (default false) -> data-krate-replace (history.replaceState)
// - scroll (default true) -> data-krate-scroll="false" disables scroll-to-top
//   - target/rel/className/title/aria-label/id forwarded as anchor attributes
//
// External links (http(s), mailto, tel, hash, _blank, download) are emitted as
// plain anchors (data-krate-external) so the router never intercepts them.
// Children and dynamic slots are delegated to buildStaticElementSlots via a
// synthesized <a> element.
func (b *builder) buildLinkSlots(el *ast.JSXElement, parentID string) []SlotNode {
	href := ""
	prefetch := true // prefetch local links by default, like Next.js
	replace := false
	scroll := true
	className := ""
	target := ""
	rel := ""
	title := ""
	ariaLabel := ""
	id := ""
	external := false
	var forwarded []*ast.JSXAttr

	for _, attr := range el.Opening.Attributes {
		if attr.Spread {
			forwarded = append(forwarded, attr)
			continue
		}
		if attr.Value == nil {
			switch attr.Name {
			case "prefetch":
				prefetch = true
			case "replace":
				replace = true
			case "external":
				external = true
			default:
				forwarded = append(forwarded, attr)
			}
			continue
		}
		val := b.evalAttrValue(attr.Value)
		switch attr.Name {
		case "href":
			href = val
		case "prefetch":
			prefetch = val == "true" || val == "1"
		case "replace":
			replace = val == "true"
		case "scroll":
			scroll = val != "false"
		case "external":
			external = val == "true"
		case "className":
			className = val
		case "target":
			target = val
		case "rel":
			rel = val
		case "title":
			title = val
		case "aria-label", "ariaLabel":
			ariaLabel = val
		case "id":
			id = val
		case "download":
			forwarded = append(forwarded, attr)
			external = true
		default:
			forwarded = append(forwarded, attr)
		}
	}

	local := !external && isLocalHref(href) && target != "_blank"

	var aAttrs []*ast.JSXAttr
	aAttrs = append(aAttrs, &ast.JSXAttr{Name: "href", Value: &ast.Literal{Kind: ast.StringLit, Value: href}})
	if local {
		aAttrs = append(aAttrs, &ast.JSXAttr{Name: "data-krate-link", Value: nil})
		if prefetch {
			aAttrs = append(aAttrs, &ast.JSXAttr{Name: "data-prefetch", Value: nil})
		}
		if replace {
			aAttrs = append(aAttrs, &ast.JSXAttr{Name: "data-krate-replace", Value: nil})
		}
		if !scroll {
			aAttrs = append(aAttrs, &ast.JSXAttr{Name: "data-krate-scroll", Value: &ast.Literal{Kind: ast.StringLit, Value: "false"}})
		}
	} else {
		aAttrs = append(aAttrs, &ast.JSXAttr{Name: "data-krate-external", Value: nil})
	}
	if className != "" {
		aAttrs = append(aAttrs, &ast.JSXAttr{Name: "class", Value: &ast.Literal{Kind: ast.StringLit, Value: className}})
	}
	if target != "" {
		aAttrs = append(aAttrs, &ast.JSXAttr{Name: "target", Value: &ast.Literal{Kind: ast.StringLit, Value: target}})
	}
	if target == "_blank" && rel == "" {
		rel = "noopener noreferrer"
	}
	if rel != "" {
		aAttrs = append(aAttrs, &ast.JSXAttr{Name: "rel", Value: &ast.Literal{Kind: ast.StringLit, Value: rel}})
	}
	if title != "" {
		aAttrs = append(aAttrs, &ast.JSXAttr{Name: "title", Value: &ast.Literal{Kind: ast.StringLit, Value: title}})
	}
	if ariaLabel != "" {
		aAttrs = append(aAttrs, &ast.JSXAttr{Name: "aria-label", Value: &ast.Literal{Kind: ast.StringLit, Value: ariaLabel}})
	}
	if id != "" {
		aAttrs = append(aAttrs, &ast.JSXAttr{Name: "id", Value: &ast.Literal{Kind: ast.StringLit, Value: id}})
	}
	aAttrs = append(aAttrs, forwarded...)

	a := &ast.JSXElement{
		Opening:  &ast.JSXOpening{Name: "a", Attributes: aAttrs, SelfClosing: el.Opening.SelfClosing},
		Children: el.Children,
		Closing:  &ast.JSXClosing{Name: "a"},
	}
	return b.buildStaticElementSlots(a, parentID)
}

// isLocalHref reports whether an href should be treated as an internal SPA link.
func isLocalHref(href string) bool {
	if href == "" {
		return true
	}
	if strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") ||
		strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
		return false
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "//") {
		return false
	}
	return true
}

// buildSyntaxHighlightSlots - <SyntaxHighlight> -> chroma-highlighted HTML

// isChildrenPlaceholderExpr reports whether expr is the {children} /
// {props.children} passthrough placeholder.
func isChildrenPlaceholderExpr(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Identifier:
		return t.Name == "children"
	case *ast.MemberExpr:
		if id, ok := t.Object.(*ast.Identifier); ok && id.Name == "props" {
			if pid, ok := t.Property.(*ast.Identifier); ok && pid.Name == "children" {
				return true
			}
		}
	}
	return false
}

// resolveCallSiteChildrenText extracts compile-time text from the current
// component's call-site children (e.g. the template literal passed to
// <Code>{`...code...`}</Code>). Returns ok=false when there are no call-site
// children or any part is dynamic (signal refs, unresolved identifiers, JSX
// elements) - the caller must then fall back to hydration-based rendering.
func (b *builder) resolveCallSiteChildrenText() (string, bool) {
	if len(b.callSiteChildren) == 0 {
		return "", false
	}
	var sb strings.Builder
	for _, child := range b.callSiteChildren {
		switch c := child.(type) {
		case *ast.JSXText:
			sb.WriteString(c.Value)
		case *ast.JSXExprContainer:
			if c.Expression == nil || b.referencesSignal(c.Expression) || !b.testFullyKnown(c.Expression) {
				return "", false
			}
			sb.WriteString(evalConstWithSignals(c.Expression, b.sigMap(), b.localProps))
		default:
			// JSX elements / fragments at the call site can't become plain text.
			return "", false
		}
	}
	return sb.String(), true
}

func (b *builder) buildSyntaxHighlightSlots(el *ast.JSXElement, parentID string) []SlotNode {
	lang := ""
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil {
			continue
		}
		if attr.Name == "lang" {
			lang = evalConstWithSignals(attr.Value, b.sigMap(), b.localProps)
		}
	}

	normalizedLang := syntaxhighlight.NormalizeLanguage(lang)

	// Try to extract children text content for compile-time highlighting.
	var code strings.Builder
	canHighlight := true
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			code.WriteString(c.Value)
		case *ast.JSXExprContainer:
			// {children} passthrough inside a component body: the actual code
			// text lives at the component's call site (e.g.
			// <Code lang="tsx">{`...code...`}</Code>). Resolve it from the
			// call-site children so compile-time chroma highlighting applies.
			if isChildrenPlaceholderExpr(c.Expression) && b.hasCallSiteChildren() {
				if text, ok := b.resolveCallSiteChildrenText(); ok {
					code.WriteString(text)
					continue
				}
			}
			val := evalConstWithSignals(c.Expression, b.sigMap(), b.localProps)
			if id, ok := c.Expression.(*ast.Identifier); ok && val == id.Name {
				// Unresolvable identifier - can't highlight at compile time.
				canHighlight = false
				break
			}
			if val != "" {
				code.WriteString(val)
			}
		}
	}

	if canHighlight {
		codeStr := strings.TrimSpace(code.String())
		var html strings.Builder
		if normalizedLang != "" {
			highlighted := syntaxhighlight.HighlightTheme(codeStr, normalizedLang, b.codeTheme)
			fmt.Fprintf(&html, "<pre class=\"chroma\"><code class=\"language-%s\">%s</code></pre>", lang, highlighted)
		} else {
			escaped := escape.HTML(codeStr)
			fmt.Fprintf(&html, "<pre><code>%s</code></pre>", escaped)
		}
		return []SlotNode{&StaticHTML{HTML: html.String()}}
	}

	// Children are dynamic (e.g. {children} in a component body). Emit the
	// <pre><code> wrapper as static HTML, build children as normal slot nodes,
	// then close the tags. Chroma highlighting is skipped - the server stub
	// applies plain escaping.
	openTag := "<pre class=\"chroma\"><code class=\"language-" + escape.HTML(lang) + "\">"
	closeTag := "</code></pre>"

	var result []SlotNode
	result = append(result, &StaticHTML{HTML: openTag})

	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			if c.Value != "" {
				result = append(result, &StaticHTML{HTML: escape.HTML(c.Value)})
			}
		case *ast.JSXExprContainer:
			childNodes := b.buildExprContainerChildren(c, parentID)
			result = append(result, childNodes...)
		}
	}

	result = append(result, &StaticHTML{HTML: closeTag})
	return result
}

// buildMetaSlot - Head/Script/Style content

func (b *builder) buildMetaSlot(el *ast.JSXElement, parentID string) *MetaSlot {
	name := strings.ToLower(el.Opening.Name)
	var children []SlotNode

	// <Script>/<style> and <Style> elements need their element tag preserved
	// (including a src attribute) so they land in the body as valid markup;
	// <Head> and <style> content is children-only because it is injected into
	// the document <head>. Self-closing <script src="..."/> must still emit
	// its tag.
	if name == "script" || name == "style" {
		if tag := b.buildMetaOpeningTag(el, name); tag != "" {
			children = append(children, &StaticHTML{HTML: tag})
		}
	}

	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			if c.Value != "" {
				children = append(children, &StaticHTML{HTML: c.Value})
			}
		case *ast.JSXExprContainer:
			// Meta content (Head/Script/Style) is intentionally raw HTML - a
			// template literal inside <Script>{...}</Script> must NOT be
			// HTML-escaped or the inline script would be corrupted.
			childNodes := b.buildMetaExprContainerChildren(c, parentID)
			children = append(children, childNodes...)
		case *ast.JSXElementChild:
			childNodes := b.buildSlotNodes(c.Element, parentID)
			children = append(children, childNodes...)
		case *ast.JSXFragmentChild:
			childNodes := b.buildFragmentSlots(c.Fragment, parentID)
			children = append(children, childNodes...)
		}
	}

	if name == "script" || name == "style" {
		children = append(children, &StaticHTML{HTML: "</" + name + ">"})
	}

	return &MetaSlot{
		ComponentName: el.Opening.Name,
		Children:      children,
	}
}

// buildMetaOpeningTag renders a <script>/<style> element's opening tag
// (including static attributes like src) for capture into meta output.
func (b *builder) buildMetaOpeningTag(el *ast.JSXElement, tag string) string {
	var buf strings.Builder
	buf.WriteByte('<')
	buf.WriteString(tag)
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil || attr.Name == "dangerouslySetInnerHTML" {
			continue
		}
		val := b.evalAttrValue(attr.Value)
		if isBooleanAttr(attr.Name) {
			if val == "true" {
				buf.WriteByte(' ')
				buf.WriteString(ast.HTMLAttrName(attr.Name))
			}
			continue
		}
		buf.WriteByte(' ')
		buf.WriteString(ast.HTMLAttrName(attr.Name))
		buf.WriteString(`="`)
		buf.WriteString(escape.HTML(val))
		buf.WriteByte('"')
	}
	buf.WriteByte('>')
	return buf.String()
}

// evalInnerHTMLAttr extracts the statically-resolvable __html value from a
// dangerouslySetInnerHTML={{__html: expr}} attribute. Returns ok=false when the
// attribute is absent or the value cannot be resolved at build time.
func (b *builder) evalInnerHTMLAttr(el *ast.JSXElement) (string, bool) {
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Name != "dangerouslySetInnerHTML" || attr.Value == nil {
			continue
		}
		obj, ok := attr.Value.(*ast.ObjectExpr)
		if !ok {
			return "", false
		}
		for _, prop := range obj.Properties {
			if prop.Spread || prop.Key != "__html" || prop.Value == nil {
				continue
			}
			return evalConstWithSignals(prop.Value, b.sigMap(), b.localProps), true
		}
	}
	return "", false
}

// buildComponentSlot - recursive component build

func (b *builder) buildComponentSlot(el *ast.JSXElement, parentID string) []SlotNode {
	childName := el.Opening.Name
	childFn := b.functions[childName]
	if childFn == nil {
		return nil
	}

	childID := joinSlotID(parentID, childName)

	// Runtime-tier components are NOT rendered at build time. The emitter
	// produces a krate-id placeholder + props entry so the serve-time renderer
	// evaluates them (via QuickJS/streaming) at request time with fresh values.
	if childTier := b.ann.ComponentTiers[childName]; childTier == TierRuntime {
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

	childSignals := b.collectSignalDecls(childFn)
	attrs := extractPropsAST(el)
	// CSS signal components must go through buildComponentNode so their
	// analyzer is installed and triggers/panels are rewritten. A panels-only
	// component has no signals/handlers and would otherwise take the SSREval
	// shortcut, which bypasses the interception and breaks the panels.
	if len(childSignals) == 0 && !b.cssSignalsOK(childFn) {
		if !b.componentNeedsClient(childFn, attrs) {
			// Inline signal-derived props: a signal-less component that renders
			// a parent's signal value (e.g. <Display value={display()}/>) must
			// stay reactive, so its return JSX is inlined with the props
			// substituted by their call-site expressions.
			if b.componentHasSignalProps(attrs) {
				return b.inlineSignalPropsComponent(el, childFn, childID, attrs)
			}
			// Pure prop-driven component - mark for SSR evaluation at emit time.
			// Extract prop bindings from the JSX call site so the emitter can
			// evaluate the component's return statement with those bindings.
			// Disambiguate multiple instances (_cN) so CSS-choice radio names are
			// unique across repeated use of the same component.
			instanceIdx := b.instanceCounts[childName]
			b.instanceCounts[childName]++
			childID = SlotID(string(childID) + "_c" + itoa(instanceIdx))

			paramNames := extractParamNames(childFn)
			bindings := b.buildPropBindings(paramNames, attrs, childFn)

			childNode := &ComponentNode{
				ID:               childID,
				Name:             childName,
				Tier:             TierStatic,
				Fn:               childFn,
				SSREvalBindings:  bindings,
				IsSSREval:        true,
				CallSiteChildren: el.Children,
				RestProps:        restProps(childFn, attrs),
				RestPropsName:    fnRestParamName(childFn),
			}

			// Recursively discover sub-components within call-site children.
			// Even though this component has no signals itself, its children
			// may contain interactive components that need their own hydration.
			savedCS := b.callSiteChildren
			b.callSiteChildren = el.Children
			// When the call-site children are fully static text, keep the raw
			// text so SSREval can chroma-highlight <SyntaxHighlight> content.
			if text, ok := b.resolveCallSiteChildrenText(); ok {
				childNode.CallSiteChildrenText = text
			}
			childNode.Children = b.buildCallSiteChildSlots(el.Children, string(childID))
			b.callSiteChildren = savedCS

			// Also discover sub-components inside the component's OWN return JSX.
			// A signal-less wrapper function (e.g. a demo wrapper) may return JSX
			// containing interactive client components; those must be emitted
			// through the tree path too or they'd be SSR'd as flat static HTML
			// with no hydration.
			// The child's own params are installed as local props and the
			// pending handler/attr/ref accumulators are saved and restored so
			// the child's internal bindings never leak into the parent's
			// signature (the slots here are only used to locate nested client
			// components, never emitted directly).
			if ret := findReturnStmt(childFn.Body); ret != nil && ret.Value != nil {
				savedHandlers, savedAttrs, savedRefs := b.pendingHandlers, b.pendingAttrs, b.pendingRefs
				b.pendingHandlers, b.pendingAttrs, b.pendingRefs = nil, nil, nil
				savedProps := b.localProps
				savedRest := b.restProps
				savedFnBody := b.localFnBody
				b.localProps = b.propBindingsToLocalProps(childFn, attrs, savedProps)
				b.restProps = b.restPropsFor(fnRestParamName(childFn), childFn, attrs)
				b.localFnBody = childFn.Body
				childNode.ReturnSlots = b.buildSlotNodes(ret.Value, string(childID))
				b.localProps = savedProps
				b.restProps = savedRest
				b.localFnBody = savedFnBody
				b.pendingHandlers, b.pendingAttrs, b.pendingRefs = savedHandlers, savedAttrs, savedRefs
			}

			return []SlotNode{&ComponentSlot{
				ID:        childID,
				Component: childNode,
			}}
		}
	}

	// Provide resolved call-site prop values to the child component walk so
	// props.X reads and bare param identifiers evaluate to their SSR initial.
	savedProps := b.localProps
	savedFuncProps := b.localFuncProps
	savedRestProps := b.restProps
	b.localProps = make(map[string]string, len(attrs))
	var funcProps map[string]bool
	for name, expr := range attrs {
		if b.isFuncReference(expr) {
			if funcProps == nil {
				funcProps = make(map[string]bool)
			}
			funcProps[name] = true
			// Function props have no const value; keep them absent from the
			// child's localProps so a `var X = props.<fn>` alias isn't folded to
			// a function-name string.
			continue
		}
		b.localProps[name] = evalConstWithSignals(expr, b.sigMap(), savedProps)
	}
	b.localFuncProps = funcProps
	// Record what a rest parameter collects: every call-site attr not bound by
	// a named/destructured parameter.
	if restName := fnRestParamName(childFn); restName != "" {
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
		b.restProps = map[string]map[string]ast.Expr{restName: rest}
	} else {
		b.restProps = nil
	}
	savedCallSite := b.callSiteChildren
	b.callSiteChildren = el.Children

	childNode := b.buildComponentNode(childFn, string(childID))
	b.localProps = savedProps
	b.localFuncProps = savedFuncProps
	b.restProps = savedRestProps
	childNode.Props = extractProps(el)
	childNode.CallSiteChildren = el.Children
	childNode.CallSiteSlots = b.buildCallSiteChildSlots(el.Children, string(childID))
	b.callSiteChildren = savedCallSite

	// Auto-promoted SSR-evaluated components need prop bindings so that
	// {props.X} resolves at build time during SSREval. The bindings are
	// built from call-site attrs (not node.Props which is the raw AST)
	// and the component's parameter names.
	if childNode.IsSSREval && childNode.SSREvalBindings == nil {
		paramNames := extractParamNames(childFn)
		childNode.SSREvalBindings = b.buildPropBindings(paramNames, attrs, childFn)
	}

	// Hoist props for handlers that reference props.X. The props object
	// values may reference THIS component's signals (e.g. checked={checked1()}),
	// so it must be built in the parent's scope and shared with the child via
	// the __krate_props registry instead of inlining it into the child IIFE
	// (which would reference out-of-scope identifiers). Even with no call-site
	// props the child handlers may read props.X (e.g. props.onOpenChange), so
	// an empty {} registration is emitted to keep `props` defined.
	if childNode.Tier == TierClient && (len(childNode.Handlers) > 0 || len(childNode.Effects) > 0 || len(childNode.Memos) > 0 || len(childNode.Signals) > 0) {
		if handlersOrLocalsReferenceProps(childNode.Handlers, childFn.Body) ||
			compiledRefsProps(childNode.Effects) || compiledRefsProps(childNode.Memos) ||
			signalsReferenceProps(childNode.Signals) || len(childNode.FuncPropAliases) > 0 ||
			// A child that reads props only inside rendered JSX (e.g. a list
			// binding `props.items.map(...)`) still needs the registry so
			// `props` resolves at hydration.
			bodyReferencesProps(childFn.Body) {
			reg := b.buildChildPropsRegDecl(string(childNode.ID), childNode.Props)
			b.pendingPropsRegs = append(b.pendingPropsRegs, reg)
			childNode.ExtraVars = append([]string{"var props=__krate_props[" + strconv.Quote(string(childNode.ID)) + "]"}, childNode.ExtraVars...)
		}
	}

	// Walk children and update TextSlot initials with resolved values
	// (buildTextSlot uses resolveSignal which can't resolve props.X at build time)
	updateTextSlotInitials(childNode.Children, childNode.Signals)

	return []SlotNode{&ComponentSlot{
		ID:        childID,
		Component: childNode,
	}}
}

// inlineSignalPropsComponent inlines a signal-less component whose props read
// the parent's signals (e.g. <Display value={display()}/>). Its return JSX is
// built directly into the parent's slot tree with props.X substituted by their
// call-site expressions, so {props.value} becomes the reactive {display()}
// instead of a static snapshot.
func (b *builder) inlineSignalPropsComponent(el *ast.JSXElement, childFn *ast.FnDecl, childID SlotID, attrs map[string]ast.Expr) []SlotNode {
	instanceIdx := b.instanceCounts[childFn.Name]
	b.instanceCounts[childFn.Name]++
	id := SlotID(string(childID) + "_c" + itoa(instanceIdx))

	ret := findReturnStmt(childFn.Body)
	if ret == nil || ret.Value == nil {
		return nil
	}
	substituted := substitutePropExprs(ret.Value, attrs)
	return b.buildSlotNodes(substituted, string(id))
}

// componentHasSignalProps reports whether any call-site prop reads a signal.
func (b *builder) componentHasSignalProps(attrs map[string]ast.Expr) bool {
	for _, expr := range attrs {
		if b.referencesSignal(expr) {
			return true
		}
	}
	return false
}

// substitutePropExprs returns a copy of expr with props.X member accesses
// replaced by their call-site expressions (used when inlining a signal-less
// component that receives signal-derived props).
func substitutePropExprs(expr ast.Expr, props map[string]ast.Expr) ast.Expr {
	if expr == nil || len(props) == 0 {
		return expr
	}
	switch e := expr.(type) {
	case *ast.MemberExpr:
		if !e.Computed {
			if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
				if pid, ok := e.Property.(*ast.Identifier); ok {
					if repl, ok := props[pid.Name]; ok {
						return repl
					}
				}
			}
		}
		return &ast.MemberExpr{Position: e.Position, Object: substitutePropExprs(e.Object, props), Property: e.Property, Computed: e.Computed, Optional: e.Optional}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{Position: e.Position, Left: substitutePropExprs(e.Left, props), Op: e.Op, Right: substitutePropExprs(e.Right, props)}
	case *ast.ConditionalExpr:
		return &ast.ConditionalExpr{Position: e.Position, Test: substitutePropExprs(e.Test, props), Consequent: substitutePropExprs(e.Consequent, props), Alternate: substitutePropExprs(e.Alternate, props)}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Position: e.Position, Op: e.Op, Arg: substitutePropExprs(e.Arg, props), Postfix: e.Postfix}
	case *ast.CallExpr:
		return &ast.CallExpr{Position: e.Position, Callee: substitutePropExprs(e.Callee, props), Args: substitutePropExprList(e.Args, props)}
	case *ast.TemplateExpr:
		return &ast.TemplateExpr{Position: e.Position, Parts: substitutePropExprList(e.Parts, props), Raw: e.Raw}
	case *ast.ArrayExpr:
		return &ast.ArrayExpr{Position: e.Position, Elements: substitutePropExprList(e.Elements, props)}
	case *ast.ObjectExpr:
		out := make([]*ast.ObjectProp, len(e.Properties))
		for i, p := range e.Properties {
			out[i] = &ast.ObjectProp{Key: p.Key, Value: substitutePropExprs(p.Value, props), Shorthand: p.Shorthand, Spread: p.Spread, Method: p.Method}
		}
		return &ast.ObjectExpr{Position: e.Position, Properties: out}
	case *ast.TypeAssertion:
		return &ast.TypeAssertion{Position: e.Position, Expr: substitutePropExprs(e.Expr, props), TypeRef: e.TypeRef}
	case *ast.JSXElement:
		return substitutePropExprsJSXElement(e, props)
	case *ast.JSXFragment:
		return &ast.JSXFragment{Position: e.Position, Children: substitutePropExprsJSXChildren(e.Children, props)}
	case *ast.NewExpr:
		return &ast.NewExpr{Position: e.Position, Callee: substitutePropExprs(e.Callee, props), Args: substitutePropExprList(e.Args, props)}
	case *ast.AwaitExpr:
		return &ast.AwaitExpr{Position: e.Position, Arg: substitutePropExprs(e.Arg, props)}
	case *ast.DynamicImport:
		return &ast.DynamicImport{Position: e.Position, Arg: substitutePropExprs(e.Arg, props)}
	case *ast.ImportMetaExpr:
		return &ast.ImportMetaExpr{Position: e.Position}
	default:
		// Identifiers, literals, arrow functions, this, etc. are returned as-is.
		return expr
	}
}

func substitutePropExprList(list []ast.Expr, props map[string]ast.Expr) []ast.Expr {
	out := make([]ast.Expr, len(list))
	for i, e := range list {
		out[i] = substitutePropExprs(e, props)
	}
	return out
}

func substitutePropExprsJSXElement(el *ast.JSXElement, props map[string]ast.Expr) *ast.JSXElement {
	opening := &ast.JSXOpening{Name: el.Opening.Name, SelfClosing: el.Opening.SelfClosing}
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil {
			opening.Attributes = append(opening.Attributes, attr)
			continue
		}
		opening.Attributes = append(opening.Attributes, &ast.JSXAttr{Position: attr.Position, Name: attr.Name, Value: substitutePropExprs(attr.Value, props)})
	}
	return &ast.JSXElement{
		Position: el.Position,
		Opening:  opening,
		Children: substitutePropExprsJSXChildren(el.Children, props),
		Closing:  el.Closing,
	}
}

func substitutePropExprsJSXChildren(children []ast.JSXChild, props map[string]ast.Expr) []ast.JSXChild {
	out := make([]ast.JSXChild, 0, len(children))
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			out = append(out, &ast.JSXExprContainer{Expression: substitutePropExprs(c.Expression, props)})
		case *ast.JSXElementChild:
			out = append(out, &ast.JSXElementChild{Element: substitutePropExprsJSXElement(c.Element, props)})
		case *ast.JSXFragmentChild:
			out = append(out, &ast.JSXFragmentChild{Fragment: &ast.JSXFragment{Position: c.Fragment.Position, Children: substitutePropExprsJSXChildren(c.Fragment.Children, props)}})
		default:
			out = append(out, child)
		}
	}
	return out
}

// buildStaticElementSlots - HTML element, returns []SlotNode
// When the element has component or dynamic children, the result is split:
//   [StaticHTML(opening+pre), ComponentSlot, StaticHTML(post+closing)]
// When all children are static, returns a single StaticHTML.

func (b *builder) buildStaticElementSlots(el *ast.JSXElement, parentID string) []SlotNode {
	logicalID := joinSlotID(parentID, b.nextElementTag(el.Opening.Name, parentID))
	id := b.assignSlotID(logicalID)
	var children []SlotNode
	var handlers []HandlerDecl
	var attrs []AttrBinding

	// dangerouslySetInnerHTML={{__html: "..."}} injects raw, pre-rendered HTML
	// (e.g. build-time markdown). When statically resolvable, the whole element
	// becomes a single StaticHTML slot so the value is never HTML-escaped.
	if rawHTML, ok := b.evalInnerHTMLAttr(el); ok {
		openingTag := b.buildElementOpening(el, nil, nil, nil, string(id))
		return []SlotNode{&StaticHTML{HTML: openingTag + ">" + rawHTML + "</" + el.Opening.Name + ">"}}
	}

	// Process attributes: handlers, bindings, static attrs. A spread whose
	// value is a known rest parameter (`{...props}`) is expanded to the
	// collected call-site attributes so forwarded props reach the element.
	var refs []RefBinding
	attributes, _ := b.expandSpreadAttrs(el.Opening.Attributes)
	for _, attr := range attributes {
		if attr.Spread {
			continue
		}
		// React-only directives (key, suppressHydrationWarning) carry no DOM
		// meaning and must not become attributes or hydration bindings.
		if isReactDirectiveAttr(attr.Name) {
			continue
		}
		if attr.Name == "ref" {
			if attr.Value != nil {
				// Callback ref: ref={(el) => {...}}. The arrow function is the
				// callback itself - it receives the mounted element directly
				// (React-style). Render it as-is instead of treating it as an
				// assignment target.
				if fn, ok := attr.Value.(*ast.ArrowFn); ok {
					if cb := renderArrowFn(fn, b.sigMap()); cb != "" {
						refs = append(refs, RefBinding{ElementSlotID: id, Callback: cb})
					}
					continue
				}
				target := generateExprJS(attr.Value, b.sigMap())
				if target != "" {
					// ref={fnName} where fnName is a function declared in this
					// component is a callback ref: pass the function to kbindRef so
					// it is INVOKED with the mounted element. Falling through to the
					// assignment target would emit el=>{fnName=el;} - overwriting
					// the function variable and breaking later calls/reads.
					if refID, ok := attr.Value.(*ast.Identifier); ok && b.refCallbackVars != nil && b.refCallbackVars[refID.Name] {
						refs = append(refs, RefBinding{ElementSlotID: id, Callback: refID.Name})
						continue
					}
					// An identifier ref may be a `{current}` object (useRef), a
					// callback, or a ref forwarded through props/params. Emit an
					// adaptive setter that handles all three, so `ref={inputRef}`
					// works whether inputRef is an object or a function.
					if _, ok := attr.Value.(*ast.Identifier); ok {
						refs = append(refs, RefBinding{ElementSlotID: id, Target: target, Adaptive: true})
						continue
					}
					refs = append(refs, RefBinding{ElementSlotID: id, Target: target})
				}
			}
			continue
		}
		if isOnEvent(attr.Name) {
			h := b.buildHandlerDecl(attr, string(id))
			if h != nil {
				handlers = append(handlers, *h)
			}
		} else if isAttrBinding(attr) {
			a := b.buildAttrBinding(attr, string(id))
			if a != nil {
				attrs = append(attrs, *a)
			}
		}
	}

	// Accumulate handlers for the component node
	b.pendingHandlers = append(b.pendingHandlers, handlers...)
	b.pendingAttrs = append(b.pendingAttrs, attrs...)
	b.pendingRefs = append(b.pendingRefs, refs...)

	// Process children
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			if c.Value != "" {
				children = append(children, &StaticHTML{HTML: c.Value})
			}
		case *ast.JSXExprContainer:
			childNodes := b.buildExprContainerChildren(c, string(id))
			children = append(children, childNodes...)
		case *ast.JSXElementChild:
			childNodes := b.buildSlotNodes(c.Element, string(id))
			children = append(children, childNodes...)
		case *ast.JSXFragmentChild:
			childNodes := b.buildFragmentSlots(c.Fragment, string(id))
			children = append(children, childNodes...)
		}
	}

	// Check if any children are ComponentSlots or other dynamic non-StaticHTML slots
	hasDynamicChildren := false
	for _, child := range children {
		if child == nil {
			continue
		}
		switch child.(type) {
		case *StaticHTML, *ChildrenSlot:
			// These are OK to inline
		default:
			hasDynamicChildren = true
		}
	}

	openingTag := b.buildElementOpening(el, handlers, attrs, refs, string(id))
	closingTag := "</" + el.Opening.Name + ">"

	// Self-closing: void HTML elements can use />, others need full </tagName>
	if el.Opening.SelfClosing {
		if isVoidElement(el.Opening.Name) {
			return []SlotNode{&StaticHTML{HTML: openingTag + " />"}}
		}
		return []SlotNode{&StaticHTML{HTML: openingTag + "></" + el.Opening.Name + ">"}}
	}

	// No dynamic children - everything fits in one StaticHTML
	if !hasDynamicChildren {
		var buf strings.Builder
		buf.WriteString(openingTag)
		buf.WriteByte('>')
		for _, child := range children {
			if s, ok := child.(*StaticHTML); ok {
				buf.WriteString(s.HTML)
			} else if _, ok := child.(*ChildrenSlot); ok {
				buf.WriteString("<!--__children__-->")
			}
		}
		buf.WriteString(closingTag)
		return []SlotNode{&StaticHTML{HTML: buf.String()}}
	}

	// Has dynamic children - split into StaticHTML segments around them
	var result []SlotNode
	var preBuf strings.Builder
	preBuf.WriteString(openingTag)
	preBuf.WriteByte('>')

	for _, child := range children {
		if child == nil {
			continue
		}
		switch c := child.(type) {
		case *StaticHTML:
			preBuf.WriteString(c.HTML)
		case *ChildrenSlot:
			preBuf.WriteString("<!--__children__-->")
		default:
			// Flush pre-buffer as StaticHTML, then add the dynamic child
			result = append(result, &StaticHTML{HTML: preBuf.String()})
			preBuf.Reset()
			result = append(result, child)
		}
	}
	// Flush remaining pre-buffer + closing tag
	preBuf.WriteString(closingTag)
	result = append(result, &StaticHTML{HTML: preBuf.String()})

	return result
}

// buildElementOpening builds the opening tag string with attributes.
func (b *builder) buildElementOpening(el *ast.JSXElement, handlers []HandlerDecl, attrs []AttrBinding, refs []RefBinding, elementSlotID string) string {
	var buf strings.Builder
	name := el.Opening.Name

	buf.WriteByte('<')
	buf.WriteString(name)

	// Emit data-k attribute for elements with handlers, refs, or dynamic
	// attribute bindings so the hydration code can find them via querySelector.
	if len(handlers) > 0 || len(attrs) > 0 || len(refs) > 0 {
		buf.WriteString(` data-k="k:`)
		buf.WriteString(elementSlotID)
		buf.WriteByte('"')
	}

	// Static attributes (spread rest params expanded to concrete attributes).
	attrsForOpening, _ := b.expandSpreadAttrs(el.Opening.Attributes)
	for _, attr := range attrsForOpening {
		if attr.Spread || attr.Name == "ref" || attr.Name == "dangerouslySetInnerHTML" {
			continue
		}
		if isReactDirectiveAttr(attr.Name) {
			continue
		}
		if isOnEvent(attr.Name) {
			continue
		}
		// Dynamic bindings are emitted below (with SSR initial + marker).
		// Statically-resolvable values - even those typed as bindings (e.g.
		// {String(i)}, {length}) - are emitted directly here instead.
		if isAttrBinding(attr) && !b.isStaticResolvable(attr.Value) {
			continue
		}
		if attr.Value != nil {
			val := b.evalAttrValue(attr.Value)
			// Boolean attributes: omit when falsy, emit bare name when true.
			if isBooleanAttr(attr.Name) {
				if val == "true" {
					buf.WriteByte(' ')
					buf.WriteString(ast.HTMLAttrName(attr.Name))
					buf.WriteString(`="true"`)
				}
				continue
			}
			// undefined/null attributes are omitted entirely (React semantics)
			// instead of rendering `value="undefined"`. A string literal
			// "null"/"undefined" is real text and is kept.
			if !isStringLiteral(attr.Value) && (val == "undefined" || val == "null") {
				continue
			}
			buf.WriteByte(' ')
			buf.WriteString(ast.HTMLAttrName(attr.Name))
			buf.WriteString(`="`)
			buf.WriteString(escape.HTML(val))
			buf.WriteByte('"')
		} else {
			buf.WriteByte(' ')
			buf.WriteString(ast.HTMLAttrName(attr.Name))
		}
	}

	// Dynamic attributes: emit the SSR initial value plus the hydration marker.
	for _, a := range attrs {
		// Boolean attributes: omit when false/unset, emit bare name when true.
		// All other attributes: emit when the initial value is non-empty.
		if isBooleanAttr(a.AttrName) {
			if a.Initial == "true" {
				buf.WriteByte(' ')
				buf.WriteString(ast.HTMLAttrName(a.AttrName))
				buf.WriteString(`="true"`)
			}
		} else if a.Initial != "" && (a.Initial != "undefined" && a.Initial != "null" || isStringLiteral(a.InitialExpr)) {
			buf.WriteByte(' ')
			buf.WriteString(ast.HTMLAttrName(a.AttrName))
			buf.WriteString(`="`)
			buf.WriteString(escape.HTML(a.Initial))
			buf.WriteByte('"')
		}
		buf.WriteString(fmt.Sprintf(` data-kattr-%s="k:%s"`, a.AttrName, a.ElementSlotID))
	}

	return buf.String()
}

// isBooleanAttr reports whether an HTML attribute is a boolean attribute whose
// presence alone is meaningful (an empty/false value would still be truthy).
func isBooleanAttr(name string) bool {
	switch name {
	case "disabled", "required", "readonly", "checked", "selected", "multiple", "autofocus", "hidden", "inert", "novalidate", "open", "async", "defer", "autoplay", "controls", "loop", "muted", "playsinline", "truespeed", "allowfullscreen", "default", "ismap", "itemscope", "nohref", "noresize", "noshade", "nowrap", "reversed", "scoped", "seamless", "sortable", "translate":
		return true
	}
	return false
}

// buildExprContainerChildren - build slots from {expr} in JSX

func (b *builder) buildExprContainerChildren(ec *ast.JSXExprContainer, parentID string) []SlotNode {
	return b.buildExprContainerChildrenMode(ec, parentID, true)
}

// buildMetaExprContainerChildren is the raw (non-escaping) variant used by
// Head/Script/Style meta slots whose content is intentionally raw HTML.
func (b *builder) buildMetaExprContainerChildren(ec *ast.JSXExprContainer, parentID string) []SlotNode {
	return b.buildExprContainerChildrenMode(ec, parentID, false)
}

// buildExprContainerChildrenMode routes a {expr} JSX container child to the
// correct slot type. When `doEscape` is true the statically-evaluated result is
// HTML-escaped because it sits in a JSX text position (a template literal like
// <Code>{`function App() { return <h1>...</h1>; }`}</Code> must not leak real
// markup). Meta content passes doEscape=false so inline scripts stay raw.
func (b *builder) buildExprContainerChildrenMode(ec *ast.JSXExprContainer, parentID string, doEscape bool) []SlotNode {
	if ec.Expression == nil {
		return nil
	}

	// Special: {children} placeholder for layouts
	if id, ok := ec.Expression.(*ast.Identifier); ok && id.Name == "children" {
		return []SlotNode{&ChildrenSlot{}}
	}

	// Special: {props.children} - same children placeholder
	if mem, ok := ec.Expression.(*ast.MemberExpr); ok {
		if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == "props" {
			if pid, ok := mem.Property.(*ast.Identifier); ok && pid.Name == "children" {
				return []SlotNode{&ChildrenSlot{}}
			}
		}
	}

	// CSS signal live text: a bare `{getter()}` read of a compiled scope renders
	// the inherited `--krate-current` custom property via `.krc-live::after`,
	// with zero JS (the value updates through the `:has()` variable rules).
	if b.cssSignals != nil && b.cssSignals.OK() {
		if _, ok := b.cssSignals.MatchText(ec.Expression); ok {
			return []SlotNode{&StaticHTML{HTML: `<span class="krc-live"></span>`}}
		}
	}

	// .map() call - always try to resolve at build time (even without signals)
	if isMapCall(ec.Expression) {
		return []SlotNode{b.buildListSlot(ec.Expression, parentID)}
	}

	// Ternary with JSX in branches: {cond() ? <A/> : <B/>}
	// This must be checked BEFORE the static value path, otherwise
	// unresolved variables (e.g. title, showCopy from props) cause
	// evalConst to fall through to a null alternate and emit "null".
	if cond, ok := ec.Expression.(*ast.ConditionalExpr); ok {
		// Statically-known test -> build the winning branch directly so SSR
		// renders real content (e.g. a checkmark SVG or children label).
		if nodes := b.tryResolveConditional(cond, parentID); len(nodes) > 0 {
			return nodes
		}
		if isTernaryWithJSX(ec.Expression) {
			return []SlotNode{b.buildConditionalSlot(ec.Expression, parentID)}
		}
	}

	// Boolean short-circuit with JSX on the right: {left && <A/>} and
	// {left || <A/>}.  The static-value path const-evaluates the right
	// operand via evalConst, which cannot serialize JSX to HTML (returns "")
	// so client-tier layouts (e.g. the docs theme shell) that gate sections
	// on props would silently lose that markup.  Route guards through the
	// conditional-slot machinery instead: statically-known tests fold to
	// the winning branch so SSR renders real content; reactive guards
	// become hidable ConditionalSlots.
	if bin, ok := ec.Expression.(*ast.BinaryExpr); ok && (bin.Op == "&&" || bin.Op == "||") {
		if hasJSXInExpr(bin.Right) {
			if nodes, resolved := b.tryResolveGuard(bin, parentID); resolved {
				return nodes
			}
			return []SlotNode{b.buildGuardSlot(bin, parentID)}
		}
	}

	// Check if expression references any signal
	if !b.referencesSignal(ec.Expression) {
		// Simple identifiers that reference local variables (not signals,
		// not children) must go through the dynamic expr slot so they
		// are evaluated at hydration time. Otherwise evalConst would
		// return the variable name as literal text ("inputElements").
		// First check for a for-loop-built JSX array so components like
		// OTPField can render their imperatively-pushed element lists.
		if id, ok := ec.Expression.(*ast.Identifier); ok {
			if nodes := b.tryResolveForLoopArray(ec.Expression, parentID); len(nodes) > 0 {
				return nodes
			}
			// Module-level constants (const X = <literal>) resolve to their
			// value at build time; render them statically instead of deferring
			// to a hydration binding that would reference an out-of-scope
			// identifier (which would render the raw name as text).
			if id.Name != "children" {
				if v, isConst := b.moduleConsts[id.Name]; isConst {
					if doEscape {
						return []SlotNode{&StaticHTML{HTML: escape.HTML(v)}}
					}
					return []SlotNode{&StaticHTML{HTML: v}}
				}
				// Local variables resolved via collectLocalVars (var X = expr)
				// have known values at build time; render statically.
				if v, ok := b.localProps[id.Name]; ok {
					if doEscape {
						return []SlotNode{&StaticHTML{HTML: escape.HTML(v)}}
					}
					return []SlotNode{&StaticHTML{HTML: v}}
				}
			}
			return []SlotNode{b.buildExprSlot(ec.Expression, parentID)}
		}
		slot := b.buildStaticValueSlot(ec.Expression, parentID)
		if slot != nil {
			if doEscape {
				return []SlotNode{&StaticHTML{HTML: escape.HTML(slot.HTML)}}
			}
			return []SlotNode{slot}
		}
		return nil
	}

	// Simple signal read: {count()}
	if isSimpleSignalRead(ec.Expression) {
		slot := b.buildTextSlot(ec.Expression, parentID)
		if slot != nil {
			return []SlotNode{slot}
		}
		return nil
	}

	// .map() call
	if isMapCall(ec.Expression) {
		return []SlotNode{b.buildListSlot(ec.Expression, parentID)}
	}

	// Fallback: complex expression (binary, member chain, template, etc.)
	return []SlotNode{b.buildExprSlot(ec.Expression, parentID)}
}

// buildTextSlot - simple signal read

func (b *builder) buildTextSlot(expr ast.Expr, parentID string) *TextSlot {
	signalName := b.extractSignalName(expr)
	if signalName == "" {
		return nil
	}

	id := b.assignSlotID(b.nextDynamicSlot(parentID, "text"))
	decl := b.resolveSignal(signalName)
	initial := evalConstWithSignals(decl.InitialExpr, b.sigMap(), b.localProps)

	return &TextSlot{
		ID:      id,
		Signal:  decl,
		Initial: initial,
	}
}

// buildExprSlot - complex expression

func (b *builder) buildExprSlot(expr ast.Expr, parentID string) *ExprSlot {
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "expr"))
	signals := b.collectSignalReads(expr)
	exprSource := generateExprJS(expr, b.sigMap())
	initial := evalConstWithSignals(expr, b.sigMap(), b.localProps)

	return &ExprSlot{
		ID:         id,
		ExprSource: exprSource,
		Signals:    signals,
		Initial:    initial,
	}
}

// tryResolveForLoopArray detects the `var NAME = []; for (...) { NAME.push(<JSX/>) }`
// pattern and statically builds the pushed JSX elements into slot nodes so
// components like OTPField render their imperatively-constructed input lists.
func (b *builder) tryResolveForLoopArray(expr ast.Expr, parentID string) []SlotNode {
	id, ok := expr.(*ast.Identifier)
	if !ok || b.localFnBody == nil {
		return nil
	}
	name := id.Name

	arrayDecl := false
	for _, stmt := range b.localFnBody {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name == name {
				if arr, ok := decl.Init.(*ast.ArrayExpr); ok && len(arr.Elements) == 0 {
					arrayDecl = true
				}
			}
		}
	}
	if !arrayDecl {
		return nil
	}

	for _, stmt := range b.localFnBody {
		fs, ok := stmt.(*ast.ForStmt)
		if !ok {
			continue
		}
		var pushArgs []ast.Expr
		for _, bodyStmt := range fs.Body {
			es, ok := bodyStmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := es.Expression.(*ast.CallExpr)
			if !ok {
				continue
			}
			mem, ok := call.Callee.(*ast.MemberExpr)
			if !ok {
				continue
			}
			objID, ok := mem.Object.(*ast.Identifier)
			if !ok || objID.Name != name {
				continue
			}
			propID, ok := mem.Property.(*ast.Identifier)
			if !ok || propID.Name != "push" || len(call.Args) != 1 {
				continue
			}
			if hasJSXInExpr(call.Args[0]) {
				pushArgs = append(pushArgs, call.Args[0])
			}
		}
		if len(pushArgs) > 0 {
			return b.evalForLoopArray(fs, pushArgs, parentID)
		}
	}
	return nil
}

// evalForLoopArray statically iterates a for-loop, binding the loop variable
// into the props context, and builds each pushed JSX element as a slot node.
func (b *builder) evalForLoopArray(fs *ast.ForStmt, pushArgs []ast.Expr, parentID string) []SlotNode {
	loopVars := make(map[string]string)
	evalProps := func() map[string]string {
		m := make(map[string]string, len(b.localProps)+len(loopVars))
		for k, v := range b.localProps {
			m[k] = v
		}
		for k, v := range loopVars {
			m[k] = v
		}
		return m
	}

	if vs, ok := fs.Init.(*ast.VarStmt); ok {
		for _, decl := range vs.Decls {
			if decl.Name != "" {
				if decl.Init != nil {
					loopVars[decl.Name] = evalConstWithSignals(decl.Init, b.sigMap(), evalProps())
				} else {
					loopVars[decl.Name] = ""
				}
			}
		}
	}

	savedProps := b.localProps
	defer func() { b.localProps = savedProps }()

	var nodes []SlotNode
	for iter := 0; iter < 1000; iter++ {
		if isFalsyValue(evalConstWithSignals(fs.Test, b.sigMap(), evalProps())) {
			break
		}
		b.localProps = evalProps()
		for _, arg := range pushArgs {
			nodes = append(nodes, b.buildSlotNodes(arg, parentID)...)
		}
		if upd, ok := fs.Update.(*ast.UnaryExpr); ok && (upd.Op == "++" || upd.Op == "--") {
			if id, ok := upd.Arg.(*ast.Identifier); ok {
				if n, ok := parseNumeric(loopVars[id.Name]); ok {
					if upd.Op == "++" {
						loopVars[id.Name] = trimFloat(n + 1)
					} else {
						loopVars[id.Name] = trimFloat(n - 1)
					}
				}
			}
		}
	}
	return nodes
}

// buildConditionalSlot - ternary with JSX

// tryResolveConditional statically resolves a ternary whose test is known at
// build time, building the winning branch's slot nodes directly. This lets
// SSR render the actual JSX branch (e.g. a checkmark SVG) instead of an empty
// hydration-only ConditionalSlot. Returns nil when the test isn't resolvable
// OR when the test depends on signals (signal-backed ternaries must stay
// reactive - either as an ExprSlot for text or a ConditionalSlot for JSX).
func (b *builder) tryResolveConditional(cond *ast.ConditionalExpr, parentID string) []SlotNode {
	if b.referencesSignal(cond.Test) {
		return nil
	}
	val, known := b.knownTestValue(cond.Test)
	if !known {
		return nil
	}
	var branch ast.Expr
	if isTruthyValue(val) {
		branch = cond.Consequent
	} else {
		branch = cond.Alternate
	}
	switch br := branch.(type) {
	case *ast.JSXElement:
		return b.buildSlotNodes(br, parentID)
	case *ast.JSXFragment:
		return b.buildSlotNodes(br, parentID)
	default:
		v := evalConstWithSignals(branch, b.sigMap(), b.localProps)
		return []SlotNode{&StaticHTML{HTML: v}}
	}
}

// ShowIfExpr returns the test for a `showIf`/`visibleIf` attribute along with a
// shallow copy of the element with all such attributes removed. The third return
// is false when the element has neither attribute. A missing value
// (`<X showIf />`) yields the literal `true`. When both `showIf` and
// `visibleIf` are present, `showIf` wins (the canonical spelling).
// The original AST is never mutated: JSX elements are shared across dynamic
// route renders and multiple component instances, so only the attribute slice
// is rebuilt and the opening/element structs are shallow-copied.
// Exported so the SSREval path (renderer) can handle signal-less components
// identically to the slot-builder path.
func ShowIfExpr(el *ast.JSXElement) (ast.Expr, *ast.JSXElement, bool) {
	if el == nil || el.Opening == nil {
		return nil, nil, false
	}
	// Collect all occurrences; `showIf` is preferred over `visibleIf`.
	canonicalIdx, aliasIdx := -1, -1
	for i, attr := range el.Opening.Attributes {
		if attr == nil || attr.Spread {
			continue
		}
		switch attr.Name {
		case "showIf":
			if canonicalIdx < 0 {
				canonicalIdx = i
			}
		case "visibleIf":
			if aliasIdx < 0 {
				aliasIdx = i
			}
		}
	}
	if canonicalIdx < 0 && aliasIdx < 0 {
		return nil, nil, false
	}
	testIdx := canonicalIdx
	if testIdx < 0 {
		testIdx = aliasIdx
	}
	test := el.Opening.Attributes[testIdx].Value
	if test == nil {
		test = &ast.Literal{Kind: ast.BoolLit, Value: "true"}
	}
	attrs := make([]*ast.JSXAttr, 0, len(el.Opening.Attributes))
	for i, attr := range el.Opening.Attributes {
		if i == canonicalIdx || i == aliasIdx {
			continue
		}
		attrs = append(attrs, attr)
	}
	opening := *el.Opening
	opening.Attributes = attrs
	clone := *el
	clone.Opening = &opening
	return test, &clone, true
}

// isShowIfAttr reports whether an attribute is the showIf/visibleIf sugar, which
// is always compiler-erased and must never reach emitted HTML or a component's
// props.
func isShowIfAttr(name string) bool {
	return name == "showIf" || name == "visibleIf"
}

// tryResolveGuard folds a boolean short-circuit guard ({cond && <A/>} /
// {cond || <A/>}) when the guard test is statically known, rendering the
// JSX operand that would be active.  Returns slot nodes and true when the
// guard was resolved; returns nil and false when the test depends on a
// signal or otherwise cannot be determined at build time.
func (b *builder) tryResolveGuard(bin *ast.BinaryExpr, parentID string) ([]SlotNode, bool) {
	if b.referencesSignal(bin.Left) {
		return nil, false
	}
	val, known := b.knownTestValue(bin.Left)
	if !known {
		return nil, false
	}
	truthy := isTruthyValue(val)
	// && renders the right operand when the left is truthy;
	// || renders the right operand when the left is falsy.
	renderJSX := (bin.Op == "&&") == truthy
	if !renderJSX {
		// Guard renders nothing emit nothing (nil slice).
		return nil, true
	}
	return b.buildSlotNodes(bin.Right, parentID), true
}

// buildGuardSlot converts a BinaryExpr guard ({A && B} / {A || B}) to a
// ConditionalSlot.  For {A && B} the equivalent ternary is A ? B : <empty>;
// for {A || B} it is A ? <empty> : B.
func (b *builder) buildGuardSlot(bin *ast.BinaryExpr, parentID string) *ConditionalSlot {
	// Normalize: {A && B} -> A ? B : <empty/nil>, {A || B} -> A ? nil : B.
	var consequent, alternate ast.Expr
	if bin.Op == "&&" {
		consequent = bin.Right
	} else {
		alternate = bin.Right
	}
	norm := &ast.ConditionalExpr{Test: bin.Left, Consequent: consequent, Alternate: alternate}
	return b.buildConditionalSlot(norm, parentID)
}

// knownTestValue resolves a ternary test to a value and reports whether the
// resolution is authoritative (no unresolvable identifiers leaked).
func (b *builder) knownTestValue(test ast.Expr) (string, bool) {
	// {props.children ? <A/> : <B/>} - truthiness comes from call-site children.
	if mem, ok := test.(*ast.MemberExpr); ok {
		if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == "props" {
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "children" {
				if b.hasCallSiteChildren() {
					return "true", true
				}
				return "", true
			}
		}
	}
	v := evalConstWithSignals(test, b.sigMap(), b.localProps)
	if b.testFullyKnown(test) || b.propsReadsClosed(test) {
		return v, true
	}
	return v, false
}

// propsReadsClosed reports whether every leaf of a guard/ternary test is
// either resolvable at build time or a direct `props.<name>` member read.
// A component's props are closed at the call site: any name not present in
// localProps is a definitive undefined, so `{props.optional && <X/>}` folds to
// nothing instead of degrading to a hydration-only ConditionalSlot.
func (b *builder) propsReadsClosed(e ast.Expr) bool {
	switch x := e.(type) {
	case nil:
		return true
	case *ast.Literal:
		return true
	case *ast.Identifier:
		if _, ok := b.sigMap()[x.Name]; ok {
			return true
		}
		if _, ok := b.localProps[x.Name]; ok {
			return true
		}
		if _, ok := b.moduleConsts[x.Name]; ok {
			return true
		}
		return false
	case *ast.MemberExpr:
		// A direct props.<name> read is always closed: present in localProps or
		// a definitive undefined. Function props have no build-time value.
		if id, ok := x.Object.(*ast.Identifier); ok && id.Name == "props" {
			if prop, ok := x.Property.(*ast.Identifier); ok {
				if b.localFuncProps != nil && b.localFuncProps[prop.Name] {
					return false
				}
				return true
			}
		}
		// Deeper chains (props.a.b, props.a.length) are closed only when they
		// fully const-fold against the call-site props.
		if _, ok := resolveMemberChainValue(x, b.localProps); ok {
			return true
		}
		return false
	case *ast.BinaryExpr:
		return b.propsReadsClosed(x.Left) && b.propsReadsClosed(x.Right)
	case *ast.UnaryExpr:
		return b.propsReadsClosed(x.Arg)
	case *ast.ConditionalExpr:
		return b.propsReadsClosed(x.Test) && b.propsReadsClosed(x.Consequent) && b.propsReadsClosed(x.Alternate)
	case *ast.TemplateExpr:
		for _, p := range x.Parts {
			if !b.propsReadsClosed(p) {
				return false
			}
		}
		return true
	case *ast.TypeAssertion:
		return b.propsReadsClosed(x.Expr)
	}
	return false
}

func (b *builder) testFullyKnown(e ast.Expr) bool {
	if e == nil {
		return false
	}
	switch x := e.(type) {
	case *ast.Literal:
		return true
	case *ast.Identifier:
		if _, ok := b.sigMap()[x.Name]; ok {
			return true
		}
		if _, ok := b.localProps[x.Name]; ok {
			return true
		}
		if _, ok := b.moduleConsts[x.Name]; ok {
			return true
		}
		return false
	case *ast.CallExpr:
		if id, ok := x.Callee.(*ast.Identifier); ok {
			if _, ok := b.sigMap()[id.Name]; ok {
				return true
			}
			// Pure built-in constructors: String(), Number(), Boolean()
			if (id.Name == "String" || id.Name == "Number" || id.Name == "Boolean") && len(x.Args) == 1 {
				return b.testFullyKnown(x.Args[0])
			}
		}
		return false
	case *ast.MemberExpr:
		if id, ok := x.Object.(*ast.Identifier); ok && id.Name == "props" {
			if prop, ok := x.Property.(*ast.Identifier); ok {
				if prop.Name == "children" {
					return true
				}
				_, ok := b.localProps[prop.Name]
				return ok
			}
		}
		// Nested chains rooted at props (props.a.b, props.a.length) are known
		// whenever the whole chain const-folds against the call-site props.
		if _, ok := resolveMemberChainValue(x, b.localProps); ok {
			return true
		}
		return false
	case *ast.BinaryExpr:
		return b.testFullyKnown(x.Left) && b.testFullyKnown(x.Right)
	case *ast.UnaryExpr:
		return b.testFullyKnown(x.Arg)
	case *ast.ConditionalExpr:
		return b.testFullyKnown(x.Test) && b.testFullyKnown(x.Consequent) && b.testFullyKnown(x.Alternate)
	case *ast.TemplateExpr:
		for _, p := range x.Parts {
			if !b.testFullyKnown(p) {
				return false
			}
		}
		return true
	case *ast.ArrayExpr:
		for _, el := range x.Elements {
			if el != nil && !b.testFullyKnown(el) {
				return false
			}
		}
		return true
	case *ast.ObjectExpr:
		for _, prop := range x.Properties {
			if !prop.Spread && prop.Value != nil && !b.testFullyKnown(prop.Value) {
				return false
			}
		}
		return true
	case *ast.TypeAssertion:
		return b.testFullyKnown(x.Expr)
	}
	return false
}

func (b *builder) hasCallSiteChildren() bool {
	if len(b.callSiteChildren) == 0 {
		return false
	}
	for _, child := range b.callSiteChildren {
		switch c := child.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(c.Value) != "" {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func (b *builder) buildConditionalSlot(expr ast.Expr, parentID string) *ConditionalSlot {
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "cond"))
	signals := b.collectSignalReads(expr)
	exprSource := generateExprJS(expr, b.sigMap())
	initial := evalConstWithSignals(expr, b.sigMap(), b.localProps)

	cond, _ := expr.(*ast.ConditionalExpr)
	var testJS string
	active := false
	if cond != nil {
		testJS = generateExprJS(cond.Test, b.sigMap())
		active = isTruthyValue(evalConstWithSignals(cond.Test, b.sigMap(), b.localProps))
	}

	slot := &ConditionalSlot{
		ID:            id,
		ExprSource:    exprSource,
		TestJS:        testJS,
		Initial:       initial,
		InitialActive: active,
		Signals:       signals,
	}
	if cond != nil {
		// Build branches under branch-scoped parents so nested slots in the
		// consequent and alternate branches never share slot IDs.
		slot.Consequent = b.buildSlotNodes(cond.Consequent, string(id)+".c")
		slot.Alternate = b.buildSlotNodes(cond.Alternate, string(id)+".a")
	}
	return slot
}

// buildListSlot - .map() with key detection

func (b *builder) buildListSlot(expr ast.Expr, parentID string) *ListSlot {
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "list"))
	exprSource := generateExprJS(expr, b.sigMap())

	// Resolve SSR items. A .map() over a literal array is resolved directly;
	// a .map() over a build-time constant (a prop, module const, or local var)
	// is resolved by evaluating the array source expression to a literal and
	// parsing it back, so client components still SSR their initial list
	// content instead of rendering an empty slot until hydration.
	items := b.tryResolveItems(expr, string(id))
	if len(items) == 0 {
		if arr := b.resolveArrayLiteralExpr(expr); arr != nil {
			items = b.tryResolveArrayItems(arr, expr, string(id))
		}
	}

	return &ListSlot{
		ID:         id,
		ExprSource: exprSource,
		Items:      items,
		Keyed:      false,
		Signals:    b.collectSignalReads(expr),
		Components: collectListComponents(expr),
	}
}

// resolveArrayLiteralExpr returns the ArrayExpr a .map() call iterates over,
// resolving identifier/member sources through the build-time constant tables
// (props locals, module consts). Returns nil when the array cannot be reduced
// to a literal at build time (e.g. a genuine runtime signal-driven list).
func (b *builder) resolveArrayLiteralExpr(expr ast.Expr) *ast.ArrayExpr {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok {
		return nil
	}
	if prop, ok := mem.Property.(*ast.Identifier); !ok || prop.Name != "map" {
		return nil
	}
	// Resolve the array object expression to a JS source literal.
	var source string
	switch obj := mem.Object.(type) {
	case *ast.ArrayExpr:
		return obj
	case *ast.Identifier:
		if v, isConst := b.moduleConsts[obj.Name]; isConst {
			source = v
		} else if v, ok := b.localProps[obj.Name]; ok {
			source = v
		}
	case *ast.MemberExpr:
		source = evalConstWithSignals(obj, b.sigMap(), b.localProps)
	}
	if source == "" {
		return nil
	}
	prog := parseExprAsProgram(source)
	if prog == nil {
		return nil
	}
	for _, stmt := range prog.Body {
		if es, ok := stmt.(*ast.ExprStmt); ok {
			if arr, ok := es.Expression.(*ast.ArrayExpr); ok {
				return arr
			}
		}
	}
	return nil
}

// tryResolveArrayItems renders a list slot's SSR items from an already-resolved
// ArrayExpr source, using the given .map() call expression to extract the
// mapping arrow. Each element is substituted for the arrow parameter inside a
// clone of the map body, and the concrete body is built through the full slot
// pipeline - so nested components recurse with their own hydration markers and
// member reads (item.title) fold to the element's real literal values.
func (b *builder) tryResolveArrayItems(arr *ast.ArrayExpr, mapExpr ast.Expr, listID string) []*ListItem {
	call, ok := mapExpr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil
	}
	arrow, ok := call.Args[0].(*ast.ArrowFn)
	if !ok {
		return nil
	}
	var paramName string
	var indexName string
	if len(arrow.Params) >= 1 {
		paramName = arrow.Params[0].Name
	}
	if len(arrow.Params) >= 2 {
		indexName = arrow.Params[1].Name
	}
	bodyExpr := arrowBodyExpr(arrow)
	if bodyExpr == nil {
		return nil
	}
	if paramName == "" {
		return nil
	}

	var items []*ListItem
	for i, elem := range arr.Elements {
		key := itoa(i)
		concrete := substituteListParam(bodyExpr, paramName, elem)
		if indexName != "" {
			concrete = substituteListParam(concrete, indexName, &ast.Literal{Position: elem.Pos(), Kind: ast.NumberLit, Value: itoa(i)})
		}
		itemID := joinSlotIDKey(SlotID(listID), key)
		contents := b.buildSlotNodes(concrete, string(itemID))
		if len(contents) == 0 {
			contents = []SlotNode{&StaticHTML{HTML: ""}}
		}
		items = append(items, &ListItem{
			Key:      key,
			Contents: contents,
		})
	}
	return items
}

// substituteListParam returns a deep clone of body with every reference to the
// loop parameter replaced by the element's AST: a bare `item` identifier is
// replaced by the element literal, and member reads like `item.title` are
// replaced by the resolved property value (so text/attribute interpolation and
// conditional tests see real values, not object-source text).
func substituteListParam(body ast.Expr, param string, elem ast.Expr) ast.Expr {
	if body == nil {
		return nil
	}
	sub := &listParamSubstituter{param: param, elem: elem}
	return sub.sub(body)
}

type listParamSubstituter struct {
	param string
	elem  ast.Expr
}

func (s *listParamSubstituter) sub(expr ast.Expr) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if e.Name == s.param {
			return cloneExpr(s.elem)
		}
		return e
	case *ast.MemberExpr:
		obj := s.sub(e.Object)
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == s.param {
			if v := memberASTOf(s.elem, e.Property); v != nil {
				return cloneExpr(v)
			}
		}
		return &ast.MemberExpr{Position: e.Position, Object: obj, Property: e.Property, Computed: e.Computed, Optional: e.Optional}
	case *ast.CallExpr:
		return &ast.CallExpr{Position: e.Position, Callee: s.sub(e.Callee), Args: subExprList(s, e.Args)}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{Position: e.Position, Left: s.sub(e.Left), Op: e.Op, Right: s.sub(e.Right)}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Position: e.Position, Op: e.Op, Arg: s.sub(e.Arg), Postfix: e.Postfix}
	case *ast.ConditionalExpr:
		return &ast.ConditionalExpr{Position: e.Position, Test: s.sub(e.Test), Consequent: s.sub(e.Consequent), Alternate: s.sub(e.Alternate)}
	case *ast.TemplateExpr:
		return &ast.TemplateExpr{Position: e.Position, Parts: subExprList(s, e.Parts), Raw: e.Raw}
	case *ast.ArrayExpr:
		return &ast.ArrayExpr{Position: e.Position, Elements: subExprList(s, e.Elements)}
	case *ast.ObjectExpr:
		out := make([]*ast.ObjectProp, len(e.Properties))
		for i, p := range e.Properties {
			out[i] = &ast.ObjectProp{Key: p.Key, Value: s.sub(p.Value), Shorthand: p.Shorthand, Spread: p.Spread, Method: p.Method}
		}
		return &ast.ObjectExpr{Position: e.Position, Properties: out}
	case *ast.JSXElement:
		return substituteListParamJSX(e, s)
	case *ast.JSXFragment:
		return &ast.JSXFragment{Position: e.Position, Children: subJSXChildrenList(s, e.Children)}
	case *ast.ArrowFn:
		return e // map bodies are expressions; nested arrows capture their own scope
	case *ast.TypeAssertion:
		return &ast.TypeAssertion{Position: e.Position, Expr: s.sub(e.Expr), TypeRef: e.TypeRef}
	default:
		return expr
	}
}

func subExprList(s *listParamSubstituter, list []ast.Expr) []ast.Expr {
	out := make([]ast.Expr, len(list))
	for i, e := range list {
		out[i] = s.sub(e)
	}
	return out
}

func subJSXChildrenList(s *listParamSubstituter, children []ast.JSXChild) []ast.JSXChild {
	out := make([]ast.JSXChild, 0, len(children))
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			out = append(out, &ast.JSXExprContainer{Expression: s.sub(c.Expression)})
		case *ast.JSXElementChild:
			out = append(out, &ast.JSXElementChild{Element: substituteListParamJSX(c.Element, s)})
		case *ast.JSXFragmentChild:
			out = append(out, &ast.JSXFragmentChild{Fragment: &ast.JSXFragment{Position: c.Fragment.Position, Children: subJSXChildrenList(s, c.Fragment.Children)}})
		default:
			out = append(out, child)
		}
	}
	return out
}

func substituteListParamJSX(el *ast.JSXElement, s *listParamSubstituter) *ast.JSXElement {
	opening := &ast.JSXOpening{Name: el.Opening.Name, SelfClosing: el.Opening.SelfClosing}
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil {
			opening.Attributes = append(opening.Attributes, attr)
			continue
		}
		opening.Attributes = append(opening.Attributes, &ast.JSXAttr{Position: attr.Position, Name: attr.Name, Value: s.sub(attr.Value)})
	}
	return &ast.JSXElement{
		Position: el.Position,
		Opening:  opening,
		Children: subJSXChildrenList(s, el.Children),
		Closing:  el.Closing,
	}
}

// cloneExpr returns a shallow structural clone of an AST expression so a
// substituted element can appear in multiple items without aliasing.
func cloneExpr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.Literal:
		return &ast.Literal{Position: e.Position, Kind: e.Kind, Value: e.Value}
	case *ast.ObjectExpr:
		out := make([]*ast.ObjectProp, len(e.Properties))
		for i, p := range e.Properties {
			out[i] = &ast.ObjectProp{Key: p.Key, Value: cloneExpr(p.Value), Shorthand: p.Shorthand, Spread: p.Spread, Method: p.Method}
		}
		return &ast.ObjectExpr{Position: e.Position, Properties: out}
	case *ast.ArrayExpr:
		out := make([]ast.Expr, len(e.Elements))
		for i, el := range e.Elements {
			out[i] = cloneExpr(el)
		}
		return &ast.ArrayExpr{Position: e.Position, Elements: out}
	case *ast.MemberExpr:
		return &ast.MemberExpr{Position: e.Position, Object: cloneExpr(e.Object), Property: e.Property, Computed: e.Computed, Optional: e.Optional}
	default:
		return expr
	}
}

// memberASTOf returns the AST of a property on a binding AST (object), so a
// chain like item.group.title can keep folding member reads during list-item
// substitution.
func memberASTOf(binding ast.Expr, prop ast.Expr) ast.Expr {
	var propName string
	if id, ok := prop.(*ast.Identifier); ok {
		propName = id.Name
	} else if lit, ok := prop.(*ast.Literal); ok {
		propName = lit.Value
	}
	if propName == "" {
		return nil
	}
	if src, ok := binding.(*ast.ObjectExpr); ok {
		for _, p := range src.Properties {
			if p.Spread {
				continue
			}
			if p.Key == propName {
				return p.Value
			}
		}
	}
	return nil
}

// parseExprAsProgram parses a JavaScript expression source string into a
// Program whose body is a single expression statement. Used to turn a
// build-time-resolved constant (e.g. an array literal rendered to JS source)
// back into an AST so list slots can SSR their items.
func parseExprAsProgram(source string) *ast.Program {
	tokens := lexer.New(source).Tokenize()
	prog := parser.New(tokens).ParseProgram()
	return prog
}

// collectListComponents walks a .map() expression and collects the uppercase
// component names referenced in the map body's JSX. These components must be
// available in the hydration scope so the runtime can re-render the list via
// h(ComponentName, props) when the underlying signal changes.
func collectListComponents(expr ast.Expr) []string {
	var names []string
	seen := make(map[string]bool)
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		if e == nil {
			return
		}
		switch v := e.(type) {
		case *ast.CallExpr:
			walk(v.Callee)
			for _, a := range v.Args {
				walk(a)
			}
		case *ast.MemberExpr:
			walk(v.Object)
			walk(v.Property)
		case *ast.ArrowFn:
			for _, p := range v.Params {
				walk(&ast.Identifier{Name: p.Name})
			}
			for _, stmt := range v.Body {
				if ret, ok := stmt.(*ast.ReturnStmt); ok {
					walk(ret.Value)
				}
				if es, ok := stmt.(*ast.ExprStmt); ok {
					walk(es.Expression)
				}
			}
		case *ast.JSXElement:
			if len(v.Opening.Name) > 0 && v.Opening.Name[0] >= 'A' && v.Opening.Name[0] <= 'Z' {
				if !seen[v.Opening.Name] {
					seen[v.Opening.Name] = true
					names = append(names, v.Opening.Name)
				}
			}
			for _, child := range v.Children {
				if el, ok := child.(*ast.JSXElementChild); ok {
					walk(el.Element)
				}
			}
		case *ast.JSXFragment:
			for _, child := range v.Children {
				if el, ok := child.(*ast.JSXElementChild); ok {
					walk(el.Element)
				}
			}
		}
	}
	walk(expr)
	return names
}

// tryResolveItems attempts to evaluate a .map() call on a literal array at build time.
func (b *builder) tryResolveItems(expr ast.Expr, listID string) []*ListItem {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok {
		return nil
	}
	prop, ok := mem.Property.(*ast.Identifier)
	if !ok || prop.Name != "map" || len(call.Args) != 1 {
		return nil
	}
	arr, ok := mem.Object.(*ast.ArrayExpr)
	if !ok {
		return nil
	}
	return b.tryResolveArrayItems(arr, expr, listID)
}

// buildFragmentSlots - flatten fragments

func (b *builder) buildFragmentSlots(frag *ast.JSXFragment, parentID string) []SlotNode {
	var result []SlotNode
	for _, child := range frag.Children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			result = append(result, b.buildSlotNodes(c.Element, parentID)...)
		case *ast.JSXFragmentChild:
			result = append(result, b.buildFragmentSlots(c.Fragment, parentID)...)
		case *ast.JSXExprContainer:
			result = append(result, b.buildExprContainerChildren(c, parentID)...)
		case *ast.JSXText:
			if c.Value != "" {
				result = append(result, &StaticHTML{HTML: c.Value})
			}
		}
	}
	return result
}

// buildHandlerDecl - extract event handler

func (b *builder) buildHandlerDecl(attr *ast.JSXAttr, elementID string) *HandlerDecl {
	eventName, capture, direct := reactEventName(attr.Name)
	body := b.extractHandlerBody(attr.Value)
	if body == "" {
		return nil
	}
	signals := b.collectSignalReads(attr.Value)

	return &HandlerDecl{
		ElementSlotID: SlotID(elementID),
		Event:         eventName,
		Body:          body,
		Signals:       signals,
		Capture:       capture,
		Direct:        direct,
	}
}

// buildAttrBinding - extract dynamic attribute

func (b *builder) buildAttrBinding(attr *ast.JSXAttr, elementID string) *AttrBinding {
	// Fully static values need no hydration binding - emit them statically
	// so the bundle doesn't re-evaluate loop vars (e.g. String(i)) at runtime.
	if b.isStaticResolvable(attr.Value) {
		return nil
	}
	signalName := b.extractSignalName(attr.Value)
	exprSource := ""
	if signalName == "" {
		exprSource = generateExprJS(attr.Value, b.sigMap())
	}

	return &AttrBinding{
		ElementSlotID: SlotID(elementID),
		// Store the HTML attribute name so hydration targets the same attribute
		// the SSR output uses (e.g. className -> class, htmlFor -> for, SVG
		// kebab-case aliases), instead of `setAttribute("className", ...)`.
		AttrName:    ast.HTMLAttrName(attr.Name),
		SignalName:  signalName,
		ExprSource:  exprSource,
		Initial:     b.evalAttrValue(attr.Value),
		InitialExpr: attr.Value,
		IsString:    isStringType(attr.Value, b.sigMap()),
	}
}

// buildStaticValueSlot - expression with no signals

func (b *builder) buildStaticValueSlot(expr ast.Expr, parentID string) *StaticHTML {
	val := evalConstWithSignals(expr, b.sigMap(), b.localProps)
	// An expression that resolves to undefined/null renders nothing (React
	// semantics) rather than the literal text "undefined". A string literal
	// "null"/"undefined" is real text and is kept.
	if !isStringLiteral(expr) && (val == "undefined" || val == "null") {
		return &StaticHTML{HTML: ""}
	}
	return &StaticHTML{HTML: val}
}

// buildStaticTextSlot is buildStaticValueSlot for JSX text positions: the
// statically-evaluated result is HTML-escaped so a template literal or string
// expression cannot inject real markup (e.g. <Code>{`return <h1>x</h1>`}</Code>).
func (b *builder) buildStaticTextSlot(expr ast.Expr, parentID string) *StaticHTML {
	val := evalConstWithSignals(expr, b.sigMap(), b.localProps)
	if !isStringLiteral(expr) && (val == "undefined" || val == "null") {
		return &StaticHTML{HTML: ""}
	}
	return &StaticHTML{HTML: escape.HTML(val)}
}

// Signal resolution helpers

func (b *builder) resolveSignal(name string) SignalDecl {
	if initial, ok := b.sigMap()[name]; ok {
		return SignalDecl{
			Name:        name,
			SetterName:  "set" + strings.ToUpper(name[:1]) + name[1:],
			Initial:     evalConst(initial),
			IsString:    isStringType(initial, b.sigMap()),
			InitialExpr: initial,
		}
	}
	return SignalDecl{Name: name}
}

func (b *builder) referencesSignal(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		_, ok := b.sigMap()[e.Name]
		return ok
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if _, ok := b.sigMap()[id.Name]; ok {
				return true
			}
		}
		if b.referencesSignal(e.Callee) {
			return true
		}
		for _, a := range e.Args {
			if b.referencesSignal(a) {
				return true
			}
		}
		return false
	case *ast.MemberExpr:
		return b.referencesSignal(e.Object)
	case *ast.BinaryExpr:
		return b.referencesSignal(e.Left) || b.referencesSignal(e.Right)
	case *ast.ConditionalExpr:
		return b.referencesSignal(e.Test) || b.referencesSignal(e.Consequent) || b.referencesSignal(e.Alternate)
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			if b.referencesSignal(el) {
				return true
			}
		}
		return false
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if b.referencesSignal(p.Value) {
				return true
			}
		}
		return false
	case *ast.ArrowFn:
		// Arrow bodies execute immediately in some positions (e.g. inside
		// createMemo(() => count() * 2) or createEffect), so a signal read
		// inside the thunk matters for dependency ordering.
		for _, s := range e.Body {
			if stmtReferenceSignal(s, b) {
				return true
			}
		}
		return false
	case *ast.NewExpr:
		if b.referencesSignal(e.Callee) {
			return true
		}
		for _, a := range e.Args {
			if b.referencesSignal(a) {
				return true
			}
		}
		return false
	case *ast.UnaryExpr:
		return b.referencesSignal(e.Arg)
	case *ast.TemplateExpr:
		for _, part := range e.Parts {
			if b.referencesSignal(part) {
				return true
			}
		}
		return false
	case *ast.JSXElement:
		for _, child := range e.Children {
			switch c := child.(type) {
			case *ast.JSXExprContainer:
				if b.referencesSignal(c.Expression) {
					return true
				}
			}
		}
		return false
	case *ast.TypeAssertion:
		return b.referencesSignal(e.Expr)
	}
	return false
}

func isSimpleSignalRead(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Identifier:
		return len(e.Name) > 0 && e.Name[0] >= 'a' && e.Name[0] <= 'z'
	case *ast.CallExpr:
		// count() - call to a signal getter function
		if id, ok := e.Callee.(*ast.Identifier); ok {
			return len(id.Name) > 0 && id.Name[0] >= 'a' && id.Name[0] <= 'z'
		}
	}
	return false
}

func isTernaryWithJSX(expr ast.Expr) bool {
	cond, ok := expr.(*ast.ConditionalExpr)
	if !ok {
		return false
	}
	return hasJSXInExpr(cond.Consequent) || hasJSXInExpr(cond.Alternate)
}

func hasJSXInExpr(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.JSXElement, *ast.JSXFragment:
		return true
	case *ast.BinaryExpr:
		return hasJSXInExpr(e.Left) || hasJSXInExpr(e.Right)
	case *ast.ConditionalExpr:
		return hasJSXInExpr(e.Test) || hasJSXInExpr(e.Consequent) || hasJSXInExpr(e.Alternate)
	case *ast.UnaryExpr:
		return hasJSXInExpr(e.Arg)
	case *ast.TypeAssertion:
		return hasJSXInExpr(e.Expr)
	}
	return false
}

func isMapCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok {
		return false
	}
	prop, ok := mem.Property.(*ast.Identifier)
	return ok && prop.Name == "map" && len(call.Args) == 1
}

// extractSignalName returns the signal getter name when expr is exactly a
// signal read - a bare signal identifier or a call to a signal getter. It
// consults the signal map so arbitrary calls (e.g. cn(...)) are not mistaken
// for getters: a call whose callee is not a known signal is not a signal read.
func (b *builder) extractSignalName(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	sigMap := b.sigMap()
	switch e := expr.(type) {
	case *ast.Identifier:
		if _, ok := sigMap[e.Name]; ok {
			return e.Name
		}
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if _, ok := sigMap[id.Name]; ok && len(e.Args) == 0 {
				return id.Name
			}
		}
	}
	return ""
}

// collectSignalDecls from function body

func (b *builder) collectSignalDecls(fn *ast.FnDecl) []SignalDecl {
	if fn == nil {
		return nil
	}
	// CSS primitive declarations are always compiler-erased: the state lives in
	// hidden inputs + `:has()` CSS, so no signal is emitted. There is no
	// fallback; an un-compilable declaration is a build error (collected in
	// buildComponentNode), and its getter/setter are never referenced because
	// the component fails to build.
	var decls []SignalDecl
	for _, d := range sigutil.Find(fn.Body, false) {
		if d.IsResource || d.Initial == nil || d.CSSKind != sigutil.CSSKindNone {
			continue
		}
		initial := evalConstWithSignals(d.Initial, b.sigMap(), b.localProps)
		// Preserve explicit null/undefined initial values: evalConstWithSignals
		// collapses NullLit to "" (an SSR "no value" sentinel), which would emit
		// createSignal() (undefined) instead of createSignal(null).
		if initial == "" {
			if lit, ok := d.Initial.(*ast.Literal); ok && lit.Kind == ast.NullLit {
				initial = lit.Value
			}
		}
		isStr := isStringType(d.Initial, b.sigMap())
		rawInit := signalRawInit(d.Initial, initial, b.sigMap(), b.localProps)
		// Array/object initializers must be emitted as real JS literals (their
		// folded value is JS source, not a string), so keep the source verbatim.
		if rawInit == "" {
			switch d.Initial.(type) {
			case *ast.ArrayExpr, *ast.ObjectExpr:
				rawInit = generateExprJS(d.Initial, b.sigMap())
			}
		}
		decls = append(decls, SignalDecl{
			Name:        d.Name,
			SetterName:  d.Setter,
			Initial:     initial,
			IsString:    isStr,
			InitialExpr: d.Initial,
			RawInit:     rawInit,
			FactoryJS:   b.reducerFactoryJS(d),
			OptionsJS:   signalOptionsJS(d, b.sigMap()),
		})
	}
	return decls
}

// reducerFactoryJS renders the hydration `createReducer(reducer, initial)` call
// for a createReducer declaration. Returns "" for ordinary signals so the normal
// createSignal emission path applies.
func (b *builder) reducerFactoryJS(d sigutil.Decl) string {
	if d.Factory != "createReducer" || len(d.Args) < 2 {
		return ""
	}
	reducer := generateExprJS(d.Args[0], b.sigMap())
	if reducer == "" {
		return ""
	}
	initial := generateExprJS(d.Args[1], b.sigMap())
	if initial == "" {
		initial = "undefined"
	}
	return "createReducer(" + reducer + "," + initial + ")"
}

// signalOptionsJS renders the options argument of `createSignal(value, opts)`
// (e.g. `{ persist: "key" }`) so the client runtime can persist the signal.
// Returns "" when there is no options argument.
func signalOptionsJS(d sigutil.Decl, signals map[string]ast.Expr) string {
	if d.Factory != "createSignal" || len(d.Args) < 2 {
		return ""
	}
	return generateExprJS(d.Args[1], signals)
}

// signalRawInit decides whether a signal initializer must be emitted verbatim
// rather than const-folded. evalConstWithSignals returns "" for non-constant
// expressions (calls like Math.random(), Date.now(), or unknown identifiers).
// Dropping those would hydrate the signal to undefined even though the client
// should evaluate the real expression. This returns the JS source of the
// initializer when it's a genuine runtime expression we can't fold, and ""
// otherwise (so newhydrate emits the folded literal).
func signalRawInit(expr ast.Expr, folded string, signals map[string]ast.Expr, props map[string]string) string {
	if expr == nil {
		return ""
	}
	// If the expression folded to a usable literal, prefer it.
	if folded != "" {
		return ""
	}
	// A literal that folded to "" is a null/empty sentinel - keep that behavior.
	if lit, ok := expr.(*ast.Literal); ok {
		_ = lit
		return ""
	}
	// Signals resolve through the signals map (folded), props too. A bare
	// identifier that isn't resolvable would reference an undefined global at
	// runtime - leave it dropped rather than emit a broken reference.
	if id, ok := expr.(*ast.Identifier); ok {
		_ = id
		return ""
	}
	// Resource getters and member access on resources are SSR-sentinels.
	if isResourceSentinelExpr(expr, signals) {
		return ""
	}
	// Remaining expressions (calls, member access on non-resources, binary
	// expressions with unknowns, etc.) are real runtime values; emit them so
	// the client evaluates them at hydration time.
	return generateExprJS(expr, signals)
}

// isResourceSentinelExpr reports whether an expression is a resource access or
// resource getter call whose value is intentionally unresolved during SSR.
func isResourceSentinelExpr(expr ast.Expr, signals map[string]ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if initial, ok := signals[id.Name]; ok && isResourceSentinel(initial) {
				return true
			}
		}
		return false
	case *ast.Identifier:
		if e.Name == "__krate_resource__" {
			return true
		}
		if initial, ok := signals[e.Name]; ok {
			return isResourceSentinel(initial)
		}
		return false
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok {
			if initial, ok := signals[id.Name]; ok && isResourceSentinel(initial) {
				return true
			}
		}
		return false
	}
	return false
}

// collectFunctionRefVars returns the local names that hold functions: named
// function declarations and variables initialized to an arrow/function
// expression. A `ref={name}` where name is in this set is a callback ref (the
// function receives the mounted node); any other identifier ref is an object or
// bare-variable assignment target.
func collectFunctionRefVars(body []ast.Stmt) map[string]bool {
	fns := make(map[string]bool)
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if s.Name != "" {
				fns[s.Name] = true
			}
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if decl == nil || decl.Name == "" || decl.Init == nil {
					continue
				}
				switch decl.Init.(type) {
				case *ast.ArrowFn:
					fns[decl.Name] = true
				}
			}
		}
	}
	return fns
}

// collectRefObjectVars returns the set of local variable names bound to a
// useRef object - either a `{current: ...}` object literal (the React-compat
// rewrite of useRef()) or a `useRef(...)` call (native @krate/runtime). A
// `ref={myRef}` binding on such a variable must assign `.current` rather than
// reassign the variable itself.
func collectRefObjectVars(body []ast.Stmt) map[string]bool {
	refs := make(map[string]bool)
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl == nil || decl.Name == "" || decl.Init == nil {
				continue
			}
			switch init := decl.Init.(type) {
			case *ast.ObjectExpr:
				for _, prop := range init.Properties {
					if prop != nil && !prop.Spread && prop.Key == "current" {
						refs[decl.Name] = true
						break
					}
				}
			case *ast.CallExpr:
				if id, ok := init.Callee.(*ast.Identifier); ok && id.Name == "useRef" {
					refs[decl.Name] = true
				}
			}
		}
	}
	return refs
}

// collectBodySignalUses returns every signal that is read (signalName()) or
// written (setSignalName(...)) anywhere in a component function body - inside
// named helper functions, control flow, early returns, and nested JSX. These
// feeds the reactive validator so signals used only in named helpers or
// early-return branches aren't reported as "declared but never used".
func (b *builder) collectBodySignalUses(body []ast.Stmt, decls []SignalDecl) []string {
	setterToSignal := make(map[string]string)
	for _, d := range decls {
		if d.SetterName != "" {
			setterToSignal[d.SetterName] = d.Name
		}
	}

	var uses []string
	seen := make(map[string]bool)
	mark := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			uses = append(uses, name)
		}
	}

	var walkStmt func([]ast.Stmt)
	var walkExpr func(ast.Expr)
	var walkJSXChild func(ast.JSXChild)

	walkExpr = func(e ast.Expr) {
		if e == nil {
			return
		}
		switch v := e.(type) {
		case *ast.CallExpr:
			if id, ok := v.Callee.(*ast.Identifier); ok {
				if _, isSig := b.sigMap()[id.Name]; isSig {
					mark(id.Name) // signalName() read
				} else if sigName := setterToSignal[id.Name]; sigName != "" {
					mark(sigName) // setSignalName() write
				}
			}
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.MemberExpr:
			walkExpr(v.Object)
			walkExpr(v.Property)
		case *ast.BinaryExpr:
			walkExpr(v.Left)
			walkExpr(v.Right)
		case *ast.UnaryExpr:
			walkExpr(v.Arg)
		case *ast.ConditionalExpr:
			walkExpr(v.Test)
			walkExpr(v.Consequent)
			walkExpr(v.Alternate)
		case *ast.TemplateExpr:
			for _, p := range v.Parts {
				walkExpr(p)
			}
		case *ast.ArrowFn:
			walkStmt(v.Body)
		case *ast.AwaitExpr:
			walkExpr(v.Arg)
		case *ast.DynamicImport:
			walkExpr(v.Arg)
		case *ast.ImportMetaExpr:
		case *ast.NewExpr:
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.JSXElement:
			if v.Opening != nil {
				for _, attr := range v.Opening.Attributes {
					if attr.Value != nil {
						walkExpr(attr.Value)
					}
				}
			}
			for _, c := range v.Children {
				walkJSXChild(c)
			}
		case *ast.JSXFragment:
			for _, c := range v.Children {
				walkJSXChild(c)
			}
		}
	}
	walkJSXChild = func(c ast.JSXChild) {
		switch ch := c.(type) {
		case *ast.JSXExprContainer:
			walkExpr(ch.Expression)
		case *ast.JSXElementChild:
			walkExpr(ch.Element)
		case *ast.JSXFragmentChild:
			walkExpr(ch.Fragment)
		}
	}
	walkStmt = func(stmts []ast.Stmt) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ExprStmt:
				walkExpr(s.Expression)
			case *ast.VarStmt:
				for _, d := range s.Decls {
					if d.Init != nil {
						walkExpr(d.Init)
					}
				}
			case *ast.ReturnStmt:
				if s.Value != nil {
					walkExpr(s.Value)
				}
			case *ast.BlockStmt:
				walkStmt(s.Body)
			case *ast.IfStmt:
				walkExpr(s.Test)
				walkStmt(s.Consequent)
				walkStmt(s.Alternate)
			case *ast.ForStmt:
				if s.Init != nil {
					walkStmt([]ast.Stmt{s.Init})
				}
				if s.Test != nil {
					walkExpr(s.Test)
				}
				walkStmt(s.Body)
			case *ast.WhileStmt:
				walkExpr(s.Test)
				walkStmt(s.Body)
			case *ast.DoWhileStmt:
				walkStmt(s.Body)
				walkExpr(s.Test)
			case *ast.SwitchStmt:
				walkExpr(s.Discriminant)
				for _, c := range s.Cases {
					if c.Test != nil {
						walkExpr(c.Test)
					}
					walkStmt(c.Body)
				}
			case *ast.TryStmt:
				walkStmt(s.Body)
				if s.Catch != nil {
					walkStmt(s.Catch.Body)
				}
				walkStmt(s.Finally)
			case *ast.ThrowStmt:
				walkExpr(s.Value)
			case *ast.FnDecl:
				walkStmt(s.Body)
			}
		}
	}
	walkStmt(body)
	return uses
}

// collectEffectJS from function body

// collectEffectJS walks the component body for createEffect / onMount calls and
// returns their rendered JS. It recurses into control-flow bodies (if/loops/
// switch/try/blocks) so effects nested inside conditionals are still emitted -
// a top-level-only walk silently drops them, leaving the callback to never run
// during hydration.
func (b *builder) collectEffectJS(body []ast.Stmt) []string {
	var effects []string
	var walk func(stmts []ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ExprStmt:
				if call, ok := s.Expression.(*ast.CallExpr); ok {
					if id, ok := call.Callee.(*ast.Identifier); ok {
						switch id.Name {
						case "createEffect":
							if len(call.Args) >= 1 {
								js := generateExprJS(call.Args[0], b.sigMap())
								effects = append(effects, "createEffect("+js+")")
							}
						case "onMount":
							if len(call.Args) >= 1 {
								js := generateExprJS(call.Args[0], b.sigMap())
								effects = append(effects, "onMount("+js+")")
							}
						case "onCleanup":
							// Top-level onCleanup registers a root-scoped cleanup
							// that runs on unmount (disposeAll at route change).
							if len(call.Args) >= 1 {
								js := generateExprJS(call.Args[0], b.sigMap())
								effects = append(effects, "onCleanup("+js+")")
							}
						}
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
			case *ast.SwitchStmt:
				for _, c := range s.Cases {
					walk(c.Body)
				}
			case *ast.TryStmt:
				walk(s.Body)
				if s.Catch != nil {
					walk(s.Catch.Body)
				}
				walk(s.Finally)
			}
		}
	}
	walk(body)
	return effects
}

// collectResourceJS from function body

// collectResourceJS walks the component body for createResource declarations
// (const [user, actions] = createResource(...)) and returns their full
// declaration statements. These are emitted as extra vars so the resource
// getter and actions object are in scope at hydration time - slot bindings and
// handlers reference user()/user.loading/actions.refetch() and would otherwise
// throw ReferenceError because createResource was previously dropped entirely.
func (b *builder) collectResourceJS(body []ast.Stmt) []string {
	var out []string
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Init == nil {
				continue
			}
			call, ok := decl.Init.(*ast.CallExpr)
			if !ok {
				continue
			}
			id, ok := call.Callee.(*ast.Identifier)
			if !ok || id.Name != "createResource" {
				continue
			}
			out = append(out, renderStmtJS(&ast.VarStmt{Kind: vs.Kind, Decls: []*ast.VarDecl{decl}}, b.sigMap()))
		}
	}
	return out
}

// collectResourceNames returns the getter names of createResource declarations
// (the first destructured name, e.g. `user` in const [user, actions] = ...).
func (b *builder) collectResourceNames(body []ast.Stmt) []string {
	var out []string
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.IsDestructuring && len(decl.Names) >= 1 && decl.Init != nil {
				if call, ok := decl.Init.(*ast.CallExpr); ok {
					if id, ok := call.Callee.(*ast.Identifier); ok && id.Name == "createResource" {
						out = append(out, decl.Names[0])
					}
				}
			}
		}
	}
	return out
}

// collectMemoJS from function body

// collectNamedMemos walks the component body for non-destructuring
// `const <name> = createMemo(<arrowFn>)` declarations and returns a map of
// memo getter name -> arrow function. These are registered as local signals
// so JSX reads like {doubled()} become reactive text bindings (() => doubled())
// with a computed SSR initial value, and the declaration itself is emitted as
// an extra var so the getter is in scope at hydration time.
func (b *builder) collectNamedMemos(body []ast.Stmt) map[string]*ast.ArrowFn {
	memos := make(map[string]*ast.ArrowFn)
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.IsDestructuring || decl.Init == nil {
				continue
			}
			call, ok := decl.Init.(*ast.CallExpr)
			if !ok {
				continue
			}
			id, ok := call.Callee.(*ast.Identifier)
			if !ok || id.Name != "createMemo" {
				continue
			}
			if len(call.Args) < 1 {
				continue
			}
			if arrow, ok := call.Args[0].(*ast.ArrowFn); ok {
				memos[decl.Name] = arrow
			}
		}
	}
	return memos
}

func (b *builder) collectMemoJS(body []ast.Stmt) []string {
	var memos []string
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if decl.IsDestructuring && decl.Init != nil {
					if call, ok := decl.Init.(*ast.CallExpr); ok {
						if id, ok := call.Callee.(*ast.Identifier); ok && id.Name == "createMemo" {
							if len(call.Args) >= 1 {
								js := generateExprJS(call.Args[0], b.sigMap())
								memos = append(memos, "createMemo("+js+")")
							}
						}
					}
				}
			}
		case *ast.ExprStmt:
			if call, ok := s.Expression.(*ast.CallExpr); ok {
				if id, ok := call.Callee.(*ast.Identifier); ok && id.Name == "createMemo" {
					if len(call.Args) >= 1 {
						js := generateExprJS(call.Args[0], b.sigMap())
						memos = append(memos, "createMemo("+js+")")
					}
				}
			}
		}
	}
	return memos
}

// collectExtraVarJS from function body

// collectExtraVarJS walks top-level value declarations and returns them split
// into two groups: pre (declarations that reference NO signal getters, safe to
// emit before signal declarations) and post (declarations that read signals,
// which must come after the signal decls they depend on). Signal initializers
// like createSignal(initial.value) evaluate local values at hydration time, so
// a plain local object placed in `pre` is declared first - matching source
// order.
func (b *builder) collectExtraVarJS(body []ast.Stmt) (pre, post []string) {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if decl.Init != nil {
					// useId() is resolved to a per-instance literal by the
					// caller (collectLocalVars) and emitted from there, so it is
					// skipped here to avoid emitting the raw marker.
					if isUseIdCall(decl.Init) {
						continue
					}
					// A context read folds to its default value (SSR) and is
					// emitted as a folded local, so skip the raw call here -
					// emitting `var x=Ctx.useContext()` would reference an
					// undefined context object at hydration time.
					if isContextRead(decl.Init) {
						continue
					}
					if call, ok := decl.Init.(*ast.CallExpr); ok {
						if id, ok := call.Callee.(*ast.Identifier); ok {
							// createMemo stays an extra var so the named getter
							// (const doubled = createMemo(...)) is declared with
							// its real name, letting slot bindings reference it
							// as () => doubled(). Signals/resources/effects are
							// handled by their own collection passes.
							switch id.Name {
							case "createSignal", "createResource", "createEffect":
								continue
							}
						}
					}
					if !decl.IsDestructuring && !referencesProps(decl.Init) {
						js := generateExprJS(decl.Init, b.sigMap())
						if b.referencesSignal(decl.Init) {
							post = append(post, "var "+decl.Name+"="+js)
						} else {
							pre = append(pre, "var "+decl.Name+"="+js)
						}
					}
				}
			}
		case *ast.BlockStmt:
			p, po := b.collectExtraVarJS(s.Body)
			pre = append(pre, p...)
			post = append(post, po...)
		case *ast.IfStmt:
			// Render early-return guards (`if (!items || items.length === 0)
			// return <span/>`) are a render-time decision already captured by the
			// SSR slot output. Re-emitting them at hydration as a top-level
			// statement that can `return` would abort the component IIFE BEFORE
			// its refs/effects register (the guard also runs before any local
			// `var items = props.items` declaration it reads, because those
			// locals are appended to ExtraVars afterwards). Skip them here.
			if ifEndsInReturn(s) {
				continue
			}
			if stmtsReferenceSignal(s.Consequent, b) || stmtsReferenceSignal(s.Alternate, b) {
				post = append(post, renderStmtJS(s, b.sigMap()))
			} else {
				pre = append(pre, renderStmtJS(s, b.sigMap()))
			}
		case *ast.ForStmt:
			renderLoopStmt(&pre, &post, s, b)
		case *ast.ForInStmt:
			renderLoopStmt(&pre, &post, s, b)
		case *ast.WhileStmt:
			renderLoopStmt(&pre, &post, s, b)
		case *ast.DoWhileStmt:
			renderLoopStmt(&pre, &post, s, b)
		}
	}
	return pre, post
}

// ifEndsInReturn reports whether the if statement is a render early-return
// guard: any of its branches (directly or inside a wrapping block) contains a
// `return` statement.
func ifEndsInReturn(s *ast.IfStmt) bool {
	return stmtsContainReturn(s.Consequent) || stmtsContainReturn(s.Alternate)
}

func stmtsContainReturn(stmts []ast.Stmt) bool {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			return true
		case *ast.BlockStmt:
			if stmtsContainReturn(s.Body) {
				return true
			}
		case *ast.IfStmt:
			if ifEndsInReturn(s) {
				return true
			}
		}
	}
	return false
}

func renderLoopStmt(pre, post *[]string, s ast.Stmt, b *builder) {
	js := renderStmtJS(s, b.sigMap())
	if stmtsReferenceSignal([]ast.Stmt{s}, b) {
		*post = append(*post, js)
	} else {
		*pre = append(*pre, js)
	}
}

// stmtsReferenceSignal reports whether any statement in the list reads a
// signal getter, either directly or nested in blocks/control flow.
func stmtsReferenceSignal(stmts []ast.Stmt, b *builder) bool {
	for _, stmt := range stmts {
		if stmtReferenceSignal(stmt, b) {
			return true
		}
	}
	return false
}

func stmtReferenceSignal(stmt ast.Stmt, b *builder) bool {
	switch s := stmt.(type) {
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if d.Init != nil && b.referencesSignal(d.Init) {
				return true
			}
		}
	case *ast.BlockStmt:
		if stmtsReferenceSignal(s.Body, b) {
			return true
		}
	case *ast.IfStmt:
		if b.referencesSignal(s.Test) || stmtsReferenceSignal(s.Consequent, b) || stmtsReferenceSignal(s.Alternate, b) {
			return true
		}
	case *ast.ForStmt:
		if s.Init != nil && stmtReferenceSignal(s.Init, b) {
			return true
		}
		if b.referencesSignal(s.Test) || b.referencesSignal(s.Update) || stmtsReferenceSignal(s.Body, b) {
			return true
		}
	case *ast.ForInStmt:
		if b.referencesSignal(s.Right) || stmtsReferenceSignal(s.Body, b) {
			return true
		}
	case *ast.WhileStmt:
		if b.referencesSignal(s.Test) || stmtsReferenceSignal(s.Body, b) {
			return true
		}
	case *ast.DoWhileStmt:
		if b.referencesSignal(s.Test) || stmtsReferenceSignal(s.Body, b) {
			return true
		}
	case *ast.SwitchStmt:
		if b.referencesSignal(s.Discriminant) {
			return true
		}
		for _, c := range s.Cases {
			if stmtsReferenceSignal(c.Body, b) {
				return true
			}
		}
	case *ast.TryStmt:
		if stmtsReferenceSignal(s.Body, b) || stmtsReferenceSignal(s.Finally, b) {
			return true
		}
		if s.Catch != nil && stmtsReferenceSignal(s.Catch.Body, b) {
			return true
		}
	case *ast.ExprStmt:
		if b.referencesSignal(s.Expression) {
			return true
		}
	case *ast.ReturnStmt:
		if b.referencesSignal(s.Value) {
			return true
		}
	case *ast.ThrowStmt:
		if b.referencesSignal(s.Value) {
			return true
		}
	}
	return false
}

// collectSignalReads from an expression

func (b *builder) collectSignalReads(expr ast.Expr) []string {
	var reads []string
	seen := make(map[string]bool)
	var walkJSXChild func(child ast.JSXChild)
	var walkJSXAttr func(attr *ast.JSXAttr)
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		if e == nil {
			return
		}
		switch v := e.(type) {
		case *ast.Identifier:
			if _, ok := b.sigMap()[v.Name]; ok && !seen[v.Name] {
				seen[v.Name] = true
				reads = append(reads, v.Name)
			}
		case *ast.CallExpr:
			walk(v.Callee)
			for _, a := range v.Args {
				walk(a)
			}
		case *ast.MemberExpr:
			walk(v.Object)
		case *ast.BinaryExpr:
			walk(v.Left)
			walk(v.Right)
		case *ast.ConditionalExpr:
			walk(v.Test)
			walk(v.Consequent)
			walk(v.Alternate)
		case *ast.UnaryExpr:
			walk(v.Arg)
		case *ast.TemplateExpr:
			for _, p := range v.Parts {
				walk(p)
			}
		case *ast.JSXElement:
			if v.Opening != nil {
				for _, attr := range v.Opening.Attributes {
					walkJSXAttr(attr)
				}
			}
			for _, child := range v.Children {
				walkJSXChild(child)
			}
		case *ast.JSXFragment:
			for _, child := range v.Children {
				walkJSXChild(child)
			}
		case *ast.ArrowFn:
			for _, stmt := range v.Body {
				if ret, ok := stmt.(*ast.ReturnStmt); ok {
					walk(ret.Value)
				}
			}
		}
	}
	walkJSXChild = func(child ast.JSXChild) {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			walk(c.Expression)
		case *ast.JSXElementChild:
			walk(c.Element)
		case *ast.JSXFragmentChild:
			walk(c.Fragment)
		}
	}
	walkJSXAttr = func(attr *ast.JSXAttr) {
		if attr != nil && attr.Value != nil {
			walk(attr.Value)
		}
	}
	walk(expr)
	return reads
}

// extractHandlerBody

func (b *builder) extractHandlerBody(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.ArrowFn:
		return renderArrowFn(e, b.sigMap())
	case *ast.Identifier:
		if b.localFnBody != nil {
			if fn := findLocalFunction(e.Name, b.localFnBody); fn != nil {
				return renderFnAsHandler(fn, b.sigMap())
			}
			if arrow := findLocalConstFn(e.Name, b.localFnBody); arrow != nil {
				return renderArrowFn(arrow, b.sigMap())
			}
		}

		if _, ok := b.functions[e.Name]; ok && !b.ann.UsedComponents[e.Name] {
			return e.Name
		}
		return ""
	case *ast.MemberExpr:
		// A function prop forwarded directly, e.g. onClick={props.onClick}.
		// Render it as a live reference resolved through the child's props
		// object (var props = __krate_props[...]) at hydration time.
		return generateExprJS(e, b.sigMap())
	default:
		return ""
	}
}

// findLocalFunction finds a function declaration by name in the given body.
// isFuncReference reports whether a call-site prop expression is a function
// reference: an inline arrow/function expression, an identifier bound to a
// local function declaration or const-arrow in the current component body, or
// an identifier that itself aliases a function prop of the current component.
// Function props must reach the child as live references (via __krate_props),
// never as const-folded name strings.
func (b *builder) isFuncReference(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.ArrowFn:
		return true
	case *ast.Identifier:
		if b.localFnBody != nil {
			if findLocalFunction(e.Name, b.localFnBody) != nil {
				return true
			}
			if findLocalConstFn(e.Name, b.localFnBody) != nil {
				return true
			}
		}
		if b.localFuncProps != nil && b.localFuncProps[e.Name] {
			return true
		}
	case *ast.MemberExpr:
		// props.<alias> where <alias> is a func prop in the current scope.
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
			if pid, ok := e.Property.(*ast.Identifier); ok && b.localFuncProps != nil && b.localFuncProps[pid.Name] {
				return true
			}
		}
	}
	return false
}

func findLocalFunction(name string, body []ast.Stmt) *ast.FnDecl {
	for _, stmt := range body {
		if fn, ok := stmt.(*ast.FnDecl); ok && fn.Name == name {
			return fn
		}
	}
	return nil
}

func findLocalConstFn(name string, body []ast.Stmt) *ast.ArrowFn {
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name == name && decl.Init != nil {
				if arrow, ok := decl.Init.(*ast.ArrowFn); ok {
					return arrow
				}
			}
		}
	}
	return nil
}

// renderFnAsHandler renders a function declaration as a handler function expression.
func renderFnAsHandler(fn *ast.FnDecl, signals map[string]ast.Expr) string {
	var b strings.Builder
	b.WriteString("function(")
	for i, p := range fn.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		if p.Pattern != "" {
			b.WriteString(p.Pattern)
		} else {
			b.WriteString(p.Name)
		}
	}
	b.WriteString("){")
	for _, stmt := range fn.Body {
		b.WriteString(renderStmtJS(stmt, signals))
	}
	b.WriteByte('}')
	return b.String()
}

// extractProps from JSX element

func extractProps(el *ast.JSXElement) map[string]ast.Expr {
	props := make(map[string]ast.Expr)
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || isShowIfAttr(attr.Name) || isReactDirectiveAttr(attr.Name) {
			continue
		}
		// A bare attribute (`<Child disabled />`) means boolean true.
		if attr.Value == nil {
			props[attr.Name] = &ast.Literal{Kind: ast.BoolLit, Value: "true"}
			continue
		}
		props[attr.Name] = attr.Value
	}
	return props
}
