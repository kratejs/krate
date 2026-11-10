package build

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPageAssets(t *testing.T) {
	html := `<html><head>
<link rel="stylesheet" href="/assets/app.css?v=1">
<link rel="modulepreload" href="/chunk-a.js">
<link rel="icon" href="/favicon.ico">
<script type="module" src="/page.js"></script>
<script src='https://cdn.example.com/x.js'></script>
<script>inline()</script>
<link rel=stylesheet href=/plain.css>
</head></html>`
	scripts, styles := pageAssets(html)
	wantScripts := []string{"/chunk-a.js", "/page.js", "https://cdn.example.com/x.js"}
	if strings.Join(scripts, ",") != strings.Join(wantScripts, ",") {
		t.Errorf("scripts = %v, want %v", scripts, wantScripts)
	}
	wantStyles := []string{"/assets/app.css?v=1", "/plain.css"}
	if strings.Join(styles, ",") != strings.Join(wantStyles, ",") {
		t.Errorf("styles = %v, want %v", styles, wantStyles)
	}
}

func TestSummarizeRealBuild(t *testing.T) {
	cfg := buildContractSite(t, true)
	root := filepath.Dir(cfg.PagesDir)
	root = filepath.Dir(root)
	b := New(root, cfg)
	s, err := b.Summarize()
	if err != nil {
		t.Fatal(err)
	}
	byRoute := map[string]RouteSize{}
	for _, r := range s.Routes {
		byRoute[r.Route] = r
	}
	home, counter := byRoute["/"], byRoute["/counter"]
	if home.HTML == 0 || counter.HTML == 0 {
		t.Fatalf("pages not measured: %+v", s.Routes)
	}
	if counter.JS == 0 || counter.GzipJS == 0 || counter.GzipJS >= counter.JS {
		t.Errorf("counter page should ship gzip-able JS, got js=%d gzip=%d", counter.JS, counter.GzipJS)
	}
	if s.UniqueGzip == 0 || s.UniqueGzip > home.Gzip()+counter.Gzip() {
		t.Errorf("unique total %d must be positive and at most the sum of the pages %d", s.UniqueGzip, home.Gzip()+counter.Gzip())
	}
	table := FormatSummary(s, nil)
	for _, want := range []string{"Route", "/counter", "Total gzip", "shipped in total"} {
		if !strings.Contains(table, want) {
			t.Errorf("table misses %q:\n%s", want, table)
		}
	}
}

func TestSummaryDiff(t *testing.T) {
	route := func(name string, gz int64) RouteSize { return RouteSize{Route: name, Mode: "static", GzipHTML: gz} }
	prev := &BuildSummary{Routes: []RouteSize{route("/", 1000), route("/gone", 500), route("/same", 700)}, UniqueGzip: 2200}
	cur := &BuildSummary{Routes: []RouteSize{route("/", 1500), route("/new", 300), route("/same", 700)}, UniqueGzip: 2500}
	out := FormatSummary(cur, prev)
	for _, want := range []string{"vs last", "+500 B", "new", "removed since last build: /gone", "+300 B (last build 2.2 kB)"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff output misses %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "=") {
		t.Errorf("an unchanged route should show =:\n%s", out)
	}
	if strings.Contains(FormatSummary(cur, nil), "vs last") {
		t.Error("first build has nothing to compare with")
	}
}

func TestSummaryRowLimit(t *testing.T) {
	s := &BuildSummary{}
	for i := 0; i < 40; i++ {
		s.Routes = append(s.Routes, RouteSize{Route: "/p" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Mode: "static", GzipHTML: int64(100 + i)})
	}
	out := FormatSummary(s, nil)
	if !strings.Contains(out, "and 15 more route(s)") {
		t.Errorf("long tables are cut to the heaviest routes:\n%s", out)
	}
}

func TestSaveAndLoadSummary(t *testing.T) {
	root := t.TempDir()
	if LoadSummary(root) != nil {
		t.Fatal("no summary yet")
	}
	want := &BuildSummary{Routes: []RouteSize{{Route: "/", Mode: "static", GzipHTML: 42}}, UniqueGzip: 42}
	if err := SaveSummary(root, want); err != nil {
		t.Fatal(err)
	}
	got := LoadSummary(root)
	if got == nil || len(got.Routes) != 1 || got.Routes[0].GzipHTML != 42 || got.UniqueGzip != 42 {
		t.Errorf("round trip = %+v", got)
	}
}
