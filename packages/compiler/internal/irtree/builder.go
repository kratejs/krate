package irtree

import (
	"fmt"
	"sort"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/csssignals"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

// resourceSentinel marks a localSignals entry as a createResource getter so
// evalConstWithSignals can resolve its status members (.loading/.state/.error)
// during SSR while the getter itself is still treated as reactive.
var resourceSentinel = &ast.Identifier{Name: "__krate_resource__"}

// isResourceSentinel reports whether expr is the resource getter marker.
func isResourceSentinel(expr ast.Expr) bool {
	if id, ok := expr.(*ast.Identifier); ok {
		return id.Name == "__krate_resource__"
	}
	return false
}

// contextMarkerPrefix prefixes a `createContext(default)` binding name in the
// localProps table so `Ctx.useContext()` folds to its default during SSG.
const contextMarkerPrefix = "__krate_context__"

// contextPropPrefix marks the synthetic prop that carries a dynamic context
// value across a component boundary.
const contextPropPrefix = "__krate_ctx_"

// BuildOptions configures IR construction.
type BuildOptions struct {
	// CodeTheme is the chroma theme used to highlight <Code>/<SyntaxHighlight>
	// at build time. It must match the stylesheet generated for the same theme
	// (syntaxhighlight.CSSForTheme). Empty selects the default theme.
	CodeTheme string
	// IDPrefix namespaces the compact slot IDs emitted into data-k markers and
	// hydration JS. It must be set when a build is merged with another build
	// (e.g. a layout merged into a page) so the two ID spaces can't collide.
	// Use a character outside the base62 alphabet (e.g. "_") so it can never
	// clash with an unprefixed ID.
	IDPrefix string
	// AggressiveDCE drops local helper functions the reference scan cannot
	// prove are used, instead of conservatively emitting every local function
	// declaration. It can shrink hydration bundles but risks dropping a
	// function referenced only from verbatim-emitted body fragments, so it is
	// opt-in (config `dce.aggressive`).
	AggressiveDCE bool
	// Compat selects how CSS-signal stylesheets are emitted (modern/legacy/
	// hybrid) for browser compatibility. The zero value is StyleModern.
	Compat csssignals.StyleMode
	// GoComponents maps imported local names to Go components (from `.go`
	// imports), so the builder lowers <Card/> into a GoComponentSlot.
	GoComponents map[string]GoComponentRef
	// ExternalImports maps a bare-package import binding to its module, so an
	// unresolved JSX component from a package is mounted at runtime.
	ExternalImports map[string]ExternalImport
	// LocalComponentImports lists relative-path import bindings. An external
	// island cannot render them, so the builder warns when its subtree
	// references one.
	LocalComponentImports map[string]bool
	// ModuleConsts and ModuleConstInits extend the entry module's module-level
	// constants with values collected from imported modules (including
	// `export { a as b }` aliases), so imported literals fold in page and
	// component expressions. Entry-module values take precedence on collision.
	ModuleConsts     map[string]string
	ModuleConstInits map[string]ast.Expr
	// ActionURLs maps an imported server-action binding to its POST endpoint.
	// The binding is a client proxy at runtime, but SSR renders the URL so
	// forms work without JavaScript.
	ActionURLs map[string]string
	// BoundaryThrows renders the listed error boundary instances in their
	// fallback state (see boundary_build.go).
	BoundaryThrows map[SlotID]BoundaryThrow
}

// Build constructs a ComponentTree from a parsed program and its annotations
// using default options.
func Build(prog *ast.Program, ann *Annotations) *ComponentTree {
	return BuildWithOptions(prog, ann, BuildOptions{})
}

// BuildWithOptions constructs a ComponentTree from a parsed program and its
// annotations using the given options.
func BuildWithOptions(prog *ast.Program, ann *Annotations, opts BuildOptions) *ComponentTree {
	entryFn := ann.Functions[ann.EntryPoint]
	if entryFn == nil {
		return &ComponentTree{
			Root:         &ComponentNode{Name: "_empty", Tier: TierStatic},
			RuntimeStore: NewRuntimePropStore(),
		}
	}

	builder := newBuilder(prog, ann, opts)
	root := builder.buildComponentNode(entryFn, "")
	root.SourceFile = ann.SourceFile
	return builder.finishTree(root)
}

// newBuilder prepares the build-global state for one IR construction.
func newBuilder(prog *ast.Program, ann *Annotations, opts BuildOptions) *builder {
	codeTheme := opts.CodeTheme
	if codeTheme == "" {
		codeTheme = syntaxhighlight.DefaultTheme
	}
	moduleConsts, moduleConstInits := mergedModuleConstants(prog, opts)
	b := &builder{
		functions:        ann.Functions,
		ann:              ann,
		idCounter:        make(map[string]int),
		instanceCounts:   make(map[string]int),
		elementCounts:    make(map[string]int),
		slotIDMap:        make(map[string]SlotID),
		slotCounts:       make(map[string]int),
		moduleConsts:     typedModuleConsts(moduleConstInits, moduleConsts),
		moduleConstInits: moduleConstInits,
		moduleStmts:      collectModuleStmts(prog),
		entryProg:        prog,
		cvaFactories:     mergeCVAFactories(ann.CVAFactories, prog),
		contextDefaults:  mergeContextDefaults(ann.ContextDefaults, prog),
		codeTheme:        codeTheme,
		idPrefix:         opts.IDPrefix,
		aggressiveDCE:    opts.AggressiveDCE,
		compat:           opts.Compat,
		goComponents:     opts.GoComponents,
		externalImports:  opts.ExternalImports,
		localImports:     opts.LocalComponentImports,
		actionURLs:       opts.ActionURLs,
		boundaryThrows:   opts.BoundaryThrows,
	}
	if entryFn := ann.Functions[ann.EntryPoint]; entryFn != nil {
		b.expansion = []expansionFrame{{name: entryFn.Name}}
	}
	return b
}

// finishTree validates the module's functions and assembles the tree around
// the built root.
func (b *builder) finishTree(root *ComponentNode) *ComponentTree {
	ann := b.ann
	// Validate that every construct in the module's functions is supported by
	// the JS code generator. Unsupported nodes become hard build errors rather
	// than being silently dropped from the emitted hydration/SSR code.
	// Functions are visited in name order so the error list is deterministic;
	// an aliased function (an import alias maps to the same declaration) is
	// validated once.
	fnNames := make([]string, 0, len(ann.Functions))
	for name := range ann.Functions {
		fnNames = append(fnNames, name)
	}
	sort.Strings(fnNames)
	checked := make(map[*ast.FnDecl]bool, len(fnNames))
	for _, name := range fnNames {
		fn := ann.Functions[name]
		if fn == nil || checked[fn] {
			continue
		}
		checked[fn] = true
		b.srcFn = name
		b.codegenErrs = append(b.codegenErrs, codegenIssues(fn, func(pos ast.Pos, msg, hint string) error {
			return b.diagAt(pos, diag.SeverityError, msg, hint)
		})...)
		b.srcFn = ""
	}

	errs := make([]error, 0, len(b.cssErrs)+len(b.buildErrs)+len(b.codegenErrs))
	errs = append(errs, b.cssErrs...)
	errs = append(errs, b.buildErrs...)
	errs = append(errs, b.codegenErrs...)
	errs = append(errs, b.markupErrs...)

	b.reportClassNotes()

	cssSignals, noJSFallback := b.cssStylesheets()
	return &ComponentTree{
		Root:             root,
		HasLinks:         b.hasLinks,
		RuntimeStore:     b.runtimeProps,
		Functions:        ann.Functions,
		CVAFactories:     b.cvaFactories,
		ContextDefaults:  b.contextDefaults,
		ModuleConstInits: b.moduleConstInits,
		CSSSignalsCSS:    cssSignals,
		HasCSSSignals:    len(b.cssCollected) > 0 || len(b.cssConditions) > 0,
		NoJSFallbackCSS:  noJSFallback,
		NeedsCSSARIA:     b.cssNeedsARIA,
		Errors:           errs,

		ExternalComponents: b.externals,
		Warnings:           b.warnings,
	}
}

// cssStylesheets returns the generated CSS signal stylesheet (mode-aware) and
// the no-JS fallback (empty when the tree has no CSS signals). Legacy mode never
// uses :has(), so its fallback is unconditional; hybrid must gate on :has() so a
// modern browser with scripting off keeps native :has() behavior.
func (b *builder) cssStylesheets() (stylesheet, noJSFallback string) {
	if len(b.cssCollected) == 0 && len(b.cssConditions) == 0 {
		return "", ""
	}
	scopes := make([]*csssignals.Scope, 0, len(b.cssCollected))
	for _, s := range b.cssCollected {
		scopes = append(scopes, s)
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].Index < scopes[j].Index })
	conditions := append([]*csssignals.Condition(nil), b.cssConditions...)
	sort.SliceStable(conditions, func(i, j int) bool {
		if a, c := conditions[i].OwningScope().Index, conditions[j].OwningScope().Index; a != c {
			return a < c
		}
		return conditions[i].ExprIdx < conditions[j].ExprIdx
	})
	stylesheet = csssignals.StylesheetMode(scopes, conditions, b.compat)
	if b.compat == csssignals.StyleLegacy {
		noJSFallback = csssignals.RevealCSS(scopes, conditions)
	} else {
		noJSFallback = csssignals.NoHasFallback(scopes, conditions)
	}
	return stylesheet, noJSFallback
}

