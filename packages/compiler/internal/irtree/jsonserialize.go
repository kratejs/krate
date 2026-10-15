package irtree

import (
	"encoding/json"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// SerializeJSON renders a const expression as a JSON value for a Go component
// prop. Literals, arrays, and objects are serialized structurally so numeric,
// boolean, array, and object props decode into the right Go types. Anything else
// (identifiers, member/call expressions) is resolved with `resolve`; a resolved
// value that is already valid JSON is embedded as-is, otherwise it becomes a
// JSON string. Returns "null" when nothing can be resolved.
func SerializeJSON(expr ast.Expr, resolve func(ast.Expr) (string, bool)) string {
	switch e := expr.(type) {
	case *ast.Literal:
		switch e.Kind {
		case ast.StringLit, ast.RegexpLit:
			b, _ := json.Marshal(e.Value)
			return string(b)
		case ast.NullLit:
			return "null"
		case ast.BoolLit, ast.NumberLit:
			return e.Value
		default:
			b, _ := json.Marshal(e.Value)
			return string(b)
		}
	case *ast.ArrayExpr:
		parts := make([]string, 0, len(e.Elements))
		for _, el := range e.Elements {
			parts = append(parts, SerializeJSON(el, resolve))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case *ast.ObjectExpr:
		parts := make([]string, 0, len(e.Properties))
		for _, p := range e.Properties {
			if p == nil || p.Spread || p.Key == "" || p.Value == nil {
				continue
			}
			key, _ := json.Marshal(p.Key)
			parts = append(parts, string(key)+":"+SerializeJSON(p.Value, resolve))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case *ast.UnaryExpr:
		if e.Op == "-" || e.Op == "+" {
			return e.Op + SerializeJSON(e.Arg, resolve)
		}
	}
	if resolve != nil {
		if v, ok := resolve(expr); ok && v != "" {
			if json.Valid([]byte(v)) {
				return v
			}
			b, _ := json.Marshal(v)
			return string(b)
		}
	}
	return "null"
}

// IsSpreadProp reports whether an attribute is a spread (`{...obj}`). Used to
// reject unsupported spreads on Go components with a clear error.
func IsSpreadProp(attr *ast.JSXAttr) bool {
	return attr != nil && attr.Spread
}
