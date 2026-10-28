package build

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMatchRoutePattern(t *testing.T) {
	cases := []struct {
		url, pattern string
		want         map[string]string
		ok           bool
	}{
		{"/video/abc", "video/[id]", map[string]string{"id": "abc"}, true},
		{"/video", "video/[id]", nil, false},
		{"/video/a/b", "video/[id]", nil, false},
		{"/docs/a/b/c", "docs/[...slug]", map[string]string{"slug": "a/b/c"}, true},
		{"/docs", "docs/[...slug]", nil, false},
		{"/shop", "shop/[[...filter]]", map[string]string{"filter": ""}, true},
		{"/shop/", "shop/[[...filter]]", map[string]string{"filter": ""}, true},
		{"/shop/red/xl", "shop/[[...filter]]", map[string]string{"filter": "red/xl"}, true},
		{"/", "[[...all]]", map[string]string{"all": ""}, true},
		{"/u/x/posts/7", "u/[name]/posts/[id]", map[string]string{"name": "x", "id": "7"}, true},
		{"/about", "about", map[string]string{}, true},
		{"/a//b", "a/[...rest]", nil, false},
	}
	for _, c := range cases {
		got, ok := matchRoutePattern(c.url, c.pattern)
		if ok != c.ok || (ok && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("matchRoutePattern(%q, %q) = %v, %v; want %v, %v", c.url, c.pattern, got, ok, c.want, c.ok)
		}
	}
	// The server-side wrappers share the matcher.
	if p, ok := matchDynamicRoute("/docs/x/y", "docs/[...slug]"); !ok || p["slug"] != "x/y" {
		t.Errorf("matchDynamicRoute catch-all = %v %v", p, ok)
	}
	if p, ok := matchRoute("/shop", "/shop/[[...f]]"); !ok || p["f"] != "" {
		t.Errorf("matchRoute optional catch-all = %v %v", p, ok)
	}
}

