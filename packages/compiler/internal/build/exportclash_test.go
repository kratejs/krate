package build

import (
	"strings"
	"testing"
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
