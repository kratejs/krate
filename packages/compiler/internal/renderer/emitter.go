package renderer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// EmitResult consolidates all page output from the emitter.
type EmitResult struct {
	HTML       string
	HeadHTML   string
	ScriptHTML string
	StyleHTML  string
	Signatures []irtree.ComponentSignature
	HasLinks   bool
	UsedFuncs  map[string]bool
	UsedCSS    map[string]bool
	// ListComponents are the client component functions referenced by dynamic
	// list slots (e.g. <Toast> in a .map() body). They're emitted into the
	// hydration scope so the runtime can re-render lists via h(Component, ...).
	ListComponents []*ast.FnDecl
	// Errors holds diagnostics for unsupported constructs encountered during
	// render. A non-empty set means the build must fail rather than ship
	// silently empty/wrong output.
	Errors []error
	// GoRenders are build-time Go-component render requests; the build renders
	// them via the Go host and substitutes HTML at the markers.
	GoRenders []irtree.GoRenderRequest
	// IslandBootJS publishes page-scope signal values for external React
	// islands. It is emitted inside the page hydration IIFE (where those signals
	// are in scope) before the islands mount.
	IslandBootJS string
	// RuntimeChildren maps a runtime Go region id ("region-<id>") to the HTML
	// of its build-rendered static children. Emit never mutates the shared IR,
	// so the build attaches these to the region props itself.
	RuntimeChildren map[string]string
	// LazyChunks holds the hydration code of lazy() components, filled by
	// GenerateNewHydrationJS; the build writes each one to its own file.
	LazyChunks []LazyChunk
	// BoundaryThrows lists error boundaries whose subtree threw while the page
	// was built; the build re-renders them in their fallback state.
	BoundaryThrows []irtree.BoundaryThrow
}

// SlotOutput is the result of emitting a single slot node. Beyond HTML and
// signatures it carries subtree-local metadata (Head/Script/Style content) and
// orphan bindings, merged into the parent output in document order.
type SlotOutput struct {
	HTML       string
	Signatures []irtree.ComponentSignature
	HeadHTML   string
	ScriptHTML string
	StyleHTML  string
	Orphans    []orphanBinding
	GoRenders  []irtree.GoRenderRequest
	// Templates holds the <template data-kc> elements carrying the inactive
	// branch of each conditional in this subtree. They are hoisted to the end
	// of the page HTML so conditionals stay valid inside <table>, <ul>,
	// <select> or <p>; see emitConditionalSlot.
	Templates string
	// RuntimeChildren: see EmitResult.RuntimeChildren.
	RuntimeChildren map[string]string
	// BoundaryThrows: see EmitResult.BoundaryThrows.
	BoundaryThrows []irtree.BoundaryThrow
}

// inlineHTML returns the output's HTML with its hoisted templates appended,
// for consumers that keep only the HTML (props passed to a Go host, baked
// suspense content, meta capture).
func (o SlotOutput) inlineHTML() string {
	return o.HTML + o.Templates
}

// parallelMinChildren is the smallest sibling list worth spawning goroutines
// for. Below this the sequential path is used to avoid goroutine overhead.
// A var (not const) so tests can force one path or the other.
var parallelMinChildren = 8

// orphanBinding pairs a slot binding (collected from a static component's
// call-site children or a client component's call-site slots) with the ID of
// the client signature that is expected to own its signals.
type orphanBinding struct {
	owner irtree.SlotID
	bind  irtree.SlotBinding
	// lists names the list slots whose rows contain the binding's slot (see
	// ComponentSignature.InLists).
	lists []irtree.SlotID
}

// Emitter walks a ComponentTree and produces HTML + hydration metadata.
type Emitter struct {
	functions map[string]*ast.FnDecl
	// clientStack tracks the ComponentIDs of client components currently being
	// emitted, so orphan bindings can be attached to the signature that owns
	// the signals they reference.
	clientStack []irtree.SlotID
	// orphans holds slot bindings for dynamic slots that live in a child/static
	// component's call-site children but reference the caller's signals.
	// They're attached to the owning signature in Emit().
	orphans []orphanBinding
	// headHTML/scriptHTML/styleHTML accumulate <Head>/<Script>/<Style> content
	// captured by SSREval during signal-less component emission. walkMetaSlots
	// only reaches MetaSlots in the IR tree; SSREval'd wrappers (layouts, doc
	// shells) render their return JSX directly so their meta content must be
	// collected here and merged into the EmitResult.
	headHTML   string
	scriptHTML string
	styleHTML  string

	// IconResolver compiles an <Icon name="..."> to its SVG markup at SSR
	// time. It's wired by the build pipeline to resolve icons whose `name`
	// attribute is a runtime-evaluated expression (e.g. name={icon}). When
	// nil, dynamic-name icons are left as unknown components.
	IconResolver func(name string, attrs []*ast.JSXAttr) (string, bool)

	// EvalJS is wired by the build to the embedded QuickJS engine. It evaluates
	// self-contained JS expressions with full built-ins (Date, Math, String,
	// Number, ...) so server-component SSR computes globals like Date.now()
	// with real JS semantics instead of Go approximations.
	EvalJS func(code string) (string, error)

	// GoComponents maps imported Go component names to their refs. Wired by the
	// build pipeline; SSREval renders them via the shared goCollector.
	GoComponents map[string]irtree.GoComponentRef

	// cvaFactories holds module-wide `const X = cva(...)` specs so variant calls
	// in prop-driven components fold to static class strings during SSREval.
	cvaFactories map[string]*irtree.CVASpec

	// contextDefaults holds module-wide `const X = createContext(v)` defaults so
	// `X.useContext()` folds during SSREval (no Provider exists at build time).
	contextDefaults map[string]jsvalue.Value

	// moduleConsts holds module-level `const X = <expr>` initializers so
	// SSREval resolves module constants referenced by signal-less components.
	moduleConsts map[string]ast.Expr

	// CodeTheme is the chroma theme used to highlight <Code>/<SyntaxHighlight>
	// during SSREval. It must match the stylesheet generated for the same theme
	// (syntaxhighlight.CSSForTheme). Wire it from the build; empty selects the
	// default theme.
	CodeTheme string

	// errs collects diagnostics for unsupported constructs encountered during
	// emit (unknown IR slots, or SSR-evaluated expressions the evaluator could
	// not handle). They surface on EmitResult.Errors so the build fails instead
	// of silently emitting empty/wrong output.
	errs []error

	// goCollector accumulates build-time Go-component render requests from both
	// the slot path and SSREval, so the whole page is rendered in one host call.
	goCollector *goRenderCollector
}

