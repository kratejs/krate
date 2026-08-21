package build

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"krate-compiler/internal/config"
)

// testProjectPath returns the examples root used by the integration tests.
func testProjectPath(t *testing.T) string {
	t.Helper()
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", "..", "examples"))
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Skipf("examples not found at %s", root)
	}
	return root
}

func buildTestProject(t *testing.T) string {
	t.Helper()
	root := testProjectPath(t)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return cfg.OutDir
}

func readOut(t *testing.T, outDir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(data)
}

// TestBuildLayoutChildrenInjection verifies a page's HTML is wrapped by the
// _layout.tsx shell (nav + footer) with the page content injected at {children}.
func TestBuildLayoutChildrenInjection(t *testing.T) {
	outDir := buildTestProject(t)
	html := readOut(t, outDir, filepath.Join("about", "index.html"))
	if !strings.Contains(html, `<nav>`) {
		t.Errorf("expected layout nav in page, got:\n%.400s", html)
	}
	if !strings.Contains(html, `&copy; 2026`) {
		t.Errorf("expected layout footer in page, got:\n%.400s", html)
	}
	if !strings.Contains(html, `class=layout`) {
		t.Errorf("expected layout wrapper class, got:\n%.400s", html)
	}
}

// TestBuildLoadingPageRendered verifies src/pages/loading.tsx is emitted and
// rendered into the loading route's HTML.
func TestBuildLoadingPageRendered(t *testing.T) {
	outDir := buildTestProject(t)
	html := readOut(t, outDir, filepath.Join("loading", "index.html"))
	if !strings.Contains(html, "Loading page content...") {
		t.Errorf("expected loading page content rendered, got:\n%.400s", html)
	}
}

// TestBuildMixedTierPage verifies the server-runtime-demo page demonstrates the
// server/runtime split: server components are baked with a real QuickJS
// Date.now() timestamp, runtime components are deferred krate-id placeholders
// backed by a props script.
func TestBuildMixedTierPage(t *testing.T) {
	outDir := buildTestProject(t)
	html := readOut(t, outDir, filepath.Join("server-runtime-demo", "index.html"))

	// Server components are evaluated by QuickJS at build time → a real
	// millisecond epoch timestamp is baked into the static HTML.
	tsRe := regexp.MustCompile(`Compiled at build time: (\d{13})`)
	m := tsRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("expected a 13-digit QuickJS Date.now() timestamp baked at build, got:\n%.600s", html)
	}

	// Runtime components are NOT baked — they produce krate-id placeholders.
	if !strings.Contains(html, `<div krate-id=0>`) || !strings.Contains(html, `<div krate-id=1>`) {
		t.Errorf("expected runtime component placeholders, got:\n%.600s", html)
	}

	// The runtime props script carries resolved props for each placeholder.
	if !strings.Contains(html, `application/krate-runtime`) {
		t.Errorf("expected runtime props script, got:\n%.600s", html)
	}
	if !strings.Contains(html, `"label":"Interactive Widget"`) {
		t.Errorf("expected resolved RuntimeWidget props in script, got:\n%.600s", html)
	}
	if !strings.Contains(html, `"title":"Runtime Card"`) {
		t.Errorf("expected resolved RuntimeCard props in script, got:\n%.600s", html)
	}
}

