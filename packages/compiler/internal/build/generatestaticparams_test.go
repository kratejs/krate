package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestExecuteGenerateStaticParams(t *testing.T) {
	t.Parallel()
	// Create a temp directory with a TSX file that has generateStaticParams
	tmpDir := t.TempDir()
	pagesDir := filepath.Join(tmpDir, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a page with generateStaticParams
	pageContent := `
export default function VideoPage({ params }: { params: { id: string } }) {
  return <div>Video {params.id}</div>;
}

export function generateStaticParams() {
  return [
    { id: 'abc-123' },
    { id: 'def-456' },
  ];
}
`
	pagePath := filepath.Join(pagesDir, "video", "[id].tsx")
	if err := os.MkdirAll(filepath.Dir(pagePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pagePath, []byte(pageContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(tmpDir, "dist")
	b := New(tmpDir, cfg)

	paramSets, err := b.executeGenerateStaticParams(pagePath, b.newBundler())
	if err != nil {
		t.Fatalf("executeGenerateStaticParams: %v", err)
	}

	if len(paramSets) != 2 {
		t.Fatalf("expected 2 param sets, got %d", len(paramSets))
	}

	if paramSets[0]["id"] != "abc-123" {
		t.Errorf("paramSets[0][id] = %q, want %q", paramSets[0]["id"], "abc-123")
	}
	if paramSets[1]["id"] != "def-456" {
		t.Errorf("paramSets[1][id] = %q, want %q", paramSets[1]["id"], "def-456")
	}
}

// TestExecuteGenerateStaticParamsRuntime exercises the bundled fallback under
// each supported build-time runtime (node, bun, deno). CI installs Bun and Deno
// so this is the runtime matrix gate.
func TestExecuteGenerateStaticParamsRuntime(t *testing.T) {
	t.Parallel()
	for _, rt := range []string{"node", "bun", "deno"} {
		t.Run(rt, func(t *testing.T) {
			if _, err := exec.LookPath(rt); err != nil {
				t.Skipf("%s not available", rt)
			}
			tmpDir := t.TempDir()
			pagesDir := filepath.Join(tmpDir, "src", "pages")
			pagePath := filepath.Join(pagesDir, "video", "[id].tsx")
			if err := os.MkdirAll(filepath.Dir(pagePath), 0755); err != nil {
				t.Fatal(err)
			}
			pageContent := `import { homedir } from "node:os";
export function generateStaticParams() {
  return [{ id: homedir() ? "ok" : "no" }];
}
`
			if err := os.WriteFile(pagePath, []byte(pageContent), 0644); err != nil {
				t.Fatal(err)
			}
			cfg := config.Default()
			cfg.PagesDir = pagesDir
			cfg.OutDir = filepath.Join(tmpDir, "dist")
			cfg.Runtime = rt
			b := New(tmpDir, cfg)
			paramSets, err := b.executeGenerateStaticParams(pagePath, b.newBundler())
			if err != nil {
				t.Fatalf("executeGenerateStaticParams (%s): %v", rt, err)
			}
			if len(paramSets) != 1 || paramSets[0]["id"] != "ok" {
				t.Fatalf("%s param sets: %+v", rt, paramSets)
			}
		})
	}
}

func TestStaticParamsModuleDataDestructuring(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		src   string
		wantA string
		wantB string
	}{
		{
			name: "array destructure after object destructure",
			src: `const ROWS: Record<string, string[]> = { a: ["Alpha", "x"], b: ["Beta", "y"] };
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export default function Page({ params }: { params: { slug: string } }) {
  const { slug } = params;
  const [out] = ROWS[slug];
  return <h1>sync {out}</h1>;
}`,
			wantA: "<h1>sync Alpha</h1>",
			wantB: "<h1>sync Beta</h1>",
		},
		{
			name: "array destructure with direct params member",
			src: `const ROWS: Record<string, string[]> = { a: ["Alpha", "x"], b: ["Beta", "y"] };
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export default function Page({ params }: { params: { slug: string } }) {
  const [out] = ROWS[params.slug];
  return <h1>direct {out}</h1>;
}`,
			wantA: "<h1>direct Alpha</h1>",
			wantB: "<h1>direct Beta</h1>",
		},
		{
			name: "property access after object destructure",
			src: `const ITEMS: Record<string, { title: string }> = { a: { title: "Alpha" }, b: { title: "Beta" } };
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export default function Page({ params }: { params: { slug: string } }) {
  const { slug } = params;
  const title = ITEMS[slug].title;
  return <h1>{title}</h1>;
}`,
			wantA: "<h1>Alpha</h1>",
			wantB: "<h1>Beta</h1>",
		},
		{
			name: "nested object destructuring in body",
			src: `const ITEMS: Record<string, { title: string; meta: { tag: string } }> = {
  a: { title: "Alpha", meta: { tag: "one" } },
  b: { title: "Beta", meta: { tag: "two" } },
};
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export default function Page({ params }: { params: { slug: string } }) {
  const { slug } = params;
  const { title, meta: { tag } } = ITEMS[slug];
  return <h1>{title} ({tag})</h1>;
}`,
			wantA: "<h1>Alpha (one)</h1>",
			wantB: "<h1>Beta (two)</h1>",
		},
		{
			name: "nested object destructuring in parameter",
			src: `const ROWS: Record<string, string[]> = { a: ["Alpha", "x"], b: ["Beta", "y"] };
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export default function Page({ params: { slug } }: { params: { slug: string } }) {
  const [out] = ROWS[slug];
  return <h1>param {out}</h1>;
}`,
			wantA: "<h1>param Alpha</h1>",
			wantB: "<h1>param Beta</h1>",
		},
		{
			name: "renamed params parameter",
			src: `const ROWS: Record<string, string[]> = { a: ["Alpha", "x"], b: ["Beta", "y"] };
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export default function Page({ params: routeParams }: { params: { slug: string } }) {
  const { slug } = routeParams;
  const [out] = ROWS[slug];
  return <h1>alias {out}</h1>;
}`,
			wantA: "<h1>alias Alpha</h1>",
			wantB: "<h1>alias Beta</h1>",
		},
		{
			name: "plain props parameter",
			src: `const ROWS: Record<string, string[]> = { a: ["Alpha", "x"], b: ["Beta", "y"] };
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export default function Page(props: { params: { slug: string } }) {
  const { slug } = props.params;
  const [out] = ROWS[slug];
  return <h1>props {out}</h1>;
}`,
			wantA: "<h1>props Alpha</h1>",
			wantB: "<h1>props Beta</h1>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, root := scProject(t, map[string]string{
				"src/pages/items/[slug].tsx": tc.src,
			})
			if err := b.BuildAll(); err != nil {
				t.Fatalf("BuildAll failed: %v", err)
			}
			for slug, want := range map[string]string{"a": tc.wantA, "b": tc.wantB} {
				dir := filepath.Join(root, "dist", "items", slug)
				htmlBytes, err := os.ReadFile(filepath.Join(dir, "index.html"))
				if err != nil {
					t.Fatalf("read %s/index.html: %v", slug, err)
				}
				html := string(htmlBytes)
				if !strings.Contains(html, want) {
					t.Errorf("%s HTML missing %q:\n%s", slug, want, html)
				}
				if strings.Contains(html, "<!--k:") {
					t.Errorf("%s HTML should not contain hydration slot markers:\n%s", slug, html)
				}
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range entries {
					if strings.HasSuffix(e.Name(), ".js") {
						t.Errorf("%s emitted unexpected client JS %s", slug, e.Name())
					}
				}
			}
		})
	}
}