// goRenderCollector accumulates Go-component render requests and hands out
// unique marker ids. Shared across the parallel sub-emitters of one page.
type goRenderCollector struct {
	mu   sync.Mutex
	n    int
	reqs []irtree.GoRenderRequest
}

func (c *goRenderCollector) enqueue(callID, name, props string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	id := "g" + strconv.Itoa(c.n)
	c.reqs = append(c.reqs, irtree.GoRenderRequest{ID: id, Name: name, Props: props, CallID: callID})
	return irtree.GoRenderMarker(id)
}

func (c *goRenderCollector) snapshot() []irtree.GoRenderRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]irtree.GoRenderRequest(nil), c.reqs...)
}

// addErr records an unsupported-construct diagnostic during emit.
func (e *Emitter) addErr(format string, args ...interface{}) {
	e.errs = append(e.errs, fmt.Errorf("unsupported render: "+format, args...))
}

// Errors returns the diagnostics collected during emit (may be empty).
func (e *Emitter) Errors() []error {
	return e.errs
}

// NewEmitter creates a new emitter.
func NewEmitter() *Emitter {
	return &Emitter{goCollector: &goRenderCollector{}}
}

// Emit walks the ComponentTree and produces an EmitResult.
func (e *Emitter) Emit(tree *irtree.ComponentTree) *EmitResult {
	e.functions = tree.Functions
	e.cvaFactories = tree.CVAFactories
	e.contextDefaults = tree.ContextDefaults
	e.moduleConsts = tree.ModuleConstInits
	output := e.emitNode(tree.Root)

	// Merge subtree-local metadata and orphans collected during emit.
	e.headHTML += output.HeadHTML
	e.scriptHTML += output.ScriptHTML
	e.styleHTML += output.StyleHTML
	e.orphans = append(e.orphans, output.Orphans...)

	// Rows of a list bound by the keyed list runtime are rendered and adopted
	// by that list, so the components and bindings inside them must not also
	// hydrate on their own.
	managed := managedLists(output.Signatures, e.orphans)

	// Attach orphan bindings (dynamic slots in call-site children that
	// reference a caller's signals) to the signature that owns them: first try
	// the client component that enclosed the collection site, then fall back to
	// the first signature declaring the binding's signals.
	if len(e.orphans) > 0 {
		for _, o := range e.orphans {
			if inManagedList(o.lists, managed) {
				continue
			}
			if owner := findSignatureOwner(output.Signatures, o.owner, o.bind); owner >= 0 {
				output.Signatures[owner].SlotBindings = append(output.Signatures[owner].SlotBindings, o.bind)
			}
		}
	}
	if len(managed) > 0 {
		kept := output.Signatures[:0]
		for _, sig := range output.Signatures {
			if !inManagedList(sig.InLists, managed) {
				kept = append(kept, sig)
			}
		}
		output.Signatures = kept
	}

	// Collect client component functions referenced by dynamic list slots so
	// they can be emitted into the hydration scope. Without them, a .map()
	// body like <Toast/> compiles to h(Toast, ...) which throws ReferenceError.
	listComponents := collectListComponentFuncs(tree.Root, tree.Functions)

	return &EmitResult{
		HTML:            output.HTML + output.Templates,
		HeadHTML:        e.headHTML,
		ScriptHTML:      e.scriptHTML,
		StyleHTML:       e.styleHTML,
		Signatures:      output.Signatures,
		HasLinks:        tree.HasLinks,
		UsedFuncs:       make(map[string]bool),
		UsedCSS:         make(map[string]bool),
		ListComponents:  listComponents,
		Errors:          e.errs,
		GoRenders:       append(output.GoRenders, e.goCollector.snapshot()...),
		RuntimeChildren: output.RuntimeChildren,
		BoundaryThrows:  output.BoundaryThrows,
	}
}

// collectListComponentFuncs walks the component tree for ListSlots and returns
// the client component FnDecls their map bodies reference (deduplicated, in a
// stable order). Only client-tier components are included - static/server
// components are SSR-evaluated and don't exist as runtime functions.
func collectListComponentFuncs(root *irtree.ComponentNode, functions map[string]*ast.FnDecl) []*ast.FnDecl {
	seen := make(map[string]bool)
	var result []*ast.FnDecl
	var walk func(n *irtree.ComponentNode)
	walk = func(n *irtree.ComponentNode) {
		if n == nil {
			return
		}
		for _, child := range n.Children {
			if ls, ok := child.(*irtree.ListSlot); ok {
				for _, name := range ls.Components {
					if seen[name] {
						continue
					}
					fn := functions[name]
					if fn == nil {
						continue
					}
					seen[name] = true
					result = append(result, fn)
				}
			}
		}
		for _, child := range n.CallSiteSlots {
			if ls, ok := child.(*irtree.ListSlot); ok {
				for _, name := range ls.Components {
					if seen[name] {
						continue
					}
					fn := functions[name]
					if fn == nil {
						continue
					}
					seen[name] = true
					result = append(result, fn)
				}
			}
		}
		for _, child := range n.CallSiteSlots {
			if cs, ok := child.(*irtree.ComponentSlot); ok {
				walk(cs.Component)
			}
		}
		for _, child := range n.Children {
			if cs, ok := child.(*irtree.ComponentSlot); ok {
				walk(cs.Component)
			}
		}
	}
	walk(root)
	return result
}

