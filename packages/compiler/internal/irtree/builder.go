package irtree

import (
	"fmt"
	"sort"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/csssignals"
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

	codeTheme := opts.CodeTheme
	if codeTheme == "" {
		codeTheme = syntaxhighlight.DefaultTheme
	}
	builder := &builder{
		functions:        ann.Functions,
		ann:              ann,
		idCounter:        make(map[string]int),
		instanceCounts:   make(map[string]int),
		elementCounts:    make(map[string]int),
		slotIDMap:        make(map[string]SlotID),
		slotCounts:       make(map[string]int),
		moduleConsts:     collectModuleConsts(prog),
		moduleConstInits: collectModuleConstInits(prog),
		cvaFactories:     mergeCVAFactories(ann.CVAFactories, prog),
		contextDefaults:  mergeContextDefaults(ann.ContextDefaults, prog),
		codeTheme:        codeTheme,
		idPrefix:         opts.IDPrefix,
		aggressiveDCE:    opts.AggressiveDCE,
	}

	root := builder.buildComponentNode(entryFn, "")
	root.SourceFile = ann.SourceFile

	// Validate that every construct in the module's functions is supported by
	// the JS code generator. Unsupported nodes become hard build errors rather
	// than being silently dropped from the emitted hydration/SSR code.
	for _, fn := range ann.Functions {
		builder.codegenErrs = append(builder.codegenErrs, codegenIssues(fn)...)
	}

	errs := make([]error, 0, len(builder.cssErrs)+len(builder.codegenErrs))
	errs = append(errs, builder.cssErrs...)
	errs = append(errs, builder.codegenErrs...)

	return &ComponentTree{
		Root:            root,
		HasLinks:        builder.hasLinks,
		RuntimeStore:    builder.runtimeProps,
		Functions:       ann.Functions,
		CVAFactories:    builder.cvaFactories,
		ContextDefaults: builder.contextDefaults,
		CSSSignalsCSS:   builder.cssStylesheet(),
		NeedsCSSARIA:    builder.cssNeedsARIA,
		Errors:          errs,
	}
}

// cssStylesheet returns the generated CSS signal stylesheet with scopes and
// compound-show conditions collected during the build (empty when none).
func (b *builder) cssStylesheet() string {
	if len(b.cssCollected) == 0 && len(b.cssConditions) == 0 {
		return ""
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
	return csssignals.Stylesheet(scopes, conditions)
}

// builder holds state during IR construction.
type builder struct {
	functions        map[string]*ast.FnDecl
	ann              *Annotations
	idCounter        map[string]int
	hasLinks         bool
	runtimeProps     *RuntimePropStore
	instanceCounts   map[string]int      // per-component-name instance counter
	elementCounts    map[string]int      // per-parent tag name counter for sibling disambiguation
	slotCounter      int                 // monotonic counter for compact slot IDs
	slotIDMap        map[string]SlotID   // logical ID -> compact ID
	pendingHandlers  []HandlerDecl       // accumulated during buildSlotNodes
	pendingAttrs     []AttrBinding       // accumulated during buildSlotNodes
	pendingRefs      []RefBinding        // accumulated during buildSlotNodes
	pendingPropsRegs []string            // __krate_props["id"]={...} registrations for child components
	slotCounts       map[string]int      // per-parent dynamic-slot counters for sibling disambiguation
	localFnBody      []ast.Stmt          // body of current component function for handler resolution
	localSignals     map[string]ast.Expr // component-local signal context (name -> initial expr)
	localProps       map[string]string   // component-local resolved props (name -> value)
	localParamNames  map[string]bool     // component parameter names (destructured or not) for undefined folding
	// restProps maps a rest parameter name (e.g. `props` in `{a, ...props}`) to
	// the call-site attributes it collects (attr name -> expression). Used to
	// expand `{...props}` spreads on intrinsic elements.
	restProps        map[string]map[string]ast.Expr
	localFuncProps   map[string]bool     // component-local prop names whose values are function references
	refObjectVars    map[string]bool     // component-local names bound to a useRef {current:...} object
	refCallbackVars  map[string]bool     // component-local names bound to a function (callback-ref targets)
	callSiteChildren []ast.JSXChild      // call-site children of the current component
	moduleConsts     map[string]string   // module-level const values (name -> resolved literal)
	moduleConstInits map[string]ast.Expr // module-level const initializers (name -> expr)
	cvaFactories     map[string]*CVASpec // module-level `const X = cva(...)` factories
	contextDefaults  map[string]string   // module-level `const X = createContext(v)` defaults
	codeTheme        string              // chroma theme for compile-time <Code> highlighting
	idPrefix         string              // namespace for compact slot IDs (merged builds)
	aggressiveDCE    bool                // drop unreferenced local functions (opt-in)
	suspenseCount    int                 // monotonic counter for stable StreamID generation

	// cssIndex assigns stable, page-unique indices to (component, var) scope
	// pairs so classes/CSS are deterministic across builds.
	cssIndex map[string]int
	// cssAnalyzers memoizes per-component CSS signal analysis (nil entries for
	// components without any CSS signal declaration).
	cssAnalyzers map[*ast.FnDecl]*csssignals.Analyzer
	// cssSignals is the analyzer for the component currently being walked, and
	// cssToken its per-instance token. Both are nil/"" outside a CSS signal
	// component. They drive trigger/panel interception in buildJSXSlot and the
	// controller/diagnostics collection.
	cssSignals *csssignals.Analyzer
	cssToken   string
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
	a := csssignals.Analyze(fn.Name, fn.Body, func(component, variable string) int {
		key := component + "\x00" + variable
		if idx, ok := b.cssIndex[key]; ok {
			return idx
		}
		idx := len(b.cssIndex)
		b.cssIndex[key] = idx
		return idx
	})
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
