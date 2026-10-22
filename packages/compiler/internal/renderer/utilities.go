package renderer

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
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

// JS string escaping

// escapeJSString escapes a string for safe embedding in a JS single-quoted string literal.
func escapeJSString(s string) string {
	return escape.JSString(s)
}

// AST helpers

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

// JSON helpers

// extractJSONProp extracts a top-level property value from a JSON-like
// object string. e.g. extractJSONProp(`{"count":42,"items":[1,2,3]}`, "count")
// returns "42". Valid JSON is decoded with encoding/json; JS object-literal
// source (unquoted keys, single-quoted strings, produced by the const
// serializer) is scanned structurally, so keys are only matched at the top
// level and never inside nested objects or string values. String values are
// returned unquoted; other values are returned as source text.
func extractJSONProp(objStr, prop string) string {
	objStr = strings.TrimSpace(objStr)
	if objStr == "" || objStr[0] != '{' {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(objStr), &obj); err == nil {
		raw, ok := obj[prop]
		if !ok {
			return ""
		}
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			return str
		}
		return string(raw)
	}
	if v, ok := scanObjectProp(objStr, prop); ok {
		return unquoteJSONPropValue(v)
	}
	return ""
}

// scanObjectProp finds the source text of a top-level property value in a JS
// object literal, honoring nesting and string quoting.
func scanObjectProp(src, prop string) (string, bool) {
	n := len(src)
	i := 1
	skipSpace := func() {
		for i < n && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
			i++
		}
	}
	for i < n {
		skipSpace()
		if i >= n || src[i] == '}' {
			return "", false
		}
		// Key: quoted string or bare identifier/number.
		var key string
		if src[i] == '"' || src[i] == '\'' {
			q := src[i]
			j := i + 1
			for j < n && src[j] != q {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j >= n {
				return "", false
			}
			key = unquoteJSONPropValue(src[i : j+1])
			i = j + 1
		} else {
			j := i
			for j < n && src[j] != ':' && src[j] != ',' && src[j] != '}' && src[j] != ' ' {
				j++
			}
			key = src[i:j]
			i = j
		}
		skipSpace()
		if i >= n || src[i] != ':' {
			// Shorthand or malformed entry: skip to the next top-level comma.
			if i < n && src[i] == ',' {
				i++
				continue
			}
			return "", false
		}
		i++
		skipSpace()
		start := i
		depth := 0
		quote := byte(0)
		for i < n {
			ch := src[i]
			if quote != 0 {
				if ch == '\\' {
					i += 2
					continue
				}
				if ch == quote {
					quote = 0
				}
				i++
				continue
			}
			if ch == '"' || ch == '\'' || ch == '`' {
				quote = ch
			} else if ch == '{' || ch == '[' || ch == '(' {
				depth++
			} else if ch == '}' || ch == ']' || ch == ')' {
				if depth == 0 {
					break
				}
				depth--
			} else if ch == ',' && depth == 0 {
				break
			}
			i++
		}
		if key == prop {
			return strings.TrimSpace(src[start:min(i, n)]), true
		}
		if i < n && src[i] == ',' {
			i++
		}
	}
	return "", false
}

// unquoteJSONPropValue normalizes an extracted JSON property value. Numeric,
// boolean, and null values pass through as-is; JSON string values have their
// surrounding quotes removed (and escapes resolved) so they render as plain
// text in SSR output (e.g. params.id -> abc-123 rather than "abc-123").
func unquoteJSONPropValue(v string) string {
	if len(v) >= 2 {
		var q byte
		switch v[0] {
		case '"':
			q = '"'
		case '\'':
			q = '\''
		}
		if q != 0 && v[len(v)-1] == q {
			var sb strings.Builder
			for i := 1; i < len(v)-1; i++ {
				c := v[i]
				if c == '\\' && i+1 < len(v)-1 {
					i++
					next := v[i]
					switch next {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case 'r':
						sb.WriteByte('\r')
					case '\\', '"', '\'', '/':
						sb.WriteByte(next)
					case 'u':
						// Skip \uXXXX sequences (rare in params); leave as-is marker.
						sb.WriteString("\\u")
						if i+4 < len(v) {
							sb.WriteString(v[i+1 : i+5])
							i += 4
						}
					default:
						sb.WriteByte(next)
					}
					continue
				}
				sb.WriteByte(c)
			}
			return sb.String()
		}
	}
	return v
}

// Number helpers

// toFloat attempts to parse a string as a float64.
func toFloat(s string) float64 {
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

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

// mangleIdentInString replaces occurrences of `from` with `to` in `src`,
// skipping single- and double-quoted string literals. Template literals are
// not skipped (their ${} interpolations hold code). Runs in one linear pass.
func mangleIdentInString(src, from, to string) string {
	if from == "" {
		return src
	}
	var b strings.Builder
	b.Grow(len(src))
	quote := byte(0)
	for i := 0; i < len(src); {
		ch := src[i]
		if quote != 0 {
			b.WriteByte(ch)
			i++
			if ch == '\\' && i < len(src) {
				b.WriteByte(src[i])
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			b.WriteByte(ch)
			i++
			continue
		}
		if strings.HasPrefix(src[i:], from) {
			b.WriteString(to)
			i += len(from)
			continue
		}
		b.WriteByte(ch)
		i++
	}
	return b.String()
}
