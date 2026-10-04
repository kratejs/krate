// Package csssignals implements the compiler-side analysis for the zero-JS CSS
// primitives:
//
//   - createCSSChoice(initial, options?) — a radio group (tabs, segments).
//   - createCSSToggle(initial)           — a single checkbox (on/off).
//   - createCSSFlags([...])              — independent checkboxes.
//
// They compile to hidden `<input>` controllers, `<label>` triggers, and
// `:has()` CSS — no client JavaScript. This package owns the analysis (which
// declarations are transformable, the option universe, matching
// triggers/panels) and the generated stylesheet. It never mutates the AST.
//
// Unlike a fallback design, an un-transformable declaration is a hard error: a
// declaration that cannot be expressed in CSS must be replaced with
// `createSignal` by the author, because silently hydrating it would ship
// behaviour the author did not ask for.
package csssignals

import (
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/sigutil"
)

// Kind identifies the CSS signal primitive.
type Kind int

const (
	// KindChoice is a radio group (`createCSSChoice`).
	KindChoice Kind = iota
	// KindToggle is a single checkbox (`createCSSToggle`).
	KindToggle
	// KindFlags is a set of independent checkboxes (`createCSSFlags`).
	KindFlags
	// KindGroup is an optional radio group (`createCSSGroup`): a choice with an
	// explicit closed (null) sentinel option.
	KindGroup
	// KindRange is a discrete radio chain over integer values (`createCSSRange`).
	KindRange
	// KindStack is a declared navigation tree (`createCSSStack`): a radio group
	// whose selected segment is the stack top, with push/pop/clear triggers.
	KindStack
)

// isChoiceLike reports whether a kind compiles to a radio group (one checked
// radio per scope) rather than independent checkboxes.
func (k Kind) isChoiceLike() bool {
	switch k {
	case KindChoice, KindGroup, KindRange, KindStack:
		return true
	}
	return false
}

// ClassPrefix is the shared prefix for every generated class/identifier. Kept
// short deliberately: these tokens ship to the browser on every page.
const ClassPrefix = "krc"

// HiddenClass marks the visually-hidden controller inputs.
const HiddenClass = ClassPrefix + "-h"

// Scope is one CSS signal declaration within a component. It is shared by every
// instance of that component: all selectors are class-based and
// instance-agnostic. Per-instance input `name`/`id` uniqueness is handled by the
// builder via an instance token.
type Scope struct {
	Kind Kind
	// Var is the getter identifier: the choice/toggle getter, or the flags
	// object for KindFlags.
	Var string
	// Setter is the setter identifier.
	Setter string
	// Initial is the initial selection for choice/toggle ("" for flags).
	Initial string
	// Options is the ordered option universe: choice values, ["on"] for a
	// toggle, or flag names for flags.
	Options []string
	// Index is the page-unique scope index; the root class is ClassPrefix+index.
	Index int
	// Class is the scope's root class, shared by every instance.
	Class string
	// Role is the resolved ARIA surface for this scope (zero value = none).
	Role Role
	// Label is an optional accessible label for the scope container.
	Label string
	// Sentinel is the explicit "closed" option for KindGroup ("" when none).
	// It is stored as a normal option but rendered without an associated panel.
	Sentinel string
	// NumMin/NumMax/NumStep hold the literal numeric bounds for KindRange.
	NumMin, NumMax, NumStep string
	// Levels is the number of nested levels for KindStack (1 for flat scopes).
	Levels int
	// Tree maps each stack node to its parent node (root maps to ""), inferred
	// from where each `push('x')` appears. Root is the initial node.
	Tree map[string]string
	// Root is the stack's initial/root node.
	Root string
	// Vars maps a CSS custom property to per-option values, emitted on the
	// active-option anchor rule so state drives live text/themes with zero JS.
	Vars map[string]map[string]string
	// LiveText is true when a bare `{getter()}` text read of this scope was
	// found, so the stylesheet emits the `.krc-live` content rule.
	LiveText bool
}

// isChoiceLike reports whether the scope compiles to a radio group.
func (s *Scope) isChoiceLike() bool { return s.Kind.isChoiceLike() }

// IsChoiceLike reports whether the scope compiles to a radio group (one checked
// radio per scope) rather than independent checkboxes. Exported for the builder.
func (s *Scope) IsChoiceLike() bool { return s.isChoiceLike() }

// base returns the scope's root class (e.g. "krc0").
func (s *Scope) base() string { return ClassPrefix + encodeIndex(s.Index) }

// RadioClass returns the class marking the controller input for a choice
// option.
func (s *Scope) RadioClass(option string) string {
	return s.Class + "-r-" + sanitizeToken(option)
}

// CheckboxClass returns the class marking the checkbox controller for a toggle
// (no option) or a flag option.
func (s *Scope) CheckboxClass(option string) string {
	if s.Kind == KindToggle {
		return s.Class + "-c"
	}
	return s.Class + "-c-" + sanitizeToken(option)
}

// TriggerClass returns the class applied to a trigger label for an option
// (choice/flags) or the sole toggle trigger.
func (s *Scope) TriggerClass(option string) string {
	if s.Kind == KindToggle {
		return s.Class + "-t"
	}
	return s.Class + "-t-" + sanitizeToken(option)
}

// ScopeClass is the class the builder adds to the scope anchor.
func (s *Scope) ScopeClass() string { return s.Class }

// ControlName returns the controller input's `name` for one instance. Choice-
// like scopes (choice/group/range/stack) use a shared name (radio group);
// toggle/flags are independent checkboxes and so get a per-option name.
func (s *Scope) ControlName(token, option string) string {
	if s.isChoiceLike() {
		return token + "-r" + encodeIndex(s.Index)
	}
	return token + "-c" + encodeIndex(s.Index) + "-" + sanitizeToken(option)
}

