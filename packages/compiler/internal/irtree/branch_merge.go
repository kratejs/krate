package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Same-type branch merge.
//
// React keeps a component instance when the two branches of a conditional
// render the same component type at the same position:
//
//	isEdit ? <Form mode="edit" /> : <Form mode="new" />
//
// Mounting each branch separately would restart the component (state,
// effects, refs, typed input) on every flip. The pair is lowered to the one
// element a person would write by hand, with the differing props turned into
// conditional expressions that the normal prop reactivity already tracks:
//
//	<Form mode={isEdit ? "edit" : "new"} />
//
// Only components that hold state are merged, so stateless components keep
// their existing output. Branches with different keys are left alone (React
// remounts them too). Shapes that cannot be lowered warn with KR8360.

type mergeStatus int

const (
	// mergeNone: the pair is not a same-type pair, or React remounts it too.
	mergeNone mergeStatus = iota
	// mergeOK: the pair was lowered to a single element.
	mergeOK
	// mergeFail: the pair is a same-type pair that cannot be lowered.
	mergeFail
)

// mergeSameTypeBranches lowers a ternary whose branches render the same
// stateful component to one element. ok is false when the ternary is left as
// a conditional; a same-type pair that cannot be lowered also warns.
func (b *builder) mergeSameTypeBranches(cond *ast.ConditionalExpr) (*ast.JSXElement, bool) {
	el, status, reason, pos := b.mergeBranches(cond)
	switch status {
	case mergeOK:
		return el, true
	case mergeFail:
		name := ""
		if el != nil && el.Opening != nil {
			name = el.Opening.Name
		}
		b.warnCodeAt(pos, diag.CodeBranchStateReset,
			"state of <"+name+"> resets when the condition changes: "+reason,
			"render a single <"+name+"> and pass the differing values as props, give both branches the same shape, or use different key props when the restart is intended")
	}
	return nil, false
}

// mergeBranches resolves one ternary. On mergeFail the returned element is
// the second same-type element, and pos is where to report.
func (b *builder) mergeBranches(cond *ast.ConditionalExpr) (*ast.JSXElement, mergeStatus, string, ast.Pos) {
	cons, cs, creason, cpos := b.branchLeaf(cond.Consequent)
	alt, as, areason, apos := b.branchLeaf(cond.Alternate)
	if cs == mergeFail {
		return cons, cs, creason, cpos
	}
	if as == mergeFail {
		return alt, as, areason, apos
	}
	if cons == nil || alt == nil || cs != mergeOK && cs != mergeNone || as != mergeOK && as != mergeNone {
		return nil, mergeNone, "", ast.Pos{}
	}
	if cons.Opening == nil || alt.Opening == nil || cons.Opening.Name != alt.Opening.Name {
		return nil, mergeNone, "", ast.Pos{}
	}
	name := cons.Opening.Name
	if !sameKey(cons, alt) {
		return nil, mergeNone, "", ast.Pos{}
	}
	if !b.holdsInstanceState(name) {
		// A wrapper that holds no state is rebuilt per branch, and so is
		// anything stateful inside it that React would keep.
		if inner := b.nestedStatefulPair(cons, alt, 0); inner != nil {
			return inner, mergeFail, "it sits inside <" + name + ">, which each branch builds separately", inner.Position
		}
		return nil, mergeNone, "", ast.Pos{}
	}
	if reason := b.unmergeableReason(name); reason != "" {
		return alt, mergeFail, reason, alt.Position
	}
	if !pureTest(cond.Test) {
		return alt, mergeFail, "the condition has side effects, so it cannot be evaluated once per prop", alt.Position
	}
	merged, reason := mergeElements(cond.Test, cons, alt)
	if merged == nil {
		return alt, mergeFail, reason, alt.Position
	}
	return merged, mergeOK, "", ast.Pos{}
}

// branchLeaf returns the element a branch renders: a JSX element, or the
// merge of a nested ternary chain (`a ? <X/> : b ? <X/> : <X/>`). A chain
// whose branches are not all the same type returns nil.
func (b *builder) branchLeaf(expr ast.Expr) (*ast.JSXElement, mergeStatus, string, ast.Pos) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return b.branchLeaf(e.Expr)
	case *ast.JSXElement:
		return e, mergeNone, "", ast.Pos{}
	case *ast.ConditionalExpr:
		el, status, reason, pos := b.mergeBranches(e)
		if status == mergeOK {
			return el, mergeOK, "", ast.Pos{}
		}
		return el, status, reason, pos
	}
	return nil, mergeNone, "", ast.Pos{}
}

