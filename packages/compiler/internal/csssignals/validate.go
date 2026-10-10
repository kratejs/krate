package csssignals

import "github.com/kratejs/krate/packages/compiler/ast"

// labelableTags are the tags a trigger may be rewritten into (<label>).
var labelableTags = map[string]bool{
	"button": true, "label": true, "a": true, "span": true, "li": true, "div": true,
}

// CanBeLabel reports whether a tag can be rewritten into a <label>.
func CanBeLabel(tag string) bool { return labelableTags[tag] }

// isOnEvent reports whether a JSX attribute name is an event handler.
func isOnEvent(name string) bool {
	return len(name) > 2 && name[0] == 'o' && name[1] == 'n' &&
		name[2] >= 'A' && name[2] <= 'Z'
}

// isIntrinsic reports whether a JSX tag name is a lowercase HTML element.
func isIntrinsic(name string) bool {
	return name != "" && name[0] >= 'a' && name[0] <= 'z'
}

// validate returns the first reason the component cannot be compiled, or "".
// The rules:
//   - State may be READ only as a panel condition inside showIf/visibleIf.
//   - State may be WRITTEN only from an onClick trigger calling the setter.
//   - A trigger must be a labelable element with a static class.
//   - A panel/scope anchor must have a static class.
//
// - Anything else (text, dynamic attribute, nested function, ...) is rejected.
func (a *Analyzer) validate(body []ast.Stmt) string {
	names := a.names()
	ret := findReturnStmt(body)
	if ret == nil || ret.Value == nil {
		return "component has no return statement"
	}
	if reason := a.validateJSXValue(ret.Value, names); reason != "" {
		return reason
	}
	return a.validateStmts(body, ret, names)
}

func (a *Analyzer) validateJSXValue(expr ast.Expr, names map[string]bool) string {
	switch e := expr.(type) {
	case *ast.JSXElement:
		return a.validateJSX(e, names)
	case *ast.JSXFragment:
		return a.validateFragment(e, names)
	case *ast.TypeAssertion:
		return a.validateJSXValue(e.Expr, names)
	}
	return "component return value must be JSX"
}

// validateJSX checks one element: it may be a trigger, a panel, or neither, and
// must not use the state anywhere else.
func (a *Analyzer) validateJSX(el *ast.JSXElement, names map[string]bool) string {
	if el == nil || el.Opening == nil {
		return ""
	}
	if !isIntrinsic(el.Opening.Name) {
		if elementReferencesCSS(el, names) {
			return "CSS state is used on a component element (" + el.Opening.Name + "); use a native HTML element"
		}
		return ""
	}

	_, isTrigger := a.MatchTrigger(el)
	_, isPanel := a.ParsePanel(el)

	if isTrigger {
		if !CanBeLabel(el.Opening.Name) {
			return "trigger <" + el.Opening.Name + "> cannot become a <label>; use <button> (or switch to createSignal)"
		}
		if !hasStaticClass(el) {
			return "trigger <" + el.Opening.Name + "> has a dynamic class; use a static string class (or switch to createSignal)"
		}
	} else if isPanel {
		if !hasStaticClass(el) {
			return "panel <" + el.Opening.Name + "> has a dynamic class; use a static string class (or switch to createSignal)"
		}
	}
	// A dynamic class on the component root is fine: the scope class is then
	// applied to a display:contents wrapper by the builder. Only triggers and
	// panels (which need the class inline) require a static class.

	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil || attr.Spread {
			continue
		}
		if isOnEvent(attr.Name) {
			if isTrigger {
				continue
			}
			if referencesCSS(attr.Value, names) {
				return "the setter is called outside a recognised `() => set(...)` handler"
			}
			continue
		}
		if attr.Name == "showIf" || attr.Name == "visibleIf" {
			if isPanel {
				continue
			}
			if referencesCSS(attr.Value, names) {
				return "the state is used in showIf without a supported condition (combine get()==='x', get(), or flags.x() with &&, ||, !, ===/!==)"
			}
			continue
		}
		if referencesCSS(attr.Value, names) {
			return "the state is used in a dynamic attribute; only showIf panels and onClick triggers are supported"
		}
	}

	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			if reason := a.validateJSX(c.Element, names); reason != "" {
				return reason
			}
		case *ast.JSXFragmentChild:
			if reason := a.validateFragment(c.Fragment, names); reason != "" {
				return reason
			}
		case *ast.JSXExprContainer:
			if _, ok := a.MatchText(c.Expression); ok {
				continue // live text: driven by --krate-current, zero JS
			}
			if referencesCSS(c.Expression, names) {
				return "the state is rendered as text; only showIf panels and onClick triggers are supported"
			}
		}
	}
	return ""
}

