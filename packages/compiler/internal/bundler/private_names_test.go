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

func collisionWarnings(t *testing.T, bundle *Bundle) []diag.Diagnostic {
	t.Helper()
	var got []diag.Diagnostic
	for _, d := range moduleNamed(t, bundle, "page.tsx").Diagnostics {
		if d.Code == diag.CodeExportCollision {
			got = append(got, d)
		}
	}
	return got
}

func importSpecs(mod *Module, base string) []ast.NamedImport {
	for _, stmt := range mod.Program.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok && strings.HasSuffix(strings.Trim(imp.Source, "\"'"), base) {
			return imp.Named
		}
	}
	return nil
}

func TestSameNamedExportsMoveAndImportersFollow(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.tsx":    "export function Card() { return <p />; }\nexport const unused = 1;\n",
		"b.tsx":    "export function Card() { return <b />; }\nexport const other = 2;\n",
		"page.tsx": "import { Card as A } from './a';\nimport { Card as B } from './b';\nexport default function P() { return <main><A /><B /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if got := collisionWarnings(t, bundle); len(got) != 0 {
		t.Errorf("the collision should be resolved: %+v", got)
	}
	a, b := topNames(moduleNamed(t, bundle, "a.tsx").Program), topNames(moduleNamed(t, bundle, "b.tsx").Program)
	if !a["Card"] || b["Card"] {
		t.Errorf("a: %v, b: %v", a, b)
	}
	var moved string
	for name := range b {
		if OriginalName(name) == "Card" {
			moved = name
		}
	}
	if moved == "" || moved == "Card" || !strings.HasPrefix(moved, "Card"+PrivateNameMark) {
		t.Fatalf("b's names: %v", b)
	}
	specs := importSpecs(moduleNamed(t, bundle, "page.tsx"), "./b")
	if len(specs) != 1 || specs[0].Remote != moved || specs[0].Local != "B" {
		t.Errorf("import of b: %+v, want Card -> %s as B", specs, moved)
	}
	if specs := importSpecs(moduleNamed(t, bundle, "page.tsx"), "./a"); len(specs) != 1 || specs[0].Remote != "Card" {
		t.Errorf("import of a changed: %+v", specs)
	}
	// An export that only one module declares is left alone.
	if !b["other"] || !a["unused"] {
		t.Errorf("unrelated export renamed: a %v, b %v", a, b)
	}
}

func TestPlainImportOfMovedExportRebindsTheLocal(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.tsx":    "export function Card() { return <p />; }\n",
		"b.tsx":    "export function Card() { return <b />; }\n",
		"x.tsx":    "import { Card } from './b';\nexport function X() { return <Card />; }\n",
		"page.tsx": "import { Card } from './a';\nimport { X } from './x';\nexport default function P() { return <main><Card /><X /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if got := collisionWarnings(t, bundle); len(got) != 0 {
		t.Errorf("warnings: %+v", got)
	}
	x := moduleNamed(t, bundle, "x.tsx")
	specs := importSpecs(x, "./b")
	if len(specs) != 1 || specs[0].Local == "Card" || specs[0].Local != specs[0].Remote || OriginalName(specs[0].Remote) != "Card" {
		t.Fatalf("import in x: %+v", specs)
	}
	var tag string
	visitStmts(x.Program.Body, false, func(n any, _ bool) bool {
		if el, ok := n.(*ast.JSXElement); ok {
			tag = el.Opening.Name
		}
		return true
	})
	if tag != specs[0].Local {
		t.Errorf("JSX tag %q does not follow the import %q", tag, specs[0].Local)
	}
}

// A module stays as it is, and the collision stays a warning, when something
// reads its export names from outside the import graph.
func TestSameNamedExportsStayAndWarnWhenTheyCannotMove(t *testing.T) {
	base := map[string]string{
		"a.tsx":    "export function Card() { return <p />; }\n",
		"b.tsx":    "export function Card() { return <b />; }\n",
		"page.tsx": "import { Card as A } from './a';\nimport { Card as B } from './b';\nexport default function P() { return <main><A /><B /></main>; }\n",
	}
	importBarrel := base["page.tsx"] + "import './barrel';\n"
	cases := map[string]map[string]string{
		"barrel":       {"barrel.ts": "export { Card as CA } from './a';\nexport { Card as CB } from './b';\n", "page.tsx": importBarrel},
		"star":         {"barrel.ts": "export * from './a';\nexport * from './b';\n", "page.tsx": importBarrel},
		"local barrel": {"barrel.ts": "import { Card as CA } from './a';\nimport { Card as CB } from './b';\nexport { CA, CB };\n", "page.tsx": importBarrel},
		"use client":   {"a.tsx": "\"use client\";\nexport function Card() { return <p />; }\n", "b.tsx": "\"use client\";\nexport function Card() { return <b />; }\n"},
		"use server":   {"a.tsx": "\"use server\";\nexport async function Card() { return 1; }\n", "b.tsx": "\"use server\";\nexport async function Card() { return 2; }\n"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{}
			for k, v := range base {
				files[k] = v
			}
			for k, v := range extra {
				files[k] = v
			}
			dir := writeModules(t, files)
			bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
			if err != nil {
				t.Fatal(err)
			}
			got := collisionWarnings(t, bundle)
			if len(got) != 1 || !strings.Contains(got[0].Message, `"Card"`) {
				t.Errorf("diagnostics: %+v", got)
			}
			for _, base := range []string{"a.tsx", "b.tsx"} {
				if names := topNames(moduleNamed(t, bundle, base).Program); !names["Card"] {
					t.Errorf("%s was renamed: %v", base, names)
				}
			}
			if specs := importSpecs(moduleNamed(t, bundle, "page.tsx"), "./b"); len(specs) != 1 || specs[0].Remote != "Card" {
				t.Errorf("import of b changed: %+v", specs)
			}
		})
	}
}

