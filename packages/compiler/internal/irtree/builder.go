package irtree

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"krate-compiler/internal/ast"
	"krate-compiler/internal/escape"
	"krate-compiler/internal/sigutil"
	"krate-compiler/internal/syntaxhighlight"
)

// Build constructs a ComponentTree from a parsed program and its annotations.
func Build(prog *ast.Program, ann *Annotations) *ComponentTree {
	entryFn := ann.Functions[ann.EntryPoint]
	if entryFn == nil {
		return &ComponentTree{
			Root:         &ComponentNode{Name: "_empty", Tier: TierStatic},
			RuntimeStore: NewRuntimePropStore(),
		}
	}

	builder := &builder{
		functions:      ann.Functions,
		ann:            ann,
		idCounter:      make(map[string]int),
		instanceCounts: make(map[string]int),
		elementCounts:  make(map[string]int),
		slotIDMap:      make(map[string]SlotID),
		slotCounts:     make(map[string]int),
		moduleConsts:   collectModuleConsts(prog),
	}

	root := builder.buildComponentNode(entryFn, "")
	root.SourceFile = ann.SourceFile

	return &ComponentTree{
		Root:         root,
		HasLinks:     builder.hasLinks,
		RuntimeStore: builder.runtimeProps,
		Functions:    ann.Functions,
	}
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
	slotIDMap        map[string]SlotID   // logical ID → compact ID
	pendingHandlers  []HandlerDecl       // accumulated during buildSlotNodes
	pendingAttrs     []AttrBinding       // accumulated during buildSlotNodes
	pendingPropsRegs []string            // __krate_props["id"]={...} registrations for child components
	slotCounts       map[string]int      // per-parent dynamic-slot counters for sibling disambiguation
	localFnBody      []ast.Stmt          // body of current component function for handler resolution
	localSignals     map[string]ast.Expr // component-local signal context (name → initial expr)
	localProps       map[string]string   // component-local resolved props (name → value)
	callSiteChildren []ast.JSXChild      // call-site children of the current component
	moduleConsts     map[string]string   // module-level const values (name → resolved literal)
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
	compact := SlotID(toBase62(b.slotCounter))
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

// ─── buildComponentNode ────────────────────────────────────────────────────

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

	node.Signals = b.collectSignalDecls(fn.Body)
	node.BodyUses = b.collectBodySignalUses(fn.Body, node.Signals)

	// Client-only: effects, memos, extra vars
	if tier == TierClient {
		node.InstanceID = deriveInstanceID(string(id))
		node.Effects = b.collectEffectJS(fn.Body)
		node.Memos = b.collectMemoJS(fn.Body)
		node.ExtraVars = b.collectExtraVarJS(fn.Body)
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

	// Save parent's pending props registrations for the same reason
	savedPropsRegs := b.pendingPropsRegs
	b.pendingPropsRegs = nil

	// Set local function body context for handler resolution
	savedLocalFnBody := b.localFnBody
	b.localFnBody = fn.Body

	// Set component-local signal context: component's own signals take
	// precedence over the global annotation map so signal reads resolve to
	// the correct initial value even with name collisions across components.
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

	// Set component-local props: resolved call-site attribute values so that
	// prop reads (props.X and bare param identifiers) resolve during the walk.
	// buildComponentSlot populates b.localProps before invoking this method.
	savedLocalProps := b.localProps

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

	// Resolve component local variables (var lang = props.lang || "", etc.) to
	// build-time constants. Folded into localProps for SSR initial evaluation
	// and emitted as var declarations in the hydration bundle so bindings that
	// reference them don't throw ReferenceError.
	if b.localProps != nil {
		locals := collectLocalVars(fn.Body, b.sigMap(), b.localProps)
		if len(locals) > 0 {
			for k, v := range locals {
				if _, exists := b.localProps[k]; !exists {
					b.localProps[k] = v
				}
			}
			if tier == TierClient {
				declared := declaredLocalNames(node, fn)
				for _, name := range sortedKeys(locals) {
					if declared[name] {
						continue
					}
					node.ExtraVars = append(node.ExtraVars, "var "+name+"="+jsLiteralFor(locals[name]))
				}
			}
		}
	}

	// Find return statement and build children
	// Handlers are accumulated in b.pendingHandlers during this walk
	returnStmt := findReturnStmt(fn.Body)
	if returnStmt != nil && returnStmt.Value != nil {
		node.Children = b.buildSlotNodes(returnStmt.Value, string(id))
	}

	// Restore context
	b.localFnBody = savedLocalFnBody
	b.localSignals = savedLocalSignals
	b.localProps = savedLocalProps

	// Read accumulated handlers from slot building walk
	if tier == TierClient {
		node.Handlers = b.pendingHandlers
		node.AttrBindings = b.pendingAttrs
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
			len(node.Handlers) == 0 && len(node.AttrBindings) == 0 {
			tier = TierStatic
			node.Tier = TierStatic
		}
	}
	b.pendingHandlers = savedHandlers
	b.pendingAttrs = savedAttrs
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
	for _, stmt := range body {
		if fn, ok := stmt.(*ast.FnDecl); ok {
			candidates[fn.Name] = fn
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

	for _, h := range node.Handlers {
		scan(h.Body)
	}
	for _, eff := range node.Effects {
		scan(eff)
	}
	for _, memo := range node.Memos {
		scan(memo)
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

// ─── buildSlotNodes — dispatch by expression type ─────────────────────────

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

// buildCallExprSlots handles call expressions — detects .map() calls and falls back to ExprSlot.
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

// ─── buildJSXSlot — lowercase HTML or uppercase component ──────────────────

func (b *builder) buildJSXSlot(el *ast.JSXElement, parentID string) []SlotNode {
	name := el.Opening.Name

	// Special components — route to metadata output
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
	}

	// Uppercase = component
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		if slots := b.buildComponentSlot(el, parentID); len(slots) > 0 {
			return slots
		}
		// Unknown component — render as empty span
		return []SlotNode{&StaticHTML{HTML: "<span></span>"}}
	}

	// Lowercase = HTML element
	return b.buildStaticElementSlots(el, parentID)
}

// ─── buildLinkSlots — <Link> → <a> with SPA navigation ────────────────────

// buildLinkSlots compiles <Link> into a real <a> element wired for client-side
// navigation (data-krate-link) with Next.js-style props:
//
//   - prefetch (default true)  → data-prefetch (hover + viewport prefetch)
//   - replace (default false)  → data-krate-replace (history.replaceState)
//   - scroll  (default true)   → data-krate-scroll="false" disables scroll-to-top
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
		val := evalConstWithSignals(attr.Value, b.sigMap(), b.localProps)
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

// ─── buildSyntaxHighlightSlots — <SyntaxHighlight> → chroma-highlighted HTML ──

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
// elements) — the caller must then fall back to hydration-based rendering.
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
				// Unresolvable identifier — can't highlight at compile time.
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
			highlighted := syntaxhighlight.Highlight(codeStr, normalizedLang)
			fmt.Fprintf(&html, "<pre class=\"chroma\"><code class=\"language-%s\">%s</code></pre>", lang, highlighted)
		} else {
			escaped := escape.HTML(codeStr)
			fmt.Fprintf(&html, "<pre><code>%s</code></pre>", escaped)
		}
		return []SlotNode{&StaticHTML{HTML: html.String()}}
	}

	// Children are dynamic (e.g. {children} in a component body). Emit the
	// <pre><code> wrapper as static HTML, build children as normal slot nodes,
	// then close the tags. Chroma highlighting is skipped — the server stub
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

// ─── buildMetaSlot — Head/Script/Style content ──────────────────────────────

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
			// Meta content (Head/Script/Style) is intentionally raw HTML — a
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
		val := evalConstWithSignals(attr.Value, b.sigMap(), b.localProps)
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

// ─── buildComponentSlot — recursive component build ────────────────────────

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
				SourceFile:   b.ann.SourceFile,
				Line:         childFn.Position.Line,
			},
		}}
	}

	childSignals := b.collectSignalDecls(childFn.Body)
	attrs := extractPropsAST(el)
	if len(childSignals) == 0 {
		if !b.componentNeedsClient(childFn, attrs) {
			// Inline signal-derived props: a signal-less component that renders
			// a parent's signal value (e.g. <Display value={display()}/>) must
			// stay reactive, so its return JSX is inlined with the props
			// substituted by their call-site expressions.
			if b.componentHasSignalProps(attrs) {
				return b.inlineSignalPropsComponent(el, childFn, childID, attrs)
			}
			// Pure prop-driven component — mark for SSR evaluation at emit time.
			// Extract prop bindings from the JSX call site so the emitter can
			// evaluate the component's return statement with those bindings.
			paramNames := extractParamNames(childFn)
			bindings := buildPropBindings(paramNames, attrs, b.sigMap())

			childNode := &ComponentNode{
				ID:               childID,
				Name:             childName,
				Tier:             TierStatic,
				Fn:               childFn,
				SSREvalBindings:  bindings,
				IsSSREval:        true,
				CallSiteChildren: el.Children,
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
			if ret := findReturnStmt(childFn.Body); ret != nil && ret.Value != nil {
				childNode.ReturnSlots = b.buildSlotNodes(ret.Value, string(childID))
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
	b.localProps = make(map[string]string, len(attrs))
	for name, expr := range attrs {
		b.localProps[name] = evalConstWithSignals(expr, b.sigMap(), savedProps)
	}
	savedCallSite := b.callSiteChildren
	b.callSiteChildren = el.Children

	childNode := b.buildComponentNode(childFn, string(childID))
	b.localProps = savedProps
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
		childNode.SSREvalBindings = buildPropBindings(paramNames, attrs, b.sigMap())
	}

	// Hoist props for handlers that reference props.X. The props object
	// values may reference THIS component's signals (e.g. checked={checked1()}),
	// so it must be built in the parent's scope and shared with the child via
	// the __krate_props registry instead of inlining it into the child IIFE
	// (which would reference out-of-scope identifiers). Even with no call-site
	// props the child handlers may read props.X (e.g. props.onOpenChange), so
	// an empty {} registration is emitted to keep `props` defined.
	if childNode.Tier == TierClient && (len(childNode.Handlers) > 0 || len(childNode.Effects) > 0 || len(childNode.Memos) > 0) {
		if handlersOrLocalsReferenceProps(childNode.Handlers, childFn.Body) ||
			compiledRefsProps(childNode.Effects) || compiledRefsProps(childNode.Memos) {
			reg := buildPropsRegDecl(string(childNode.ID), childNode.Props, b.sigMap())
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

// ─── buildStaticElementSlots — HTML element, returns []SlotNode ───────────
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
		openingTag := b.buildElementOpening(el, nil, nil, string(id))
		return []SlotNode{&StaticHTML{HTML: openingTag + ">" + rawHTML + "</" + el.Opening.Name + ">"}}
	}

	// Process attributes: handlers, bindings, static attrs
	for _, attr := range el.Opening.Attributes {
		if attr.Spread {
			continue
		}
		if attr.Name == "ref" {
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

	openingTag := b.buildElementOpening(el, handlers, attrs, string(id))
	closingTag := "</" + el.Opening.Name + ">"

	// Self-closing: void HTML elements can use />, others need full </tagName>
	if el.Opening.SelfClosing {
		if isVoidElement(el.Opening.Name) {
			return []SlotNode{&StaticHTML{HTML: openingTag + " />"}}
		}
		return []SlotNode{&StaticHTML{HTML: openingTag + "></" + el.Opening.Name + ">"}}
	}

	// No dynamic children — everything fits in one StaticHTML
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

	// Has dynamic children — split into StaticHTML segments around them
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
func (b *builder) buildElementOpening(el *ast.JSXElement, handlers []HandlerDecl, attrs []AttrBinding, elementSlotID string) string {
	var buf strings.Builder
	name := el.Opening.Name

	buf.WriteByte('<')
	buf.WriteString(name)

	// Emit data-k attribute for elements with handlers or dynamic attribute bindings
	// so the hydration code can find them via querySelector.
	if len(handlers) > 0 || len(attrs) > 0 {
		buf.WriteString(` data-k="k:`)
		buf.WriteString(elementSlotID)
		buf.WriteByte('"')
	}

	// Static attributes
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Name == "ref" || attr.Name == "dangerouslySetInnerHTML" {
			continue
		}
		if isOnEvent(attr.Name) {
			continue
		}
		// Dynamic bindings are emitted below (with SSR initial + marker).
		// Statically-resolvable values — even those typed as bindings (e.g.
		// {String(i)}, {length}) — are emitted directly here instead.
		if isAttrBinding(attr) && !b.isStaticResolvable(attr.Value) {
			continue
		}
		if attr.Value != nil {
			val := evalConstWithSignals(attr.Value, b.sigMap(), b.localProps)
			// Boolean attributes: omit when falsy, emit bare name when true.
			if isBooleanAttr(attr.Name) {
				if val == "true" {
					buf.WriteByte(' ')
					buf.WriteString(ast.HTMLAttrName(attr.Name))
					buf.WriteString(`="true"`)
				}
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
		} else if a.Initial != "" {
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

// ─── buildExprContainerChildren — build slots from {expr} in JSX ──────────

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

	// Special: {props.children} — same children placeholder
	if mem, ok := ec.Expression.(*ast.MemberExpr); ok {
		if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == "props" {
			if pid, ok := mem.Property.(*ast.Identifier); ok && pid.Name == "children" {
				return []SlotNode{&ChildrenSlot{}}
			}
		}
	}

	// .map() call — always try to resolve at build time (even without signals)
	if isMapCall(ec.Expression) {
		return []SlotNode{b.buildListSlot(ec.Expression, parentID)}
	}

	// Ternary with JSX in branches: {cond() ? <A/> : <B/>}
	// This must be checked BEFORE the static value path, otherwise
	// unresolved variables (e.g. title, showCopy from props) cause
	// evalConst to fall through to a null alternate and emit "null".
	if cond, ok := ec.Expression.(*ast.ConditionalExpr); ok {
		// Statically-known test → build the winning branch directly so SSR
		// renders real content (e.g. a checkmark SVG or children label).
		if nodes := b.tryResolveConditional(cond, parentID); len(nodes) > 0 {
			return nodes
		}
		if isTernaryWithJSX(ec.Expression) {
			return []SlotNode{b.buildConditionalSlot(ec.Expression, parentID)}
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

// ─── buildTextSlot — simple signal read ────────────────────────────────────

func (b *builder) buildTextSlot(expr ast.Expr, parentID string) *TextSlot {
	signalName := extractSignalName(expr)
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

// ─── buildExprSlot — complex expression ────────────────────────────────────

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

// ─── buildConditionalSlot — ternary with JSX ───────────────────────────────

// tryResolveConditional statically resolves a ternary whose test is known at
// build time, building the winning branch's slot nodes directly. This lets
// SSR render the actual JSX branch (e.g. a checkmark SVG) instead of an empty
// hydration-only ConditionalSlot. Returns nil when the test isn't resolvable
// OR when the test depends on signals (signal-backed ternaries must stay
// reactive — either as an ExprSlot for text or a ConditionalSlot for JSX).
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

// knownTestValue resolves a ternary test to a value and reports whether the
// resolution is authoritative (no unresolvable identifiers leaked).
func (b *builder) knownTestValue(test ast.Expr) (string, bool) {
	// {props.children ? <A/> : <B/>} — truthiness comes from call-site children.
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
	if b.testFullyKnown(test) {
		return v, true
	}
	return v, false
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

// ─── buildListSlot — .map() with key detection ─────────────────────────────

func (b *builder) buildListSlot(expr ast.Expr, parentID string) *ListSlot {
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "list"))
	exprSource := generateExprJS(expr, b.sigMap())

	return &ListSlot{
		ID:         id,
		ExprSource: exprSource,
		Items:      b.tryResolveItems(expr),
		Keyed:      false,
		Signals:    b.collectSignalReads(expr),
		Components: collectListComponents(expr),
	}
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
func (b *builder) tryResolveItems(expr ast.Expr) []*ListItem {
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
	arrow, ok := call.Args[0].(*ast.ArrowFn)
	if !ok {
		return nil
	}

	var items []*ListItem
	for i, elem := range arr.Elements {
		key := itoa(i)
		elemVal := evalConst(elem)
		bindings := map[string]string{}
		if len(arrow.Params) >= 1 {
			bindings[arrow.Params[0].Name] = elemVal
		}
		bodyExpr := arrowBodyExpr(arrow)
		if bodyExpr == nil {
			continue
		}
		html := evalExprWithBindings(bodyExpr, bindings)
		items = append(items, &ListItem{
			Key:      key,
			Contents: []SlotNode{&StaticHTML{HTML: html}},
		})
	}
	return items
}

// ─── buildFragmentSlots — flatten fragments ────────────────────────────────

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

// ─── buildHandlerDecl — extract event handler ──────────────────────────────

func (b *builder) buildHandlerDecl(attr *ast.JSXAttr, elementID string) *HandlerDecl {
	eventName := strings.ToLower(attr.Name[2:]) // "onClick" → "click"
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
	}
}

// ─── buildAttrBinding — extract dynamic attribute ──────────────────────────

func (b *builder) buildAttrBinding(attr *ast.JSXAttr, elementID string) *AttrBinding {
	// Fully static values need no hydration binding — emit them statically
	// so the bundle doesn't re-evaluate loop vars (e.g. String(i)) at runtime.
	if b.isStaticResolvable(attr.Value) {
		return nil
	}
	signalName := extractSignalName(attr.Value)
	exprSource := ""
	if signalName == "" {
		exprSource = generateExprJS(attr.Value, b.sigMap())
	}

	return &AttrBinding{
		ElementSlotID: SlotID(elementID),
		AttrName:      attr.Name,
		SignalName:    signalName,
		ExprSource:    exprSource,
		Initial:       evalConstWithSignals(attr.Value, b.sigMap(), b.localProps),
		InitialExpr:   attr.Value,
		IsString:      isStringType(attr.Value, b.sigMap()),
	}
}

// ─── buildStaticValueSlot — expression with no signals ─────────────────────

func (b *builder) buildStaticValueSlot(expr ast.Expr, parentID string) *StaticHTML {
	val := evalConstWithSignals(expr, b.sigMap(), b.localProps)
	return &StaticHTML{HTML: val}
}

// buildStaticTextSlot is buildStaticValueSlot for JSX text positions: the
// statically-evaluated result is HTML-escaped so a template literal or string
// expression cannot inject real markup (e.g. <Code>{`return <h1>x</h1>`}</Code>).
func (b *builder) buildStaticTextSlot(expr ast.Expr, parentID string) *StaticHTML {
	val := evalConstWithSignals(expr, b.sigMap(), b.localProps)
	return &StaticHTML{HTML: escape.HTML(val)}
}

// ─── Signal resolution helpers ─────────────────────────────────────────────

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
		return b.referencesSignal(e.Callee)
	case *ast.MemberExpr:
		return b.referencesSignal(e.Object)
	case *ast.BinaryExpr:
		return b.referencesSignal(e.Left) || b.referencesSignal(e.Right)
	case *ast.ConditionalExpr:
		return b.referencesSignal(e.Test) || b.referencesSignal(e.Consequent) || b.referencesSignal(e.Alternate)
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
		// count() — call to a signal getter function
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

func extractSignalName(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		return e.Name
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			return id.Name
		}
	}
	return ""
}

// ─── collectSignalDecls from function body ─────────────────────────────────

func (b *builder) collectSignalDecls(body []ast.Stmt) []SignalDecl {
	var decls []SignalDecl
	for _, d := range sigutil.Find(body, false) {
		if d.IsResource || d.Initial == nil {
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
		decls = append(decls, SignalDecl{
			Name:        d.Name,
			SetterName:  d.Setter,
			Initial:     initial,
			IsString:    isStr,
			InitialExpr: d.Initial,
		})
	}
	return decls
}

// collectBodySignalUses returns every signal that is read (signalName()) or
// written (setSignalName(...)) anywhere in a component function body — inside
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

// ─── collectEffectJS from function body ────────────────────────────────────

func (b *builder) collectEffectJS(body []ast.Stmt) []string {
	var effects []string
	for _, stmt := range body {
		if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
			if call, ok := exprStmt.Expression.(*ast.CallExpr); ok {
				if id, ok := call.Callee.(*ast.Identifier); ok && id.Name == "createEffect" {
					if len(call.Args) >= 1 {
						js := generateExprJS(call.Args[0], b.sigMap())
						effects = append(effects, "createEffect("+js+")")
					}
				}
			}
		}
	}
	return effects
}

// ─── collectMemoJS from function body ──────────────────────────────────────

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

// ─── collectExtraVarJS from function body ──────────────────────────────────

func (b *builder) collectExtraVarJS(body []ast.Stmt) []string {
	var vars []string
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if decl.Init != nil {
					if call, ok := decl.Init.(*ast.CallExpr); ok {
						if id, ok := call.Callee.(*ast.Identifier); ok {
							switch id.Name {
							case "createSignal", "createResource", "createMemo", "createEffect":
								continue
							}
						}
					}
					if !decl.IsDestructuring && !referencesProps(decl.Init) {
						js := generateExprJS(decl.Init, b.sigMap())
						vars = append(vars, "var "+decl.Name+"="+js)
					}
				}
			}
		case *ast.ForStmt:
			vars = append(vars, renderStmtJS(s, b.sigMap()))
		case *ast.WhileStmt:
			vars = append(vars, renderStmtJS(s, b.sigMap()))
		case *ast.DoWhileStmt:
			vars = append(vars, renderStmtJS(s, b.sigMap()))
		}
	}
	return vars
}

// ─── collectSignalReads from an expression ─────────────────────────────────

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

// ─── extractHandlerBody ────────────────────────────────────────────────────

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
		b.WriteString(p.Name)
	}
	b.WriteString("){")
	for _, stmt := range fn.Body {
		b.WriteString(renderStmtJS(stmt, signals))
	}
	b.WriteByte('}')
	return b.String()
}

// ─── extractProps from JSX element ─────────────────────────────────────────

func extractProps(el *ast.JSXElement) map[string]ast.Expr {
	props := make(map[string]ast.Expr)
	for _, attr := range el.Opening.Attributes {
		if !attr.Spread && attr.Value != nil {
			props[attr.Name] = attr.Value
		}
	}
	return props
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func findReturnStmt(body []ast.Stmt) *ast.ReturnStmt {
	for _, stmt := range body {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			return ret
		}
	}
	return nil
}

// collectLocalVars resolves top-level local variable declarations in a component
// body to build-time constants (derived from props and other locals). Handles
// sequential `var x = <expr>` declarations and simple reassignments/mutations
// (`x = ...`, `x += ...`) across `var`/expression/`if` statements. The result
// is used to resolve SSR initial values AND to emit `var x = <const>` decls into
// the hydration bundle so bindings referencing locals don't throw ReferenceError.
func collectLocalVars(body []ast.Stmt, sigMap map[string]ast.Expr, props map[string]string) map[string]string {
	locals := make(map[string]string)
	working := make(map[string]string, len(props))
	for k, v := range props {
		working[k] = v
	}
	applyLocalStmts(body, locals, working, sigMap)
	return locals
}

func applyLocalStmts(stmts []ast.Stmt, locals, working map[string]string, sigMap map[string]ast.Expr) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				name := decl.Name
				if name == "" && len(decl.Names) > 0 {
					name = decl.Names[0]
				}
				if name == "" || name == "children" || decl.IsDestructuring {
					continue
				}
				if decl.Init != nil {
					// Skip locals whose initializer references unknowns (leaks),
					// but KEEP those that resolve to a definitive value even if
					// it's an empty string (e.g. `var src = props.src || ""`
					// with props.src="" — the hydration effects still read src).
					if operandLeaks(decl.Init, sigMap, working) {
						continue
					}
					v := evalConstWithSignals(decl.Init, sigMap, working)
					locals[name] = v
					working[name] = v
				}
			}
		case *ast.ExprStmt:
			applyLocalAssignment(s.Expression, locals, working, sigMap)
		case *ast.IfStmt:
			test := evalConstWithSignals(s.Test, sigMap, working)
			if isTruthyValue(test) {
				applyLocalStmts(s.Consequent, locals, working, sigMap)
				continue
			}
			if isFalsyValue(test) {
				applyLocalStmts(s.Alternate, locals, working, sigMap)
				continue
			}
		case *ast.BlockStmt:
			applyLocalStmts(s.Body, locals, working, sigMap)
		}
	}
}

