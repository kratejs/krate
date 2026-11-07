package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const counterSrc = `import { useState } from 'react';
export default function Counter() {
  const [n, setN] = useState(0);
  return <button onClick={() => setN(n + 1)}>{n}</button>;
}
`

func analysisFixture(t *testing.T) *Service {
	t.Helper()
	svc := newTestService(t, map[string]string{
		"src/pages/_layout.tsx": "import Nav from '../components/Nav';\nexport default function Layout({ children }) { return <div><Nav />{children}</div>; }\n",
		"src/pages/index.tsx": `import Counter from '@/components/Counter';
import Banner from '../components/Banner.server';
import { Chart } from 'chart-lib';
import '../styles/app.css';
export default function Home() {
  return <main><h1>Home</h1><Banner /><Counter client:visible /><Chart /></main>;
}
`,
		"src/pages/about.tsx":              "export default function About() { return <main><h1>About</h1><p>static</p></main>; }\n",
		"src/pages/blog/[slug].tsx":        "export default function Post() { return <article>post</article>; }\n",
		"src/components/Counter.tsx":       counterSrc,
		"src/components/Nav.tsx":           "export default function Nav() { return <nav><a href=\"/\">Home</a></nav>; }\n",
		"src/components/Banner.server.tsx": "import _ from 'lodash';\nexport default function Banner() { return <p>{_.upperCase('hi')}</p>; }\n",
		"src/components/Dec.tsx":           "@server\nexport default function Dec() { return <p>x</p>; }\n",
		"src/components/Rt.tsx":            "// @runtime\nexport default function Rt() { return <p>x</p>; }\n",
		"src/components/Stats.tsx":         "export default function Stats() { return <p>x</p>; }\n",
		"src/components/Async.tsx":         "export default async function Async() { return <p>x</p>; }\n",
		"src/styles/app.css":               "body { color: red; }\n",
	})
	svc.cfg.PathAliases = config.PathAliases{{Prefix: "@/*", Targets: []string{"./src/*"}}}
	svc.cfg.ServerComponents = []string{"Stats"}
	return svc
}

func TestTierClassificationFollowsDocumentedPriority(t *testing.T) {
	svc := analysisFixture(t)
	cases := map[string][2]string{
		"src/components/Counter.tsx":       {"client", "default"},
		"src/components/Banner.server.tsx": {"server", "file name"},
		"src/components/Dec.tsx":           {"server", "decorator"},
		"src/components/Rt.tsx":            {"runtime", "directive"},
		"src/components/Stats.tsx":         {"server", "krate.config"},
		"src/components/Async.tsx":         {"runtime", "async"},
	}
	for file, want := range cases {
		mi, err := svc.analyzeModule(file)
		if err != nil {
			t.Fatal(err)
		}
		if mi.Tier != want[0] || !strings.Contains(mi.TierSource, want[1]) {
			t.Errorf("%s = %s (%s), want %s (%s)", file, mi.Tier, mi.TierSource, want[0], want[1])
		}
	}
	// A directive outranks the file name; a decorator outranks both.
	svc2 := newTestService(t, map[string]string{
		"src/a.server.tsx": "// @static\nexport default function A() { return <p/>; }\n",
		"src/b.tsx":        "// @runtime\n@server\nexport default function B() { return <p/>; }\n",
	})
	if mi, _ := svc2.analyzeModule("src/a.server.tsx"); mi.Tier != "static" {
		t.Errorf("directive must win over the file name: %s (%s)", mi.Tier, mi.TierSource)
	}
	if mi, _ := svc2.analyzeModule("src/b.tsx"); mi.Tier != "server" {
		t.Errorf("decorator must win over the directive: %s (%s)", mi.Tier, mi.TierSource)
	}
}

