package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A workspace package consumed as .ts source through a symlink outside the
// project root must never be parsed as JSX: `c.default < c.min` is a
// comparison, not a tag.
func TestWorkspaceTSFileIsNotParsedAsJSX(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "app")
	pkg := filepath.Join(base, "packages", "builder-core")
	pagesDir := filepath.Join(root, "src", "pages")
	for _, d := range []string{pagesDir, filepath.Join(pkg, "src"), filepath.Join(root, "node_modules", "@acme")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(pkg, "package.json"), `{"name":"@acme/builder-core","version":"1.0.0","main":"src/problems.ts"}`)
	write(filepath.Join(pkg, "src", "problems.ts"), `export interface Control { default: number; min: number; max: number }
export function badDefault(c: Control): boolean {
  return c.default < c.min || c.default > c.max;
}
`)
	if err := os.Symlink(pkg, filepath.Join(root, "node_modules", "@acme", "builder-core")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	write(filepath.Join(pagesDir, "p", "[id].tsx"), `import { badDefault } from "@acme/builder-core";
export default function Page({ params }: { params: { id: string } }) {
  const bad = badDefault({ default: 5, min: 1, max: 3 });
  return <div>{params.id}:{String(bad)}</div>;
}
export function generateStaticParams() {
  return [{ id: "a" }];
}
`)

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(root, "dist", "p", "a", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "a:true") {
		t.Fatalf("unexpected output: %s", out)
	}
}