func applyLocalAssignment(expr ast.Expr, locals, working map[string]string, sigMap map[string]ast.Expr) {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return
	}
	id, ok := bin.Left.(*ast.Identifier)
	if !ok {
		return
	}
	name := id.Name
	cur, exists := working[name]
	if !exists && !isKnownLocal(name, locals) {
		return
	}
	rhs := evalConstWithSignals(bin.Right, sigMap, working)
	if rhs == "" {
		return
	}
	switch bin.Op {
	case "=":
		locals[name] = rhs
		working[name] = rhs
	case "+=", "-=", "*=", "/=", "%=":
		if cur == "" {
			return
		}
		combined := cur + " " + bin.Op[:1] + " " + rhs
		if bin.Op == "+=" {
			combined = cur + rhs
		}
		locals[name] = combined
		working[name] = combined
	}
}

func isKnownLocal(name string, locals map[string]string) bool {
	_, ok := locals[name]
	return ok
}

// isTruthyValue reports whether a resolved const string is truthy in JS terms.
func isTruthyValue(v string) bool {
	return v != "" && v != "false" && v != "null" && v != "undefined" && v != "0"
}

// evalBinaryValue computes the value of a binary operation between two resolved
// const operands. Arithmetic/comparison ops use numeric evaluation when both
// operands are numeric; string concat uses raw concatenation.
func evalBinaryValue(op, left, right string) string {
	lNum, lOk := parseNumeric(left)
	rNum, rOk := parseNumeric(right)
	switch op {
	case "+":
		if lOk && rOk {
			return trimFloat(lNum + rNum)
		}
		return left + right
	case "-":
		if lOk && rOk {
			return trimFloat(lNum - rNum)
		}
	case "*":
		if lOk && rOk {
			return trimFloat(lNum * rNum)
		}
	case "/":
		if lOk && rOk && rNum != 0 {
			return trimFloat(lNum / rNum)
		}
	case "%":
		if lOk && rOk && rNum != 0 {
			return trimFloat(float64(int(lNum) % int(rNum)))
		}
	case "<":
		if lOk && rOk {
			return strconv.FormatBool(lNum < rNum)
		}
		return strconv.FormatBool(left < right)
	case "<=":
		if lOk && rOk {
			return strconv.FormatBool(lNum <= rNum)
		}
		return strconv.FormatBool(left <= right)
	case ">":
		if lOk && rOk {
			return strconv.FormatBool(lNum > rNum)
		}
		return strconv.FormatBool(left > right)
	case ">=":
		if lOk && rOk {
			return strconv.FormatBool(lNum >= rNum)
		}
		return strconv.FormatBool(left >= right)
	}
	return ""
}

