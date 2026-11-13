package renderer

import (
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// itoa converts an int to its decimal string representation (negative values
// included).
func itoa(i int) string {
	return strconv.Itoa(i)
}

// JS string escaping

// escapeJSString escapes a string for safe embedding in a JS single-quoted string literal.
func escapeJSString(s string) string {
	return escape.JSString(s)
}

// Number helpers

// AST analysis helpers (used by tests and IR tree)

// hasJSX returns true if the expression tree contains a JSX element.
func hasJSX(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.JSXElement:
		return true
	case *ast.JSXFragment:
		return true
	case *ast.BinaryExpr:
		return hasJSX(e.Left) || hasJSX(e.Right)
	case *ast.ConditionalExpr:
		return hasJSX(e.Test) || hasJSX(e.Consequent) || hasJSX(e.Alternate)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			if hasJSX(arg) {
				return true
			}
		}
	}
	return false
}