// findSignatureOwner returns the index of the client signature whose ID
// matches the recorded owner AND declares all of the binding's signal reads.
// If the recorded owner is empty or doesn't declare the signals, the first
// client signature declaring all signals is used as a fallback. Returns -1
// when no signature can own the binding.
func findSignatureOwner(sigs []irtree.ComponentSignature, owner irtree.SlotID, bind irtree.SlotBinding) int {
	if owner != "" {
		for i, sig := range sigs {
			if sig.Tier == irtree.TierClient && sig.ComponentID == owner && declaresSignals(sig, bind.Signals) {
				return i
			}
		}
	}
	for i, sig := range sigs {
		if sig.Tier == irtree.TierClient && declaresSignals(sig, bind.Signals) {
			return i
		}
	}
	return -1
}

// declaresSignals reports whether the signature's declared signals include all
// of the given reads.
func declaresSignals(sig irtree.ComponentSignature, reads []string) bool {
	if len(reads) == 0 {
		return false
	}
	for _, s := range reads {
		owned := false
		for _, d := range sig.Signals {
			if d.Name == s {
				owned = true
				break
			}
		}
		if !owned {
			return false
		}
	}
	return true
}

// emitNode dispatches by component tier.
func (e *Emitter) emitNode(node *irtree.ComponentNode) SlotOutput {
	if node == nil {
		return SlotOutput{}
	}
	// SSR-evaluated components: use the SSREval engine with prop bindings
	if node.IsSSREval {
		return e.emitSSREvaluated(node)
	}
	switch node.Tier {
	case irtree.TierStatic:
		return e.emitStatic(node)
	case irtree.TierServer:
		return e.emitServer(node)
	case irtree.TierClient:
		return e.emitClient(node)
	case irtree.TierRuntime:
		return e.emitRuntime(node)
	default:
		return e.emitClient(node)
	}
}

// emitStatic - static component SSR

func (e *Emitter) emitStatic(node *irtree.ComponentNode) SlotOutput {
	var out SlotOutput
	for _, child := range node.Children {
		mergeSlotOutput(&out, e.emitSlotNode(child))
	}
	// A static component built through the tree path may render {props.children}
	// (e.g. a context-providing root or a scope-part component); fill the
	// placeholder with its call-site slots, like emitClient.
	if strings.Contains(out.HTML, "<!--__children__-->") && len(node.CallSiteSlots) > 0 {
		callSiteOut := e.emitSlotsParallel(node.CallSiteSlots)
		html := callSiteOut.HTML
		callSiteOut.HTML = ""
		out.HTML = strings.ReplaceAll(out.HTML, "<!--__children__-->", html)
		mergeSlotOutput(&out, callSiteOut)
	}
	return out
}

// emitSSREvaluated - prop-driven components evaluated at build time