func TestStaticParamsClientFallbackParams(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/pages/live/[slug].tsx": `export function generateStaticParams() { return [{ slug: "a" }]; }
export default function Page({ params }: { params: { slug: string } }) {
  const value = Object.values(params).join(",") + ":" + Math.random();
  return <h1>{value}</h1>;
}`,
		"src/pages/destruct/[slug].tsx": `export function generateStaticParams() { return [{ slug: "a" }]; }
export default function Page({ params }: { params: { slug: string } }) {
  const { slug } = params;
  const [value] = [slug + ":" + Math.random()];
  return <h1>{value}</h1>;
}`,
		"src/pages/dyn/[slug].tsx": `const ROWS: Record<string, string[]> = { a: ["Alpha", "x"] };
export default function Page({ params }: { params: { slug: string } }) {
  const { slug } = params;
  const [out] = ROWS[slug];
  return <h1>{out}</h1>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll failed: %v", err)
	}

	readHydrationJS := func(relDir string) string {
		t.Helper()
		dir := filepath.Join(root, "dist", filepath.FromSlash(relDir))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("readdir %s: %v", relDir, err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "index.") && strings.HasSuffix(e.Name(), ".js") {
				data, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
		}
		t.Fatalf("no hydration JS found in %s", relDir)
		return ""
	}

	// Concrete page reading `params` directly must bind its concrete params object.
	liveJS := readHydrationJS("live/a")
	if !strings.Contains(liveJS, `{slug:"a"}`) && !strings.Contains(liveJS, `{slug:'a'}`) && !strings.Contains(liveJS, `{"slug":"a"}`) {
		t.Errorf("live/a hydration JS missing concrete params binding:\n%s", liveJS)
	}
	if strings.Contains(liveJS, "=params") || strings.Contains(liveJS, "(params)") {
		t.Errorf("live/a hydration JS references undeclared params:\n%s", liveJS)
	}

	// Concrete page with destructured `slug` feeding a runtime expression must
	// bake `slug` without leaving a bare `params` reference.
	destructJS := readHydrationJS("destruct/a")
	if !strings.Contains(destructJS, `"a"`) && !strings.Contains(destructJS, `'a'`) {
		t.Errorf("destruct/a hydration JS missing concrete slug value:\n%s", destructJS)
	}
	if strings.Contains(destructJS, "=params") {
		t.Errorf("destruct/a hydration JS references undeclared params:\n%s", destructJS)
	}

	// Dynamic-route template falling back to the client must read window.__KRATE_PARAMS__.
	dynJS := readHydrationJS("dyn/[slug]")
	if !strings.Contains(dynJS, "window.__KRATE_PARAMS__") {
		t.Errorf("dyn/[slug] hydration JS missing window.__KRATE_PARAMS__ binding:\n%s", dynJS)
	}
}
