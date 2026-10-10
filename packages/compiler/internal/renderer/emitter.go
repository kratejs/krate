package renderer

import (
	"fmt"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
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

	// cvaFactories holds module-wide `const X = cva(...)` specs so variant calls
	// in prop-driven components fold to static class strings during SSREval.
	cvaFactories map[string]*irtree.CVASpec

	// contextDefaults holds module-wide `const X = createContext(v)` defaults so
	// `X.useContext()` folds during SSREval (no Provider exists at build time).
	contextDefaults map[string]string

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
	return &Emitter{}
}

// Emit walks the ComponentTree and produces an EmitResult.
func (e *Emitter) Emit(tree *irtree.ComponentTree) *EmitResult {
	e.functions = tree.Functions
	e.cvaFactories = tree.CVAFactories
	e.contextDefaults = tree.ContextDefaults
	output := e.emitNode(tree.Root)

	// Merge subtree-local metadata and orphans collected during emit.
	e.headHTML += output.HeadHTML
	e.scriptHTML += output.ScriptHTML
	e.styleHTML += output.StyleHTML
	e.orphans = append(e.orphans, output.Orphans...)

	// Attach orphan bindings (dynamic slots in call-site children that
	// reference a caller's signals) to the signature that owns them: first try
	// the client component that enclosed the collection site, then fall back to
	// the first signature declaring the binding's signals.
	if len(e.orphans) > 0 {
		for _, o := range e.orphans {
			if owner := findSignatureOwner(output.Signatures, o.owner, o.bind); owner >= 0 {
				output.Signatures[owner].SlotBindings = append(output.Signatures[owner].SlotBindings, o.bind)
			}
		}
	}

	// Collect client component functions referenced by dynamic list slots so
	// they can be emitted into the hydration scope. Without them, a .map()
	// body like <Toast/> compiles to h(Toast, ...) which throws ReferenceError.
	listComponents := collectListComponentFuncs(tree.Root, tree.Functions)

	return &EmitResult{
		HTML:           output.HTML,
		HeadHTML:       e.headHTML,
		ScriptHTML:     e.scriptHTML,
		StyleHTML:      e.styleHTML,
		Signatures:     output.Signatures,
		HasLinks:       tree.HasLinks,
		UsedFuncs:      make(map[string]bool),
		UsedCSS:        make(map[string]bool),
		ListComponents: listComponents,
		Errors:         e.errs,
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
	if e.cvaFactories != nil {
		eval.SetCVAFactories(e.cvaFactories)
	}
	if e.contextDefaults != nil {
		eval.SetContextDefaults(e.contextDefaults)
	}
	eval.SetCodeTheme(e.CodeTheme)
	if node.RestProps != nil && node.RestPropsName != "" {
		eval.SetRestProps(map[string]map[string]ast.Expr{node.RestPropsName: node.RestProps})
	}
	if node.SSREvalBindings != nil {
		eval.SetBindings(node.SSREvalBindings)
	}
	if e.IconResolver != nil {
		eval.iconEmit = func(name string, attrs []*ast.JSXAttr) (string, bool) {
			return e.IconResolver(name, attrs)
		}
	}
	// Bind local variables (var className = ..., etc.) so return-statement
	// evaluation resolves locals derived from props.
	if node.Fn != nil {
		eval.BindLocalVars(node.Fn.Body)
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
			bindings := eval.bindings
			if bindings == nil {
				bindings = make(map[string]string)
			}
			bindings["children"] = childrenHTML.String()
			eval.SetBindings(bindings)
			// Children were rendered through the slot pipeline (text escaped,
			// elements hydrated) - a `{children}` container must inject them raw.
			eval.childrenIsHTML = true
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
			out := e.emitComponentSlot(slot)
			sigs = append(sigs, out.Signatures...)
			return out.HTML, true
		}
		defer func() { eval.interactiveEmit = nil }()
	}

	// Resolve the component's return value. Early if/else-if returns (each
	// branch ending in `return <jsx/>`, with or without a wrapping block) are
	// resolved first so prop-driven components whose return depends on a
	// member value (e.g. <a> vs <div>) render the right branch. Otherwise the
	// trailing top-level return is evaluated.
	var retHTML string
	if v, ok := eval.evalBranchReturns(node.Fn.Body); ok {
		retHTML = v
	} else if ret := findReturnStmtIn(node.Fn.Body); ret != nil && ret.Value != nil {
		retHTML = eval.Eval(ret.Value)
	}
	html := retHTML
	e.errs = append(e.errs, eval.Errors()...)
	out.HeadHTML += eval.HeadHTML
	out.ScriptHTML += eval.ScriptHTML
	out.StyleHTML += eval.StyleHTML
	out.HTML = html
	out.Signatures = sigs

	return out
}