func (e *Emitter) emitSSREvaluated(node *irtree.ComponentNode) SlotOutput {
	if node.Fn == nil {
		return SlotOutput{}
	}
	eval := NewSSREval(e.functions)
	if e.EvalJS != nil {
		eval.SetEvalJS(e.EvalJS)
	}
	if len(e.GoComponents) > 0 {
		collector := e.goCollector
		eval.SetGoComponents(e.GoComponents, func(callID, name, propsJSON string) string {
			return collector.enqueue(callID, name, propsJSON)
		})
	}
	if e.cvaFactories != nil {
		eval.SetCVAFactories(e.cvaFactories)
	}
	if e.contextDefaults != nil {
		eval.SetContextDefaults(e.contextDefaults)
	}
	eval.SetContextValues(node.ContextValues)
	eval.SetCodeTheme(e.CodeTheme)
	eval.SetModuleConsts(e.moduleConsts)
	if node.RestProps != nil && node.RestPropsName != "" {
		eval.SetRestProps(map[string]map[string]ast.Expr{node.RestPropsName: node.RestProps})
	}
	if node.SSREvalBindings != nil {
		eval.SetBindings(node.SSREvalBindings)
	}
	eval.ResolveJSXProps(node.SSREvalAttrs)
	if e.IconResolver != nil {
		eval.iconEmit = func(name string, attrs []*ast.JSXAttr) (string, bool) {
			return e.IconResolver(name, attrs)
		}
	}
	var sigs []irtree.ComponentSignature
	var out SlotOutput

	// Emit call site JSX children through the pre-built slot tree so that
	// elements with handlers/attr bindings retain their data-k/data-kh
	// hydration markers. SSREval is only used to evaluate the component's own
	// return statement, whose {props.children} placeholder is replaced with
	// this HTML via the "children" binding.
	if len(node.Children) > 0 {
		var childrenHTML strings.Builder
		childrenOut := e.emitSlotsParallel(node.Children)
		childrenHTML.WriteString(childrenOut.HTML)
		sigs = append(sigs, childrenOut.Signatures...)
		out.Orphans = append(out.Orphans, childrenOut.Orphans...)
		out.HeadHTML += childrenOut.HeadHTML
		out.ScriptHTML += childrenOut.ScriptHTML
		out.StyleHTML += childrenOut.StyleHTML
		out.Templates += childrenOut.Templates
		out.GoRenders = append(out.GoRenders, childrenOut.GoRenders...)
		out.RuntimeChildren = mergeRuntimeChildren(out.RuntimeChildren, childrenOut.RuntimeChildren)
		// Conditional/list slots in call-site children (e.g. {items().map(...)}
		// inside a signal-less wrapper like ToastViewport) reference the
		// CALLER's signals - register an orphan binding so the re-render effect
		// is attached to the signature that owns those signals.
		owner := irtree.SlotID("")
		if len(e.clientStack) > 0 {
			owner = e.clientStack[len(e.clientStack)-1]
		}
		out.Orphans = append(out.Orphans, collectChildrenOrphans(node.Children, owner)...)
		if childrenHTML.Len() > 0 {
			// Children were rendered through the slot pipeline (text escaped,
			// elements hydrated), so they bind as a JSX value injected raw.
			eval.SetChildrenHTML(childrenHTML.String())
			// When the call-site children were fully static text, SSREval also
			// gets the raw text so <SyntaxHighlight> chroma-highlights the
			// original code instead of the rendered HTML.
			if node.CallSiteChildrenText != "" {
				eval.childrenRawText = node.CallSiteChildrenText
			}
		}
	}

	// Interactive client components nested in the component's OWN return JSX
	// (e.g. a signal-less wrapper function returning <DropdownMenu>...) must
	// also be emitted through the tree path so they keep their hydration
	// markers. The hook consumes ReturnSlots in document order as SSREval
	// encounters them. Only CLIENT-tier components are routed through the tree
	// path - signal-less (SSREval'd) children must stay in the parent's eval
	// flow so they inherit the parent's prop bindings (e.g. <SidebarNav
	// items={props.sidebarItems}/> needs `sidebarItems` bound in the parent's
	// SSREval context). Routing them through a fresh emitSSREvaluated loses
	// those bindings and they render empty.
	// Index the component's own return slots by component NAME so the hook can
	// match the element SSREval is currently evaluating. Matching by call order
	// alone is wrong: SSREval visits every uppercase child (including
	// signal-less ones that it must evaluate itself), so a preceding signal-less
	// child would otherwise consume a client child's slot and render the wrong
	// component (or drop its hydration).
	returnSlots := map[string][]*irtree.ComponentSlot{}
	for _, child := range node.ReturnSlots {
		if slot, ok := child.(*irtree.ComponentSlot); ok && slot.Component != nil {
			returnSlots[slot.Component.Name] = append(returnSlots[slot.Component.Name], slot)
		}
	}
	if len(returnSlots) > 0 {
		used := map[string]int{}
		eval.interactiveEmit = func(el *ast.JSXElement) (string, bool) {
			name := el.Opening.Name
			queue := returnSlots[name]
			i := used[name]
			if i >= len(queue) {
				return "", false
			}
			used[name] = i + 1
			slot := queue[i]
			// Signal-less children stay in the parent's SSREval flow so they
			// inherit the parent's prop bindings.
			if slot.Component.Tier != irtree.TierClient {
				return "", false
			}
			slotOut := e.emitComponentSlot(slot)
			sigs = append(sigs, slotOut.Signatures...)
			out.Templates += slotOut.Templates
			out.GoRenders = append(out.GoRenders, slotOut.GoRenders...)
			out.RuntimeChildren = mergeRuntimeChildren(out.RuntimeChildren, slotOut.RuntimeChildren)
			return slotOut.HTML, true
		}
		defer func() { eval.interactiveEmit = nil }()
	}

	// Run the component body (locals, if/else returns, loops) and render its
	// return value.
	html := eval.EvalComponentBody(node.Fn)
	e.errs = append(e.errs, eval.Errors()...)
	out.HeadHTML += eval.HeadHTML
	out.ScriptHTML += eval.ScriptHTML
	out.StyleHTML += eval.StyleHTML
	out.HTML = html
	out.Signatures = sigs

	return out
}

// findReturnStmtIn finds the render return of a function body. Early-return
// guards and switch statements are lowered to a conditional chain the same
// way the IR builder lowers them for client components.
func findReturnStmtIn(body []ast.Stmt) *ast.ReturnStmt {
	return irtree.RenderReturn(body)
}

// emitServer - server component SSR

func (e *Emitter) emitServer(node *irtree.ComponentNode) SlotOutput {
	var out SlotOutput
	for _, child := range node.Children {
		mergeSlotOutput(&out, e.emitSlotNode(child))
	}
	wrapped := `<div data-krate-server="` + escape.HTMLAttr(node.Name) + `">` + out.HTML + `</div>`
	out.HTML = wrapped
	return out
}

// emitClient - client component SSR + scoped hydration metadata

