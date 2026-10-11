package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/csssignals"
)

// isIntrinsicTag reports whether a JSX tag name is a lowercase HTML element.
func isIntrinsicTag(el *ast.JSXElement) bool {
	if el == nil || el.Opening == nil {
		return false
	}
	name := el.Opening.Name
	return name != "" && name[0] >= 'a' && name[0] <= 'z'
}

// rewriteCSSSignalElement returns a copy of el rewritten for CSS signal
// semantics, or nil when el is neither a trigger nor a panel:
//
//   - trigger: the setter event handler is removed and the element becomes a
//     `<label for>` pointing at the controller input, with the option's trigger
//     class added.
//   - panel: the showIf condition is removed, the panel gains the base class,
//     and it is wrapped in a display:contents element carrying the panel class
//     so the show/hide toggles the wrapper (not the panel's own display).
//
// All rewrites operate on copies; the shared AST is never mutated.
func (b *builder) rewriteCSSSignalElement(el *ast.JSXElement) ast.Expr {
	a := b.cssSignals
	if a == nil {
		return nil
	}

	// Panel first: an element whose showIf matches one of our scopes.
	if panel, ok := a.MatchPanel(el); ok {
		return b.buildCSSPanel(el, panel)
	}

	// Trigger: an element whose handler calls one of our setters.
	if trigger, ok := a.MatchTrigger(el); ok {
		return b.buildCSSTrigger(el, trigger)
	}
	return nil
}

// buildCSSTrigger rewrites a trigger element into a `<label>`.
func (b *builder) buildCSSTrigger(el *ast.JSXElement, t csssignals.Trigger) ast.Expr {
	return b.buildCSSTriggerAt(el, t, "")
}

// buildCSSTriggerAt is buildCSSTrigger with an explicit enclosing stack node,
// used to resolve pop()/push() relative to the panel being built.
func (b *builder) buildCSSTriggerAt(el *ast.JSXElement, t csssignals.Trigger, node string) ast.Expr {
	if !csssignals.CanBeLabel(el.Opening.Name) {
		// Validation rejects this before we get here; keep the element intact
		// as a defensive no-op.
		return nil
	}
	clone := shallowCloneJSX(el)
	clone.Opening.Name = "label"
	clone.Opening.SelfClosing = false
	if clone.Closing != nil {
		clone.Closing.Name = "label"
	}
	var kept []*ast.JSXAttr
	for _, attr := range el.Opening.Attributes {
		if attr == nil {
			continue
		}
		if isOnEvent(attr.Name) || attr.Name == "type" || attr.Name == "href" {
			continue
		}
		kept = append(kept, attr)
	}
	clone.Opening.Attributes = kept
	if t.StackAction == "inc" || t.StackAction == "dec" {
		return b.buildRangeStepper(clone, t)
	}
	if t.StackAction == "pop" {
		return b.buildStackPopTrigger(clone, t, node)
	}
	// A group trigger whose option is already selected must close the group on a
	// second click. A single <label for=radio> can't uncheck a radio, so emit a
	// second label (shown only while the option is checked) pointing at the
	// closed sentinel.
	if t.Scope.Kind == csssignals.KindGroup && t.Scope.Sentinel == "" && t.Option != "" {
		return b.buildGroupTrigger(clone, t)
	}
	appendClass(clone, t.Scope.TriggerClass(t.Option))
	// A null/closed group option has no controller input; clicking it cannot
	// point at one, so close via the sentinel's hidden radio.
	clone.Opening.Attributes = append(clone.Opening.Attributes, &ast.JSXAttr{
		Name:  "for",
		Value: &ast.Literal{Kind: ast.StringLit, Value: t.Scope.ControlID(b.cssToken, t.Option)},
	})
	b.applyTriggerARIA(clone, t)
	return clone
}