func TestResolveImportFollowsExtensionsAliasesAndKinds(t *testing.T) {
	svc := analysisFixture(t)
	for _, c := range []struct{ from, spec, kind, resolved string }{
		{"src/pages/index.tsx", "../components/Counter", "local", "src/components/Counter.tsx"},
		{"src/pages/index.tsx", "@/components/Counter", "local", "src/components/Counter.tsx"},
		{"src/pages/index.tsx", "../styles/app.css", "style", ""},
		{"src/pages/index.tsx", "chart-lib", "package", ""},
		{"src/pages/index.tsx", "../components/Missing", "unresolved", ""},
		{"src/pages/index.tsx", "@/nope", "unresolved", ""},
	} {
		kind, res := svc.resolveImport(c.from, c.spec)
		if kind != c.kind || (c.kind == "local" && res != c.resolved) {
			t.Errorf("resolveImport(%q) = %s %q, want %s %q", c.spec, kind, res, c.kind, c.resolved)
		}
	}
	if packageName("@scope/pkg/deep/file") != "@scope/pkg" || packageName("lodash/fp") != "lodash" || packageName("node:fs") != "fs" {
		t.Error("packageName")
	}
}

func TestExplainPageReportsTiersIslandsAndLayouts(t *testing.T) {
	svc := analysisFixture(t)
	m := mustOK(t, svc, "explain_page", map[string]any{"route": "/"})
	if m["source"] != "src/pages/index.tsx" || m["mode"] != "ssg" {
		t.Fatalf("explain_page = %v", m)
	}
	if got := strs(list(t, m, "layouts")); len(got) != 1 || got[0] != "src/pages/_layout.tsx" {
		t.Errorf("layouts = %v", got)
	}
	if m["hydratesComponents"] != true {
		t.Error("the page renders Counter, which hydrates")
	}
	var counter, chart bool
	for _, i := range list(t, m, "islands") {
		im := asMap(i)
		if im["file"] == "src/components/Counter.tsx" {
			counter = true
			if im["directive"] != "client:visible" {
				t.Errorf("Counter directive = %v", im["directive"])
			}
			rs := list(t, im, "reasons")
			if len(rs) == 0 || asMap(rs[0])["kind"] != "hook" || asMap(rs[0])["line"] == float64(0) {
				t.Errorf("reasons must lead with the hook and carry file:line: %v", rs)
			}
		}
		if im["type"] == "package-island" && im["name"] == "chart-lib" {
			chart = true
		}
	}
	if !counter || !chart {
		t.Errorf("islands missing Counter (%v) or chart-lib (%v): %v", counter, chart, m["islands"])
	}
	bt := list(t, m, "buildTimeOnly")
	if len(bt) != 1 || asMap(bt[0])["file"] != "src/components/Banner.server.tsx" {
		t.Errorf("buildTimeOnly = %v", bt)
	}
	if asMap(m["built"])["built"] != false {
		t.Errorf("an unbuilt site must say so: %v", m["built"])
	}
	// A static page has no islands.
	s := mustOK(t, svc, "explain_page", map[string]any{"route": "/about"})
	if s["hydratesComponents"] != false || len(list(t, s, "islands")) != 0 {
		t.Errorf("about = %v", s)
	}
	// A concrete URL resolves to its dynamic route.
	d := mustOK(t, svc, "explain_page", map[string]any{"route": "/blog/hello-world"})
	if d["route"] != "/blog/[slug]" {
		t.Errorf("dynamic match = %v", d["route"])
	}
	if msg := mustFail(t, svc, "explain_page", map[string]any{"route": "/missing"}); !strings.Contains(msg, "no route matches") {
		t.Errorf("unknown route: %s", msg)
	}
}

func TestWhyHydratedGivesEvidenceAndSuggestions(t *testing.T) {
	svc := analysisFixture(t)
	m := mustOK(t, svc, "why_hydrated", map[string]any{"route": "/"})
	owners := list(t, m, "owners")
	if len(owners) != 1 {
		t.Fatalf("owners = %v", owners)
	}
	o := asMap(owners[0])
	if o["file"] != "src/components/Counter.tsx" {
		t.Errorf("owner = %v", o)
	}
	chain := strs(list(t, o, "importChain"))
	if len(chain) != 2 || chain[0] != "src/pages/index.tsx" || chain[1] != "src/components/Counter.tsx" {
		t.Errorf("import chain = %v", chain)
	}
	kinds := map[string]bool{}
	for _, r := range list(t, o, "reasons") {
		rm := asMap(r)
		kinds[rm["kind"].(string)] = true
		if rm["line"].(float64) < 1 {
			t.Errorf("reason without a line: %v", rm)
		}
	}
	if !kinds["hook"] || !kinds["event-handler"] {
		t.Errorf("reason kinds = %v", kinds)
	}
	if !strings.Contains(m["summary"].(string), "1 client module") {
		t.Errorf("summary = %v", m["summary"])
	}

	about := mustOK(t, svc, "why_hydrated", map[string]any{"route": "/about"})
	if about["hydratesComponents"] != false || !strings.Contains(about["summary"].(string), "plain HTML") {
		t.Errorf("static page = %v", about)
	}
}