// ControlID returns the controller input's DOM id for one instance/option. Only
// a radio group needs the option appended (its name is shared); toggle/flag
// names are already unique.
func (s *Scope) ControlID(token, option string) string {
	if s.isChoiceLike() {
		return s.ControlName(token, option) + "-" + sanitizeToken(option)
	}
	return s.ControlName(token, option)
}

// Trigger is a matched setter call.
type Trigger struct {
	Scope  *Scope
	Option string // choice/flags option; "on" for toggle
	// StackAction is set for stack/range stepper triggers: "push", "pop",
	// "clear", "inc", or "dec". Empty for an ordinary setter trigger.
	StackAction string
}

// Atom is one state predicate: a choice `get()==='x'` (or `!==`), a toggle
// `on()` (or `!on()`), or a flag `flags.x()` (or `!flags.x()`).
type Atom struct {
	Scope   *Scope
	Option  string
	Negated bool // literal polarity: `get()!=='x'`, `!on()`, `!flags.x()`
}

// Condition is a panel's boolean test normalized to DNF: an OR of AND-terms,
// each term a set of literals (atoms or their negation). Two conditions with
// the same canonical DNF are interchangeable and dedupe to one wrapper class.
type Condition struct {
	Scopes  []*Scope  // distinct scopes referenced, first-appearance order
	Terms   [][]*Atom // DNF: OR of AND-terms; each term is a sorted atom set
	Class   string    // wrapper class: krcN-p-<x>/krcN-n-<x> (simple) or krcN-x-<i> (compound)
	ExprIdx int       // component-local index for compound wrapper classes
}

// Panel is a matched showIf/visibleIf condition.
type Panel struct {
	Cond *Condition
}

// Analyzer holds the CSS signal scopes for a single component plus the
// validation result. A component with any CSS signal that cannot be compiled
// yields errors and must fail the build.
type Analyzer struct {
	scopes   []*Scope
	bySetter map[string]*Scope
	byVar    map[string]*Scope
	// byStackMethod maps a stack action name (push/pop/clear) to its scope.
	byStackMethod map[string]*Scope
	// stackMethods records the action names declared per stack scope, in order.
	stackMethods []stackBinding
	haveAny      bool
	errs         []string

	// conditions holds every deduped matched panel condition (registration
	// order), condByKey maps a canonical DNF string to its condition, and
	// exprIdx is the component-local counter for compound wrapper classes.
	conditions []*Condition
	condByKey  map[string]*Condition
	exprIdx    int
}

// stackBinding associates a stack scope with the destructured action names.
type stackBinding struct {
	scope   *Scope
	methods []string
}

// closedOption is the sentinel option a KindGroup uses for "nothing selected".
const closedOption = ""

// isNullLiteral reports whether an expression is the `null` literal.
func isNullLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.Literal)
	return ok && lit.Kind == ast.NullLit
}

