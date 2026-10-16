package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

func TestExtractParamNames(t *testing.T) {
	tmpDir := t.TempDir()
	pagesDir := filepath.Join(tmpDir, "src", "pages")
	tests := []struct {
		pagePath string
		pagesDir string
		want     []string
	}{
		{
			pagePath: filepath.Join(pagesDir, "video", "[id].tsx"),
			pagesDir: pagesDir,
			want:     []string{"id"},
		},
		{
			pagePath: filepath.Join(pagesDir, "user", "[username]", "posts", "[postId].tsx"),
			pagesDir: pagesDir,
			want:     []string{"username", "postId"},
		},
		{
			pagePath: filepath.Join(pagesDir, "about.tsx"),
			pagesDir: pagesDir,
			want:     nil,
		},
		{
			pagePath: filepath.Join(pagesDir, "index.tsx"),
			pagesDir: pagesDir,
			want:     nil,
		},
	}

	for _, tt := range tests {
		got := extractParamNames(tt.pagePath, tt.pagesDir)
		if len(got) != len(tt.want) {
			t.Errorf("extractParamNames(%q, %q) = %v, want %v", tt.pagePath, tt.pagesDir, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("extractParamNames(%q, %q)[%d] = %q, want %q", tt.pagePath, tt.pagesDir, i, got[i], tt.want[i])
			}
		}
	}
}

func TestIsDynamicRoute(t *testing.T) {
	tmpDir := t.TempDir()
	pagesDir := filepath.Join(tmpDir, "src", "pages")
	tests := []struct {
		pagePath string
		pagesDir string
		want     bool
	}{
		{filepath.Join(pagesDir, "video", "[id].tsx"), pagesDir, true},
		{filepath.Join(pagesDir, "user", "[username]", "posts", "[postId].tsx"), pagesDir, true},
		{filepath.Join(pagesDir, "about.tsx"), pagesDir, false},
		{filepath.Join(pagesDir, "index.tsx"), pagesDir, false},
	}

	for _, tt := range tests {
		got := isDynamicRoute(tt.pagePath, tt.pagesDir)
		if got != tt.want {
			t.Errorf("isDynamicRoute(%q, %q) = %v, want %v", tt.pagePath, tt.pagesDir, got, tt.want)
		}
	}
}

