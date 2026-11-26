package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Classes written only in a top-level components/ folder that a page imports
// through a tsconfig alias reach the stylesheet with no scan config.
func TestTailwindDetectsImportedModulesOutsideSrc(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{
		"tsconfig.json":         `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["./*"]}}}`,
		"src/global.css":        `@import "tailwindcss";`,
		"src/pages/index.tsx":   "import Nav from \"@/components/nav\";\nexport default function P() { return <main class=\"flex\"><Nav /></main>; }",
		"components/nav.tsx":    "export default function Nav() { return <nav class=\"h-[1.3rem] uppercase tracking-widest md:grid-cols-3\">x</nav>; }",
		"components/unused.tsx": "export default function U() { return <i class=\"lowercase\">x</i>; }",
	}
	for name, c := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = true
	cfg.PathAliases = config.PathAliases{{Prefix: "@/*", Targets: []string{"./*"}}}
	cfg.TSBaseDir = root
	logged, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, logged)
	}
	all := readAllCSS(t, cfg.OutDir)
	for _, want := range []string{`.h-\[1\.3rem\]`, "uppercase", "tracking-widest", `md\:grid-cols-3`} {
		if !strings.Contains(all, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
}
