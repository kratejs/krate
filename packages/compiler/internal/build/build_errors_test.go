package build

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A dev rebuild builds only the affected pages. When that one page fails, the
// error must say the page failed, not that pagesDir has no pages.
func TestBuildPagesAllFailedIsNotAPagesDirError(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `export default function Page() { return <p>ok</p>; }`)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	page := filepath.Join(cfg.PagesDir, "index.tsx")
	writeFileRel(t, root, "src/pages/index.tsx", `export default function Page() { return <div><p>hi</div>; }`)

	_, err := b.BuildPages([]string{page})
	if err == nil {
		t.Fatal("expected the broken page to fail the rebuild")
	}
	if got := err.Error(); got != "build failed: 1 error(s)" {
		t.Errorf("error = %q, want the failure count", got)
	}
	if len(DiagnosticsFromError(err)) == 0 {
		t.Error("the page diagnostics must stay reachable")
	}

}

func TestBuildErrorSummary(t *testing.T) {
	multi := &multiError{
		summary: "build failed: 2 error(s):\n  a.tsx: broken\n  b.tsx: broken",
		errs:    []error{errors.New("a"), errors.New("b")},
	}
	want := "build failed: 2 error(s) (details above)"
	if got := BuildErrorSummary(multi); got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	if got := BuildErrorSummary(fmt.Errorf("wrapped: %w", multi)); got != want {
		t.Errorf("wrapped summary = %q, want %q", got, want)
	}
	if got := BuildErrorSummary(errors.New("creating output dir: denied")); got != "creating output dir: denied" {
		t.Errorf("plain error changed: %q", got)
	}
	if got := BuildErrorSummary(nil); got != "" {
		t.Errorf("nil error = %q", got)
	}
}
