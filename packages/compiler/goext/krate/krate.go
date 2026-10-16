// Package krate provides HTML helpers for Go components. Import it as
//
//	kr "github.com/kratejs/krate/packages/compiler/goext/krate"
//
// A component builds UI by composing Nodes; text and attribute values are
// escaped by default, and trusted markup is wrapped in Raw (or the HTML type).
// Components may return either a Node or an HTML value.
package krate

import (
	"fmt"
	"html"
	htmltemplate "html/template"
	"net/url"
	"sort"
	"strings"
)

// Node is a renderable UI fragment. Every helper returns a Node, and a
// component may return one directly (or any type implementing fmt.Stringer).
type Node interface {
	// Render returns trusted, pre-rendered HTML.
	Render() HTML
	// String lets a Node flow through fmt.Sprint, which the generated
	// component registry uses to emit markup.
	String() string
}

// HTML is trusted, pre-rendered markup. A component returning HTML has its
// output inserted verbatim (not escaped).
type HTML string

// Render returns the markup unchanged.
func (h HTML) Render() HTML { return h }

// String returns the markup unchanged.
func (h HTML) String() string { return string(h) }

// Raw marks s as trusted markup. Use it only for content you built yourself.
func Raw(s string) HTML { return HTML(s) }

// Escape escapes s for safe inclusion in HTML text.
func Escape(s string) string { return html.EscapeString(s) }

// Attr escapes s for safe inclusion in a quoted HTML attribute value.
func Attr(s string) string { return html.EscapeString(s) }

// URL escapes s as a URL path segment (for a path-only href you also want to
// pass through Attrs, which HTML-escapes it).
func URL(s string) string { return (&url.URL{Path: s}).String() }

// text is untrusted text, escaped when rendered.
type text string

func (t text) Render() HTML   { return HTML(html.EscapeString(string(t))) }
func (t text) String() string { return string(t.Render()) }

// Text returns a node that renders s with HTML escaping.
func Text(s string) Node { return text(s) }

// frag is a group of nodes with no wrapping element.
type frag []Node

func (f frag) Render() HTML {
	var b strings.Builder
	for _, c := range f {
		writeNode(&b, c)
	}
	return HTML(b.String())
}

func (f frag) String() string { return string(f.Render()) }

// Frag groups children with no wrapping element.
func Frag(children ...Node) Node { return frag(children) }

// Join renders parts separated by sep.
func Join(sep Node, parts ...Node) Node {
	out := make(frag, 0, len(parts)*2)
	for i, p := range parts {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, p)
	}
	return out
}

// Each renders one node per item, in order.
func Each[T any](items []T, fn func(T) Node) Node {
	out := make(frag, 0, len(items))
	for _, it := range items {
		out = append(out, fn(it))
	}
	return out
}

// writeNode renders n into b, ignoring nil.
func writeNode(b *strings.Builder, n Node) {
	if n == nil {
		return
	}
	b.WriteString(string(n.Render()))
}

// el is an element node.
type el struct {
	tag      string
	attrs    Attrs
	children []Node
}

func (e el) Render() HTML {
	if !validName(e.tag) {
		return ""
	}
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(e.tag)
	b.WriteString(e.attrs.render())
	if isVoid(e.tag) {
		return HTML(b.String() + ">")
	}
	b.WriteByte('>')
	for _, c := range e.children {
		writeNode(&b, c)
	}
	b.WriteString("</")
	b.WriteString(e.tag)
	b.WriteByte('>')
	return HTML(b.String())
}

func (e el) String() string { return string(e.Render()) }

// El builds an element. tag is trusted (invalid names render nothing); attribute
// values and children are escaped by their own types.
func El(tag string, attrs Attrs, children ...Node) Node {
	return el{tag: tag, attrs: attrs, children: children}
}

// Attrs is a set of HTML attributes. Values may be:
//
//	string          rendered as name="value"
//	bool            rendered as a bare name when true, omitted when false
//	[]string        rendered space-joined (class-style)
//	map[string]string rendered as name="k:v;k:v" (style-style)
//
// Attribute names and values are validated/escaped; unknown name characters are
// skipped.
type Attrs map[string]any

