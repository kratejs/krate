// Package htmlspec is the single source of truth for how Krate serializes JSX
// elements to HTML: void elements, boolean / booleanish / enumerated
// attributes, the React prop-name to attribute-name mapping (HTML, SVG, xlink
// and xml namespaces), form-control value handling, style objects, URL
// sanitization and tag/attribute name validation.
//
// Every HTML emitter (the IR tree builder, the SSR evaluator, the renderer)
// must use this package instead of keeping its own lists, so the paths cannot
// drift apart. The semantics follow react-dom/server (React 19).
//
// The package deliberately has no dependency on the AST: callers convert
// their evaluated values into a Value, which carries the JS type when known.
package htmlspec

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// Kind is the JS type of an attribute or style value, as far as the caller
// knows it.
type Kind int

const (
	// Loose is an evaluated value whose JS type is unknown (the evaluator
	// works on strings). "null"/"undefined" are treated as nullish and
	// boolean attributes use JS truthiness of the text; everything else is a
	// string.
	Loose Kind = iota
	// String is a value known to be a JS string (e.g. a string literal).
	String
	// Number is a value known to be a JS number.
	Number
	// Bool is a value known to be a JS boolean; Str is "true" or "false".
	Bool
	// Null is null or undefined.
	Null
	// Bare is an attribute written without a value (`<input disabled>`),
	// which JSX defines as `true`. Non-boolean bare attributes are emitted
	// bare as well so internal marker attributes stay compact.
	Bare
)

// Value is an attribute or style value with its (possibly unknown) JS type.
type Value struct {
	Kind Kind
	Str  string
	// Items holds the element texts when the value is a JS array (used by
	// <select multiple> selections).
	Items []string
}

// StrVal builds a known-string Value.
func StrVal(s string) Value { return Value{Kind: String, Str: s} }

// LooseVal builds an untyped evaluated Value.
func LooseVal(s string) Value { return Value{Kind: Loose, Str: s} }

// BoolVal builds a known-boolean Value.
func BoolVal(b bool) Value {
	if b {
		return Value{Kind: Bool, Str: "true"}
	}
	return Value{Kind: Bool, Str: "false"}
}

// IsNullish reports whether v renders as nothing (null/undefined).
func (v Value) IsNullish() bool {
	if v.Kind == Null {
		return true
	}
	return v.Kind == Loose && (v.Str == "null" || v.Str == "undefined")
}

// Truthy reports the JS truthiness of v.
func (v Value) Truthy() bool {
	switch v.Kind {
	case Null:
		return false
	case Bare:
		return true
	case Bool:
		return v.Str == "true"
	case String:
		return v.Str != ""
	case Number:
		return !isZeroOrNaN(v.Str)
	}
	switch v.Str {
	case "", "false", "null", "undefined":
		return false
	}
	return !isZeroOrNaN(v.Str)
}

func isZeroOrNaN(s string) bool {
	switch s {
	case "0", "-0", "0.0", "NaN":
		return true
	}
	return false
}

// voidElements are elements that never have children or a closing tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "keygen": true, "link": true,
	"meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

// IsVoid reports whether tag is an HTML void element.
func IsVoid(tag string) bool {
	return voidElements[tag]
}

// booleanAttrs are HTML attributes whose presence alone means true. Keys are
// the emitted (lowercase) attribute names. `hidden` is boolean except for the
// "until-found" keyword, handled in Attr.
var booleanAttrs = map[string]bool{
	"allowfullscreen": true, "async": true, "autofocus": true, "autoplay": true,
	"checked": true, "controls": true, "default": true, "defer": true,
	"disabled": true, "disablepictureinpicture": true, "disableremoteplayback": true,
	"formnovalidate": true, "hidden": true, "inert": true, "itemscope": true,
	"loop": true, "multiple": true, "muted": true, "nomodule": true,
	"novalidate": true, "open": true, "playsinline": true, "readonly": true,
	"required": true, "reversed": true, "scoped": true, "seamless": true,
	"selected": true, "shadowrootclonable": true, "shadowrootdelegatesfocus": true,
	"shadowrootserializable": true,
	// Legacy presentational booleans still parsed by browsers.
	"compact": true, "declare": true, "ismap": true, "nohref": true,
	"noresize": true, "noshade": true, "nowrap": true, "truespeed": true,
}

// IsBoolean reports whether name (an emitted attribute name, or a JSX prop
// name) is a boolean attribute.
func IsBoolean(name string) bool {
	return booleanAttrs[strings.ToLower(AttrName(name))]
}

