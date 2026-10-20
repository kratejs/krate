package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/deploy"
)

func TestSubstituteImportMetaURL(t *testing.T) {
	js := "const base = new URL('../models/', import.meta.url);"
	got := substituteImportMetaURL(js, "demo", "index.ab12cd.js", "")
	want := "const base = new URL('../models/', \"/demo/index.ab12cd.js\");"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if !strings.Contains(substituteImportMetaURL("no import.meta here", "x", "y.js", ""), "import.meta") {
		t.Fatal("must be a no-op when import.meta.url is absent")
	}
	// basePath is applied so relative imports resolve under the sub-path.
	withBase := substituteImportMetaURL(js, "demo", "index.ab12cd.js", "/docs")
	if !strings.Contains(withBase, `"/docs/demo/index.ab12cd.js"`) {
		t.Fatalf("basePath not applied: %q", withBase)
	}
}

func TestWriteAssetFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "asset.png")
	if err := os.WriteFile(src, []byte("png-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	assets := map[string]string{src: "/assets/logo-x1y2z3.png"}
	if err := b.writeAssetFiles(assets); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "assets", "logo-x1y2z3.png"))
	if err != nil {
		t.Fatalf("copied asset missing: %v", err)
	}
	if string(data) != "png-bytes" {
		t.Fatalf("asset contents mismatch: %q", data)
	}
	// Idempotent second pass must not error.
	if err := b.writeAssetFiles(assets); err != nil {
		t.Fatalf("second write: %v", err)
	}
}

