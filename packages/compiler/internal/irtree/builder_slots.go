package irtree

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/htmlspec"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	"github.com/kratejs/krate/packages/compiler/internal/sigutil"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

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
		// Compiled external packages (Radix, etc.) are bundled into islands by
		// esbuild. Their parsed function declarations must never be re-emitted
		// into the page hydration bundle: they are not valid Krate codegen input
		// (e.g. hyphenated object keys), and they would duplicate the island.
		// Workspace @krate packages are Krate source and stay eligible.
		if src := filepath.ToSlash(b.ann.ComponentSources[name]); strings.Contains(src, "/node_modules/") && !strings.Contains(src, "/node_modules/@krate/") {
			continue
		}
		candidates[name] = fn
	}
	if len(candidates) == 0 {
		return nil
	}

	referenced := make(map[string]bool)
	var queue []string
	// Tokenize each code fragment once; string literals, comments and
	// property names (`obj.helper`) never count as references.
	scan := func(code string) {
		if code == "" {
			return
		}
		scanJSIdents(code, func(name string) {
			if _, ok := candidates[name]; ok && !referenced[name] {
				referenced[name] = true
				queue = append(queue, name)
			}
		})
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
	// Callback refs (including bound form actions) run at hydration and can
	// call local or imported helpers.
	for _, r := range node.RefBindings {
		scan(r.Callback)
		scan(r.Target)
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
	// Signal factory calls (e.g. createReducer's reducer function) and runtime
	// initializers (a lazy useState(loadDraft) call) may reference
	// local/module functions that must be hoisted into the hydration scope.
	for _, s := range node.Signals {
		if s.FactoryJS != "" {
			scan(s.FactoryJS)
		}
		if s.RawInit != "" {
			scan(s.RawInit)
		}
	}
	// Props registrations (__krate_props["<id>"]={onClick:clear,...}) reference
	// local functions from the parent scope; hoist them so the child's forwarded
	// handlers resolve at runtime.
	for _, ev := range node.ExtraVars {
		scan(ev)
	}
	// Replayed body statements (e.g. a custom hook call `useTitle("x")`).
	for _, ev := range node.PreSignalVars {
		scan(ev)
	}
	// Dynamic list slot expressions (e.g. {items().map(x => <Item onSelect={fn}/>)})
	// reference local functions too; without hoisting them the runtime re-render
	// of the list would throw ReferenceError. The ListSlot may be nested inside
	// a signal-less child wrapper's call-site slots (e.g. <ToastViewport>{list}</ToastViewport>),
	// so walk the built slot tree transitively.
	scannedComponents := map[string]bool{}
	var scanComponentBody func(name string)
	scanComponentBody = func(name string) {
		fn := b.functions[name]
		if fn == nil || scannedComponents[name] {
			return
		}
		scannedComponents[name] = true
		// Declare the component in this scope (its body then resolves this
		// scope's helpers); the queue below scans its body transitively.
		candidates[name] = fn
		if !referenced[name] {
			referenced[name] = true
			queue = append(queue, name)
		}
		code := renderStmtJS(fn, nil)
		scanJSIdents(code, func(id string) {
			if id != name && isComponentName(id) && b.functions[id] != nil {
				scanComponentBody(id)
			}
		})
	}
	var scanSlots func(slots []SlotNode)
	scanSlots = func(slots []SlotNode) {
		for _, child := range slots {
			switch s := child.(type) {
			case *ListSlot:
				scan(s.ExprSource)
				// Components a list row renders run in this scope at runtime,
				// so the helpers their bodies call must be declared here too.
				for _, name := range s.Components {
					scanComponentBody(name)
				}
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

// codeContainsIdent reports whether JS code references name as an identifier.
// Used to detect local function references in generated JS like
// `var handler=handleClickOutside`. The scan is token-based: occurrences
// inside string literals, template text, comments and property names
// (`x.name`) are not references.
func codeContainsIdent(code, name string) bool {
	if name == "" || code == "" || !strings.Contains(code, name) {
		return false
	}
	found := false
	scanJSIdents(code, func(word string) {
		if word == name {
			found = true
		}
	})
	return found
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
	case *ast.NewExpr, *ast.AwaitExpr, *ast.ThisExpr, *ast.DynamicImport, *ast.ImportMetaExpr:
		// Render like the same expression in a JSX child position: folded
		// statically when possible, otherwise a runtime expression slot.
		return b.buildExprContainerChildrenMode(&ast.JSXExprContainer{Expression: e}, parentID, true)
	case *ast.ObjectExpr:
		b.errorAt(e.Position, "an object literal is rendered as JSX content, which is not valid (React rejects objects as children)",
			"render one of its properties (e.g. {obj.label}), JSON.stringify(obj), or map it to elements")
		return nil
	default:
		b.errorAt(expr.Pos(), fmt.Sprintf("this expression (%T) cannot be rendered as JSX content", expr),
			"wrap it in a JSX expression the compiler supports, such as a string, number, element, conditional or .map() call")
		return nil
	}
}

// buildCallExprSlots handles call expressions - detects .map() calls and falls back to ExprSlot.
func (b *builder) buildCallExprSlots(call *ast.CallExpr, parentID string) []SlotNode {
	if content, container, ok := portalCall(call); ok {
		return b.buildPortal(content, container, call.Position, parentID)
	}
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
	if el == nil || el.Opening == nil {
		return nil
	}
	// A key that reads reactive state remounts the element when it changes.
	// A lowered class remounts itself (its runtime instance reads the key),
	// which keeps it and its children in the error-boundary owner tree.
	if key := jsxKeyAttr(el); key != nil && b.referencesSignal(key) && !b.ann.IsLoweredClass(el.Opening.Name) {
		return b.buildKeyedSlot(el, key, parentID)
	}
	// Built-in <Portal>: render children inline with a marker, then move the
	// node to document.body on the client (see HydrationBootstrapJS).
	if el.Opening != nil && el.Opening.Name == "Portal" {
		return b.buildPortalSlot(el, parentID)
	}
	// Built-in <Slot> (shadcn `asChild`): merge the Slot's props onto its single
	// child element.
	if el.Opening != nil && el.Opening.Name == "Slot" {
		return b.buildSlotSlots(el, parentID)
	}
	// CSS interception. When the component currently being walked declares a
	// zero-JS CSS primitive, a trigger (a setter handler) becomes a `<label for>`
	// and a panel (a scope condition in showIf) becomes a wrapper-toggled
	// element. The rewrite returns an expression (a label element or a
	// display:contents wrapper), which is then built by the normal slot pipeline
	// so children nested components, lists, conditionals render as usual.
	// This runs before the showIf handling below, which the panel rewrite removes.
	// First-class scope handles: `<el {...scope.trigger('a')}>` /
	// `{...scope.panel('a')}` / `{...scope.close()}` expand to the zero-JS
	// trigger/panel/close markup, anywhere in any component.
	if isIntrinsicTag(el) {
		if rewritten := b.rewriteScopeSpread(el); rewritten != nil {
			return b.buildSlotNodes(rewritten, parentID)
		}
	}

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

	// Special components - route to metadata output. Lowercase <script> and
	// <style> are ordinary elements rendered in place (e.g. a <style> inside
	// an <svg>); the documented <Head>/<Script>/<Style> are hoisted, and so is
	// React 19 document metadata (<title>/<meta>/<link>, see metadata.go).
	switch name {
	case "Head", "head", "Script", "Style":
		return []SlotNode{b.buildMetaSlot(el, parentID)}
	case "Link":
		b.hasLinks = true
		return b.buildLinkSlots(el, parentID)
	case "SyntaxHighlight":
		return b.buildSyntaxHighlightSlots(el, parentID)
	case "Icon":
		if b.transformIcon != nil {
			if transformed := b.transformIcon(el); transformed != nil && transformed != el {
				return b.buildSlotNodes(transformed, parentID)
			}
		}
		return []SlotNode{&StaticHTML{HTML: ""}}
	case "Image":
		if b.transformImage != nil {
			if transformed := b.transformImage(el); transformed != nil && transformed != el {
				return b.buildSlotNodes(transformed, parentID)
			}
		}
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

	// External components (including `Namespace.Provider`) are islands. Their
	// provider context must live inside the island, so do not flatten them as
	// Krate context providers.
	if root := name; root != "" {
		if i := strings.IndexByte(root, '.'); i >= 0 {
			root = root[:i]
		}
		if _, external := b.externalImports[root]; external {
			if slot, ok := b.buildExternalComponentSlot(name, el); ok {
				return []SlotNode{slot}
			}
		}
	}

	// Context provider: render children (a display:contents passthrough). When
	// the provided `value` is a CSS scope handle, make it the active scope for
	// the subtree so descendant `Ctx.useContext()` reads resolve to it. A literal
	// value is also folded into the subtree's context marker so static rendering
	// sees the provided value instead of the context default.
	if ctx, ok := b.consumerContext(name); ok {
		return b.buildContextConsumer(el, ctx, parentID)
	}
	if ctx, ok := b.providerContext(name); ok {
		var valueExpr ast.Expr
		for _, attr := range el.Opening.Attributes {
			if attr != nil && attr.Name == "value" {
				valueExpr = attr.Value
			}
		}
		savedScope := b.contextScopes
		savedProps := b.localProps
		savedValues := b.contextValues
		savedInitials := b.contextInitials
		isScope := false
		if ref, ok := b.resolveScopeExpr(valueExpr); ok {
			isScope = true
			b.contextScopes = cloneScopeMap(savedScope)
			b.contextScopes[ctx] = ref
		}
		if initial, ok := b.staticContextValue(valueExpr); ok && !isScope {
			// A build-time constant (literal, folded local, serializable
			// object) shadows any outer dynamic provider of the same context.
			b.localProps = cloneStringMap(savedProps)
			b.localProps[contextMarkerPrefix+ctx] = initial
			if _, outer := savedValues[ctx]; outer {
				b.contextValues = cloneExprMap(savedValues)
				delete(b.contextValues, ctx)
			}
		} else if valueExpr != nil && !isScope {
			// A dynamic value (signal/prop expression): substitute it for
			// `Ctx.useContext()` reads while building the subtree.
			b.contextValues = cloneExprMap(savedValues)
			b.contextValues[ctx] = valueExpr
			// Static rendering (SSREval) still folds reads from this marker, so
			// seed it with the value's build-time initial as a fallback.
			initial := evalConstValue(valueExpr, b.sigMap(), savedProps)
			b.contextInitials = make(map[string]jsvalue.Value, len(savedInitials)+1)
			for k, v := range savedInitials {
				b.contextInitials[k] = v
			}
			b.contextInitials[ctx] = initial
			if !initial.ContainsUnknown() {
				b.localProps = cloneStringMap(savedProps)
				b.localProps[contextMarkerPrefix+ctx] = initial
			}
		}
		slots := b.buildFragmentSlots(&ast.JSXFragment{Children: el.Children}, parentID)
		b.contextScopes = savedScope
		b.localProps = savedProps
		b.contextValues = savedValues
		b.contextInitials = savedInitials
		return slots
	}

	// Uppercase = component.
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		// A bare-package import is an external React component: mount the whole
		// subtree as a runtime island (Preact) instead of statically rendering it.
		if slot, ok := b.buildExternalComponentSlot(name, el); ok {
			return []SlotNode{slot}
		}
		// Go component imported from a `.go` file (import { Card } from "./card.go").
		if ref, ok := b.goComponents[name]; ok {
			return b.buildGoComponentSlot(el, ref, parentID)
		}
		if slots := b.buildComponentSlot(el, parentID); len(slots) > 0 {
			return slots
		}
		// Unknown component - render as empty span, but warn instead of failing
		// silently. Class components get a specific, actionable message naming
		// the unsupported member.
		b.warnUnrenderedComponent(name, el)
		return []SlotNode{&StaticHTML{HTML: "<span></span>"}}
	}

	// Lowercase = HTML element. React 19 document metadata moves to <head>.
	if b.hoistsMetadata(el) {
		return b.buildHoistedMetadata(el, parentID)
	}
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
	b.suspenseDepth++
	primary, mode, resolved := b.resolveSuspensePrimary(el.Children, string(id))
	b.suspenseDepth--
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
		if !ok || elc.Element == nil || elc.Element.Opening == nil {
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
				out = append(out, &StaticHTML{HTML: escape.Text(ch.Value)})
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
			if c.Expression == nil || b.referencesSignal(c.Expression) {
				return "", false
			}
			text, ok := childText(b.evalValue(c.Expression))
			if !ok {
				return "", false
			}
			sb.WriteString(text)
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
			val, ok := childText(b.evalValue(c.Expression))
			if !ok {
				// Not known at build time - can't highlight at compile time.
				canHighlight = false
				break
			}
			code.WriteString(val)
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
	b.metaDepth++
	defer func() { b.metaDepth-- }()

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
				if name == "script" || name == "style" {
					children = append(children, &StaticHTML{HTML: htmlspec.RawText(name, c.Value)})
				} else {
					children = append(children, &StaticHTML{HTML: escape.Text(c.Value)})
				}
			}
		case *ast.JSXExprContainer:
			// Meta content (Head/Script/Style) is intentionally raw HTML - a
			// template literal inside <Script>{...}</Script> must NOT be
			// HTML-escaped or the inline script would be corrupted. Only the
			// closing-tag sequence is neutralized inside script/style.
			childNodes := b.buildMetaExprContainerChildren(c, parentID)
			if name == "script" || name == "style" {
				for i, n := range childNodes {
					if st, ok := n.(*StaticHTML); ok {
						childNodes[i] = &StaticHTML{HTML: htmlspec.RawText(name, st.HTML)}
					}
				}
			}
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
		if attr.Spread || attr.Name == "dangerouslySetInnerHTML" {
			continue
		}
		buf.WriteString(RenderAttr(tag, attr, b.evalAttrConst, b.evalAttrValue))
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

// buildGoComponentSlot lowers `<Card/>` where Card is a Go component imported
// from a `.go` file. Props are const-evaluated to JSON string values; children
// are built as slots and rendered by the emitter (passed as the `children`
// prop). Server-tier refs are rendered at build time by the Go host; runtime
// refs become request-time regions.
func (b *builder) buildGoComponentSlot(el *ast.JSXElement, ref GoComponentRef, parentID string) []SlotNode {
	childID := joinSlotID(parentID, el.Opening.Name)
	instanceIdx := b.instanceCounts[el.Opening.Name]
	b.instanceCounts[el.Opening.Name]++
	id := SlotID(string(childID) + "_c" + itoa(instanceIdx))

	// Runtime-tier Go components become request-time regions rendered by the Go
	// host (region-<id> splice marker). Children are rendered at build time and
	// passed to the region as the `children` prop.
	if ref.Tier == TierRuntime {
		rtProps := map[string]any{}
		for _, attr := range el.Opening.Attributes {
			if attr == nil {
				continue
			}
			if attr.Spread {
				b.codegenErrs = append(b.codegenErrs, fmt.Errorf("Go component <%s>: spread props are not supported; pass explicit props", el.Opening.Name))
				continue
			}
			if attr.Name == "" {
				continue
			}
			if attr.Value == nil {
				rtProps[attr.Name] = true
				continue
			}
			rtProps[attr.Name] = json.RawMessage(b.constJSON(attr.Value))
		}
		var children []SlotNode
		if len(el.Children) > 0 {
			children = b.buildFragmentSlots(&ast.JSXFragment{Children: el.Children}, string(id))
		}
		return []SlotNode{&ComponentSlot{
			ID: id,
			Component: &ComponentNode{
				ID:           id,
				Name:         ref.Func,
				Tier:         TierRuntime,
				Runtime:      "go",
				SourceFile:   ref.File,
				RuntimeProps: rtProps,
				Children:     children,
			},
		}}
	}

	props := map[string]string{}
	for _, attr := range el.Opening.Attributes {
		if attr == nil {
			continue
		}
		if attr.Spread {
			b.codegenErrs = append(b.codegenErrs, fmt.Errorf("Go component <%s>: spread props are not supported; pass explicit props", el.Opening.Name))
			continue
		}
		if attr.Name == "" {
			continue
		}
		if attr.Value == nil {
			props[attr.Name] = "true"
			continue
		}
		props[attr.Name] = b.constJSON(attr.Value)
	}
	var children []SlotNode
	if len(el.Children) > 0 {
		children = b.buildFragmentSlots(&ast.JSXFragment{Children: el.Children}, string(id))
	}
	return []SlotNode{&GoComponentSlot{ID: id, Ref: ref, Props: props, Children: children, Pos: el.Position}}
}

// constJSON serializes a call-site prop expression to JSON, resolving
// signal/prop reads to their build-time values.
func (b *builder) constJSON(expr ast.Expr) string {
	if js, ok := jsvalue.JSONStringify(b.evalValue(expr)); ok {
		return js
	}
	return SerializeJSON(expr, func(e ast.Expr) (string, bool) {
		return evalConstWithSignals(e, b.sigMap(), b.localProps), true
	})
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
	if el == nil || el.Opening == nil {
		return el
	}
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
	attrsForOpening, _ := b.expandForwardedSpreadAttrs(el.Opening.Attributes)
	// actionAttrs maps an action prop that names a server action (directly or
	// through a useActionState dispatch) to its native POST target, so the
	// form still submits without JavaScript.
	actionAttrs := make(map[*ast.JSXAttr]string)
	hasMethod := false
	hasFormMethod := false
	for _, attr := range attrsForOpening {
		switch attr.Name {
		case "method":
			hasMethod = true
		case "formmethod", "formMethod":
			hasFormMethod = true
		case "action", "formAction", "formaction":
			if u, ok := b.actionAttrURL(attr.Value); ok {
				actionAttrs[attr] = u
			} else if id, ok := attr.Value.(*ast.Identifier); ok {
				if u, ok := b.actionStateURL(id.Name); ok {
					actionAttrs[attr] = u
				}
			}
		}
	}
	// Inside <select value>, an option's selection comes from the select.
	inSelect := false
	optionSelected := false
	if name == "option" && len(b.selectStack) > 0 && b.selectStack[len(b.selectStack)-1] != nil {
		inSelect = true
		optionSelected = OptionSelected(b.selectStack[len(b.selectStack)-1], OptionValue(el, b.evalAttrConst))
	}
	for _, attr := range attrsForOpening {
		if attr.Spread || attr.Name == "ref" || attr.Name == "dangerouslySetInnerHTML" {
			continue
		}
		if isReactDirectiveAttr(attr.Name) || shadowedFormDefault(attrsForOpening, attr.Name) {
			continue
		}
		if isOnEvent(attr.Name) {
			continue
		}
		if u, ok := actionAttrs[attr]; ok {
			buf.WriteString(renderActionAttr(name, attr.Name, u))
			continue
		}
		if isFormActionAttr(attr.Name) && attr.Value != nil && b.isFunctionAction(attr.Value) {
			continue
		}
		// Dynamic bindings are emitted below (with SSR initial + marker).
		// Statically-resolvable values - even those typed as bindings (e.g.
		// {String(i)}, {length}) - are emitted directly here instead.
		if isAttrBinding(attr) && !b.isStaticResolvable(attr.Value) {
			continue
		}
		if skipFormValueAttr(name, attr.Name, inSelect) {
			continue
		}
		buf.WriteString(RenderAttr(name, attr, b.evalAttrConst, b.evalAttrValue))
	}
	if optionSelected {
		buf.WriteString(" selected")
	}
	if len(actionAttrs) > 0 {
		if name == "form" && !hasMethod {
			buf.WriteString(` method="post"`)
		}
		if !hasFormMethod {
			for attr := range actionAttrs {
				if attr.Name == "formAction" || attr.Name == "formaction" {
					buf.WriteString(` formmethod="post"`)
					break
				}
			}
		}
	}

	// Dynamic attributes: emit the SSR initial value plus the hydration marker.
	for _, a := range attrs {
		if a.HydrateOnly {
			continue
		}
		// The SSR initial value goes through the same table as static
		// attributes; an empty non-boolean initial is omitted.
		switch {
		case skipFormValueAttr(name, a.AttrName, inSelect):
		case a.AttrName == "style":
			if css, ok := StyleCSS(a.InitialExpr, b.evalAttrConst); ok {
				buf.WriteString(` style="` + escape.HTMLAttr(css) + `"`)
			}
		case htmlspec.IsBoolean(a.AttrName) || a.Initial != "":
			buf.WriteString(htmlspec.Attr(name, a.AttrName, JSAttrValue(b.evalAttrConst(a.InitialExpr), a.Initial)))
		}
		if !htmlspec.ValidAttrName(a.AttrName) {
			continue
		}
		buf.WriteString(` data-kattr-` + a.AttrName + `="k:` + escape.HTMLAttr(string(a.ElementSlotID)) + `"`)
	}

	return buf.String()
}

// actionAttrURL resolves a JSX value that names a server action to its POST
// endpoint. Actions arrive as identifiers from the import rewrite, or as string
// literals after a parent forwards an action prop.
func (b *builder) actionAttrURL(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.Identifier:
		u, ok := b.actionURLs[e.Name]
		return u, ok
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" && !e.Computed {
			if prop, ok := e.Property.(*ast.Identifier); ok {
				if raw, ok := b.localProps[prop.Name]; ok && raw.Kind() == jsvalue.KindString {
					val := strings.Trim(raw.StringValue(), `"'`)
					if strings.Contains(val, "/__krate/action/") {
						return val, true
					}
				}
			}
		}
	case *ast.Literal:
		if e.Kind == ast.StringLit && strings.Contains(e.Value, "/__krate/action/") {
			return e.Value, true
		}
	}
	return "", false
}

// skipFormValueAttr reports attributes rendered as content or selection
// instead of an attribute: value/defaultValue on <textarea> and <select>, and
// an option's own selected flag inside a <select value>.
func skipFormValueAttr(tag, name string, inSelect bool) bool {
	if htmlspec.ContentValue(tag, name) || htmlspec.SelectValueProp(tag, name) {
		return true
	}
	return inSelect && name == "selected"
}

// addMarkupErrs records markup diagnostics once (an element can be built more
// than once, e.g. for each instance of a component).
func (b *builder) addMarkupErrs(errs []error) {
	for _, err := range errs {
		dup := false
		for _, have := range b.markupErrs {
			if have.Error() == err.Error() {
				dup = true
				break
			}
		}
		if !dup {
			b.markupErrs = append(b.markupErrs, err)
		}
	}
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

// buildTextSlot - simple signal read

func (b *builder) buildTextSlot(expr ast.Expr, parentID string) *TextSlot {
	signalName := b.extractSignalName(expr)
	if signalName == "" {
		return nil
	}

	id := b.assignSlotID(b.nextDynamicSlot(parentID, "text"))
	decl := b.resolveSignal(signalName)
	initial, _ := childText(b.evalValue(decl.InitialExpr))

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
	// The SSR initial is the value rendered as child text (React semantics:
	// false/null/undefined render nothing); the emitter escapes it.
	initial, _ := childText(b.evalValue(expr))

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
	loopVars := make(valueMap)
	evalProps := func() valueMap {
		m := make(valueMap, len(b.localProps)+len(loopVars))
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
					loopVars[decl.Name] = evalConstValue(decl.Init, b.sigMap(), evalProps())
				} else {
					loopVars[decl.Name] = jsvalue.Undefined()
				}
			}
		}
	}

	savedProps := b.localProps
	defer func() { b.localProps = savedProps }()

	var nodes []SlotNode
	for iter := 0; iter < 1000; iter++ {
		if truthy, known := evalConstValue(fs.Test, b.sigMap(), evalProps()).Truthy(); !known || !truthy {
			break
		}
		b.localProps = evalProps()
		for _, arg := range pushArgs {
			nodes = append(nodes, b.buildSlotNodes(arg, parentID)...)
		}
		if upd, ok := fs.Update.(*ast.UnaryExpr); ok && (upd.Op == "++" || upd.Op == "--") {
			if id, ok := upd.Arg.(*ast.Identifier); ok {
				if cur := loopVars[id.Name]; cur.Kind() == jsvalue.KindNumber {
					if upd.Op == "++" {
						loopVars[id.Name] = jsvalue.Number(cur.NumberValue() + 1)
					} else {
						loopVars[id.Name] = jsvalue.Number(cur.NumberValue() - 1)
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
	if val.ToBoolean() {
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
		// A non-JSX branch renders as a child value (strings escaped,
		// null/false empty); a branch holding JSX goes through the slot
		// pipeline.
		if !hasJSXInExpr(branch) {
			if nodes, ok := staticChildNodes(b.evalValue(branch), true); ok {
				if nodes[0].(*StaticHTML).HTML == "" {
					// Keep a node so the resolved (empty) branch is not
					// mistaken for an unresolved test.
					return []SlotNode{&StaticHTML{HTML: ""}}
				}
				return nodes
			}
		}
		if nodes := b.buildSlotNodes(branch, parentID); len(nodes) > 0 {
			return nodes
		}
		return []SlotNode{&StaticHTML{HTML: ""}}
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
	truthy := val.ToBoolean()
	// && renders the right operand when the left is truthy;
	// || renders the right operand when the left is falsy.
	renderJSX := (bin.Op == "&&") == truthy
	if !renderJSX {
		// The guard evaluates to its left operand, which renders as a child:
		// `{0 && <A/>}` shows 0 (React semantics), false/null/"" show nothing.
		if nodes, ok := staticChildNodes(val, true); ok && nodes[0].(*StaticHTML).HTML != "" {
			return nodes, true
		}
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
func (b *builder) knownTestValue(test ast.Expr) (jsvalue.Value, bool) {
	// {props.children ? <A/> : <B/>} - truthiness comes from call-site children.
	if mem, ok := test.(*ast.MemberExpr); ok {
		if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == "props" {
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "children" {
				return jsvalue.Bool(b.hasCallSiteChildren()), true
			}
		}
	}
	// A typed value that folded completely is authoritative: Unknown
	// propagates through every operator, so only fully-resolved leaves (or a
	// short-circuit that never reads the unknown side) produce a known value.
	v := b.evalValue(test)
	if _, known := v.Truthy(); !known {
		return v, false
	}
	if isPropsMemberOfFuncProp(test, b.localFuncProps) {
		return v, false
	}
	return v, true
}

// isPropsMemberOfFuncProp reports a direct `props.<fn>` read of a function
// prop, which has no build-time value.
func isPropsMemberOfFuncProp(e ast.Expr, funcProps map[string]bool) bool {
	mem, ok := e.(*ast.MemberExpr)
	if !ok || funcProps == nil {
		return false
	}
	id, ok := mem.Object.(*ast.Identifier)
	if !ok || id.Name != "props" {
		return false
	}
	prop, ok := mem.Property.(*ast.Identifier)
	return ok && funcProps[prop.Name]
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
	if ce, ok := expr.(*ast.ConditionalExpr); ok {
		if folded, ok := foldStructuralReact(ce.Test); ok {
			cp := *ce
			cp.Test = folded
			expr = &cp
		}
	}
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "cond"))
	signals := b.collectSignalReads(expr)
	exprSource := generateExprJS(expr, b.sigMap())
	initial := evalConstWithSignals(expr, b.sigMap(), b.localProps)

	cond, _ := expr.(*ast.ConditionalExpr)
	var testJS string
	active := false
	if cond != nil {
		testJS = generateExprJS(cond.Test, b.sigMap())
		active = b.evalValue(cond.Test).ToBoolean()
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
		b.branchDepth++
		slot.Consequent = b.buildSlotNodes(cond.Consequent, string(id)+".c")
		slot.Alternate = b.buildSlotNodes(cond.Alternate, string(id)+".a")
		b.branchDepth--
	}
	return slot
}

// buildMapSlots renders a .map() call. A fully known, non-reactive source is
// flattened directly into static slots: no list markers, no list binding and
// no hydration runtime. Reactive sources keep the ListSlot path.
func (b *builder) buildMapSlots(expr ast.Expr, parentID string) []SlotNode {
	if nodes, ok := b.buildStaticMapSlots(expr, parentID); ok {
		return nodes
	}
	return []SlotNode{b.buildListSlot(expr, parentID)}
}

func (b *builder) buildStaticMapSlots(expr ast.Expr, parentID string) ([]SlotNode, bool) {
	_, mem, ok := mapCallParts(expr)
	if !ok || b.referencesSignal(mem.Object) || b.dynamicListRows > 0 {
		return nil, false
	}
	arr := b.resolveArrayLiteralExpr(expr)
	if arr == nil {
		return nil, false
	}
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "list"))
	items, ok := b.tryResolveArrayItems(arr, expr, string(id), true)
	if !ok {
		return nil, false
	}
	var nodes []SlotNode
	for _, item := range items {
		nodes = append(nodes, item.Contents...)
	}
	if len(nodes) == 0 {
		nodes = []SlotNode{&StaticHTML{HTML: ""}}
	}
	return nodes, true
}

// buildListSlot - .map() with key detection

func (b *builder) buildListSlot(expr ast.Expr, parentID string) *ListSlot {
	b.branchDepth++
	defer func() { b.branchDepth-- }()
	id := b.assignSlotID(b.nextDynamicSlot(parentID, "list"))
	exprSource := generateExprJS(expr, b.sigMap())

	// Resolve SSR items. A .map() over a literal array is resolved directly;
	// a .map() over a build-time constant (a prop, module const, or local var)
	// is resolved by evaluating the array source expression to a literal and
	// parsing it back, so client components still SSR their initial list
	// content instead of rendering an empty slot until hydration.
	b.dynamicListRows++
	items := b.tryResolveItems(expr, string(id))
	if len(items) == 0 {
		if arr := b.resolveArrayLiteralExpr(expr); arr != nil {
			items, _ = b.tryResolveArrayItems(arr, expr, string(id), false)
		}
	}
	b.dynamicListRows--

	slot := &ListSlot{
		ID:         id,
		ExprSource: exprSource,
		Items:      items,
		Signals:    b.collectSignalReads(expr),
		Components: collectListComponents(expr),
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		if plan := planMapCall(call); plan != nil {
			slot.Keyed = plan.KeyJS != ""
			slot.Bind = &ListBinding{Source: plan.SourceJS, Key: plan.KeyJS, Render: plan.RenderJS, Accessor: plan.Accessor}
			b.checkListKeys(call, plan)
		}
	}
	return slot
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
	if prop, ok := mem.Property.(*ast.Identifier); !ok || (prop.Name != "map" && prop.Name != "flatMap") {
		return nil
	}
	if obj, ok := mem.Object.(*ast.ArrayExpr); ok {
		return obj
	}
	// Fold the receiver (a prop, local, module const, signal initial, or a
	// chain such as items.filter(...).slice(0, 3)) to a typed array value and
	// turn its elements back into literals for item substitution.
	v := b.evalValue(mem.Object)
	if !v.IsArray() || v.ContainsUnknown() {
		return nil
	}
	arr, _ := jsvalue.ToAST(v).(*ast.ArrayExpr)
	return arr
}

// tryResolveArrayItems renders a list slot's SSR items from an already-resolved
// ArrayExpr source, using the given .map() call expression to extract the
// mapping arrow. Each element is substituted for the arrow parameter inside a
// clone of the map body, and the concrete body is built through the full slot
// pipeline - so nested components recurse with their own hydration markers and
// member reads (item.title) fold to the element's real literal values.
func (b *builder) tryResolveArrayItems(arr *ast.ArrayExpr, mapExpr ast.Expr, listID string, requireStatic bool) ([]*ListItem, bool) {
	call, ok := mapExpr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, false
	}
	arrow := b.resolveMapCallback(call.Args[0])
	if arrow == nil {
		return nil, false
	}
	bodyExpr, ok := staticMapCallbackBody(arrow)
	if !ok {
		return nil, false
	}
	keyExpr := keyExprOf(bodyExpr)
	seenKeys := map[string]int{}

	items := make([]*ListItem, 0, len(arr.Elements))
	for i, elem := range arr.Elements {
		key := itoa(i)
		if elem == nil {
			// An array hole (`[a, , b]`) maps like undefined.
			elem = undefinedExpr()
		}
		bindings := mapCallbackBindings(arrow, elem, i, arr)
		concrete, ok := b.substituteCallbackLocalsBound(arrow, bodyExpr, bindings, requireStatic)
		if !ok {
			return nil, false
		}
		keyBindings := mapCallbackBindings(arrow, elem, i, arr)
		if keyExpr != nil {
			// The server-rendered row is marked with the item's real key so
			// hydration adopts it by key. Duplicates get the same occurrence
			// suffix the runtime uses.
			k, ok := b.substituteCallbackLocalsBound(arrow, keyExpr, keyBindings, requireStatic)
			if !ok {
				return nil, false
			}
			if folded, ok := b.foldListKey(k); ok {
				key = "$" + folded
				if n, dup := seenKeys[key]; dup {
					seenKeys[key] = n + 1
					b.warnListDuplicateKey(keyExpr, folded)
					key += "\x00" + itoa(n)
				} else {
					seenKeys[key] = 1
				}
			}
		}
		itemID := joinSlotIDKey(SlotID(listID), key)
		restore := b.pushShadow(blockBindingNames(arrow.Body))
		mark := b.markRowBindings()
		contents := b.buildSlotNodes(concrete, string(itemID))
		b.tagRowBindings(mark, SlotID(listID))
		restore()
		if len(contents) == 0 {
			contents = []SlotNode{&StaticHTML{HTML: ""}}
		}
		items = append(items, &ListItem{
			Key:      key,
			Contents: contents,
		})
	}
	return items, true
}

// substituteCallbackLocals substitutes the simple `const name = init` locals a
// block-bodied map callback declares before its return into the rendered
// body. Each init has the loop parameter (and index) substituted first.
func substituteCallbackLocals(arrow *ast.ArrowFn, body ast.Expr, param string, elem ast.Expr, indexName string, index ast.Expr) ast.Expr {
	if arrow == nil || arrow.Expression {
		return body
	}
	type local struct {
		name string
		init ast.Expr
	}
	var locals []local
	for _, stmt := range arrow.Body {
		if _, ok := stmt.(*ast.ReturnStmt); ok {
			break
		}
		vs, ok := stmt.(*ast.VarStmt)
		if !ok || vs.Kind != ast.VarConst {
			continue
		}
		for _, d := range vs.Decls {
			if d == nil || d.IsDestructuring || d.Name == "" || d.Init == nil {
				continue
			}
			init := substituteListParam(d.Init, param, elem)
			if index != nil {
				init = substituteListParam(init, indexName, index)
			}
			for _, prev := range locals {
				init = substituteListParam(init, prev.name, prev.init)
			}
			locals = append(locals, local{name: d.Name, init: init})
		}
	}
	for i := len(locals) - 1; i >= 0; i-- {
		body = substituteListParam(body, locals[i].name, locals[i].init)
	}
	return body
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
		prop := e.Property
		if e.Computed {
			prop = s.sub(e.Property)
		}
		return &ast.MemberExpr{Position: e.Position, Object: obj, Property: prop, Computed: e.Computed, Optional: e.Optional}
	case *ast.CallExpr:
		return &ast.CallExpr{Position: e.Position, Callee: s.sub(e.Callee), Args: subExprList(s, e.Args), Optional: e.Optional}
	case *ast.NewExpr:
		return &ast.NewExpr{Position: e.Position, Callee: s.sub(e.Callee), Args: subExprList(s, e.Args)}
	case *ast.AwaitExpr:
		return &ast.AwaitExpr{Position: e.Position, Arg: s.sub(e.Arg)}
	case *ast.DynamicImport:
		return &ast.DynamicImport{Position: e.Position, Arg: s.sub(e.Arg)}
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
			cp := *p
			cp.Value = s.sub(p.Value)
			if p.Computed && p.KeyExpr != nil {
				cp.KeyExpr = s.sub(p.KeyExpr)
			}
			// `{ item }` must expand to `{ item: <replacement> }` once the
			// value is no longer the bare identifier.
			if p.Shorthand {
				if id, ok := cp.Value.(*ast.Identifier); !ok || id.Name != p.Key {
					cp.Shorthand = false
				}
			}
			out[i] = &cp
		}
		return &ast.ObjectExpr{Position: e.Position, Properties: out}
	case *ast.ParenExpr:
		return &ast.ParenExpr{Position: e.Position, Expr: s.sub(e.Expr)}
	case *ast.TaggedTemplateExpr:
		cp := *e
		cp.Tag = s.sub(e.Tag)
		if q, ok := s.sub(e.Quasi).(*ast.TemplateExpr); ok {
			cp.Quasi = q
		}
		return &cp
	case *ast.JSXElement:
		return substituteListParamJSX(e, s)
	case *ast.JSXFragment:
		return &ast.JSXFragment{Position: e.Position, Children: subJSXChildrenList(s, e.Children), Key: s.sub(e.Key)}
	case *ast.ArrowFn:
		// A nested function (e.g. `onClick={() => rm(item.id)}`) closes over
		// the loop parameter, which no longer exists once the item is
		// rendered on its own, so it is substituted too - unless the function
		// rebinds the name (a parameter or local of the same name).
		if bindingScopeDeclares(functionScopeNames(e.Params, e.Body), s.param) {
			return e
		}
		cp := *e
		cp.Body = s.subStmts(e.Body)
		return &cp
	case *ast.TypeAssertion:
		return &ast.TypeAssertion{Position: e.Position, Expr: s.sub(e.Expr), TypeRef: e.TypeRef}
	default:
		return expr
	}
}

// subStmts substitutes the loop parameter in a nested function body,
// stopping at nested scopes that rebind it.
func (s *listParamSubstituter) subStmts(stmts []ast.Stmt) []ast.Stmt {
	if len(stmts) == 0 {
		return stmts
	}
	out := make([]ast.Stmt, len(stmts))
	for i, stmt := range stmts {
		out[i] = s.subStmt(stmt)
	}
	return out
}

func (s *listParamSubstituter) subStmt(stmt ast.Stmt) ast.Stmt {
	switch st := stmt.(type) {
	case *ast.ExprStmt:
		return &ast.ExprStmt{Position: st.Position, Expression: s.sub(st.Expression)}
	case *ast.ReturnStmt:
		return &ast.ReturnStmt{Position: st.Position, Value: s.sub(st.Value)}
	case *ast.ThrowStmt:
		return &ast.ThrowStmt{Position: st.Position, Value: s.sub(st.Value)}
	case *ast.VarStmt:
		decls := make([]*ast.VarDecl, len(st.Decls))
		for i, d := range st.Decls {
			if d == nil {
				continue
			}
			cp := *d
			cp.Init = s.sub(d.Init)
			decls[i] = &cp
		}
		return &ast.VarStmt{Position: st.Position, Kind: st.Kind, Decls: decls}
	case *ast.IfStmt:
		return &ast.IfStmt{Position: st.Position, Test: s.sub(st.Test), Consequent: s.subBlock(st.Consequent), Alternate: s.subBlock(st.Alternate)}
	case *ast.BlockStmt:
		return &ast.BlockStmt{Position: st.Position, Body: s.subBlock(st.Body)}
	case *ast.WhileStmt:
		return &ast.WhileStmt{Position: st.Position, Test: s.sub(st.Test), Body: s.subBlock(st.Body)}
	case *ast.DoWhileStmt:
		return &ast.DoWhileStmt{Position: st.Position, Body: s.subBlock(st.Body), Test: s.sub(st.Test)}
	case *ast.ForInStmt:
		cp := *st
		cp.Right = s.sub(st.Right)
		if !bindingScopeDeclares(forInBindingNames(st), s.param) {
			cp.Body = s.subBlock(st.Body)
		}
		return &cp
	case *ast.ForStmt:
		if vs, ok := st.Init.(*ast.VarStmt); ok && vs.Kind != ast.VarVar {
			for _, d := range vs.Decls {
				if bindingScopeDeclares(varDeclBindingNames(d), s.param) {
					return st
				}
			}
		}
		cp := *st
		if st.Init != nil {
			cp.Init = s.subStmt(st.Init)
		}
		cp.Test = s.sub(st.Test)
		cp.Update = s.sub(st.Update)
		cp.Body = s.subBlock(st.Body)
		return &cp
	case *ast.SwitchStmt:
		cases := make([]*ast.CaseClause, len(st.Cases))
		for i, c := range st.Cases {
			if c == nil {
				continue
			}
			cases[i] = &ast.CaseClause{Position: c.Position, Test: s.sub(c.Test), Body: s.subBlock(c.Body)}
		}
		return &ast.SwitchStmt{Position: st.Position, Discriminant: s.sub(st.Discriminant), Cases: cases}
	case *ast.TryStmt:
		cp := *st
		cp.Body = s.subBlock(st.Body)
		cp.Finally = s.subBlock(st.Finally)
		if st.Catch != nil && !bindingScopeDeclares(catchBindingNames(st.Catch), s.param) {
			c := *st.Catch
			c.Body = s.subBlock(st.Catch.Body)
			cp.Catch = &c
		}
		return &cp
	case *ast.FnDecl:
		if bindingScopeDeclares(functionScopeNames(st.Params, st.Body), s.param) {
			return st
		}
		cp := *st
		cp.Body = s.subStmts(st.Body)
		return &cp
	}
	// Statements the substituter does not model (for loops with their own
	// bindings, labels, ...) are kept as-is.
	return stmt
}

// subBlock substitutes a nested block unless it declares the parameter name.
func (s *listParamSubstituter) subBlock(stmts []ast.Stmt) []ast.Stmt {
	if bindingScopeDeclares(blockBindingNames(stmts), s.param) {
		return stmts
	}
	return s.subStmts(stmts)
}

func bindingScopeDeclares(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
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
			out = append(out, &ast.JSXFragmentChild{Fragment: &ast.JSXFragment{Position: c.Fragment.Position, Children: subJSXChildrenList(s, c.Fragment.Children), Key: s.sub(c.Fragment.Key)}})
		default:
			out = append(out, child)
		}
	}
	return out
}