func parseNumeric(s string) (float64, bool) {
	if s == "" || !isNumericLiteral(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func trimFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// isFalsyValue reports whether a resolved const string is falsy in JS terms.
func isFalsyValue(v string) bool {
	return v == "" || v == "false" || v == "null" || v == "undefined" || v == "0"
}

// jsLiteralFor renders a resolved const value as a JS literal. Numeric and
// boolean values are emitted bare; everything else is quoted as a string.
func jsLiteralFor(v string) string {
	if isTruthyValue(v) && (v == "true" || isNumericLiteral(v)) {
		return v
	}
	if v == "false" {
		return "false"
	}
	return "'" + escape.JSString(v) + "'"
}

func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != 'e' && r != 'E' && r != '+' {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// declaredLocalNames returns the set of identifiers already declared in the
// component's hydration scope: signal names, extra vars, and local functions.
// Locals collected by collectLocalVars that collide with these are skipped so
// the hydration bundle doesn't redeclare (and clobber) them.
func declaredLocalNames(node *ComponentNode, fn *ast.FnDecl) map[string]bool {
	declared := make(map[string]bool)
	for _, sig := range node.Signals {
		declared[sig.Name] = true
	}
	for _, ev := range node.ExtraVars {
		name := extraVarName(ev)
		if name != "" {
			declared[name] = true
		}
	}
	for _, stmt := range fn.Body {
		if fd, ok := stmt.(*ast.FnDecl); ok {
			declared[fd.Name] = true
		}
	}
	return declared
}

func extraVarName(ev string) string {
	s := strings.TrimSpace(ev)
	if !strings.HasPrefix(s, "var ") {
		return ""
	}
	s = strings.TrimPrefix(s, "var ")
	if i := strings.IndexAny(s, "= ;,("); i > 0 {
		return s[:i]
	}
	return s
}

func deriveInstanceID(id string) string {
	return strings.ReplaceAll(id, ".", "_")
}

// nextElementTag returns a unique tag identifier for an element under a given parent.
// When multiple sibling elements share the same tag name, appends _1, _2, etc.
func (b *builder) nextElementTag(tagName, parentID string) string {
	sanitized := sanitizeTagName(tagName)
	key := parentID + "." + sanitized
	count := b.elementCounts[key]
	b.elementCounts[key]++
	if count == 0 {
		return sanitized
	}
	return sanitized + "_" + itoa(count)
}

func isOnEvent(name string) bool {
	return len(name) > 2 && name[0] == 'o' && name[1] == 'n' && name[2] >= 'A' && name[2] <= 'Z'
}

// componentNeedsClient reports whether a signal-less component must be built
// as a client component (through the tree path) instead of being flattened by
// the SSR evaluator. That is the case when it receives a function prop (which
// can only run as a live handler), a signal-valued prop (which must stay
// reactive), or its return JSX contains event handlers (e.g. a reusable
// <Button onClick={props.onClick}> wrapper).
func (b *builder) componentNeedsClient(fn *ast.FnDecl, attrs map[string]ast.Expr) bool {
	for _, expr := range attrs {
		switch e := expr.(type) {
		case *ast.ArrowFn:
			return true
		case *ast.Identifier:
			if b.localFnBody != nil {
				if findLocalFunction(e.Name, b.localFnBody) != nil {
					return true
				}
			}
		}
		// Signal-valued props: <Child value={count()} /> means the child
		// must stay reactive even if it has no local signals.
		if b.referencesSignal(expr) {
			return true
		}
	}
	if ret := findReturnStmt(fn.Body); ret != nil {
		return hasEventHandlerExpr(ret.Value)
	}
	return false
}

// hasEventHandlerExpr reports whether a JSX expression tree contains any on*
// event handler attributes. Used to keep signal-less wrapper components
// interactive instead of flattening their handlers into dead static markup.
func hasEventHandlerExpr(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.JSXElement:
		for _, attr := range e.Opening.Attributes {
			if isOnEvent(attr.Name) {
				return true
			}
		}
		return hasEventHandlerChildren(e.Children)
	case *ast.JSXFragment:
		return hasEventHandlerChildren(e.Children)
	case *ast.ConditionalExpr:
		return hasEventHandlerExpr(e.Consequent) || hasEventHandlerExpr(e.Alternate)
	case *ast.BinaryExpr:
		return hasEventHandlerExpr(e.Left) || hasEventHandlerExpr(e.Right)
	case *ast.TypeAssertion:
		return hasEventHandlerExpr(e.Expr)
	}
	return false
}

func hasEventHandlerChildren(children []ast.JSXChild) bool {
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			if hasEventHandlerExpr(c.Expression) {
				return true
			}
		case *ast.JSXElementChild:
			if hasEventHandlerExpr(c.Element) {
				return true
			}
		case *ast.JSXFragmentChild:
			if hasEventHandlerExpr(c.Fragment) {
				return true
			}
		}
	}
	return false
}

