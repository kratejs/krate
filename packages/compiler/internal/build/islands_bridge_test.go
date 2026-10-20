package build

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// setupIslandProject writes a page that renders a compiled external package
// (fake-lib) and, when withPreact is set, copies the examples' Preact fixture
// into the project's node_modules.
func setupIslandProject(t *testing.T, root string, withPreact bool) *config.Config {
	t.Helper()
	if withPreact {
		preactSrc := filepath.Join("..", "..", "..", "..", "examples", "node_modules", "preact")
		if _, err := os.Stat(preactSrc); err != nil {
			t.Skipf("preact fixture not installed: %v", err)
		}
		if err := copyTree(t, preactSrc, filepath.Join(root, "node_modules", "preact")); err != nil {
			t.Fatalf("copying preact fixture: %v", err)
		}
	}

	libDir := filepath.Join(root, "node_modules", "fake-lib")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "package.json"), []byte(`{"name":"fake-lib","version":"1.0.0","main":"index.js"}`), 0644); err != nil {
		t.Fatal(err)
	}
	libSrc := `
		import { jsx } from 'react/jsx-runtime';
		export function Badge({ label }) {
			return jsx('span', { className: 'badge', children: label });
		}
	`
	if err := os.WriteFile(filepath.Join(libDir, "index.js"), []byte(libSrc), 0644); err != nil {
		t.Fatal(err)
	}

	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	page := `
		import { createSignal } from '@krate/runtime';
		import { Badge } from 'fake-lib';
		export default function Page() {
			const [label, setLabel] = createSignal('hello');
			return (
				<div>
					<Badge label={label()} onPing={setLabel} />
					<button onClick={() => setLabel('bye')}>x</button>
				</div>
			);
		}
	`
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	return cfg
}

// TestIslandSignalPropBridge verifies that a page-scope signal is evaluated in
// the page, published to the island boot values, and passed as a prop.
func TestIslandSignalPropBridge(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, "data-krate-island") {
		t.Fatalf("expected an island placeholder:\n%s", html)
	}
	if !strings.Contains(html, `type="importmap"`) {
		t.Fatalf("expected the shared Preact import map:\n%s", html)
	}

	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(jsFiles) == 0 {
		t.Fatalf("expected a hydration bundle: %v", err)
	}
	jsBytes, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsBytes), `"label":label()`) {
		t.Fatalf("island boot values should evaluate the page signal:\n%s", jsBytes)
	}
	if !strings.Contains(string(jsBytes), `"setLabel":setLabel`) {
		t.Fatalf("island boot values should bridge the page setter as a function prop:\n%s", jsBytes)
	}
	if !strings.Contains(string(jsBytes), "__krate_island_subscribe") {
		t.Fatalf("island boot should subscribe the island to page signal changes:\n%s", jsBytes)
	}

	chunks, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "island.*.js"))
	if err != nil || len(chunks) == 0 {
		t.Fatalf("expected an island chunk: %v", err)
	}
	chunk, err := os.ReadFile(chunks[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chunk), "badge") {
		t.Fatalf("island chunk does not contain the external component:\n%s", chunk)
	}
}

// TestIslandMissingPreactHint verifies the build fails with an actionable
// install hint when Preact is not in the project.
func TestIslandMissingPreactHint(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, false)
	err := New(root, cfg).BuildAll()
	if err == nil {
		t.Fatal("expected a missing-Preact build error")
	}
	if !strings.Contains(err.Error(), "pnpm add preact") {
		t.Fatalf("expected an install hint, got: %v", err)
	}
}

// copyTree copies src into dst, following symlinks (pnpm's node_modules uses
// them for package entries).
func copyTree(t *testing.T, src, dst string) error {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}