func TestRouteSpecificityOrder(t *testing.T) {
	patterns := []string{
		"[[...all]]",
		"docs/[...slug]",
		"docs/[id]",
		"docs/intro",
		"[lang]/docs",
		"docs/[id]/edit",
		"shop/[[...f]]",
		"shop/[...f]",
	}
	sortRoutePatterns(patterns)
	want := []string{
		"docs/intro",
		"docs/[id]/edit",
		"docs/[id]",
		"docs/[...slug]",
		"shop/[...f]",
		"shop/[[...f]]",
		"[lang]/docs",
		"[[...all]]",
	}
	if !reflect.DeepEqual(patterns, want) {
		t.Errorf("order = %v\nwant    %v", patterns, want)
	}
	// Deterministic for any input order.
	rev := append([]string{}, want...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	sortRoutePatterns(rev)
	if !reflect.DeepEqual(rev, want) {
		t.Errorf("order depends on input: %v", rev)
	}
}

func TestFindDynamicRoutesSpecificity(t *testing.T) {
	out := t.TempDir()
	for _, d := range []string{"docs/[...slug]", "docs/[id]", "shop/[[...f]]", "blog/[slug]/comments"} {
		p := filepath.Join(out, filepath.FromSlash(d))
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "index.html"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, r := range findDynamicRoutes(out) {
		got = append(got, r.pattern)
	}
	want := []string{"blog/[slug]/comments", "docs/[id]", "docs/[...slug]", "shop/[[...f]]"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findDynamicRoutes = %v, want %v", got, want)
	}
}

func TestPageToOutputRouteGroups(t *testing.T) {
	pages := filepath.FromSlash("/p")
	cases := map[string]string{
		"/p/(marketing)/about.tsx":       "about",
		"/p/(marketing)/index.tsx":       ".",
		"/p/(shop)/(inner)/cart.tsx":     "cart",
		"/p/blog/(posts)/[slug].tsx":     "blog/[slug]",
		"/p/(app)/dashboard/index.tsx":   "dashboard",
		"/p/docs/[...slug].tsx":          "docs/[...slug]",
		"/p/shop/[[...filter]].tsx":      "shop/[[...filter]]",
		"/p/(marketing)/(.)notgroup.tsx": "(.)notgroup",
	}
	for page, want := range cases {
		if got := pageToOutput(filepath.FromSlash(page), pages); got != want {
			t.Errorf("pageToOutput(%s) = %q, want %q", page, got, want)
		}
	}
}

func TestInterceptLevels(t *testing.T) {
	cases := map[string][2]any{
		"(.)photo":      {0, "photo"},
		"(..)photo":     {1, "photo"},
		"(..)(..)photo": {2, "photo"},
		"(...)photo":    {-1, "photo"},
	}
	for seg, want := range cases {
		n, rest, ok := interceptLevels(seg)
		if !ok || n != want[0].(int) || rest != want[1].(string) {
			t.Errorf("interceptLevels(%s) = %d %q %v", seg, n, rest, ok)
		}
	}
	if _, _, ok := interceptLevels("(group)"); ok {
		t.Errorf("a route group is not an intercepting segment")
	}
}

func TestSlotDiscoveryAndInterceptTargets(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/feed/_layout.tsx":                 "x",
		"src/pages/feed/@modal/default.tsx":          "x",
		"src/pages/feed/@modal/(.)photo/[id].tsx":    "x",
		"src/pages/feed/@modal/(..)user/[name].tsx":  "x",
		"src/pages/feed/@modal/(...)login.tsx":       "x",
		"src/pages/feed/@modal/settings.tsx":         "x",
		"src/pages/feed/@modal/(grp)/help/index.tsx": "x",
		"src/pages/feed/@modal/_private.tsx":         "x",
		"src/pages/feed/@team/(..)(..)toolong/x.tsx": "x",
		"src/pages/feed/@team/default.tsx":           "x",
	})
	pagesDir := filepath.Join(root, "src", "pages")
	_, err := findSlots(filepath.Join(pagesDir, "feed"), pagesDir)
	if err == nil || !strings.Contains(err.Error(), "above the app root") {
		t.Fatalf("want an error for (..)(..) above the root, got %v", err)
	}
	if err := os.RemoveAll(filepath.Join(pagesDir, "feed", "@team")); err != nil {
		t.Fatal(err)
	}
	slots, err := findSlots(filepath.Join(pagesDir, "feed"), pagesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 || slots[0].Name != "modal" || slots[0].Default == "" {
		t.Fatalf("slots = %+v", slots)
	}
	got := map[string]string{}
	for _, sp := range slots[0].Pages {
		rel, _ := filepath.Rel(slots[0].Dir, sp.File)
		if sp.Intercept {
			got[filepath.ToSlash(rel)] = "intercept:" + sp.Target
		} else {
			got[filepath.ToSlash(rel)] = "route:" + sp.Route
		}
	}
	want := map[string]string{
		"(.)photo/[id].tsx":    "intercept:feed/photo/[id]",
		"(..)user/[name].tsx":  "intercept:user/[name]",
		"(...)login.tsx":       "intercept:login",
		"settings.tsx":         "route:settings",
		"(grp)/help/index.tsx": "route:help",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("slot pages = %v\nwant %v", got, want)
	}
	info := interceptForPage(filepath.Join(pagesDir, "feed", "@modal", "(.)photo", "[id].tsx"), pagesDir)
	if info == nil || info.OutDir != interceptPrefix+"/"+slots[0].Key+"/feed/photo/[id]" {
		t.Errorf("interceptForPage = %+v", info)
	}
	if interceptForPage(filepath.Join(pagesDir, "feed", "@modal", "settings.tsx"), pagesDir) != nil {
		t.Errorf("a non-intercepting slot page is not a fragment")
	}
}

func TestRouteGroupConflictIsADiagnostic(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/(a)/about.tsx": "export default function A() { return <p>a</p>; }\n",
		"src/pages/(b)/about.tsx": "export default function B() { return <p>b</p>; }\n",
	})
	_, err := lpBuild(t, root)
	if err == nil || !strings.Contains(err.Error(), "conflicting routes") || !strings.Contains(err.Error(), "/about") {
		t.Fatalf("want a conflicting-routes diagnostic, got %v", err)
	}
}

func TestRouteGroupLayoutScope(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/(marketing)/_layout.tsx": "export default function M({ children }) { return <div class=\"mkt\">{children}</div>; }\n",
		"src/pages/(marketing)/about.tsx":   "export default function About() { return <p>about</p>; }\n",
		"src/pages/app.tsx":                 "export default function App() { return <p>app</p>; }\n",
	})
	cfg, err := lpBuild(t, root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lpRead(t, cfg, "about/index.html"), `class="mkt"`) {
		t.Errorf("group layout must wrap pages in the group")
	}
	if strings.Contains(lpRead(t, cfg, "app/index.html"), `class="mkt"`) {
		t.Errorf("group layout must not wrap pages outside the group")
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, "(marketing)")); err == nil {
		t.Errorf("route group directory leaked into the output")
	}
}

func TestCatchAllBuildOutput(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/docs/[...slug].tsx":     "export default function D({ params }) { return <p>docs {params.slug}</p>; }\n",
		"src/pages/shop/[[...filter]].tsx": "export default function S({ params }) { return <p>shop {params.filter}</p>; }\n",
	})
	cfg, err := lpBuild(t, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/[...slug]/index.html", "shop/[[...filter]]/index.html"} {
		html := lpRead(t, cfg, rel)
		if !strings.Contains(html, dynamicParamSentinel("slug")) && !strings.Contains(html, dynamicParamSentinel("filter")) {
			t.Errorf("%s does not bind its catch-all param:\n%s", rel, html)
		}
	}
}