// moveToFront returns list with v moved to index 0 (no-op when absent).
func moveToFront(list []string, v string) []string {
	out := make([]string, 0, len(list))
	out = append(out, v)
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// rangeOptions enumerates the integer values from min..max stepping by step.
// It returns ok=false when the bounds are not positive-step integers.
func rangeOptions(min, max, step string) ([]string, bool) {
	lo, err1 := strconv.Atoi(min)
	hi, err2 := strconv.Atoi(max)
	st, err3 := strconv.Atoi(step)
	if err1 != nil || err2 != nil || err3 != nil || st <= 0 || hi < lo {
		return nil, false
	}
	var out []string
	for v := lo; v <= hi; v += st {
		out = append(out, strconv.Itoa(v))
	}
	return out, true
}

// indexFor resolves a stable page-unique index for a (component, var) pair.
type indexFor func(component, variable string) int

// Analyze inspects a component body for CSS signal declarations.
//
// When no declaration is present, HaveAny reports false and Errors is empty.
// When a declaration is present but not compilable, Errors lists the reasons
// (the build must fail; there is no fallback).
func Analyze(component string, body []ast.Stmt, index indexFor) *Analyzer {
	a := &Analyzer{
		bySetter:      make(map[string]*Scope),
		byVar:         make(map[string]*Scope),
		byStackMethod: make(map[string]*Scope),
	}
	var decls []sigutil.Decl
	for _, d := range sigutil.Find(body, true) {
		if d.CSSKind != sigutil.CSSKindNone {
			decls = append(decls, d)
		}
	}
	if len(decls) == 0 {
		return a
	}
	a.haveAny = true

	for _, d := range decls {
		if d.As != "" && !KnownRole(d.As) {
			a.errs = append(a.errs, "unknown `as` role preset \""+d.As+"\"; see the ARIA role presets in the docs")
			continue
		}
		switch d.CSSKind {
		case sigutil.CSSKindChoice:
			if d.VarErr != "" {
				a.errs = append(a.errs, "createCSSChoice: "+d.VarErr)
				continue
			}
			if d.Initial == nil || d.Setter == "" {
				a.errs = append(a.errs, "createCSSChoice needs a literal initial value and a setter")
				continue
			}
			initial, ok := literalValue(d.Initial)
			if !ok {
				a.errs = append(a.errs, "createCSSChoice's initial value must be a string or number literal")
				continue
			}
			options := d.Options
			if len(options) == 0 {
				options = inferOptions(body, d.Setter, initial)
			} else if !contains(options, initial) {
				options = append([]string{initial}, options...)
			}
			a.addScope(&Scope{Kind: KindChoice, Var: d.Name, Setter: d.Setter, Initial: initial, Options: options, Role: roleFor(KindChoice, d.As, d.Aria, d.AriaAttrs), Label: d.Label, Vars: d.Vars})
		case sigutil.CSSKindToggle:
			if d.VarErr != "" {
				a.errs = append(a.errs, "createCSSToggle: "+d.VarErr)
				continue
			}
			if d.Initial == nil || d.Setter == "" {
				a.errs = append(a.errs, "createCSSToggle needs a literal initial value and a setter")
				continue
			}
			initial, ok := literalValue(d.Initial)
			if !ok {
				a.errs = append(a.errs, "createCSSToggle's initial value must be true or false")
				continue
			}
			if initial != "true" && initial != "false" {
				a.errs = append(a.errs, "createCSSToggle's initial value must be true or false")
				continue
			}
			a.addScope(&Scope{Kind: KindToggle, Var: d.Name, Setter: d.Setter, Initial: initial, Options: []string{"on"}, Role: roleFor(KindToggle, d.As, d.Aria, d.AriaAttrs), Label: d.Label, Vars: d.Vars})
		case sigutil.CSSKindFlags:
			if d.VarErr != "" {
				a.errs = append(a.errs, "createCSSFlags: "+d.VarErr)
				continue
			}
			if d.Setter == "" || len(d.Options) == 0 {
				a.errs = append(a.errs, "createCSSFlags needs a literal array of flag names and a setter")
				continue
			}
			a.addScope(&Scope{Kind: KindFlags, Var: d.Name, Setter: d.Setter, Options: d.Options, Role: roleFor(KindFlags, d.As, d.Aria, d.AriaAttrs), Label: d.Label, Vars: d.Vars})
		case sigutil.CSSKindGroup:
			if d.VarErr != "" {
				a.errs = append(a.errs, "createCSSGroup: "+d.VarErr)
				continue
			}
			if d.Setter == "" {
				a.errs = append(a.errs, "createCSSGroup needs a setter")
				continue
			}
			initial := closedOption
			if d.Initial != nil {
				if isNullLiteral(d.Initial) {
					initial = closedOption
				} else if v, ok := literalValue(d.Initial); ok {
					initial = v
				} else {
					a.errs = append(a.errs, "createCSSGroup's initial value must be a string, number, or null literal")
					continue
				}
			}
			options := append([]string(nil), d.Options...)
			closed := initial == closedOption
			for _, o := range inferOptions(body, d.Setter, "") {
				if o == "" || o == closedOption {
					continue
				}
				if !contains(options, o) {
					options = append(options, o)
				}
			}
			if !contains(options, closedOption) {
				options = append(options, closedOption)
			}
			if closed {
				options = moveToFront(options, closedOption)
			} else if !contains(options, initial) {
				options = append([]string{initial}, options...)
			}
			a.addScope(&Scope{Kind: KindGroup, Var: d.Name, Setter: d.Setter, Initial: initial, Options: options, Sentinel: closedOption, Role: roleFor(KindGroup, d.As, d.Aria, d.AriaAttrs), Label: d.Label, Vars: d.Vars})
		case sigutil.CSSKindRange:
			if d.VarErr != "" {
				a.errs = append(a.errs, "createCSSRange: "+d.VarErr)
				continue
			}
			if d.Initial == nil || d.Setter == "" {
				a.errs = append(a.errs, "createCSSRange needs a numeric initial value and a setter")
				continue
			}
			initial, ok := literalValue(d.Initial)
			if !ok {
				a.errs = append(a.errs, "createCSSRange's initial value must be a number literal")
				continue
			}
			min, max, step := d.Min, d.Max, d.Step
			if min == "" {
				min = "0"
			}
			if max == "" {
				max = "10"
			}
			if step == "" {
				step = "1"
			}
			options, ok := rangeOptions(min, max, step)
			if !ok {
				a.errs = append(a.errs, "createCSSRange needs integer min/max/step literals")
				continue
			}
			if !contains(options, initial) {
				a.errs = append(a.errs, "createCSSRange's initial value must be within [min, max] on the step grid")
				continue
			}
			a.addScope(&Scope{Kind: KindRange, Var: d.Name, Setter: d.Setter, Initial: initial, Options: options, NumMin: min, NumMax: max, NumStep: step, Role: roleFor(KindRange, d.As, d.Aria, d.AriaAttrs), Label: d.Label, Vars: d.Vars})
		case sigutil.CSSKindStack:
			if d.VarErr != "" {
				a.errs = append(a.errs, "createCSSStack: "+d.VarErr)
				continue
			}
			if len(d.Options) == 0 || d.Setter == "" {
				a.errs = append(a.errs, "createCSSStack needs a literal array of node names")
				continue
			}
			if len(d.Names) < 2 {
				a.errs = append(a.errs, "createCSSStack must destructure [stack, { push, pop, clear }]")
				continue
			}
			// Names after the first are the action object's names; each is a
			// setter-like trigger (push/pop/clear).
			methods := d.Names[1:]
			options := append([]string(nil), d.Options...)
			// The declared array names the root(s); every literal push target is
			// also a node, in source order.
			for _, n := range inferStackNodes(body, methods) {
				if !contains(options, n) {
					options = append(options, n)
				}
			}
			sc := &Scope{Kind: KindStack, Var: d.Name, Setter: d.Setter, Initial: options[0], Options: options, Levels: len(options), Root: options[0], Role: roleFor(KindStack, d.As, d.Aria, d.AriaAttrs), Label: d.Label, Vars: d.Vars}
			sc.Tree = map[string]string{options[0]: ""}
			a.stackMethods = append(a.stackMethods, stackBinding{scope: sc, methods: methods})
			a.addScope(sc)
			for _, m := range methods {
				if m != "" {
					a.byStackMethod[m] = sc
				}
			}
		}
	}

	// Infer each stack's tree from where its pushes appear (the enclosing panel
	// names the parent node), so pop() can be resolved statically.
	if len(a.errs) == 0 {
		if ret := findReturnStmt(body); ret != nil {
			for _, s := range a.scopes {
				if s.Kind == KindStack {
					a.inferStackTree(ret.Value, s, s.Root)
				}
			}
		}
	}

	if len(a.errs) == 0 {
		if r := a.validate(body); r != "" {
			a.errs = append(a.errs, r)
		}
	}
	if len(a.errs) > 0 {
		return a
	}

	// Assign stable indices/classes now that we know the component compiles.
	for _, s := range a.scopes {
		s.Index = index(component, s.Var)
		s.Class = s.base()
	}

	// Register every matched panel condition (deduped) in source order now that
	// scope classes exist, so Conditions() is populated for callers and the
	// builder can emit compound show rules.
	if ret := findReturnStmt(body); ret != nil {
		a.registerPanels(ret.Value)
	}
	return a
}

func (a *Analyzer) addScope(s *Scope) {
	a.scopes = append(a.scopes, s)
	a.bySetter[s.Setter] = s
	a.byVar[s.Var] = s
}

// HaveAny reports whether the component declares any CSS signal.
func (a *Analyzer) HaveAny() bool { return a.haveAny }

// OK reports whether the component compiles with no errors.
func (a *Analyzer) OK() bool { return a.haveAny && len(a.errs) == 0 }

// Errors returns the reasons a CSS signal could not be compiled.
func (a *Analyzer) Errors() []string { return a.errs }

// Scopes returns the analyzed scopes in declaration order. Only meaningful when
// OK is true.
func (a *Analyzer) Scopes() []*Scope { return a.scopes }

// NeedsARIA reports whether any scope uses a role that needs the tiny ARIA
// micro-runtime to keep synthesized state (aria-selected / aria-expanded) in
// sync. When false the output is fully zero-JS.
func (a *Analyzer) NeedsARIA() bool {
	for _, s := range a.scopes {
		if s.Role.NeedsRuntime() {
			return true
		}
	}
	return false
}

// names returns every getter/setter/var identifier belonging to a scope.
func (a *Analyzer) names() map[string]bool {
	set := make(map[string]bool, len(a.scopes)*2)
	for _, s := range a.scopes {
		set[s.Var] = true
		set[s.Setter] = true
	}
	for m := range a.byStackMethod {
		set[m] = true
	}
	return set
}

// MatchTrigger matches an element whose event handler is a valid setter call
// for one of this component's scopes.
func (a *Analyzer) MatchTrigger(el *ast.JSXElement) (Trigger, bool) {
	if el == nil || el.Opening == nil {
		return Trigger{}, false
	}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil || !isOnEvent(attr.Name) {
			continue
		}
		if t, ok := a.matchHandler(attr.Value); ok {
			return t, true
		}
	}
	return Trigger{}, false
}

