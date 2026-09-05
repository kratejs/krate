package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"krate-compiler/internal/config"
)

// TestBuildPageWorker verifies `new Worker('./worker.ts')` in a page: the
// worker target is resolved, the page's hydration JS references the hashed
// /workers/… URL, and a real esbuild-bundled worker file is emitted.
func TestBuildPageWorker(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	workersDir := filepath.Join(root, "src", "workers")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workersDir, 0755); err != nil {
		t.Fatal(err)
	}
	workerSrc := `
		import { shared } from './helper.js';
		self.onmessage = (e) => { postMessage(shared + ':' + e.data); };
	`
	if err := os.WriteFile(filepath.Join(workersDir, "worker.ts"), []byte(workerSrc), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workersDir, "helper.js"), []byte("export const shared = 'ok';"), 0644); err != nil {
		t.Fatal(err)
	}
	page := `
		export default function WorkerPage() {
			const w = new Worker(new URL('../workers/worker.ts', import.meta.url), { type: 'module' });
			return <div id="root">{w ? 'spawned' : 'none'}</div>;
		}
	`
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	// The esbuild-bundled worker file must exist in <out>/workers/.
	matches, err := filepath.Glob(filepath.Join(cfg.OutDir, "workers", "worker-*.js"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one emitted worker, got %v (err %v)", matches, err)
	}
	url := "/workers/" + filepath.Base(matches[0])

	workerJS, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workerJS), "shared") {
		t.Fatalf("worker bundle missing bundled import:\n%.800s", workerJS)
	}

	// Hydration JS must reference the hashed URL, not the source path.
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(jsFiles) != 1 {
		t.Fatalf("expected one hydration bundle, got %v (err %v)", jsFiles, err)
	}
	js, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), url) {
		t.Fatalf("hydration JS missing worker URL %q:\n%.1200s", url, js)
	}
	if strings.Contains(string(js), "worker.ts") {
		t.Fatalf("hydration JS still references worker source:\n%.1200s", js)
	}

	// workers.json index should list the source → URL mapping.
	if idx, err := os.ReadFile(filepath.Join(cfg.OutDir, "workers.json")); err == nil {
		if !strings.Contains(string(idx), url) {
			t.Fatalf("workers.json missing worker URL:\n%.800s", idx)
		}
	}
}