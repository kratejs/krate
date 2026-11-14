package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func writeModules(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func topNames(prog *ast.Program) map[string]bool {
	out := map[string]bool{}
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Declaration != nil {
			stmt = exp.Declaration
		}
		for _, n := range declaredNames(stmt) {
			out[n] = true
		}
	}
	return out
}

func moduleNamed(t *testing.T, bundle *Bundle, base string) *Module {
	t.Helper()
	for _, m := range bundle.Modules {
		if filepath.Base(m.Path) == base {
			return m
		}
	}
	t.Fatalf("no module %s", base)
	return nil
}

func TestPrivateNamesAreIsolated(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.tsx":    "function up() { return 'a'; }\nconst K = 1;\nexport function A() { return <p>{up()}{K}</p>; }\n",
		"b.tsx":    "function up() { return 'b'; }\nconst K = 2;\nexport function B() { return <p>{up()}{K}</p>; }\n",
		"page.tsx": "import { A } from './a';\nimport { B } from './b';\nexport default function P() { return <main><A /><B /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := topNames(moduleNamed(t, bundle, "a.tsx").Program), topNames(moduleNamed(t, bundle, "b.tsx").Program)
	if len(a) != 3 || len(b) != 3 {
		t.Fatalf("a %v, b %v", a, b)
	}
	for name := range a {
		if b[name] {
			t.Errorf("%s is shared", name)
		}
	}
	if !a["A"] || !b["B"] {
		t.Errorf("an export was renamed: %v %v", a, b)
	}
}

func TestExportCollisionWarnsOncePerBuild(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"node_modules/zod/package.json": `{"name":"zod","main":"index.js"}`,
		"node_modules/zod/schemas.js":   "export function mergeValues() { return 1; }\n",
		"node_modules/zod/util.js":      "import { mergeValues } from './schemas.js';\nexport function run() { return mergeValues(); }\n",
		"node_modules/zod/index.js":     "export { mergeValues } from './schemas.js';\nexport { run } from './util.js';\n",
		"values.ts":                     "export function mergeValues() { return 2; }\n",
		"consumer.ts":                   "import { mergeValues } from './values';\nexport const c = mergeValues();\n",
		"barrel.ts":                     "export { mergeValues } from './values';\nexport { c } from './consumer';\n",
		"a.ts":                          "export function dup() { return 'A'; }\n",
		"b.ts":                          "export function dup() { return 'B'; }\n",
		"ca.ts":                         "import { dup } from './a';\nexport const ra = dup();\n",
		"cb.ts":                         "import { dup } from './b';\nexport const rb = dup();\n",
		"ab.ts":                         "export * as A from './a';\nexport * as B from './b';\nexport { ra } from './ca';\nexport { rb } from './cb';\n",
		"pkg.tsx":                       "import { run } from 'zod';\nimport { c } from './barrel';\nexport default function P() { return <p>{run()}{c}</p>; }\n",
		"p1.tsx":                        "import { ra, rb } from './ab';\nexport default function P() { return <p>{ra}{rb}</p>; }\n",
		"p2.tsx":                        "import { ra, rb } from './ab';\nexport default function P() { return <b>{ra}{rb}</b>; }\n",
	}
	for name, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cache := NewModuleCache()
	warnings := func(page string) []string {
		b := New(dir)
		b.SetModuleCache(cache)
		bundle, err := b.Bundle(filepath.Join(dir, page))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range bundle.Modules {
			for _, d := range m.Diagnostics {
				if d.Code == diag.CodeExportCollision {
					out = append(out, d.Message)
				}
			}
		}
		return out
	}
	if w := warnings("pkg.tsx"); len(w) != 0 {
		t.Errorf("a package export is bundled apart from the project's, got %v", w)
	}
	w := warnings("p1.tsx")
	if len(w) != 1 || !strings.Contains(w[0], "ca.ts, cb.ts") {
		t.Errorf("want one warning naming the importers, got %v", w)
	}
	if w := warnings("p2.tsx"); len(w) != 0 {
		t.Errorf("the same clash must not warn again on the next page, got %v", w)
	}
}