func TestWhyInBundleTracesChainsAndBuildTimeOnly(t *testing.T) {
	svc := analysisFixture(t)
	m := mustOK(t, svc, "why_in_bundle", map[string]any{"module": "lodash"})
	if m["reachable"] != true {
		t.Fatalf("lodash = %v", m)
	}
	r := asMap(list(t, m, "routes")[0])
	if r["route"] != "/" || r["buildTimeOnly"] != true {
		t.Errorf("lodash is only imported by a server component: %v", r)
	}
	chain := strs(list(t, r, "importChain"))
	if len(chain) != 2 || chain[1] != "src/components/Banner.server.tsx" {
		t.Errorf("chain = %v", chain)
	}
	// A client file is reached through the layout on every route.
	n := mustOK(t, svc, "why_in_bundle", map[string]any{"module": "src/components/Nav.tsx", "limit": 2})
	if n["total"].(float64) < 3 || n["nextCursor"] == "" {
		t.Errorf("Nav is in every route's layout and the result must paginate: %v", n)
	}
	c := mustOK(t, svc, "why_in_bundle", map[string]any{"module": "src/components/Counter.tsx", "route": "/"})
	if asMap(list(t, c, "routes")[0])["buildTimeOnly"] != false {
		t.Errorf("Counter ships to the browser: %v", c)
	}
	none := mustOK(t, svc, "why_in_bundle", map[string]any{"module": "src/components/Dec.tsx"})
	if none["reachable"] != false {
		t.Errorf("unreferenced module = %v", none)
	}
	if msg := mustRPCError(t, svc, "why_in_bundle", map[string]any{}); !strings.Contains(msg, "module") {
		t.Errorf("missing module: %s", msg)
	}
}

func TestListIslandsAggregatesAcrossRoutes(t *testing.T) {
	svc := analysisFixture(t)
	m := mustOK(t, svc, "list_islands", nil)
	var counter map[string]any
	for _, i := range list(t, m, "islands") {
		if asMap(i)["file"] == "src/components/Counter.tsx" {
			counter = asMap(i)
		}
	}
	if counter == nil || strs(list(t, counter, "routes"))[0] != "/" {
		t.Fatalf("Counter island = %v in %v", counter, m)
	}
	one := mustOK(t, svc, "list_islands", map[string]any{"route": "/about"})
	if len(list(t, one, "islands")) != 0 || one["routesScanned"] != float64(1) {
		t.Errorf("about = %v", one)
	}
}

var builtFixture = map[string]string{
	"src/pages/index.tsx": `import { useState } from 'react';
export default function Home() {
  const [n, setN] = useState(0);
  return (
    <main>
      <Head><title>Home page</title><meta name="description" content="The home page" /></Head>
      <h1>Home</h1>
      <p>Welcome to the site.</p>
      <a href="/about">About</a>
      <img src="/x.png" />
      <form action="/api/x" method="post"><input name="email" /><button onClick={() => setN(n + 1)}>{n}</button></form>
    </main>
  );
}
`,
	"src/pages/about.tsx": "export default function About() { return <main><Head><title>About us</title></Head><h1>About</h1><h2>Team</h2><p>We are a team.</p></main>; }\n",
}

