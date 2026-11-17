package build

// Output audit and per-route size budgets.
//
// TestOutputAudit builds one fixture site (the docs theme with markdown code,
// math and a diagram, a Tailwind page, a React page and a component-library
// page) and checks properties of the emitted output that regress quietly:
// third-party loads, the runtime feature set, stray elements in markdown,
// duplicate ids and dangling references, preload targets, cache headers per
// deploy adapter and the head order. TestOutputBudgets compares the gzip size
// of every route (HTML, CSS, own JS and the full transitive JS) with
// testdata/output_budgets.json.
//
// Updating on purpose:
//
//   - Runtime features: edit auditExpectedRuntimeFeatures below.
//   - Size budgets: a value may grow by 3% or 512 B (whichever is larger)
//     before the test fails; shrinking always passes and logs "budgets
//     improved". Refresh the snapshot after a deliberate change with
//     KRATE_BUDGETS_UPDATE=1 go test ./internal/build -run TestOutputBudgets
//     (or pass -update-budgets) and commit testdata/output_budgets.json.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"golang.org/x/net/html"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/deploy"
)

var updateBudgets = flag.Bool("update-budgets", false, "rewrite testdata/output_budgets.json from the current build")

// auditExpectedRuntimeFeatures is the set of optional runtime features the
// fixture site ships (see optionalRuntimeFeatures). When a change adds or
// drops one on purpose, edit this list.
var auditExpectedRuntimeFeatures = []string{"list"}

// auditMayUseCDN reports whether a fixture route may carry the mermaid and
// KaTeX loaders. A site that uses either feature ships the loader on every
// docs page (client navigation does not re-run head scripts), so all docs
// routes may; the pages outside the docs section must not name the CDN.
func auditMayUseCDN(route string) bool { return route == "/docs" || strings.HasPrefix(route, "/docs/") }

const outputBudgetFile = "testdata/output_budgets.json"

type auditSite struct {
	root    string
	cfg     *config.Config
	builder *Builder
	pages   map[string]string // route -> html file path
	// results are the page results the runtime chunk was derived from.
	results []*PageResult
}

var (
	auditOnce sync.Once
	auditSt   *auditSite
	auditErr  error
)

// buildAuditSite builds the fixture once per test binary. The project lives
// inside examples/ so @krate/core, the docs theme and the runtime resolve from
// its node_modules, like the headless tests.
func buildAuditSite(t *testing.T) *auditSite {
	t.Helper()
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	examples := filepath.Join(repoRoot, "examples")
	if _, err := os.Stat(filepath.Join(examples, "node_modules", "@krate", "base-docs-theme")); err != nil {
		skipOrFail(t, "examples workspace is not installed: "+err.Error())
	}
	ensureRuntimeDist(t)
	auditOnce.Do(func() {
		root, err := os.MkdirTemp(examples, ".krate-audit-")
		if err != nil {
			auditErr = err
			return
		}
		for p, c := range auditFixtureFiles {
			full := filepath.Join(root, filepath.FromSlash(p))
			if auditErr = os.MkdirAll(filepath.Dir(full), 0o755); auditErr != nil {
				return
			}
			if auditErr = os.WriteFile(full, []byte(c), 0o644); auditErr != nil {
				return
			}
		}
		cfg, err := config.Load(root)
		if err != nil {
			auditErr = err
			return
		}
		b := New(root, cfg)
		var results []*PageResult
		captureRuntimeResults(b, &results)
		if auditErr = b.BuildAll(); auditErr != nil {
			return
		}
		st := &auditSite{root: root, cfg: cfg, builder: b, pages: map[string]string{}, results: results}
		routes, err := b.RouteList()
		if err != nil {
			auditErr = err
			return
		}
		for _, r := range routes {
			if !r.Built {
				continue
			}
			name := strings.Trim(r.Route, "/")
			for _, cand := range []string{filepath.Join(cfg.OutDir, filepath.FromSlash(name), "index.html"), filepath.Join(cfg.OutDir, filepath.FromSlash(name)+".html")} {
				if _, err := os.Stat(cand); err == nil {
					st.pages[r.Route] = cand
					break
				}
			}
		}
		auditSt = st
	})
	if auditErr != nil {
		if strings.Contains(auditErr.Error(), "api.iconify.design") {
			skipOrFail(t, "the docs theme icons need network access: "+auditErr.Error())
		}
		t.Fatalf("building the audit fixture: %v", auditErr)
	}
	return auditSt
}

// cleanupAuditSite removes the fixture project; TestMain calls it after
// the last test, so the tests that share the build can run in any order.
func cleanupAuditSite() {
	if auditSt != nil {
		_ = os.RemoveAll(auditSt.root)
	}
}