func isAttrBinding(attr *ast.JSXAttr) bool {
	if attr.Spread || attr.Value == nil {
		return false
	}
	if isOnEvent(attr.Name) {
		return false
	}
	switch attr.Value.(type) {
	case *ast.Identifier, *ast.CallExpr, *ast.MemberExpr, *ast.ConditionalExpr, *ast.BinaryExpr, *ast.ArrowFn:
		return true
	}
	return false
}

// isStaticResolvable reports whether an expression can be fully resolved at
// build time from literals, props, and local bindings — i.e. it contains no
// signal reads and every identifier is a known prop/local. Statically
// resolvable attribute values are emitted directly into the SSR HTML with no
// hydration binding, avoiding runtime evaluation of build-time-only locals
// like for-loop counters.
func (b *builder) isStaticResolvable(expr ast.Expr) bool {
	if expr == nil {
		return true
	}
	switch e := expr.(type) {
	case *ast.Literal:
		return true
	case *ast.Identifier:
		if e.Name == "props" {
			return false
		}
		if _, inSig := b.sigMap()[e.Name]; inSig {
			return false
		}
		_, inProps := b.localProps[e.Name]
		return inProps
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if _, inSig := b.sigMap()[id.Name]; inSig {
				return false
			}
			if id.Name == "String" && len(e.Args) == 1 {
				return b.isStaticResolvable(e.Args[0])
			}
		}
		return false
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
			if _, ok := e.Property.(*ast.Identifier); ok {
				return true
			}
		}
		return false
	case *ast.BinaryExpr:
		return b.isStaticResolvable(e.Left) && b.isStaticResolvable(e.Right)
	case *ast.UnaryExpr:
		return b.isStaticResolvable(e.Arg)
	case *ast.ConditionalExpr:
		return b.isStaticResolvable(e.Test) && b.isStaticResolvable(e.Consequent) && b.isStaticResolvable(e.Alternate)
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			if !b.isStaticResolvable(p) {
				return false
			}
		}
		return true
	case *ast.TypeAssertion:
		return b.isStaticResolvable(e.Expr)
	}
	return false
}