// TestBuildRuntimeComponentBundlesCompiled verifies runtime component files are
// compiled to self-contained quickjs bundles listed in the manifest.
func TestBuildRuntimeComponentBundlesCompiled(t *testing.T) {
	outDir := buildTestProject(t)
	for _, name := range []string{"RuntimeWidget.runtime.js", "RuntimeCard.runtime.js"} {
		p := filepath.Join(outDir, "server-components", name)
		if !fileExists(p) {
			t.Errorf("expected compiled runtime component bundle: %s", p)
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		if !strings.Contains(string(data), "__krate_render") {
			t.Errorf("%s missing __krate_render entry point", name)
		}
	}
}

// TestGenerateHTMLDevModePreservesInlineScripts verifies dev-mode HTML keeps
// inline Script/Style content and injects the hot-reload script.
func TestGenerateHTMLDevModePreservesInlineScripts(t *testing.T) {
	inline := `<script>console.log("inline")</script>`
	style := `<style>.a{color:red}</style>`
	html := generateHTML(
		"<div>body</div>", "<title>t</title>", inline, style,
		true, "", "runtime.js", "/", true,
	)
	if !strings.Contains(html, `console.log("inline")`) {
		t.Errorf("dev-mode HTML dropped inline script:\n%s", html)
	}
	if !strings.Contains(html, `.a{color:red}`) {
		t.Errorf("dev-mode HTML dropped inline style:\n%s", html)
	}
	if !strings.Contains(html, "__krate/hotreload") {
		t.Errorf("dev-mode HTML missing hot-reload script:\n%s", html)
	}
}

// TestBuildLocalIconsFolder verifies the project-root icons/ folder feature:
// icons/<name>.svg registers <name> as a usable <Icon> name. Local SVGs are
// inlined into the page HTML with their own viewBox preserved, user attributes
// forwarded, and malicious content (scripts, event handlers) sanitized.
func TestBuildLocalIconsFolder(t *testing.T) {
	root := t.TempDir()
	writeFile := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	writeFile("src/pages/index.tsx", `export default function Page() {
  return (
    <div>
      <Icon name="menu" />
      <Icon name="custom32" className="big" />
      <Icon name="menu" stroke-width="3" />
    </div>
  );
}`)
	writeFile("icons/menu.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M4 6h16M4 12h16M4 18h16"/></svg>`)
	writeFile("icons/custom32.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" onclick="alert(1)"><script>alert('x')</script><circle cx="16" cy="16" r="8"/></svg>`)

	cfg := config.Default()
	cfg.Resolve(root)
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	html := readOut(t, cfg.OutDir, filepath.Join("index.html"))

	// Local icon inlined, default attrs applied, user attr forwarded.
	if !strings.Contains(html, `<path d="M4 6h16M4 12h16M4 18h16"/>`) {
		t.Errorf("expected local menu icon inlined, got:\n%.800s", html)
	}
	if !strings.Contains(html, `className=big`) {
		t.Errorf("expected forwarded class attr on custom32, got:\n%.800s", html)
	}
	if !strings.Contains(html, `viewBox="0 0 32 32"`) {
		t.Errorf("expected local viewBox preserved, got:\n%.800s", html)
	}

	// User attr must override the default (dedupeAttrs keeps the last one).
	// icons 1+2 keep the default stroke-width=2; icon 3's override replaces it
	// rather than emitting a duplicate conflicting attribute.
	if got := strings.Count(html, `stroke-width=3`); got != 1 {
		t.Errorf("expected user stroke-width override once, got %d:\n%.800s", got, html)
	}
	if got := strings.Count(html, `stroke-width=2`); got != 2 {
		t.Errorf("expected default stroke-width on the two non-overriding icons, got %d:\n%.800s", got, html)
	}

	// Malicious SVG content must be stripped, not emitted raw.
	if strings.Contains(html, "onclick") || strings.Contains(html, "alert(1)") {
		t.Errorf("malicious event handler not sanitized:\n%.800s", html)
	}
	if strings.Contains(html, "<script>") {
		t.Errorf("malicious <script> not stripped:\n%.800s", html)
	}
}

// TestBuildLocalIconsFolderMissing verifies a missing local icon produces an
// escaped error span instead of raw file content and does not break the build.
func TestBuildLocalIconsFolderMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "pages"), 0755); err != nil {
		t.Fatal(err)
	}
	page := `export default function Page() { return <Icon name="not-registered" />; }`
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Resolve(root)
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html := readOut(t, cfg.OutDir, filepath.Join("index.html"))
	if !strings.Contains(html, `[local icon`) {
		t.Errorf("expected escaped error span for missing icon, got:\n%.800s", html)
	}
	if strings.Contains(html, `&lt;`) {
		t.Errorf("error span should not double-escape HTML, got:\n%.800s", html)
	}
}

// TestGenerateHTMLMinifiedNotDev excludes the live-reload script in production.
func TestGenerateHTMLMinifiedNotDev(t *testing.T) {
	html := generateHTML(
		"<div>body</div>", "<title>t</title>", "", "",
		false, "", "", "/", false,
	)
	if strings.Contains(html, "__krate/hotreload") {
		t.Errorf("production HTML should not include dev hot-reload script:\n%s", html)
	}
}