// matchHandler matches an arrow whose entire body is one setter call.
func (a *Analyzer) matchHandler(handler ast.Expr) (Trigger, bool) {
	fn, ok := handler.(*ast.ArrowFn)
	if !ok || len(fn.Body) != 1 {
		return Trigger{}, false
	}
	var expr ast.Expr
	switch b := fn.Body[0].(type) {
	case *ast.ExprStmt:
		expr = b.Expression
	case *ast.ReturnStmt:
		expr = b.Value
	default:
		return Trigger{}, false
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return Trigger{}, false
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return Trigger{}, false
	}
	// Stack actions (push/pop/clear) are destructured from a stack's second
	// element, not a scope setter.
	if sc := a.byStackMethod[id.Name]; sc != nil {
		return a.matchStackAction(sc, id.Name, call)
	}
	s := a.bySetter[id.Name]
	if s == nil {
		return Trigger{}, false
	}
	return a.matchSetterArgs(s, call)
}

// matchStackAction matches a stack action call: `push('x')`, `pop()`, or
// `clear()`. Each maps to a target option (the segment to select).
func (a *Analyzer) matchStackAction(s *Scope, method string, call *ast.CallExpr) (Trigger, bool) {
	switch method {
	case "clear":
		if len(call.Args) != 0 {
			return Trigger{}, false
		}
		return Trigger{Scope: s, Option: s.Options[0], StackAction: "clear"}, true
	case "pop":
		if len(call.Args) != 0 {
			return Trigger{}, false
		}
		return Trigger{Scope: s, Option: "", StackAction: "pop"}, true
	case "push":
		if len(call.Args) != 1 {
			return Trigger{}, false
		}
		if v, ok := literalValue(call.Args[0]); ok && contains(s.Options, v) {
			return Trigger{Scope: s, Option: v, StackAction: "push"}, true
		}
	}
	return Trigger{}, false
}