func stmtReferencesProps(stmt ast.Stmt) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return referencesProps(s.Expression)
	case *ast.VarStmt:
		for _, decl := range s.Decls {
			if referencesProps(decl.Init) {
				return true
			}
		}
		return false
	case *ast.ReturnStmt:
		return referencesProps(s.Value)
	case *ast.IfStmt:
		if referencesProps(s.Test) {
			return true
		}
		for _, b := range s.Consequent {
			if stmtReferencesProps(b) {
				return true
			}
		}
		for _, b := range s.Alternate {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.BlockStmt:
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.ForStmt:
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.WhileStmt:
		if referencesProps(s.Test) {
			return true
		}
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.DoWhileStmt:
		if referencesProps(s.Test) {
			return true
		}
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.TryStmt:
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		if s.Catch != nil {
			for _, b := range s.Catch.Body {
				if stmtReferencesProps(b) {
					return true
				}
			}
		}
		for _, b := range s.Finally {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.ThrowStmt:
		return referencesProps(s.Value)
	case *ast.SwitchStmt:
		if referencesProps(s.Discriminant) {
			return true
		}
		for _, c := range s.Cases {
			if referencesProps(c.Test) {
				return true
			}
			for _, b := range c.Body {
				if stmtReferencesProps(b) {
					return true
				}
			}
		}
		return false
	default:
		return false
	}
}

