package check

import "testing"

func TestBrokenLink(t *testing.T) {
	page := Page{Route: "/", HTML: doc(`<a href="/missing/">bad</a><a href="/about/">ok</a><a href="https://x.test/">ext</a><a href="/logo.png">asset</a>`, "")}
	cfg := Config{Active: true, Routes: map[string]bool{"/": true, "/about/": true, "/about": true}, Rules: map[string]Severity{}, Categories: map[string]Severity{}, Ignore: map[string]bool{}}
	findings, err := Run(cfg, []Page{page})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range findings {
		if f.Rule == "seo/broken-link" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected 1 broken-link finding, got %d: %+v", n, findings)
	}
}