// matchSetterArgs validates the setter call shape per kind.
func (a *Analyzer) matchSetterArgs(s *Scope, call *ast.CallExpr) (Trigger, bool) {
	switch s.Kind {
	case KindChoice:
		if len(call.Args) != 1 {
			return Trigger{}, false
		}
		if v, ok := literalValue(call.Args[0]); ok && contains(s.Options, v) {
			return Trigger{Scope: s, Option: v}, true
		}
	case KindGroup:
		// `set('x')` selects an option; `set(null)` closes the group.
		if len(call.Args) != 1 {
			return Trigger{}, false
		}
		if isNullLiteral(call.Args[0]) {
			return Trigger{Scope: s, Option: closedOption}, true
		}
		if v, ok := literalValue(call.Args[0]); ok && contains(s.Options, v) {
			return Trigger{Scope: s, Option: v}, true
		}
	case KindRange:
		if len(call.Args) != 1 {
			return Trigger{}, false
		}
		// A literal index, or `r() + n` / `r() - n` (a zero-JS stepper). The
		// stepper target is resolved relative to the checked index at runtime by
		// emitting a per-index pair of alternate labels; here we record the
		// direction and let the builder emit the bounded chain.
		if v, ok := literalValue(call.Args[0]); ok && contains(s.Options, v) {
			return Trigger{Scope: s, Option: v}, true
		}
		if dir, ok := stepperDirection(call.Args[0], s.Var); ok {
			return Trigger{Scope: s, Option: "", StackAction: dir}, true
		}
	case KindStack:
		// Handled via matchStackAction; a direct setter call selects a node.
		if len(call.Args) != 1 {
			return Trigger{}, false
		}
		if v, ok := literalValue(call.Args[0]); ok && contains(s.Options, v) {
			return Trigger{Scope: s, Option: v, StackAction: "push"}, true
		}
	case KindToggle:
		if len(call.Args) != 1 {
			return Trigger{}, false
		}
		// `set(!on())` (toggle) or `set(true)`/`set(false)`. A label only
		// toggles, so any accepted form maps to "click toggles the checkbox".
		arg := call.Args[0]
		if isNotGetterCall(arg, s.Var) {
			return Trigger{Scope: s, Option: "on"}, true
		}
		if _, ok := literalBool(arg); ok {
			return Trigger{Scope: s, Option: "on"}, true
		}
	case KindFlags:
		if len(call.Args) != 2 {
			return Trigger{}, false
		}
		name, ok := literalValue(call.Args[0])
		if !ok || !contains(s.Options, name) {
			return Trigger{}, false
		}
		// The value may be a literal bool or `!flags.x()`. A label only
		// toggles the checkbox, so the option name is what matters.
		if _, ok := literalBool(call.Args[1]); !ok && !isNotFlagCall(call.Args[1]) {
			return Trigger{}, false
		}
		return Trigger{Scope: s, Option: name}, true
	}
	return Trigger{}, false
}

// inferStackTree walks the returned JSX, tracking the current stack node from
// the enclosing panel conditions, and records each `push('x')` target's parent.
func (a *Analyzer) inferStackTree(expr ast.Expr, s *Scope, cur string) {
	switch e := expr.(type) {
	case *ast.JSXElement:
		if e == nil {
			return
		}
		node := cur
		if cond, ok := a.ParsePanel(e); ok && cond.referencesScope(s) {
			if n := cond.nodeForScope(s); n != "" {
				node = n
			}
		}
		a.recordStackPushes(e, s, node)
		for _, child := range e.Children {
			a.inferStackChild(child, s, node)
		}
	case *ast.JSXFragment:
		for _, child := range e.Children {
			a.inferStackChild(child, s, cur)
		}
	case *ast.TypeAssertion:
		a.inferStackTree(e.Expr, s, cur)
	}
}

func (a *Analyzer) inferStackChild(child ast.JSXChild, s *Scope, cur string) {
	switch c := child.(type) {
	case *ast.JSXElementChild:
		a.inferStackTree(c.Element, s, cur)
	case *ast.JSXFragmentChild:
		a.inferStackTree(c.Fragment, s, cur)
	case *ast.JSXExprContainer:
		a.inferStackTree(c.Expression, s, cur)
	}
}

// referencesScope reports whether a condition reads the given scope.
func (c *Condition) referencesScope(s *Scope) bool {
	for _, sc := range c.Scopes {
		if sc == s {
			return true
		}
	}
	return false
}

// nodeForScope returns the option of the first single-atom condition term that
// selects a non-negated option of s (the panel's stack node).
func (c *Condition) nodeForScope(s *Scope) string {
	for _, term := range c.Terms {
		for _, at := range term {
			if at.Scope == s && !at.Negated {
				return at.Option
			}
		}
	}
	return ""
}

// recordStackPushes records the parent of every `push('x')` appearing on an
// element (typically a trigger inside a panel at stack node parent).
func (a *Analyzer) recordStackPushes(el *ast.JSXElement, s *Scope, parent string) {
	if el == nil || el.Opening == nil {
		return
	}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil || !isOnEvent(attr.Name) {
			continue
		}
		walkExpr(attr.Value, func(call *ast.CallExpr) {
			id, ok := call.Callee.(*ast.Identifier)
			if !ok || id.Name != "push" || len(call.Args) != 1 {
				return
			}
			if a.byStackMethod[id.Name] != s {
				return
			}
			if v, ok := literalValue(call.Args[0]); ok && contains(s.Options, v) {
				if _, seen := s.Tree[v]; !seen {
					s.Tree[v] = parent
				}
			}
		})
	}
}

// inferStackNodes collects every literal first argument of a `push('x')` call
// to one of the stack's action names, in source order. These are the declared
// node names (the array holds the initial/root node).
func inferStackNodes(body []ast.Stmt, methods []string) []string {
	names := make(map[string]bool, len(methods))
	push := ""
	for _, m := range methods {
		names[m] = true
	}
	// The conventional action name is "push"; any method invoked with a single
	// string literal and named push is treated as a node source.
	if names["push"] {
		push = "push"
	}
	if push == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	walkStmts(body, func(call *ast.CallExpr) {
		id, ok := call.Callee.(*ast.Identifier)
		if !ok || id.Name != push || len(call.Args) != 1 {
			return
		}
		if v, ok := literalValue(call.Args[0]); ok && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	})
	return out
}