// componentCtx is the build state scoped to the component instance being
// built. The builder embeds the active context; buildComponentNode pushes the
// caller's context on entry and pops it on exit, so nothing a component build
// changes here can leak into its parent or siblings. A child starts from a
// copy of its caller's context: the call-site inputs are set by
// buildComponentSlot before the push, and the render scope is replaced as the
// child's body is processed.
type componentCtx struct {
	// Call-site inputs.
	localProps       valueMap        // resolved props, params, locals and module consts (name -> value)
	localFuncProps   map[string]bool // prop names whose values are function references
	callSiteChildren []ast.JSXChild  // call-site children of the current component
	// restProps maps a rest (or whole-props) parameter name to the call-site
	// attributes it collects (attr name -> expression). Used to expand
	// `{...props}` spreads on intrinsic elements.
	restProps         map[string]map[string]ast.Expr
	pendingScopeProps map[string]scopeRef // the parent's scope-valued props for the child build
	forceClient       bool                // keep a wrapper client so call-site child slots hydrate
	// liveProps names the props whose call-site value reads a caller signal;
	// they are registered as getters, so `<livePropsParam>.<name>` reads are
	// reactive in the child's hydration code.
	liveProps      map[string]bool
	livePropsParam string
	// contextScopes maps an active context name (as provided by a
	// `<Ctx.Provider value={scope}>` subtree) to the scope handle it carries, so
	// `const s = Ctx.useContext()` inside a descendant resolves `s.trigger(...)`.
	contextScopes map[string]scopeRef

	// Render scope of the component body.
	currentNode     *ComponentNode      // component whose slots are being built
	localFnBody     []ast.Stmt          // body of current component function for handler resolution
	localSignals    map[string]ast.Expr // component-local signal context (name -> initial expr)
	signalSetters   map[string]bool     // component-local signal setter names
	localParamNames map[string]bool     // component parameter names (destructured or not) for undefined folding
	refObjectVars   map[string]bool     // component-local names bound to a useRef {current:...} object
	refCallbackVars map[string]bool     // component-local names bound to a function (callback-ref targets)
	// shadow is the chain of nested lexical scopes active during an expression
	// walk. Names it declares shadow component-level signals and locals.
	shadow *bindingScope
	// contextValues holds the active provider value expression per context,
	// pushed while building a `<Ctx.Provider value={expr}>` subtree so a
	// dynamic value substitutes for `Ctx.useContext()` reads.
	contextValues map[string]ast.Expr
	// contextInitials holds the build-time value of each active dynamic
	// provider value, so a consumer deeper in the tree (where the value is
	// read through a forwarded getter prop) still gets its SSR initial.
	contextInitials map[string]jsvalue.Value
	// scopeHandles maps a local identifier (or `props.<name>`) to the CSS scope
	// it holds, so `x.trigger(v)` / `x.panel(v)` / `x.close()` spreads resolve
	// anywhere in any component (a handle can be passed to children as a prop).
	scopeHandles map[string]scopeRef
	// cssSignals is the analyzer for the component currently being walked, and
	// cssToken its per-instance token. Both are nil/"" outside a CSS signal
	// component. They drive trigger/panel interception in buildJSXSlot and the
	// controller/diagnostics collection.
	cssSignals *csssignals.Analyzer
	cssToken   string

	// Hydration bindings accumulated while building the component's slots.
	pendingHandlers  []HandlerDecl
	pendingAttrs     []AttrBinding
	pendingRefs      []RefBinding
	pendingPropsRegs []string // __krate_props["id"]={...} registrations for child components
}