func TestOutputAudit(t *testing.T) {
	site := buildAuditSite(t)
	routes := make([]string, 0, len(site.pages))
	for r := range site.pages {
		routes = append(routes, r)
	}
	sort.Strings(routes)
	if len(routes) < 5 {
		t.Fatalf("fixture built only %d pages: %v", len(routes), routes)
	}
	docs := map[string]*html.Node{}
	raw := map[string]string{}
	for _, r := range routes {
		data, err := os.ReadFile(site.pages[r])
		if err != nil {
			t.Fatal(err)
		}
		raw[r] = string(data)
		doc, err := html.Parse(strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("%s: %v", r, err)
		}
		docs[r] = doc
	}

	t.Run("runtime features", func(t *testing.T) {
		got := append([]string(nil), site.builder.runtimeFeatures...)
		want := append([]string(nil), auditExpectedRuntimeFeatures...)
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("the fixture ships runtime features %v, expected %v.\nIf this change is intended, edit auditExpectedRuntimeFeatures in output_audit_test.go.", got, want)
		}
	})

	t.Run("runtime features cover the old scan", func(t *testing.T) {
		checkLegacyCoverage(t, site.results)
	})

	t.Run("third-party loads", func(t *testing.T) {
		for _, r := range routes {
			if !auditMayUseCDN(r) && strings.Contains(raw[r], cspCDNOrigin) {
				t.Errorf("%s names %s but uses no mermaid or math", r, cspCDNOrigin)
			}
			walkHTML(docs[r], func(n *html.Node) {
				switch n.Data {
				case "script":
					if src := attr(n, "src"); isRemoteURL(src) {
						t.Errorf("%s: <script src=%q> loads third-party code", r, src)
					}
					if n.FirstChild != nil && staticRemoteImportRe.MatchString(n.FirstChild.Data) {
						t.Errorf("%s: an inline script statically imports a remote module; a loader must use dynamic import()", r)
					}
				case "link":
					rel := strings.ToLower(attr(n, "rel"))
					if (strings.Contains(rel, "stylesheet") || strings.Contains(rel, "preload")) && isRemoteURL(attr(n, "href")) && !auditMayUseCDN(r) {
						t.Errorf("%s: <link rel=%s href=%q> loads a third-party resource", r, rel, attr(n, "href"))
					}
				}
			})
		}
	})

	t.Run("elements", func(t *testing.T) {
		for _, r := range routes {
			walkHTML(docs[r], func(n *html.Node) {
				if n.Namespace != "" || strings.Contains(n.Data, "-") || knownHTMLElements[n.Data] {
					return
				}
				t.Errorf("%s: unknown element <%s>: raw angle brackets in prose are parsed as a tag and swallow the text", r, n.Data)
			})
		}
	})

	t.Run("ids and references", func(t *testing.T) {
		for _, r := range routes {
			ids := map[string]int{}
			var refs [][2]string
			walkHTMLSkipTemplates(docs[r], func(n *html.Node) {
				if id := attr(n, "id"); id != "" {
					ids[id]++
				}
				for _, name := range []string{"aria-controls", "aria-labelledby", "for"} {
					for _, v := range strings.Fields(attr(n, name)) {
						refs = append(refs, [2]string{name, v})
					}
				}
			})
			for id, n := range ids {
				if n > 1 {
					t.Errorf("%s: id %q is used %d times", r, id, n)
				}
			}
			for _, ref := range refs {
				if ids[ref[1]] == 0 {
					t.Errorf("%s: %s=%q points at no element on the page", r, ref[0], ref[1])
				}
			}
		}
	})

	t.Run("preloads resolve", func(t *testing.T) {
		for _, r := range routes {
			walkHTML(docs[r], func(n *html.Node) {
				if n.Data != "link" {
					return
				}
				rel := strings.ToLower(attr(n, "rel"))
				if !strings.Contains(rel, "preload") {
					return
				}
				href := attr(n, "href")
				if href == "" || isRemoteURL(href) {
					return
				}
				if i := strings.IndexAny(href, "?#"); i >= 0 {
					href = href[:i]
				}
				rel2 := strings.TrimPrefix(strings.TrimPrefix(href, strings.TrimSuffix(site.cfg.BaseURLPath(), "/")), "/")
				if _, err := os.Stat(filepath.Join(site.cfg.OutDir, filepath.FromSlash(rel2))); err != nil {
					t.Errorf("%s: <link rel=%s href=%q> names no file in the output", r, rel, href)
				}
			})
		}
	})

	t.Run("head", func(t *testing.T) {
		for _, r := range routes {
			var titles int
			walkHTML(docs[r], func(n *html.Node) {
				if n.Data == "title" && n.Namespace == "" {
					titles++
				}
			})
			if titles != 1 {
				t.Errorf("%s: %d <title> elements, want exactly 1", r, titles)
			}
			var head *html.Node
			walkHTML(docs[r], func(n *html.Node) {
				if n.Data == "head" && head == nil {
					head = n
				}
			})
			var kids []*html.Node
			for c := head.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode {
					kids = append(kids, c)
				}
			}
			if len(kids) == 0 || kids[0].Data != "meta" || attr(kids[0], "charset") == "" {
				t.Errorf("%s: <meta charset> must be the first element of <head>", r)
				continue
			}
			for i, k := range kids {
				if k.Data == "meta" && strings.EqualFold(attr(k, "http-equiv"), "content-security-policy") && i != 1 {
					t.Errorf("%s: the CSP <meta> is child %d of <head>, want right after charset", r, i)
				}
			}
			if site.cfg.CSP.Enabled && !strings.Contains(strings.ToLower(raw[r]), "http-equiv=\"content-security-policy\"") && !strings.Contains(strings.ToLower(raw[r]), "http-equiv=content-security-policy") {
				t.Errorf("%s: csp is enabled but the page has no CSP <meta>", r)
			}
		}
	})

	t.Run("immutable headers per adapter", func(t *testing.T) {
		hashed := deploy.LoadHashedAssets(site.cfg.OutDir)
		if len(hashed) < 5 {
			t.Fatalf("manifest lists only %d hashed assets", len(hashed))
		}
		for _, target := range []string{"netlify", "cloudflare", "vercel"} {
			t.Run(target, func(t *testing.T) {
				dir := t.TempDir()
				if err := copyTree(t, site.cfg.OutDir, dir); err != nil {
					t.Fatal(err)
				}
				if _, err := deploy.Emit(target, site.cfg, dir, dir); err != nil {
					t.Fatalf("deploy.Emit(%s): %v", target, err)
				}
				var immutable []*regexp.Regexp
				if target == "vercel" {
					immutable = vercelImmutableSources(t, filepath.Join(dir, "vercel.json"))
				} else {
					immutable = headersFileImmutableSources(t, filepath.Join(dir, "_headers"))
				}
				for _, asset := range hashed {
					ok := false
					for _, re := range immutable {
						if re.MatchString(asset) {
							ok = true
							break
						}
					}
					if !ok {
						t.Errorf("%s: hashed asset %s has no immutable Cache-Control rule", target, asset)
					}
				}
			})
		}
	})
}