func (e *Emitter) emitClient(node *irtree.ComponentNode) SlotOutput {
	var out SlotOutput

	e.clientStack = append(e.clientStack, node.ID)
	defer func() { e.clientStack = e.clientStack[:len(e.clientStack)-1] }()
	errStart := len(e.errs)

	mergeSlotOutput(&out, e.emitSlotsParallel(node.Children))
	ownCount := len(out.Signatures)

	// Replace {props.children} placeholder with rendered call site children.
	// This ensures interactive sub-components nested inside signal components
	// retain their data-k/data-kh hydration markers.
	if strings.Contains(out.HTML, "<!--__children__-->") && len(node.CallSiteSlots) > 0 {
		callSiteOut := e.emitSlotsParallel(node.CallSiteSlots)
		html := callSiteOut.HTML
		callSiteOut.HTML = ""
		out.HTML = strings.ReplaceAll(out.HTML, "<!--__children__-->", html)
		if cond, branch, ok := childrenPlaceholderCond(node.Children); ok {
			claimCond(callSiteOut.Signatures, cond, branch)
		}
		mergeSlotOutput(&out, callSiteOut)
	}
	claimOwnerLinks(out.Signatures, node, ownCount)
	if node.Boundary {
		e.claimBoundaryThrows(node, errStart, &out)
		if len(node.CallSiteSlots) == 0 {
			// A boundary built in its fallback state has no children to splice.
			out.HTML = strings.ReplaceAll(out.HTML, "<!--__children__-->", "")
			out.Templates = strings.ReplaceAll(out.Templates, "<!--__children__-->", "")
		}
	}

	childIDs := collectChildIDs(node.Children)
	slotBindings := collectSlotBindings(node.Children)
	// Dynamic slots in call-site children (e.g. a Toggle's {pressed() ? "On" :
	// "Off"} text) reference the CALLER's signals, not this component's scope,
	// so they're collected as orphans and attached to the owning signature
	// during post-processing in Emit().
	if len(node.CallSiteSlots) > 0 {
		owner := irtree.SlotID("")
		if len(e.clientStack) > 1 {
			owner = e.clientStack[len(e.clientStack)-2]
		}
		for _, b := range collectSlotBindings(node.CallSiteSlots) {
			out.Orphans = append(out.Orphans, orphanBinding{owner: owner, bind: b})
		}
	}
	sig := irtree.ComponentSignature{
		ComponentID:     node.ID,
		Tier:            node.Tier,
		Signals:         node.Signals,
		Handlers:        node.Handlers,
		RefBindings:     node.RefBindings,
		Effects:         node.Effects,
		Memos:           node.Memos,
		ExtraVars:       node.ExtraVars,
		PreSignalVars:   node.PreSignalVars,
		FuncPropAliases: node.FuncPropAliases,
		IslandBoot:      node.IslandBoot,
		BodyUses:        node.BodyUses,
		Children:        childIDs,
		SlotBindings:    slotBindings,
		AttrBindings:    node.AttrBindings,
		Name:            node.Name,
		Class:           node.Class,
		Lazy:            node.Lazy,
	}
	if node.Lazy && node.LazyLoader == "" {
		sig.LazyName = node.Name
	}
	if len(e.clientStack) > 1 {
		sig.ParentID = e.clientStack[len(e.clientStack)-2]
	}
	out.Signatures = append([]irtree.ComponentSignature{sig}, out.Signatures...)

	return out
}

// emitRuntime - runtime component splice marker

func (e *Emitter) emitRuntime(node *irtree.ComponentNode) SlotOutput {
	id := "region-" + escape.Comment(string(node.ID))
	out := SlotOutput{
		HTML: `<!--region:` + id + `--><!--/region:` + id + `-->`,
	}
	// Runtime Go components are baked as a region; their (static) children are
	// rendered now and passed to the host as the `children` prop per request.
	// The HTML is returned in RuntimeChildren instead of being written into the
	// shared IR, so emit stays pure and safe to run concurrently or twice.
	if node.Runtime == "go" && len(node.Children) > 0 {
		childOut := e.emitSlotsParallel(node.Children)
		if html := childOut.inlineHTML(); html != "" {
			out.RuntimeChildren = map[string]string{"region-" + string(node.ID): html}
		}
	}
	return out
}

// mergeRuntimeChildren merges src into dst, allocating dst when needed.
func mergeRuntimeChildren(dst, src map[string]string) map[string]string {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]string, len(src))
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// emitGoComponentSlot renders an imported Go component's children, records a
// build-time render request, and emits a marker the build replaces with the
// host's HTML.
func (e *Emitter) emitGoComponentSlot(s *irtree.GoComponentSlot) SlotOutput {
	childOut := e.emitSlotsParallel(s.Children)
	id := string(s.ID)
	props := goPropsJSON(s.Props, childOut.inlineHTML())
	return SlotOutput{
		HTML:       "<!--krate-gocomp:" + escape.Comment(id) + "-->",
		Signatures: childOut.Signatures,
		HeadHTML:   childOut.HeadHTML,
		ScriptHTML: childOut.ScriptHTML,
		StyleHTML:  childOut.StyleHTML,
		GoRenders: append(childOut.GoRenders, irtree.GoRenderRequest{
			ID:     id,
			Name:   s.Ref.Func,
			Props:  props,
			CallID: irtree.GoCallID(s.Pos.Line, s.Pos.Col, s.Ref.Func),
		}),
	}
}

// goPropsJSON merges the JSON-encoded attr props with the rendered children
// under the `children` key and returns a JSON object (keys sorted for
// determinism). Prop values are expected to be JSON already; a value that is
// not valid JSON is encoded as a JSON string, so the result is always valid.
func goPropsJSON(props map[string]string, childrenHTML string) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	first := true
	write := func(k string, v []byte) {
		if !first {
			b.WriteByte(',')
		}
		first = false
		kj, _ := json.Marshal(k)
		b.Write(kj)
		b.WriteByte(':')
		b.Write(v)
	}
	for _, k := range keys {
		if k == "children" && childrenHTML != "" {
			continue
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(props[k])); err == nil && compact.Len() > 0 {
			write(k, compact.Bytes())
			continue
		}
		enc, _ := json.Marshal(props[k])
		write(k, enc)
	}
	if childrenHTML != "" {
		enc, _ := json.Marshal(childrenHTML)
		write("children", enc)
	}
	b.WriteByte('}')
	return b.String()
}

// emitMetaSlot - Head/Script/Style content routing
func (e *Emitter) emitMetaSlot(s *irtree.MetaSlot) SlotOutput {
	var out SlotOutput
	for _, child := range s.Children {
		mergeSlotOutput(&out, e.emitSlotNode(child))
	}
	// <Head> children are routed into the document <head> by walkMetaSlots; they
	// must not also render into the body. <Script>/<Style> keep their body HTML
	// (valid there, and captured separately into Script/Style fields).
	if strings.EqualFold(s.ComponentName, "Head") || s.ComponentName == irtree.HoistMeta {
		out.HTML = ""
	}
	return out
}