// pushComponentCtx saves the active component context.
func (b *builder) pushComponentCtx() {
	b.ctxStack = append(b.ctxStack, b.componentCtx)
}

// popComponentCtx restores the context saved by the matching push.
func (b *builder) popComponentCtx() {
	n := len(b.ctxStack) - 1
	b.componentCtx = b.ctxStack[n]
	b.ctxStack[n] = componentCtx{}
	b.ctxStack = b.ctxStack[:n]
}

// builder holds state during IR construction. Per-component state lives in
// the embedded componentCtx; the fields below are build-global: inputs shared
// by every component, counters that keep IDs unique across the page, and the
// collected output (CSS, externals, diagnostics).
type builder struct {
	componentCtx
	ctxStack []componentCtx

	// locateOnly is non-zero while building slots that only locate nested
	// client components and are never emitted (a build-time evaluated
	// component's own return JSX); warnings about rendered output are skipped.
	locateOnly     int
	functions      map[string]*ast.FnDecl
	ann            *Annotations
	idCounter      map[string]int
	hasLinks       bool
	runtimeProps   *RuntimePropStore
	instanceCounts map[string]int // per-component-name instance counter
	boundaryThrows map[SlotID]BoundaryThrow
	slotCounter    int               // monotonic counter for compact slot IDs
	slotIDMap      map[string]SlotID // logical ID -> compact ID
	slotCounts     map[string]int    // per-parent dynamic-slot counters for sibling disambiguation
	// elementCounts is the per-parent tag counter for sibling disambiguation.
	// A component's walk clears it on entry and restores the caller's counts
	// on exit, so siblings after a child component keep unique IDs.
	elementCounts    map[string]int
	moduleConsts     valueMap                  // module-level const values (name -> folded value)
	moduleConstInits map[string]ast.Expr       // module-level const initializers (name -> expr)
	moduleStmts      []ast.Stmt                // entry module top-level statements that run code
	entryProg        *ast.Program              // the entry module (imports for KR8302)
	cvaFactories     map[string]*CVASpec       // module-level `const X = cva(...)` factories
	contextDefaults  map[string]string         // module-level `const X = createContext(v)` defaults
	codeTheme        string                    // chroma theme for compile-time <Code> highlighting
	idPrefix         string                    // namespace for compact slot IDs (merged builds)
	aggressiveDCE    bool                      // drop unreferenced local functions (opt-in)
	suspenseCount    int                       // monotonic counter for stable StreamID generation
	compat           csssignals.StyleMode      // CSS-signal stylesheet mode (modern/legacy/hybrid)
	goComponents     map[string]GoComponentRef // imported Go components (local name -> ref)
	externalImports  map[string]ExternalImport // bare-package import bindings (local -> module)
	localImports     map[string]bool           // relative-path import bindings
	actionURLs       map[string]string         // server-action binding -> POST URL
	externals        []ExternalComponent       // unresolved package components mounted at runtime
	warnings         []diag.Diagnostic         // non-fatal, positioned build warnings
	// snapshotPropWarned dedupes snapshot-prop warnings per component and props.
	snapshotPropWarned map[string]bool
	// eventDiagSeen dedupes event/form/ref warnings per source position.
	eventDiagSeen map[string]bool
	markupErrs    []error    // invalid tag/attribute names, children on void elements
	selectStack   [][]string // selected option values of enclosing <select> elements

	// cssIndex assigns stable, page-unique indices to (component, var) scope
	// pairs so classes/CSS are deterministic across builds.
	cssIndex map[string]int
	// cssAnalyzers memoizes per-component CSS signal analysis (nil entries for
	// components without any CSS signal declaration).
	cssAnalyzers map[*ast.FnDecl]*csssignals.Analyzer
	// scopeOverrides holds call-site option universes for compound scope-root
	// components (their namespaced parts): fn -> options/initial. Consulted by
	// cssAnalyzer so controllers/classes match the exact set of call-site parts.
	scopeOverrides map[*ast.FnDecl]scopeSpec
	// cssCollected holds every scope whose controller inputs were emitted, so
	// the tree can expose the exact set of stylesheets the page needs.
	cssCollected map[int]*csssignals.Scope
	// cssConditions holds deduped matched conditions whose wrappers were
	// emitted, keyed by wrapper class for cross-instance deduplication.
	cssConditions []*csssignals.Condition
	cssCondSeen   map[string]bool
	// cssNeedsARIA is set when any scope uses a role that needs the tiny ARIA
	// micro-runtime (tabs/listbox/disclosure). When false the page is zero-JS.
	cssNeedsARIA bool
	// cssErrs collects hard errors for CSS signals that cannot be compiled.
	// These fail the build: a CSS signal that cannot be expressed in CSS must be
	// replaced with createSignal by the author - silently hydrating it would
	// ship behaviour the author did not ask for.
	cssErrs []error
	// codegenErrs collects hard errors for AST constructs the JS code generator
	// does not support. They fail the build so no construct is silently dropped
	// from emitted hydration/SSR code.
	codegenErrs []error
	// buildErrs collects positioned hard errors raised while building slots
	// (unlowerable early returns, recursive component expansion, ...).
	buildErrs []error

	// expansion is the stack of component functions currently being expanded,
	// used to detect recursive components; recursiveExpansions counts every
	// re-entry so a diverging recursion is cut off with a diagnostic.
	expansion           []expansionFrame
	recursiveExpansions int
	recursionReported   map[string]bool
	// returnIssueReported dedupes unlowerable early-return diagnostics per
	// component function (a component may be instantiated many times).
	returnIssueReported map[*ast.FnDecl]bool
	// srcFn names the function whose AST is being walked when it differs from
	// the component node being built (a child's return walked for discovery),
	// so diagnostics resolve the right source file.
	srcFn string
}

