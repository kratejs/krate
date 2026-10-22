package irtree

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/htmlspec"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
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
	case *ast.UnaryExpr:
		// `void 0` is how the TypeScript strip pass spells undefined.
		if e.Op == "void" {
			return htmlspec.Value{Kind: htmlspec.Null}
		}
	}
	return htmlspec.LooseVal(val)
}

// RendersNothing reports child expressions that React renders as nothing:
// boolean, null and undefined literals (including `void 0`).
func RendersNothing(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	v := AttrValue(expr, "")
	return v.Kind == htmlspec.Bool || v.Kind == htmlspec.Null
}

// ValueFunc evaluates an attribute expression to a typed build-time value.
type ValueFunc func(ast.Expr) jsvalue.Value

// StyleCSS serializes a style attribute value. Object values go through
// htmlspec.Style; strings are used as CSS text. ok is false when the
// attribute must be omitted (null/undefined/booleans/empty object, or a value
// not known at build time).
func StyleCSS(expr ast.Expr, eval ValueFunc) (string, bool) {
	if expr == nil {
		return "", false
	}
	v := eval(expr)
	switch v.Kind() {
	case jsvalue.KindObject:
		entries := make([]htmlspec.StyleEntry, 0, len(v.Keys()))
		for _, k := range v.Keys() {
			p, _ := v.Get(k)
			entries = append(entries, htmlspec.StyleEntry{Key: k, Value: JSAttrValue(p, "")})
		}
		css := htmlspec.Style(entries)
		return css, css != ""
	case jsvalue.KindString:
		return v.StringValue(), v.StringValue() != ""
	case jsvalue.KindNumber:
		return v.ToString(), true
	}
	return "", false
}

// RenderAttr serializes one static attribute of element tag: ` name="value"`,
// ` name`, or "" when React would omit it. Callers skip spreads, refs, event
// handlers and dangerouslySetInnerHTML. unknownText supplies the text used
// when the value is not known at build time.
func RenderAttr(tag string, attr *ast.JSXAttr, eval ValueFunc, unknownText func(ast.Expr) string) string {
	if attr.Name == "style" {
		css, ok := StyleCSS(attr.Value, eval)
		if !ok {
			return ""
		}
		return ` style="` + escape.HTMLAttr(css) + `"`
	}
	if attr.Value == nil {
		return htmlspec.Attr(tag, attr.Name, htmlspec.Value{Kind: htmlspec.Bare})
	}
	return htmlspec.Attr(tag, attr.Name, attrValueOf(attr.Value, eval, unknownText))
}

func attrValueOf(expr ast.Expr, eval ValueFunc, unknownText func(ast.Expr) string) htmlspec.Value {
	v := eval(expr)
	text := ""
	if v.IsUnknown() && unknownText != nil {
		text = unknownText(expr)
	}
	return JSAttrValue(v, text)
}

// MarkupReporter builds a diagnostic for a markup problem at pos.
type MarkupReporter func(pos ast.Pos, msg, hint string) error

// FileMarkupReporter reports markup diagnostics against file without a
// source line (used where only a path is known).
func FileMarkupReporter(file string) MarkupReporter {
	return func(pos ast.Pos, msg, hint string) error {
		return diag.New(file, pos.Line, pos.Col, msg, hint, "")
	}
}

// CheckElementMarkup validates the tag name, the attribute names, and (for
// void elements) the absence of children, reporting each problem through
// report.
func CheckElementMarkup(el *ast.JSXElement, report MarkupReporter) []error {
	if el == nil || el.Opening == nil {
		return nil
	}
	var errs []error
	pos := el.Position
	name := el.Opening.Name
	if !htmlspec.ValidTagName(name) {
		errs = append(errs, report(pos,
			fmt.Sprintf("invalid element name %q", name),
			"element names must start with a letter and contain only letters, digits, '-', '_', '.' or ':'"))
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
			errs = append(errs, report(p,
				fmt.Sprintf("invalid attribute name %q on <%s>", attr.Name, name),
				"attribute names may contain letters, digits, '-', '_', '.' and ':' and must not start with a digit, '-' or '.'"))
		}
	}
	if htmlspec.IsVoid(name) && HasMeaningfulChildren(el.Children) {
		errs = append(errs, report(pos,
			fmt.Sprintf("<%s> is a void element and cannot have children", name),
			fmt.Sprintf("write it self-closing as <%s />; move the content next to it", name)))
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
func OptionValue(el *ast.JSXElement, eval ValueFunc) string {
	for _, attr := range el.Opening.Attributes {
		if attr != nil && !attr.Spread && attr.Name == "value" && attr.Value != nil {
			return eval(attr.Value).ToString()
		}
	}
	var b strings.Builder
	for _, c := range el.Children {
		switch ch := c.(type) {
		case *ast.JSXText:
			b.WriteString(ch.Value)
		case *ast.JSXExprContainer:
			if ch.Expression != nil {
				if text, ok := childText(eval(ch.Expression)); ok {
					b.WriteString(text)
				}
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
func SelectSelection(el *ast.JSXElement, eval ValueFunc) ([]string, bool) {
	var found bool
	var sel []string
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Spread || attr.Value == nil {
			continue
		}
		if attr.Name == "value" || (attr.Name == "defaultValue" && !found) {
			sel = htmlspec.SelectValues(JSAttrValue(eval(attr.Value), ""))
			found = true
		}
	}
	return sel, found
}

// TextareaValue returns the escaped content of a <textarea> whose value comes
// from a value/defaultValue attribute.
func TextareaValue(el *ast.JSXElement, eval ValueFunc) (string, bool) {
	var found bool
	var out string
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Spread || attr.Value == nil {
			continue
		}
		if attr.Name == "value" || (attr.Name == "defaultValue" && !found) {
			v := eval(attr.Value)
			out = ""
			if !v.IsNullish() && v.Kind() != jsvalue.KindBool && !v.IsUnknown() {
				out = v.ToString()
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
