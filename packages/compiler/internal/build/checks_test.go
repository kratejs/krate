package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBuildChecksFailOnError verifies a `checks` config makes BuildAll fail
// when a page violates an error-severity rule.
func TestBuildChecksFailOnError(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/index.tsx", `export default function Page() {
  return <body><img src="/logo.png" /></body>;
}`)

	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	cfg.Checks = map[string]any{"a11y": "error"}
	b := New(root, cfg)
	err := b.BuildAll()
	if err == nil {
		t.Fatal("expected build to fail on a11y error (missing img alt)")
	}
	if !containsStr(err.Error(), "checks") {
		t.Errorf("error should mention checks, got: %v", err)
	}
}

// TestBuildChecksAbsentInactive verifies that without a `checks` key the build
// does not run quality gates (strictly opt-in).
func TestBuildChecksAbsentInactive(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/index.tsx", `export default function Page() {
  return <body><img src="/logo.png" /></body>;
}`)

	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("build without checks should succeed, got: %v", err)
	}
}

// TestBuildChecksWarningDoesNotFail verifies warning severity does not fail a
// build configured with failOn=error.
func TestBuildChecksWarningDoesNotFail(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/index.tsx", `export default function Page() {
  return <body><img src="/logo.png" /></body>;
}`)

	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	cfg.Checks = map[string]any{"a11y": "warning"}
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("warning-only checks should not fail the build, got: %v", err)
	}
}

// TestCheckSiteReadsBuiltOutput verifies CheckSite re-reads dist/ and reports
// findings against the emitted HTML.
func TestCheckSiteReadsBuiltOutput(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/pages/index.tsx", `export default function Page() {
  return <body><img src="/logo.png" /></body>;
}`)

	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	// No `checks` key: build should not run gates...
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	// ...but CheckSite(true) uses built-in defaults.
	findings, checkCfg, err := b.CheckSite(true)
	if err != nil {
		t.Fatalf("CheckSite: %v", err)
	}
	if !checkCfg.Active {
		t.Fatal("CheckSite should activate default rules")
	}
	found := false
	for _, f := range findings {
		if f.Rule == "a11y/img-alt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a11y/img-alt finding from emitted HTML, got %+v", findings)
	}
}

// TestCheckSiteAbsentManifest ensures a missing manifest does not error.
func TestCheckSiteAbsentManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Resolve(root)
	b := New(root, cfg)
	if _, _, err := b.CheckSite(true); err != nil {
		t.Fatalf("CheckSite with no output should not error: %v", err)
	}
}
