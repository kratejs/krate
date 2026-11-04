package build

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildFiles writes files (paths relative to root) and runs BuildAll with
// Tailwind enabled, returning the root, the out dir and captured stderr.
func buildFiles(t *testing.T, files map[string]string, tweak func(*config.Config)) (root, outDir, stderr string) {
	t.Helper()
	root = t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = true
	cfg.Tailwind.Strict = true
	if tweak != nil {
		tweak(cfg)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	berr := New(root, cfg).BuildAll()
	os.Stderr = orig
	_ = w.Close()
	logged, _ := io.ReadAll(r)
	if berr != nil {
		t.Fatalf("BuildAll: %v\n%s", berr, logged)
	}
	return root, cfg.OutDir, string(logged)
}

const r1Page = `import "./site.css";
export default function P() { return <main class="flex">x</main>; }`

func TestTailwindImportNoSpuriousWarning(t *testing.T) {
	_, _, logged := buildFiles(t, map[string]string{
		"src/pages/index.tsx": r1Page,
		"src/pages/site.css":  `@import "tailwindcss"; h1{color:red}`,
	}, nil)
	if strings.Contains(logged, "not found") {
		t.Errorf("spurious import warning:\n%s", logged)
	}
}

func TestTailwindImportWarnsWhenDisabled(t *testing.T) {
	_, _, logged := buildFiles(t, map[string]string{
		"src/pages/index.tsx": r1Page,
		"src/pages/site.css":  `@import "tailwindcss"; h1{color:red}`,
	}, func(c *config.Config) { c.Tailwind.Enabled = false })
	if !strings.Contains(logged, "not enabled") {
		t.Errorf("expected a not-enabled warning:\n%s", logged)
	}
}

func TestTailwindSourceInRootNoWarningAndInline(t *testing.T) {
	_, out, logged := buildFiles(t, map[string]string{
		"src/pages/index.tsx":      r1Page,
		"src/pages/site.css":       `@import "tailwindcss"; @source "../../extra4"; @source inline("mt-77"); @import "./deep/inner.css";`,
		"src/pages/deep/inner.css": `@source "../../../extra5";`,
		"extra5/y.html":            `<p class="pt-7">`,
		"extra4/x.html":            `<p class="pb-9">`,
	}, nil)
	if strings.Contains(logged, "outside the project root") {
		t.Errorf("spurious source warning:\n%s", logged)
	}
	if strings.Contains(logged, "mt-77") {
		t.Errorf("mt-77 reported unknown:\n%s", logged)
	}
	all := readAllCSS(t, out)
	for _, want := range []string{".pb-9", ".pt-7", ".mt-77"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %s in:\n%s", want, all)
		}
	}
}
