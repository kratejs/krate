package bundler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
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
