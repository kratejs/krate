package bundler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

func bundleNamedImports(t *testing.T, files map[string]string) error {
	t.Helper()
	resolver.ResetCaches()
	t.Cleanup(resolver.ResetCaches)
	dir := t.TempDir()
	for rel, src := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	b := New(dir)
	b.SetPathAliases([]string{"@/*"}, [][]string{{"./*"}}, dir)
	_, err := b.Bundle(filepath.Join(dir, "page.tsx"))
	return err
}

const namedPage = "export default function P() { return <p>{String(x)}</p>; }\n"

func TestNamedImportMissing(t *testing.T) {
	err := bundleNamedImports(t, map[string]string{
		"data.ts":  "export const itemsList = [1];\nexport function loadRows() { return []; }\n",
		"page.tsx": "import { itemList as x } from './data';\n" + namedPage,
	})
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeNamedImportMissing {
		t.Fatalf("want a KR7128 diagnostic, got %v", err)
	}
	if d.Line != 1 || !strings.Contains(d.Message, `"itemList"`) || !strings.Contains(d.Hint, "did you mean itemsList") {
		t.Errorf("diagnostic lacks the position, name or suggestion: %+v", d)
	}
}

func TestNamedImportThroughAlias(t *testing.T) {
	err := bundleNamedImports(t, map[string]string{
		"lib/data.ts": "export const present = 1;\n",
		"page.tsx":    "import { missing as x } from '@/lib/data';\n" + namedPage,
	})
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeNamedImportMissing {
		t.Fatalf("want a KR7128 diagnostic, got %v", err)
	}
}

func TestNamedImportAccepted(t *testing.T) {
	cases := map[string]map[string]string{
		"declarations and lists": {
			"data.ts":  "export const a = 1, b = 2;\nexport function f() {}\nconst c = 3;\nexport { c, c as x };\nexport default 1;\n",
			"page.tsx": "import { a, b, f, c, x } from './data';\nimport d from './data';\n" + namedPage,
		},
		"types without the type keyword": {
			"data.ts":  "export type Props = { a: number };\nexport interface Shape { w: number }\nexport declare const ambient: number;\nexport const x = 1;\n",
			"page.tsx": "import { Props, Shape, ambient, x } from './data';\n" + namedPage,
		},
		"type-only imports of missing names": {
			"data.ts":  "export const x = 1;\n",
			"page.tsx": "import type { Gone } from './data';\nimport { type AlsoGone, x } from './data';\n" + namedPage,
		},
		"re-exports": {
			"inner.ts": "export const x = 1;\nexport const y = 2;\n",
			"data.ts":  "export * from './inner';\nexport { y as z } from './inner';\nexport type { T } from './inner';\n",
			"page.tsx": "import { x, y, z, T } from './data';\n" + namedPage,
		},
		"star re-export of a package is not enumerable": {
			"data.ts":  "export * from 'some-package';\n",
			"page.tsx": "import { anything as x } from './data';\n" + namedPage,
		},
		"commonjs": {
			"data.js":  "module.exports = { thing: 1 };\n",
			"page.tsx": "import { thing as x } from './data.js';\n" + namedPage,
		},
		"json": {
			"data.json": `{"a":1}`,
			"page.tsx":  "import { a as x } from './data.json';\n" + namedPage,
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			err := bundleNamedImports(t, files)
			var d diag.Diagnostic
			if errors.As(err, &d) && d.Code == diag.CodeNamedImportMissing {
				t.Fatalf("false positive: %v", err)
			}
		})
	}
}
