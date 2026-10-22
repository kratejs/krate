package irtree

import "github.com/kratejs/krate/packages/compiler/ast"

// SpreadExpansion is what one `{...expr}` attribute contributes to an element.
type SpreadExpansion struct {
	// Attrs are the attributes the spread provides, in key order.
	Attrs []*ast.JSXAttr
	// Resolved reports whether the spread value was known. An unresolved
	// spread is kept on the element unchanged (and skipped by emitters).
	Resolved bool
	// ForwardsChildren reports whether the spread carries `children`, which
	// React renders as the element's content when it has none of its own.
	ForwardsChildren bool
}

// MergeSpreadAttrs expands the spread attributes of an element with React's
// props semantics: attributes apply in source order and a later attribute or
// spread overrides an earlier one with the same name, keeping the position
// where the name first appeared (object key order). expand resolves one
// spread attribute. The second result reports whether any resolved spread
// forwards children.
func MergeSpreadAttrs(attrs []*ast.JSXAttr, expand func(*ast.JSXAttr) SpreadExpansion) ([]*ast.JSXAttr, bool) {
	hasSpread := false
	for _, a := range attrs {
		if a != nil && a.Spread {
			hasSpread = true
			break
		}
	}
	if !hasSpread {
		return attrs, false
	}
	out := make([]*ast.JSXAttr, 0, len(attrs))
	index := make(map[string]int)
	set := func(a *ast.JSXAttr) {
		if i, ok := index[a.Name]; ok {
			out[i] = a
			return
		}
		index[a.Name] = len(out)
		out = append(out, a)
	}
	children := false
	for _, a := range attrs {
		if a == nil {
			continue
		}
		if !a.Spread {
			set(a)
			continue
		}
		if a.Value == nil {
			out = append(out, a)
			continue
		}
		exp := expand(a)
		if !exp.Resolved {
			out = append(out, a)
			continue
		}
		children = children || exp.ForwardsChildren
		for _, p := range exp.Attrs {
			set(p)
		}
	}
	return out, children
}