// buildGroupTrigger returns two labels for a group option: a primary "select"
// label and an alternate "close" label (pointing at the closed sentinel) shown
// while the option is checked.
func (b *builder) buildGroupTrigger(base *ast.JSXElement, t csssignals.Trigger) ast.Expr {
	s := t.Scope
	selectLabel := shallowCloneJSX(base)
	appendClass(selectLabel, s.TriggerClass(t.Option))
	selectLabel.Opening.Attributes = append(selectLabel.Opening.Attributes, strJSXAttr("for", s.ControlID(b.cssToken, t.Option)))
	b.applyTriggerARIA(selectLabel, t)

	closeLabel := shallowCloneJSX(base)
	appendClass(closeLabel, s.TriggerClass(t.Option))
	appendClass(closeLabel, s.TriggerAltClass(t.Option))
	closeLabel.Opening.Attributes = append(closeLabel.Opening.Attributes, strJSXAttr("for", s.ControlID(b.cssToken, s.Sentinel)))
	b.applyTriggerARIA(closeLabel, t)

	return &ast.JSXFragment{Children: []ast.JSXChild{
		&ast.JSXElementChild{Element: selectLabel},
		&ast.JSXElementChild{Element: closeLabel},
	}}
}

// buildRangeStepper rewrites a `setR(r() + 1)` / `setR(r() - 1)` trigger into a
// per-index label chain: one label per index, each pointing at the adjacent
// radio, hidden except when its own index is checked. This yields a bounded,
// zero-JS stepper.
func (b *builder) buildRangeStepper(clone *ast.JSXElement, t csssignals.Trigger) ast.Expr {
	s := t.Scope
	n := len(s.Options)
	delta := 1
	if t.StackAction == "dec" {
		delta = -1
	}
	var children []ast.JSXChild
	for i := range s.Options {
		// Out-of-range ends toggle nothing (point at the current index), so the
		// stepper is bounded at min/max.
		j := i + delta
		if j < 0 || j >= n {
			j = i
		}
		lbl := shallowCloneJSX(clone)
		appendClass(lbl, s.TriggerClass(s.Options[i]))
		lbl.Opening.Attributes = append(lbl.Opening.Attributes, &ast.JSXAttr{
			Name:  "for",
			Value: &ast.Literal{Kind: ast.StringLit, Value: s.ControlID(b.cssToken, s.Options[j])},
		})
		b.applyTriggerARIA(lbl, t)
		children = append(children, &ast.JSXElementChild{Element: lbl})
	}
	return wrapWithClassOnly(children, s.Class+"-st-"+t.StackAction)
}

// buildStackPopTrigger rewrites pop() into a label pointing at the declared
// parent of the enclosing node (or the root when the node is the root/unknown).
func (b *builder) buildStackPopTrigger(clone *ast.JSXElement, t csssignals.Trigger, node string) ast.Expr {
	s := t.Scope
	target := s.Root
	if node != "" {
		if parent, ok := s.Tree[node]; ok && parent != "" {
			target = parent
		}
	}
	appendClass(clone, s.TriggerClass(target))
	clone.Opening.Attributes = append(clone.Opening.Attributes, &ast.JSXAttr{
		Name:  "for",
		Value: &ast.Literal{Kind: ast.StringLit, Value: s.ControlID(b.cssToken, target)},
	})
	return clone
}

// applyTriggerARIA adds structural ARIA attributes to a trigger label based on
// the scope's resolved role. Zero-JS presets only; stateful attributes
// (aria-selected/aria-expanded) are left to the micro-runtime.
func (b *builder) applyTriggerARIA(clone *ast.JSXElement, t csssignals.Trigger) {
	if t.Scope.IsDisabled(t.Option) {
		clone.Opening.Attributes = append(clone.Opening.Attributes, strJSXAttr("aria-disabled", "true"))
	}
	r := t.Scope.Role
	if r.Trigger == "" {
		return
	}
	// The role goes on the label, but a <label> whose `for` points at a radio
	// keeps native activation. role=tab/button replaces the label semantics.
	clone.Opening.Attributes = append(clone.Opening.Attributes, strJSXAttr("role", r.Trigger))
	if r.Haspopup != "" {
		clone.Opening.Attributes = append(clone.Opening.Attributes, strJSXAttr("aria-haspopup", r.Haspopup))
	}
	if r.SyncExpanded {
		// Initial state reflects whether this option starts selected; the
		// micro-runtime keeps it in sync afterwards.
		expanded := "false"
		if t.Option == t.Scope.Initial {
			expanded = "true"
		}
		clone.Opening.Attributes = append(clone.Opening.Attributes, strJSXAttr("aria-expanded", expanded))
	}
	_ = t
}