func (a *Analyzer) validateFragment(frag *ast.JSXFragment, names map[string]bool) string {
	if frag == nil {
		return ""
	}
	for _, child := range frag.Children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			if reason := a.validateJSX(c.Element, names); reason != "" {
				return reason
			}
		case *ast.JSXFragmentChild:
			if reason := a.validateFragment(c.Fragment, names); reason != "" {
				return reason
			}
		case *ast.JSXExprContainer:
			if _, ok := a.MatchText(c.Expression); ok {
				continue // live text
			}
			if referencesCSS(c.Expression, names) {
				return "the state is rendered as text; only showIf panels and onClick triggers are supported"
			}
		}
	}
	return ""
}

// hasStaticClass reports whether the element's class (if present) is a static
// string literal.
func hasStaticClass(el *ast.JSXElement) bool {
	for _, attr := range el.Opening.Attributes {
		if attr == nil || (attr.Name != "class" && attr.Name != "className") {
			continue
		}
		if attr.Value == nil {
			return true
		}
		lit, ok := attr.Value.(*ast.Literal)
		return ok && lit.Kind == ast.StringLit
	}
	return true
}

// validateStmts rejects any use of the state outside the return JSX and the
// declarations.
func (a *Analyzer) validateStmts(stmts []ast.Stmt, skip *ast.ReturnStmt, names map[string]bool) string {
	for _, stmt := range stmts {
		if stmt == nil || stmt == ast.Stmt(skip) {
			continue
		}
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d.Init != nil {
					if call, ok := d.Init.(*ast.CallExpr); ok {
						if id, ok := call.Callee.(*ast.Identifier); ok && isCSSFactory(id.Name) {
							continue
						}
					}
				}
				if referencesCSS(d.Init, names) {
					return "the state is assigned to a variable; only showIf panels and onClick triggers are supported"
				}
			}
		case *ast.ReturnStmt:
			if referencesCSS(s.Value, names) {
				return "the state is returned outside the main JSX"
			}
		case *ast.ExprStmt:
			if referencesCSS(s.Expression, names) {
				return "the state is used in a statement; only showIf panels and onClick triggers are supported"
			}
		case *ast.IfStmt:
			if referencesCSS(s.Test, names) {
				return "the state is used in a condition outside showIf"
			}
			if reason := a.validateStmts(s.Consequent, skip, names); reason != "" {
				return reason
			}
			if reason := a.validateStmts(s.Alternate, skip, names); reason != "" {
				return reason
			}
		case *ast.BlockStmt:
			if reason := a.validateStmts(s.Body, skip, names); reason != "" {
				return reason
			}
		case *ast.ForStmt:
			if referencesCSS(s.Test, names) || referencesCSS(s.Update, names) {
				return "the state is used in a loop header outside showIf"
			}
			if reason := a.validateStmts(s.Body, skip, names); reason != "" {
				return reason
			}
		case *ast.ForInStmt:
			if referencesCSS(s.Right, names) {
				return "the state is used in a loop header outside showIf"
			}
			if reason := a.validateStmts(s.Body, skip, names); reason != "" {
				return reason
			}
		case *ast.WhileStmt:
			if referencesCSS(s.Test, names) {
				return "the state is used in a loop header outside showIf"
			}
			if reason := a.validateStmts(s.Body, skip, names); reason != "" {
				return reason
			}
		case *ast.DoWhileStmt:
			if reason := a.validateStmts(s.Body, skip, names); reason != "" {
				return reason
			}
		case *ast.SwitchStmt:
			if referencesCSS(s.Discriminant, names) {
				return "the state is used in a switch outside showIf"
			}
			for _, c := range s.Cases {
				if reason := a.validateStmts(c.Body, skip, names); reason != "" {
					return reason
				}
			}
		case *ast.TryStmt:
			if reason := a.validateStmts(s.Body, skip, names); reason != "" {
				return reason
			}
			if s.Catch != nil {
				if reason := a.validateStmts(s.Catch.Body, skip, names); reason != "" {
					return reason
				}
			}
			if reason := a.validateStmts(s.Finally, skip, names); reason != "" {
				return reason
			}
		case *ast.FnDecl:
			if referencesCSS(&ast.Identifier{Name: s.Name}, names) {
				return "the state is captured by a nested function"
			}
			if reason := a.validateStmts(s.Body, skip, names); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// isCSSFactory reports whether a callee name is one of the CSS signal factories.
func isCSSFactory(name string) bool {
	switch name {
	case "createCSSChoice", "createCSSToggle", "createCSSFlags",
		"createCSSGroup", "createCSSRange", "createCSSStack":
		return true
	}
	return false
}

// elementReferencesCSS reports whether a component element mentions any
// choice name (it cannot be rewritten).
func elementReferencesCSS(el *ast.JSXElement, names map[string]bool) bool {
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil {
			continue
		}
		if referencesCSS(attr.Value, names) {
			return true
		}
	}
	return false
}

func findReturnStmt(body []ast.Stmt) *ast.ReturnStmt {
	for _, stmt := range body {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			return ret
		}
	}
	return nil
}