func isStringType(expr ast.Expr, signals map[string]ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Literal:
		return e.Kind == ast.StringLit
	case *ast.BinaryExpr:
		// Logical operators return one of their operands. A string operand
		// (especially a string fallback like props.x || "") means the result
		// may be a string and must be emitted as a quoted JS literal.
		if e.Op != "||" && e.Op != "&&" && e.Op != "??" {
			return false
		}
		return isStringType(e.Left, signals) || isStringType(e.Right, signals)
	case *ast.ConditionalExpr:
		// Only a string when BOTH branches are strings. Array/object branches
		// (e.g. createSignal(type === "single" ? "" : [])) must be emitted as
		// real arrays or Array.isArray() checks fail at runtime.
		return isStringType(e.Consequent, signals) && isStringType(e.Alternate, signals)
	case *ast.TemplateExpr:
		return true
	case *ast.UnaryExpr:
		return e.Op == "typeof" || isStringType(e.Arg, signals)
	case *ast.ArrayExpr, *ast.ObjectExpr:
		return false
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if id.Name == "String" {
				return true
			}
			if initial, ok := signals[id.Name]; ok {
				return isStringType(initial, signals)
			}
		}
		return false
	case *ast.Identifier:
		if initial, ok := signals[e.Name]; ok {
			return isStringType(initial, signals)
		}
		return false
	}
	return false
}

func referencesProps(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		return e.Name == "props"
	case *ast.MemberExpr:
		return referencesProps(e.Object)
	case *ast.CallExpr:
		if referencesProps(e.Callee) {
			return true
		}
		for _, arg := range e.Args {
			if referencesProps(arg) {
				return true
			}
		}
		return false
	case *ast.BinaryExpr:
		return referencesProps(e.Left) || referencesProps(e.Right)
	case *ast.UnaryExpr:
		return referencesProps(e.Arg)
	case *ast.ConditionalExpr:
		return referencesProps(e.Test) || referencesProps(e.Consequent) || referencesProps(e.Alternate)
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			if referencesProps(p) {
				return true
			}
		}
		return false
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			if referencesProps(el) {
				return true
			}
		}
		return false
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if referencesProps(prop.Value) {
				return true
			}
		}
		return false
	case *ast.ArrowFn:
		if e.Expression {
			bodyExpr := arrowBodyExpr(e)
			if bodyExpr != nil {
				return referencesProps(bodyExpr)
			}
			return false
		}
		for _, stmt := range e.Body {
			if ref := stmtReferencesProps(stmt); ref {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// ─── SSR expression helpers ────────────────────────────────────────────────

// collectModuleConsts scans the module-level (top-of-file) variable
// declarations and constant-folds those with statically-evaluable initializers
// into a name → value map. This lets JSX text like {hexNum} resolve to 255 at
// build time instead of leaking the identifier name as literal text. Later
// declarations may reference earlier ones (e.g. const c = a + b).
func collectModuleConsts(prog *ast.Program) map[string]string {
	consts := make(map[string]string)
	if prog == nil {
		return consts
	}
	for _, stmt := range prog.Body {
		var vs *ast.VarStmt
		switch s := stmt.(type) {
		case *ast.VarStmt:
			vs = s
		case *ast.ExportStmt:
			if v, ok := s.Declaration.(*ast.VarStmt); ok {
				vs = v
			}
		}
		if vs == nil {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name == "" || decl.Init == nil {
				continue
			}
			if val := evalConstWithSignals(decl.Init, nil, consts); val != "" {
				consts[decl.Name] = val
			}
		}
	}
	return consts
}

// evalConst evaluates a constant expression to a string value.
func evalConst(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Literal:
		switch e.Kind {
		case ast.StringLit:
			return unescapeStringValue(e.Value)
		case ast.NumberLit:
			return e.Value
		case ast.BoolLit:
			return e.Value
		case ast.NullLit:
			return "null"
		default:
			return e.Value
		}
	case *ast.Identifier:
		return e.Name
	case *ast.UnaryExpr:
		arg := evalConst(e.Arg)
		if arg == "" {
			return ""
		}
		return e.Op + arg
	case *ast.BinaryExpr:
		left := evalConst(e.Left)
		right := evalConst(e.Right)
		// If either side can't be const-evaluated, return empty —
		// partial stringification produces broken JS like " || false".
		if left == "" || right == "" {
			return ""
		}
		// Numeric binary expressions (16 / 9, width * 2) must be computed
		// numerically, not string-concatenated. Otherwise a prop like
		// ratio={16/9} resolves to the literal "16 / 9", and downstream
		// arithmetic concatenates it into garbage ("116 / 9100").
		if _, lOk := parseNumeric(left); lOk {
			if _, rOk := parseNumeric(right); rOk {
				if v := evalBinaryValue(e.Op, left, right); v != "" {
					return v
				}
			}
		}
		return left + " " + e.Op + " " + right
	case *ast.ArrayExpr:
		var parts []string
		for _, el := range e.Elements {
			val := evalConst(el)
			if lit, ok := el.(*ast.Literal); ok && lit.Kind == ast.StringLit {
				val = "'" + escape.JSString(val) + "'"
			}
			parts = append(parts, val)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case *ast.ObjectExpr:
		var parts []string
		for _, prop := range e.Properties {
			if prop.Spread {
				continue
			}
			val := evalConst(prop.Value)
			parts = append(parts, prop.Key+`:`+val)
		}
		return "{" + strings.Join(parts, ",") + "}"
	case *ast.ConditionalExpr:
		// Ternary: evaluate test, return appropriate branch
		test := evalConst(e.Test)
		if test == "" {
			// Can't evaluate test — variables or props not resolvable.
			// Return empty instead of falling through to alternate (which
			// could be null literal, producing "null" text in output).
			return ""
		}
		if test != "false" && test != "null" && test != "undefined" && test != "0" {
			return evalConst(e.Consequent)
		}
		return evalConst(e.Alternate)
	case *ast.TemplateExpr:
		var b strings.Builder
		for i, raw := range e.Raw {
			b.WriteString(raw)
			if i < len(e.Parts) {
				b.WriteString(evalConst(e.Parts[i]))
			}
		}
		return b.String()
	default:
		return ""
	}
}

// evalConstWithSignals evaluates a constant expression, resolving signal reads
// (name()) and bare signal identifiers to their initial values. Used to compute
// the SSR initial value of dynamic attributes like data-state={open() ? "open" : "closed"}.
// props maps component parameter names to pre-resolved call-site values so
// props.X reads and bare param identifiers resolve.
func evalConstWithSignals(expr ast.Expr, signals map[string]ast.Expr, props map[string]string) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Literal:
		if e.Kind == ast.NullLit {
			return ""
		}
		if e.Kind == ast.StringLit {
			return unescapeStringValue(e.Value)
		}
		return e.Value
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if initial, ok := signals[id.Name]; ok {
				return evalConstWithSignals(initial, signals, props)
			}
			if id.Name == "String" && len(e.Args) == 1 {
				return evalConstWithSignals(e.Args[0], signals, props)
			}
		}
		return ""
	case *ast.Identifier:
		if initial, ok := signals[e.Name]; ok {
			return evalConstWithSignals(initial, signals, props)
		}
		if v, ok := props[e.Name]; ok {
			return v
		}
		return e.Name
	case *ast.ConditionalExpr:
		test := evalConstWithSignals(e.Test, signals, props)
		if test == "false" || test == "null" || test == "undefined" || test == "0" || test == "" {
			return evalConstWithSignals(e.Alternate, signals, props)
		}
		return evalConstWithSignals(e.Consequent, signals, props)
	case *ast.BinaryExpr:
		switch e.Op {
		case "||":
			left := evalConstWithSignals(e.Left, signals, props)
			if isTruthyValue(left) {
				return left
			}
			return evalConstWithSignals(e.Right, signals, props)
		case "&&":
			left := evalConstWithSignals(e.Left, signals, props)
			if !isTruthyValue(left) {
				return left
			}
			return evalConstWithSignals(e.Right, signals, props)
		case "==", "===", "!=", "!==":
			lVal := evalConstWithSignals(e.Left, signals, props)
			rVal := evalConstWithSignals(e.Right, signals, props)
			// A literal operand is known even when its resolved value is "".
			// A non-empty resolved value is also known. Only treat truly
			// unresolvable operands (unresolved identifier, call, etc.) as unknown.
			_, lLit := e.Left.(*ast.Literal)
			_, rLit := e.Right.(*ast.Literal)
			if !lLit && lVal == "" {
				return ""
			}
			if !rLit && rVal == "" {
				return ""
			}
			switch e.Op {
			case "==", "===":
				return strconv.FormatBool(lVal == rVal)
			default:
				return strconv.FormatBool(lVal != rVal)
			}
		default:
			left := evalConstWithSignals(e.Left, signals, props)
			right := evalConstWithSignals(e.Right, signals, props)
			if operandLeaks(e.Left, signals, props) || operandLeaks(e.Right, signals, props) {
				return ""
			}
			return evalBinaryValue(e.Op, left, right)
		}
	case *ast.UnaryExpr:
		arg := evalConstWithSignals(e.Arg, signals, props)
		if arg == "" {
			return ""
		}
		return e.Op + arg
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
			if prop, ok := e.Property.(*ast.Identifier); ok {
				if v, ok := props[prop.Name]; ok {
					return v
				}
			}
		}
		return ""
	case *ast.TemplateExpr:
		var buf strings.Builder
		for i, raw := range e.Raw {
			buf.WriteString(raw)
			if i < len(e.Parts) {
				buf.WriteString(evalConstWithSignals(e.Parts[i], signals, props))
			}
		}
		return buf.String()
	default:
		return evalConst(expr)
	}
}

