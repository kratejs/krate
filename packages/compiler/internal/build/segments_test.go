package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestSegmentKindOfFile(t *testing.T) {
	cases := map[string]struct {
		kind SegmentKind
		ok   bool
	}{
		"404.tsx":       {SegmentNotFound, true},
		"not-found.tsx": {SegmentNotFound, true},
		"500.tsx":       {SegmentError, true},
		"error.tsx":     {SegmentError, true},
		"error.mdx":     {SegmentError, true},
		"errors.tsx":    {"", false},
		"index.tsx":     {"", false},
		"notfound.tsx":  {"", false},
	}
	for name, want := range cases {
		kind, ok := segmentKindOfFile(name)
		if ok != want.ok || kind != want.kind {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", name, kind, ok, want.kind, want.ok)
		}
	}
}

func TestSegmentPathsAreSlashNormalizedAndKeepGroups(t *testing.T) {
	pagesDir := filepath.Join(t.TempDir(), "src", "pages")
	page := filepath.Join(pagesDir, "(shop)", "blog", "[slug]", "not-found.tsx")
	if got := segmentHTMLRel(page, pagesDir); got != "(shop)/blog/[slug]/404.html" {
		t.Errorf("html rel = %q", got)
	}
	if got := segmentAssetOut(page, pagesDir); got != "(shop)/blog/[slug]/__404" {
		t.Errorf("asset out = %q", got)
	}
	if got := segmentURLDir(segmentDirKey(page, pagesDir)); got != "blog/[slug]" {
		t.Errorf("url dir = %q", got)
	}
	root := filepath.Join(pagesDir, "error.tsx")
	if got := segmentHTMLRel(root, pagesDir); got != "500.html" {
		t.Errorf("root error html = %q", got)
	}
	if got := pageToOutput(filepath.Join(pagesDir, "404.tsx"), pagesDir); got != "__404" {
		t.Errorf("pageToOutput(404) = %q", got)
	}
}

func TestLookupSegmentPageURLWalk(t *testing.T) {
	entries := []SegmentEntry{
		{Key: "", Dir: "", NotFound: "404.html", Error: "500.html"},
		{Key: "blog", Dir: "blog", NotFound: "blog/404.html"},
		{Key: "blog/new", Dir: "blog/new", NotFound: "blog/new/404.html"},
		{Key: "blog/[slug]", Dir: "blog/[slug]", NotFound: "blog/[slug]/404.html"},
		{Key: "docs/[...rest]", Dir: "docs/[...rest]", NotFound: "docs/[...rest]/404.html"},
		{Key: "(shop)", Dir: "", Group: true, NotFound: "(shop)/404.html"},
		{Key: "(shop)/cart", Dir: "cart", Group: true, NotFound: "(shop)/cart/404.html"},
	}
	cases := []struct {
		path     string
		kind     SegmentKind
		want     string
		wantSlug string
	}{
		{"/", SegmentNotFound, "404.html", ""},
		{"/nope", SegmentNotFound, "404.html", ""},
		{"/blog/", SegmentNotFound, "blog/404.html", ""},
		{"/blog", SegmentNotFound, "blog/404.html", ""},
		{"/blog/new/x/y", SegmentNotFound, "blog/new/404.html", ""},
		{"/blog/hello", SegmentNotFound, "blog/[slug]/404.html", "hello"},
		{"/blog/hello/deeper/still", SegmentNotFound, "blog/[slug]/404.html", "hello"},
		{"/docs", SegmentNotFound, "404.html", ""},
		{"/docs/a/b/c", SegmentNotFound, "docs/[...rest]/404.html", ""},
		{"/cart/x", SegmentNotFound, "404.html", ""},
		{"/blog/x", SegmentError, "500.html", ""},
	}
	for _, c := range cases {
		m, ok := LookupSegmentPage(entries, c.kind, c.path, false, "")
		if !ok || m.File != c.want {
			t.Errorf("%s %s: got (%q, %v), want %q", c.kind, c.path, m.File, ok, c.want)
			continue
		}
		if c.wantSlug != "" && m.Params["slug"] != c.wantSlug {
			t.Errorf("%s: slug = %q, want %q", c.path, m.Params["slug"], c.wantSlug)
		}
	}
	if _, ok := LookupSegmentPage(nil, SegmentNotFound, "/x", false, ""); ok {
		t.Error("no entries must not match")
	}
	if _, ok := LookupSegmentPage([]SegmentEntry{{Key: "blog", Dir: "blog", NotFound: "blog/404.html"}}, SegmentNotFound, "/x", false, ""); ok {
		t.Error("a nested page must not answer URLs outside its directory")
	}
}