func TestBuildTestProject(t *testing.T) {
	// Find the examples project root relative to this package
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// from packages/compiler/internal/build/ -> packages/compiler/ -> examples
	projectRoot := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", "..", "examples"))
	if _, err := os.Stat(projectRoot); os.IsNotExist(err) {
		requireE2E(t, "examples not found at %s", projectRoot)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	b := New(projectRoot, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	// Verify output directory exists
	outDir := cfg.OutDir
	if !dirExists(outDir) {
		t.Fatalf("output directory %s does not exist", outDir)
	}

	// Verify key output files
	checks := []struct {
		path     string
		contains []string // substrings that must appear in the file
	}{
		{path: filepath.Join(outDir, "index.html"), contains: []string{"<!DOCTYPE html>", "<html", "<head", "<body"}},
		{path: filepath.Join(outDir, "about", "index.html")},
		{path: filepath.Join(outDir, "blog", "index.html")},
		{path: filepath.Join(outDir, "test", "index.html")},
		{path: filepath.Join(outDir, "video", "[id]", "index.html"), contains: []string{"<!DOCTYPE html>", "video"}},
		{path: filepath.Join(outDir, "syntax-robustness", "index.html"), contains: []string{"<!DOCTYPE html>", "Syntax Robustness"}},
		{path: filepath.Join(outDir, "go-component", "index.html"), contains: []string{"<!DOCTYPE html>", "krate-go-badge", "go-native"}},
		{path: filepath.Join(outDir, "server-runtime-demo", "index.html"), contains: []string{"go-server-time", "Go build time"}},
		{path: filepath.Join(outDir, "manifest.json"), contains: []string{"\"pages\"", "\"runtime\": \"go\"", "\"component\": \"GoRuntimeTime\""}},
	}

	for _, c := range checks {
		info, err := os.Stat(c.path)
		if os.IsNotExist(err) {
			t.Errorf("missing output: %s", c.path)
			continue
		}
		if info.IsDir() {
			t.Errorf("expected file, got directory: %s", c.path)
			continue
		}
		if len(c.contains) > 0 {
			data, err := os.ReadFile(c.path)
			if err != nil {
				t.Errorf("reading %s: %v", c.path, err)
				continue
			}
			content := string(data)
			for _, substr := range c.contains {
				if !strings.Contains(content, substr) {
					t.Errorf("%s missing expected content: %q", c.path, substr)
				}
			}
		}
	}

	// Config-surface features exercised by examples/krate.config.ts.
	if cfg.Site.URL != "https://example.com" || cfg.SEO.BaseURL != cfg.Site.URL {
		t.Errorf("site block not folded into SEO: site=%q seo=%q", cfg.Site.URL, cfg.SEO.BaseURL)
	}
	if cfg.Deploy.Target != "netlify" {
		t.Errorf("deploy.target = %q, want netlify", cfg.Deploy.Target)
	}
	if !cfg.Compress.Gzip {
		t.Error("compress.gzip not parsed")
	}
	if cfg.Images.Quality != 80 {
		t.Errorf("images.quality = %d, want 80", cfg.Images.Quality)
	}
	if len(cfg.Markdown.Plugins) == 0 {
		t.Error("markdown.plugins not parsed")
	}
	// `extends` is consumed during load; the proof it merged is that the base's
	// `tailwind.enabled` is present although the main config only sets
	// `markdown.plugins`.
	if !cfg.Tailwind.Enabled {
		t.Error("extends merge not applied (tailwind.enabled from shared.config.ts missing)")
	}
	if cfg.Security.FrameOptions != "SAMEORIGIN" || len(cfg.Headers) == 0 {
		t.Errorf("security/headers not parsed: %+v / %d", cfg.Security, len(cfg.Headers))
	}
	if len(cfg.Head.Scripts) == 0 || len(cfg.Head.Meta) == 0 {
		t.Errorf("head not parsed: %+v", cfg.Head)
	}

	// Output checks for those features.
	if data, err := os.ReadFile(filepath.Join(outDir, "markdown-demo", "index.html")); err != nil {
		t.Errorf("reading markdown page: %v", err)
	} else if !strings.Contains(string(data), "data-markdown-plugin") {
		t.Error("markdown.plugins transform did not run")
	}
	if data, err := os.ReadFile(filepath.Join(outDir, "index.html")); err == nil {
		h := string(data)
		for _, want := range []string{"example-head", "/analytics.js"} {
			if !strings.Contains(h, want) {
				t.Errorf("index.html missing %q (head/analytics injection)", want)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "index.html.gz")); err != nil {
		t.Errorf("precompressed sibling missing (compress.gzip): %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(outDir, "robots.txt")); err != nil {
		t.Errorf("robots.txt missing: %v", err)
	} else if !strings.Contains(string(data), "Allow: /") {
		t.Errorf("robots plugin options not applied:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(outDir, "llms.txt")); err != nil {
		t.Errorf("llms.txt missing: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(outDir, "docs", "getting-started", "index.html")); err == nil {
		h := string(data)
		if !strings.Contains(h, "edit/main/examples/src/content") {
			t.Error("docs editLink.pattern not applied")
		}
		if !strings.Contains(h, "Search the docs") || !strings.Contains(h, "krate-docs-search-kbd") {
			t.Error("docs search placeholder/hotkey not applied")
		}
		if !strings.Contains(h, "data-markdown-plugin") {
			t.Error("markdown.plugins did not run on docs pages")
		}
	}

	if data, err := os.ReadFile(filepath.Join(outDir, "lang-demo", "index.html")); err != nil {
		t.Errorf("reading lang-demo page: %v", err)
	} else if !strings.Contains(string(data), "lang=fr") && !strings.Contains(string(data), `lang="fr"`) {
		t.Error("per-page `export const lang` not applied to <html lang>")
	}

	// Deploy adapters emit explicit header rules for static hosts.
	if _, err := deploy.Emit("netlify", cfg, outDir); err != nil {
		t.Errorf("deploy.Emit: %v", err)
	} else if headers, err := os.ReadFile(filepath.Join(outDir, "_headers")); err != nil {
		t.Error("deploy did not write _headers")
	} else if !strings.Contains(string(headers), "/_krate/images/*") {
		t.Errorf("_headers missing images rule:\n%s", headers)
	}

	// Verify syntax-robustness page renders alias-imported components. The
	// Badge component is imported via `@/components/ui/badge`; its
	// `<span class="badge">` markup only appears if the @-alias imports
	// resolved through the bundler AND the imported component was merged into
	// the annotation set (page-local MiscDemo renders it in both slots).
	syntaxPage := filepath.Join(outDir, "syntax-robustness", "index.html")
	if data, err := os.ReadFile(syntaxPage); err == nil {
		content := string(data)
		if !strings.Contains(content, "Syntax Robustness") {
			t.Errorf("syntax-robustness page missing heading")
		}
		// Minification drops the quotes around attribute values, so match both.
		badge1 := `<span class="badge">fragment + spread attrs</span>`
		badge1Min := "<span class=badge>fragment + spread attrs</span>"
		badge2 := `<span class="badge">fragments render children</span>`
		badge2Min := "<span class=badge>fragments render children</span>"
		if !strings.Contains(content, badge1) && !strings.Contains(content, badge1Min) {
			t.Errorf("syntax-robustness page missing alias-imported <Badge> markup (alias import or module merge dropped it)")
		}
		if !strings.Contains(content, badge2) && !strings.Contains(content, badge2Min) {
			t.Errorf("syntax-robustness page missing second alias-imported <Badge> markup")
		}
	}

	// Dynamic-route regression: the reusable [id] template must be built with a
	// replaceable sentinel (not a leaked variable name or a folded literal), so
	// the server can substitute the matched URL segment anywhere the param is
	// read - body text, <title>, and meta attributes.
	templatePath := filepath.Join(outDir, "video", "[id]", "index.html")
	if data, err := os.ReadFile(templatePath); err == nil {
		content := string(data)
		if !strings.Contains(content, "__KRATE_PARAM_id__") {
			t.Errorf("dynamic route template missing param sentinel (dynamic params not bound in props fold)")
		}
		if strings.Contains(content, ">videoId<") || strings.Contains(content, "Video ID: <strong>videoId") {
			t.Errorf("dynamic route template leaked the local variable name instead of a sentinel")
		}
	} else {
		t.Errorf("reading dynamic route template: %v", err)
	}

	// generateStaticParams pages are built separately with concrete values and
	// must not contain a sentinel.
	for _, id := range []string{"abc123", "demo-42"} {
		p := filepath.Join(outDir, "video", id, "index.html")
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("reading static param page %s: %v", id, err)
			continue
		}
		content := string(data)
		if !strings.Contains(content, id) {
			t.Errorf("static param page %s missing its concrete id value", id)
		}
		if strings.Contains(content, "__KRATE_PARAM_") {
			t.Errorf("static param page %s unexpectedly contains a dynamic sentinel", id)
		}
		if strings.Contains(content, ">unknown<") || strings.Contains(content, "videoId") {
			t.Errorf("static param page %s rendered a placeholder instead of its param", id)
		}
	}

	// Verify JS hydration files exist (hashed names)
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	hasJS := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "index.") && strings.HasSuffix(e.Name(), ".js") && !e.IsDir() {
			hasJS = true
			break
		}
	}
	if !hasJS {
		t.Error("no hashed JS hydration file found in output directory")
	}

	// Verify Tailwind CSS generation: the site-global stylesheet (linked on
	// every page) should contain utility rules. Per-page stylesheets are
	// separate now, so scan the root styles.*.css files for one that carries
	// the generated utilities.
	twChecks := []struct {
		desc string
		seek string
	}{
		{"padding utility", ".p-6"},
		{"margin utility", ".m-4"},
		{"flex utility", ".flex"},
		{"flex-wrap utility", ".flex-wrap"},
		{"gap utility", ".gap-4"},
		{"border-radius utility", ".rounded-lg"},
		{"rounded-xl utility", ".rounded-xl"},
		{"shadow utility", ".shadow-lg"},
		{"font-bold utility", ".font-bold"},
		{"text color utility", ".text-gray-600"},
		{"bg color utility", ".bg-blue-50"},
		{"text size utility", ".text-2xl"},
	}
	cssFiles := []string{}
	for _, c := range entries {
		if strings.HasPrefix(c.Name(), "styles.") && strings.HasSuffix(c.Name(), ".css") && !c.IsDir() {
			data, err := os.ReadFile(filepath.Join(outDir, c.Name()))
			if err != nil {
				t.Fatalf("reading CSS file %s: %v", c.Name(), err)
			}
			cssFiles = append(cssFiles, string(data))
		}
	}
	if len(cssFiles) == 0 {
		t.Error("no styles.*.css file found")
	}
	for _, tw := range twChecks {
		found := false
		for _, css := range cssFiles {
			if strings.Contains(css, tw.seek) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Tailwind CSS missing %s: %q not in any stylesheet", tw.desc, tw.seek)
		}
	}

	// Verify docs plugin generated pages
	docChecks := []string{
		filepath.Join(outDir, "docs", "index.html"),
		filepath.Join(outDir, "docs", "getting-started", "index.html"),
		filepath.Join(outDir, "docs", "configuration", "index.html"),
		filepath.Join(outDir, "docs", "guides", "advanced", "index.html"),
		filepath.Join(outDir, "docs", "data", "sidebar.json"),
		filepath.Join(outDir, "docs", "data", "search-index.json"),
	}
	for _, docPath := range docChecks {
		if !fileExists(docPath) {
			t.Errorf("docs plugin did not generate: %s", docPath)
		}
	}

	// Verify docs HTML contains expected elements
	docPage := filepath.Join(outDir, "docs", "getting-started", "index.html")
	if data, err := os.ReadFile(docPage); err == nil {
		content := string(data)
		for _, want := range []string{"sidebar", "breadcrumbs", "docs-content", "nav-prev", "nav-next"} {
			if !strings.Contains(content, want) {
				t.Errorf("docs page missing %q", want)
			}
		}
	}

	/* Clean up
	if err := os.RemoveAll(outDir); err != nil {
		t.Errorf("cleanup: %v", err)
	}
	_ = os.MkdirAll(outDir, 0755)*/
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// TestBuildRejectsUnsupportedRenderSyntax verifies that a page whose
// SSR-evaluated component uses an expression Krate cannot statically evaluate
// (e.g. `this`) fails the build with a clear render error instead of silently
// emitting empty/wrong output.
func TestBuildRejectsUnsupportedRenderSyntax(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", "..", "examples"))
	if _, err := os.Stat(projectRoot); os.IsNotExist(err) {
		requireE2E(t, "examples not found at %s", projectRoot)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	pagePath := filepath.Join(cfg.PagesDir, "_render_error.tsx")
	src := `function Bad(props) {
  return <div>{this}</div>;
}
export default function Page() {
  return <Bad />;
}`
	if err := os.WriteFile(pagePath, []byte(src), 0644); err != nil {
		t.Fatalf("writing scratch page: %v", err)
	}
	defer os.Remove(pagePath)

	b := New(projectRoot, cfg)
	_, _, err = b.buildPage(pagePath)
	if err == nil {
		t.Fatal("expected buildPage to return a render error for unsupported syntax, got nil")
	}
	if !strings.Contains(err.Error(), "render failed") {
		t.Errorf("expected render-failed error, got: %v", err)
	}
}

func TestFindServerRendererSourceNpmLayout(t *testing.T) {
	// Simulate a consumer app whose node_modules/@krate/runtime ships only the
	// compiled dist/ (matching the published package's "files": ["dist"]).
	fakeRoot := t.TempDir()
	runtimePkg := filepath.Join(fakeRoot, "node_modules", "@krate", "runtime")
	if err := os.MkdirAll(filepath.Join(runtimePkg, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	rendererJS := "export default function createServer(){return null;}"
	if err := os.WriteFile(filepath.Join(runtimePkg, "dist", "server-renderer.js"), []byte(rendererJS), 0644); err != nil {
		t.Fatal(err)
	}

	src := findServerRendererSource(fakeRoot)
	if src == "" {
		t.Fatal("findServerRendererSource failed to locate dist/server-renderer.js in an npm layout")
	}
	if !strings.HasSuffix(src, "server-renderer.js") {
		t.Errorf("expected the compiled renderer, got %q", src)
	}

	// Staging the compiled JS must produce a bundle without needing the source tree.
	outDir := filepath.Join(fakeRoot, "dist")
	staged := stageServerRenderer(fakeRoot, outDir)
	if staged == "" {
		t.Fatal("stageServerRenderer failed to stage from compiled dist renderer")
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("staged driver missing: %v", err)
	}
}

func TestStageServerRenderer(t *testing.T) {
	// Stage from the monorepo runtime source (repo root discovered by walking up).
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	src := findServerRendererSource(repoRoot)
	if src == "" {
		requireE2E(t, "server-renderer source not found under %s", repoRoot)
	}

	// Fake project root so findRendererScript locates dist/.krate/ exactly as
	// it would after a real build with OutDir=dist.
	fakeRoot := t.TempDir()
	outDir := filepath.Join(fakeRoot, "dist")
	staged := stageServerRenderer(repoRoot, outDir)
	if staged == "" {
		t.Fatal("stageServerRenderer returned empty path")
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("staged driver missing: %v", err)
	}
	data, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "createServer") {
		t.Errorf("staged driver missing node:http server, got:\n%s", content[:200])
	}
	if !strings.Contains(content, "renderToString") {
		t.Errorf("staged driver missing bundled SSR runtime (renderToString)")
	}

	// Node must be able to parse the bundled driver.
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}
	if out, err := exec.Command(node, "--check", staged).CombinedOutput(); err != nil {
		t.Fatalf("node --check failed: %v\n%s", err, out)
	}

	// The SSR server manager must prefer the staged driver over the TS source.
	server := NewSSRServer(fakeRoot, 0, "node")
	got := server.findRendererScript()
	if got == "" {
		t.Fatal("findRendererScript returned empty path")
	}
	if got != staged {
		t.Errorf("findRendererScript = %q, want staged driver %q", got, staged)
	}
}

func repoRootPath() (string, error) {
	pkgDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// from packages/compiler/internal/build/ -> repo root
	root := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "packages")); err != nil {
		return "", fmt.Errorf("repo root not found at %s", root)
	}
	return root, nil
}