func substituteListParamJSX(el *ast.JSXElement, s *listParamSubstituter) *ast.JSXElement {
	if el == nil || el.Opening == nil {
		return el
	}
	opening := &ast.JSXOpening{Name: el.Opening.Name, SelfClosing: el.Opening.SelfClosing}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil {
			opening.Attributes = append(opening.Attributes, attr)
			continue
		}
		opening.Attributes = append(opening.Attributes, &ast.JSXAttr{Position: attr.Position, Name: attr.Name, Value: s.sub(attr.Value), Spread: attr.Spread})
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
			if v.Opening != nil && len(v.Opening.Name) > 0 && v.Opening.Name[0] >= 'A' && v.Opening.Name[0] <= 'Z' {
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
	items, _ := b.tryResolveArrayItems(arr, expr, listID, false)
	return items
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
				result = append(result, &StaticHTML{HTML: escape.Text(c.Value)})
			}
		}
	}
	return result
}

// buildHandlerDecl - extract event handler

func (b *builder) buildHandlerDecl(attr *ast.JSXAttr, elementID string) *HandlerDecl {
	eventName, capture, direct := reactEventName(attr.Name)
	// A handler forwarded as a prop must invoke the function from the child's
	// props registry (created in the parent scope), not an inlined parent-local
	// expression (which is undefined inside the child IIFE).
	if id, ok := attr.Value.(*ast.Identifier); ok && b.localFuncProps != nil && b.localFuncProps[id.Name] {
		return &HandlerDecl{ElementSlotID: SlotID(elementID), Prop: attr.Name, Event: eventName, Body: "props." + id.Name, Capture: capture, Direct: direct}
	}
	if mem, ok := attr.Value.(*ast.MemberExpr); ok && !mem.Computed {
		if obj, ok := mem.Object.(*ast.Identifier); ok && obj.Name == "props" {
			if prop, ok := mem.Property.(*ast.Identifier); ok && b.localFuncProps != nil && b.localFuncProps[prop.Name] {
				return &HandlerDecl{ElementSlotID: SlotID(elementID), Prop: attr.Name, Event: eventName, Body: "props." + prop.Name, Capture: capture, Direct: direct}
			}
		}
	}
	body := b.extractHandlerBody(attr.Value)
	if body == "" {
		return nil
	}
	signals := b.collectSignalReads(attr.Value)

	return &HandlerDecl{
		ElementSlotID: SlotID(elementID),
		Prop:          attr.Name,
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
		Prop:        formDefaultProp(attr.Name),
	}
}

