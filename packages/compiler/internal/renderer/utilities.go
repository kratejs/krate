package renderer

import (
	"strconv"
	"strings"

	"krate-compiler/internal/ast"
	"krate-compiler/internal/escape"
)

// itoa converts an int to its decimal string representation without allocating fmt.Sprintf.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// stripArraySep removes the internal \x1f array-item separator. SSREval joins
// array elements (e.g. .map() results, array literals) with \x1f; any write
// path that bypasses escape.HTML (raw HTML-producing output) must strip it too.
func stripArraySep(s string) string {
	return strings.ReplaceAll(s, "\x1f", "")
}

// ─── JS string escaping ──────────────────────────────────────────────────────

// escapeJSString escapes a string for safe embedding in a JS single-quoted string literal.
func escapeJSString(s string) string {
	return escape.JSString(s)
}

// ─── AST helpers ─────────────────────────────────────────────────────────────

// stringLiteralValue returns the raw value string from a Literal AST node.
func stringLiteralValue(e *ast.Literal) string {
	return e.Value
}

// arrowBodyExpr extracts the expression body from an arrow function.
// Works for both expression arrows (x => expr) and block arrows with a return.
func arrowBodyExpr(arrow *ast.ArrowFn) ast.Expr {
	if arrow.Expression {
		for _, s := range arrow.Body {
			if ret, ok := s.(*ast.ReturnStmt); ok && ret.Value != nil {
				return ret.Value
			}
			if exprStmt, ok := s.(*ast.ExprStmt); ok {
				return exprStmt.Expression
			}
		}
	}
	for _, s := range arrow.Body {
		if ret, ok := s.(*ast.ReturnStmt); ok && ret.Value != nil {
			return ret.Value
		}
	}
	return nil
}

// ─── JSON helpers ────────────────────────────────────────────────────────────

// extractJSONProp extracts a property value from a JSON-like object string.
// e.g., extractJSONProp(`{"count":42,"items":[1,2,3]}`, "count") → "42"
func extractJSONProp(objStr, prop string) string {
	if objStr == "" || objStr[0] != '{' {
		return ""
	}
	// Try quoted key format: "prop":
	quotedKey := `"` + prop + `":`
	idx := strings.Index(objStr, quotedKey)
	if idx < 0 {
		// Try unquoted key format: prop:
		quotedKey = prop + ":"
		idx = strings.Index(objStr, quotedKey)
	}
	if idx < 0 {
		return ""
	}
	valStart := idx + len(quotedKey)
	if valStart >= len(objStr) {
		return ""
	}
	// Extract value: skip whitespace
	i := valStart
	for i < len(objStr) && objStr[i] == ' ' {
		i++
	}
	if i >= len(objStr) {
		return ""
	}
	// Find end of value
	depth := 0
	inStr := false
	for j := i; j < len(objStr); j++ {
		ch := objStr[j]
		if inStr {
			if ch == '\\' {
				j++
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		if ch == '"' {
			inStr = true
			continue
		}
		if ch == '{' || ch == '[' {
			depth++
		} else if ch == '}' || ch == ']' {
			if depth == 0 {
				return objStr[i:j]
			}
			depth--
		} else if ch == ',' && depth == 0 {
			return objStr[i:j]
		}
	}
	return objStr[i:]
}

// ─── Number helpers ──────────────────────────────────────────────────────────

// toFloat attempts to parse a string as a float64.
func toFloat(s string) float64 {
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// ─── AST analysis helpers (used by tests and IR tree) ────────────────────────

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

// isVoidElement returns true for HTML void elements that can be self-closing.
// Non-void elements like <div/> must be emitted as <div></div>.
func isVoidElement(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

// isBooleanAttr reports whether an HTML attribute is a boolean attribute whose
// mere presence is meaningful — an empty or "false" string value is still truthy.
func isBooleanAttr(name string) bool {
	switch name {
	case "disabled", "required", "readonly", "checked", "selected", "multiple", "autofocus", "hidden", "inert", "novalidate", "open", "async", "defer", "autoplay", "controls", "loop", "muted", "playsinline", "allowfullscreen", "default", "ismap", "itemscope", "nohref", "noresize", "noshade", "nowrap", "reversed", "scoped", "seamless", "sortable", "translate":
		return true
	}
	return false
}

// mangleIdentInString replaces occurrences of `from` with `to` in `src`, skipping string literals.
func mangleIdentInString(src, from, to string) string {
	var b strings.Builder
	b.Grow(len(src))
	i := 0
	for i < len(src) {
		if isInsideString(src, i) {
			b.WriteByte(src[i])
			i++
			continue
		}
		if i+len(from) <= len(src) && src[i:i+len(from)] == from {
			b.WriteString(to)
			i += len(from)
			continue
		}
		b.WriteByte(src[i])
		i++
	}
	return b.String()
}

// isInsideString checks if position `pos` is inside a string literal in `src`.
func isInsideString(src string, pos int) bool {
	if pos >= len(src) {
		return false
	}
	inString := false
	strChar := byte(0)
	templateDepth := 0
	for i := 0; i < pos; i++ {
		ch := src[i]
		if inString {
			if ch == '\\' {
				i++
				continue
			}
			if ch == '`' {
				templateDepth--
				if templateDepth == 0 {
					inString = false
				}
				continue
			}
			if ch == strChar {
				inString = false
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = true
			strChar = ch
		} else if ch == '`' {
			inString = true
			strChar = '`'
			templateDepth++
		}
	}
	return inString && templateDepth == 0
}
