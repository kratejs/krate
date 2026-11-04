package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func bundleDiag(t *testing.T, root string, setup func(*Bundler)) diag.Diagnostic {
	t.Helper()
	b := New(root)
	if setup != nil {
		setup(b)
	}
	_, err := b.Bundle(filepath.Join(root, "src/pages/index.tsx"))
	if err == nil {
		t.Fatal("expected an unresolved-import error, got none")
	}
	d, ok := diag.FromError(err)
	if !ok {
		t.Fatalf("not a diagnostic: %v", err)
	}
	return d
}

func TestUnresolvedRelativeImportIsLocatedError(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/pages/index.tsx": "const a = 1;\nimport {helper} from \"../does-not-exist\";\nexport default function P(){ return <p>{helper}</p> }\n",
	})
	d := bundleDiag(t, root, nil)
	if d.Line != 2 || d.Col != 1 || !strings.HasSuffix(d.File, "index.tsx") || !strings.Contains(d.Message, "../does-not-exist") || d.Hint == "" {
		t.Fatalf("diag = %+v", d)
	}
	if !strings.Contains(d.Source, "does-not-exist") {
		t.Errorf("source line missing: %q", d.Source)
	}
}

func TestUnresolvedSideEffectCSSImport(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/pages/index.tsx": "import \"./missing.css\";\nexport default function P(){ return <p>x</p> }\n",
	})
	d := bundleDiag(t, root, nil)
	if d.Line != 1 || !strings.Contains(d.Message, "./missing.css") {
		t.Fatalf("diag = %+v", d)
	}
}

func TestUnresolvedBarePackageNotInstalled(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/pages/index.tsx": "import {x} from \"not-installed-pkg/sub\";\nexport default function P(){ return <p>{x}</p> }\n",
	})
	d := bundleDiag(t, root, nil)
	if !strings.Contains(d.Message, "not installed") || !strings.Contains(d.Hint, "pnpm add not-installed-pkg") {
		t.Fatalf("diag = %+v", d)
	}
}

func TestUnresolvedAliasImportNamesTheAlias(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/pages/index.tsx":     "import Nope from \"@/components/Nope\";\nexport default function P(){ return <Nope/> }\n",
		"src/components/Real.tsx": "export default function Real(){ return <i/> }\n",
	})
	d := bundleDiag(t, root, func(b *Bundler) {
		b.SetPathAliases([]string{"@/*"}, [][]string{{"./src/*"}}, root)
	})
	if !strings.Contains(d.Message, "tsconfig path alias \"@/*\"") || strings.Contains(d.Message, "not installed") {
		t.Fatalf("diag = %+v", d)
	}
}

func TestUnresolvedImportInNestedModuleReportsThatFile(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/pages/index.tsx":       "import B from \"../components/button\";\nexport default function P(){ return <B/> }\n",
		"src/components/button.tsx": "\n\nimport {z} from \"../nope\";\nexport default function B(){ return <b>{z}</b> }\n",
	})
	d := bundleDiag(t, root, nil)
	if !strings.HasSuffix(filepath.ToSlash(d.File), "components/button.tsx") || d.Line != 3 {
		t.Fatalf("diag = %+v", d)
	}
}

func TestAliasBeatsNodeModules(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/pages/index.tsx":           "import x from \"lib\";\nexport default function P(){ return <p>{x}</p> }\n",
		"src/mine/lib.ts":               "export default 'mine'\n",
		"node_modules/lib/package.json": `{"main":"index.js"}`,
		"node_modules/lib/index.js":     "export default 'pkg'\n",
	})
	b := New(root)
	b.SetPathAliases([]string{"lib"}, [][]string{{"./src/mine/lib.ts"}}, root)
	got := b.Resolve(filepath.Join(root, "src/pages/index.tsx"), "lib")
	if !strings.HasSuffix(filepath.ToSlash(got), "src/mine/lib.ts") {
		t.Fatalf("resolved %s", got)
	}
}

func TestScaffoldTemplateAliasResolves(t *testing.T) {
	for _, tpl := range []string{"create-krate-app/templates/app", "create-krate-docs/templates/docs"} {
		dir := filepath.Join("..", "..", "..", tpl)
		data, err := os.ReadFile(filepath.Join(dir, "tsconfig.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"baseUrl": "."`) {
			t.Errorf("%s tsconfig must use baseUrl \".\" so tsc and krate resolve @/* the same way", tpl)
		}
	}
}

func TestTSExtensionSpecifiersResolveToTSSources(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/pages/index.tsx": "import {h} from \"../util.js\";\nimport {m} from \"../m.mjs\";\nimport C from \"../c.jsx\";\nexport default function P(){ return <p>{h()}{m}<C/></p> }\n",
		"src/util.ts":         "export const h = () => 'h'\n",
		"src/m.mts":           "export const m = 'm'\n",
		"src/c.tsx":           "export default function C(){ return <i/> }\n",
	})
	b := New(root)
	bundle, err := b.Bundle(filepath.Join(root, "src/pages/index.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range bundle.Modules {
		got = append(got, filepath.Base(m.Path))
	}
	for _, want := range []string{"util.ts", "m.mts", "c.tsx"} {
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Errorf("%s not bundled; modules = %v", want, got)
		}
	}
}

func TestPackageImportsSpecifierBundles(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"package.json":        `{"imports":{"#util":"./src/util.ts"}}`,
		"src/pages/index.tsx": "import {u} from \"#util\";\nexport default function P(){ return <p>{u}</p> }\n",
		"src/util.ts":         "export const u = 'u'\n",
	})
	b := New(root)
	bundle, err := b.Bundle(filepath.Join(root, "src/pages/index.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range bundle.Modules {
		found = found || filepath.Base(m.Path) == "util.ts"
	}
	if !found {
		t.Error("#util not bundled")
	}
}