// stepperDirection recognises `getter() +/- n` and returns "inc" or "dec".
func stepperDirection(expr ast.Expr, getter string) (string, bool) {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return "", false
	}
	if bin.Op != "+" && bin.Op != "-" {
		return "", false
	}
	call, ok := bin.Left.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return "", false
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok || id.Name != getter {
		return "", false
	}
	if bin.Op == "+" {
		return "inc", true
	}
	return "dec", true
}

// MatchText matches a bare getter read `get()` (or `stack.top()`) rendered as
// JSX text, so it can compile to a CSS-variable-backed live value with zero JS.
// It returns the scope whose `--krate-current` drives the text.
func (a *Analyzer) MatchText(expr ast.Expr) (*Scope, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	name, ok := getterName(call)
	if !ok {
		return nil, false
	}
	s := a.byVar[name]
	if s == nil {
		return nil, false
	}
	if !s.isChoiceLike() && s.Kind != KindToggle {
		return nil, false
	}
	return s, true
}

// MatchPanel matches an element whose `showIf`/`visibleIf` test selects one of
// this component's scopes. Matching a panel registers its condition (assigning
// a deterministic wrapper class); identical conditions dedupe to one class.
func (a *Analyzer) MatchPanel(el *ast.JSXElement) (Panel, bool) {
	cond, ok := a.ParsePanel(el)
	if !ok {
		return Panel{}, false
	}
	return Panel{Cond: a.registerCondition(cond)}, true
}

// ParsePanel parses an element's `showIf`/`visibleIf` test into a condition
// without registering it. Validation uses this so it can classify a panel
// without consuming wrapper-class indices.
func (a *Analyzer) ParsePanel(el *ast.JSXElement) (*Condition, bool) {
	if el == nil || el.Opening == nil {
		return nil, false
	}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil {
			continue
		}
		if attr.Name != "showIf" && attr.Name != "visibleIf" {
			continue
		}
		return a.matchPanelTest(attr.Value)
	}
	return nil, false
}

// Conditions returns the registered conditions in dedupe (first-match) order.
func (a *Analyzer) Conditions() []*Condition { return a.conditions }

// registerCondition returns the canonical condition for c, assigning its
// wrapper class. Identical conditions dedupe to the same pointer and class; a
// condition with a single atom keeps the classic per-scope wrapper class, while
// compound conditions consume a component-local ExprIdx.
func (a *Analyzer) registerCondition(c *Condition) *Condition {
	if a.condByKey == nil {
		a.condByKey = make(map[string]*Condition)
	}
	key := a.conditionKey(c)
	if existing, ok := a.condByKey[key]; ok {
		return existing
	}
	if c.simple() {
		at := c.Terms[0][0]
		c.Class = at.Scope.PanelWrapperClass(at.Option, at.Negated)
	} else {
		c.ExprIdx = a.exprIdx
		a.exprIdx++
		c.Class = c.owningScope().Class + "-x-" + strconv.Itoa(c.ExprIdx)
	}
	a.condByKey[key] = c
	a.conditions = append(a.conditions, c)
	return c
}

// registerPanels walks the component's returned JSX and registers every matched
// panel condition (deduped, in source order). Called by Analyze after scope
// classes are stamped so Conditions() is populated without the builder.
func (a *Analyzer) registerPanels(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.JSXElement:
		if e == nil {
			return
		}
		// Registers the panel (or dedupes it to an existing condition).
		a.MatchPanel(e)
		for _, child := range e.Children {
			a.registerPanelChild(child)
		}
	case *ast.JSXFragment:
		for _, child := range e.Children {
			a.registerPanelChild(child)
		}
	case *ast.TypeAssertion:
		a.registerPanels(e.Expr)
	}
}

func (a *Analyzer) registerPanelChild(child ast.JSXChild) {
	switch c := child.(type) {
	case *ast.JSXElementChild:
		a.registerPanels(c.Element)
	case *ast.JSXFragmentChild:
		a.registerPanels(c.Fragment)
	case *ast.JSXExprContainer:
		// A bare `{getter()}` text read compiles to a live value driven by the
		// scope's `--krate-current`; mark the scope so the stylesheet emits the
		// `.krc-live` content rule.
		if s, ok := a.MatchText(c.Expression); ok {
			s.LiveText = true
		}
		a.registerPanels(c.Expression)
	}
}

// matchPanelTest parses a test expression into a DNF condition over this
// component's atoms. An expression that is not classifiable returns ok=false.
func (a *Analyzer) matchPanelTest(test ast.Expr) (*Condition, bool) {
	terms, ok := a.parseBoolean(test)
	if !ok {
		return nil, false
	}
	terms = a.normalizeTerms(terms)
	if len(terms) == 0 {
		return nil, false
	}
	return &Condition{Terms: terms, Scopes: collectScopes(terms)}, true
}

// parseBoolean recursively parses a boolean expression into DNF terms: OR of
// (AND of atoms). Parens are transparent in this parser's AST, so no grouped
// expression node needs special handling.
func (a *Analyzer) parseBoolean(expr ast.Expr) ([][]*Atom, bool) {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op != "!" {
			return nil, false
		}
		terms, ok := a.parseBoolean(e.Arg)
		if !ok {
			return nil, false
		}
		return a.negate(terms), true
	case *ast.BinaryExpr:
		switch e.Op {
		case "&&":
			l, lok := a.parseBoolean(e.Left)
			if !lok {
				return nil, false
			}
			r, rok := a.parseBoolean(e.Right)
			if !rok {
				return nil, false
			}
			return a.and(l, r), true
		case "||":
			l, lok := a.parseBoolean(e.Left)
			if !lok {
				return nil, false
			}
			r, rok := a.parseBoolean(e.Right)
			if !rok {
				return nil, false
			}
			return append(l, r...), true
		case "===", "==", "!==", "!=":
			negated := e.Op == "!==" || e.Op == "!="
			if at, ok := a.matchCompareAtom(e.Left, e.Right, negated); ok {
				return [][]*Atom{{at}}, true
			}
			if at, ok := a.matchCompareAtom(e.Right, e.Left, negated); ok {
				return [][]*Atom{{at}}, true
			}
			return nil, false
		}
		return nil, false
	case *ast.CallExpr:
		if at, ok := a.matchTruthyAtom(e); ok {
			return [][]*Atom{{at}}, true
		}
	}
	return nil, false
}

