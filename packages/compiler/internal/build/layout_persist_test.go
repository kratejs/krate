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
