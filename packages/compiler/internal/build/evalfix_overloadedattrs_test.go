package build

import (
	"strings"
	"testing"
)

// download and capture are flags when true and carry a value otherwise, as in
// React: true renders an empty attribute and false renders none.
func TestOverloadedBooleanAttributes(t *testing.T) {
	html, _ := buildPlain(t, map[string]string{"src/pages/index.tsx": `export default function P() {
	return <div><a download href="/a">1</a><a download={true} href="/b">2</a><a download="f.txt" href="/c">3</a><a download={false} href="/d">4</a><input capture={true} /></div>;
}
`})
	for _, want := range []string{`<a download="" href=/a>1</a>`, `<a download="" href=/b>2</a>`, `<a download=f.txt href=/c>3</a>`, `<a href=/d>4</a>`, `<input capture=""/>`} {
		if !strings.Contains(html, want) {
			t.Errorf("want %s in:\n%s", want, rootOf(t, html))
		}
	}
}
