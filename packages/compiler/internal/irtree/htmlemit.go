package irtree

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/htmlspec"
)

// Shared element/attribute serialization used by both the IR tree builder and
// the renderer's SSR evaluator. The rules themselves live in htmlspec; this
// file adapts AST attributes to them.

// AttrValue pairs an evaluated attribute value with the JS type implied by its
// expression, so htmlspec can tell `disabled={false}` from `title="false"`.
// A nil expression is a bare attribute.
func AttrValue(expr ast.Expr, val string) htmlspec.Value {
	if expr == nil {
		return htmlspec.Value{Kind: htmlspec.Bare}
	}
	switch e := expr.(type) {
	case *ast.TypeAssertion:
		return AttrValue(e.Expr, val)
	case *ast.Literal:
		switch e.Kind {
		case ast.StringLit:
			return htmlspec.StrVal(val)
		case ast.NumberLit:
			return htmlspec.Value{Kind: htmlspec.Number, Str: val}
		case ast.BoolLit:
			return htmlspec.BoolVal(e.Value == "true")
		case ast.NullLit:
			return htmlspec.Value{Kind: htmlspec.Null}
		}
	case *ast.TemplateExpr:
		return htmlspec.StrVal(val)
	case *ast.Identifier:
		if e.Name == "undefined" {
			return htmlspec.Value{Kind: htmlspec.Null}
		}
	}
	return htmlspec.LooseVal(val)
}

// StyleCSS serializes a style attribute value. Object literals (and values
// that evaluate to object-literal source) go through htmlspec.Style; any
// other value is used as a CSS string. ok is false when the attribute must be
// omitted (null/undefined/empty object).
func StyleCSS(expr ast.Expr, eval func(ast.Expr) string) (string, bool) {
	if expr == nil {
		return "", false
	}
	if ta, isTA := expr.(*ast.TypeAssertion); isTA {
		return StyleCSS(ta.Expr, eval)
	}
	if obj, isObj := expr.(*ast.ObjectExpr); isObj {
		if css, done := styleFromObject(obj, eval); done {
			return css, css != ""
		}
	}
	val := eval(expr)
	v := AttrValue(expr, val)
	if v.IsNullish() || v.Kind == htmlspec.Bool {
		return "", false
	}
	if _, isLit := expr.(*ast.Literal); !isLit {
		if t := strings.TrimSpace(val); strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}") {
			if obj, isObj := constSourceToAST(t).(*ast.ObjectExpr); isObj {
				if css, done := styleFromObject(obj, eval); done {
					return css, css != ""
				}
			}
		}
	}
	return val, val != ""
}

// styleFromObject converts an object literal to CSS. done is false when the
// object contains a spread, which only a full evaluation can resolve.
func styleFromObject(obj *ast.ObjectExpr, eval func(ast.Expr) string) (string, bool) {
	entries := make([]htmlspec.StyleEntry, 0, len(obj.Properties))
	for _, prop := range obj.Properties {
		if prop == nil {
			continue
		}
		if prop.Spread || prop.Value == nil {
			return "", false
		}
		entries = append(entries, htmlspec.StyleEntry{Key: prop.Key, Value: AttrValue(prop.Value, eval(prop.Value))})
	}
	return htmlspec.Style(entries), true
}

// RenderAttr serializes one static attribute of element tag: ` name="value"`,
// ` name`, or "" when React would omit it. Callers skip spreads, refs, event
// handlers and dangerouslySetInnerHTML.
func RenderAttr(tag string, attr *ast.JSXAttr, eval func(ast.Expr) string) string {
	if attr.Name == "style" {
		css, ok := StyleCSS(attr.Value, eval)
		if !ok {
			return ""
		}
		return ` style="` + escape.HTMLAttr(css) + `"`
	}
	if attr.Value == nil {
		return htmlspec.Attr(tag, attr.Name, AttrValue(nil, ""))
	}
	return htmlspec.Attr(tag, attr.Name, AttrValue(attr.Value, eval(attr.Value)))
}

