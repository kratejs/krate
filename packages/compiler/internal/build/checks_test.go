package build

import (
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
