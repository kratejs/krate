package icons

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const gradientSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0%" stop-color="#2298bd"/><stop offset="100%" stop-color="#0ed7b5"/></linearGradient></defs><path fill="url(#g)" d="M0 0h24v24H0z"/></svg>`

// stopsOf parses markup and returns the stop-color of every <stop>, failing if
// any stop is nested inside another stop.
func stopsOf(t *testing.T, markup string) []string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader("<svg>" + markup + "</svg>"))
	if err != nil {
		t.Fatal(err)
	}
	var colors []string
	var walk func(n *html.Node, inStop bool)
	walk = func(n *html.Node, inStop bool) {
		isStop := n.Type == html.ElementNode && n.Data == "stop"
		if isStop {
			if inStop {
				t.Fatalf("stop element nested inside another stop:\n%s", markup)
			}
			for _, a := range n.Attr {
				if a.Key == "stop-color" {
					colors = append(colors, a.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inStop || isStop)
		}
	}
	walk(doc, false)
	return colors
}

func TestGradientIconSiblingStops(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "icons", "grad.svg"), gradientSVG)
	ic, err := ResolveIcon(root, "grad")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ic.Inner, `"/`) {
		t.Errorf("self-closing slash glued to a quoted value: %s", ic.Inner)
	}
	got := stopsOf(t, ic.Inner)
	if len(got) != 2 || got[0] != "#2298bd" || got[1] != "#0ed7b5" {
		t.Errorf("stop colors = %v", got)
	}
}

func TestSanitizeTagSelfClosing(t *testing.T) {
	cases := map[string]string{
		`<stop offset="0%" stop-color="#2298bd"/>`: `<stop offset="0%" stop-color="#2298bd" />`,
		`<path d=M0/>`:                     `<path d="M0/">`,
		`<path d=M0 />`:                    `<path d="M0" />`,
		`<path fill-rule d="x"/>`:          `<path fill-rule d="x" />`,
		`<g id='a"b'>`:                     `<g id='a"b'>`,
		`<path onclick=alert(1) d="x">`:    `<path d="x">`,
		`<use href=javascript:alert(1) />`: `<use />`,
		`<rect width=10 height=10>`:        `<rect width="10" height="10">`,
	}
	for in, want := range cases {
		if got := sanitizeTag(in); got != want {
			t.Errorf("sanitizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeSVGUnquotedAndTruncated(t *testing.T) {
	out := sanitizeSVG(`<g fill=red><path d=M0 onload=x/></g>`)
	if strings.Contains(out, "onload") {
		t.Errorf("unquoted event handler survived: %s", out)
	}
	for _, in := range []string{`<path d="x`, `<path d=`, `<`, `a < b`, `<path /`} {
		_ = sanitizeSVG(in) // must not panic
	}
	if got := sanitizeSVG("a < b"); strings.Contains(got, "< b") {
		t.Errorf("lone '<' left unescaped: %q", got)
	}
}
