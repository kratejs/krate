package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// writeProject writes rel->body files under a fresh project root.
func lpWrite(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func lpBuild(t *testing.T, root string) (*config.Config, error) {
	t.Helper()
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	return cfg, New(root, cfg).BuildAll()
}

func lpRead(t *testing.T, cfg *config.Config, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s not emitted: %v", rel, err)
	}
	return string(data)
}

type routeInfoDoc struct {
	V     int               `json:"v"`
	Chain []routeChainEntry `json:"chain"`
}

var routeInfoRe = regexp.MustCompile(`<script type="application/json" id="__krate_route">(.*?)</script>`)

func parseRouteInfo(t *testing.T, html string) routeInfoDoc {
	t.Helper()
	m := routeInfoRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("page has no #__krate_route route info:\n%s", html)
	}
	var info routeInfoDoc
	if err := json.Unmarshal([]byte(m[1]), &info); err != nil {
		t.Fatalf("route info is not JSON: %v\n%s", err, m[1])
	}
	return info
}

const counterLayout = `import { useState } from 'react';
export default function Root({ children }) {
  const [n, setN] = useState(0);
  return <div class="shell"><button onClick={() => setN(n + 1)}>{n}</button><main>{children}</main></div>;
}
`

// TestLayoutBoundaryMarkersAndRouteInfo checks that each layout's children
// are bracketed by stable <!--kl:ID--> markers, the per-page route info
// lists the chain outermost first, and the layout's hydration code is
// bracketed by its route scope.
func TestLayoutBoundaryMarkersAndRouteInfo(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/_layout.tsx":      counterLayout,
		"src/pages/blog/_layout.tsx": "export default function Blog({ children }) { return <section class=\"blog\">{children}</section>; }\n",
		"src/pages/blog/post.tsx":    "export default function Post() { return <p>post</p>; }\n",
	})
	cfg, err := lpBuild(t, root)
	if err != nil {
		t.Fatal(err)
	}
	html := lpRead(t, cfg, "blog/post/index.html")
	info := parseRouteInfo(t, html)
	if len(info.Chain) != 2 {
		t.Fatalf("chain = %+v, want 2 layouts", info.Chain)
	}
	outer, inner := info.Chain[0], info.Chain[1]
	if outer.File != "src/pages/_layout.tsx" || inner.File != "src/pages/blog/_layout.tsx" {
		t.Errorf("chain order = %s, %s; want root layout first", outer.File, inner.File)
	}
	if outer.Depth != 0 || inner.Depth != 1 {
		t.Errorf("depths = %d, %d; want 0, 1", outer.Depth, inner.Depth)
	}
	if !outer.Persist || !inner.Persist {
		t.Errorf("layouts should be persistable: %+v", info.Chain)
	}
	for _, e := range info.Chain {
		open, end := "<!--kl:"+e.ID+"-->", "<!--/kl:"+e.ID+"-->"
		if strings.Count(html, open) != 1 || strings.Count(html, end) != 1 {
			t.Errorf("boundary %s markers missing or duplicated:\n%s", e.ID, html)
		}
	}
	// Nesting: outer open < inner open < page < inner close < outer close.
	o := strings.Index(html, "<!--kl:"+outer.ID+"-->")
	i := strings.Index(html, "<!--kl:"+inner.ID+"-->")
	p := strings.Index(html, "<p>post</p>")
	ic := strings.Index(html, "<!--/kl:"+inner.ID+"-->")
	oc := strings.Index(html, "<!--/kl:"+outer.ID+"-->")
	if !(o < i && i < p && p < ic && ic < oc) {
		t.Errorf("boundary markers are not nested around the page: %d %d %d %d %d", o, i, p, ic, oc)
	}

	js := lpPageJS(t, cfg, "blog/post", html)
	if !strings.Contains(js, `krateEnterScope("`+outer.ID+`")`) {
		t.Errorf("layout hydration is not bracketed by its route scope:\n%s", js)
	}

	// The id is derived from the project-relative path: a second checkout
	// produces the same ids.
	root2 := lpWrite(t, map[string]string{
		"src/pages/_layout.tsx":      counterLayout,
		"src/pages/blog/_layout.tsx": "export default function Blog({ children }) { return <section class=\"blog\">{children}</section>; }\n",
		"src/pages/blog/post.tsx":    "export default function Post() { return <p>post</p>; }\n",
	})
	cfg2, err := lpBuild(t, root2)
	if err != nil {
		t.Fatal(err)
	}
	info2 := parseRouteInfo(t, lpRead(t, cfg2, "blog/post/index.html"))
	if info2.Chain[0].ID != outer.ID || info2.Chain[1].ID != inner.ID {
		t.Errorf("boundary ids differ across checkouts: %+v vs %+v", info.Chain, info2.Chain)
	}
}

// pageJS returns the hydration script referenced by a page.
func lpPageJS(t *testing.T, cfg *config.Config, route, html string) string {
	t.Helper()
	prefix := "/"
	if route != "" {
		prefix = "/" + route + "/"
	}
	m := regexp.MustCompile(`<script src="` + regexp.QuoteMeta(prefix) + `(index\.[a-z0-9]+\.js)"></script>`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("page %s has no hydration script:\n%s", route, html)
	}
	return lpRead(t, cfg, strings.TrimPrefix(prefix, "/")+m[1])
}

