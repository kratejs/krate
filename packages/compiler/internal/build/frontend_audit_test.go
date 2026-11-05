package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildPlain builds files without Tailwind and returns the index page HTML and
// the captured build log.
func buildPlain(t *testing.T, files map[string]string) (html, logged string) {
	t.Helper()
	_, out, logged := buildFiles(t, files, func(c *config.Config) {
		c.Tailwind.Enabled = false
		c.Tailwind.Strict = false
	})
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b), logged
}

func TestNamedReexportBarrelRendersComponents(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/components/Card.tsx": "export function Card() { return <b>CARD</b>; }\nexport default function D() { return <i>DEF</i>; }\n",
		"src/components/index.ts": "export { Card } from './Card';\nexport { default as Dflt } from './Card';\n",
		"src/components/deep.ts":  "export { Card as Deep } from './index';\n",
		"src/pages/index.tsx":     "import { Card, Dflt } from '../components';\nimport { Deep } from '../components/deep';\nexport default function Home() { return <main><Card /><Dflt /><Deep /></main>; }\n",
	})
	if !strings.Contains(html, "<b>CARD</b><i>DEF</i><b>CARD</b>") {
		t.Errorf("barrel components missing:\n%s\nlog:\n%s", html, logged)
	}
	if strings.Contains(logged, "could not be resolved") {
		t.Errorf("unexpected unresolved warning:\n%s", logged)
	}
}

func TestComponentFileBesideSameNamedDirectory(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/components/Button.tsx":       "export default function Button() { return <b>BTN</b>; }\n",
		"src/components/Button/style.css": "b { color: red; }\n",
		"src/pages/index.tsx":             "import Button from '../components/Button';\nexport default function Home() { return <main><Button /></main>; }\n",
	})
	if !strings.Contains(html, "<b>BTN</b>") || strings.Contains(logged, "could not be resolved") {
		t.Errorf("Button missing:\n%s\nlog:\n%s", html, logged)
	}
}

func TestReservedNameExportsRenderAndHydrate(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/lib/theme.ts": "export const krateTheme = { name: 'dark' };\nexport function h() { return 'H'; }\n",
		"src/pages/index.tsx": "import { createSignal } from '@krate/runtime';\nimport { krateTheme, h } from '../lib/theme';\n" +
			"export default function Home() { const [n, setN] = createSignal(0); return <main><p>{krateTheme.name}</p><p>{h()}</p><button onClick={() => setN(n() + 1)}>{n()}{krateTheme.name}</button></main>; }\n",
	})
	if !strings.Contains(html, "<p>dark</p><p>H</p>") {
		t.Errorf("exports with reserved names did not render:\n%s", html)
	}
	if strings.Contains(logged, "KR8302") {
		t.Errorf("unexpected KR8302:\n%s", logged)
	}
}

func buildErrText(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
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
	cfg.Tailwind.Enabled = false
	if err := New(root, cfg).BuildAll(); err != nil {
		return err.Error()
	}
	return ""
}

func TestScssImportIsABuildError(t *testing.T) {
	got := buildErrText(t, map[string]string{
		"src/pages/index.tsx": "import './a.scss';\nexport default function P() { return <p>x</p>; }\n",
		"src/pages/a.scss":    "a { b { color: red } }\n",
	})
	if !strings.Contains(got, "KR7120") || !strings.Contains(got, "a.scss") || !strings.Contains(got, "Sass") {
		t.Errorf("build error = %q", got)
	}
}
