package irtree

import "github.com/kratejs/krate/packages/compiler/ast"

// buildExprContainerChildrenMode routes a {expr} JSX container child to the
// correct slot type. When `doEscape` is true the statically-evaluated result is
// HTML-escaped because it sits in a JSX text position (a template literal like
// <Code>{`function App() { return <h1>...</h1>; }`}</Code> must not leak real
// markup). Meta content passes doEscape=false so inline scripts stay raw.
//
// The routes are tried in order: JSX values (including substituted context
// values and folded React structural helpers), children placeholders, CSS
// signal live text, control flow (lists, ternaries, guards, JSX arrays), then
// a static value or a hydration binding.
func (b *builder) buildExprContainerChildrenMode(ec *ast.JSXExprContainer, parentID string, doEscape bool) []SlotNode {
	expr := ec.Expression
	if expr == nil {
		return nil
	}
	if nodes, ok := b.jsxValueChild(expr, parentID, doEscape); ok {
		return nodes
	}
	if isChildrenPlaceholder(expr) {
		b.recordChildrenContext()
		return []SlotNode{&ChildrenSlot{}}
	}
	if b.isCSSLiveText(expr) {
		return []SlotNode{&StaticHTML{HTML: `<span class="krc-live"></span>`}}
	}
	if nodes, ok := b.controlFlowChild(expr, parentID, doEscape); ok {
		return nodes
	}
	if !b.referencesSignal(expr) {
		return b.nonReactiveChild(expr, parentID, doEscape)
	}
	// Simple signal read: {count()}
	if isSimpleSignalRead(expr) {
		if slot := b.buildTextSlot(expr, parentID); slot != nil {
			return []SlotNode{slot}
		}
		return nil
	}
	// Complex expression (binary, member chain, template, etc.).
	return []SlotNode{b.buildExprSlot(expr, parentID)}
}

// exprChild builds a sub-expression as a container child.
func (b *builder) exprChild(expr ast.Expr, parentID string, doEscape bool) []SlotNode {
	return b.buildExprContainerChildrenMode(&ast.JSXExprContainer{Expression: expr}, parentID, doEscape)
}

// jsxValueChild handles children that are, or resolve to, JSX: a JSX node
// produced by a build-time substitution (e.g. the child injected by
// Children.map), an active dynamic context value, and React structural
// helpers that fold when their inputs are statically known.
func (b *builder) jsxValueChild(expr ast.Expr, parentID string, doEscape bool) ([]SlotNode, bool) {
	switch e := expr.(type) {
	case *ast.JSXElement:
		return b.buildSlotNodes(e, parentID), true
	case *ast.JSXFragment:
		return b.buildFragmentSlots(e, parentID), true
	}
	if v, ok := b.activeContextValue(expr); ok {
		if slot := b.buildTextSlot(v, parentID); slot != nil {
			return []SlotNode{slot}, true
		}
		return b.exprChild(v, parentID, doEscape), true
	}
	folded, ok := foldStructuralReact(expr)
	if !ok {
		return nil, false
	}
	switch f := folded.(type) {
	case *ast.JSXElement:
		return b.buildSlotNodes(f, parentID), true
	case *ast.JSXFragment:
		return b.buildFragmentSlots(f, parentID), true
	case *ast.ArrayExpr:
		frag := &ast.JSXFragment{}
		for _, el := range f.Elements {
			switch e := el.(type) {
			case *ast.JSXElement:
				frag.Children = append(frag.Children, &ast.JSXElementChild{Element: e})
			case *ast.JSXFragment:
				frag.Children = append(frag.Children, &ast.JSXFragmentChild{Fragment: e})
			default:
				frag.Children = append(frag.Children, &ast.JSXExprContainer{Expression: e})
			}
		}
		return b.buildFragmentSlots(frag, parentID), true
	}
	return b.exprChild(folded, parentID, doEscape), true
}

// isChildrenPlaceholder reports `{children}` and `{props.children}`, the
// layout children placeholder.
func isChildrenPlaceholder(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Identifier:
		return e.Name == "children"
	case *ast.MemberExpr:
		id, ok := e.Object.(*ast.Identifier)
		if !ok || id.Name != "props" {
			return false
		}
		pid, ok := e.Property.(*ast.Identifier)
		return ok && pid.Name == "children"
	}
	return false
}

