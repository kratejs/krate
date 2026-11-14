package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Two modules that a barrel re-exports both export "dup". An importer through a
// barrel and a direct importer each get their own.
func TestStuckExportsKeepTheirOwnBinding(t *testing.T) {
	html, logged := buildPlain(t, map[string]string{
		"src/lib/a.ts":        "export function dup() { return 'AAA'; }\n",
		"src/lib/b.ts":        "export function dup() { return 'BBB'; }\n",
		"src/lib/ca.tsx":      "import { dup } from './ba';\nexport function Ca() { return <i>{dup()}</i>; }\n",
		"src/lib/cb.tsx":      "import { dup } from './b';\nexport function Cb() { return <u>{dup()}</u>; }\n",
		"src/lib/ba.ts":       "export { dup } from './a';\n",
		"src/lib/index.ts":    "export { dup as dupA } from './a';\nexport { dup as dupB } from './b';\nexport { Ca } from './ca';\nexport { Cb } from './cb';\n",
		"src/pages/index.tsx": "import { Ca, Cb } from '../lib';\nexport default function P() { return <main><Ca /><Cb /></main>; }\n",
	})
	if !strings.Contains(html, "<i>AAA</i>") || !strings.Contains(html, "<u>BBB</u>") {
		t.Errorf("each module must keep its own dup:\n%s\nlog:\n%s", html, logged)
	}
	if strings.Contains(logged, "KR7116") {
		t.Errorf("the clash is resolved, no warning expected:\n%s", logged)
	}
}

// A clash that cannot be renamed is reported once per build, not once per page
// that bundles it (static pages, a generateStaticParams page and the rest).
func TestExportCollisionWarnsOncePerBuild(t *testing.T) {
	page := func(name string) string {
		return "import { ra, rb } from '../lib/ab';\nexport default function " + name + "() { return <p>{ra}{rb}</p>; }\n"
	}
	_, _, logged := buildFiles(t, map[string]string{
		"src/lib/a.ts":        "export function dup() { return 'A'; }\n",
		"src/lib/b.ts":        "export function dup() { return 'B'; }\n",
		"src/lib/ca.ts":       "import { dup } from './a';\nexport const ra = dup();\n",
		"src/lib/cb.ts":       "import { dup } from './b';\nexport const rb = dup();\n",
		"src/lib/cc.ts":       "import { dup } from './a';\nexport const rc = dup();\n",
		"src/lib/ab.ts":       "export * as A from './a';\nexport * as B from './b';\nexport { ra } from './ca';\nexport { rb } from './cb';\n",
		"src/pages/index.tsx": page("Home"),
		"src/pages/about.tsx": "import { ra, rb } from '../lib/ab';\nimport { rc } from '../lib/cc';\nexport default function About() { return <p>{ra}{rb}{rc}</p>; }\n",
		"src/pages/posts/[slug].tsx": "import { ra, rb } from '../../lib/ab';\n" +
			"export function generateStaticParams() { return [{ slug: 'x' }, { slug: 'y' }, { slug: 'z' }]; }\n" +
			"export default function Post({ params }: { params: { slug: string } }) { return <p>{params.slug}{rb}{ra}</p>; }\n",
	}, func(c *config.Config) {
		c.Tailwind.Enabled = false
		c.Tailwind.Strict = false
	})
	if n := strings.Count(logged, "KR7116"); n != 1 {
		t.Errorf("KR7116 reported %d times, want 1:\n%s", n, logged)
	}
}
