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

func TestBrokenAnchor(t *testing.T) {
	page := Page{Route: "/", HTML: doc(`<h2 id="ok">A</h2><a href="#ok">good</a><a href="#nope">bad</a>`, "")}
	fs := ruleBrokenAnchor(&page, &Config{})
	if len(fs) != 1 {
		t.Fatalf("expected 1 broken-anchor finding, got %+v", fs)
	}
}

func TestOGImage(t *testing.T) {
	cases := []struct {
		html string
		want int
	}{
		{doc(`<h1>x</h1>`, ``), 1},
		{doc(`<h1>x</h1>`, `<meta property="og:image" content="/rel.png">`), 1},
		{doc(`<h1>x</h1>`, `<meta property="og:image" content="https://x.test/og.png">`), 0},
	}
	for i, c := range cases {
		page := Page{Route: "/", HTML: c.html}
		if got := len(ruleOGImage(&page, &Config{})); got != c.want {
			t.Errorf("case %d: got %d findings, want %d", i, got, c.want)
		}
	}
}

func TestDuplicateMeta(t *testing.T) {
	p1 := Page{Route: "/a", HTML: doc(`<h1>a</h1>`, `<title>Same</title><meta name="description" content="dup">`)}
	p2 := Page{Route: "/b", HTML: doc(`<h1>b</h1>`, `<title>Same</title><meta name="description" content="dup">`)}
	fs := duplicateMetaFindings([]Page{p1, p2})
	if len(fs) != 2 {
		t.Fatalf("expected duplicate title + description findings, got %+v", fs)
	}
}

func TestResolveHrefRelative(t *testing.T) {
	cases := map[string]string{
		"other": "/docs/other",
		"./x":   "/docs/x",
		"../up": "/up",
	}
	for target, want := range cases {
		if got := resolveHref("/docs/guide", target); got != want {
			t.Errorf("resolveHref(/docs/guide, %q) = %q, want %q", target, got, want)
		}
	}
	if got := resolveHref("/", "about"); got != "/about" {
		t.Errorf("resolveHref(/, about) = %q, want /about", got)
	}
}

func TestBrokenLinkSkipsApiAndNonPageRoutes(t *testing.T) {
	page := Page{Route: "/", HTML: doc(`<a href="/api/subscribe">api</a><a href="/__krate/status">dev</a><a href="/real">real</a>`, "")}
	cfg := Config{Active: true, Routes: map[string]bool{"/": true}, Rules: map[string]Severity{}, Categories: map[string]Severity{}, Ignore: map[string]bool{}}
	findings, _ := Run(cfg, []Page{page})
	n := 0
	for _, f := range findings {
		if f.Rule == "seo/broken-link" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected only /real flagged, got %d: %+v", n, findings)
	}
}
