package plugin

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// docsSite writes content files under a temp root and runs the docs plugin's
// BeforeBuild with the given extra plugin options. It returns the root and the
// generated pages (path -> source), keyed by path relative to .krate/gen.
func docsSite(t *testing.T, files map[string]string, slash string, extra map[string]interface{}) (string, map[string]string) {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, "src/content/docs", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "layout.tsx"), []byte("export default function L(){return null}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := map[string]interface{}{"layout": "layout.tsx", "search": map[string]interface{}{"enabled": false}, "lastUpdated": false}
	for k, v := range extra {
		opts[k] = v
	}
	cfg := &config.Config{
		TrailingSlash: slash,
		Plugins:       []config.PluginConfig{{Name: "docs", Options: opts}},
	}
	var gen []GeneratedPage
	ctx := &BuildHookCtx{Root: root, OutDir: filepath.Join(root, "dist"), Config: cfg, GeneratedPages: &gen}
	if err := (&DocsPlugin{}).beforeBuild(ctx); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	genRoot := filepath.Join(root, ".krate", "gen")
	for _, g := range gen {
		b, err := os.ReadFile(g.Path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(genRoot, g.Path)
		out[filepath.ToSlash(rel)] = string(b)
	}
	return root, out
}

func page(title, order, body string) string {
	fm := "---\ntitle: " + title + "\n"
	if order != "" {
		fm += "order: " + order + "\n"
	}
	return fm + "---\n\n" + body + "\n"
}

var linkRe = regexp.MustCompile(`(prev|next)Link: "([^"]*)"`)

func navLinks(src string) map[string]string {
	m := map[string]string{}
	for _, g := range linkRe.FindAllStringSubmatch(src, -1) {
		m[g[1]] = g[2]
	}
	return m
}

func TestDocsReservedPageNames(t *testing.T) {
	_, out := docsSite(t, map[string]string{
		"cli/template.md": page("TplPage", "", "tpl-body-marker"),
		"cli/error.md":    page("ErrPage", "", "err-body-marker"),
		"cli/loading.md":  page("LoadPage", "", "load-body-marker"),
		"cli/_x.md":       page("UnderPage", "", "under-body-marker"),
		"cli/other.md":    page("OtherPage", "", "other-body-marker"),
	}, "", nil)
	want := map[string]string{
		"docs/cli/template/index.tsx": "tpl-body-marker",
		"docs/cli/error/index.tsx":    "err-body-marker",
		"docs/cli/loading/index.tsx":  "load-body-marker",
		"docs/cli/_x/index.tsx":       "under-body-marker",
		"docs/cli/other.tsx":          "other-body-marker",
	}
	for path, marker := range want {
		src, ok := out[path]
		if !ok {
			t.Errorf("missing generated page %s (have %v)", path, keys(out))
			continue
		}
		if !strings.Contains(src, marker) {
			t.Errorf("%s does not hold its own content %q", path, marker)
		}
	}
	for _, bad := range []string{"docs/cli/template.tsx", "docs/cli/error.tsx", "docs/cli/loading.tsx", "docs/cli/_x.tsx"} {
		if _, ok := out[bad]; ok {
			t.Errorf("reserved name generated as %s", bad)
		}
	}
}

func keys(m map[string]string) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	return k
}

func TestGenPageFile(t *testing.T) {
	for route, want := range map[string]string{
		"docs/cli/template":     "docs/cli/template/index.tsx",
		"fr/docs/cli/not-found": "fr/docs/cli/not-found/index.tsx",
		"docs/404":              "docs/404/index.tsx",
		"docs/cli/other":        "docs/cli/other.tsx",
		"docs":                  "docs.tsx",
	} {
		if got := filepath.ToSlash(genPageFile(route)); got != want {
			t.Errorf("genPageFile(%q) = %q, want %q", route, got, want)
		}
	}
}

func TestDocsPrevNextFollowSidebar(t *testing.T) {
	// Sidebar order: guide/zeta (order 1) before guide/alpha (order 2) is the
	// same as the page order here, so make the section order differ: the
	// "api" section index sets order 1 and sorts before the root link.
	files := map[string]string{
		"intro.md":     page("Intro", "5", "i"),
		"api/index.md": "---\ntitle: API Reference\nsidebar:\n  order: 1\n---\n\napi\n",
		"api/beta.md":  page("Beta", "", "b"),
		"secret.md":    "---\ntitle: Secret\nsidebar:\n  hidden: true\n---\n\ns\n",
	}
	_, out := docsSite(t, files, "", nil)
	// Sidebar: API Reference section (index, beta) then Intro.
	if got := navLinks(out["docs/api.tsx"]); got["prev"] != "" || got["next"] != "/docs/api/beta/" {
		t.Errorf("api index links = %v", got)
	}
	if got := navLinks(out["docs/api/beta.tsx"]); got["prev"] != "/docs/api/" || got["next"] != "/docs/intro/" {
		t.Errorf("beta links = %v", got)
	}
	if got := navLinks(out["docs/intro.tsx"]); got["prev"] != "/docs/api/beta/" || got["next"] != "" {
		t.Errorf("intro links = %v", got)
	}
	if got := navLinks(out["docs/secret.tsx"]); len(got) != 0 {
		t.Errorf("a page outside the sidebar must have no neighbours, got %v", got)
	}
}