func TestLookupSegmentPageRouteWalk(t *testing.T) {
	entries := []SegmentEntry{
		{Key: "", NotFound: "404.html"},
		{Key: "(shop)", Dir: "", Group: true, NotFound: "(shop)/404.html", Error: "(shop)/500.html"},
		{Key: "(shop)/cart", Dir: "cart", Group: true, NotFound: "(shop)/cart/404.html"},
		{Key: "blog", Dir: "blog", Error: "blog/500.html"},
	}
	cases := []struct {
		segDir string
		kind   SegmentKind
		want   string
	}{
		{"", SegmentNotFound, "404.html"},
		{"(shop)", SegmentNotFound, "(shop)/404.html"},
		{"(shop)/cart", SegmentNotFound, "(shop)/cart/404.html"},
		{"(shop)/cart/[id]", SegmentNotFound, "(shop)/cart/404.html"},
		{"(shop)/cart", SegmentError, "(shop)/500.html"},
		{"blog", SegmentNotFound, "404.html"},
		{"blog/[slug]", SegmentError, "blog/500.html"},
		{"other", SegmentError, ""},
	}
	for _, c := range cases {
		m, ok := LookupSegmentPage(entries, c.kind, "/ignored", true, c.segDir)
		if c.want == "" {
			if ok {
				t.Errorf("%s %s: got %q, want none", c.kind, c.segDir, m.File)
			}
			continue
		}
		if !ok || m.File != c.want {
			t.Errorf("%s %s: got (%q, %v), want %q", c.kind, c.segDir, m.File, ok, c.want)
		}
	}
}

func TestValidateSegmentPages(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string // substring of the error; "" means valid
	}{
		{"valid nested", map[string]string{"404.tsx": "x", "blog/404.tsx": "x", "blog/error.tsx": "x", "blog/[slug]/not-found.tsx": "x"}, ""},
		{"404 and not-found", map[string]string{"blog/404.tsx": "x", "blog/not-found.tsx": "x"}, "KR9001"},
		{"error and 500", map[string]string{"error.tsx": "x", "500.tsx": "x"}, "KR9001"},
		{"same extension twice", map[string]string{"404.tsx": "x", "404.mdx": "x"}, "KR9001"},
		{"two groups one dir", map[string]string{"(a)/blog/404.tsx": "x", "(b)/blog/404.tsx": "x"}, "KR9001"},
		{"group and plain", map[string]string{"(a)/404.tsx": "x", "404.tsx": "x"}, "KR9001"},
		{"group only", map[string]string{"(a)/blog/404.tsx": "x", "(b)/docs/404.tsx": "x"}, ""},
		{"in slot", map[string]string{"_layout.tsx": "x", "@modal/404.tsx": "x"}, "KR9002"},
		{"in intercepting", map[string]string{"@modal/(.)photo/error.tsx": "x"}, "KR9002"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, map[string]string{})
			pagesDir := filepath.Join(root, "src", "pages")
			files := map[string]string{}
			for k, v := range c.files {
				files["src/pages/"+k] = v
			}
			writeTree(t, root, files)
			err := validateSegmentPages(pagesDir)
			if c.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %s, got %v", c.want, err)
			}
		})
	}
}

