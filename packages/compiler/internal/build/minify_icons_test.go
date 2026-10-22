package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/icons"
	"golang.org/x/net/html"
)

// TestMinifiedGradientIconKeepsSiblingStops runs an Iconify-style gradient
// icon (compact "/> self-closing stops) through the icon sanitizer and the
// HTML minifier and checks the stops stay siblings with intact colors.
func TestMinifiedGradientIconKeepsSiblingStops(t *testing.T) {
	root := t.TempDir()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><defs><linearGradient id="g"><stop offset="0%" stop-color="#2298bd"/><stop offset="100%" stop-color="#0ed7b5"/></linearGradient></defs><path fill="url(#g)" d="M0 0h24v24H0z"/></svg>`
	if err := os.MkdirAll(filepath.Join(root, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "icons", "grad.svg"), []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	ic, err := icons.ResolveIcon(root, "grad")
	if err != nil {
		t.Fatal(err)
	}
	page := `<svg viewBox="` + ic.ViewBox + `">` + ic.Inner + `</svg>`
	out := minifyHTML(page)

	doc, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	var colors []string
	var walk func(n *html.Node, inStop bool)
	walk = func(n *html.Node, inStop bool) {
		isStop := n.Type == html.ElementNode && n.Data == "stop"
		if isStop {
			if inStop {
				t.Fatalf("nested stop in minified output:\n%s", out)
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
	if len(colors) != 2 || colors[0] != "#2298bd" || colors[1] != "#0ed7b5" {
		t.Errorf("stop colors = %v in:\n%s", colors, out)
	}
}