// CheckElementMarkup validates the tag name, the attribute names, and (for
// void elements) the absence of children. It returns diagnostics for file.
func CheckElementMarkup(file string, el *ast.JSXElement) []error {
	if el == nil || el.Opening == nil {
		return nil
	}
	var errs []error
	pos := el.Position
	name := el.Opening.Name
	if !htmlspec.ValidTagName(name) {
		errs = append(errs, diag.New(file, pos.Line, pos.Col,
			fmt.Sprintf("invalid element name %q", name),
			"element names must start with a letter and contain only letters, digits, '-', '_', '.' or ':'", ""))
	}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Spread || isOnEvent(attr.Name) || htmlspec.IsReservedProp(attr.Name) {
			continue
		}
		if !htmlspec.ValidAttrName(htmlspec.AttrName(attr.Name)) {
			p := attr.Position
			if p.Line == 0 {
				p = pos
			}
			errs = append(errs, diag.New(file, p.Line, p.Col,
				fmt.Sprintf("invalid attribute name %q on <%s>", attr.Name, name),
				"attribute names may contain letters, digits, '-', '_', '.' and ':' and must not start with a digit, '-' or '.'", ""))
		}
	}
	if htmlspec.IsVoid(name) && HasMeaningfulChildren(el.Children) {
		errs = append(errs, diag.New(file, pos.Line, pos.Col,
			fmt.Sprintf("<%s> is a void element and cannot have children", name),
			fmt.Sprintf("write it self-closing as <%s />; move the content next to it", name), ""))
	}
	return errs
}

// HasMeaningfulChildren reports whether children contain anything other than
// JSX-insignificant whitespace.
func HasMeaningfulChildren(children []ast.JSXChild) bool {
	for _, c := range children {
		if t, ok := c.(*ast.JSXText); ok {
			if strings.TrimSpace(t.Value) == "" && (t.Value == "" || strings.ContainsAny(t.Value, "\n\r")) {
				continue
			}
		}
		return true
	}
	return false
}

// OptionValue returns the value an <option> submits: its value attribute, or
// else its static text content.
func OptionValue(el *ast.JSXElement, eval func(ast.Expr) string) string {
	for _, attr := range el.Opening.Attributes {
		if attr != nil && !attr.Spread && attr.Name == "value" && attr.Value != nil {
			return eval(attr.Value)
		}
	}
	var b strings.Builder
	for _, c := range el.Children {
		switch ch := c.(type) {
		case *ast.JSXText:
			b.WriteString(ch.Value)
		case *ast.JSXExprContainer:
			if ch.Expression != nil {
				b.WriteString(eval(ch.Expression))
			}
		}
	}
	return b.String()
}

// OptionSelected reports whether an option value is in the selection.
func OptionSelected(selection []string, value string) bool {
	for _, s := range selection {
		if s == value {
			return true
		}
	}
	return false
}

// SelectSelection returns the option values selected by a <select>'s value or
// defaultValue attribute, and whether such an attribute was present.
func SelectSelection(el *ast.JSXElement, eval func(ast.Expr) string) ([]string, bool) {
	var found bool
	var sel []string
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Spread || attr.Value == nil {
			continue
		}
		if attr.Name == "value" || (attr.Name == "defaultValue" && !found) {
			sel = htmlspec.SelectValues(AttrValue(attr.Value, eval(attr.Value)))
			found = true
		}
	}
	return sel, found
}

// TextareaValue returns the escaped content of a <textarea> whose value comes
// from a value/defaultValue attribute.
func TextareaValue(el *ast.JSXElement, eval func(ast.Expr) string) (string, bool) {
	var found bool
	var out string
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Spread || attr.Value == nil {
			continue
		}
		if attr.Name == "value" || (attr.Name == "defaultValue" && !found) {
			v := AttrValue(attr.Value, eval(attr.Value))
			out = ""
			if !v.IsNullish() && v.Kind != htmlspec.Bool {
				out = v.Str
			}
			found = true
		}
	}
	if !found {
		return "", false
	}
	return escape.Text(out), true
}

// PreserveLeadingNewline re-adds the newline the HTML parser drops right after
// a <pre>, <textarea> or <listing> start tag, matching React.
func PreserveLeadingNewline(tag, content string) string {
	switch tag {
	case "pre", "textarea", "listing":
		if strings.HasPrefix(content, "\n") || strings.HasPrefix(content, "\r") {
			return "\n" + content
		}
	}
	return content
}
