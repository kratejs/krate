package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBuildIsDeterministic verifies two independent builds of the same sources
// produce byte-identical output (stable content hashes, no map-order leakage).
func TestBuildIsDeterministic(t *testing.T) {
	const page = `
import { createSignal } from '@krate/runtime';
export default function Page() {
  const [n, setN] = createSignal(0);
  return <div class="p-4 flex"><button onClick={() => setN(n() + 1)}>{n()}</button></div>;
}
`
	build := func() map[string][]byte {
		root := t.TempDir()
		writeFileRel(t, root, "src/pages/index.tsx", page)
		writeFileRel(t, root, "src/pages/about.tsx", `export default function About() { return <h1>About</h1>; }`)
		cfg := config.Default()
		cfg.PagesDir = filepath.Join(root, "src", "pages")
		cfg.OutDir = filepath.Join(root, "dist")
		cfg.Minify = false
		if err := New(root, cfg).BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		files := map[string][]byte{}
		err := filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(cfg.OutDir, p)
			data, _ := os.ReadFile(p)
			files[filepath.ToSlash(rel)] = data
			return nil
		})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		return files
	}

	a := build()
	b := build()

	if len(a) != len(b) {
		t.Fatalf("file count differs: %d vs %d", len(a), len(b))
	}
	for name, dataA := range a {
		dataB, ok := b[name]
		if !ok {
			t.Errorf("file %s missing from second build", name)
			continue
		}
		if string(dataA) != string(dataB) {
			t.Errorf("file %s differs between builds:\n--- A ---\n%s\n--- B ---\n%s", name, dataA, dataB)
		}
	}
}