const galleryLayout = `export default function Root({ children, modal }) {
  return <div><main>{children}</main><aside id="slot">{modal}</aside></div>;
}
`

func TestParallelSlotsBuildOutput(t *testing.T) {
	root := lpWrite(t, map[string]string{
		"src/pages/_layout.tsx":              galleryLayout,
		"src/pages/@modal/default.tsx":       "export default function D() { return <span id=\"def\"></span>; }\n",
		"src/pages/@modal/settings.tsx":      "export default function S() { return <span id=\"slot-settings\"></span>; }\n",
		"src/pages/@modal/(.)photo/[id].tsx": "export default function M({ params }) { return <div id=\"modal\">{params.id}</div>; }\n",
		"src/pages/index.tsx":                "export default function Feed() { return <p>feed</p>; }\n",
		"src/pages/settings.tsx":             "export default function Settings() { return <p>settings</p>; }\n",
		"src/pages/photo/[id].tsx":           "export default function P({ params }) { return <h1>photo {params.id}</h1>; }\n",
	})
	cfg, err := lpBuild(t, root)
	if err != nil {
		t.Fatal(err)
	}
	pagesDir := filepath.Join(root, "src", "pages")
	key := slotKey(filepath.Join(pagesDir, "@modal"), pagesDir)

	home := lpRead(t, cfg, "index.html")
	if !strings.Contains(home, "<!--ks:"+key+`--><span id="def"></span><!--/ks:`+key+"-->") {
		t.Errorf("home must render the slot default inside slot markers:\n%s", home)
	}
	info := parseRouteInfo(t, home)
	if len(info.Chain) != 1 || len(info.Chain[0].Slots) != 1 {
		t.Fatalf("route info has no slot: %+v", info.Chain)
	}
	sl := info.Chain[0].Slots[0]
	if sl.Name != "modal" || sl.Key != key || sl.Src != "default" || !reflect.DeepEqual(sl.Intercepts, []string{"photo/[id]"}) {
		t.Errorf("slot info = %+v", sl)
	}
	if strings.Contains(home, "<krate-slot") {
		t.Errorf("slot placeholder leaked into the output")
	}

	settings := lpRead(t, cfg, "settings/index.html")
	if !strings.Contains(settings, `id="slot-settings"`) || parseRouteInfo(t, settings).Chain[0].Slots[0].Src != "page" {
		t.Errorf("/settings must render the matching slot page:\n%s", settings)
	}

	// The photo page renders the real page with the slot default (hard load).
	photo := lpRead(t, cfg, "photo/[id]/index.html")
	if strings.Contains(photo, `id="modal"`) || !strings.Contains(photo, `id="def"`) {
		t.Errorf("a hard load of the intercepted URL must render the page, not the modal:\n%s", photo)
	}

	// The intercepting fragment is built under _krate/i/<key>/.
	frag := lpRead(t, cfg, interceptPrefix+"/"+key+"/photo/[id]/index.html")
	if !strings.Contains(frag, "<!--ks:"+key+"-->") || !strings.Contains(frag, `id="modal"`) || !strings.Contains(frag, dynamicParamSentinel("id")) {
		t.Errorf("fragment output:\n%s", frag)
	}
	if strings.Contains(frag, "<main>") {
		t.Errorf("a fragment must not be wrapped in layouts:\n%s", frag)
	}
	// Fragments are not routes.
	if routes, err := os.ReadFile(filepath.Join(root, ".krate", "types", "routes.d.ts")); err == nil && strings.Contains(string(routes), "_krate") {
		t.Errorf("route types list the fragment:\n%s", routes)
	}
}

func TestSlotErrors(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"missing default": {map[string]string{
			"src/pages/_layout.tsx":         galleryLayout,
			"src/pages/@modal/settings.tsx": "export default function S() { return <i></i>; }\n",
			"src/pages/index.tsx":           "export default function H() { return <p>h</p>; }\n",
		}, "no default.tsx"},
		"slot without layout": {map[string]string{
			"src/pages/feed/@modal/default.tsx": "export default function D() { return <i></i>; }\n",
			"src/pages/index.tsx":               "export default function H() { return <p>h</p>; }\n",
		}, "has no _layout"},
		"intercept outside a slot": {map[string]string{
			"src/pages/(.)photo/[id].tsx": "export default function P() { return <i></i>; }\n",
		}, "inside a parallel-route slot"},
		"dynamic non-intercepting slot page": {map[string]string{
			"src/pages/_layout.tsx":          galleryLayout,
			"src/pages/@modal/default.tsx":   "export default function D() { return <i></i>; }\n",
			"src/pages/@modal/post/[id].tsx": "export default function P() { return <i></i>; }\n",
			"src/pages/index.tsx":            "export default function H() { return <p>h</p>; }\n",
		}, "not supported"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := lpBuild(t, lpWrite(t, c.files))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}