// emitSlotNode - type-switch dispatch for SlotNode

func (e *Emitter) emitSlotNode(slot irtree.SlotNode) SlotOutput {
	if slot == nil {
		return SlotOutput{}
	}
	switch s := slot.(type) {
	case *irtree.StaticHTML:
		return e.emitStaticHTML(s)
	case *irtree.TextSlot:
		return e.emitTextSlot(s)
	case *irtree.ExprSlot:
		return e.emitExprSlot(s)
	case *irtree.ConditionalSlot:
		return e.emitConditionalSlot(s)
	case *irtree.ListSlot:
		return e.emitListSlot(s)
	case *irtree.ComponentSlot:
		return e.emitComponentSlot(s)
	case *irtree.GoComponentSlot:
		return e.emitGoComponentSlot(s)
	case *irtree.SuspenseSlot:
		return e.emitSuspenseSlot(s)
	case *irtree.MetaSlot:
		return e.emitMetaSlot(s)
	case *irtree.ChildrenSlot:
		return SlotOutput{HTML: "<!--__children__-->"}
	default:
		// Unknown IR slot node kind - error instead of silently dropping output.
		e.addErr("slot node type %T is not supported", slot)
		return SlotOutput{}
	}
}

// Slot emitters

func (e *Emitter) emitStaticHTML(s *irtree.StaticHTML) SlotOutput {
	return SlotOutput{HTML: s.HTML}
}

// mergeSlotOutput concatenates a child output into an accumulator in document
// order, including hoisted conditional templates and runtime-region children.
func mergeSlotOutput(acc *SlotOutput, out SlotOutput) {
	acc.HTML += out.HTML
	acc.Signatures = append(acc.Signatures, out.Signatures...)
	acc.HeadHTML += out.HeadHTML
	acc.ScriptHTML += out.ScriptHTML
	acc.StyleHTML += out.StyleHTML
	acc.Orphans = append(acc.Orphans, out.Orphans...)
	acc.GoRenders = append(acc.GoRenders, out.GoRenders...)
	acc.Templates += out.Templates
	acc.RuntimeChildren = mergeRuntimeChildren(acc.RuntimeChildren, out.RuntimeChildren)
	acc.BoundaryThrows = append(acc.BoundaryThrows, out.BoundaryThrows...)
}

// emitWorkers bounds the number of emit goroutines running at once across the
// whole process (nested parallel emits included). A slot that cannot get a
// worker slot is emitted inline by the calling goroutine, so nesting can never
// deadlock waiting for a worker.
var emitWorkers = make(chan struct{}, runtime.GOMAXPROCS(0))

// emitSlotsParallel emits sibling slots concurrently using fresh sub-emitters.
// Each sub-emitter carries its own client-stack, orphan list, and meta
// accumulators, so no shared state is ever mutated concurrently (emit never
// writes to the IR). After the goroutines finish the results are merged back
// in document order, keeping output byte-for-byte deterministic. Lists below
// parallelMinChildren are emitted sequentially to avoid goroutine overhead.
func (e *Emitter) emitSlotsParallel(slots []irtree.SlotNode) SlotOutput {
	var merged SlotOutput
	if len(slots) == 0 {
		return merged
	}
	if len(slots) < parallelMinChildren {
		for _, slot := range slots {
			mergeSlotOutput(&merged, e.emitSlotNode(slot))
		}
		return merged
	}

	outputs := make([]SlotOutput, len(slots))
	subErrs := make([][]error, len(slots))
	base := append([]irtree.SlotID{}, e.clientStack...)
	emitOne := func(idx int) {
		sub := NewEmitter()
		sub.functions = e.functions
		sub.IconResolver = e.IconResolver
		sub.EvalJS = e.EvalJS
		sub.GoComponents = e.GoComponents
		sub.goCollector = e.goCollector
		// Propagate build-wide tables so SSREval in this branch folds cva
		// variants and context defaults exactly like the sequential path.
		sub.cvaFactories = e.cvaFactories
		sub.contextDefaults = e.contextDefaults
		sub.moduleConsts = e.moduleConsts
		sub.CodeTheme = e.CodeTheme
		sub.clientStack = append([]irtree.SlotID{}, base...)
		outputs[idx] = sub.emitSlotNode(slots[idx])
		subErrs[idx] = sub.errs
	}
	var wg sync.WaitGroup
	for i := range slots {
		select {
		case emitWorkers <- struct{}{}:
			wg.Add(1)
			go func(idx int) {
				defer func() {
					<-emitWorkers
					wg.Done()
				}()
				emitOne(idx)
			}(i)
		default:
			emitOne(i)
		}
	}
	wg.Wait()

	for i, out := range outputs {
		mergeSlotOutput(&merged, out)
		// Diagnostics raised in a parallel branch must still fail the build.
		e.errs = append(e.errs, subErrs[i]...)
	}
	return merged
}

// collectChildrenOrphans registers orphan bindings for conditional/list slots
// among direct call-site children. These reference the CALLER's signals, so
// the owner is the enclosing client component (or empty for the signal-based
// fallback in findSignatureOwner).
func collectChildrenOrphans(children []irtree.SlotNode, owner irtree.SlotID) []orphanBinding {
	var orphans []orphanBinding
	for _, slot := range children {
		if cs, ok := slot.(*irtree.ConditionalSlot); ok {
			orphans = append(orphans, orphanBinding{
				owner: owner,
				bind: irtree.SlotBinding{
					SlotID:  cs.ID,
					Type:    "conditional",
					ExprJS:  cs.TestJS,
					Signals: cs.Signals,
				},
			})
		}
		if ls, ok := slot.(*irtree.ListSlot); ok {
			orphans = append(orphans, orphanBinding{
				owner: owner,
				bind: irtree.SlotBinding{
					SlotID:  ls.ID,
					Type:    "list",
					ExprJS:  ls.ExprSource,
					Signals: ls.Signals,
					List:    ls.Bind,
				},
			})
		}
	}
	return orphans
}

