package css

import (
	"path/filepath"
	"strings"
	"testing"
)

func detectCSS(t *testing.T, root string, opts TailwindOptions) string {
	t.Helper()
	d := ScanTailwindDirectives(root, opts)
	res, err := GenerateTailwindDirectives(root, LoadTailwindConfigWithOptions(root, opts), opts, d)
	if err != nil {
		t.Fatal(err)
	}
	return res.CSS
}

func TestTailwindSourceFilesOutsideSrc(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/g.css"), `@import "tailwindcss";`)
	writeFile(t, filepath.Join(root, "src/p.tsx"), `<div class="flex">x</div>`)
	comp := filepath.Join(root, "components/nav.tsx")
	writeFile(t, comp, `<nav class="h-[1.3rem] uppercase md:grid-cols-3">x</nav>`)

	if out := detectCSS(t, root, TailwindOptions{}); strings.Contains(out, "uppercase") {
		t.Fatalf("components/ is not a source without the module list:\n%s", out)
	}
	out := detectCSS(t, root, TailwindOptions{SourceFiles: []string{comp, filepath.Join(root, "components/logo.png")}})
	for _, want := range []string{"uppercase", `.h-\[1\.3rem\]`, `md\:grid-cols-3`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// Explicit scanDirs keeps its meaning: module files are not added.
	out = detectCSS(t, root, TailwindOptions{ScanDirs: []string{"src"}, SourceFiles: []string{comp}})
	if strings.Contains(out, "uppercase") {
		t.Errorf("scanDirs must not be widened:\n%s", out)
	}
}

func TestTailwindSourceDirectiveAddsToDefault(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/g.css"), "@import \"tailwindcss\";\n@source \"../components\";\n")
	writeFile(t, filepath.Join(root, "src/p.tsx"), `<div class="flex">x</div>`)
	writeFile(t, filepath.Join(root, "components/a.tsx"), `<i class="uppercase">x</i>`)
	out := detectCSS(t, root, TailwindOptions{})
	for _, want := range []string{"uppercase", "display: flex"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestTailwindSourceGlobs(t *testing.T) {
	for name, src := range map[string]string{
		"recursive": `@source "../components/**/*.tsx";`,
		"braces":    `@source "../components/*.{ts,tsx}";`,
		"import":    `@import "tailwindcss" source("../components/**/*.tsx");`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			css := "@import \"tailwindcss\";\n" + src + "\n"
			if name == "import" {
				css = src + "\n"
			}
			writeFile(t, filepath.Join(root, "src/g.css"), css)
			writeFile(t, filepath.Join(root, "components/deep/a.tsx"), `<i class="tracking-widest">x</i>`)
			writeFile(t, filepath.Join(root, "components/b.tsx"), `<i class="uppercase">x</i>`)
			out := detectCSS(t, root, TailwindOptions{})
			if !strings.Contains(out, "uppercase") {
				t.Errorf("glob source not scanned:\n%s", out)
			}
		})
	}
}

func TestTailwindSourceNoneDisablesDetection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src/g.css"), `@import "tailwindcss" source(none);`)
	writeFile(t, filepath.Join(root, "src/p.tsx"), `<div class="uppercase">x</div>`)
	comp := filepath.Join(root, "components/a.tsx")
	writeFile(t, comp, `<i class="tracking-widest">x</i>`)
	out := detectCSS(t, root, TailwindOptions{SourceFiles: []string{comp}})
	if strings.Contains(out, "uppercase") || strings.Contains(out, "tracking-widest") {
		t.Errorf("source(none) must disable auto detection:\n%s", out)
	}
}