func TestExecuteGenerateStaticParams(t *testing.T) {
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

	paramSets, err := executeGenerateStaticParams(pagePath, nil, "")
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

func TestResolveStaticParamsPages(t *testing.T) {
	tmpDir := t.TempDir()
	pagesDir := filepath.Join(tmpDir, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a static page (no generateStaticParams)
	aboutContent := `export default function About() { return <div>About</div>; }`
	if err := os.WriteFile(filepath.Join(pagesDir, "about.tsx"), []byte(aboutContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a dynamic page with generateStaticParams
	videoContent := `
export default function VideoPage({ params }: { params: { id: string } }) {
  return <div>Video {params.id}</div>;
}
export function generateStaticParams() {
  return [
    { id: 'abc' },
    { id: 'xyz' },
  ];
}
`
	pagePath := filepath.Join(pagesDir, "video", "[id].tsx")
	if err := os.MkdirAll(filepath.Dir(pagePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pagePath, []byte(videoContent), 0644); err != nil {
		t.Fatal(err)
	}

	b := New(tmpDir, &config.Config{
		PagesDir: filepath.Join(tmpDir, "src", "pages"),
	})

	pages := []string{
		filepath.Join(pagesDir, "about.tsx"),
		pagePath,
	}

	expanded, err := b.resolveStaticParamsPages(pages)
	if err != nil {
		t.Fatalf("resolveStaticParamsPages: %v", err)
	}

	if len(expanded) != 2 {
		t.Fatalf("expected 2 expanded pages, got %d", len(expanded))
	}

	// Check that outPath has the params substituted
	for _, ep := range expanded {
		if !strings.Contains(ep.OutPath, "video/") {
			t.Errorf("expected outPath to contain 'video/', got %q", ep.OutPath)
		}
	}

	if expanded[0].Params["id"] != "abc" {
		t.Errorf("expanded[0].Params[id] = %q, want %q", expanded[0].Params["id"], "abc")
	}
	if expanded[1].Params["id"] != "xyz" {
		t.Errorf("expanded[1].Params[id] = %q, want %q", expanded[1].Params["id"], "xyz")
	}
}

func TestInjectStaticParams(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "params object",
			src:  "export default function VideoPage({ params }: { params: { id: string } }) {\n  return <div>Video {params.id}</div>;\n}",
			want: "<div>Video abc-123</div>",
		},
		{
			name: "direct destructure",
			src:  "export default function VideoPage({ id }: { id: string }) {\n  return <div>Video {id}</div>;\n}",
			want: "<div>Video abc-123</div>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			pagesDir := filepath.Join(tmpDir, "src", "pages")
			pagePath := filepath.Join(pagesDir, "video", "[id].tsx")
			_ = os.MkdirAll(filepath.Dir(pagePath), 0755)
			if err := os.WriteFile(pagePath, []byte(tc.src), 0644); err != nil {
				t.Fatal(err)
			}

			bnd := bundler.New(tmpDir)
			bundle, err := bnd.Bundle(pagePath)
			if err != nil {
				t.Fatalf("bundle: %v", err)
			}
			ent := findEntryModule(bundle.Modules)
			ann := annotator.Annotate(ent.Program, nil, pagePath, ent.SourceCode)
			tree := irtree.Build(ent.Program, ann)
			injectStaticParams(tree, map[string]string{"id": "abc-123"})
			em := renderer.NewEmitter()
			res := em.Emit(tree)
			if res.HTML != tc.want {
				t.Errorf("HTML = %q, want %q", res.HTML, tc.want)
			}
		})
	}
}

// TestBuildStaticParamsPageRunsPlugins verifies statically generated
// dynamic-param pages flow through the same plugin hook chain as regular pages
// (AfterParse + AfterRender), not just AfterPage. Regression: buildStaticParamsPage
// previously skipped every hook except the AfterPage pass in the caller.
func TestBuildStaticParamsPageRunsPlugins(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	outDir := filepath.Join(root, "dist")
	pagePath := filepath.Join(pagesDir, "video", "[id].tsx")
	if err := os.MkdirAll(filepath.Dir(pagePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	pageSrc := `export default function VideoPage({ params }: { params: { id: string } }) {
  return <div>Video {params.id}</div>;
}`
	if err := os.WriteFile(pagePath, []byte(pageSrc), 0644); err != nil {
		t.Fatal(err)
	}

	// A JS plugin that proves AfterParse and AfterRender ran.
	pluginDir := filepath.Join(root, "plugins", "static-hook")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	pluginSrc := `
export default {
  name: "static-hook",
  order: 10,
  hooks: {
    AfterParse(ctx, options, krate) { krate.emitFile("afterparse-ran.txt", "1"); return {}; },
    AfterRender(ctx, options, krate) { return { rawCSS: ".static-param-hook{}" }; },
  },
};
`
	pluginPath := filepath.Join(pluginDir, "index.js")
	if err := os.WriteFile(pluginPath, []byte(pluginSrc), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		PagesDir: pagesDir,
		OutDir:   outDir,
		Entry:    "src/index.tsx",
	}
	cfg.Plugins = []config.PluginConfig{{Name: "static-hook", Module: pluginPath}}
	b := New(root, cfg)

	result, rawCSS, err := b.buildStaticParamsPage(staticParamsPage{
		PagePath: pagePath,
		Params:   map[string]string{"id": "abc-123"},
		OutPath:  "video/abc-123",
	})
	if err != nil {
		t.Fatalf("buildStaticParamsPage: %v", err)
	}

	// AfterParse side-effect (krate.emitFile) reached disk.
	if _, err := os.Stat(filepath.Join(outDir, "afterparse-ran.txt")); err != nil {
		t.Errorf("AfterParse did not run for the static-param page: %v", err)
	}
	// AfterRender rawCSS reached the page bundle.
	if !strings.Contains(rawCSS, ".static-param-hook{}") || !strings.Contains(result.CSS, ".static-param-hook{}") {
		t.Errorf("AfterRender did not run for the static-param page: rawCSS=%q result.CSS=%q", rawCSS, result.CSS)
	}
}

// buildTreeForSrc parses/bundles a page source and returns its IR tree.
func buildTreeForSrc(t *testing.T, root, pagePath, src string) *irtree.ComponentTree {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(pagePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pagePath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	bnd := bundler.New(root)
	bundle, err := bnd.Bundle(pagePath)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	ent := findEntryModule(bundle.Modules)
	if ent == nil || ent.Program == nil {
		t.Fatal("no entry module")
	}
	ann := annotator.Annotate(ent.Program, &config.Config{}, pagePath, ent.SourceCode)
	return irtree.Build(ent.Program, ann)
}

// TestInjectDynamicRoutePlaceholdersStaticRoot verifies a static route root is
// SSREval'd with sentinels for every [param].
func TestInjectDynamicRoutePlaceholdersStaticRoot(t *testing.T) {
	root := t.TempDir()
	pagePath := filepath.Join(root, "src", "pages", "video", "[id].tsx")
	src := `export default function VideoPage(props: { params?: { id: string } }) {
  const id = props.params?.id || "unknown";
  return <div>Video {id}</div>;
}`
	tree := buildTreeForSrc(t, root, pagePath, src)
	if tree.Root.Tier != irtree.TierStatic {
		t.Fatalf("expected static root tier, got %v", tree.Root.Tier)
	}
	injectDynamicRoutePlaceholders(tree, []string{"id"})
	if !tree.Root.IsSSREval {
		t.Fatal("static dynamic-route root should be SSREval'd with sentinels")
	}
	if got := tree.Root.SSREvalBindings["id"]; got != dynamicParamSentinel("id") {
		t.Errorf("binding id = %q, want sentinel %q", got, dynamicParamSentinel("id"))
	}
	if got := tree.Root.SSREvalBindings["params"]; !strings.Contains(got, dynamicParamSentinel("id")) {
		t.Errorf("params binding = %q, want it to carry the sentinel", got)
	}
}

// TestInjectDynamicRoutePlaceholdersSkipsClientRoot verifies an interactive
// dynamic-route root is NOT frozen to a sentinel: it renders params reactively
// after hydration, so the server must not substitute a static placeholder.
func TestInjectDynamicRoutePlaceholdersSkipsClientRoot(t *testing.T) {
	root := t.TempDir()
	pagePath := filepath.Join(root, "src", "pages", "video", "[id].tsx")
	src := `export default function VideoPage() {
  const [id, setId] = createSignal("x");
  return <button onClick={() => setId(id() + "!")}>{id()}</button>;
}`
	tree := buildTreeForSrc(t, root, pagePath, src)
	if tree.Root.Tier != irtree.TierClient {
		t.Fatalf("expected client root tier, got %v", tree.Root.Tier)
	}
	injectDynamicRoutePlaceholders(tree, []string{"id"})
	if tree.Root.IsSSREval {
		t.Error("client dynamic-route root must not be frozen to a sentinel")
	}
}