func (e *Emitter) emitTextSlot(s *irtree.TextSlot) SlotOutput {
	id := string(s.ID)
	initial := s.Initial
	html := slotMarker(id) + escape.Text(initial) + slotEndMarker(id)
	return SlotOutput{HTML: html}
}

func (e *Emitter) emitExprSlot(s *irtree.ExprSlot) SlotOutput {
	id := string(s.ID)
	// ExprSlot initials come from static evaluation of string/template/binary
	// expressions (JSX-producing expressions route to ListSlot/ConditionalSlot),
	// so they are always text and must be HTML-escaped to prevent template
	// literals like `return <h1>x</h1>` from leaking real markup into the page.
	html := slotMarker(id) + escape.Text(s.Initial) + slotEndMarker(id)
	return SlotOutput{HTML: html}
}

// slotMarker and slotEndMarker render the comment markers that delimit a
// dynamic slot. The id is comment-encoded so it can never close the comment.
func slotMarker(id string) string    { return "<!--k:" + escape.Comment(id) + "-->" }
func slotEndMarker(id string) string { return "<!--/k:" + escape.Comment(id) + "-->" }

// emitConditionalSlot renders a reactive conditional as comment markers around
// the ACTIVE branch only:
//
//	<!--k:ID-->active branch<!--/k:ID-->
//
// No wrapper element is emitted, so a conditional is valid anywhere (inside
// <table>, <tbody>, <tr>, <ul>, <ol>, <select>, <p>) and CSS child selectors
// see the real children. The inactive branch is rendered into a
//
//	<template data-kc="ID" data-kb="c|a">inactive branch</template>
//
// that is hoisted to the end of the page (SlotOutput.Templates), where
// data-kb names the branch it holds (c = consequent, a = alternate). The
// hydration bootstrap binds slots inside template content too, and kbindCond
// swaps the branch nodes between the markers and a detached fragment, so
// handlers and nested bindings of both branches survive toggling.
func (e *Emitter) emitConditionalSlot(s *irtree.ConditionalSlot) SlotOutput {
	id := string(s.ID)
	var cons, alt SlotOutput
	for _, child := range s.Consequent {
		mergeSlotOutput(&cons, e.emitSlotNode(child))
	}
	for _, child := range s.Alternate {
		mergeSlotOutput(&alt, e.emitSlotNode(child))
	}
	activeHTML, inactiveHTML, held := cons.HTML, alt.HTML, "a"
	if !s.InitialActive {
		activeHTML, inactiveHTML, held = alt.HTML, cons.HTML, "c"
	}
	cons.HTML, alt.HTML = "", ""
	tagCondBranch(cons.Signatures, s.ID, "c", s.InitialActive)
	tagCondBranch(alt.Signatures, s.ID, "a", !s.InitialActive)

	claimCond(cons.Signatures, s.ID, "c")
	claimCond(alt.Signatures, s.ID, "a")

	var out SlotOutput
	mergeSlotOutput(&out, cons)
	mergeSlotOutput(&out, alt)
	out.HTML = slotMarker(id) + activeHTML + slotEndMarker(id)
	out.Templates = `<template data-kc="` + escape.HTMLAttr(id) + `" data-kb="` + held + `">` + inactiveHTML + `</template>` + out.Templates
	return out
}

// emitListSlot renders a list as comment markers around its rows:
//
//	<!--k:ID--><!--k:ID:KEY-->row<!--/k:ID:KEY-->...<!--/k:ID-->
//
// KEY is "$" plus the row's key for keyed rows (comment-encoded) or the index,
// so hydration adopts each server-rendered row by key. Comments are valid in
// every content model (<tbody>, <select>, <ul>), so lists work anywhere.
// Signatures and orphan bindings from the rows are tagged with the list ID;
// Emit drops them when the list itself is bound by the list runtime.
func (e *Emitter) emitListSlot(s *irtree.ListSlot) SlotOutput {
	id := string(s.ID)
	var itemsHTML strings.Builder
	var out SlotOutput

	for _, item := range s.Items {
		itemID := id + ":" + item.Key
		itemsHTML.WriteString(slotMarker(itemID))
		for _, child := range item.Contents {
			childOut := e.emitSlotNode(child)
			itemsHTML.WriteString(childOut.HTML)
			childOut.HTML = ""
			if s.Bind != nil {
				for i := range childOut.Signatures {
					childOut.Signatures[i].InLists = append(childOut.Signatures[i].InLists, s.ID)
				}
				for i := range childOut.Orphans {
					childOut.Orphans[i].lists = append(childOut.Orphans[i].lists, s.ID)
				}
			}
			mergeSlotOutput(&out, childOut)
		}
		itemsHTML.WriteString(slotEndMarker(itemID))
	}

	out.HTML = slotMarker(id) + itemsHTML.String() + slotEndMarker(id)
	for i := range out.Signatures {
		if out.Signatures[i].ListSlot == "" {
			out.Signatures[i].ListSlot = s.ID
		}
	}
	return out
}

// tagCondBranch records that the components in sigs render inside one branch
// of conditional slot id (outer conditionals are prepended later, so Conds
// ends up outermost first).
func tagCondBranch(sigs []irtree.ComponentSignature, id irtree.SlotID, branch string, shown bool) {
	for i := range sigs {
		sigs[i].Conds = append([]irtree.CondBranch{{SlotID: id, Branch: branch, InitiallyShown: shown}}, sigs[i].Conds...)
	}
}

