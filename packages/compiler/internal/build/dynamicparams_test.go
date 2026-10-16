package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestDynamicParamsDirectiveParsing(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want *bool
	}{
		{"sibling false", `export const dynamicParams = false;`, boolPtr(false)},
		{"sibling true", `export const dynamicParams = true;`, boolPtr(true)},
		{"config false", `export const config = { dynamicParams: false };`, boolPtr(false)},
		{"config true", `export const config = { dynamicParams: true };`, boolPtr(true)},
		{"fn prop false", `export function generateStaticParams(){} generateStaticParams.dynamicParams = false;`, boolPtr(false)},
		{"unset", `export const config = { isr: true };`, nil},
	}
	for _, c := range cases {
		prog := parseInlineSource(c.src)
		cfg := parsePageConfig(prog)
		if c.want == nil {
			if cfg.dynamicParams != nil {
				t.Errorf("%s: expected nil dynamicParams, got %v", c.name, *cfg.dynamicParams)
			}
			continue
		}
		if cfg.dynamicParams == nil {
			t.Errorf("%s: expected %v, got nil", c.name, *c.want)
			continue
		}
		if *cfg.dynamicParams != *c.want {
			t.Errorf("%s: expected %v, got %v", c.name, *c.want, *cfg.dynamicParams)
		}
	}
}

func TestDynamicParamsAllowedResolution(t *testing.T) {
	if !dynamicParamsAllowed(parseInlineSource(`export default function P(){}`), false) {
		t.Error("default (no static mode) should allow dynamic params")
	}
	if dynamicParamsAllowed(parseInlineSource(`export default function P(){}`), true) {
		t.Error("static output mode should close dynamic params by default")
	}
	if !dynamicParamsAllowed(parseInlineSource(`export const dynamicParams = true;`), true) {
		t.Error("page-level dynamicParams=true should override static mode")
	}
	if dynamicParamsAllowed(parseInlineSource(`export const dynamicParams = false;`), false) {
		t.Error("page-level dynamicParams=false should close params")
	}
}

func boolPtr(b bool) *bool { return &b }

// TestStaticOnlyDynamicRouteBuild drives a build of a dynamic route with
// `dynamicParams = false` and asserts the `[param]` fallback template is not
// emitted, the concrete generateStaticParams page is, and the manifest records
// the route as static-only.
func TestStaticOnlyDynamicRouteBuild(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/blog/[slug].tsx", `export function generateStaticParams() {
  return [{ slug: 'hello' }, { slug: 'world' }];
}
export const dynamicParams = false;
export default function Post(props) {
  return <h1>Post {props.params?.slug}</h1>;
}`)

	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	// Concrete pages baked.
	for _, slug := range []string{"hello", "world"} {
		p := filepath.Join(root, "dist", "blog", slug, "index.html")
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected concrete page %s: %v", p, err)
		}
	}
	// Fallback template NOT emitted.
	if _, err := os.Stat(filepath.Join(root, "dist", "blog", "[slug]", "index.html")); err == nil {
		t.Error("static-only route should not emit a [slug] fallback template")
	}
	// Manifest records it.
	data, _ := os.ReadFile(filepath.Join(root, "dist", "manifest.json"))
	if !containsStr(string(data), `"/blog/[slug]"`) || !containsStr(string(data), "staticOnlyRoutes") {
		t.Errorf("manifest should list /blog/[slug] as static-only:\n%s", data)
	}
}

// TestDynamicByDefaultKeepsFallback verifies that without the directive (and no
// static output mode) the fallback template is still emitted.
func TestDynamicByDefaultKeepsFallback(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/blog/[slug].tsx", `export function generateStaticParams() {
  return [{ slug: 'hello' }];
}
export default function Post(props) {
  return <h1>Post {props.params?.slug}</h1>;
}`)

	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "blog", "[slug]", "index.html")); err != nil {
		t.Errorf("dynamic-by-default route should emit the fallback template: %v", err)
	}
}

// TestStaticOutputModeClosesDynamicRoutes verifies `output: "static"` closes
// dynamic routes globally, and `dynamicParams = true` re-opens one.
func TestStaticOutputModeClosesDynamicRoutes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/blog/[slug].tsx", `export function generateStaticParams() { return [{ slug: 'a' }]; }
export default function Post(props) { return <h1>{props.params?.slug}</h1>; }`)
	writeTestFile(t, root, "src/pages/open/[id].tsx", `export function generateStaticParams() { return [{ id: '1' }]; }
export const dynamicParams = true;
export default function Open(props) { return <h1>{props.params?.id}</h1>; }`)

	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	cfg.Output = "static"
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "dist", "blog", "[slug]", "index.html")); err == nil {
		t.Error("static output mode should remove /blog/[slug] fallback")
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "open", "[id]", "index.html")); err != nil {
		t.Error("dynamicParams=true should keep /open/[id] fallback even in static mode")
	}
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOfStr(haystack, needle) >= 0)
}

func indexOfStr(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
