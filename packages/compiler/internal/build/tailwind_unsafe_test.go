package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const twUnsafePage = `import "./site.css";
export default function P() {
  return <div class="bg-[#d81f26]/[.16] px-3 grid-cols-2">x</div>;
}`

// The scanner already skips candidates with unbalanced brackets, so the bad
// class arrives through @source inline(), which bypasses it.
const twUnsafeCSS = "@import \"tailwindcss\";\n@source inline(\"w-[a)]\");\n"

// A class whose generated value would break the stylesheet is dropped with a
// warning that names it, and the rules around it survive.
func TestTailwindDropsUnsafeClassWithWarning(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]string{"index.tsx": twUnsafePage, "site.css": twUnsafeCSS} {
		if err := os.WriteFile(filepath.Join(pagesDir, name), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = true
	logged, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, logged)
	}
	if !strings.Contains(logged, "dropped class w-[a)]") {
		t.Errorf("no warning naming the dropped class:\n%s", logged)
	}
	all := readAllCSS(t, cfg.OutDir)
	for _, bad := range []string{"a)", "]/["} {
		if strings.Contains(all, bad) {
			t.Errorf("stylesheet contains %q", bad)
		}
	}
	for _, want := range []string{"rgb(216 31 38 / .16)", ".px-3", "grid-template-columns"} {
		if !strings.Contains(all, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
}