// TestOutputBudgets compares per-route gzip sizes with the committed snapshot.
func TestOutputBudgets(t *testing.T) {
	site := buildAuditSite(t)
	sum, err := site.builder.Summarize()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]routeBudget{}
	for _, r := range sum.Routes {
		got[r.Route] = routeBudget{HTML: r.GzipHTML, CSS: r.GzipCSS, OwnJS: r.GzipOwnJS, JS: r.GzipJS}
	}
	if len(got) < 5 {
		t.Fatalf("summary lists %d routes", len(got))
	}
	if *updateBudgets || os.Getenv("KRATE_BUDGETS_UPDATE") == "1" {
		data, _ := json.MarshalIndent(got, "", "  ")
		if err := os.WriteFile(outputBudgetFile, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", outputBudgetFile)
		return
	}
	data, err := os.ReadFile(outputBudgetFile)
	if err != nil {
		t.Fatalf("%v\nCreate the snapshot with KRATE_BUDGETS_UPDATE=1 go test ./internal/build -run TestOutputBudgets", err)
	}
	var want map[string]routeBudget
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("%s: %v", outputBudgetFile, err)
	}
	var grew, improved []string
	for route, w := range want {
		g, ok := got[route]
		if !ok {
			grew = append(grew, fmt.Sprintf("%s: route no longer built", route))
			continue
		}
		for _, f := range []struct {
			name      string
			got, want int64
		}{{"html", g.HTML, w.HTML}, {"css", g.CSS, w.CSS}, {"own js", g.OwnJS, w.OwnJS}, {"total js", g.JS, w.JS}} {
			tol := max(f.want*3/100, 512)
			switch {
			case f.got > f.want+tol:
				grew = append(grew, fmt.Sprintf("%s %s: %d B gzip, budget %d B (+%d, tolerance %d)", route, f.name, f.got, f.want, f.got-f.want, tol))
			case f.got < f.want-tol:
				improved = append(improved, fmt.Sprintf("%s %s: %d B, was %d B", route, f.name, f.got, f.want))
			}
		}
	}
	for route := range got {
		if _, ok := want[route]; !ok {
			grew = append(grew, route+": route is not in the snapshot")
		}
	}
	sort.Strings(grew)
	if len(improved) > 0 {
		sort.Strings(improved)
		t.Logf("budgets improved: run with -update-budgets or KRATE_BUDGETS_UPDATE=1 to refresh %s\n  %s", outputBudgetFile, strings.Join(improved, "\n  "))
	}
	if len(grew) > 0 {
		t.Errorf("output size grew past the budget (gzip bytes):\n  %s\nIf the growth is intended, refresh the snapshot with KRATE_BUDGETS_UPDATE=1 go test ./internal/build -run TestOutputBudgets and commit %s.", strings.Join(grew, "\n  "), outputBudgetFile)
	}
}

