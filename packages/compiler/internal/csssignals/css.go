package csssignals

import (
	"sort"
	"strconv"
	"strings"
)

// activeTriggerDecl is the declaration block applied to a selected trigger.
// Themes override the colours via --krate-css-active*; components read the
// inherited --krate-active (0/1) and --krate-active-color hooks to render their
// own selected visuals (radio dots, tab underlines, toggle fills) with zero JS.
//
// --krate-active-bg is emitted twice: a static fallback first, then the
// color-mix() value. Browsers without color-mix() (e.g. Chrome < 111) keep the
// fallback; modern browsers take the color-mix() value.
const activeTriggerDecl = "color:var(--krate-css-active-fg,inherit);font-weight:600;--krate-active:1;--krate-active-color:var(--krate-css-active,var(--krate-primary,#f97316));--krate-active-bg:#f9731626;--krate-active-bg:color-mix(in srgb,var(--krate-css-active,var(--krate-primary,#f97316)) 15%,transparent)"

// HiddenCSS is the base rule for the visually-hidden controller inputs. They
// stay focusable (unlike display:none) so keyboard navigation and screen readers
// keep working.
const HiddenCSS = "." + HiddenClass + "{position:absolute;width:1px;height:1px;margin:-1px;padding:0;border:0;clip:rect(0 0 0 0);overflow:hidden;white-space:nowrap}"

// focusMarkerCSS is the legacy focus ring: the scoped shim marks the trigger
// label of the focused controller input with the `krc-f` class (legacy mode has
// no :has() to express "trigger of the focused input").
const focusMarkerCSS = ".krc-f{outline:2px solid var(--krate-focus-ring,#f97316);outline-offset:2px}"

// Stylesheet returns the generated stylesheet in the default (modern) mode.
func Stylesheet(scopes []*Scope, conditions []*Condition) string {
	return StylesheetMode(scopes, conditions, StyleModern)
}