func TestSegmentEntriesAreDeterministic(t *testing.T) {
	pagesDir := filepath.Join(t.TempDir(), "pages")
	pages := []string{
		filepath.Join(pagesDir, "blog", "error.tsx"),
		filepath.Join(pagesDir, "404.tsx"),
		filepath.Join(pagesDir, "blog", "not-found.tsx"),
		filepath.Join(pagesDir, "(g)", "x", "500.tsx"),
		filepath.Join(pagesDir, "index.tsx"),
	}
	a := segmentEntries(pages, pagesDir)
	for i, j := 0, len(pages)-1; i < j; i, j = i+1, j-1 {
		pages[i], pages[j] = pages[j], pages[i]
	}
	b := segmentEntries(pages, pagesDir)
	if len(a) != 3 || len(a) != len(b) {
		t.Fatalf("entries = %+v", a)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("entry %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
	if a[0].Key != "" || a[1].Key != "(g)/x" || a[2].Key != "blog" {
		t.Errorf("order = %+v", a)
	}
	if !a[1].Group || a[1].Dir != "x" || a[1].Error != "(g)/x/500.html" {
		t.Errorf("group entry = %+v", a[1])
	}
	if a[2].NotFound != "blog/404.html" || a[2].Error != "blog/500.html" {
		t.Errorf("blog entry = %+v", a[2])
	}
}

// A build writes the pages next to their directories, records the segment map
// and each route's source directory in the manifest, and keeps segment pages
// out of the route list.
func TestBuildWritesSegmentPagesAndManifest(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/_layout.tsx":           `export default function L({ children }: any) { return <div id="root-layout">{children}</div>; }`,
		"src/pages/index.tsx":             devPage("home"),
		"src/pages/404.tsx":               devPage("ROOT404"),
		"src/pages/blog/_layout.tsx":      `export default function L({ children }: any) { return <section id="blog-layout">{children}</section>; }`,
		"src/pages/blog/index.tsx":        devPage("blog"),
		"src/pages/blog/not-found.tsx":    devPage("BLOG404"),
		"src/pages/blog/[slug]/index.tsx": devPage("post"),
		"src/pages/blog/[slug]/404.tsx":   devPage("POST404"),
		"src/pages/(shop)/cart/index.tsx": devPage("cart"),
		"src/pages/(shop)/cart/404.tsx":   devPage("CART404"),
		"src/pages/(shop)/cart/500.tsx":   devPage("CART500"),
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for rel, want := range map[string][]string{
		"404.html":             {"ROOT404", "root-layout"},
		"blog/404.html":        {"BLOG404", "root-layout", "blog-layout"},
		"blog/[slug]/404.html": {"POST404", "blog-layout"},
		"(shop)/cart/404.html": {"CART404"},
		"(shop)/cart/500.html": {"CART500"},
	} {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s not written: %v", rel, err)
			continue
		}
		for _, w := range want {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s missing %q", rel, w)
			}
		}
		if !strings.Contains(string(data), "noindex") {
			t.Errorf("%s should be noindex", rel)
		}
		if strings.Contains(string(data), "canonical") {
			t.Errorf("%s must not carry a canonical link", rel)
		}
	}
	m := loadManifestForTest(t, cfg.OutDir)
	if len(m.Segments) != 4 {
		t.Fatalf("segments = %+v", m.Segments)
	}
	for _, p := range m.Pages {
		if isSegmentPageBase(strings.TrimSuffix(filepath.Base(p.Source), filepath.Ext(p.Source))) {
			t.Errorf("segment page %s leaked into the route list", p.Source)
		}
		if p.Route == "/blog/[slug]" && p.SegDir != "blog/[slug]" {
			t.Errorf("segDir of %s = %q", p.Route, p.SegDir)
		}
		if p.Route == "/cart" && p.SegDir != "(shop)/cart" {
			t.Errorf("segDir of %s = %q", p.Route, p.SegDir)
		}
	}
	// No route exists at the segment page URLs.
	for _, rel := range []string{"blog/404/index.html", "404/index.html"} {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, filepath.FromSlash(rel))); err == nil {
			t.Errorf("unexpected route directory %s", rel)
		}
	}
}