// buildCSSPanel rewrites a panel element: strip the showIf condition, add the
// base panel class, and wrap it in a display:contents wrapper that the generated
// CSS toggles.
func (b *builder) buildCSSPanel(el *ast.JSXElement, p csssignals.Panel) ast.Expr {
	clone := shallowCloneJSX(el)
	clone.Opening.Attributes = filterAttrs(el.Opening.Attributes, func(attr *ast.JSXAttr) bool {
		return attr.Name != "showIf" && attr.Name != "visibleIf"
	})
	// The display:contents wrapper is the toggle target, so the panel keeps its
	// own `display` (`.panel{display:flex}` is preserved when shown). The wrapper
	// must NOT carry an inline style: the stylesheet toggles `display:none` /
	// `display:contents`, and an inline style would beat the stylesheet and keep
	// every panel visible.
	wrapperClass := p.Cond.Class
	wrapper := wrapWithClassOnly([]ast.JSXChild{&ast.JSXElementChild{Element: clone}}, wrapperClass)
	// Panel-level structural ARIA (role=tabpanel/region/dialog/...). The panel
	// itself is the wrapper's only child, so the role lands on the wrapper.
	if p.Cond != nil {
		if r := p.Cond.PanelRole(); r != "" {
			wrapper.Opening.Attributes = append(wrapper.Opening.Attributes, strJSXAttr("role", r))
		}
		// Resolve stack actions (push/pop/clear) nested in this panel against
		// the panel's stack node now, so the later pipeline only sees plain
		// `<label>` triggers.
		if o := p.Cond.OwningScope(); o != nil && o.Kind == csssignals.KindStack {
			clone = b.rewriteStackTriggers(clone, p.Cond.StackNode())
			wrapper = wrapWithClassOnly([]ast.JSXChild{&ast.JSXElementChild{Element: clone}}, wrapperClass)
		}
	}
	return wrapper
}

// rewriteStackTriggers returns a copy of el with every nested stack-action
// trigger (push/pop/clear/stepper) rewritten to a `<label>` resolved against
// node (the enclosing panel's stack node).
func (b *builder) rewriteStackTriggers(el *ast.JSXElement, node string) *ast.JSXElement {
	if el == nil || el.Opening == nil {
		return el
	}
	clone := shallowCloneJSX(el)
	clone.Children = b.rewriteStackChildren(el.Children, node)
	return clone
}

func (b *builder) rewriteStackChildren(children []ast.JSXChild, node string) []ast.JSXChild {
	out := make([]ast.JSXChild, 0, len(children))
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			if c.Element != nil {
				if t, ok := b.cssSignals.MatchTrigger(c.Element); ok && t.StackAction != "" {
					if rewritten, ok := b.buildCSSTriggerAt(c.Element, t, node).(*ast.JSXElement); ok {
						out = append(out, &ast.JSXElementChild{Element: rewritten})
						continue
					}
				}
				out = append(out, &ast.JSXElementChild{Element: b.rewriteStackTriggers(c.Element, node)})
				continue
			}
			out = append(out, child)
		case *ast.JSXFragmentChild:
			if c.Fragment != nil {
				frag := *c.Fragment
				frag.Children = b.rewriteStackChildren(c.Fragment.Children, node)
				out = append(out, &ast.JSXFragmentChild{Fragment: &frag})
				continue
			}
			out = append(out, child)
		default:
			out = append(out, child)
		}
	}
	return out
}

// wrapWithScopeAnchor builds a `<div class="..." style="display:contents">`
// around children, with optional controller inputs prepended.
func wrapWithScopeAnchor(children []ast.JSXChild, class string, controls []ast.JSXChild) *ast.JSXElement {
	wrapper := &ast.JSXElement{
		Opening: &ast.JSXOpening{
			Name: "div",
			Attributes: []*ast.JSXAttr{
				strJSXAttr("class", class),
				strJSXAttr("style", "display:contents"),
			},
		},
		Closing: &ast.JSXClosing{Name: "div"},
	}
	if len(controls) > 0 {
		wrapper.Children = append(wrapper.Children, controls...)
	}
	wrapper.Children = append(wrapper.Children, children...)
	return wrapper
}