// One of the two can move even when the other cannot.
func TestMovableModuleMakesWayForOneThatCannotMove(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.tsx":    "\"use client\";\nexport function Card() { return <p />; }\n",
		"b.tsx":    "export function Card() { return <b />; }\n",
		"page.tsx": "import { Card as A } from './a';\nimport { Card as B } from './b';\nexport default function P() { return <main><A /><B /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if got := collisionWarnings(t, bundle); len(got) != 0 {
		t.Errorf("warnings: %+v", got)
	}
	if !topNames(moduleNamed(t, bundle, "a.tsx").Program)["Card"] || topNames(moduleNamed(t, bundle, "b.tsx").Program)["Card"] {
		t.Error("the movable module should have moved")
	}
}

// A name exported by one module and imported by it from several importers is
// an ordinary import.
func TestOrdinaryNamedImportsRenameNothing(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"card.tsx": "export function Card() { return <p />; }\nexport const other = 1;\n",
		"x.tsx":    "import { Card } from './card';\nexport function X() { return <Card />; }\n",
		"b.tsx":    "export function Card2() { return <b />; }\n",
		"page.tsx": "import { Card } from './card';\nimport { Card2 } from './b';\nimport { X } from './x';\nexport default function P() { return <main><Card /><Card2 /><X /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{"card.tsx", "x.tsx", "b.tsx", "page.tsx"} {
		for name := range topNames(moduleNamed(t, bundle, base).Program) {
			if strings.Contains(name, PrivateNameMark) {
				t.Errorf("%s renamed %s", base, name)
			}
		}
	}
	if len(collisionWarnings(t, bundle)) != 0 {
		t.Error("unexpected warning")
	}
}

func TestMovedExportKeepsTheCachedProgramAlone(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.tsx":  "export function Card() { return <p />; }\n",
		"b.tsx":  "export function Card() { return <b />; }\n",
		"p1.tsx": "import { Card as A } from './a';\nimport { Card as B } from './b';\nexport default function P() { return <main><A /><B /></main>; }\n",
		"p2.tsx": "import { Card } from './b';\nexport default function P() { return <Card />; }\n",
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
	first := moduleNamed(t, build("p1.tsx"), "b.tsx").Program
	if topNames(first)["Card"] {
		t.Fatal("export did not move")
	}
	if second := moduleNamed(t, build("p2.tsx"), "b.tsx").Program; !topNames(second)["Card"] {
		t.Error("a rename leaked into a page without the collision")
	}
	if again := moduleNamed(t, build("p1.tsx"), "b.tsx").Program; again != first {
		t.Error("the renamed copy was not reused")
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

// Two modules that export the same name collide in the page's one namespace
// even when the page imports it from only one of them: menu's onMenu calls its
// own onClick, and the page imports tray's. Before, menu's onClick was dropped
// and onMenu subscribed to tray clicks (@krate/app/menu and @krate/app/tray).
func TestExportDeclaredTwiceMovesWhenImportedOnce(t *testing.T) {
	for _, pkg := range []string{"", "node_modules/pkg/"} {
		t.Run("dir="+pkg, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{
				pkg + "menu.ts": "export function onClick(fn) { return 'menu:' + fn; }\nexport function onMenu(fn) { return onClick(fn); }\n",
				pkg + "tray.ts": "export function onClick(fn) { return 'tray:' + fn; }\n",
				"page.tsx": "import { onMenu } from './" + pkg + "menu';\nimport { onClick } from './" + pkg + "tray';\n" +
					"export default function P() { return <main onClick={() => { onMenu(1); onClick(2); }} />; }\n",
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
			bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
			if err != nil {
				t.Fatal(err)
			}
			if got := collisionWarnings(t, bundle); len(got) != 0 {
				t.Errorf("the collision should be resolved: %+v", got)
			}
			menu, tray := topNames(moduleNamed(t, bundle, "menu.ts").Program), topNames(moduleNamed(t, bundle, "tray.ts").Program)
			for name := range menu {
				if tray[name] {
					t.Errorf("menu.ts and tray.ts both declare %s: menu %v, tray %v", name, menu, tray)
				}
			}
		})
	}
}

// Namespace imports of two modules that export one name: a.main and b.main
// must stay two functions (@krate/app/window and @krate/app/chrome).
func TestNamespaceImportsKeepSameNamedExportsApart(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"a.ts":     "export function main() { return 'a'; }\n",
		"b.ts":     "export function main() { return 'b'; }\n",
		"page.tsx": "import * as a from './a';\nimport * as b from './b';\nexport default function P() { return <main onClick={() => [a.main(), b.main()]} />; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := topNames(moduleNamed(t, bundle, "a.ts").Program), topNames(moduleNamed(t, bundle, "b.ts").Program)
	for name := range a {
		if b[name] {
			t.Errorf("a.ts and b.ts both declare %s: a %v, b %v; warnings %+v", name, a, b, collisionWarnings(t, bundle))
		}
	}
}