func (e *Emitter) emitComponentSlot(s *irtree.ComponentSlot) SlotOutput {
	return e.emitNode(s.Component)
}

// EmitMeta - extract Head/Script/Style content from tree

// EmitMeta walks the tree and extracts Head/Script/Style content,
// routing MetaSlot children to the appropriate EmitResult fields.
func EmitMeta(tree *irtree.ComponentTree, result *EmitResult) {
	if tree == nil || tree.Root == nil {
		return
	}
	walkMetaSlots(tree.Root, result)
}

// walkMetaSlots recursively walks ComponentNode children looking for MetaSlots.
// A component's own-return <Head>/<Script>/<Style> is captured by SSREval while
// the component's return JSX is evaluated (into EmitResult via the emitter's
// headHTML/scriptHTML/styleHTML accumulators). For the page ROOT, node.Children
// ARE that own-return tree, so walking them here would append the same content a
// second time - and, when the root was SSREval'd with injected prop bindings
// (generateStaticParams params, dynamic-route sentinels), with stale
// pre-binding values. Child SSREval nodes keep their own-return tree in
// ReturnSlots, so their Children are CALL-SITE slots, which SSREval does not
// capture - a call-site <Head> passed into a signal-less wrapper must still be
// walked. Non-evaluated nodes are never captured by SSREval, so their own-return
// Children are walked as before.
func walkMetaSlots(node *irtree.ComponentNode, result *EmitResult) {
	if node == nil {
		return
	}
	// Root-like SSREval node: Children are the component's own return, already
	// captured during SSREval. Any SSREval node with a call site has
	// CallSiteChildren set, so this identifies the entry/root node.
	skipOwnReturn := node.IsSSREval && node.CallSiteChildren == nil
	if !skipOwnReturn {
		for _, child := range node.Children {
			if meta, ok := child.(*irtree.MetaSlot); ok {
				var childHTML strings.Builder
				tmp := NewEmitter()
				for _, c := range meta.Children {
					out := tmp.emitSlotNode(c)
					childHTML.WriteString(out.inlineHTML())
				}
				switch strings.ToLower(meta.ComponentName) {
				case irtree.HoistMeta:
					// Hoisted React 19 metadata; identical elements (the same
					// stylesheet or tag rendered twice) are emitted once.
					html := childHTML.String()
					if !strings.Contains(result.HeadHTML, html) {
						result.HeadHTML += html
					}
				case "head":
					result.HeadHTML += childHTML.String()
				case "script":
					result.ScriptHTML += childHTML.String()
				case "style":
					result.StyleHTML += childHTML.String()
				}
			}
		}
	}
	for _, child := range node.Children {
		if comp, ok := child.(*irtree.ComponentSlot); ok && comp.Component != nil {
			walkMetaSlots(comp.Component, result)
		}
	}
}

// Helpers

func collectChildIDs(children []irtree.SlotNode) []irtree.SlotID {
	var ids []irtree.SlotID
	for _, child := range children {
		if id := child.GetID(); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// collectSlotBindings walks slot children and collects binding info for
// text slots, expression slots, and conditional slots so the hydrate
// code can generate createEffect calls to connect signals to DOM.
func collectSlotBindings(children []irtree.SlotNode) []irtree.SlotBinding {
	var bindings []irtree.SlotBinding
	for _, child := range children {
		switch s := child.(type) {
		case *irtree.TextSlot:
			bindings = append(bindings, irtree.SlotBinding{
				SlotID:  s.ID,
				Type:    "text",
				ExprJS:  s.Signal.Name + "()",
				Signals: []string{s.Signal.Name},
			})
		case *irtree.ExprSlot:
			bindings = append(bindings, irtree.SlotBinding{
				SlotID:  s.ID,
				Type:    "expr",
				ExprJS:  s.ExprSource,
				Signals: s.Signals,
			})
		case *irtree.ConditionalSlot:
			bindings = append(bindings, irtree.SlotBinding{
				SlotID:  s.ID,
				Type:    "conditional",
				ExprJS:  s.TestJS,
				Signals: s.Signals,
			})
			// Recurse into both branches so nested slots (text slots, nested
			// conditionals, lists) stay reactive after the conditional toggles.
			// The inactive branch lives in a <template data-kc> whose content
			// the bootstrap also binds, so both branches' nodes exist to bind
			// against.
			bindings = append(bindings, collectSlotBindings(s.Consequent)...)
			bindings = append(bindings, collectSlotBindings(s.Alternate)...)
		case *irtree.ListSlot:
			bindings = append(bindings, irtree.SlotBinding{
				SlotID:  s.ID,
				Type:    "list",
				ExprJS:  s.ExprSource,
				Signals: s.Signals,
				List:    s.Bind,
			})
		}
	}
	return bindings
}

// managedLists returns the IDs of list slots that will be bound by the keyed
// list runtime: list bindings with a runtime plan, either declared by a
// signature or carried by an orphan binding that has an owner.
func managedLists(sigs []irtree.ComponentSignature, orphans []orphanBinding) map[irtree.SlotID]bool {
	managed := map[irtree.SlotID]bool{}
	for _, sig := range sigs {
		if sig.Tier != irtree.TierClient {
			continue
		}
		for _, b := range sig.SlotBindings {
			if b.Type == "list" && b.List != nil {
				managed[b.SlotID] = true
			}
		}
	}
	for _, o := range orphans {
		if o.bind.Type == "list" && o.bind.List != nil && findSignatureOwner(sigs, o.owner, o.bind) >= 0 {
			managed[o.bind.SlotID] = true
		}
	}
	return managed
}

func inManagedList(lists []irtree.SlotID, managed map[irtree.SlotID]bool) bool {
	for _, id := range lists {
		if managed[id] {
			return true
		}
	}
	return false
}