func loadManifestForTest(t *testing.T, outDir string) *Manifest {
	t.Helper()
	data, err := os.ReadFile(ManifestPath(outDir))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func TestErrorPlaceholdersMangled(t *testing.T) {
	ok := "<p>" + errSentinelMessage + "</p><i>" + errSentinelDigest + "</i>"
	if errorPlaceholdersMangled(ok) {
		t.Error("untouched placeholders are fine")
	}
	for _, bad := range []string{
		"<p>" + strings.ToLower(errSentinelMessage) + "</p>",
		"<p>" + errSentinelDigest[:6] + "</p>",
		"<p>x" + errSentinelName[2:] + "</p>",
	} {
		if !errorPlaceholdersMangled(bad) {
			t.Errorf("mangled placeholder not detected: %s", bad)
		}
	}
}

// An error component that lowercases the message cannot be baked with a
// placeholder: the page is built to read the error on the client, and no
// placeholder text ends up in the HTML.
func TestErrorPageThatTransformsTheMessageReadsItOnTheClient(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": devPage("home"),
		"src/pages/error.tsx": `export default function E({ error }: any) { return <p id="m">{String(error.message).toLowerCase()}</p>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "500.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "krate_err") {
		t.Errorf("placeholder text leaked into the page:\n%s", data)
	}
	js, _ := filepath.Glob(filepath.Join(root, "dist", "__500", "index.*.js"))
	if len(js) != 1 {
		t.Fatalf("the page should hydrate and read the injected error; scripts: %v", js)
	}
	code, _ := os.ReadFile(js[0])
	if !strings.Contains(string(code), "__krate_error") {
		t.Errorf("the page script should read the injected error block:\n%s", code)
	}
}

// Segment pages stay out of the sitemap, the route list and the route types,
// and the site graph shows each route's nearest pages.
func TestSegmentPagesStayOutOfRouteInventories(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/index.tsx":          devPage("home"),
		"src/pages/404.tsx":            devPage("ROOT404"),
		"src/pages/blog/index.tsx":     devPage("blog"),
		"src/pages/blog/not-found.tsx": devPage("BLOG404"),
		"src/pages/blog/error.tsx":     `export default function E({ error }: any) { return <p>{error.message}</p>; }`,
		"src/pages/blog/[slug].tsx":    `export default function P() { return <p>post</p>; }`,
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.SEO.BaseURL = "https://example.com"
	cfg.Plugins = []config.PluginConfig{{Name: "sitemap"}}
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	sitemap, err := os.ReadFile(filepath.Join(cfg.OutDir, "sitemap.xml"))
	if err != nil {
		t.Fatalf("sitemap.xml: %v", err)
	}
	for _, bad := range []string{"404", "500", "__", "not-found", "error"} {
		if strings.Contains(string(sitemap), bad) {
			t.Errorf("sitemap must not list segment pages (%q):\n%s", bad, sitemap)
		}
	}
	if !strings.Contains(string(sitemap), "https://example.com/blog/") {
		t.Errorf("sitemap lost the blog route:\n%s", sitemap)
	}

	g, err := b.SiteGraph()
	if err != nil {
		t.Fatal(err)
	}
	byRoute := map[string]SiteGraphRoute{}
	for _, r := range g.Routes {
		byRoute[r.Route] = r
		if strings.Contains(r.Route, "404") || strings.Contains(r.Route, "500") || strings.Contains(r.Route, "__") {
			t.Errorf("segment page leaked into the route list: %+v", r)
		}
	}
	if r := byRoute["/blog/[slug]"]; r.NotFound != "src/pages/blog/not-found.tsx" || r.Error != "src/pages/blog/error.tsx" {
		t.Errorf("nearest pages of /blog/[slug] = %q, %q", r.NotFound, r.Error)
	}
	if r := byRoute["/"]; r.NotFound != "src/pages/404.tsx" || r.Error != "" {
		t.Errorf("nearest pages of / = %q, %q", r.NotFound, r.Error)
	}
	if len(g.Segments) != 2 || g.Segments[0].Dir != "/" || g.Segments[1].Dir != "/blog" ||
		g.Segments[1].NotFound != "src/pages/blog/not-found.tsx" || g.Segments[1].Error != "src/pages/blog/error.tsx" {
		t.Errorf("segments = %+v", g.Segments)
	}

	// Route types are generated for routes only.
	if errs := GenerateTypes(root, cfg); len(errs) > 0 {
		t.Fatalf("GenerateTypes: %v", errs)
	}
	types, err := os.ReadFile(filepath.Join(root, ".krate", "types", "routes.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(types), "404") || strings.Contains(string(types), "500") || strings.Contains(string(types), "__") {
		t.Errorf("route types must not include segment pages:\n%s", types)
	}
	if !strings.Contains(string(types), "/blog/") && !strings.Contains(string(types), `"/blog`) {
		t.Errorf("route types lost /blog:\n%s", types)
	}
}

// Adding an error.tsx changes the pages below it, so the build cache must not
// replay them.
func TestRouteSignatureSeesErrorBoundaries(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/blog/index.tsx": devPage("blog"),
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	b := New(root, cfg)
	page := filepath.Join(cfg.PagesDir, "blog", "index.tsx")
	before := b.routeSignature(page)
	routeDirSigCache.Range(func(k, _ any) bool { routeDirSigCache.Delete(k); return true })
	writeTree(t, root, map[string]string{"src/pages/blog/error.tsx": devPage("err")})
	if after := b.routeSignature(page); after == before {
		t.Error("an added error.tsx must change the route signature of the pages below it")
	}
}

// A page that cannot be prerendered still fails the build; the hint says why
// the segment's error page does not stand in for it.
func TestBuildTimeThrowNamesTheSegmentErrorPage(t *testing.T) {
	err := buildErr(t, map[string]string{
		"src/pages/blog/error.tsx": `export default function E({ error }: any) { return <p>{error.message}</p>; }`,
		"src/pages/blog/index.tsx": `function Broken(props: any) { if (!props.data) throw new Error('no data for ' + props.label); return <p>{props.data}</p>; }
export default function Page() { return <main><Broken label="z" /></main>; }`,
	})
	if err == nil {
		t.Fatal("a page that throws while prerendered must fail the build")
	}
	if !strings.Contains(err.Error(), "[KR3010]") || !strings.Contains(err.Error(), "no data for z") {
		t.Errorf("error = %v", err)
	}
	var hint string
	for _, d := range DiagnosticsFromError(err) {
		hint += d.Hint
	}
	for _, want := range []string{"src/pages/blog/error.tsx", "not used as a build-time fallback"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint should mention %q: %q", want, hint)
		}
	}
}