// formDefaultProp returns the JSX name of an uncontrolled form default
// (defaultValue/defaultChecked). Those share their HTML attribute with
// value/checked but must update the default, never the live value.
func formDefaultProp(name string) string {
	if name == "defaultValue" || name == "defaultChecked" {
		return name
	}
	return ""
}

// buildStaticValueSlot - expression with no signals

// buildStaticTextSlot is buildStaticValueSlot for JSX text positions: the
// statically-evaluated result is HTML-escaped so a template literal or string
// expression cannot inject real markup (e.g. <Code>{`return <h1>x</h1>`}</Code>).
func (b *builder) buildStaticTextSlot(expr ast.Expr, parentID string) *StaticHTML {
	v := b.evalValue(expr)
	if nodes, ok := staticChildNodes(v, true); ok {
		return nodes[0].(*StaticHTML)
	}
	return &StaticHTML{HTML: escape.HTML(legacyConstString(expr, v))}
}

// staticChildNodes renders a build-time value in JSX child position with React
// semantics (null/undefined/booleans render nothing, numbers and strings are
// text, arrays concatenate). escapeText=false keeps text raw for meta content
// (inline <script>/<style>). ok=false when the value is not statically known
// or not renderable as static text.
func staticChildNodes(v jsvalue.Value, escapeText bool) ([]SlotNode, bool) {
	if v.ContainsUnknown() {
		return nil, false
	}
	if escapeText {
		html, ok := renderChildValue(v)
		if !ok {
			return nil, false
		}
		return []SlotNode{&StaticHTML{HTML: html}}, true
	}
	text, ok := childText(v)
	if !ok {
		return nil, false
	}
	return []SlotNode{&StaticHTML{HTML: text}}, true
}

