package jsvalue

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// ErrObjectChild is returned when a plain object is rendered as a JSX child
// (React throws "Objects are not valid as a React child").
var ErrObjectChild = errors.New("objects are not valid as a JSX child")

// UnknownChildError is returned by RenderChild for a value (or array element)
// that is not statically known.
type UnknownChildError struct{ Value Value }

func (e *UnknownChildError) Error() string {
	if e.Value.IsUnsupported() {
		return "unsupported expression: " + e.Value.Reason()
	}
	return "value is not known at build time: " + e.Value.Reason()
}

// RenderChild renders a value in JSX child position with React semantics:
// null, undefined and booleans render nothing; strings and numbers render as
// escaped text; arrays render each element in order; JSX values are inserted
// as markup. Plain objects are an error, functions render nothing.
func RenderChild(v Value) (string, error) {
	var b strings.Builder
	if err := renderChild(&b, v); err != nil {
		return b.String(), err
	}
	return b.String(), nil
}

func renderChild(b *strings.Builder, v Value) error {
	switch v.kind {
	case KindUndefined, KindNull, KindBool, KindFunction:
		return nil
	case KindNumber:
		b.WriteString(NumberToString(v.n))
	case KindString:
		b.WriteString(escape.HTML(v.s))
	case KindJSX:
		b.WriteString(v.s)
	case KindArray:
		for _, e := range v.Elems() {
			if err := renderChild(b, e); err != nil {
				return err
			}
		}
	case KindObject:
		return ErrObjectChild
	default:
		return &UnknownChildError{Value: v}
	}
	return nil
}

// RenderRawChild is RenderChild without HTML escaping of text, used for
// content captured into raw contexts (inline <script>/<style> bodies).
func RenderRawChild(v Value) string {
	switch v.kind {
	case KindNumber:
		return NumberToString(v.n)
	case KindString:
		return v.s
	case KindJSX:
		return v.s
	case KindArray:
		var b strings.Builder
		for _, e := range v.Elems() {
			b.WriteString(RenderRawChild(e))
		}
		return b.String()
	}
	return ""
}

// JSSource renders a statically-known value as JavaScript source (strings
// single-quoted). ok=false for JSX, functions and Unknown values.
func JSSource(v Value) (string, bool) {
	var b strings.Builder
	if !writeJSSource(&b, v) {
		return "", false
	}
	return b.String(), true
}

func writeJSSource(b *strings.Builder, v Value) bool {
	switch v.kind {
	case KindUndefined:
		b.WriteString("undefined")
	case KindNull:
		b.WriteString("null")
	case KindBool:
		b.WriteString(v.ToString())
	case KindNumber:
		if v.n == 0 && math.Signbit(v.n) {
			b.WriteString("-0")
		} else {
			b.WriteString(NumberToString(v.n))
		}
	case KindString:
		b.WriteByte('\'')
		b.WriteString(escape.JSString(v.s))
		b.WriteByte('\'')
	case KindArray:
		b.WriteByte('[')
		for i, e := range v.Elems() {
			if i > 0 {
				b.WriteByte(',')
			}
			if !writeJSSource(b, e) {
				return false
			}
		}
		b.WriteByte(']')
	case KindObject:
		b.WriteByte('{')
		for i, k := range v.Keys() {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(ObjectKeySource(k))
			b.WriteByte(':')
			p, _ := v.Get(k)
			if !writeJSSource(b, p) {
				return false
			}
		}
		b.WriteByte('}')
	default:
		return false
	}
	return true
}

