package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const twLayerPage = `import "./site.css";
export default function P() {
  return (
    <main>
      <p>Don't panic</p>
      <div id="u" class="not-a-real-utility-zz mx-8 p-6 transition text-red-500 bg-brand-500 [&_p]:mt-2 data-[state=open]:p-4">x</div>
      <h1 id="h">Title</h1>
      <h2 id="h2">Sub</h2>
      <a id="a" href="/">link</a>
      <ul id="ul"><li>a</li></ul>
      <button id="b">btn</button>
      <i id="btnx" class="btn">z</i>
    </main>
  );
}`

const twLayerCSS = `@import "tailwindcss";
@theme { --color-brand-500: #ff0000; --animate-fadein: fadein 1s; @keyframes fadein{from{opacity:0}to{opacity:1}} }
@source inline("underline");
h1 { font-size: 40px }
a { color: rgb(255, 0, 0) }
ul { padding-left: 30px }
button { padding: 10px; background: green }
@layer base { h2 { font-size: 33px } }
.btn { @apply px-4 py-2; }
`

// buildTailwindProjectLogged builds the Tailwind fixture and returns its
// root, output, index page and the warnings of the build.
func buildTailwindProjectLogged(t *testing.T) (root, outDir, html, logged string) {
	t.Helper()
	root = t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	for p, c := range map[string]string{
		filepath.Join(pagesDir, "index.tsx"): twLayerPage,
		filepath.Join(pagesDir, "site.css"):  twLayerCSS,
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = true
	cfg.Tailwind.Preflight = true
	cfg.Tailwind.Strict = true
	logged, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, logged)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return root, cfg.OutDir, string(data), logged
}

func TestTailwindLayeringAndDirectivesBuild(t *testing.T) {
	t.Parallel()
	_, outDir, _, logged := buildTailwindProjectLogged(t)
	if !strings.Contains(logged, "Tailwind strict") || !strings.Contains(logged, "not-a-real-utility-zz") {
		t.Errorf("strict mode did not report unknown classes; stderr:\n%s", logged)
	}
	all := readAllCSS(t, outDir)
	for _, bad := range []string{"@theme", "@import \"tailwindcss\"", "@import 'tailwindcss'", "@apply", "@source"} {
		if strings.Contains(all, bad) {
			t.Errorf("directive %q shipped to the browser:\n%s", bad, all)
		}
	}
	for _, want := range []string{
		"@layer properties, theme, base, components, utilities;",
		"@layer properties{@supports",
		"--color-brand-500:#f00",
		"@layer base{", "@layer utilities{",
		".p-6", ".transition", ".underline", "padding-left",
	} {
		if !strings.Contains(strings.ReplaceAll(all, " ", ""), strings.ReplaceAll(want, " ", "")) {
			t.Errorf("stylesheet missing %q:\n%s", want, all)
		}
	}
	if !strings.Contains(all, ".bg-brand-500") && !strings.Contains(all, "bg-brand-500") {
		t.Errorf("bg-brand-500 from @theme missing:\n%s", all)
	}
	if !strings.Contains(all, "mt-2") || !strings.Contains(all, "data-[state=open]") && !strings.Contains(all, "data-\\[state") {
		t.Errorf("arbitrary variants missing:\n%s", all)
	}
}