var auditFixtureFiles = map[string]string{
	"krate.config.ts": `import { defineConfig, docs, sitemap } from '@krate/core';
import { baseDocsTheme } from '@krate/base-docs-theme';
export default defineConfig({
  entry: "src/pages/index.tsx", outDir: "dist", pagesDir: "src/pages", publicDir: "public", minify: true,
  markdown: { gfm: true, headingAnchors: true, admonitions: true, codeHighlight: true, math: true, mermaid: true },
  tailwind: { enabled: true },
  csp: { enabled: true },
  site: { url: "https://example.com", name: "Audit", description: "Audit fixture." },
  plugins: [
    sitemap({ baseUrl: "https://example.com" }),
    docs({ contentDir: "src/content/docs", title: "Audit", theme: baseDocsTheme(), search: { enabled: true, engine: "docfind", maxResults: 8 } }),
  ],
});
`,
	"src/pages/index.tsx": `export default function Home() { return <main><h1>Home</h1><p class="p-4 text-lg font-bold text-sky-600">Tailwind page</p></main>; }`,
	"src/pages/react-island.tsx": `import { useState } from 'react';
export default function Island() {
  const [n, setN] = useState(0);
  return <main><h1>Island</h1><button onClick={() => setN(n + 1)}>n={n}</button></main>;
}`,
	"src/pages/components.tsx": `import { Card, Aside } from '@krate/components';
export default function C() { return <main><h1>Components</h1><Card title="A">body</Card><Aside type="note">hello</Aside></main>; }`,
	"src/content/docs/index.md": "---\ntitle: Welcome\norder: 1\n---\n\n# Welcome\n\nPlain page with no code, math or diagrams.\n",
	// Headings and code samples that name runtime APIs, server action markup
	// and a "use server" directive: text, which must not add runtime features.
	"src/content/docs/apis.md":  "---\ntitle: APIs\norder: 3\n---\n\n# APIs\n\n## requestFormReset\n\n## Children, keys, and refs\n\n## krateHighlight and useParams\n\n```tsx\nasync function save() {\n  \"use server\";\n}\nexport default () => <form action={save} data-krate-action />;\n```\n",
	"src/content/docs/guide.md": "---\ntitle: Guide\norder: 2\n---\n\n# Guide\n\nWrite generics as `Array<appId>` or &lt;Profiler&gt; in prose.\n\n```ts\nconst a: Array<number> = [1];\n```\n\n```mermaid\ngraph TD; A-->B;\n```\n\nInline $x^2$ and\n\n$$\n\\int_0^1 x dx\n$$\n\n## Second\n\ntext\n",
}

type routeBudget struct {
	HTML  int64 `json:"html"`
	CSS   int64 `json:"css"`
	OwnJS int64 `json:"ownJs"`
	JS    int64 `json:"js"`
}

// TestSummaryCountsTransitiveChunks pins the size table's JS column to the
// scripts a page loads through imports, not just the ones its HTML names.
func TestSummaryCountsTransitiveChunks(t *testing.T) {
	files := map[string]string{
		"/p/index.js":          `import{a}from"./dep.js";import"/chunks/shared.js";import x from"preact";import("/chunks/island_aa.js");import("/chunks/lazy.js");a(x)`,
		"/p/dep.js":            `export const a=1;import"../chunks/deep.js"`,
		"/chunks/shared.js":    `export default 1`,
		"/chunks/deep.js":      `export default 2`,
		"/chunks/island_aa.js": `import"./isl-bb.js"`,
		"/chunks/isl-bb.js":    `export default 3`,
		"/chunks/lazy.js":      `export default 4`,
		"/chunks/pre.js":       `export default 5`,
		"/chunks/preact.js":    `export default 6`,
	}
	read := func(url string) (string, bool) { s, ok := files[url]; return s, ok }
	got := transitiveScripts([]string{"/p/index.js", "/chunks/pre.js"}, map[string]string{"preact": "/chunks/preact.js"}, read)
	want := []string{"/p/index.js", "/chunks/pre.js", "/p/dep.js", "/chunks/shared.js", "/chunks/preact.js", "/chunks/island_aa.js", "/chunks/deep.js", "/chunks/isl-bb.js"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("transitiveScripts = %v\nwant %v (lazy.js is a dynamic import that does not run on load)", got, want)
	}
}

