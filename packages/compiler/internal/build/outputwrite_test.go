package build

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// blockedOutDir returns an output directory that cannot be created because
// its parent is a regular file.
func blockedOutDir(t *testing.T) string {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(blocker, "dist")
}

func requireOutputWriteError(t *testing.T, err error, wantPath string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a write error, got nil")
	}
	var w *OutputWriteError
	if !errors.As(err, &w) {
		t.Fatalf("error is not an OutputWriteError: %v", err)
	}
	if !strings.Contains(err.Error(), wantPath) {
		t.Errorf("error does not name %s: %v", wantPath, err)
	}
}

func TestWriteOutputNamesPath(t *testing.T) {
	dir := blockedOutDir(t)
	path := filepath.Join(dir, "a.css")
	requireOutputWriteError(t, writeOutput(path, []byte("x")), path)
	if err := writeOutput(filepath.Join(t.TempDir(), "ok", "a.css"), []byte("x")); err != nil {
		t.Fatalf("a writable path must succeed: %v", err)
	}
}

func TestWriteGlobalCSSReportsFailure(t *testing.T) {
	cfg := config.Default()
	cfg.OutDir = blockedOutDir(t)
	b := New(t.TempDir(), cfg)
	file, err := b.writeGlobalCSS("body{color:red}")
	if err == nil {
		t.Fatalf("writeGlobalCSS swallowed a failed write (returned %q)", file)
	}
	requireOutputWriteError(t, err, cfg.OutDir)
}

func TestWritePageCSSReportsFailure(t *testing.T) {
	cfg := config.Default()
	cfg.OutDir = blockedOutDir(t)
	b := New(t.TempDir(), cfg)
	_, err := b.writePageCSS([]*PageResult{{Page: "a.tsx", OutName: ".", CSS: "p{color:red}"}})
	requireOutputWriteError(t, err, cfg.OutDir)
}

func TestWriteDevChunkReportsFailure(t *testing.T) {
	out := blockedOutDir(t)
	_, err := writeDevChunk(out, t.TempDir())
	requireOutputWriteError(t, err, out)
}

func TestWriteRuntimeChunkReportsFailure(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", ".."))
	out := blockedOutDir(t)
	rel, err := writeRuntimeChunk(out, true, repoRoot, runtimeChunkOptions{SPA: false})
	if rel == "" && err == nil {
		t.Skip("runtime dist not available")
	}
	requireOutputWriteError(t, err, out)
}

// A stylesheet that cannot be written must fail a rebuild instead of leaving
// HTML that links a file that does not exist.
func TestBuildPagesFailsWhenStylesheetCannotBeWritten(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.css", "p { color: rgb(1, 2, 3); }\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import './index.css';\nexport default function Page() { return <p>hi</p>; }\n")
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	page := filepath.Join(cfg.PagesDir, "index.tsx")

	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(cfg.OutDir, "styles.*.css"))
	if len(matches) != 1 {
		t.Fatalf("expected one stylesheet, got %v", matches)
	}
	// Replace the stylesheet with a non-empty directory so the rewrite fails.
	if err := os.Remove(matches[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(matches[0], "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := New(root, cfg).BuildPages([]string{page})
	if err == nil {
		t.Fatal("BuildPages succeeded although the stylesheet could not be written")
	}
	if !strings.Contains(err.Error(), filepath.Base(matches[0])) {
		t.Errorf("error does not name the stylesheet: %v", err)
	}
}