// holdsInstanceState reports whether a component keeps something a remount
// would lose: signals, effects, reactive factories or an opaque hook.
func (b *builder) holdsInstanceState(name string) bool {
	if strings.ContainsRune(name, '.') || name == "" || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	fn := b.functions[name]
	if fn == nil {
		return false
	}
	if b.ann.ComponentTiers[name] == TierRuntime {
		return false
	}
	if _, ok := b.goComponents[name]; ok {
		return false
	}
	if _, ok := b.consumerContext(name); ok {
		return false
	}
	if _, ok := b.providerContext(name); ok {
		return false
	}
	if len(b.collectSignalDecls(fn)) > 0 {
		return true
	}
	return hasLifecycleCall(fn.Body) || bodyUsesReactiveFactory(fn.Body) || b.callsOpaqueHook(fn.Body)
}

// unmergeableReason reports why a stateful component cannot be lowered, or "".
func (b *builder) unmergeableReason(name string) string {
	if b.isLazyComponent(name) {
		return "a lazy component is loaded per branch"
	}
	return ""
}

// sameKey reports whether two elements agree on their key: both absent or
// the same expression. Different keys mean React remounts too.
func sameKey(a, c *ast.JSXElement) bool {
	ka, kc := jsxKeyAttr(a), jsxKeyAttr(c)
	if ka == nil && kc == nil {
		return true
	}
	if ka == nil || kc == nil {
		return false
	}
	return astprint.PrintExpr(ka) == astprint.PrintExpr(kc)
}

// pureTest reports whether evaluating a test several times is safe.
func pureTest(test ast.Expr) bool {
	pure := true
	WalkAST(test, func(n any) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if strings.HasSuffix(x.Op, "=") && x.Op != "==" && x.Op != "===" && x.Op != "!=" && x.Op != "!==" && x.Op != "<=" && x.Op != ">=" {
				pure = false
			}
		case *ast.UnaryExpr:
			if x.Op == "++" || x.Op == "--" || x.Op == "delete" {
				pure = false
			}
		case *ast.AwaitExpr, *ast.NewExpr, *ast.ArrowFn, *ast.FnDecl, *ast.ClassExpr, *ast.JSXElement, *ast.JSXFragment:
			pure = false
		}
		return pure
	})
	return pure
}

// mergeElements builds the single element for a same-type pair. It returns
// nil and a reason when the pair cannot be expressed as one element.
func mergeElements(test ast.Expr, a, c *ast.JSXElement) (*ast.JSXElement, string) {
	if functionChild(a.Children) != nil || functionChild(c.Children) != nil {
		return nil, "a render-prop function as children cannot be switched"
	}
	attrs, reason := mergeAttrs(test, a.Opening.Attributes, c.Opening.Attributes)
	if attrs == nil && reason != "" {
		return nil, reason
	}
	children := a.Children
	if printChildren(a.Children) != printChildren(c.Children) {
		children = []ast.JSXChild{&ast.JSXExprContainer{Expression: &ast.ConditionalExpr{
			Position:   a.Position,
			Test:       test,
			Consequent: childrenExpr(a.Children),
			Alternate:  childrenExpr(c.Children),
		}}}
	}
	name := a.Opening.Name
	merged := &ast.JSXElement{
		Position: a.Position,
		Opening:  &ast.JSXOpening{Name: name, Attributes: attrs, SelfClosing: len(children) == 0},
		Children: children,
	}
	if len(children) > 0 {
		merged.Closing = &ast.JSXClosing{Name: name}
	}
	return merged, ""
}

// childrenExpr wraps a children list as one expression: a fragment, or null
// when the list is empty.
func childrenExpr(children []ast.JSXChild) ast.Expr {
	if len(children) == 0 {
		return nullExpr()
	}
	return &ast.JSXFragment{Children: children}
}

func printChildren(children []ast.JSXChild) string {
	if len(children) == 0 {
		return ""
	}
	return astprint.PrintExpr(&ast.JSXFragment{Children: children})
}

// attrValue is an attribute's value, with the implicit `true` of a bare
// attribute made explicit.
func attrValue(a *ast.JSXAttr) ast.Expr {
	if a.Value == nil {
		return &ast.Literal{Kind: ast.BoolLit, Value: "true"}
	}
	return a.Value
}

