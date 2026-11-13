package build

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Semantic oracle: parse both documents with x/net/html and compare element
// names, attributes (as sets), and text. Comments are ignored. Whitespace runs
// in text are equivalent except inside raw/whitespace-significant elements;
// whitespace at the very start and end of the document is ignored.

var oracleRaw = map[string]bool{
	"pre": true, "textarea": true, "script": true, "style": true, "title": true,
	"xmp": true, "iframe": true, "noembed": true, "noframes": true,
	"noscript": true, "plaintext": true,
}

type oracleEvent struct {
	desc string
	text bool
	raw  bool
}

func flatten(n *html.Node, depth int, raw bool, out *[]oracleEvent) {
	switch n.Type {
	case html.ElementNode:
		keys := make([]string, 0, len(n.Attr))
		for _, a := range n.Attr {
			keys = append(keys, fmt.Sprintf("%s:%s=%q", a.Namespace, a.Key, a.Val))
		}
		sort.Strings(keys)
		*out = append(*out, oracleEvent{desc: fmt.Sprintf("%d<%s:%s %s>", depth, n.Namespace, n.Data, strings.Join(keys, " "))})
		raw = raw || oracleRaw[n.Data]
	case html.TextNode:
		if k := len(*out) - 1; k >= 0 && (*out)[k].text && (*out)[k].raw == raw {
			(*out)[k].desc += n.Data
		} else {
			*out = append(*out, oracleEvent{desc: n.Data, text: true, raw: raw})
		}
		return
	case html.CommentNode:
		return
	case html.DoctypeNode:
		*out = append(*out, oracleEvent{desc: "doctype:" + n.Data})
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		flatten(c, depth+1, raw, out)
	}
}

func domEvents(t testing.TB, src string) []string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		t.Skipf("parse error: %v", err)
	}
	var ev []oracleEvent
	flatten(doc, 0, false, &ev)
	var res []string
	for i, e := range ev {
		d := e.desc
		if e.text && !e.raw {
			d = strings.Join(strings.FieldsFunc(d, func(r rune) bool {
				return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
			}), " ")
			if d == "" {
				continue
			}
			// Keep a marker for significant inner-edge whitespace.
			edge := e.desc
			if strings.ContainsAny(edge[:1], " \t\n\f\r") && i > 0 {
				d = "_" + d
			}
			if strings.ContainsAny(edge[len(edge)-1:], " \t\n\f\r") && i < len(ev)-1 {
				d += "_"
			}
		}
		if e.text {
			d = "T:" + d
		}
		res = append(res, d)
	}
	return res
}

func assertSameDOM(t testing.TB, in, out string) {
	t.Helper()
	a := domEvents(t, in)
	b := domEvents(t, out)
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Fatalf("DOM changed by minifyHTML\n in : %q\n out: %q\n dom(in) : %q\n dom(out): %q", in, out, a, b)
	}
}

func FuzzMinifyHTML(f *testing.F) {
	seeds := []string{
		// Text ending in ">" foster-parented out of a table keeps its trailing space.
		"<tABle 00000000000>0> ",
		"<table>0 ",
		"<p>a</p> <!--x--> ",
		`<svg><linearGradient id="g"><stop offset="0%" stop-color="#2298bd"/><stop offset="100%" stop-color="#0ed7b5"/></linearGradient></svg>`,
		`<img src="/a.png"/>`,
		`<a href="/">home</a>`,
		`<a href="/x/">x</a>`,
		`<div title='say "hi"' data-x="it's" data-y="a<b" data-z="a` + "`" + `b"></div>`,
		`<input value="" disabled>`,
		`<div content="a  b" title="x   y"></div>`,
		"<pre>  a\n   b  </pre><p>x   y</p>",
		"<textarea>  a\n   b  </textarea>",
		`<p>a</p><script>if (a  <  b) {}</script><p>b</p>`,
		`<style>a  { color : red }</style>`,
		`<!-- <pre> --><p>a    b</p><!-- </pre> -->`,
		`<p>a < b</p>`,
		`<script`,
		`<script>`,
		`<pre`,
		`<textarea`,
		`<a href=x/>t</a>`,
		`<br/><hr />`,
		`<!--k:1--><span>x</span><!--/k:1-->`,
		`<script><!-- <script> </script> --> </script>`,
		`<table> <tr> <td> a </td> </tr> </table>`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		assertSameDOM(t, in, minifyHTML(in))
	})
}
