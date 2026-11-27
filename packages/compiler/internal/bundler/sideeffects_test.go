package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/css"
)

// barrelLib writes a package whose index re-exports three components, each
// importing its own stylesheet, plus a shared token sheet the index imports.
func barrelLib(t *testing.T, root, sideEffects string) {
	t.Helper()
	pkg := `{"name":"uilib","version":"1.0.0","main":"index.ts"`
	if sideEffects != "" {
		pkg += `,"sideEffects":` + sideEffects
	}
	writeTree(t, root, map[string]string{
		"node_modules/uilib/package.json": pkg + "}",
		"node_modules/uilib/index.ts":     "import './tokens.css';\nexport * from './a/a';\nexport * from './b/b';\nexport { C as Renamed } from './c/c';\n",
		"node_modules/uilib/tokens.css":   ".tokens{}",
		"node_modules/uilib/a/a.tsx":      "import './a.css';\nexport function A() { return <i class=\"a\"/>; }\n",
		"node_modules/uilib/a/a.css":      ".a{}",
		"node_modules/uilib/b/b.tsx":      "import './b.css';\nimport { A } from '../a/a';\nexport function B() { return <A/>; }\n",
		"node_modules/uilib/b/b.css":      ".b{}",
		"node_modules/uilib/c/c.tsx":      "import './c.css';\nexport function C() { return <i class=\"c\"/>; }\n",
		"node_modules/uilib/c/c.css":      ".c{}",
	})
}

func cssKeys(t *testing.T, bundle *Bundle) []string {
	t.Helper()
	var out []string
	for _, p := range css.SplitParts(bundle.CSS) {
		out = append(out, strings.TrimSpace(p.CSS))
	}
	return out
}

func TestSideEffectFreeBarrelKeepsOnlyUsedComponentCSS(t *testing.T) {
	cases := []struct {
		name, sideEffects, page string
		want                    []string
	}{
		{"one component", `["*.css"]`, "import { A } from 'uilib';", []string{".tokens{}", ".a{}"}},
		// B imports A, so A's sheet comes first, as in the full import order.
		{"component using another", `["*.css"]`, "import { B } from 'uilib';", []string{".tokens{}", ".a{}", ".b{}"}},
		{"renamed re-export", `["*.css"]`, "import { Renamed } from 'uilib';", []string{".tokens{}", ".c{}"}},
		{"namespace import", `["*.css"]`, "import * as ui from 'uilib';", []string{".tokens{}", ".a{}", ".b{}", ".c{}"}},
		{"sideEffects false", `false`, "import { C } from 'uilib/c/c';", []string{".c{}"}},
		// Without the promise every module of the package runs.
		{"sideEffects true", `true`, "import { A } from 'uilib';", []string{".tokens{}", ".a{}", ".b{}", ".c{}"}},
		{"sideEffects absent", ``, "import { A } from 'uilib';", []string{".tokens{}", ".a{}", ".b{}", ".c{}"}},
		// A listed module is a side effect and keeps its sheet.
		{"listed module", `["*.css","./c/c.tsx"]`, "import { A } from 'uilib';", []string{".tokens{}", ".a{}", ".c{}"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			barrelLib(t, root, c.sideEffects)
			writeTree(t, root, map[string]string{
				"src/page.tsx": c.page + "\nimport './page.css';\nexport default function P() { return <div/>; }\n",
				"src/page.css": ".page{}",
			})
			got := cssKeys(t, bundleEntry(t, root, "src/page.tsx", nil))
			want := append(append([]string{}, c.want...), ".page{}")
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Fatalf("CSS order = %v, want %v", got, want)
			}
		})
	}
}

// TestSideEffectFreeComponentNamedWithoutImport: code may name an export of a
// module in the graph without importing it (the bundle shares one scope), so
// that module's sheet stays.
func TestSideEffectFreeComponentNamedWithoutImport(t *testing.T) {
	root := t.TempDir()
	barrelLib(t, root, `["*.css"]`)
	writeTree(t, root, map[string]string{
		"src/page.tsx": "import { A } from 'uilib';\nexport default function P() { return <div><A/><C/></div>; }\n",
	})
	got := cssKeys(t, bundleEntry(t, root, "src/page.tsx", nil))
	if strings.Join(got, " ") != ".tokens{} .a{} .c{}" {
		t.Fatalf("CSS = %v, want the sheets of A and C", got)
	}
}

// TestRawImportKeepsCSSOfTheImportedModule: importing a module and its own
// source text (?raw) must not hide the module's stylesheets, nor those of the
// side-effect-free package components it imports.
func TestRawImportKeepsCSSOfTheImportedModule(t *testing.T) {
	root := t.TempDir()
	barrelLib(t, root, `["*.css"]`)
	writeTree(t, root, map[string]string{
		"src/demo.tsx": "import './demo.css';\nimport { A } from 'uilib';\nexport default function Demo() { return <A/>; }\n",
		"src/demo.css": ".demo{}",
		"src/page.tsx": "import Demo from './demo';\nimport src from './demo.tsx?raw';\nexport default function P() { return <main><Demo/><pre>{src}</pre></main>; }\n",
	})
	got := cssKeys(t, bundleEntry(t, root, "src/page.tsx", nil))
	if strings.Join(got, " ") != ".demo{} .tokens{} .a{}" {
		t.Fatalf("CSS = %v, want the sheets of demo and A", got)
	}
}

// linkComponents links the workspace @krate/components (and the runtime it
// imports) into root's node_modules.
func linkComponents(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"components", "runtime"} {
		target, err := filepath.Abs(filepath.Join("..", "..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "node_modules", "@krate", name)
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("cannot link %s: %v", name, err)
		}
	}
}

// TestComponentStylesFollowTheNamesUsed: a component's styles load when it is
// imported from the barrel as well as from its own module, and a function the
// barrel exports (toast) brings the styles of the component it shows.
func TestComponentStylesFollowTheNamesUsed(t *testing.T) {
	cases := []struct {
		name, page, want string
		notWant          string
	}{
		{"dialog from barrel", "import { Dialog } from '@krate/components';\nexport default function P() { return <Dialog/>; }\n", ".krate-dialog", ".krate-toast"},
		{"dialog from module", "import { Dialog } from '@krate/components/dialog';\nexport default function P() { return <Dialog/>; }\n", ".krate-dialog", ".krate-toast"},
		{"toast() from barrel", "import { toast } from '@krate/components';\nexport default function P() { return <button onClick={() => toast('hi')}>x</button>; }\n", ".krate-toast", ".krate-dialog"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			linkComponents(t, root)
			writeTree(t, root, map[string]string{"src/page.tsx": c.page})
			sheet := strings.Join(cssKeys(t, bundleEntry(t, root, "src/page.tsx", nil)), "\n")
			if !strings.Contains(sheet, c.want+" {") {
				t.Errorf("styles for %s are missing", c.want)
			}
			if strings.Contains(sheet, c.notWant+" {") {
				t.Errorf("styles for %s should not ship", c.notWant)
			}
		})
	}
}