// Signal resolution helpers

// setterNameFor derives the conventional setter name (`count` gives
// `setCount`); an empty name has no setter.
func setterNameFor(name string) string {
	if name == "" {
		return ""
	}
	return "set" + strings.ToUpper(name[:1]) + name[1:]
}

func (b *builder) resolveSignal(name string) SignalDecl {
	if initial, ok := b.sigMap()[name]; ok {
		return SignalDecl{
			Name:        name,
			SetterName:  setterNameFor(name),
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
		return b.isSignalName(e.Name)
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if ok := b.isSignalName(id.Name); ok {
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
		if b.isLivePropRead(e) {
			return true
		}
		// `tabs[index()]` reads a signal in its computed key.
		if e.Computed && b.referencesSignal(e.Property) {
			return true
		}
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
		// inside the thunk matters for dependency ordering. Parameters and
		// body-level declarations shadow component signals of the same name
		// (`items.map(count => count)` does not read a `count` signal).
		defer b.pushShadow(functionScopeNames(e.Params, e.Body))()
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

// isLivePropRead reports whether e reads a getter-registered prop
// (`props.title` where the caller passed a signal-derived value).
func (b *builder) isLivePropRead(e *ast.MemberExpr) bool {
	if len(b.liveProps) == 0 || b.livePropsParam == "" || e.Computed {
		return false
	}
	obj, ok := e.Object.(*ast.Identifier)
	if !ok || obj.Name != b.livePropsParam || b.isShadowed(obj.Name) {
		return false
	}
	prop, ok := e.Property.(*ast.Identifier)
	return ok && b.liveProps[prop.Name]
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
		// An identifier initializer the build cannot resolve (a module
		// constant or binding only known at run time) must be evaluated on
		// the client, not emitted as its own name.
		if id, ok := d.Initial.(*ast.Identifier); ok && rawInit == "" && operandLeaks(d.Initial, b.sigMap(), b.localProps) {
			rawInit = id.Name
		}
		// Array/object initializers must be emitted as real JS literals (their
		// folded value is JS source, not a string), so keep the source verbatim.
		if rawInit == "" {
			switch init := d.Initial.(type) {
			case *ast.ArrayExpr, *ast.ObjectExpr:
				rawInit = generateExprJS(d.Initial, b.sigMap())
			case *ast.CallExpr:
				// A signal seeded from an earlier signal (`createSignal(items())`,
				// e.g. lowered useOptimistic) reads it at hydration; its folded
				// SSR value may be an array or object rendered as text.
				if id, ok := init.Callee.(*ast.Identifier); ok && len(init.Args) == 0 && !isResourceSentinelExpr(init, b.sigMap()) {
					if _, isSig := b.sigMap()[id.Name]; isSig {
						rawInit = generateExprJS(d.Initial, b.sigMap())
					}
				}
				// A call that folded to an object or array (Object.assign,
				// a class's getDerivedStateFromProps) runs on the client too:
				// the folded source would otherwise be emitted as a string.
				if rawInit == "" && (strings.HasPrefix(initial, "{") || strings.HasPrefix(initial, "[")) {
					rawInit = generateExprJS(d.Initial, b.sigMap())
				}
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
	// Further arguments (React's init function, signal options) pass through.
	extra := ""
	for _, a := range d.Args[2:] {
		js := generateExprJS(a, b.sigMap())
		if js == "" {
			js = "undefined"
		}
		extra += "," + js
	}
	return "createReducer(" + reducer + "," + initial + extra + ")"
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
func signalRawInit(expr ast.Expr, folded string, signals map[string]ast.Expr, props valueMap) string {
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
				if isSig := b.isSignalName(id.Name); isSig {
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
			restore := b.pushShadow(functionScopeNames(v.Params, v.Body))
			walkStmt(v.Body)
			restore()
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
				restore := b.pushShadow(functionScopeNames(s.Params, s.Body))
				walkStmt(s.Body)
				restore()
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
						case "createEffectDeps", "useImperativeHandle":
							// React useEffect/useLayoutEffect with a non-empty
							// dependency list: the deps thunk is the second arg.
							// useImperativeHandle attaches a ref handle (the
							// hydration emitter runs it in the ref phase).
							if len(call.Args) >= 1 {
								effects = append(effects, generateExprJS(call, b.sigMap()))
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
func (b *builder) collectResourceJS(body []ast.Stmt, order *bodyOrder) []bodyJS {
	var out []bodyJS
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
			out = append(out, bodyJS{
				ord: order.of(decl),
				js:  renderStmtJS(&ast.VarStmt{Kind: vs.Kind, Decls: []*ast.VarDecl{decl}}, b.sigMap()),
			})
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
			if !ok || (id.Name != "createMemo" && id.Name != "createMemoDeps") {
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
		defer b.pushShadow(blockBindingNames(s.Body))()
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
		if vs, ok := s.Init.(*ast.VarStmt); ok && vs.Kind != ast.VarVar {
			var names []string
			for _, d := range vs.Decls {
				names = append(names, varDeclBindingNames(d)...)
			}
			defer b.pushShadow(names)()
		}
		if b.referencesSignal(s.Test) || b.referencesSignal(s.Update) || stmtsReferenceSignal(s.Body, b) {
			return true
		}
	case *ast.ForInStmt:
		if b.referencesSignal(s.Right) {
			return true
		}
		defer b.pushShadow(forInBindingNames(s))()
		if stmtsReferenceSignal(s.Body, b) {
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
		if s.Catch != nil {
			restore := b.pushShadow(catchBindingNames(s.Catch))
			hit := stmtsReferenceSignal(s.Catch.Body, b)
			restore()
			if hit {
				return true
			}
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
			if ok := b.isSignalName(v.Name); ok && !seen[v.Name] {
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
			restore := b.pushShadow(functionScopeNames(v.Params, v.Body))
			for _, stmt := range v.Body {
				if ret, ok := stmt.(*ast.ReturnStmt); ok {
					walk(ret.Value)
				}
			}
			restore()
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
		// Any other binding (an imported function, a module const, a
		// destructured prop): resolve it when the event fires. `typeof` keeps
		// an unbound name from throwing, and a non-function value is ignored
		// the way React ignores it.
		return "function(e){if(typeof " + e.Name + "===\"function\")return " + e.Name + ".call(this,e);}"
	case *ast.MemberExpr:
		// A function prop forwarded directly, e.g. onClick={props.onClick}.
		// Render it as a live reference resolved through the child's props
		// object (var props = __krate_props[...]) at hydration time.
		return generateExprJS(e, b.sigMap())
	case *ast.TypeAssertion:
		return b.extractHandlerBody(e.Expr)
	case nil:
		return ""
	default:
		return b.guardedHandlerJS(expr)
	}
}

// guardedHandlerJS emits a handler for an arbitrary function-valued
// expression (`flag ? a : b`, `makeHandler(id)`, `handlers[kind]`). An
// expression that reads signals is evaluated on every event so it sees the
// current state; any other expression is evaluated once when the handler is
// bound (so `debounce(fn)` keeps one debounced instance, as in React). The
// result is invoked only when it is a function.
func (b *builder) guardedHandlerJS(expr ast.Expr) string {
	js := generateExprJS(expr, b.sigMap())
	if js == "" {
		return ""
	}
	const call = "if(typeof __h===\"function\")return __h.call(this,e);"
	if b.referencesSignal(expr) {
		return "function(e){var __h=(" + js + ");" + call + "}"
	}
	return "(function(){var __h=(" + js + ");return function(e){" + call + "};})()"
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
		if b.signalSetters != nil && b.signalSetters[e.Name] {
			return true
		}
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

// cloneStringMap copies a string map so a provider can override context
// markers for its subtree without leaking them to siblings.
func cloneStringMap(m valueMap) valueMap {
	out := make(valueMap, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// buildExternalComponentSlot captures an unresolved component imported from a
// bare npm package as a runtime island: it emits a placeholder and records the
// whole JSX subtree, which the build bundles against Preact.
func (b *builder) buildExternalComponentSlot(name string, el *ast.JSXElement) (SlotNode, bool) {
	root := name
	var members []string
	if i := strings.IndexByte(name, '.'); i >= 0 {
		root = name[:i]
		members = strings.Split(name[i+1:], ".")
	}
	imp, ok := b.externalImports[root]
	if !ok || !isExternalPackageSpecifier(imp.Source) {
		return nil, false
	}
	// A component that resolved to Krate source (for example a path-aliased
	// local module) is compiled normally. Only components whose implementation
	// lives in node_modules become runtime islands; unresolved ones (no function)
	// fall through to the bare-import island path.
	if fn, known := b.functions[name]; known && fn != nil {
		src := b.ann.ComponentSources[fn.Name]
		if !strings.Contains(strings.ReplaceAll(src, "\\", "/"), "/node_modules/") {
			return nil, false
		}
	}
	if imp.Namespace && len(members) == 0 {
		return nil, false
	}
	id := "island-" + strconv.Itoa(len(b.externals)+1)
	// Replace page-scope signal reads with island props and collect their value
	// expressions for the page hydration boot.
	usedSignals := map[string]bool{}
	b.bridgeIslandExpr(el, usedSignals)
	subtree := astprint.PrintExpr(el)
	if subtree == "" {
		return nil, false
	}
	propsJS := ""
	ssrPropsJS := "{}"
	ssrKnown := true
	if len(usedSignals) > 0 {
		names := make([]string, 0, len(usedSignals))
		for n := range usedSignals {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		ssrParts := make([]string, 0, len(names))
		for _, n := range names {
			if strings.HasPrefix(n, "fn:") {
				base := strings.TrimPrefix(n, "fn:")
				parts = append(parts, strconv.Quote(base)+":"+base)
				continue
			}
			parts = append(parts, strconv.Quote(n)+":"+generateExprJS(&ast.CallExpr{Callee: &ast.Identifier{Name: n}}, b.sigMap()))
			v := b.evalValue(&ast.Identifier{Name: n})
			src, ok := jsvalue.JSSource(v)
			if !ok || v.ContainsUnknown() || v.IsFunction() || v.IsJSX() {
				ssrKnown = false
				continue
			}
			ssrParts = append(ssrParts, strconv.Quote(n)+":"+src)
		}
		propsJS = "{" + strings.Join(parts, ",") + "}"
		ssrPropsJS = "{" + strings.Join(ssrParts, ",") + "}"
	}
	if propsJS != "" && b.currentNode != nil {
		b.currentNode.IslandBoot = append(b.currentNode.IslandBoot,
			"(globalThis.__krate_island_values||(globalThis.__krate_island_values={}))["+strconv.Quote(id)+"]=function(){return ("+propsJS+");};",
			"(globalThis.__krate_island_subscribe||(globalThis.__krate_island_subscribe={}))["+strconv.Quote(id)+"]=function(cb){return createEffect(function(){ (globalThis.__krate_island_values["+strconv.Quote(id)+"])(); cb(); });};",
		)
	}
	// Same-file local functions live in the page's Krate runtime, which is a
	// different runtime from the island's Preact tree. Imported local modules
	// are bundled; inline functions need an explicit diagnostic, never a
	// silently blank island.
	for name, fn := range b.functions {
		if fn == nil || name == root || !strings.Contains(subtree, "<"+name) {
			continue
		}
		if _, external := b.externalImports[name]; external {
			continue
		}
		if b.localImports[name] {
			continue
		}
		src := strings.ReplaceAll(b.ann.ComponentSources[fn.Name], "\\", "/")
		if src != "" && strings.Contains(src, "/node_modules/") {
			continue
		}
		pos := el.Position
		if inner := findJSXElementNamed(el, name); inner != nil {
			pos = inner.Position
		}
		b.warnCodeAt(pos, diag.CodeIslandLocalComponent, "external island for <"+root+"> references same-file local component <"+name+">, which cannot cross the Krate/Preact island boundary",
			"move <"+name+"> into its own module and import it, or render it outside the island")
	}

	props := map[string]any{}
	if el.Opening != nil {
		for _, attr := range el.Opening.Attributes {
			if attr == nil || attr.Spread || attr.Name == "" || attr.Value == nil {
				continue
			}
			lit, ok := attr.Value.(*ast.Literal)
			if !ok {
				continue
			}
			switch lit.Kind {
			case ast.StringLit:
				props[attr.Name] = lit.Value
			case ast.NumberLit:
				props[attr.Name] = lit.Value
			case ast.BoolLit:
				props[attr.Name] = lit.Value == "true"
			}
		}
	}
	b.externals = append(b.externals, ExternalComponent{
		ID: id, Source: imp.Source, Local: root, Members: members,
		Namespace: imp.Namespace, Props: props, Subtree: subtree, PropsJS: propsJS,
		SSRPropsJS: ssrPropsJS, SSRPropsKnown: ssrKnown,
		File: b.ann.SourceFile, Line: el.Position.Line, Col: el.Position.Col,
	})
	html := `<div data-krate-island="` + id + `" style="display:contents"></div>`
	return &StaticHTML{HTML: html}, true
}

// isExternalPackageSpecifier reports whether src is a bare npm package import
// (not relative, and not a Krate virtual/workspace package).
func isExternalPackageSpecifier(src string) bool {
	if src == "" {
		return false
	}
	if strings.HasPrefix(src, ".") || strings.HasPrefix(src, "/") {
		return false
	}
	if strings.HasPrefix(src, "krate/") || strings.HasPrefix(src, "@krate/") {
		return false
	}
	return true
}

// activeContextValue returns the provider's value expression when expr is a
// context read inside a dynamic provider subtree.
func (b *builder) activeContextValue(expr ast.Expr) (ast.Expr, bool) {
	if len(b.contextValues) == 0 || !isContextRead(expr) {
		return nil, false
	}
	ctx, ok := contextReadTarget(expr)
	if !ok {
		return nil, false
	}
	v, found := b.contextValues[ctx]
	if !found || v == nil {
		return nil, false
	}
	return v, true
}

// staticContextValue folds a provider value that is a build-time constant:
// it reads no signal and evaluates to a serializable value (no functions, no
// unknowns), so consumers can fold it like a context default.
func (b *builder) staticContextValue(expr ast.Expr) (jsvalue.Value, bool) {
	if expr == nil {
		return jsvalue.Value{}, false
	}
	if s, ok := literalString(expr); ok {
		return jsvalue.String(s), true
	}
	if b.referencesSignal(expr) {
		return jsvalue.Value{}, false
	}
	v := evalConstValue(expr, b.sigMap(), b.localProps)
	if v.ContainsUnknown() {
		return jsvalue.Value{}, false
	}
	if _, ok := jsvalue.JSSource(v); !ok {
		return jsvalue.Value{}, false
	}
	return v, true
}

// contextReadTarget returns the context identifier of `Ctx.useContext()` /
// `useContext(Ctx)`.
func contextReadTarget(expr ast.Expr) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	if mem, ok := call.Callee.(*ast.MemberExpr); ok {
		if id, ok := mem.Object.(*ast.Identifier); ok {
			return id.Name, true
		}
	}
	if id, ok := call.Callee.(*ast.Identifier); ok && (id.Name == "useContext" || id.Name == "use") && len(call.Args) == 1 {
		if arg, ok := call.Args[0].(*ast.Identifier); ok {
			return arg.Name, true
		}
	}
	return "", false
}

// cloneExprMap copies a map of context value expressions so a provider can
// override one entry for its subtree without leaking to siblings.
func cloneExprMap(m map[string]ast.Expr) map[string]ast.Expr {
	out := make(map[string]ast.Expr, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// buildPortalSlot builds the built-in <Portal container={...}> (container
// defaults to document.body).
func (b *builder) buildPortalSlot(el *ast.JSXElement, parentID string) []SlotNode {
	var container ast.Expr
	for _, attr := range el.Opening.Attributes {
		if attr != nil && !attr.Spread && attr.Name == "container" {
			container = attr.Value
		}
	}
	return b.buildPortal(&ast.JSXFragment{Children: el.Children}, container, el.Position, parentID)
}

// bridgeIslandExpr rewrites page-scope signal reads in an external island
// subtree to island props, collecting the signal names used (their values are
// evaluated in the page's hydration scope and handed to the island at mount).
func (b *builder) bridgeIslandExpr(el *ast.JSXElement, used map[string]bool) {
	if el == nil {
		return
	}
	if el.Opening != nil {
		for _, attr := range el.Opening.Attributes {
			if attr != nil && attr.Value != nil {
				attr.Value = b.bridgeScopeExpr(attr.Value, used)
			}
		}
	}
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			c.Expression = b.bridgeScopeExpr(c.Expression, used)
		case *ast.JSXElementChild:
			b.bridgeIslandExpr(c.Element, used)
		case *ast.JSXFragmentChild:
			b.bridgeIslandFragment(c.Fragment, used)
		}
	}
}

func (b *builder) bridgeIslandFragment(frag *ast.JSXFragment, used map[string]bool) {
	if frag == nil {
		return
	}
	for _, child := range frag.Children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			c.Expression = b.bridgeScopeExpr(c.Expression, used)
		case *ast.JSXElementChild:
			b.bridgeIslandExpr(c.Element, used)
		case *ast.JSXFragmentChild:
			b.bridgeIslandFragment(c.Fragment, used)
		}
	}
}

func (b *builder) bridgeScopeExpr(expr ast.Expr, used map[string]bool) ast.Expr {
	switch e := expr.(type) {
	case *ast.Identifier:
		if ok := b.isSignalName(e.Name); ok {
			used[e.Name] = true
			return &ast.MemberExpr{Object: &ast.Identifier{Name: "props"}, Property: &ast.Identifier{Name: e.Name}}
		}
		if b.isFuncReference(e) {
			used["fn:"+e.Name] = true
			return &ast.MemberExpr{Object: &ast.Identifier{Name: "props"}, Property: &ast.Identifier{Name: e.Name}}
		}
		if v, ok := b.localProps[e.Name]; ok && e.Name != "props" && e.Name != "children" && !strings.HasPrefix(e.Name, "__krate_") {
			if lit := jsvalue.ToAST(v); lit != nil {
				return lit
			}
		}
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok && len(e.Args) == 0 {
			if sig := b.isSignalName(id.Name); sig {
				used[id.Name] = true
				return &ast.MemberExpr{Object: &ast.Identifier{Name: "props"}, Property: &ast.Identifier{Name: id.Name}}
			}
		}
		e.Callee = b.bridgeScopeExpr(e.Callee, used)
		for i := range e.Args {
			e.Args[i] = b.bridgeScopeExpr(e.Args[i], used)
		}
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" && !e.Computed {
			if pid, ok := e.Property.(*ast.Identifier); ok && b.localFuncProps != nil && b.localFuncProps[pid.Name] {
				used["fn:"+pid.Name] = true
				return &ast.MemberExpr{Object: &ast.Identifier{Name: "props"}, Property: &ast.Identifier{Name: pid.Name}}
			}
		}
		e.Object = b.bridgeScopeExpr(e.Object, used)
		if e.Computed {
			e.Property = b.bridgeScopeExpr(e.Property, used)
		}
	case *ast.BinaryExpr:
		e.Left = b.bridgeScopeExpr(e.Left, used)
		e.Right = b.bridgeScopeExpr(e.Right, used)
	case *ast.UnaryExpr:
		e.Arg = b.bridgeScopeExpr(e.Arg, used)
	case *ast.ConditionalExpr:
		e.Test = b.bridgeScopeExpr(e.Test, used)
		e.Consequent = b.bridgeScopeExpr(e.Consequent, used)
		e.Alternate = b.bridgeScopeExpr(e.Alternate, used)
	case *ast.ArrayExpr:
		for i := range e.Elements {
			e.Elements[i] = b.bridgeScopeExpr(e.Elements[i], used)
		}
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p != nil {
				p.Value = b.bridgeScopeExpr(p.Value, used)
			}
		}
	case *ast.TemplateExpr:
		for i := range e.Parts {
			e.Parts[i] = b.bridgeScopeExpr(e.Parts[i], used)
		}
	case *ast.ArrowFn:
		b.bridgeScopeStmts(e.Body, used)
	case *ast.JSXElement:
		b.bridgeIslandExpr(e, used)
	case *ast.JSXFragment:
		b.bridgeIslandFragment(e, used)
	}
	return expr
}

func (b *builder) bridgeScopeStmts(stmts []ast.Stmt, used map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			s.Value = b.bridgeScopeExpr(s.Value, used)
		case *ast.ExprStmt:
			s.Expression = b.bridgeScopeExpr(s.Expression, used)
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d != nil {
					d.Init = b.bridgeScopeExpr(d.Init, used)
				}
			}
		case *ast.IfStmt:
			s.Test = b.bridgeScopeExpr(s.Test, used)
			b.bridgeScopeStmts(s.Consequent, used)
			b.bridgeScopeStmts(s.Alternate, used)
		case *ast.BlockStmt:
			b.bridgeScopeStmts(s.Body, used)
		case *ast.ForStmt:
			if s.Init != nil {
				b.bridgeScopeStmts([]ast.Stmt{s.Init}, used)
			}
			s.Test = b.bridgeScopeExpr(s.Test, used)
			s.Update = b.bridgeScopeExpr(s.Update, used)
			b.bridgeScopeStmts(s.Body, used)
		case *ast.WhileStmt:
			s.Test = b.bridgeScopeExpr(s.Test, used)
			b.bridgeScopeStmts(s.Body, used)
		case *ast.DoWhileStmt:
			b.bridgeScopeStmts(s.Body, used)
			s.Test = b.bridgeScopeExpr(s.Test, used)
		}
	}
}

// foldLocalConsts folds a component's local constants using the cva/cn-aware
// attribute evaluator, returning name -> value and extending props with them.
func (b *builder) foldLocalConsts(fn *ast.FnDecl, props valueMap, nextUseId func() string) valueMap {
	saved := b.localProps
	b.localProps = props
	fold := collectLocalVars(fn.Body, b.sigMap(), props, nextUseId)
	locals, inits := fold.locals, fold.inits
	for name, init := range inits {
		if init == nil || fold.changed[name] {
			continue
		}
		switch init.(type) {
		case *ast.ArrayExpr, *ast.ObjectExpr, *ast.JSXElement, *ast.JSXFragment, *ast.ArrowFn:
			continue
		}
		if v := b.evalAttrConst(init); !v.ContainsUnknown() && !v.IsFunction() && !v.IsJSX() {
			locals[name] = v
			props[name] = v
		}
	}
	b.localProps = saved
	return locals
}

// hasComponentChildren reports whether call-site children include a component
// element. Such wrappers must take the IR path so nested client components keep
// their hydration markers; the SSREval shortcut would flatten them.
func hasComponentChildren(children []ast.JSXChild) bool {
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			if c.Element == nil || c.Element.Opening == nil {
				continue
			}
			name := c.Element.Opening.Name
			if name != "" && name[0] >= 'A' && name[0] <= 'Z' && !isNonClientBuiltinTag(name) {
				return true
			}
			if c.Element.Opening != nil && hasComponentChildren(c.Element.Children) {
				return true
			}
		case *ast.JSXFragmentChild:
			if hasComponentChildren(c.Fragment.Children) {
				return true
			}
		}
	}
	return false
}

// isNonClientBuiltinTag reports tags the compiler handles itself (meta, media,
// icon) and which do not require a client component boundary.
func isNonClientBuiltinTag(name string) bool {
	switch name {
	case "Head", "head", "Script", "script", "Style", "style", "Link", "Icon", "Image", "Suspense":
		return true
	}
	return false
}