func TestBuiltPageToolsReadTheCompiledOutput(t *testing.T) {
	svc := newBuiltService(t, builtFixture)

	e := mustOK(t, svc, "explain_page", map[string]any{"route": "/"})
	built := asMap(e["built"])
	if built["shipsJavaScript"] != true {
		t.Fatalf("an interactive page ships JS: %v", built)
	}
	totals := asMap(built["totals"])
	if totals["jsBytes"].(float64) <= 0 || totals["jsGzip"].(float64) <= 0 || totals["jsGzip"].(float64) >= totals["jsBytes"].(float64) {
		t.Errorf("totals = %v", totals)
	}
	for _, a := range list(t, built, "assets") {
		am := asMap(a)
		if strings.HasPrefix(am["path"].(string), "/") || strings.Contains(am["path"].(string), "..") {
			t.Errorf("asset path must be project relative: %v", am)
		}
	}
	about := mustOK(t, svc, "explain_page", map[string]any{"route": "/about"})
	if asMap(about["built"])["shipsJavaScript"] == true && about["hydratesComponents"] == false {
		// shared runtime only: why_hydrated must say it is not component code
		w := mustOK(t, svc, "why_hydrated", map[string]any{"route": "/about"})
		if !strings.Contains(w["summary"].(string), "shared runtime") {
			t.Errorf("summary = %v", w["summary"])
		}
	}

	pm := mustOK(t, svc, "get_page_metadata", map[string]any{"route": "/"})
	head := asMap(pm["head"])
	if head["title"] != "Home page" || asMap(head["meta"])["description"] != "The home page" {
		t.Errorf("head = %v", head)
	}
	missing := strs(list(t, pm, "missing"))
	if !contains(missing, "canonical link") || contains(missing, "title") {
		t.Errorf("missing = %v", missing)
	}

	// get_rendered_html: parts and paging.
	full := mustOK(t, svc, "get_rendered_html", map[string]any{"route": "/"})
	if !strings.Contains(full["html"].(string), "<h1>Home</h1>") {
		t.Errorf("html = %v", full["html"])
	}
	headOnly := mustOK(t, svc, "get_rendered_html", map[string]any{"route": "/", "part": "head"})
	if strings.Contains(headOnly["html"].(string), "<h1>") || !strings.Contains(headOnly["html"].(string), "<title>") {
		t.Errorf("head part = %v", headOnly["html"])
	}
	text := mustOK(t, svc, "get_rendered_html", map[string]any{"route": "/", "format": "text"})
	if strings.Contains(text["html"].(string), "<") || !strings.Contains(text["html"].(string), "Welcome to the site.") {
		t.Errorf("text = %v", text["html"])
	}
	p1 := mustOK(t, svc, "get_rendered_html", map[string]any{"route": "/", "maxBytes": 200})
	if p1["returnedBytes"].(float64) > 200 || p1["nextOffset"] == "" {
		t.Fatalf("page 1 = %v", p1)
	}
	var off float64
	if err := json.Unmarshal([]byte(p1["nextOffset"].(string)), &off); err != nil {
		t.Fatal(err)
	}
	p2 := mustOK(t, svc, "get_rendered_html", map[string]any{"route": "/", "maxBytes": 200, "offset": off})
	if p2["html"] == p1["html"] || p2["offset"] != off {
		t.Errorf("page 2 = %v", p2)
	}
	if msg := mustFail(t, svc, "get_rendered_html", map[string]any{"route": "/", "part": "x"}); !strings.Contains(msg, "part must be") {
		t.Errorf("bad part: %s", msg)
	}

	rp := mustOK(t, svc, "render_preview", map[string]any{"route": "/"})
	if asMap(rp["images"])["missingAlt"] != float64(1) {
		t.Errorf("images = %v", rp["images"])
	}
	forms := list(t, rp, "forms")
	if len(forms) != 1 || asMap(forms[0])["method"] != "POST" {
		t.Errorf("forms = %v", forms)
	}
	if asMap(rp["links"])["internal"] != float64(1) {
		t.Errorf("links = %v", rp["links"])
	}
	heads := list(t, rp, "headings")
	if asMap(heads[0])["text"] != "Home" {
		t.Errorf("headings = %v", heads)
	}
	small := mustOK(t, svc, "render_preview", map[string]any{"route": "/", "textChars": 10})
	if small["textTruncated"] != true || len(small["text"].(string)) > 10 {
		t.Errorf("text limit = %v", small)
	}
}

