package csssignals

import (
	"sort"
	"strconv"
	"strings"
)

// activeTriggerDecl is the declaration block applied to a selected trigger.
// Themes override it via the --krate-css-active* custom properties.
const activeTriggerDecl = "color:var(--krate-css-active-fg,inherit);border-bottom:2px solid var(--krate-css-active,#3b82f6);font-weight:600"

// HiddenCSS is the base rule for the visually-hidden controller inputs. They
// stay focusable (unlike display:none) so keyboard navigation and screen readers
// keep working.
const HiddenCSS = "." + HiddenClass + "{position:absolute;width:1px;height:1px;margin:-1px;padding:0;border:0;clip:rect(0 0 0 0);overflow:hidden;white-space:nowrap}"

// Stylesheet returns the generated stylesheet for scopes and their matched
// conditions, in a deterministic order so the page's content hash is stable.
// Returns "" when empty. Conditions whose wrapper is hidden/shown by the
// per-scope rules (single positive choice atom, any single toggle/flag atom)
// are canonical and are not double-emitted.
func Stylesheet(scopes []*Scope, conditions []*Condition) string {
	if len(scopes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(HiddenCSS)
	b.WriteByte('\n')
	for _, s := range scopes {
		b.WriteString(s.css())
	}
	for _, c := range sortedConditions(conditions) {
		if c.needsExplicitRule() {
			b.WriteString(c.css())
		}
	}
	if UsesLiveText(scopes) {
		b.WriteString(LiveTextCSS)
		b.WriteByte('\n')
	}
	return b.String()
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
func (c *Condition) css() string {
	owner := c.owningScope()
	var b strings.Builder
	b.WriteString("." + owner.Class + " ." + c.Class + "{display:none}\n")
	for i, term := range c.Terms {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("." + owner.Class)
		for _, at := range term {
			b.WriteString(at.selector())
		}
		b.WriteString(" ." + c.Class)
	}
	b.WriteString("{display:contents}\n")
	return b.String()
}

// selector is the :has()/:not(:has()) fragment for an atom, relative to the
// scope anchor element.
func (at *Atom) selector() string {
	if at.Negated {
		return ":not(:has(" + at.controllerSel() + ":checked))"
	}
	return ":has(" + at.controllerSel() + ":checked)"
}

// controllerSel is the class selector of the atom's controller input.
func (at *Atom) controllerSel() string {
	if at.Scope.isChoiceLike() {
		return "." + at.Scope.RadioClass(at.Option)
	}
	return "." + at.Scope.CheckboxClass(at.Option)
}

// css emits the rules for a single scope.
func (s *Scope) css() string {
	switch s.Kind {
	case KindChoice, KindGroup:
		return s.choiceCSS()
	case KindRange:
		return s.rangeCSS()
	case KindStack:
		return s.stackCSS()
	case KindToggle:
		return s.toggleCSS()
	case KindFlags:
		return s.flagsCSS()
	}
	return ""
}

// rangeCSS emits the choice rules plus a proportional fill rule for a progress
// indicator: the fill width is the checked index's percentage of the range.
// Each stepper group shows only the label for the currently-checked index.
func (s *Scope) rangeCSS() string {
	css := s.choiceCSS()
	n := len(s.Options)
	if n < 2 {
		return css
	}
	var b strings.Builder
	b.WriteString(css)
	for i, opt := range s.Options {
		pct := round1(float64(i) * 100 / float64(n-1))
		b.WriteString("." + s.Class + ":has(." + s.RadioClass(opt) + ":checked) ." + ClassPrefix + "-fill{width:" + pct + "%}\n")
	}
	// Steppers: one label per index; only the label whose index is checked is
	// shown (nth-child matches the option order).
	for _, dir := range []string{"inc", "dec"} {
		b.WriteString("." + s.Class + " ." + s.Class + "-st-" + dir + ">*{display:none}\n")
		for i, opt := range s.Options {
			b.WriteString("." + s.Class + ":has(." + s.RadioClass(opt) + ":checked) ." + s.Class + "-st-" + dir + ">*:nth-child(" + strconv.Itoa(i+1) + "){display:inline-block}\n")
		}
	}
	return b.String()
}

// stackCSS emits the choice rules; each level's panel visibility is expressed
// by the ordinary panel conditions, so the base rules cover it.
func (s *Scope) stackCSS() string { return s.choiceCSS() }

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
func (s *Scope) choiceCSS() string {
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
		b.WriteString("." + s.Class + ":has(." + s.RadioClass(opt) + ":checked) ." + s.PanelWrapperClass(opt, false))
	}
	b.WriteString("{display:contents}\n")

	for _, opt := range s.Options {
		b.WriteString("." + s.Class + ":has(." + s.RadioClass(opt) + ":checked) ." + s.TriggerClass(opt))
		b.WriteString("{" + activeTriggerDecl + "}\n")
	}
	b.WriteString(s.varsCSS())
	return b.String()
}

// varsCSS emits the per-option custom-property rules that make the selected
// state readable by other CSS, with zero JS. Every choice-like scope publishes
// the stable, author-facing `--krate-current` (the quoted option token); an
// author's `vars` add more properties. Values are emitted verbatim (authors
// quote strings for `content:` themselves).
func (s *Scope) varsCSS() string {
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
		b.WriteString("." + s.Class + ":has(." + s.RadioClass(opt) + ":checked){" + decl.String() + "}\n")
	}
	return b.String()
}

// toggleCSS emits the rules for a single checkbox. Two panel wrappers may be
// used: the "on" wrapper (shown when checked) and the "off" wrapper (shown when
// not) - either or both may be absent in the markup.
func (s *Scope) toggleCSS() string {
	var b strings.Builder
	cb := s.CheckboxClass("")
	on, off := s.PanelWrapperClass("on", false), s.PanelWrapperClass("on", true)

	b.WriteString("." + s.Class + " ." + on + ",." + s.Class + " ." + off + "{display:none}\n")
	b.WriteString("." + s.Class + ":has(." + cb + ":checked) ." + on)
	b.WriteString(",." + s.Class + ":not(:has(." + cb + ":checked)) ." + off)
	b.WriteString("{display:contents}\n")
	b.WriteString("." + s.Class + ":has(." + cb + ":checked) ." + s.TriggerClass(""))
	b.WriteString("{" + activeTriggerDecl + "}\n")
	b.WriteString(s.toggleVarsCSS(cb))
	return b.String()
}

// toggleVarsCSS emits the two-state custom properties for a toggle's on/off.
func (s *Scope) toggleVarsCSS(cb string) string {
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
	b.WriteString("." + s.Class + ":has(." + cb + ":checked){" + onDecl + "}\n")
	b.WriteString("." + s.Class + ":not(:has(." + cb + ":checked)){" + offDecl + "}\n")
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
func (s *Scope) flagsCSS() string {
	var b strings.Builder
	for _, opt := range s.Options {
		on, off := s.PanelWrapperClass(opt, false), s.PanelWrapperClass(opt, true)
		cb := s.CheckboxClass(opt)
		b.WriteString("." + s.Class + " ." + on + ",." + s.Class + " ." + off + "{display:none}\n")
		b.WriteString("." + s.Class + ":has(." + cb + ":checked) ." + on)
		b.WriteString(",." + s.Class + ":not(:has(." + cb + ":checked)) ." + off)
		b.WriteString("{display:contents}\n")
		b.WriteString("." + s.Class + ":has(." + cb + ":checked) ." + s.TriggerClass(opt))
		b.WriteString("{" + activeTriggerDecl + "}\n")
	}
	return b.String()
}