// findReturnStmtIn finds the return statement in a function body.
func findReturnStmtIn(body []ast.Stmt) *ast.ReturnStmt {
	for _, stmt := range body {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			return ret
		}
	}
	return nil
}

// emitServer - server component SSR

func (e *Emitter) emitServer(node *irtree.ComponentNode) SlotOutput {
	var out SlotOutput
	for _, child := range node.Children {
		mergeSlotOutput(&out, e.emitSlotNode(child))
	}
	wrapped := `<div data-krate-server="` + node.Name + `">` + out.HTML + `</div>`
	out.HTML = wrapped
	return out
}

// emitClient - client component SSR + scoped hydration metadata

func (e *Emitter) emitClient(node *irtree.ComponentNode) SlotOutput {
	var out SlotOutput

	e.clientStack = append(e.clientStack, node.ID)
	defer func() { e.clientStack = e.clientStack[:len(e.clientStack)-1] }()

	childrenOut := e.emitSlotsParallel(node.Children)
	out.HTML = childrenOut.HTML
	out.Signatures = append(out.Signatures, childrenOut.Signatures...)
	out.Orphans = append(out.Orphans, childrenOut.Orphans...)
	out.HeadHTML += childrenOut.HeadHTML
	out.ScriptHTML += childrenOut.ScriptHTML
	out.StyleHTML += childrenOut.StyleHTML

	// Replace {props.children} placeholder with rendered call site children.
	// This ensures interactive sub-components nested inside signal components
	// retain their data-k/data-kh hydration markers.
	if strings.Contains(out.HTML, "<!--__children__-->") && len(node.CallSiteSlots) > 0 {
		callSiteOut := e.emitSlotsParallel(node.CallSiteSlots)
		out.HTML = strings.Replace(out.HTML, "<!--__children__-->", callSiteOut.HTML, 1)
		out.Signatures = append(out.Signatures, callSiteOut.Signatures...)
		out.Orphans = append(out.Orphans, callSiteOut.Orphans...)
		out.HeadHTML += callSiteOut.HeadHTML
		out.ScriptHTML += callSiteOut.ScriptHTML
		out.StyleHTML += callSiteOut.StyleHTML
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
		BodyUses:        node.BodyUses,
		Children:        childIDs,
		SlotBindings:    slotBindings,
		AttrBindings:    node.AttrBindings,
	}
	out.Signatures = append([]irtree.ComponentSignature{sig}, out.Signatures...)

	return out
}

// emitRuntime - runtime component splice marker

func (e *Emitter) emitRuntime(node *irtree.ComponentNode) SlotOutput {
	// Standalone runtime components (not inside a <Suspense> boundary) become
	// their own dynamic region. The shell carries an empty splice slot keyed by
	// the region ID (region-<slotID>, matching the build's region registry); the
	// Go server asks the SSR sidecar to render the component and splices the
	// HTML in at request time. The component's build-time resolved props live in
	// the region registry (RegionMeta.Props), so nothing is serialized inline.
	id := "region-" + string(node.ID)
	return SlotOutput{
		HTML: fmt.Sprintf(`<!--region:%s--><!--/region:%s-->`, id, id),
	}
}

// emitMetaSlot - Head/Script/Style content routing