// mergeAttrs merges the attributes of two same-type elements. Equal props
// are kept once, differing props become `test ? a : b`, and a prop on one
// side only becomes `test ? a : undefined`. Spreads must be identical and in
// the same place, because their order against named props decides overrides.
func mergeAttrs(test ast.Expr, a, c []*ast.JSXAttr) ([]*ast.JSXAttr, string) {
	hasSpread := false
	for _, list := range [][]*ast.JSXAttr{a, c} {
		for _, at := range list {
			if at != nil && at.Spread {
				hasSpread = true
			}
		}
	}
	if hasSpread {
		if len(a) != len(c) {
			return nil, "a spread props object is in different places in the two branches"
		}
		for i := range a {
			if a[i] == nil || c[i] == nil || a[i].Spread != c[i].Spread {
				return nil, "a spread props object is in different places in the two branches"
			}
			if a[i].Spread && astprint.PrintExpr(a[i].Value) != astprint.PrintExpr(c[i].Value) {
				return nil, "the branches spread different props objects"
			}
			if !a[i].Spread && a[i].Name != c[i].Name {
				return nil, "a spread props object is in different places in the two branches"
			}
		}
	}
	byName := make(map[string]*ast.JSXAttr, len(c))
	for _, at := range c {
		if at != nil && !at.Spread {
			byName[at.Name] = at
		}
	}
	out := make([]*ast.JSXAttr, 0, len(a)+len(c))
	used := map[string]bool{}
	for _, at := range a {
		if at == nil {
			continue
		}
		if at.Spread {
			out = append(out, at)
			continue
		}
		other := byName[at.Name]
		used[at.Name] = true
		if at.Name == "ref" {
			if other == nil || astprint.PrintExpr(attrValue(at)) != astprint.PrintExpr(attrValue(other)) {
				return nil, "the branches pass a different ref"
			}
			out = append(out, at)
			continue
		}
		if at.Name == "key" {
			out = append(out, at)
			continue
		}
		if other == nil {
			out = append(out, condAttr(at, test, attrValue(at), undefinedLit()))
			continue
		}
		av, ov := attrValue(at), attrValue(other)
		if astprint.PrintExpr(av) == astprint.PrintExpr(ov) {
			out = append(out, at)
			continue
		}
		out = append(out, condAttr(at, test, av, ov))
	}
	for _, at := range c {
		if at == nil || at.Spread || used[at.Name] || at.Name == "key" {
			continue
		}
		if at.Name == "ref" {
			return nil, "the branches pass a different ref"
		}
		out = append(out, condAttr(at, test, undefinedLit(), attrValue(at)))
	}
	return out, ""
}

func condAttr(at *ast.JSXAttr, test, cons, alt ast.Expr) *ast.JSXAttr {
	return &ast.JSXAttr{
		Position: at.Position,
		Name:     at.Name,
		Value:    &ast.ConditionalExpr{Position: at.Position, Test: test, Consequent: cons, Alternate: alt},
	}
}

// nestedStatefulPair finds, below two same-tag elements, a stateful component
// that React would keep: the same type at the same child position in both,
// with the same key. It returns the one in the second element.
func (b *builder) nestedStatefulPair(a, c *ast.JSXElement, depth int) *ast.JSXElement {
	if depth > 6 {
		return nil
	}
	ca, cc := positionalChildren(a.Children), positionalChildren(c.Children)
	for i := 0; i < len(ca) && i < len(cc); i++ {
		ea, ec := ca[i], cc[i]
		if ea == nil || ec == nil || ea.Opening == nil || ec.Opening == nil || ea.Opening.Name != ec.Opening.Name || !sameKey(ea, ec) {
			continue
		}
		if b.holdsInstanceState(ea.Opening.Name) {
			return ec
		}
		if inner := b.nestedStatefulPair(ea, ec, depth+1); inner != nil {
			return inner
		}
	}
	return nil
}

// positionalChildren lists a children array the way React indexes it: one
// entry per child, nil for anything that is not an element, with blank text
// between tags dropped.
func positionalChildren(children []ast.JSXChild) []*ast.JSXElement {
	var out []*ast.JSXElement
	for _, ch := range children {
		switch c := ch.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(c.Value) != "" {
				out = append(out, nil)
			}
		case *ast.JSXElementChild:
			out = append(out, c.Element)
		default:
			out = append(out, nil)
		}
	}
	return out
}