func (b *builder) collectCSSScope(s *csssignals.Scope) {
	if b.cssCollected == nil {
		b.cssCollected = make(map[int]*csssignals.Scope)
	}
	b.cssCollected[s.Index] = s
}

func (b *builder) collectCSSCondition(c *csssignals.Condition) {
	if c == nil {
		return
	}
	if b.cssCondSeen == nil {
		b.cssCondSeen = make(map[string]bool)
	}
	if b.cssCondSeen[c.Class] {
		return
	}
	b.cssCondSeen[c.Class] = true
	b.cssConditions = append(b.cssConditions, c)
}

func (b *builder) collectCSSErr(component string, line int, reason string) {
	where := component
	if line > 0 {
		where = component + ":" + itoa(line)
	}
	b.cssErrs = append(b.cssErrs, fmt.Errorf(
		"createCSS* in %s: %s. Replace it with createSignal to use client JS, "+
			"or restructure so the state is only read in showIf panels and written by onClick triggers.",
		where, reason))
}

// cssSignalsOK reports whether a component's CSS primitives compile cleanly.
func (b *builder) cssSignalsOK(fn *ast.FnDecl) bool {
	a := b.cssAnalyzer(fn)
	return a != nil && a.OK()
}

// cssAnalyzer returns (and memoizes) the CSS signal analyzer for a component
// function. The analyzer is instance-independent. The result is nil when the
// component has no CSS signal declarations; callers must check OK() to tell a
// compilable component from one with errors.
func (b *builder) cssAnalyzer(fn *ast.FnDecl) *csssignals.Analyzer {
	if fn == nil {
		return nil
	}
	if b.cssAnalyzers == nil {
		b.cssAnalyzers = make(map[*ast.FnDecl]*csssignals.Analyzer)
	}
	if a, ok := b.cssAnalyzers[fn]; ok {
		return a
	}
	if b.cssIndex == nil {
		b.cssIndex = make(map[string]int)
	}
	indexFn := func(component, variable string) int {
		key := component + "\x00" + variable
		if idx, ok := b.cssIndex[key]; ok {
			return idx
		}
		idx := len(b.cssIndex)
		b.cssIndex[key] = idx
		return idx
	}
	var a *csssignals.Analyzer
	if spec, ok := b.scopeOverrides[fn]; ok && len(spec.options) > 0 {
		a = csssignals.AnalyzeParts(fn.Name, fn.Body, indexFn, spec.options, spec.initial)
	} else {
		a = csssignals.Analyze(fn.Name, fn.Body, indexFn)
	}
	b.cssAnalyzers[fn] = a
	return a
}