// booleanishAttrs are enumerated attributes whose JS boolean values serialize
// as the strings "true"/"false" (React's "booleanish string" props).
var booleanishAttrs = map[string]bool{
	"contenteditable": true, "draggable": true, "spellcheck": true,
	"autoreverse": true, "externalresourcesrequired": true, "focusable": true,
	"preservealpha": true,
}

// IsBooleanish reports whether name is a booleanish enumerated attribute
// (contentEditable, draggable, spellCheck, ...).
func IsBooleanish(name string) bool {
	return booleanishAttrs[strings.ToLower(AttrName(name))]
}

// propertyAttrs are attributes that reflect live DOM state on form controls
// and media; after hydration they must be updated through the DOM property,
// not setAttribute.
var propertyAttrs = map[string]bool{
	"value": true, "checked": true, "selected": true, "muted": true,
}

// IsPropertyAttr reports whether a reactive update to name must go through
// the DOM property (el.value / el.checked / el.selected / el.muted).
func IsPropertyAttr(name string) bool {
	return propertyAttrs[AttrName(name)]
}

// urlAttrs are attributes whose value is navigated to or fetched, and so are
// subject to javascript: URL neutralization (React 19 sanitizeURL).
var urlAttrs = map[string]bool{
	"href": true, "src": true, "action": true, "formaction": true,
	"xlink:href": true,
}

// IsURLAttr reports whether name (emitted form) is a URL-valued attribute.
func IsURLAttr(name string) bool {
	return urlAttrs[strings.ToLower(name)]
}

// BlockedURL replaces a javascript: URL, matching React 19.
const BlockedURL = "javascript:throw new Error('React has blocked a javascript: URL as a security precaution.')"