// StylesheetMode returns the generated stylesheet for scopes and their matched
// conditions, in a deterministic order so the page's content hash is stable.
// Returns "" when empty. Conditions whose wrapper is hidden/shown by the
// per-scope rules (single positive choice atom, any single toggle/flag atom)
// are canonical and are not double-emitted.
//
// modern emits :has()-based selectors plus a reveal-all fallback. legacy emits
// class-based selectors (driven by the scoped shim) instead. hybrid emits both,
// each wrapped in @supports selector(:has(*)), so a browser picks its path with
// no server involvement.
func StylesheetMode(scopes []*Scope, conditions []*Condition, mode StyleMode) string {
	if len(scopes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(HiddenCSS)
	b.WriteByte('\n')
	switch mode {
	case StyleLegacy:
		b.WriteString(stateCSS(scopes, conditions, StyleLegacy))
		b.WriteString(tailCSS(scopes))
	case StyleHybrid:
		b.WriteString(CompatModernPrefix)
		b.WriteString(stateCSS(scopes, conditions, StyleModern))
		b.WriteString("}\n")
		b.WriteString(CompatLegacyPrefix)
		b.WriteString(stateCSS(scopes, conditions, StyleLegacy))
		b.WriteString("}\n")
		b.WriteString(tailCSS(scopes))
	default:
		b.WriteString(stateCSS(scopes, conditions, StyleModern))
		b.WriteString(tailCSS(scopes))
		b.WriteString(NoHasFallback(scopes, conditions))
	}
	return b.String()
}

// stateCSS emits the state-dependent rules (per-scope rules + non-canonical
// condition rules) for one selector mode.
func stateCSS(scopes []*Scope, conditions []*Condition, mode StyleMode) string {
	var b strings.Builder
	for _, s := range scopes {
		b.WriteString(s.css(mode))
		b.WriteString(s.focusCSS(mode))
		b.WriteString(s.anchorCSS(mode))
		b.WriteString(s.transitionCSS())
	}
	if mode == StyleLegacy {
		b.WriteString(focusMarkerCSS)
		b.WriteByte('\n')
	}
	for _, c := range sortedConditions(conditions) {
		if c.needsExplicitRule() {
			b.WriteString(c.css(mode))
		}
	}
	return b.String()
}

// tailCSS emits the mode-independent rules appended after the state rules.
func tailCSS(scopes []*Scope) string {
	var b strings.Builder
	if UsesLiveText(scopes) {
		b.WriteString(LiveTextCSS)
		b.WriteByte('\n')
	}
	b.WriteString(disabledCSS(scopes))
	return b.String()
}

// stateFrag returns the state selector fragment appended after the scope root
// class, matching when the controller `ctrl` is checked (or not) in the given
// mode. Modern uses :has()/:not(:has()); legacy relies on the scoped shim
// mirroring the input's checked state onto a root class (`<ctrl>-on`).
func (s *Scope) stateFrag(mode StyleMode, ctrl string, negated bool) string {
	if mode == StyleLegacy {
		if negated {
			return ":not(." + ctrl + "-on)"
		}
		return "." + ctrl + "-on"
	}
	if negated {
		return ":not(:has(." + ctrl + ":checked))"
	}
	return ":has(." + ctrl + ":checked)"
}

// triggerSel matches the trigger for an option whether it is a descendant of the
// scope anchor or the anchor element itself (a single-element toggle, where the
// scope class and trigger class share one element).
func (s *Scope) triggerSel(mode StyleMode, ctrl, option string) string {
	t := s.TriggerClass(option)
	if mode == StyleLegacy {
		return "." + s.Class + "." + ctrl + "-on ." + t + ",." + s.Class + "." + t + "." + ctrl + "-on"
	}
	return "." + s.Class + ":has(." + ctrl + ":checked) ." + t + ",." + s.Class + "." + t + ":has(." + ctrl + ":checked)"
}

// focusCSS shows a focus ring on a trigger's visible label when its (hidden)
// controller input has keyboard focus, since the label itself is not focused.
// Legacy mode has no :has(): the scoped shim marks the label with `krc-f`
// (see focusMarkerCSS) instead.
func (s *Scope) focusCSS(mode StyleMode) string {
	if mode == StyleLegacy {
		return ""
	}
	var b strings.Builder
	for _, opt := range s.Options {
		ctrl := s.CheckboxClass(opt)
		if s.isChoiceLike() {
			ctrl = s.RadioClass(opt)
		}
		t := s.TriggerClass(opt)
		b.WriteString("." + s.Class + ":has(." + ctrl + ":focus-visible) ." + t + ",." + s.Class + "." + t + ":has(." + ctrl + ":focus-visible)")
		b.WriteString("{outline:2px solid var(--krate-focus-ring,#f97316);outline-offset:2px}\n")
	}
	return b.String()
}

// anchorCSS positions each option's panel relative to its trigger using CSS
// anchor positioning (progressive: ignored by browsers without support, which
// fall back to the component's own positioning). Legacy browsers lack anchor
// positioning entirely, so the component's own positioning applies.
func (s *Scope) anchorCSS(mode StyleMode) string {
	if s.Anchor == "" || mode == StyleLegacy {
		return ""
	}
	decl := anchorDecl(s.Anchor)
	if decl == "" {
		return ""
	}
	var b strings.Builder
	alt := s.TriggerAltClass("")
	for _, opt := range s.Options {
		if opt == s.Sentinel {
			continue
		}
		anchor := "--" + s.Class + "-" + sanitizeToken(opt)
		b.WriteString("." + s.Class + " ." + s.TriggerClass(opt) + ":not(." + alt + "){anchor-name:" + anchor + "}\n")
		b.WriteString("." + s.Class + " ." + s.PanelWrapperClass(opt, false) + ">*{position-anchor:" + anchor + ";" + decl + "}\n")
	}
	return b.String()
}

// anchorDecl returns the anchor()-based positioning for a side.
func anchorDecl(side string) string {
	switch side {
	case "bottom":
		return "top:anchor(bottom);justify-self:anchor-center"
	case "bottom-start":
		return "top:anchor(bottom);left:anchor(left)"
	case "bottom-end":
		return "top:anchor(bottom);right:anchor(right)"
	case "top":
		return "bottom:anchor(top);justify-self:anchor-center"
	case "top-start":
		return "bottom:anchor(top);left:anchor(left)"
	case "top-end":
		return "bottom:anchor(top);right:anchor(right)"
	case "right":
		return "left:anchor(right);align-self:anchor-center"
	case "left":
		return "right:anchor(left);align-self:anchor-center"
	}
	return ""
}

// transitionCSS fades/slides a panel when it becomes rendered (open). Close is
// not animated (the wrapper is display:none); this is a progressive enhancement.
func (s *Scope) transitionCSS() string {
	if !s.Transition {
		return ""
	}
	var b strings.Builder
	for _, opt := range s.Options {
		if opt == s.Sentinel {
			continue
		}
		child := "." + s.Class + " ." + s.PanelWrapperClass(opt, false) + ">*"
		b.WriteString(child + "{transition:opacity .18s ease,translate .18s ease}\n")
		b.WriteString("@starting-style{" + child + "{opacity:0;translate:0 -.25rem}}\n")
	}
	return b.String()
}

// disabledCSS dims triggers whose option's controller input is disabled.
func disabledCSS(scopes []*Scope) string {
	var parts []string
	for _, s := range scopes {
		for _, opt := range s.Disabled {
			parts = append(parts, "."+s.Class+" ."+s.TriggerClass(opt))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ",") + "{pointer-events:none;opacity:.5}\n"
}

// NoHasFallback reveals every panel wrapper when the browser does not support
// `:has()`, so content stays visible (the interactive reveal degrades to
// "everything shown") instead of every panel being display:none. It is a no-op
// on browsers that support `:has()` (2023+). Used for the hybrid <noscript>
// fallback, where a modern browser with scripting off must keep native `:has()`.
func NoHasFallback(scopes []*Scope, conditions []*Condition) string {
	sel := revealSelectorSet(scopes, conditions)
	if sel == "" {
		return ""
	}
	return "@supports not selector(:has(*)){" + sel + "{display:contents}}\n"
}

// RevealCSS is the unconditional reveal-all rule. Legacy mode never uses
// `:has()` (it drives state with the shim), so its <noscript> fallback must not
// be gated on :has() - otherwise a modern browser with scripting off would keep
// every panel hidden.
func RevealCSS(scopes []*Scope, conditions []*Condition) string {
	sel := revealSelectorSet(scopes, conditions)
	if sel == "" {
		return ""
	}
	return sel + "{display:contents}\n"
}

// revealSelectorSet returns the sorted, comma-joined selectors of every panel
// wrapper and condition wrapper (empty when none).
func revealSelectorSet(scopes []*Scope, conditions []*Condition) string {
	set := make(map[string]bool)
	for _, s := range scopes {
		for _, opt := range s.Options {
			set["."+s.Class+" ."+s.PanelWrapperClass(opt, false)] = true
			set["."+s.Class+" ."+s.PanelWrapperClass(opt, true)] = true
		}
	}
	for _, c := range conditions {
		if o := c.owningScope(); o != nil {
			set["."+o.Class+" ."+c.Class] = true
		}
	}
	if len(set) == 0 {
		return ""
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// sortedConditions returns a deterministic copy ordered by owning scope index
// then expression index.
func sortedConditions(conditions []*Condition) []*Condition {
	out := append([]*Condition(nil), conditions...)
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := out[i].owningScope().Index, out[j].owningScope().Index; a != b {
			return a < b
		}
		return out[i].ExprIdx < out[j].ExprIdx
	})
	return out
}

// needsExplicitRule reports whether the condition needs its own hide+show rule
// beyond the per-scope choice/toggle/flags rules. A positive choice atom, a
// toggle, and a flag are canonical (covered by their scope's css); a negated
// choice atom and every compound condition are not.
func (c *Condition) needsExplicitRule() bool {
	if !c.simple() {
		return true
	}
	at := c.Terms[0][0]
	return at.Scope.isChoiceLike() && at.Negated
}

// css emits the hide + show rules for a non-canonical condition. The wrapper is
// hidden by default, then shown when any DNF term matches: one selector-list
// entry per AND-term.
func (c *Condition) css(mode StyleMode) string {
	owner := c.owningScope()
	var b strings.Builder
	b.WriteString("." + owner.Class + " ." + c.Class + "{display:none}\n")
	for i, term := range c.Terms {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("." + owner.Class)
		for _, at := range term {
			b.WriteString(at.selector(mode))
		}
		b.WriteString(" ." + c.Class)
	}
	b.WriteString("{display:contents}\n")
	return b.String()
}

// selector is the state fragment for an atom, relative to the scope anchor
// element: :has()/:not(:has()) in modern mode, or the shim-driven class in
// legacy mode.
func (at *Atom) selector(mode StyleMode) string {
	ctrl := at.controllerSel()
	if mode == StyleLegacy {
		if at.Negated {
			return ":not(" + ctrl + "-on)"
		}
		return ctrl + "-on"
	}
	if at.Negated {
		return ":not(:has(" + ctrl + ":checked))"
	}
	return ":has(" + ctrl + ":checked)"
}

// controllerSel is the class selector of the atom's controller input.
func (at *Atom) controllerSel() string {
	if at.Scope.isChoiceLike() {
		return "." + at.Scope.RadioClass(at.Option)
	}
	return "." + at.Scope.CheckboxClass(at.Option)
}

// css emits the rules for a single scope.
func (s *Scope) css(mode StyleMode) string {
	switch s.Kind {
	case KindChoice, KindGroup:
		return s.choiceCSS(mode)
	case KindRange:
		return s.rangeCSS(mode)
	case KindStack:
		return s.stackCSS(mode)
	case KindToggle:
		return s.toggleCSS(mode)
	case KindFlags:
		return s.flagsCSS(mode)
	}
	return ""
}

// rangeCSS emits the choice rules plus a proportional fill rule for a progress
// indicator: the fill width is the checked index's percentage of the range.
// Each stepper group shows only the label for the currently-checked index.
func (s *Scope) rangeCSS(mode StyleMode) string {
	css := s.choiceCSS(mode)
	n := len(s.Options)
	if n < 2 {
		return css
	}
	var b strings.Builder
	b.WriteString(css)
	for i, opt := range s.Options {
		pct := round1(float64(i) * 100 / float64(n-1))
		b.WriteString("." + s.Class + s.stateFrag(mode, s.RadioClass(opt), false) + " ." + ClassPrefix + "-fill{width:" + pct + "%}\n")
	}
	// Steppers: one label per index; only the label whose index is checked is
	// shown (nth-child matches the option order).
	for _, dir := range []string{"inc", "dec"} {
		b.WriteString("." + s.Class + " ." + s.Class + "-st-" + dir + ">*{display:none}\n")
		for i, opt := range s.Options {
			b.WriteString("." + s.Class + s.stateFrag(mode, s.RadioClass(opt), false) + " ." + s.Class + "-st-" + dir + ">*:nth-child(" + strconv.Itoa(i+1) + "){display:inline-block}\n")
		}
	}
	return b.String()
}

// stackCSS emits the choice rules; each level's panel visibility is expressed
// by the ordinary panel conditions, so the base rules cover it.
func (s *Scope) stackCSS(mode StyleMode) string { return s.choiceCSS(mode) }

// round1 formats a float with one decimal place, trimming a trailing ".0".
func round1(f float64) string {
	v := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.TrimSuffix(v, ".0")
}

// PanelWrapperClass is the class on the display:contents wrapper around a panel.
// The wrapper is what gets toggled, so the panel's own author `display` is
// preserved. `negated` selects the "state is off" wrapper.
func (s *Scope) PanelWrapperClass(option string, negated bool) string {
	if negated {
		return s.Class + "-n-" + sanitizeToken(option)
	}
	return s.Class + "-p-" + sanitizeToken(option)
}

// choiceCSS emits the rules for a radio group.
func (s *Scope) choiceCSS(mode StyleMode) string {
	var b strings.Builder

	// Every option's panel wrapper is hidden by default; the selected option's
	// wrapper becomes layout-transparent. The panel itself keeps its display.
	for i, opt := range s.Options {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("." + s.Class + " ." + s.PanelWrapperClass(opt, false))
	}
	b.WriteString("{display:none}\n")

	for i, opt := range s.Options {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("." + s.Class + s.stateFrag(mode, s.RadioClass(opt), false) + " ." + s.PanelWrapperClass(opt, false))
	}
	b.WriteString("{display:contents}\n")

	for _, opt := range s.Options {
		b.WriteString(s.triggerSel(mode, s.RadioClass(opt), opt))
		b.WriteString("{" + activeTriggerDecl + "}\n")
	}
	// Group scopes emit a second "close" label per option, shown only while the
	// option is selected, so a repeat click closes the group.
	if s.Kind == KindGroup {
		for _, opt := range s.Options {
			if opt == s.Sentinel {
				continue
			}
			b.WriteString("." + s.Class + " ." + s.TriggerAltClass(opt) + "{display:none}\n")
		}
		for _, opt := range s.Options {
			if opt == s.Sentinel {
				continue
			}
			b.WriteString("." + s.Class + s.stateFrag(mode, s.RadioClass(opt), false) + " ." + s.TriggerAltClass(opt) + "{display:inline-flex;" + activeTriggerDecl + "}\n")
			b.WriteString("." + s.Class + s.stateFrag(mode, s.RadioClass(opt), false) + " ." + s.TriggerClass(opt) + ":not(." + s.TriggerAltClass(opt) + "){display:none}\n")
		}
	}
	b.WriteString(s.varsCSS(mode))
	return b.String()
}

// varsCSS emits the per-option custom-property rules that make the selected
// state readable by other CSS, with zero JS. Every choice-like scope publishes
// the stable, author-facing `--krate-current` (the quoted option token); an
// author's `vars` add more properties. Values are emitted verbatim (authors
// quote strings for `content:` themselves).
func (s *Scope) varsCSS(mode StyleMode) string {
	if !s.isChoiceLike() {
		return ""
	}
	var b strings.Builder
	for _, opt := range s.Options {
		var decl strings.Builder
		decl.WriteString("--krate-current:\"" + opt + "\";")
		// Deterministic order for stable output hashes.
		keys := make([]string, 0, len(s.Vars))
		for k := range s.Vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if v, ok := s.Vars[k][opt]; ok {
				decl.WriteString(k + ":" + v + ";")
			}
		}
		b.WriteString("." + s.Class + s.stateFrag(mode, s.RadioClass(opt), false) + "{" + decl.String() + "}\n")
	}
	return b.String()
}

// toggleCSS emits the rules for a single checkbox. Two panel wrappers may be
// used: the "on" wrapper (shown when checked) and the "off" wrapper (shown when
// not) - either or both may be absent in the markup.
func (s *Scope) toggleCSS(mode StyleMode) string {
	var b strings.Builder
	cb := s.CheckboxClass("")
	on, off := s.PanelWrapperClass("on", false), s.PanelWrapperClass("on", true)

	b.WriteString("." + s.Class + " ." + on + ",." + s.Class + " ." + off + "{display:none}\n")
	b.WriteString("." + s.Class + s.stateFrag(mode, cb, false) + " ." + on)
	b.WriteString(",." + s.Class + s.stateFrag(mode, cb, true) + " ." + off)
	b.WriteString("{display:contents}\n")
	b.WriteString(s.triggerSel(mode, cb, ""))
	b.WriteString("{" + activeTriggerDecl + "}\n")
	b.WriteString(s.toggleVarsCSS(mode, cb))
	return b.String()
}

// toggleVarsCSS emits the two-state custom properties for a toggle's on/off.
func (s *Scope) toggleVarsCSS(mode StyleMode, cb string) string {
	var b strings.Builder
	onDecl := "--krate-current:\"on\";"
	offDecl := "--krate-current:\"off\";"
	for _, k := range sortedVarKeys(s.Vars) {
		if v, ok := s.Vars[k]["on"]; ok {
			onDecl += k + ":" + v + ";"
		}
		if v, ok := s.Vars[k]["off"]; ok {
			offDecl += k + ":" + v + ";"
		}
	}
	b.WriteString("." + s.Class + s.stateFrag(mode, cb, false) + "{" + onDecl + "}\n")
	b.WriteString("." + s.Class + s.stateFrag(mode, cb, true) + "{" + offDecl + "}\n")
	return b.String()
}

// sortedVarKeys returns a map's keys in deterministic order.
func sortedVarKeys(m map[string]map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// LiveTextCSS is the shared rule for a live value element: it renders the
// inherited `--krate-current` custom property as text. Appended once per page
// when any scope uses a live text read.
const LiveTextCSS = ".krc-live::after{content:var(--krate-current,\"\")}"

// UsesLiveText reports whether any scope renders a bare getter as live text.
func UsesLiveText(scopes []*Scope) bool {
	for _, s := range scopes {
		if s.LiveText {
			return true
		}
	}
	return false
}

// flagsCSS emits the rules for independent checkboxes.
func (s *Scope) flagsCSS(mode StyleMode) string {
	var b strings.Builder
	for _, opt := range s.Options {
		on, off := s.PanelWrapperClass(opt, false), s.PanelWrapperClass(opt, true)
		cb := s.CheckboxClass(opt)
		b.WriteString("." + s.Class + " ." + on + ",." + s.Class + " ." + off + "{display:none}\n")
		b.WriteString("." + s.Class + s.stateFrag(mode, cb, false) + " ." + on)
		b.WriteString(",." + s.Class + s.stateFrag(mode, cb, true) + " ." + off)
		b.WriteString("{display:contents}\n")
		b.WriteString(s.triggerSel(mode, cb, opt))
		b.WriteString("{" + activeTriggerDecl + "}\n")
	}
	return b.String()
}
