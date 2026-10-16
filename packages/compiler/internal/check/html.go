package check

import (
	"strings"

	"golang.org/x/net/html"
)

// document is a parsed HTML document with convenience accessors used by rules.
type document struct {
	root *html.Node
}

// parseHTML parses a final page document. The page is already a complete
// document, but html.Parse tolerates fragments too.
func parseHTML(src string) (*document, error) {
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil, err
	}
	return &document{root: root}, nil
}

// walk visits every element node in document order.
func (d *document) walk(fn func(*html.Node)) {
	if d == nil || d.root == nil {
		return
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			fn(n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(d.root)
}

// elements returns every element with the given (lower-cased) tag name.
func (d *document) elements(tag string) []*html.Node {
	var out []*html.Node
	d.walk(func(n *html.Node) {
		if strings.EqualFold(n.Data, tag) {
			out = append(out, n)
		}
	})
	return out
}

// firstElement returns the first element with the given tag name, or nil.
func (d *document) firstElement(tag string) *html.Node {
	var found *html.Node
	d.walk(func(n *html.Node) {
		if found == nil && strings.EqualFold(n.Data, tag) {
			found = n
		}
	})
	return found
}

// attr returns the value of the named attribute (case-insensitive) and whether
// it was present. For bare boolean attributes the value is "".
func attr(n *html.Node, name string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val, true
		}
	}
	return "", false
}

// hasAttr reports whether the element carries the named attribute.
func hasAttr(n *html.Node, name string) bool {
	_, ok := attr(n, name)
	return ok
}

// textContent returns the concatenated text of a node and its descendants.
func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	return strings.TrimSpace(b.String())
}

// maxHeadingLevel returns the deepest heading level (1-6) present, and the
// heading levels in document order.
func headingLevels(d *document) []int {
	var levels []int
	d.walk(func(n *html.Node) {
		if len(n.Data) == 2 && (n.Data[0] == 'h' || n.Data[0] == 'H') {
			if lvl := int(n.Data[1] - '0'); lvl >= 1 && lvl <= 6 {
				levels = append(levels, lvl)
			}
		}
	})
	return levels
}

// metaByName returns the content of <meta name="..."> (case-insensitive).
func (d *document) metaByName(name string) (string, bool) {
	for _, n := range d.elements("meta") {
		if v, ok := attr(n, "name"); ok && strings.EqualFold(v, name) {
			if c, ok := attr(n, "content"); ok {
				return c, true
			}
			return "", true
		}
	}
	return "", false
}

// metaByProperty returns the content of <meta property="...">.
func (d *document) metaByProperty(prop string) (string, bool) {
	for _, n := range d.elements("meta") {
		if v, ok := attr(n, "property"); ok && strings.EqualFold(v, prop) {
			if c, ok := attr(n, "content"); ok {
				return c, true
			}
			return "", true
		}
	}
	return "", false
}

// hasCanonical reports whether a <link rel="canonical"> exists.
func (d *document) hasCanonical() bool {
	for _, n := range d.elements("link") {
		if v, ok := attr(n, "rel"); ok && strings.EqualFold(v, "canonical") {
			return true
		}
	}
	return false
}