// IsJavaScriptURL reports whether u is a javascript: URL the way a browser
// parses it: leading C0 controls and spaces are ignored, ASCII tab/CR/LF may
// appear anywhere inside the scheme, and the scheme is case-insensitive. This
// mirrors React's isJavaScriptProtocol regular expression.
func IsJavaScriptURL(u string) bool {
	i := 0
	for i < len(u) && (u[i] <= 0x20) {
		i++
	}
	const scheme = "javascript:"
	for k := 0; k < len(scheme); k++ {
		for i < len(u) && (u[i] == '\t' || u[i] == '\n' || u[i] == '\r') {
			i++
		}
		if i >= len(u) {
			return false
		}
		c := u[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != scheme[k] {
			return false
		}
		i++
	}
	return true
}

// SanitizeURL returns u, or BlockedURL when u is a javascript: URL.
func SanitizeURL(u string) string {
	if IsJavaScriptURL(u) {
		return BlockedURL
	}
	return u
}

// rawTextElements hold unescaped text; their content must never contain the
// closing tag sequence.
var rawTextElements = map[string]bool{"script": true, "style": true}

// IsRawText reports whether tag's children are raw text (script/style).
func IsRawText(tag string) bool {
	return rawTextElements[strings.ToLower(tag)]
}

// RawText makes s safe inside a <script> or <style> element by breaking up
// any `</script` / `</style` (case-insensitive) sequence that would close the
// element early. In JS and CSS `<\/` is equivalent to `</`.
func RawText(tag, s string) string {
	closer := "</" + strings.ToLower(tag)
	lower := strings.ToLower(s)
	if !strings.Contains(lower, closer) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		if s[i] == '<' && i+1 < len(s) {
			if strings.HasPrefix(lower[i:], closer) {
				b.WriteString(`<\/`)
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Text escapes decoded JSX text for an HTML text position.
func Text(s string) string {
	return escape.Text(s)
}

// ValidTagName reports whether name is an acceptable element name (React's
// VALID_TAG_REGEX: a letter followed by letters, digits, ':', '.', '_', '-').
func ValidTagName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if i == 0 {
			if !isAlpha {
				return false
			}
			continue
		}
		if !isAlpha && !(c >= '0' && c <= '9') && c != ':' && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// ValidAttrName reports whether name is a safe attribute name, following
// React's ATTRIBUTE_NAME_START_CHAR / ATTRIBUTE_NAME_CHAR grammar (XML names).
func ValidAttrName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !isAttrNameStart(r) {
				return false
			}
			continue
		}
		if !isAttrNameStart(r) && r != '-' && r != '.' && !(r >= '0' && r <= '9') &&
			r != 0xB7 && !(r >= 0x300 && r <= 0x36F) && !(r >= 0x203F && r <= 0x2040) {
			return false
		}
	}
	return true
}

func isAttrNameStart(r rune) bool {
	switch {
	case r == ':' || r == '_':
		return true
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		return true
	case r >= 0xC0 && r <= 0xD6, r >= 0xD8 && r <= 0xF6, r >= 0xF8 && r <= 0x2FF,
		r >= 0x370 && r <= 0x37D, r >= 0x37F && r <= 0x1FFF, r >= 0x200C && r <= 0x200D,
		r >= 0x2070 && r <= 0x218F, r >= 0x2C00 && r <= 0x2FEF, r >= 0x3001 && r <= 0xD7FF,
		r >= 0xF900 && r <= 0xFDCF, r >= 0xFDF0 && r <= 0xFFFD:
		return true
	}
	return false
}

// IsReservedProp reports props that never become attributes.
func IsReservedProp(name string) bool {
	switch name {
	case "children", "key", "ref", "dangerouslySetInnerHTML", "innerHTML",
		"suppressHydrationWarning", "suppressContentEditableWarning":
		return true
	}
	return false
}

// Attr serializes one attribute of element tag. jsxName is the prop name as
// written in JSX (className, htmlFor, xlinkHref, ...). The result is either
// "" (the attribute is omitted) or ` name`, or ` name="escaped value"`.
//
// Callers handle `style` objects (Style), textarea/select values
// (ContentValue / SelectValues) and event handlers before calling Attr.
func Attr(tag, jsxName string, v Value) string {
	name := AttrName(jsxName)
	if v.IsNullish() {
		return ""
	}
	lower := strings.ToLower(name)
	switch {
	case booleanAttrs[lower]:
		if lower == "hidden" && v.Kind != Bool && v.Str == "until-found" {
			return ` hidden="until-found"`
		}
		if !v.Truthy() {
			return ""
		}
		return " " + name
	case booleanishAttrs[lower]:
		if v.Kind == Bare {
			return " " + name + `="true"`
		}
	case strings.HasPrefix(lower, "data-") || strings.HasPrefix(lower, "aria-"):
		if v.Kind == Bare {
			return " " + name
		}
	case v.Kind == Bool:
		// React drops JS booleans on non-boolean attributes.
		return ""
	case v.Kind == Bare:
		return " " + name
	}
	val := v.Str
	if v.Kind == Bare {
		val = "true"
	}
	if IsURLAttr(lower) {
		// React 19 omits an empty src, and an empty href except on <a>.
		if val == "" && (lower == "src" || (lower == "href" && tag != "a")) {
			return ""
		}
		val = SanitizeURL(val)
	}
	return " " + name + `="` + escape.HTMLAttr(val) + `"`
}

// ContentValue reports whether jsxName on tag is rendered as element content
// rather than an attribute: `value`/`defaultValue` on <textarea>.
func ContentValue(tag, jsxName string) bool {
	return tag == "textarea" && (jsxName == "value" || jsxName == "defaultValue")
}

// SelectValueProp reports whether jsxName on tag selects options instead of
// being emitted: `value`/`defaultValue` on <select>.
func SelectValueProp(tag, jsxName string) bool {
	return tag == "select" && (jsxName == "value" || jsxName == "defaultValue")
}

// SelectValues splits an evaluated select value into the option values it
// selects. Arrays (for <select multiple>) arrive as Items, or as a JSON/JS
// array literal from untyped callers.
func SelectValues(v Value) []string {
	if v.IsNullish() {
		return nil
	}
	if v.Items != nil {
		return v.Items
	}
	s := v.Str
	t := strings.TrimSpace(s)
	if v.Kind != String && len(t) >= 2 && t[0] == '[' && t[len(t)-1] == ']' {
		return splitArrayLiteral(t[1 : len(t)-1])
	}
	return []string{s}
}

// splitArrayLiteral splits the inside of a flat JS/JSON array literal into
// unquoted item strings.
func splitArrayLiteral(s string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	flush := func() {
		item := strings.TrimSpace(cur.String())
		cur.Reset()
		if item == "" {
			return
		}
		if n := len(item); n >= 2 && (item[0] == '"' || item[0] == '\'') && item[n-1] == item[0] {
			item = escape.UnescapeJSString(item[1 : n-1])
		}
		out = append(out, item)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
			cur.WriteByte(c)
		case ',':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}