// TestBuildImageResponsive verifies <Image> compiles to a WebP-first <picture>
// with responsive srcsets, sizes, blur placeholder, and CLS-mitigating
// aspect-ratio, and that the processed variants are copied into the output.
func TestBuildImageResponsive(t *testing.T) {
	root := t.TempDir()

	writeFile := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	writeFile("src/pages/index.tsx", `export default function Page() {
  return (
    <>
      <div>
        <Image src="/hero.png" width={800} height={500} alt="Hero image" />
        <Image src="/eager.png" width={400} height={300} alt="Eager" loading="eager" priority />
        <Image src="/noplaceholder.png" width={200} height={200} alt="No blur" placeholder="empty" sizes="(max-width: 600px) 100vw, 300px" />
      </div>
    </>
  );
}`)

	// Opaque source image
	hero := image.NewRGBA(image.Rect(0, 0, 1200, 750))
	for y := 0; y < 750; y++ {
		for x := 0; x < 1200; x++ {
			hero.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
		}
	}
	writePNG(t, filepath.Join(root, "public", "hero.png"), hero)
	writePNG(t, filepath.Join(root, "public", "eager.png"), hero)
	writePNG(t, filepath.Join(root, "public", "noplaceholder.png"), hero)

	cfg := config.Default()
	cfg.Minify = false
	cfg.Resolve(root)
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	html := readOut(t, cfg.OutDir, filepath.Join("index.html"))

	// WebP-first <picture> with a fallback <source>.
	if !strings.Contains(html, `<picture>`) {
		t.Errorf("expected <picture>, got:\n%.400s", html)
	}
	if !strings.Contains(html, `<source type="image/webp"`) {
		t.Errorf("expected webp <source>, got:\n%.400s", html)
	}
	if !strings.Contains(html, `<source type="image/png"`) {
		t.Errorf("expected png fallback <source>, got:\n%.400s", html)
	}
	if !strings.Contains(html, `/_krate/images/`) {
		t.Errorf("expected /_krate/images/ URLs, got:\n%.400s", html)
	}

	// CLS mitigation: aspect-ratio + width/height attrs.
	if !strings.Contains(html, `aspect-ratio:800/500`) {
		t.Errorf("expected aspect-ratio:800/500, got:\n%.400s", html)
	}
	if !strings.Contains(html, `width="800"`) || !strings.Contains(html, `height="500"`) {
		t.Errorf("expected width/height attributes, got:\n%.400s", html)
	}

	// Blur placeholder background.
	if !strings.Contains(html, `background-image:url(data:image/jpeg;base64,`) {
		t.Errorf("expected LQIP background, got:\n%.400s", html)
	}
	// ... but only for the two images that request it (placeholder="empty" on the third).
	if got := strings.Count(html, "background-image"); got != 2 {
		t.Errorf("expected 2 LQIP backgrounds, got %d:\n%.600s", got, html)
	}

	// Loading behavior.
	if !strings.Contains(html, `loading="lazy"`) {
		t.Errorf("expected lazy loading by default, got:\n%.400s", html)
	}
	if !strings.Contains(html, `loading="eager"`) {
		t.Errorf("expected eager loading for priority image, got:\n%.400s", html)
	}
	if !strings.Contains(html, `fetchpriority="high"`) {
		t.Errorf("expected fetchpriority=high for priority image, got:\n%.400s", html)
	}
	if !strings.Contains(html, `decoding="async"`) {
		t.Errorf("expected decoding=async, got:\n%.400s", html)
	}

	// Custom sizes forwarded.
	if !strings.Contains(html, `(max-width: 600px) 100vw, 300px`) {
		t.Errorf("expected custom sizes attr, got:\n%.400s", html)
	}

	// Processed variants landed in the output and are valid WebP.
	imgDir := filepath.Join(cfg.OutDir, "_krate", "images")
	entries, err := os.ReadDir(imgDir)
	if err != nil {
		t.Fatalf("image output dir: %v", err)
	}
	var webpFound bool
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".webp") {
			webpFound = true
			data, err := os.ReadFile(filepath.Join(imgDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(data), "RIFF") || !strings.Contains(string(data), "WEBP") {
				t.Errorf("%s is not valid WebP", e.Name())
			}
		}
	}
	if !webpFound {
		t.Error("no .webp variants in image output dir")
	}
}