func (a Attrs) render() string {
	if len(a) == 0 {
		return ""
	}
	keys := make([]string, 0, len(a))
	for k := range a {
		if validName(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		switch v := a[k].(type) {
		case nil:
			continue
		case bool:
			if v {
				b.WriteByte(' ')
				b.WriteString(k)
			}
		case []string:
			parts := nonEmpty(v)
			if len(parts) == 0 {
				continue
			}
			b.WriteByte(' ')
			b.WriteString(k)
			b.WriteString(`="`)
			b.WriteString(html.EscapeString(strings.Join(parts, " ")))
			b.WriteByte('"')
		case map[string]string:
			if len(v) == 0 {
				continue
			}
			mk := make([]string, 0, len(v))
			for p := range v {
				mk = append(mk, p)
			}
			sort.Strings(mk)
			decls := make([]string, 0, len(v))
			for _, p := range mk {
				decls = append(decls, p+":"+v[p])
			}
			b.WriteByte(' ')
			b.WriteString(k)
			b.WriteString(`="`)
			b.WriteString(html.EscapeString(strings.Join(decls, ";")))
			b.WriteByte('"')
		default:
			b.WriteByte(' ')
			b.WriteString(k)
			b.WriteString(`="`)
			b.WriteString(html.EscapeString(fmt.Sprint(v)))
			b.WriteByte('"')
		}
	}
	return b.String()
}

// Class builds a class attribute from names, dropping empties.
func Class(names ...string) Attrs { return Attrs{"class": nonEmpty(names)} }

// Style builds a style attribute from key/value pairs (Style("color", "red")).
// Odd trailing keys are ignored.
func Style(pairs ...string) Attrs {
	m := make(map[string]string, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return Attrs{"style": m}
}

// nonEmpty returns names with empties removed (nil when none remain).
func nonEmpty(names []string) []string {
	var out []string
	for _, n := range names {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// validName reports whether s is a safe element or attribute name.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		if i == 0 && !isLetter {
			return false
		}
		if !isLetter && !isDigit && r != '-' && r != '_' && r != ':' {
			return false
		}
	}
	return true
}

// voidElements are HTML elements with no children or closing tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

func isVoid(tag string) bool { return voidElements[tag] }

// Template is a parsed html/template with context-aware auto-escaping, for
// markup-heavy components. A component can return its Render result directly.
type Template struct{ t *htmltemplate.Template }

// NewTemplate parses name from src using html/template.
func NewTemplate(name, src string) (*Template, error) {
	t, err := htmltemplate.New(name).Parse(src)
	if err != nil {
		return nil, err
	}
	return &Template{t: t}, nil
}

// MustTemplate is NewTemplate for a literal, panicking on a parse error. The
// panic surfaces as a build error naming the component.
func MustTemplate(src string) *Template {
	return &Template{t: htmltemplate.Must(htmltemplate.New("component").Parse(src))}
}

// Render executes the template with data and returns trusted HTML.
func (t *Template) Render(data any) (HTML, error) {
	if t == nil || t.t == nil {
		return "", fmt.Errorf("krate: uninitialized template")
	}
	var b strings.Builder
	if err := t.t.Execute(&b, data); err != nil {
		return "", err
	}
	return HTML(b.String()), nil
}

// SafeHTML marks a node as trusted inside a Template ({{ .X }}), bypassing
// html/template's contextual escaping. Use only for content you built yourself.
func SafeHTML(n Node) htmltemplate.HTML {
	if n == nil {
		return ""
	}
	return htmltemplate.HTML(n.Render())
}

// Element constructors. Each takes an Attrs (nil is fine) and children; void
// elements (Img, Input, Br, ...) take Attrs only. Use El for any tag without a
// constructor below.

func Div(attrs Attrs, children ...Node) Node        { return El("div", attrs, children...) }
func Span(attrs Attrs, children ...Node) Node       { return El("span", attrs, children...) }
func P(attrs Attrs, children ...Node) Node          { return El("p", attrs, children...) }
func A(attrs Attrs, children ...Node) Node          { return El("a", attrs, children...) }
func Ul(attrs Attrs, children ...Node) Node         { return El("ul", attrs, children...) }
func Ol(attrs Attrs, children ...Node) Node         { return El("ol", attrs, children...) }
func Li(attrs Attrs, children ...Node) Node         { return El("li", attrs, children...) }
func H1(attrs Attrs, children ...Node) Node         { return El("h1", attrs, children...) }
func H2(attrs Attrs, children ...Node) Node         { return El("h2", attrs, children...) }
func H3(attrs Attrs, children ...Node) Node         { return El("h3", attrs, children...) }
func H4(attrs Attrs, children ...Node) Node         { return El("h4", attrs, children...) }
func H5(attrs Attrs, children ...Node) Node         { return El("h5", attrs, children...) }
func H6(attrs Attrs, children ...Node) Node         { return El("h6", attrs, children...) }
func Section(attrs Attrs, children ...Node) Node    { return El("section", attrs, children...) }
func Article(attrs Attrs, children ...Node) Node    { return El("article", attrs, children...) }
func Aside(attrs Attrs, children ...Node) Node      { return El("aside", attrs, children...) }
func Header(attrs Attrs, children ...Node) Node     { return El("header", attrs, children...) }
func Footer(attrs Attrs, children ...Node) Node     { return El("footer", attrs, children...) }
func Nav(attrs Attrs, children ...Node) Node        { return El("nav", attrs, children...) }
func Main(attrs Attrs, children ...Node) Node       { return El("main", attrs, children...) }
func Figure(attrs Attrs, children ...Node) Node     { return El("figure", attrs, children...) }
func Figcaption(attrs Attrs, children ...Node) Node { return El("figcaption", attrs, children...) }
func Blockquote(attrs Attrs, children ...Node) Node { return El("blockquote", attrs, children...) }
func Form(attrs Attrs, children ...Node) Node       { return El("form", attrs, children...) }
func Label(attrs Attrs, children ...Node) Node      { return El("label", attrs, children...) }
func Button(attrs Attrs, children ...Node) Node     { return El("button", attrs, children...) }
func Textarea(attrs Attrs, children ...Node) Node   { return El("textarea", attrs, children...) }
func Select(attrs Attrs, children ...Node) Node     { return El("select", attrs, children...) }
func Option(attrs Attrs, children ...Node) Node     { return El("option", attrs, children...) }
func Table(attrs Attrs, children ...Node) Node      { return El("table", attrs, children...) }
func Thead(attrs Attrs, children ...Node) Node      { return El("thead", attrs, children...) }
func Tbody(attrs Attrs, children ...Node) Node      { return El("tbody", attrs, children...) }
func Tr(attrs Attrs, children ...Node) Node         { return El("tr", attrs, children...) }
func Th(attrs Attrs, children ...Node) Node         { return El("th", attrs, children...) }
func Td(attrs Attrs, children ...Node) Node         { return El("td", attrs, children...) }
func Code(attrs Attrs, children ...Node) Node       { return El("code", attrs, children...) }
func Pre(attrs Attrs, children ...Node) Node        { return El("pre", attrs, children...) }
func Strong(attrs Attrs, children ...Node) Node     { return El("strong", attrs, children...) }
func Em(attrs Attrs, children ...Node) Node         { return El("em", attrs, children...) }
func Small(attrs Attrs, children ...Node) Node      { return El("small", attrs, children...) }
func Time(attrs Attrs, children ...Node) Node       { return El("time", attrs, children...) }
func Video(attrs Attrs, children ...Node) Node      { return El("video", attrs, children...) }
func Audio(attrs Attrs, children ...Node) Node      { return El("audio", attrs, children...) }
func Svg(attrs Attrs, children ...Node) Node        { return El("svg", attrs, children...) }
func Path(attrs Attrs, children ...Node) Node       { return El("path", attrs, children...) }

// Void elements (no children, no closing tag).
func Br(attrs Attrs) Node     { return El("br", attrs) }
func Hr(attrs Attrs) Node     { return El("hr", attrs) }
func Img(attrs Attrs) Node    { return El("img", attrs) }
func Input(attrs Attrs) Node  { return El("input", attrs) }
func Link(attrs Attrs) Node   { return El("link", attrs) }
func Meta(attrs Attrs) Node   { return El("meta", attrs) }
func Source(attrs Attrs) Node { return El("source", attrs) }
