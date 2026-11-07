package irtree

import (
	"regexp"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// buildTimeScope resolves expr to a CSS scope handle that has no run-time
// value: the handle form (`const tab = createCSSChoice(...)`). The getter of
// the destructured form stays a run-time function and is not matched.
func (b *builder) buildTimeScope(expr ast.Expr) (scopeRef, bool) {
	ref, ok := b.resolveScopeExpr(expr)
	if !ok || ref.scope == nil || ref.scope.Setter != "" {
		return scopeRef{}, false
	}
	return ref, true
}

// scopePropLocals returns the names a child component binds a prop to: the
// member of its props parameter, or the local of a destructured parameter.
func scopePropLocals(fn *ast.FnDecl, prop string) (propsParam string, locals []string) {
	if fn == nil || len(fn.Params) == 0 || fn.Params[0] == nil {
		return "", nil
	}
	p := fn.Params[0]
	if p.Pattern == "" {
		return plainPropsParamName(fn), nil
	}
	re := regexp.MustCompile(`(?:^|[{,\s])` + regexp.QuoteMeta(prop) + `\s*(?::\s*([A-Za-z_$][\w$]*))?\s*(?:=|,|}|$)`)
	if m := re.FindStringSubmatch(p.Pattern); m != nil {
		if m[1] != "" {
			return "", []string{m[1]}
		}
		return "", []string{prop}
	}
	return "", nil
}

// codeReadsProp reports whether JS source reads prop through the props
// parameter (`props.x`, `props["x"]`) or a local bound to it.
func codeReadsProp(code, propsParam string, locals []string, prop string) bool {
	if code == "" {
		return false
	}
	names := append([]string{"props"}, propsParam)
	for _, n := range names {
		if n == "" {
			continue
		}
		q := regexp.QuoteMeta(n)
		re := regexp.MustCompile(`\b` + q + `\s*(?:\?\.|\.)\s*` + regexp.QuoteMeta(prop) + `\b|\b` + q + `\s*\[\s*["']` + regexp.QuoteMeta(prop) + `["']\s*\]`)
		if re.MatchString(code) {
			return true
		}
	}
	for _, l := range locals {
		if codeContainsIdent(code, l) {
			return true
		}
	}
	return false
}

// checkScopePropReads fails the build when a child's hydration code reads a
// prop that holds a CSS scope handle. The handle exists only at build time
// (its helpers were expanded into markup and CSS), so the run-time read would
// throw. The registry entry for such a prop is left out.
func (b *builder) checkScopePropReads(el *ast.JSXElement, childFn *ast.FnDecl, childNode *ComponentNode, attrs map[string]ast.Expr) {
	if childNode == nil || childFn == nil || b.locateOnly > 0 {
		return
	}
	for _, name := range sortedKeysExpr(attrs) {
		if _, ok := b.buildTimeScope(attrs[name]); !ok {
			continue
		}
		propsParam, locals := scopePropLocals(childFn, name)
		if !hydrationCodeReads(childNode, func(code string) bool { return codeReadsProp(code, propsParam, locals, name) }) {
			continue
		}
		b.errorCodeAt(el.Position, diag.CodeScopeHandleAtRuntime,
			"<"+el.Opening.Name+"> reads the CSS scope handle passed as `"+name+"` in code that runs in the browser, where the handle does not exist",
			"use "+name+".trigger(), .panel(), .close() or .current() in <"+el.Opening.Name+">'s JSX, or pass the getter and setter values to a component with createSignal state")
	}
}

// checkCSSSignalPropReads fails the build when a CSS signal getter or setter
// is passed to a component that is rendered at build time and reads it.
func (b *builder) checkCSSSignalPropReads(el *ast.JSXElement, childFn *ast.FnDecl, attrs map[string]ast.Expr) {
	if b.cssSignals == nil || childFn == nil || b.locateOnly > 0 {
		return
	}
	for _, name := range sortedKeysExpr(attrs) {
		id, ok := attrs[name].(*ast.Identifier)
		if !ok || !b.isCSSSignalName(id.Name) {
			continue
		}
		propsParam, locals := scopePropLocals(childFn, name)
		if !fnReadsProp(childFn, name, propsParam, locals) {
			continue
		}
		b.errorCodeAt(el.Position, diag.CodeCSSSignalInBuildTimeComponent,
			"<"+el.Opening.Name+"> renders at build time but reads `"+name+"`, the CSS signal `"+id.Name+"`, whose value only exists in the page",
			"pass the scope handle instead (`const tab = createCSSChoice(...)` without destructuring, then `<"+el.Opening.Name+" scope={tab} />`) and use `props.scope.current()`, `.panel()` or `.trigger()` in <"+el.Opening.Name+">, or give it createSignal state")
	}
}

// isCSSSignalName reports whether name is the getter or setter of a CSS
// signal declared by the component being built.
func (b *builder) isCSSSignalName(name string) bool {
	if b.cssSignals == nil {
		return false
	}
	for _, sc := range b.cssSignals.Scopes() {
		if sc.Var == name || sc.Setter == name {
			return true
		}
	}
	return false
}

// fnReadsProp reports whether a component body reads prop, through its props
// parameter or the local a destructured parameter binds it to.
func fnReadsProp(fn *ast.FnDecl, prop, propsParam string, locals []string) bool {
	found := false
	WalkAST(fn.Body, func(n any) bool {
		if found {
			return false
		}
		switch e := n.(type) {
		case *ast.MemberExpr:
			if id, ok := e.Object.(*ast.Identifier); ok && propsParam != "" && id.Name == propsParam {
				switch p := e.Property.(type) {
				case *ast.Identifier:
					if !e.Computed && p.Name == prop {
						found = true
					}
				case *ast.Literal:
					if p.Kind == ast.StringLit && p.Value == prop {
						found = true
					}
				}
			}
		case *ast.Identifier:
			for _, l := range locals {
				if e.Name == l {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// checkIslandScopeProps fails the build when a CSS scope handle is passed to
// an external island: the island runs in the browser, where only build-time
// helpers could use it.
func (b *builder) checkIslandScopeProps(el *ast.JSXElement, name string) {
	if el == nil || el.Opening == nil || b.locateOnly > 0 {
		return
	}
	for _, a := range el.Opening.Attributes {
		if a == nil || a.Spread || a.Value == nil {
			continue
		}
		if _, ok := b.buildTimeScope(a.Value); !ok {
			continue
		}
		b.errorCodeAt(el.Position, diag.CodeScopeHandleAtRuntime,
			"<"+name+"> is an external island and receives the CSS scope handle `"+a.Name+"`, which exists only at build time",
			"render the triggers and panels in Krate components and pass the island plain values, or use createSignal for state shared with the island")
	}
}