// TestBuildLinkOutput verifies <Link> compiles to an <a> wired for SPA
// navigation with Next.js-style prefetch/replace/scroll props, forwarded anchor
// attributes, and plain-anchor treatment for external links.
func TestBuildLinkOutput(t *testing.T) {
	root := t.TempDir()

	writeFile := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	writeFile("src/pages/index.tsx", `export default function Page() {
  return (
    <>
      <Link href="/about">About</Link>
      <Link href="/docs" prefetch={false}>NoPrefetch</Link>
      <Link href="/faq" replace>Replace</Link>
      <Link href="/blog" scroll={false}>NoScroll</Link>
      <Link href="https://example.com">External</Link>
      <Link href="/contact" className="btn" aria-label="Contact">Contact</Link>
      <Link href="/new" target="_blank">NewTab</Link>
      <Link href="#section">Anchor</Link>
    </>
  );
}`)

	cfg := config.Default()
	cfg.Minify = false
	cfg.Resolve(root)
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	html := readOut(t, cfg.OutDir, filepath.Join("index.html"))

	// Extract every anchor opening tag for per-link assertions.
	re := regexp.MustCompile(`<a [^>]*>`)
	anchors := re.FindAllString(html, -1)
	byHref := func(href string) string {
		for _, a := range anchors {
			if strings.Contains(a, `href="`+href+`"`) {
				return a
			}
		}
		return ""
	}

	// Local link: SPA-wired, prefetch enabled by default.
	about := byHref("/about")
	if !strings.Contains(about, "data-krate-link") || !strings.Contains(about, "data-prefetch") {
		t.Errorf("about link missing SPA attrs: %q", about)
	}

	// prefetch={false} disables prefetching but keeps SPA navigation.
	docs := byHref("/docs")
	if !strings.Contains(docs, "data-krate-link") {
		t.Errorf("docs link missing data-krate-link: %q", docs)
	}
	if strings.Contains(docs, "data-prefetch") {
		t.Errorf("docs link should not prefetch: %q", docs)
	}

	// replace → history.replaceState.
	if faq := byHref("/faq"); !strings.Contains(faq, "data-krate-replace") {
		t.Errorf("faq link missing data-krate-replace: %q", faq)
	}

	// scroll={false} → data-krate-scroll="false".
	if blog := byHref("/blog"); !strings.Contains(blog, `data-krate-scroll="false"`) {
		t.Errorf("blog link missing data-krate-scroll: %q", blog)
	}

	// External links are plain anchors with data-krate-external.
	ext := byHref("https://example.com")
	if !strings.Contains(ext, "data-krate-external") || strings.Contains(ext, "data-krate-link") {
		t.Errorf("external link should be plain anchor: %q", ext)
	}

	// Forwarded attributes.
	if contact := byHref("/contact"); !strings.Contains(contact, `class="btn"`) || !strings.Contains(contact, `aria-label="Contact"`) {
		t.Errorf("contact link missing forwarded attrs: %q", contact)
	}

	// target=_blank gets rel="noopener noreferrer".
	nt := byHref("/new")
	if !strings.Contains(nt, `target="_blank"`) || !strings.Contains(nt, `rel="noopener noreferrer"`) {
		t.Errorf("_blank link missing target/rel: %q", nt)
	}

	// Hash links are plain anchors (no SPA interception).
	if hash := byHref("#section"); strings.Contains(hash, "data-krate-link") {
		t.Errorf("hash link should not be SPA-wired: %q", hash)
	}

	// Children render inside the anchor.
	if !strings.Contains(html, `>About</a>`) || !strings.Contains(html, `>Contact</a>`) {
		t.Errorf("link children not rendered:\n%.400s", html)
	}
}

// TestRuntimeChunkIncludesReconcile guards against the SPA router losing its
// dependency: reconcileTrees lives in reconcile.ts, which the runtime bundle
// (packages/runtime/bundle.cjs) must include for navigation to work.
func TestRuntimeChunkIncludesReconcile(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", ".."))
	rt := loadRuntimeFromDisk(root)
	if rt == "" {
		t.Skip("runtime dist not available in this environment")
	}
	for _, want := range []string{
		"reconcileTrees",
		"reconcile",
		"conditional",
	} {
		if !strings.Contains(rt, want) {
			t.Errorf("runtime bundle missing %q — add reconcile.js to bundle.cjs", want)
		}
	}
}

// TestPageScriptSrc verifies page hydration scripts are emitted with absolute
// site paths so they resolve correctly on first load AND when the SPA router
// injects them during client-side navigation from another route.
func TestPageScriptSrc(t *testing.T) {
	tests := []struct {
		route  string
		jsFile string
		want   string
	}{
		{".", "index.a.js", "/index.a.js"},
		{"", "index.a.js", "/index.a.js"},
		{"about", "index.b.js", "/about/index.b.js"},
		{"/about", "index.b.js", "/about/index.b.js"},
		{"docs/getting-started", "index.c.js", "/docs/getting-started/index.c.js"},
	}
	for _, tt := range tests {
		if got := pageScriptSrc(tt.route, tt.jsFile); got != tt.want {
			t.Errorf("pageScriptSrc(%q, %q) = %q, want %q", tt.route, tt.jsFile, got, tt.want)
		}
	}
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