func (e *Emitter) emitMetaSlot(s *irtree.MetaSlot) SlotOutput {
	var out SlotOutput
	for _, child := range s.Children {
		mergeSlotOutput(&out, e.emitSlotNode(child))
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
// order. Runtime props are NOT merged here - only parallel emit re-keys them.
func mergeSlotOutput(acc *SlotOutput, out SlotOutput) {
	acc.HTML += out.HTML
	acc.Signatures = append(acc.Signatures, out.Signatures...)
	acc.HeadHTML += out.HeadHTML
	acc.ScriptHTML += out.ScriptHTML
	acc.StyleHTML += out.StyleHTML
	acc.Orphans = append(acc.Orphans, out.Orphans...)
}

// emitSlotsParallel emits sibling slots concurrently using fresh sub-emitters.
// Each sub-emitter carries its own client-stack, orphan list, and meta
// accumulators, so no shared state is ever mutated concurrently. After the
// goroutines finish the results are merged back in document order, keeping
// output byte-for-byte deterministic. Lists below parallelMinChildren are
// emitted sequentially to avoid goroutine overhead.
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
	var wg sync.WaitGroup
	for i := range slots {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sub := NewEmitter()
			sub.functions = e.functions
			sub.IconResolver = e.IconResolver
			sub.EvalJS = e.EvalJS
			// Propagate build-wide tables so SSREval in this branch folds cva
			// variants and context defaults exactly like the sequential path.
			sub.cvaFactories = e.cvaFactories
			sub.contextDefaults = e.contextDefaults
			sub.CodeTheme = e.CodeTheme
			sub.clientStack = append([]irtree.SlotID{}, base...)
			outputs[idx] = sub.emitSlotNode(slots[idx])
			subErrs[idx] = sub.errs
		}(i)
	}
	wg.Wait()

	for i, out := range outputs {
		merged.HTML += out.HTML
		merged.Signatures = append(merged.Signatures, out.Signatures...)
		merged.HeadHTML += out.HeadHTML
		merged.ScriptHTML += out.ScriptHTML
		merged.StyleHTML += out.StyleHTML
		merged.Orphans = append(merged.Orphans, out.Orphans...)
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
				},
			})
		}
	}
	return orphans
}

func (e *Emitter) emitTextSlot(s *irtree.TextSlot) SlotOutput {
	id := string(s.ID)
	initial := s.Initial
	html := fmt.Sprintf("<!--k:%s-->%s<!--/k:%s-->", id, escape.HTML(initial), id)
	return SlotOutput{HTML: html}
}

func (e *Emitter) emitExprSlot(s *irtree.ExprSlot) SlotOutput {
	id := string(s.ID)
	// ExprSlot initials come from static evaluation of string/template/binary
	// expressions (JSX-producing expressions route to ListSlot/ConditionalSlot),
	// so they are always text and must be HTML-escaped to prevent template
	// literals like `return <h1>x</h1>` from leaking real markup into the page.
	html := fmt.Sprintf("<!--k:%s-->%s<!--/k:%s-->", id, escape.HTML(s.Initial), id)
	return SlotOutput{HTML: html}
}