// isCSSLiveText reports CSS signal live text, rendered with zero JS through
// the inherited `--krate-current` custom property (`.krc-live::after`): a
// bare `{getter()}` read of a compiled scope, or `{scope.current()}` on a
// scope handle (which marks the scope as needing live text).
func (b *builder) isCSSLiveText(expr ast.Expr) bool {
	if b.cssSignals != nil && b.cssSignals.OK() {
		if _, ok := b.cssSignals.MatchText(expr); ok {
			return true
		}
	}
	if name, ref, _, ok := b.helperCall(expr); ok && name == "current" {
		ref.scope.LiveText = true
		return true
	}
	return false
}

// controlFlowChild handles children that choose or repeat JSX: `.map()`
// lists (always resolved at build time when possible), ternaries and boolean
// guards with JSX branches, and array literals holding JSX.
func (b *builder) controlFlowChild(expr ast.Expr, parentID string, doEscape bool) ([]SlotNode, bool) {
	if isMapCall(expr) {
		return []SlotNode{b.buildListSlot(expr, parentID)}, true
	}
	// Ternaries are checked BEFORE the static value path, otherwise
	// unresolved variables (e.g. title, showCopy from props) make evalConst
	// fall through to a null alternate and emit "null". A statically-known
	// test builds the winning branch directly so SSR renders real content.
	if cond, ok := expr.(*ast.ConditionalExpr); ok {
		if nodes := b.tryResolveConditional(cond, parentID); len(nodes) > 0 {
			return nodes, true
		}
		if isTernaryWithJSX(expr) {
			return []SlotNode{b.buildConditionalSlot(expr, parentID)}, true
		}
	}
	// {left && <A/>} and {left || <A/>}: the static-value path cannot
	// serialize JSX, so guards go through the conditional-slot machinery.
	// Statically-known tests fold to the winning branch; reactive guards
	// become hidable ConditionalSlots.
	if bin, ok := expr.(*ast.BinaryExpr); ok && (bin.Op == "&&" || bin.Op == "||") && hasJSXInExpr(bin.Right) {
		if nodes, resolved := b.tryResolveGuard(bin, parentID); resolved {
			return nodes, true
		}
		return []SlotNode{b.buildGuardSlot(bin, parentID)}, true
	}
	// React flattens arrays in child position: render each element in order.
	if arr, ok := expr.(*ast.ArrayExpr); ok && arrayHasJSX(arr) {
		var nodes []SlotNode
		for _, el := range arr.Elements {
			if el != nil {
				nodes = append(nodes, b.exprChild(el, parentID, doEscape)...)
			}
		}
		return nodes, true
	}
	return nil, false
}

// nonReactiveChild renders a child that reads no signal: statically when its
// value is known at build time (with React child semantics), otherwise as a
// hydration expression slot.
func (b *builder) nonReactiveChild(expr ast.Expr, parentID string, doEscape bool) []SlotNode {
	id, isIdent := expr.(*ast.Identifier)
	if !isIdent {
		if nodes, ok := staticChildNodes(b.evalValue(expr), doEscape); ok {
			return nodes
		}
		return []SlotNode{b.buildExprSlot(expr, parentID)}
	}
	// A for-loop-built JSX array (components like OTPField render their
	// imperatively-pushed element lists).
	if nodes := b.tryResolveForLoopArray(expr, parentID); len(nodes) > 0 {
		return nodes
	}
	// Module-level constants and folded locals have known values: render them
	// statically instead of a hydration binding to an out-of-scope identifier.
	// Anything else must be evaluated at hydration time, or evalConst would
	// render the variable name as literal text.
	if id.Name != "children" {
		if v, isConst := b.moduleConsts[id.Name]; isConst {
			if nodes, ok := staticChildNodes(v, doEscape); ok {
				return nodes
			}
		}
		if v, ok := b.localProps[id.Name]; ok {
			if nodes, ok := staticChildNodes(v, doEscape); ok {
				return nodes
			}
		}
	}
	return []SlotNode{b.buildExprSlot(expr, parentID)}
}