func TestSummaryTableTransitiveFootnote(t *testing.T) {
	s := &BuildSummary{Routes: []RouteSize{
		{Route: "/", Mode: "static", GzipHTML: 1000, GzipJS: 5000, GzipOwnJS: 800, GzipCSS: 200},
		{Route: "/docs/x", Mode: "static", GzipHTML: 1000, GzipJS: 5000, GzipCSS: 200, ThirdParty: true},
	}, UniqueGzip: 7000}
	out := FormatSummary(s, nil)
	for _, want := range []string{"including the chunks its scripts import", "1 route(s) load mermaid or KaTeX from a CDN"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	s.Routes[1].ThirdParty = false
	if strings.Contains(FormatSummary(s, nil), "KaTeX") {
		t.Error("footnote shown without a third-party route")
	}
}

var staticRemoteImportRe = regexp.MustCompile(`(?:\bfrom|\bimport)\s*["'](?:https?:)?//`)

func isRemoteURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "//")
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func walkHTML(n *html.Node, fn func(*html.Node)) {
	if n.Type == html.ElementNode {
		fn(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkHTML(c, fn)
	}
}

// walkHTMLSkipTemplates is walkHTML without the contents of <template>,
// which have their own id scope.
func walkHTMLSkipTemplates(n *html.Node, fn func(*html.Node)) {
	if n.Type == html.ElementNode {
		fn(n)
		if n.Data == "template" {
			return
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkHTMLSkipTemplates(c, fn)
	}
}

var headerPlaceholderRe = regexp.MustCompile(`:[A-Za-z_]+`)

// headerGlob turns a headers-file path pattern into an anchored regexp: "*"
// matches anything, ":name" one path segment.
func headerGlob(pattern string) *regexp.Regexp {
	q := strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*")
	q = headerPlaceholderRe.ReplaceAllString(q, "[^/]+")
	return regexp.MustCompile("^" + q + "$")
}

// headersFileImmutableSources reads a Netlify/Cloudflare _headers file and
// returns a matcher per path pattern whose block sets an immutable
// Cache-Control.
func headersFileImmutableSources(t *testing.T, file string) []*regexp.Regexp {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("no _headers file was emitted: %v", err)
	}
	var out []*regexp.Regexp
	var cur string
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#"):
		case !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t"):
			cur = strings.TrimSpace(line)
		case cur != "" && strings.Contains(strings.ToLower(line), "cache-control") && strings.Contains(line, "immutable"):
			out = append(out, headerGlob(cur))
		}
	}
	return out
}

// vercelImmutableSources returns a matcher per vercel.json header rule that
// sets an immutable Cache-Control.
func vercelImmutableSources(t *testing.T, file string) []*regexp.Regexp {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("no vercel.json was emitted: %v", err)
	}
	var doc struct {
		Headers []struct {
			Source  string `json:"source"`
			Headers []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"headers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var out []*regexp.Regexp
	for _, h := range doc.Headers {
		for _, kv := range h.Headers {
			if strings.EqualFold(kv.Key, "cache-control") && strings.Contains(kv.Value, "immutable") {
				// The adapter writes plain paths and "/dir/:path*" sources.
				out = append(out, headerGlob(strings.ReplaceAll(h.Source, ":path*", "*")))
			}
		}
	}
	return out
}

var knownHTMLElements = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(`html head body title base link meta style script noscript template slot
		a abbr address area article aside audio b bdi bdo blockquote br button canvas caption cite code col colgroup
		data datalist dd del details dfn dialog div dl dt em embed fieldset figcaption figure footer form
		h1 h2 h3 h4 h5 h6 header hgroup hr i iframe img input ins kbd label legend li main map mark menu meter nav
		object ol optgroup option output p param picture pre progress q rp rt ruby s samp search section select small
		source span strong sub summary sup table tbody td textarea tfoot th thead time tr track u ul var video wbr`) {
		m[n] = true
	}
	return m
}()