// wrapWithClassOnly builds a `<div class="...">` around children with no inline
// style, so the generated stylesheet fully controls the wrapper. Used for CSS
// signal panel wrappers, which the stylesheet toggles between `display:none`
// and `display:contents`.
func wrapWithClassOnly(children []ast.JSXChild, class string) *ast.JSXElement {
	wrapper := &ast.JSXElement{
		Opening: &ast.JSXOpening{
			Name:       "div",
			Attributes: []*ast.JSXAttr{strJSXAttr("class", class)},
		},
		Closing: &ast.JSXClosing{Name: "div"},
	}
	wrapper.Children = append(wrapper.Children, children...)
	return wrapper
}

// prepareCSSRoot returns a copy of the component's return expression with the
// scope anchor injected:
//
//   - single intrinsic element root: the scope class is merged onto it and the
//     controller inputs are prepended to its children.
//   - anything else (fragment, component root, dynamic root class): the whole
//     tree is wrapped in a `<div class="krcN" style="display:contents">` anchor.
func (b *builder) prepareCSSRoot(ret ast.Expr, scopes []*csssignals.Scope, token string) ast.Expr {
	if el, ok := ret.(*ast.JSXElement); ok && isIntrinsicTag(el) {
		clone := shallowCloneJSX(el)
		merged := true
		for _, s := range scopes {
			if !appendClass(clone, s.ScopeClass()) {
				merged = false
				break
			}
		}
		if merged {
			clone.Children = append(b.controllerChildren(scopes, token), clone.Children...)
			applyContainerARIA(clone, scopes)
			return clone
		}
	}

	inner := copyExpr(ret)
	var classNames strings.Builder
	for i, s := range scopes {
		if i > 0 {
			classNames.WriteByte(' ')
		}
		classNames.WriteString(s.ScopeClass())
	}
	wrapper := wrapWithScopeAnchor(
		[]ast.JSXChild{exprChild(inner)},
		classNames.String(),
		b.controllerChildren(scopes, token),
	)
	applyContainerARIA(wrapper, scopes)
	return wrapper
}

// applyContainerARIA stamps the first scope's structural container ARIA (role,
// orientation, label) onto the scope anchor. Stateful attributes stay with the
// ARIA synchroniser.
func applyContainerARIA(el *ast.JSXElement, scopes []*csssignals.Scope) {
	for _, s := range scopes {
		if s.Role.Container == "" {
			continue
		}
		el.Opening.Attributes = append(el.Opening.Attributes, strJSXAttr("role", s.Role.Container))
		if s.Role.Orientation != "" {
			el.Opening.Attributes = append(el.Opening.Attributes, strJSXAttr("aria-orientation", s.Role.Orientation))
		}
		if s.Label != "" {
			el.Opening.Attributes = append(el.Opening.Attributes, strJSXAttr("aria-label", s.Label))
		}
		return
	}
}

// controllerChildren builds the hidden controller inputs for a component
// instance as JSX children.
func (b *builder) controllerChildren(scopes []*csssignals.Scope, token string) []ast.JSXChild {
	var out []ast.JSXChild
	for _, s := range scopes {
		switch s.Kind {
		case csssignals.KindChoice, csssignals.KindGroup, csssignals.KindRange, csssignals.KindStack:
			for _, opt := range s.Options {
				out = append(out, controllerInput(s, token, opt, "radio", opt == s.Initial))
			}
		case csssignals.KindToggle:
			out = append(out, controllerInput(s, token, "on", "checkbox", s.Initial == "true"))
		case csssignals.KindFlags:
			for _, opt := range s.Options {
				out = append(out, controllerInput(s, token, opt, "checkbox", false))
			}
		}
	}
	return out
}