// TestTemplateOrderingAndRouteInfo checks layout > template > page ordering
// and that templates are never persistable.
func TestTemplateOrderingAndRouteInfo(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/_layout.tsx":   "export default function L({ children }) { return <div class=\"layout\">{children}</div>; }\n",
		"src/pages/_template.tsx": "export default function T({ children }) { return <div class=\"template\">{children}</div>; }\n",
		"src/pages/index.tsx":     "export default function Home() { return <p class=\"page\">home</p>; }\n",
		"src/pages/loading.tsx":   "export default function Loading() { return <p>loading</p>; }\n",
	})
	cfg, err := lpBuild(t, root)
	if err != nil {
		t.Fatal(err)
	}
	html := lpRead(t, cfg, "index.html")
	l, tp, pg := strings.Index(html, `class="layout"`), strings.Index(html, `class="template"`), strings.Index(html, `class="page"`)
	if !(l >= 0 && l < tp && tp < pg) {
		t.Errorf("want layout > template > page, got %d %d %d:\n%s", l, tp, pg, html)
	}
	info := parseRouteInfo(t, html)
	if len(info.Chain) != 2 || info.Chain[0].Kind != "layout" || info.Chain[1].Kind != "template" {
		t.Fatalf("chain = %+v", info.Chain)
	}
	if info.Chain[1].Persist {
		t.Errorf("a template must never be persistable: %+v", info.Chain[1])
	}
	// loading.tsx is the loading UI convention, not a /loading page.
	if _, err := os.Stat(filepath.Join(cfg.OutDir, "loading", "index.html")); err == nil {
		t.Errorf("loading.tsx was built as a /loading page")
	}
}

// TestTemplateWithoutUnderscore accepts template.tsx and keeps it out of the
// page list.
func TestTemplateWithoutUnderscore(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/template.tsx": "export default function T({ children }) { return <div class=\"tpl\">{children}</div>; }\n",
		"src/pages/index.tsx":    "export default function Home() { return <p>home</p>; }\n",
	})
	cfg, err := lpBuild(t, root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lpRead(t, cfg, "index.html"), `class="tpl"`) {
		t.Errorf("template.tsx did not wrap the page")
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, "template", "index.html")); err == nil {
		t.Errorf("template.tsx was built as a /template page")
	}
}

// TestFailingLayoutIsADiagnostic: a layout that fails to build must fail
// the page build with a diagnostic instead of silently emitting it unwrapped.
func TestFailingLayoutIsADiagnostic(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/_layout.tsx": "export const notAComponent = 1;\n",
		"src/pages/index.tsx":   "export default function Home() { return <p>home</p>; }\n",
	})
	_, err := lpBuild(t, root)
	if err == nil {
		t.Fatal("build succeeded with a layout that has no default export")
	}
	if !strings.Contains(err.Error(), "_layout") {
		t.Errorf("error does not name the layout: %v", err)
	}
}

// TestLayoutWithoutChildrenIsNotPersistable: a layout that drops {children}
// is reported and never kept mounted.
func TestLayoutWithoutChildrenIsNotPersistable(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/_layout.tsx": "export default function L() { return <div>no children</div>; }\n",
		"src/pages/index.tsx":   "export default function Home() { return <p>home</p>; }\n",
	})
	cfg, err := lpBuild(t, root)
	if err != nil {
		t.Fatal(err)
	}
	info := parseRouteInfo(t, lpRead(t, cfg, "index.html"))
	if info.Chain[0].Persist || !strings.Contains(info.Chain[0].Why, "children") {
		t.Errorf("chain = %+v, want non-persistable with a reason", info.Chain)
	}
}

// TestClassComponentLayoutNotPersistable: owner-tree pages hydrate layouts
// through the owner tree, which cannot be partially skipped.
func TestClassComponentLayoutNotPersistable(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/_layout.tsx": counterLayout,
		"src/pages/index.tsx": `import { Component } from 'react';
class Clock extends Component {
  state = { n: 0 };
  render() { return <button onClick={() => this.setState({ n: this.state.n + 1 })}>{this.state.n}</button>; }
}
export default function Home() { return <Clock />; }
`,
	})
	cfg, err := lpBuild(t, root)
	if err != nil {
		t.Fatal(err)
	}
	html := lpRead(t, cfg, "index.html")
	info := parseRouteInfo(t, html)
	if info.Chain[0].Persist {
		t.Errorf("layout on an owner-tree page must not be persistable: %+v", info.Chain)
	}
	if js := lpPageJS(t, cfg, "", html); strings.Contains(js, "krateEnterScope(") {
		t.Errorf("owner-tree hydration must not be scoped:\n%s", js)
	}
}

func TestRouteDepth(t *testing.T) {
	pages := filepath.FromSlash("/p")
	cases := map[string]int{
		"/p":                    0,
		"/p/blog":               1,
		"/p/(marketing)/about":  1,
		"/p/blog/[slug]":        2,
		"/p/docs/[...rest]":     -1,
		"/p/shop/[[...filter]]": -1,
		"/p/@modal/(.)photo":    1,
	}
	for dir, want := range cases {
		if got := routeDepth(filepath.FromSlash(dir), pages); got != want {
			t.Errorf("routeDepth(%s) = %d, want %d", dir, got, want)
		}
	}
}