// sigMap returns the signal context for the current component being built.
// Component-local signals (from buildComponentNode) override the global page
// annotation signals so signal reads resolve to the correct initial values even
// when multiple components use the same signal name.
func (b *builder) sigMap() map[string]ast.Expr {
	if b.localSignals != nil {
		return b.localSignals
	}
	ann := b.ann
	return ann.Signals
}

// assignSlotID maps a logical slot ID to a compact base62 identifier.
// The compact ID is used in HTML data-k attributes, comment markers, and
// hydration JS so that HTML and JS always agree on slot IDs.
func (b *builder) assignSlotID(logical SlotID) SlotID {
	if logical == "" {
		return ""
	}
	if compact, ok := b.slotIDMap[string(logical)]; ok {
		return compact
	}
	b.slotCounter++
	compact := SlotID(b.idPrefix + toBase62(b.slotCounter))
	b.slotIDMap[string(logical)] = compact
	return compact
}

// toBase62 converts an integer to a base62 string (0-9a-zA-Z).
func toBase62(n int) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = chars[n%62]
		n /= 62
	}
	return string(buf[i:])
}

// nextDynamicSlot returns a unique logical slot ID for a dynamic child slot
// (text/expr/cond/list) of the given parent. Multiple dynamic slots under the
// same parent (e.g. two {expr} containers in one <p>) must not share a logical
// path or assignSlotID would map them to the same compact ID.
func (b *builder) nextDynamicSlot(parentID, kind string) SlotID {
	base := string(joinSlotID(parentID, kind))
	b.slotCounts[base]++
	n := b.slotCounts[base]
	if n == 1 {
		return SlotID(base)
	}
	return joinSlotID(parentID, kind+"."+itoa(n))
}

// CollectModuleConstants returns a module's top-level literal values and
// initializers. The build pipeline uses it to merge constants from imported
// modules so imported literals (and `export { a as b }` aliases) fold.
func CollectModuleConstants(prog *ast.Program) (map[string]string, map[string]ast.Expr) {
	return collectModuleConsts(prog), collectModuleConstInits(prog)
}

// mergedModuleConstants combines the entry module's constants with those
// supplied through BuildOptions. Entry-module values win on collision.
func mergedModuleConstants(prog *ast.Program, opts BuildOptions) (map[string]string, map[string]ast.Expr) {
	consts := collectModuleConsts(prog)
	inits := collectModuleConstInits(prog)
	for k, v := range opts.ModuleConsts {
		if _, ok := consts[k]; !ok {
			consts[k] = v
		}
	}
	for k, v := range opts.ModuleConstInits {
		if _, ok := inits[k]; !ok {
			inits[k] = v
		}
	}
	return consts, inits
}
