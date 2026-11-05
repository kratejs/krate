package bundler

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func bundleAssetProject(t *testing.T, files map[string]string) (*Bundle, string) {
	t.Helper()
	root := t.TempDir()
	files["src/assets/logo.png"] = "fake-png-bytes"
	writeFiles(t, root, files)
	b := New(root)
	bundle, err := b.Bundle(filepath.Join(root, "src/pages/index.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	return bundle, bundle.AssetFiles[filepath.Join(root, "src/assets/logo.png")]
}

func countLiteral(m *Module, url string) int {
	n := 0
	walkASTNodes(reflect.ValueOf(m.Program.Body), func(x any) bool {
		if lit, ok := x.(*ast.Literal); ok && lit.Value == url {
			n++
		}
		return true
	})
	return n
}

func assetModuleNamed(b *Bundle, name string) *Module {
	for _, m := range b.Modules {
		if filepath.Base(m.Path) == name {
			return m
		}
	}
	return nil
}

func assetDiags(m *Module) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range m.Diagnostics {
		if d.Code == diag.CodeAssetBindingUnresolved {
			out = append(out, d)
		}
	}
	return out
}

// `import { default as x }`, `import * as ns` (ns.default) and reads inside a
// class body or parentheses resolve to the asset URL like a default import.
func TestAssetImportForms(t *testing.T) {
	bundle, url := bundleAssetProject(t, map[string]string{
		"src/pages/index.tsx": `import logo from "../assets/logo.png";
import * as ns from "../assets/logo.png";
import { default as named } from "../assets/logo.png";
class K { src = logo; get() { return logo; } }
const wrapped = (logo);
export default function P() { return <div><img src={ns.default} /><img src={named} /><img src={ns["default"]} /></div>; }
`,
	})
	if url == "" {
		t.Fatal("asset not registered")
	}
	page := assetModuleNamed(bundle, "index.tsx")
	// ns.default, named, ns["default"], two reads in K, and the parenthesized read.
	if got := countLiteral(page, url); got != 6 {
		t.Errorf("found %d references to %s, want 6", got, url)
	}
	if d := assetDiags(page); len(d) != 0 {
		t.Errorf("unexpected diagnostics: %v", d)
	}
}

// A read the rewrite cannot reach is reported instead of staying a silent
// ReferenceError in the browser.
func TestAssetBindingStillReferencedWarns(t *testing.T) {
	bundle, _ := bundleAssetProject(t, map[string]string{
		"src/pages/index.tsx": `import logo from "../assets/logo.png";
const byUrl = { [logo]: 1 };
export default function P() { return <p>{String(Object.keys(byUrl))}</p>; }
`,
	})
	d := assetDiags(assetModuleNamed(bundle, "index.tsx"))
	if len(d) != 1 || !strings.Contains(d[0].Message, "`logo`") || d[0].Severity != diag.SeverityWarning {
		t.Fatalf("want one KR7125 warning naming logo, got %v", d)
	}
}

// An asset binding re-exported by a barrel module has no value in the page
// script; the importing file gets a warning that says so.
func TestReexportedAssetImportWarns(t *testing.T) {
	bundle, url := bundleAssetProject(t, map[string]string{
		"src/assets/index.ts": `import logo from "./logo.png";
export { logo };
export const hero = logo;
`,
		"src/pages/index.tsx": `import { logo, hero } from "../assets";
export default function P() { return <div><img src={logo} /><img src={hero} /></div>; }
`,
	})
	if url == "" {
		t.Fatal("asset not registered")
	}
	d := assetDiags(assetModuleNamed(bundle, "index.tsx"))
	if len(d) != 1 || !strings.Contains(d[0].Message, "`logo`") || strings.Contains(d[0].Message, "hero") {
		t.Fatalf("want one warning for logo only, got %v", d)
	}
	if d[0].Line != 1 {
		t.Errorf("warning at line %d, want the import on line 1", d[0].Line)
	}
}
