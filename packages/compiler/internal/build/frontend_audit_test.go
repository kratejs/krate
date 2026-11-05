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