func TestBuiltPageToolsExplainMissingBuild(t *testing.T) {
	svc := newTestService(t, builtFixture)
	for _, tool := range []string{"get_rendered_html", "render_preview"} {
		if msg := mustFail(t, svc, tool, map[string]any{"route": "/"}); !strings.Contains(msg, "not been built") {
			t.Errorf("%s: %s", tool, msg)
		}
	}
	pm := mustOK(t, svc, "get_page_metadata", map[string]any{"route": "/"})
	if pm["built"] != false {
		t.Errorf("metadata of an unbuilt page = %v", pm)
	}
	// rebuild:true builds first.
	rp := mustOK(t, svc, "render_preview", map[string]any{"route": "/about", "rebuild": true})
	if rp["title"] != "About us" {
		t.Errorf("rebuild preview = %v", rp)
	}
}

func TestHTMLInfoParsing(t *testing.T) {
	doc := `<!doctype html><html lang="en"><head><meta charset="utf-8"><title> Hi </title>
<meta name="description" content="d"><meta property="og:title" content="t"><link rel="canonical" href="/c/">
<link rel="stylesheet" href="/a.css"><link rel="modulepreload" href="/m.js">
<script type="application/ld+json">{"@graph":[{"@type":"Article"},{"@type":["Person","Thing"]}]}</script>
<script src="/x.js" type="module"></script><script>var a=1</script><style>p{}</style></head>
<body><header><nav>n</nav></header><main><h1>One</h1><h3>Three</h3><a href="https://e.com/">E</a><a href="/i">I</a>
<img src="a.png" alt=""><img src="b.png"><button>b</button><form action="/f"><input name="q"><input type="hidden" name="h"></form></main></body></html>`
	in := parseHTMLInfo([]byte(doc))
	if in.Lang != "en" || in.Title != "Hi" || in.Canonical != "/c/" || in.Meta["og:title"] != "t" || in.Meta["charset"] != "utf-8" {
		t.Errorf("head = %+v", in)
	}
	if len(in.JSONLD) != 3 || in.JSONLD[0] != "Article" {
		t.Errorf("json-ld = %v", in.JSONLD)
	}
	if len(in.Scripts) != 2 || in.Scripts[0].Src != "/x.js" || !in.Scripts[1].Inline || in.InlineJS != 1 {
		t.Errorf("scripts = %+v", in.Scripts)
	}
	if len(in.Headings) != 2 || in.Headings[1].Level != 3 {
		t.Errorf("headings = %+v", in.Headings)
	}
	if len(in.Images) != 2 || !in.Images[0].HasAlt || in.Images[1].HasAlt {
		t.Errorf("images = %+v", in.Images)
	}
	if len(in.Forms) != 1 || len(in.Forms[0].Fields) != 1 || in.Buttons != 1 {
		t.Errorf("forms = %+v buttons=%d", in.Forms, in.Buttons)
	}
	if in.Landmarks["nav"] != 1 || in.Landmarks["main"] != 1 || in.Landmarks["header"] != 1 {
		t.Errorf("landmarks = %v", in.Landmarks)
	}
	if h, b := splitHTML(doc); !strings.HasPrefix(h, "<head") || !strings.HasPrefix(b, "<body") {
		t.Errorf("splitHTML = %q / %q", h[:10], b[:10])
	}
}

func TestMatchRoutePattern(t *testing.T) {
	for _, c := range []struct {
		pattern, path string
		want          bool
	}{
		{"/", "/", true}, {"/", "/x", false},
		{"/blog/[slug]", "/blog/a", true}, {"/blog/[slug]", "/blog", false}, {"/blog/[slug]", "/blog/a/b", false},
		{"/docs/[...rest]", "/docs/a/b/c", true}, {"/docs/[...rest]", "/docs", false},
		{"/docs/[[...rest]]", "/docs", true}, {"/about", "/about", true}, {"/about", "/abou", false},
	} {
		if got := matchRoutePattern(c.pattern, c.path); got != c.want {
			t.Errorf("matchRoutePattern(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}