// matchCompareAtom matches `get() === literal` for a choice scope, or its
// negated form when negated is true (`get() !== literal`).
func (a *Analyzer) matchCompareAtom(getterSide, literalSide ast.Expr, negated bool) (*Atom, bool) {
	call, ok := getterSide.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	name, ok := getterName(call)
	if !ok {
		return nil, false
	}
	s := a.byVar[name]
	if s == nil || !s.isChoiceLike() {
		return nil, false
	}
	val, ok := literalValue(literalSide)
	if !ok || !contains(s.Options, val) {
		return nil, false
	}
	return &Atom{Scope: s, Option: val, Negated: negated}, true
}

// getterName resolves the scope getter identifier behind a call callee. An
// ordinary getter is `get()`; a stack reads its top via `stack.top()`.
func getterName(call *ast.CallExpr) (string, bool) {
	switch callee := call.Callee.(type) {
	case *ast.Identifier:
		return callee.Name, true
	case *ast.MemberExpr:
		// `stack.top()` — the property may be "top"/"peek"; the object is the
		// scope variable.
		if id, ok := callee.Object.(*ast.Identifier); ok {
			if prop, ok := callee.Property.(*ast.Identifier); ok && (prop.Name == "top" || prop.Name == "peek") {
				return id.Name, true
			}
		}
	}
	return "", false
}

// matchTruthyAtom matches a bare truthy getter read: `on()` (toggle) or
// `flags.x()` (flag).
func (a *Analyzer) matchTruthyAtom(expr ast.Expr) (*Atom, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	switch callee := call.Callee.(type) {
	case *ast.Identifier:
		s := a.byVar[callee.Name]
		if s == nil || s.Kind != KindToggle {
			return nil, false
		}
		return &Atom{Scope: s, Option: "on"}, true
	case *ast.MemberExpr:
		obj, ok := callee.Object.(*ast.Identifier)
		if !ok {
			return nil, false
		}
		s := a.byVar[obj.Name]
		if s == nil || s.Kind != KindFlags {
			return nil, false
		}
		prop, ok := callee.Property.(*ast.Identifier)
		if !ok || !contains(s.Options, prop.Name) {
			return nil, false
		}
		return &Atom{Scope: s, Option: prop.Name}, true
	}
	return nil, false
}

// and conjoins two DNF term lists via the cross product, pruning terms that
// become a contradiction.
func (a *Analyzer) and(x, y [][]*Atom) [][]*Atom {
	out := make([][]*Atom, 0, len(x)*len(y))
	for _, tx := range x {
		for _, ty := range y {
			merged := make([]*Atom, 0, len(tx)+len(ty))
			merged = append(merged, tx...)
			merged = append(merged, ty...)
			if term := a.mergeTerm(merged); len(term) > 0 {
				out = append(out, term)
			}
		}
	}
	return out
}

// negate returns the DNF of `!expr` given the DNF of `expr`, applying De
// Morgan: each AND-term's literals negate to a clause (OR of negated atoms),
// and the clauses are ANDed via the cross product.
func (a *Analyzer) negate(terms [][]*Atom) [][]*Atom {
	result := [][]*Atom{{}}
	for _, t := range terms {
		var next [][]*Atom
		for _, lit := range t {
			neg := *lit
			neg.Negated = !neg.Negated
			for _, acc := range result {
				cand := make([]*Atom, 0, len(acc)+1)
				cand = append(cand, acc...)
				cand = append(cand, &neg)
				if m := a.mergeTerm(cand); len(m) > 0 {
					next = append(next, m)
				}
			}
		}
		if len(next) == 0 {
			return nil // contradiction
		}
		result = next
	}
	return result
}

// mergeTerm dedupes atoms within one AND-term and drops the term when it
// contains an atom and its negation (an impossible conjunction).
func (a *Analyzer) mergeTerm(atoms []*Atom) []*Atom {
	saw := make(map[string]*Atom, len(atoms)) // abs-key → first atom (records polarity)
	out := make([]*Atom, 0, len(atoms))
	for _, at := range atoms {
		abs := a.atomAbsKey(at)
		if prev, ok := saw[abs]; ok {
			if prev.Negated != at.Negated {
				return nil // opposite literals in one AND-term
			}
			continue // duplicate literal
		}
		saw[abs] = at
		out = append(out, at)
	}
	return out
}

// normalizeTerms canonicalizes a DNF: sorts atoms within each term and the
// terms themselves, and dedupes duplicate terms.
func (a *Analyzer) normalizeTerms(terms [][]*Atom) [][]*Atom {
	seen := make(map[string]bool, len(terms))
	out := make([][]*Atom, 0, len(terms))
	for _, t := range terms {
		t = a.mergeTerm(t)
		if len(t) == 0 {
			continue
		}
		sort.Slice(t, func(i, j int) bool { return a.atomKey(t[i]) < a.atomKey(t[j]) })
		key := a.termKey(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return a.termKey(out[i]) < a.termKey(out[j]) })
	return out
}