// operandLeaks reports whether a binary-operand expression contains an
// unresolvable identifier/call that would leak a name or "" into a stringified
// result. Missing props (props.X) resolve to "" which is a legitimate falsy
// value, so those do NOT count as leaks.
func operandLeaks(expr ast.Expr, signals map[string]ast.Expr, props map[string]string) bool {
	switch e := expr.(type) {
	case *ast.Identifier:
		if e.Name == "props" {
			return true
		}
		if _, ok := signals[e.Name]; ok {
			return false
		}
		_, ok := props[e.Name]
		return !ok
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
			if _, ok := e.Property.(*ast.Identifier); ok {
				return false
			}
		}
		return true
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if _, ok := signals[id.Name]; ok {
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		return operandLeaks(e.Left, signals, props) || operandLeaks(e.Right, signals, props)
	case *ast.UnaryExpr:
		return operandLeaks(e.Arg, signals, props)
	case *ast.ConditionalExpr:
		if operandLeaks(e.Test, signals, props) {
			return true
		}
		tv := evalConstWithSignals(e.Test, signals, props)
		if isTruthyValue(tv) {
			return operandLeaks(e.Consequent, signals, props)
		}
		return operandLeaks(e.Alternate, signals, props)
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			if operandLeaks(p, signals, props) {
				return true
			}
		}
		return false
	case *ast.TypeAssertion:
		return operandLeaks(e.Expr, signals, props)
	default:
		return false
	}
}

// resolveSignalReadExpr resolves a signal-read expression to its initial value.
// Handles CallExpr{name()} and Identifier{name} where name is a known signal.
func resolveSignalReadExpr(expr ast.Expr, signals map[string]ast.Expr) string {
	if expr == nil || signals == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if initial, ok := signals[id.Name]; ok {
				return evalConst(initial)
			}
		}
	case *ast.Identifier:
		if initial, ok := signals[e.Name]; ok {
			return evalConst(initial)
		}
	}
	return ""
}

// updateTextSlotInitials walks slot children and updates TextSlot.Initial
// values from the resolved signal declarations.
func updateTextSlotInitials(children []SlotNode, signals []SignalDecl) {
	signalMap := make(map[string]string)
	for _, sig := range signals {
		if sig.Initial != "" {
			signalMap[sig.Name] = sig.Initial
		}
	}
	for _, child := range children {
		switch c := child.(type) {
		case *TextSlot:
			if initial, ok := signalMap[c.Signal.Name]; ok {
				c.Initial = initial
			}
		case *ComponentSlot:
			if c.Component != nil {
				updateTextSlotInitials(c.Component.Children, signals)
			}
		}
	}
}

// ─── SSR-evaluated child components ────────────────────────────────────────

// evalExprWithBindings evaluates an expression with variable bindings for list item rendering.
func evalExprWithBindings(expr ast.Expr, bindings map[string]string) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Literal:
		return e.Value
	case *ast.Identifier:
		if v, ok := bindings[e.Name]; ok {
			return v
		}
		return ""
	case *ast.JSXElement:
		return evalJSXWithBindings(e, bindings)
	case *ast.JSXFragment:
		var b strings.Builder
		for _, child := range e.Children {
			switch c := child.(type) {
			case *ast.JSXText:
				b.WriteString(c.Value)
			case *ast.JSXExprContainer:
				b.WriteString(evalExprWithBindings(c.Expression, bindings))
			case *ast.JSXElementChild:
				b.WriteString(evalJSXWithBindings(c.Element, bindings))
			case *ast.JSXFragmentChild:
				b.WriteString(evalExprWithBindings(c.Fragment, bindings))
			}
		}
		return b.String()
	case *ast.BinaryExpr:
		left := evalExprWithBindings(e.Left, bindings)
		right := evalExprWithBindings(e.Right, bindings)
		if e.Op == "+" || e.Op == "-" {
			return left + right
		}
		return left + " " + e.Op + " " + right
	case *ast.TemplateExpr:
		var b strings.Builder
		for i, raw := range e.Raw {
			b.WriteString(raw)
			if i < len(e.Parts) {
				b.WriteString(evalExprWithBindings(e.Parts[i], bindings))
			}
		}
		return b.String()
	case *ast.MemberExpr:
		return evalMemberExprWithBindings(e, bindings)
	case *ast.UnaryExpr:
		if e.Op == "!" {
			val := evalExprWithBindings(e.Arg, bindings)
			if val == "" || val == "false" {
				return "true"
			}
			return "false"
		}
		return evalExprWithBindings(e.Arg, bindings)
	case *ast.ConditionalExpr:
		test := evalExprWithBindings(e.Test, bindings)
		if test != "" && test != "false" {
			return evalExprWithBindings(e.Consequent, bindings)
		}
		return evalExprWithBindings(e.Alternate, bindings)
	default:
		return ""
	}
}

