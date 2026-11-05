package bundler

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestPageNamesWinOverImportedOnes(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.tsx":    "function Row() { return <li />; }\nexport function A() { return <Row />; }\n",
		"page.tsx": "import { A } from './a';\nfunction Row() { return <b />; }\nexport default function P() { return <Row />; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !topNames(moduleNamed(t, bundle, "page.tsx").Program)["Row"] {
		t.Error("the page lost its name")
	}
	mod := moduleNamed(t, bundle, "a.tsx")
	if topNames(mod.Program)["Row"] {
		t.Fatal("imported module kept the name")
	}
	var tag string
	visitStmts(mod.Program.Body, false, func(n any, _ bool) bool {
		if el, ok := n.(*ast.JSXElement); ok {
			tag = el.Opening.Name
		}
		return true
	})
	if OriginalName(tag) != "Row" || tag == "Row" {
		t.Errorf("JSX tag = %q", tag)
	}
}

func TestDefaultImportFollowsTheRename(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.tsx":    "export default function Card() { return <p />; }\n",
		"b.tsx":    "export default function Card() { return <b />; }\n",
		"x.tsx":    "import Card from './a';\nexport function X() { return <Card />; }\n",
		"y.tsx":    "import Card from './b';\nexport function Y() { return <Card />; }\n",
		"page.tsx": "import { X } from './x';\nimport { Y } from './y';\nexport default function P() { return <main><X /><Y /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	importOf := func(mod *Module) string {
		for _, stmt := range mod.Program.Body {
			if imp, ok := stmt.(*ast.ImportStmt); ok {
				return imp.Default
			}
		}
		return ""
	}
	x, y := importOf(moduleNamed(t, bundle, "x.tsx")), importOf(moduleNamed(t, bundle, "y.tsx"))
	if x == y || OriginalName(x) != "Card" || OriginalName(y) != "Card" {
		t.Errorf("imports: %q, %q", x, y)
	}
}

// One module with a default export is not a collision, whatever importers call it.
func TestOrdinaryImportsRenameNothing(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"card.tsx": "function up() { return 1; }\nexport default function Card() { return <p>{up()}</p>; }\n",
		"page.tsx": "import Card from './card';\nexport default function P() { return <Card />; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if names := topNames(moduleNamed(t, bundle, "card.tsx").Program); !names["Card"] || !names["up"] {
		t.Errorf("names: %v", names)
	}
}

func TestLazyTargetKeepsItsName(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"heavy.tsx": "export default function Heavy() { return <p />; }\n",
		"page.tsx":  "import { lazy } from 'react';\nconst Heavy = lazy(() => import('./heavy'));\nexport default function P() { return <Heavy />; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !topNames(moduleNamed(t, bundle, "heavy.tsx").Program)["Heavy"] {
		t.Error("the lazy target was renamed")
	}
}

func TestDefaultExportsCanShareAName(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.tsx":    "export default function Card() { return <p />; }\n",
		"b.tsx":    "function Card() { return <p />; }\nexport default Card;\n",
		"page.tsx": "import A from './a';\nimport B from './b';\nexport default function P() { return <main><A /><B /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	a := topNames(moduleNamed(t, bundle, "a.tsx").Program)
	mod := moduleNamed(t, bundle, "b.tsx")
	for name := range topNames(mod.Program) {
		if a[name] {
			t.Errorf("%s is shared", name)
		}
		for _, stmt := range mod.Program.Body {
			if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Default && exp.Local != name {
				t.Errorf("default export points at %q, binding is %q", exp.Local, name)
			}
		}
	}
}

// Cached programs are shared between pages that may need different renames.
func TestRenameLeavesTheCachedProgramAlone(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"lib.tsx": "function up() { return 1; }\nexport function L() { return <p>{up()}</p>; }\n",
		"p1.tsx":  "import { L } from './lib';\nfunction up() { return 2; }\nexport default function P() { return <L />; }\n",
		"p2.tsx":  "import { L } from './lib';\nexport default function P() { return <L />; }\n",
	})
	cache := NewModuleCache()
	build := func(page string) *Bundle {
		b := New(dir)
		b.SetModuleCache(cache)
		bundle, err := b.Bundle(filepath.Join(dir, page))
		if err != nil {
			t.Fatal(err)
		}
		return bundle
	}
	first := moduleNamed(t, build("p1.tsx"), "lib.tsx").Program
	if topNames(first)["up"] {
		t.Fatal("collision not resolved")
	}
	if second := moduleNamed(t, build("p2.tsx"), "lib.tsx").Program; !topNames(second)["up"] {
		t.Error("a rename leaked into a page without the collision")
	}
	if again := moduleNamed(t, build("p1.tsx"), "lib.tsx").Program; again != first {
		t.Error("the renamed copy was not reused")
	}
}

func TestClientAndActionModulesKeepTheirNames(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"c.tsx":    "\"use client\";\nfunction up() { return 1; }\nexport default function C() { return <p>{up()}</p>; }\n",
		"d.tsx":    "function up() { return 2; }\nexport function D() { return <p>{up()}</p>; }\n",
		"page.tsx": "import C from './c';\nimport { D } from './d';\nexport default function P() { return <main><C /><D /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !topNames(moduleNamed(t, bundle, "c.tsx").Program)["up"] {
		t.Error("a use client module was renamed")
	}
	if topNames(moduleNamed(t, bundle, "d.tsx").Program)["up"] {
		t.Error("the other module must make way")
	}
}

func TestSameNamedExportsWarn(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.tsx":    "export function Card() { return <p />; }\nexport const unused = 1;\n",
		"b.tsx":    "export function Card() { return <p />; }\nexport const unused = 2;\n",
		"page.tsx": "import { Card as A } from './a';\nimport { Card as B } from './b';\nexport default function P() { return <main><A /><B /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	var got []diag.Diagnostic
	for _, d := range moduleNamed(t, bundle, "page.tsx").Diagnostics {
		if d.Code == diag.CodeExportCollision {
			got = append(got, d)
		}
	}
	if len(got) != 1 || !strings.Contains(got[0].Message, `"Card"`) {
		t.Errorf("diagnostics: %+v", got)
	}
}

func TestCloneProgramIsDeep(t *testing.T) {
	dir := writeModules(t, map[string]string{"a.tsx": "export function A({ x }) { return <p class='y'>{x}{`t${x}`}</p>; }\n"})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "a.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	orig := bundle.Modules[0].Program
	clone := cloneProgram(orig)
	if !reflect.DeepEqual(orig, clone) {
		t.Fatal("clone differs")
	}
	before := renderNames(orig)
	renameBindings(clone, func(n string) bool { return n == "A" }, "$x")
	if renderNames(orig) != before {
		t.Error("renaming the clone changed the original")
	}
}

func renderNames(prog *ast.Program) string {
	var out []string
	visitStmts(prog.Body, false, func(n any, _ bool) bool {
		if fn, ok := n.(*ast.FnDecl); ok {
			out = append(out, fn.Name)
		}
		return true
	})
	return strings.Join(out, ",")
}