func (e *Emitter) emitConditionalSlot(s *irtree.ConditionalSlot) SlotOutput {
	id := string(s.ID)
	var b strings.Builder
	b.WriteString("<!--k:")
	b.WriteString(id)
	b.WriteString("-->")
	// Render both branches, wrapped in togglable wrappers. The hydration
	// effect toggles their display based on the test, so the SSR shows the
	// initially-active branch and interactivity switches between them.
	consequentHTML, alternateHTML := "", ""
	var allSignatures []irtree.ComponentSignature
	var allOrphans []orphanBinding
	var childMeta, childScript, childStyle string
	for _, child := range s.Consequent {
		out := e.emitSlotNode(child)
		consequentHTML += out.HTML
		allSignatures = append(allSignatures, out.Signatures...)
		allOrphans = append(allOrphans, out.Orphans...)
		childMeta += out.HeadHTML
		childScript += out.ScriptHTML
		childStyle += out.StyleHTML
	}
	for _, child := range s.Alternate {
		out := e.emitSlotNode(child)
		alternateHTML += out.HTML
		allSignatures = append(allSignatures, out.Signatures...)
		allOrphans = append(allOrphans, out.Orphans...)
		childMeta += out.HeadHTML
		childScript += out.ScriptHTML
		childStyle += out.StyleHTML
	}
	consequentStyle, alternateStyle := "", ` style="display:none"`
	if !s.InitialActive {
		consequentStyle, alternateStyle = ` style="display:none"`, ""
	}
	b.WriteString("<div data-krate-cond-w")
	b.WriteString(consequentStyle)
	b.WriteString(">")
	b.WriteString(consequentHTML)
	b.WriteString("</div><div data-krate-cond-w")
	b.WriteString(alternateStyle)
	b.WriteString(">")
	b.WriteString(alternateHTML)
	b.WriteString("</div><!--/k:")
	b.WriteString(id)
	b.WriteString("-->")
	return SlotOutput{
		HTML:       b.String(),
		Signatures: allSignatures,
		HeadHTML:   childMeta,
		ScriptHTML: childScript,
		StyleHTML:  childStyle,
		Orphans:    allOrphans,
	}
}

func (e *Emitter) emitListSlot(s *irtree.ListSlot) SlotOutput {
	id := string(s.ID)
	var itemsHTML strings.Builder
	var out SlotOutput

	for _, item := range s.Items {
		itemsHTML.WriteString(fmt.Sprintf("<!--k:%s:%s-->", id, item.Key))
		for _, child := range item.Contents {
			childOut := e.emitSlotNode(child)
			itemsHTML.WriteString(childOut.HTML)
			out.Signatures = append(out.Signatures, childOut.Signatures...)
			out.Orphans = append(out.Orphans, childOut.Orphans...)
			out.HeadHTML += childOut.HeadHTML
			out.ScriptHTML += childOut.ScriptHTML
			out.StyleHTML += childOut.StyleHTML
		}
		itemsHTML.WriteString(fmt.Sprintf("<!--/k:%s:%s-->", id, item.Key))
	}

	html := fmt.Sprintf("<!--k:%s-->%s<!--/k:%s-->", id, itemsHTML.String(), id)
	out.HTML = html
	return out
}

func (e *Emitter) emitComponentSlot(s *irtree.ComponentSlot) SlotOutput {
	return e.emitNode(s.Component)
}

func (e *Emitter) emitSuspenseSlot(s *irtree.SuspenseSlot) SlotOutput {
	var out SlotOutput
	for _, child := range s.Fallback {
		mergeSlotOutput(&out, e.emitSlotNode(child))
	}

	// ModeStatic: the primary was resolved at build time - bake it inside the
	// markers so the shell contains the real resolved content (no streaming
	// round-trip needed). ModeRegion/Default keep the fallback baked in.
	inner := out.HTML
	if s.Mode == irtree.SuspenseModeStatic {
		inner = ""
		for _, child := range s.Resolved {
			inner += e.emitSlotNode(child).HTML
		}
	}

	id := s.StreamID
	html := fmt.Sprintf(
		`<!--suspense:%s-->%s<!--/suspense:%s-->`,
		id, inner, id,
	)
	out.HTML = html
	return out
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
					childHTML.WriteString(out.HTML)
				}
				switch strings.ToLower(meta.ComponentName) {
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
			// The emitter renders both branches (render-both + visibility
			// toggle), so their nodes exist in the DOM to bind against.
			bindings = append(bindings, collectSlotBindings(s.Consequent)...)
			bindings = append(bindings, collectSlotBindings(s.Alternate)...)
		case *irtree.ListSlot:
			bindings = append(bindings, irtree.SlotBinding{
				SlotID:  s.ID,
				Type:    "list",
				ExprJS:  s.ExprSource,
				Signals: s.Signals,
			})
		}
	}
	return bindings
}