// conditionKey is the canonical dedupe key for a condition: the sorted term
// keys joined by ";".
func (a *Analyzer) conditionKey(c *Condition) string {
	var b strings.Builder
	for i, t := range c.Terms {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(a.termKey(t))
	}
	return b.String()
}

func (a *Analyzer) termKey(t []*Atom) string {
	var b strings.Builder
	for i, at := range t {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(a.atomKey(at))
	}
	return b.String()
}

// atomKey is the deterministic total-order key for sorting/deduping atoms. It
// uses the scope's declaration position (stable across analysis phases), not
// its page index, so dedupe is phase-independent.
func (a *Analyzer) atomKey(at *Atom) string {
	return atomKeyAt(a.scopePos(at.Scope), at.Option, at.Negated)
}

// atomAbsKey identifies an atom scope+option regardless of polarity, for
// contradiction detection within a term.
func (a *Analyzer) atomAbsKey(at *Atom) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(a.scopePos(at.Scope)))
	b.WriteByte('|')
	b.WriteString(at.Option)
	return b.String()
}

func atomKeyAt(pos int, option string, negated bool) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(pos))
	b.WriteByte('|')
	b.WriteString(option)
	b.WriteByte('|')
	if negated {
		b.WriteByte('1')
	} else {
		b.WriteByte('0')
	}
	return b.String()
}

// scopePos returns the declaration position of a scope in the analyzer.
func (a *Analyzer) scopePos(s *Scope) int {
	for i, x := range a.scopes {
		if x == s {
			return i
		}
	}
	return -1
}

// simple reports whether the condition is a single atom, which keeps the
// classic per-scope wrapper classes (krcN-p-<x> / krcN-n-<x>).
func (c *Condition) simple() bool {
	return len(c.Terms) == 1 && len(c.Terms[0]) == 1
}

// owningScope is the anchor scope: the referenced scope with the lowest page
// index. Its class prefixes the selectors and the compound wrapper class.
func (c *Condition) owningScope() *Scope {
	var owner *Scope
	for _, s := range c.Scopes {
		if owner == nil || s.Index < owner.Index {
			owner = s
		}
	}
	return owner
}

// OwningScope returns the anchor scope (the referenced scope with the lowest
// page index), used by the builder to order deduped conditions deterministically.
func (c *Condition) OwningScope() *Scope { return c.owningScope() }

// PanelRole returns the ARIA role for the panel wrapper, taken from the owning
// scope's resolved role (empty when none).
func (c *Condition) PanelRole() string {
	if o := c.owningScope(); o != nil {
		return o.Role.Panel
	}
	return ""
}

// StackNode returns the stack node (option) this condition selects, or "" when
// it selects none. Used by the builder to resolve nested pop() triggers.
func (c *Condition) StackNode() string {
	for _, term := range c.Terms {
		for _, at := range term {
			if at.Scope != nil && at.Scope.Kind == KindStack && !at.Negated {
				return at.Option
			}
		}
	}
	return ""
}

// collectScopes returns the distinct scopes referenced by a DNF term list, in
// first-appearance order.
func collectScopes(terms [][]*Atom) []*Scope {
	var out []*Scope
	seen := make(map[*Scope]bool)
	for _, t := range terms {
		for _, at := range t {
			if !seen[at.Scope] {
				seen[at.Scope] = true
				out = append(out, at.Scope)
			}
		}
	}
	return out
}

// literalBool resolves a boolean literal.
func literalBool(expr ast.Expr) (bool, bool) {
	lit, ok := expr.(*ast.Literal)
	if !ok || lit.Kind != ast.BoolLit {
		return false, false
	}
	return lit.Value == "true", true
}

// isNotFlagCall reports whether expr is `!flags.<x>()`.
func isNotFlagCall(expr ast.Expr) bool {
	u, ok := expr.(*ast.UnaryExpr)
	if !ok || u.Op != "!" {
		return false
	}
	call, ok := u.Arg.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	_, ok = call.Callee.(*ast.MemberExpr)
	return ok
}

// isNotGetterCall reports whether expr is `!getter()` for the given getter.
func isNotGetterCall(expr ast.Expr, getter string) bool {
	u, ok := expr.(*ast.UnaryExpr)
	if !ok || u.Op != "!" {
		return false
	}
	call, ok := u.Arg.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	id, ok := call.Callee.(*ast.Identifier)
	return ok && id.Name == getter
}

// literalValue resolves a string/number/bool literal to its string value.
func literalValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.Literal)
	if !ok {
		return "", false
	}
	switch lit.Kind {
	case ast.StringLit, ast.NumberLit, ast.BoolLit:
		return lit.Value, true
	}
	return "", false
}

// inferOptions collects every literal the choice setter is invoked with
// throughout the body (in source order), then prepends the initial value.
func inferOptions(body []ast.Stmt, setter, initial string) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(v string) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	add(initial)
	walkStmts(body, func(call *ast.CallExpr) {
		id, ok := call.Callee.(*ast.Identifier)
		if !ok || id.Name != setter || len(call.Args) != 1 {
			return
		}
		if v, ok := literalValue(call.Args[0]); ok {
			add(v)
		}
	})
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// sanitizeToken converts an arbitrary option token to a CSS-safe suffix.
func sanitizeToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	t := strings.Trim(b.String(), "-")
	if t == "" {
		return "x"
	}
	return t
}

// SanitizeToken is the exported form used by the IR builder for instance tokens.
func SanitizeToken(s string) string { return sanitizeToken(s) }

const base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// encodeIndex encodes a scope index in base62 for compact class names.
func encodeIndex(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = base62Chars[n%62]
		n /= 62
	}
	return string(buf[i:])
}
