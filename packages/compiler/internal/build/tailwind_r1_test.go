package build

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
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

func TestTailwindScansContainerAndAlphaVar(t *testing.T) {
	_, out, logged := buildFiles(t, map[string]string{
		"src/pages/index.tsx": `export default function P() { return <div class="@container/main text-red-500/(--alpha) @container"><i class="@md:p-2">x</i></div>; }`,
	}, nil)
	all := readAllCSS(t, out)
	for _, want := range []string{`\@container\/main`, `.\@container{`, `text-red-500\/\(--alpha\)`} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %s in:\n%s", want, all)
		}
	}
	if strings.Contains(all, `text-red-500\/{`) {
		t.Errorf("truncated candidate rule:\n%s", all)
	}
	if strings.Contains(logged, "matched no utility") {
		t.Errorf("unexpected strict warning:\n%s", logged)
	}
}

func countStr(s, sub string) int { return strings.Count(s, sub) }

func TestTailwindApplyKeyframesNotDuplicated(t *testing.T) {
	// Used in markup and via @apply: one @keyframes in total.
	_, out, _ := buildFiles(t, map[string]string{
		"src/pages/index.tsx": `import "./site.css";
export default function P() { return <div class="animate-spin box">x</div>; }`,
		"src/pages/site.css": `@import "tailwindcss"; .box{ @apply animate-spin; }`,
	}, nil)
	all := readAllCSS(t, out)
	if n := countStr(all, "@keyframes spin"); n != 1 {
		t.Errorf("want 1 @keyframes spin, got %d:\n%s", n, all)
	}
	// Used only via @apply: still defined once.
	_, out, _ = buildFiles(t, map[string]string{
		"src/pages/index.tsx": `import "./site.css";
export default function P() { return <div class="box">x</div>; }`,
		"src/pages/site.css": `@import "tailwindcss"; .box{ @apply animate-ping; }`,
	}, nil)
	all = readAllCSS(t, out)
	if n := countStr(all, "@keyframes ping"); n != 1 {
		t.Errorf("want 1 @keyframes ping, got %d:\n%s", n, all)
	}
	if !strings.Contains(all, "animation") {
		t.Errorf("@apply animation missing:\n%s", all)
	}
}

// linkedCSSAll returns the concatenated stylesheets the built page links.
func linkedCSSAll(t *testing.T, outDir, page string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, page))
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, m := range regexp.MustCompile(`href="?/?([^"\s>]+\.css)`).FindAllStringSubmatch(string(data), -1) {
		b, err := os.ReadFile(filepath.Join(outDir, m[1]))
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b)
	}
	return sb.String()
}

func TestTailwindThemeRemovalOnRebuild(t *testing.T) {
	root := t.TempDir()
	write := func(rel, c string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	page := filepath.Join(root, "src/pages/index.tsx")
	write("src/pages/index.tsx", `import "./site.css";
export default function P() { return <div class="bg-brand-500 p-2">x</div>; }`)
	write("src/pages/site.css", `@import "tailwindcss"; @theme { --color-brand-500: #ff0000; }`)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = true
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if got := linkedCSSAll(t, cfg.OutDir, "index.html"); !strings.Contains(got, ".bg-brand-500") {
		t.Fatalf("initial build lacks bg-brand-500:\n%s", got)
	}
	write("src/pages/site.css", `@import "tailwindcss";`)
	if _, err := b.BuildPages([]string{page}); err != nil {
		t.Fatal(err)
	}
	got := linkedCSSAll(t, cfg.OutDir, "index.html")
	if strings.Contains(got, ".bg-brand-500") || strings.Contains(got, "--color-brand-500") {
		t.Errorf("removed @theme token still applies after rebuild:\n%s", got)
	}
	if !strings.Contains(got, ".p-2") {
		t.Errorf("p-2 missing after rebuild:\n%s", got)
	}
}
