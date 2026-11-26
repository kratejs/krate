package irtree

import (
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// A tag held in a variable that reads a module-level table of components
// (`const View = views[kind()]; <View/>`) cannot be resolved to one component
// at build time. It is lowered to a conditional over the components the table
// holds, each branch a plain tag, so the existing conditional machinery
// renders the right one (server markup included), updates it when the value
// changes, and ships the branches' bodies and helpers like any other tag.

// maxDynamicTagCandidates bounds the conditional chain.
const maxDynamicTagCandidates = 16

// dynamicTagSwitch lowers el to a conditional over candidate components, or
// returns nil when its tag is not a variable read from a component table.
func (b *builder) dynamicTagSwitch(el *ast.JSXElement) ast.Expr {
	name := el.Opening.Name
	if name == "" || b.ann == nil || len(b.moduleConstInits) == 0 {
		return nil
	}
	if b.ann.Functions[name] != nil || strings.Contains(name, ".") {
		return nil
	}
	tag := b.localTagInit(name)
	if tag == nil {
		return nil
	}
	cands := b.componentTableMembers(tag)
	if len(cands) == 0 {
		return nil
	}
	var out ast.Expr = nullExpr()
	for i := len(cands) - 1; i >= 0; i-- {
		clone := *el
		opening := *el.Opening
		opening.Name = cands[i]
		clone.Opening = &opening
		if el.Closing != nil {
			closing := *el.Closing
			closing.Name = cands[i]
			clone.Closing = &closing
		}
		out = &ast.ConditionalExpr{
			Position:   el.Position,
			Test:       &ast.BinaryExpr{Position: el.Position, Op: "===", Left: tag, Right: &ast.Identifier{Name: cands[i]}},
			Consequent: &clone,
			Alternate:  out,
		}
	}
	return out
}

// localTagInit returns the initializer of the component-body const name.
func (b *builder) localTagInit(name string) ast.Expr {
	for _, stmt := range b.localFnBody {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok || vs.Kind != ast.VarConst {
			continue
		}
		for _, d := range vs.Decls {
			if d != nil && !d.IsDestructuring && d.Name == name && d.Init != nil {
				return unwrapDerived(d.Init)
			}
		}
	}
	return nil
}

// unwrapDerived returns the expression a bundler-lowered derived getter
// (`krateDerived(() => expr)`) evaluates to; any other expression is returned
// as is. A function value yields nil.
func unwrapDerived(e ast.Expr) ast.Expr {
	switch v := e.(type) {
	case *ast.ArrowFn:
		return nil
	case *ast.CallExpr:
		if id, ok := v.Callee.(*ast.Identifier); ok && id.Name == "krateDerived" && len(v.Args) == 1 {
			if arrow, ok := v.Args[0].(*ast.ArrowFn); ok {
				return arrowBodyExpr(arrow)
			}
		}
	}
	return e
}

// componentTableMembers returns, in source order, the known components that
// the module-level constants read by expr hold as values.
func (b *builder) componentTableMembers(expr ast.Expr) []string {
	var tables []string
	seenTable := map[string]bool{}
	WalkAST(expr, func(n any) bool {
		if id, ok := n.(*ast.Identifier); ok && !seenTable[id.Name] {
			if _, mod := b.moduleConstInits[id.Name]; mod {
				seenTable[id.Name] = true
				tables = append(tables, id.Name)
			}
		}
		return true
	})
	sort.Strings(tables)
	var out []string
	seen := map[string]bool{}
	for _, t := range tables {
		init := b.moduleConstInits[t]
		if _, isFn := init.(*ast.ArrowFn); isFn {
			continue
		}
		WalkAST(init, func(n any) bool {
			switch v := n.(type) {
			case *ast.ArrowFn, *ast.FnDecl:
				return false
			case *ast.Identifier:
				nm := v.Name
				if nm != "" && nm[0] >= 'A' && nm[0] <= 'Z' && !seen[nm] && b.ann.Functions[nm] != nil && len(out) < maxDynamicTagCandidates {
					seen[nm] = true
					out = append(out, nm)
				}
			}
			return true
		})
	}
	return out
}