func evalJSXWithBindings(el *ast.JSXElement, bindings map[string]string) string {
	name := el.Opening.Name
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(name)
	for _, attr := range el.Opening.Attributes {
		if attr.Spread {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(ast.HTMLAttrName(attr.Name))
		if attr.Value != nil {
			val := evalExprWithBindings(attr.Value, bindings)
			b.WriteString(`="`)
			b.WriteString(escape.HTML(val))
			b.WriteByte('"')
		}
	}
	if el.Opening.SelfClosing {
		if isVoidElement(el.Opening.Name) {
			b.WriteString(" />")
		} else {
			b.WriteString("></")
			b.WriteString(el.Opening.Name)
			b.WriteByte('>')
		}
		return b.String()
	}
	b.WriteByte('>')
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			b.WriteString(c.Value)
		case *ast.JSXExprContainer:
			b.WriteString(evalExprWithBindings(c.Expression, bindings))
		case *ast.JSXElementChild:
			b.WriteString(evalJSXWithBindings(c.Element, bindings))
		case *ast.JSXFragmentChild:
			b.WriteString(evalExprWithBindings(c.Fragment, bindings))
		}
	}
	b.WriteString("</")
	b.WriteString(name)
	b.WriteByte('>')
	return b.String()
}

func evalMemberExprWithBindings(expr *ast.MemberExpr, bindings map[string]string) string {
	prop := ""
	if id, ok := expr.Property.(*ast.Identifier); ok {
		prop = id.Name
	}
	if id, ok := expr.Object.(*ast.Identifier); ok {
		if v, ok := bindings[id.Name]; ok {
			if prop == "length" {
				return itoa(len(v))
			}
			return v
		}
	}
	obj := evalExprWithBindings(expr.Object, bindings)
	if prop == "length" && obj != "" {
		return itoa(len(obj))
	}
	return ""
}

// extractPropsAST extracts raw AST expressions from JSX attributes.
func extractPropsAST(el *ast.JSXElement) map[string]ast.Expr {
	props := make(map[string]ast.Expr)
	for _, attr := range el.Opening.Attributes {
		if attr.Spread {
			continue
		}
		if attr.Value != nil {
			props[attr.Name] = attr.Value
		}
	}
	return props
}

// extractParamNames returns the parameter names of a function declaration.
func extractParamNames(fn *ast.FnDecl) []string {
	var names []string
	for _, p := range fn.Params {
		names = append(names, p.Name)
	}
	return names
}

// buildPropBindings maps parameter names to evaluated prop values.
// Handles two patterns:
//  1. Destructured params: function({ items, label }) with <Comp items={...} label={...} />
//  2. Single props object: function(props) with <Comp items={...} label={...} />
//     In this case, we flatten the attrs into top-level bindings so the SSREval
//     can resolve `props.breadcrumbs` by treating "breadcrumbs" as a binding.
//
// The signals parameter enables resolving signal reads like name() to their initial values.
func buildPropBindings(paramNames []string, attrs map[string]ast.Expr, signals map[string]ast.Expr) map[string]string {
	bindings := make(map[string]string)
	if len(paramNames) == 1 && paramNames[0] == "props" {
		// Single props object pattern — evaluate each attribute value
		// and store them as top-level bindings. The SSREval resolves
		// identifiers like "breadcrumbs" from the attrs directly.
		for name, expr := range attrs {
			val := evalConst(expr)
			if val == "" {
				val = resolveSignalReadExpr(expr, signals)
			}
			bindings[name] = val
		}
		return bindings
	}
	// Destructured params pattern
	for _, name := range paramNames {
		if expr, ok := attrs[name]; ok {
			val := evalConst(expr)
			if val == "" {
				val = resolveSignalReadExpr(expr, signals)
			}
			bindings[name] = val
		}
	}
	return bindings
}

func (b *builder) buildCallSiteChildSlots(children []ast.JSXChild, parentID string) []SlotNode {
	var result []SlotNode
	for _, child := range children {
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

// isVoidElement returns true for HTML void elements that can be self-closing.
// Non-void elements like <div/> must be emitted as <div></div>.
func isVoidElement(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

// handlerBodiesReferenceProps checks if any handler body references `props.`
func handlerBodiesReferenceProps(handlers []HandlerDecl) bool {
	for _, h := range handlers {
		if strings.Contains(h.Body, "props.") {
			return true
		}
	}
	return false
}

// handlersOrLocalsReferenceProps reports whether any handler body OR any local
// function referenced by a handler accesses props. Local functions (e.g.
// handleChange) may reference props.X even though the handler body that calls
// them only contains the function name.
func handlersOrLocalsReferenceProps(handlers []HandlerDecl, body []ast.Stmt) bool {
	if handlerBodiesReferenceProps(handlers) {
		return true
	}
	locals := collectHandlerLocalFunctions(handlers, body)
	for _, fn := range locals {
		for _, stmt := range fn.Body {
			if stmtReferencesProps(stmt) {
				return true
			}
		}
	}
	return false
}

// propsIdentRe matches a standalone `props` identifier in compiled JS.
var propsIdentRe = regexp.MustCompile(`\bprops\b`)

// compiledRefsProps reports whether any compiled JS string references `props`.
// Effects/memos that read props.X (e.g. controlled-component sync effects like
// `if (props.checked !== undefined) setChecked(props.checked)`) require the
// parent-scope props registration so `props` resolves at runtime.
func compiledRefsProps(js []string) bool {
	for _, s := range js {
		if propsIdentRe.MatchString(s) {
			return true
		}
	}
	return false
}

// buildPropsRegDecl generates a `__krate_props["<id>"]={...}` registration.// It must be evaluated in the PARENT component's scope because the values may
// reference the parent's signals (e.g. checked={checked1()} becomes
// checked:checked1()). The child component reads it via `var props=__krate_props["<id>"]`.
func buildPropsRegDecl(id string, props map[string]ast.Expr, signals map[string]ast.Expr) string {
	var b strings.Builder
	b.WriteString("__krate_props[")
	b.WriteString(strconv.Quote(id))
	b.WriteString("]={")
	if len(props) == 0 {
		b.WriteByte('}')
		return b.String()
	}
	first := true
	for name, expr := range props {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(name)
		b.WriteByte(':')
		val := generateExprJS(expr, signals)
		if val == "" {
			val = "undefined"
		}
		b.WriteString(val)
	}
	b.WriteByte('}')
	return b.String()
}