// controllerInput builds a hidden `<input>` controller for one option.
func controllerInput(s *csssignals.Scope, token, opt, inputType string, checked bool) ast.JSXChild {
	class := csssignals.HiddenClass
	if s.IsChoiceLike() {
		class += " " + s.RadioClass(opt)
	} else {
		class += " " + s.CheckboxClass(opt)
	}
	// A `name` option routes the submitted value under a friendly key; the id
	// always stays unique per instance/option for label wiring.
	name := s.ControlName(token, opt)
	if s.FormName != "" {
		name = s.FormName
	}
	attrs := []*ast.JSXAttr{
		strJSXAttr("type", inputType),
		strJSXAttr("class", class),
		strJSXAttr("name", name),
		strJSXAttr("id", s.ControlID(token, opt)),
	}
	if s.IsChoiceLike() {
		attrs = append(attrs, strJSXAttr("value", opt))
	}
	if s.IsDisabled(opt) {
		attrs = append(attrs, &ast.JSXAttr{Name: "disabled", Value: &ast.Literal{Kind: ast.BoolLit, Value: "true"}})
	}
	if checked {
		attrs = append(attrs, &ast.JSXAttr{Name: "checked", Value: &ast.Literal{Kind: ast.BoolLit, Value: "true"}})
	}
	return &ast.JSXElementChild{Element: &ast.JSXElement{
		Opening: &ast.JSXOpening{Name: "input", Attributes: attrs, SelfClosing: true},
	}}
}

// strJSXAttr builds a static string JSX attribute.
func strJSXAttr(name, value string) *ast.JSXAttr {
	return &ast.JSXAttr{Name: name, Value: &ast.Literal{Kind: ast.StringLit, Value: value}}
}

// exprChild wraps an arbitrary expression as a JSX child.
func exprChild(expr ast.Expr) ast.JSXChild {
	switch e := expr.(type) {
	case *ast.JSXElement:
		return &ast.JSXElementChild{Element: e}
	case *ast.JSXFragment:
		return &ast.JSXFragmentChild{Fragment: e}
	default:
		return &ast.JSXExprContainer{Expression: expr}
	}
}

// shallowCloneJSX returns a copy of el's opening/attributes/children containers
// (shallow element copies) so mutation never affects the shared AST.
func shallowCloneJSX(el *ast.JSXElement) *ast.JSXElement {
	opening := *el.Opening
	// Deep-copy attributes so appendClass/ARIA mutations never write through to
	// the shared component AST (library modules are parsed once and reused across
	// pages, so a shared class literal would accumulate scope classes).
	opening.Attributes = deepCloneAttrs(el.Opening.Attributes)
	clone := *el
	clone.Opening = &opening
	if el.Closing != nil {
		closing := *el.Closing
		clone.Closing = &closing
	}
	clone.Children = append([]ast.JSXChild(nil), el.Children...)
	return &clone
}

// copyExpr returns a shallow copy suitable for wrapping.
func copyExpr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.JSXElement:
		return shallowCloneJSX(e)
	case *ast.JSXFragment:
		clone := *e
		clone.Children = append([]ast.JSXChild(nil), e.Children...)
		return &clone
	}
	return expr
}

// filterAttrs returns the attributes for which keep is true (preserving order).
func filterAttrs(attrs []*ast.JSXAttr, keep func(*ast.JSXAttr) bool) []*ast.JSXAttr {
	var out []*ast.JSXAttr
	for _, a := range attrs {
		if a != nil && keep(a) {
			out = append(out, a)
		}
	}
	return out
}

// appendClass merges cls into a static class/className attribute. Returns false
// when the class attribute is dynamic.
func appendClass(el *ast.JSXElement, cls string) bool {
	for _, attr := range el.Opening.Attributes {
		if attr == nil || (attr.Name != "class" && attr.Name != "className") {
			continue
		}
		if attr.Value == nil {
			attr.Value = &ast.Literal{Kind: ast.StringLit, Value: cls}
			return true
		}
		lit, ok := attr.Value.(*ast.Literal)
		if !ok || lit.Kind != ast.StringLit {
			return false
		}
		if strings.TrimSpace(lit.Value) == "" {
			lit.Value = cls
		} else if !strings.Contains(lit.Value, cls) {
			lit.Value = strings.TrimSpace(lit.Value) + " " + cls
		}
		return true
	}
	el.Opening.Attributes = append(el.Opening.Attributes, &ast.JSXAttr{
		Name:  "class",
		Value: &ast.Literal{Kind: ast.StringLit, Value: cls},
	})
	return true
}
