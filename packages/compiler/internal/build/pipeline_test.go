package build

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
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
		requireE2E(t, "examples not found at %s", root)
	}
	return root
}

// buildTestProject builds the shared examples/ project once per test process
// and reuses the output. The project builds in place (examples/dist and
// examples/.krate), so no test may build it again. Many integration tests need the built site; rebuilding
// it for each is by far the dominant cost (and under -race it exceeded the Go
// test timeout). The examples sources are not mutated between tests, so a single
// build is sound.
var (
	e2eBuildOnce sync.Once
	e2eBuildDir  string
	e2eBuildCfg  *config.Config
	e2eBuildErr  error
	// e2eBuildResults and e2eBuildFeatures are the page results the runtime
	// chunk was derived from and the features it shipped.
	e2eBuildResults  []*PageResult
	e2eBuildFeatures []string
)

func buildTestProject(t *testing.T) string {
	t.Helper()
	// Resolve the path per call so requireE2E's skip logic still applies.
	root := testProjectPath(t)

	e2eBuildOnce.Do(func() {
		cfg, err := config.Load(root)
		if err != nil {
			e2eBuildErr = fmt.Errorf("config.Load: %w", err)
			return
		}
		b := New(root, cfg)
		// A page replayed from the disk cache has no recorded references
		// (TestRuntimeFeaturesCoverLegacyScanExamples reads them). The
		// examples build sourcemaps, which keeps the cache off already.
		b.diskCache.enabled = false
		captureRuntimeResults(b, &e2eBuildResults)
		if err := b.BuildAll(); err != nil {
			e2eBuildErr = fmt.Errorf("BuildAll: %w", err)
			return
		}
		e2eBuildDir = cfg.OutDir
		e2eBuildCfg = cfg
		e2eBuildFeatures = b.runtimeFeatures
	})
	if e2eBuildErr != nil {
		t.Fatal(e2eBuildErr)
	}
	return e2eBuildDir
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
	t.Parallel()
	outDir := buildTestProject(t)
	html := readOut(t, outDir, filepath.Join("about", "index.html"))
	if !strings.Contains(html, `<nav>`) {
		t.Errorf("expected layout nav in page, got:\n%.400s", html)
	}
	// JSX text entities are decoded (React semantics), so `&copy;` renders as .
	if !strings.Contains(html, "\u00a9 2026") {
		t.Errorf("expected layout footer in page, got:\n%.400s", html)
	}
	if !strings.Contains(html, `class=layout`) {
		t.Errorf("expected layout wrapper class, got:\n%.400s", html)
	}
}

// TestBuildMixedTierPage verifies the server-runtime-demo page demonstrates the
// server/runtime split: server components are baked with a real QuickJS
// Date.now() timestamp, runtime components are deferred region splice markers
// (the sidecar renders them at request time and Go splices the HTML in).
func TestBuildMixedTierPage(t *testing.T) {
	t.Parallel()
	outDir := buildTestProject(t)
	html := readOut(t, outDir, filepath.Join("server-runtime-demo", "index.html"))

	// Server components are evaluated by QuickJS at build time -> a real
	// millisecond epoch timestamp is baked into the static HTML.
	tsRe := regexp.MustCompile(`Compiled at build time: (\d{13})`)
	m := tsRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("expected a 13-digit QuickJS Date.now() timestamp baked at build, got:\n%.600s", html)
	}

	// Runtime components are NOT baked - each becomes a region splice marker
	// (<!--region:region-<slotID>--><!--/region:region-<slotID>-->) that the
	// sidecar fills at request time.
	if !strings.Contains(html, "<!--region:") {
		t.Errorf("expected runtime component region markers, got:\n%.600s", html)
	}
	if strings.Contains(html, "krate-id") {
		t.Errorf("krate-id placeholders should be gone, got:\n%.600s", html)
	}
	if strings.Contains(html, "application/krate-runtime") {
		t.Errorf("inline runtime props script should be gone, got:\n%.600s", html)
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