func TestDocsPrevNextCustomSidebar(t *testing.T) {
	files := map[string]string{
		"a.md": page("A", "1", "a"),
		"b.md": page("B", "2", "b"),
		"c.md": page("C", "3", "c"),
	}
	sidebar := []interface{}{
		map[string]interface{}{"title": "C", "url": "/docs/c/"},
		map[string]interface{}{"title": "Group", "children": []interface{}{
			map[string]interface{}{"title": "A", "url": "/docs/a/"},
		}},
		map[string]interface{}{"title": "B", "url": "/docs/b/"},
	}
	_, out := docsSite(t, files, "", map[string]interface{}{"sidebar": sidebar})
	if got := navLinks(out["docs/a.tsx"]); got["prev"] != "/docs/c/" || got["next"] != "/docs/b/" {
		t.Errorf("a links = %v", got)
	}
	if got := navLinks(out["docs/c.tsx"]); got["prev"] != "" || got["next"] != "/docs/a/" {
		t.Errorf("c links = %v", got)
	}
}

func TestDocsCurrentPathHasNoIndex(t *testing.T) {
	_, out := docsSite(t, map[string]string{
		"index.md":     page("Home", "", "h"),
		"cli/index.md": page("CLI", "", "c"),
		"cli/run.md":   page("Run", "", "r"),
	}, "", nil)
	for path, want := range map[string]string{
		"docs.tsx":         `currentPath: ""`,
		"docs/cli.tsx":     `currentPath: "cli"`,
		"docs/cli/run.tsx": `currentPath: "cli/run"`,
	} {
		if !strings.Contains(out[path], want) {
			t.Errorf("%s: want %s", path, want)
		}
	}
}

func TestDocsTrailingSlashNever(t *testing.T) {
	files := map[string]string{
		"index.md":     page("Home", "1", "h"),
		"cli/index.md": page("CLI", "2", "c"),
		"cli/run.md":   page("Run", "", "r"),
	}
	_, out := docsSite(t, files, "never", nil)
	run := out["docs/cli/run.tsx"]
	if got := navLinks(run); got["prev"] != "/docs/cli" || got["next"] != "" {
		t.Errorf("prev/next = %v", got)
	}
	for _, want := range []string{`"url":"/docs/cli/run"`, `"url":"/docs/cli"`, `"url":"/docs"`} {
		if !strings.Contains(run, want) {
			t.Errorf("sidebar/breadcrumb missing %s", want)
		}
	}
	if strings.Contains(run, `"/docs/cli/run/"`) || strings.Contains(run, `"/docs/cli/"`) || strings.Contains(run, `"/docs/"`) {
		t.Errorf("a docs URL still ends in a slash:\n%s", run)
	}
	// The current page is still marked expanded under its own section.
	if !regexp.MustCompile(`"expanded":true`).MatchString(run) {
		t.Errorf("active section not expanded without trailing slashes")
	}
}

func TestDocsTrailingSlashAlwaysKeepsSlash(t *testing.T) {
	_, out := docsSite(t, map[string]string{
		"cli/index.md": page("CLI", "", "c"),
		"cli/run.md":   page("Run", "", "r"),
	}, "", nil)
	if got := navLinks(out["docs/cli/run.tsx"]); got["prev"] != "/docs/cli/" {
		t.Errorf("prev = %v", got)
	}
}

func TestDocsSidebarAndSearchURLsNever(t *testing.T) {
	root, _ := docsSite(t, map[string]string{
		"cli/index.md": page("CLI", "", "c"),
		"cli/run.md":   page("Run", "", "r"),
	}, "never", nil)
	for _, f := range []string{"docs/data/sidebar.json", "docs/data/search-index.json"} {
		b, err := os.ReadFile(filepath.Join(root, "dist", f))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `/"`) {
			t.Errorf("%s has a trailing-slash URL: %s", f, b)
		}
	}
}