// ObjectKeySource renders an object key, quoting it unless it is a valid
// identifier or array index.
func ObjectKeySource(k string) string {
	if isIdent(k) || isArrayIndex(k) {
		return k
	}
	return "'" + escape.JSString(k) + "'"
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// JSONStringify implements JSON.stringify(v) without replacer/indent.
// ok=false when the result is undefined (top-level undefined or function) or
// the value contains Unknown parts.
func JSONStringify(v Value) (string, bool) {
	var b strings.Builder
	switch r := writeJSON(&b, v); r {
	case jsonOK:
		return b.String(), true
	}
	return "", false
}

type jsonResult int

const (
	jsonOK jsonResult = iota
	jsonSkip
	jsonFail
)

func writeJSON(b *strings.Builder, v Value) jsonResult {
	switch v.kind {
	case KindUndefined, KindFunction:
		return jsonSkip
	case KindNull:
		b.WriteString("null")
	case KindBool:
		b.WriteString(v.ToString())
	case KindNumber:
		if math.IsNaN(v.n) || math.IsInf(v.n, 0) {
			b.WriteString("null")
		} else {
			b.WriteString(NumberToString(v.n))
		}
	case KindString:
		writeJSONString(b, v.s)
	case KindArray:
		b.WriteByte('[')
		for i, e := range v.Elems() {
			if i > 0 {
				b.WriteByte(',')
			}
			var eb strings.Builder
			switch writeJSON(&eb, e) {
			case jsonFail:
				return jsonFail
			case jsonSkip:
				b.WriteString("null")
			default:
				b.WriteString(eb.String())
			}
		}
		b.WriteByte(']')
	case KindObject:
		b.WriteByte('{')
		first := true
		for _, k := range v.Keys() {
			p, _ := v.Get(k)
			var pb strings.Builder
			switch writeJSON(&pb, p) {
			case jsonFail:
				return jsonFail
			case jsonSkip:
				continue
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			writeJSONString(b, k)
			b.WriteByte(':')
			b.WriteString(pb.String())
		}
		b.WriteByte('}')
	case KindJSX:
		// A React element serializes as its (opaque) object form; there is no
		// faithful build-time equivalent.
		return jsonFail
	default:
		return jsonFail
	}
	return jsonOK
}

func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// ToAST converts a statically-known value back to a literal AST expression
// (nil for JSX, functions and Unknown values).
func ToAST(v Value) ast.Expr {
	switch v.kind {
	case KindUndefined:
		return &ast.Identifier{Name: "undefined"}
	case KindNull:
		return &ast.Literal{Kind: ast.NullLit, Value: "null"}
	case KindBool:
		return &ast.Literal{Kind: ast.BoolLit, Value: v.ToString()}
	case KindNumber:
		switch {
		case math.IsNaN(v.n):
			return &ast.Identifier{Name: "NaN"}
		case math.IsInf(v.n, 1):
			return &ast.Identifier{Name: "Infinity"}
		case math.IsInf(v.n, -1):
			return &ast.UnaryExpr{Op: "-", Arg: &ast.Identifier{Name: "Infinity"}}
		case v.n < 0 || (v.n == 0 && math.Signbit(v.n)):
			return &ast.UnaryExpr{Op: "-", Arg: &ast.Literal{Kind: ast.NumberLit, Value: NumberToString(-v.n)}}
		}
		return &ast.Literal{Kind: ast.NumberLit, Value: NumberToString(v.n)}
	case KindString:
		return &ast.Literal{Kind: ast.StringLit, Value: v.s}
	case KindArray:
		elems := make([]ast.Expr, 0, len(v.Elems()))
		for _, e := range v.Elems() {
			x := ToAST(e)
			if x == nil {
				return nil
			}
			elems = append(elems, x)
		}
		return &ast.ArrayExpr{Elements: elems}
	case KindObject:
		props := make([]*ast.ObjectProp, 0, len(v.obj.keys))
		for _, k := range v.Keys() {
			p, _ := v.Get(k)
			x := ToAST(p)
			if x == nil {
				return nil
			}
			props = append(props, &ast.ObjectProp{Key: k, Value: x})
		}
		return &ast.ObjectExpr{Properties: props}
	}
	return nil
}

// FromLiteralAST converts a pure literal AST (literals, arrays, objects, unary
// minus, template literals without substitutions) to a Value. ok=false for
// anything that needs evaluation context.
func FromLiteralAST(expr ast.Expr) (Value, bool) {
	in := &Interp{}
	v := in.Eval(expr)
	if v.ContainsUnknown() || v.kind == KindFunction || v.kind == KindJSX {
		return Value{}, false
	}
	return v, true
}

// LegacyString renders a value in the compiler's historical string encoding:
// strings bare, numbers per JS, booleans/null/undefined as their keyword,
// arrays/objects as JS source, JSX as its markup, Unknown as "". It exists only
// for interop with string-typed APIs (attribute emission, JS codegen inputs).
func LegacyString(v Value) string {
	switch v.kind {
	case KindUnknown:
		return ""
	case KindString:
		return v.s
	case KindJSX:
		return v.s
	case KindArray, KindObject:
		if src, ok := JSSource(v); ok {
			return src
		}
		return ""
	case KindFunction:
		return ""
	}
	return v.ToString()
}

// AttrString converts a value to the string an HTML attribute carries, per
// React: strings as-is, numbers formatted per JS, booleans as "true"/"false",
// arrays/objects via ToString. ok=false when React omits the attribute
// (null, undefined, functions) or the value is Unknown.
func AttrString(v Value) (string, bool) {
	switch v.kind {
	case KindUnknown, KindUndefined, KindNull, KindFunction:
		return "", false
	case KindJSX:
		return "[object Object]", true
	}
	return v.ToString(), true
}

// FromLegacy decodes a value from the historical string encoding. The encoding
// is lossy (a string "0" and the number 0 look alike), so it must only be used
// where no typed value is available, e.g. values produced by external string
// APIs. Keywords and numeric text decode to their JS types; "[...]"/"{...}"
// sources decode as literals; anything else is a string.
func FromLegacy(s string) Value {
	switch s {
	case "true":
		return Bool(true)
	case "false":
		return Bool(false)
	case "null":
		return Null()
	case "undefined":
		return Undefined()
	case "NaN":
		return Number(math.NaN())
	case "Infinity":
		return Number(math.Inf(1))
	case "-Infinity":
		return Number(math.Inf(-1))
	}
	if canonicalNumber(s) {
		f, _ := strconv.ParseFloat(s, 64)
		return Number(f)
	}
	t := strings.TrimSpace(s)
	if len(t) >= 2 && ((t[0] == '[' && t[len(t)-1] == ']') || (t[0] == '{' && t[len(t)-1] == '}')) {
		if v, ok := ParseLiteralSource(t); ok {
			return v
		}
	}
	return String(s)
}

// canonicalNumber reports whether s is exactly how JS would print some number,
// so decoding it as a number round-trips.
func canonicalNumber(s string) bool {
	if s == "" || !isStrDecimal(strings.TrimPrefix(s, "-")) {
		return false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	return NumberToString(f) == s
}
