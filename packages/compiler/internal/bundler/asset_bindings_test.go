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
		t.Fatalf("want one KR7126 warning naming logo, got %v", d)
	}
}

// An asset binding re-exported through any chain of modules resolves to the
// asset URL in the module that reads it, with no warning.
func TestReexportedAssetImportsResolve(t *testing.T) {
	bundle, logoURL := bundleAssetProject(t, map[string]string{
		"src/assets/hero.png":  "fake-hero-bytes",
		"src/assets/Button.ts": `export function Button() { return "b"; }`,
		// import then export a name, export default, and a direct re-export.
		"src/assets/index.ts": `import logo from "./logo.png";
export { logo };
export { default as hero } from "./hero.png";
export { default as plain } from "./logo.png";
export { logo as mark };
export { Button } from "./Button";
`,
		"src/assets/default.ts": `import logo from "./logo.png";
export default logo;
`,
		"src/assets/viaDefault.ts": `export { default } from "./default";
export { default as again } from "./default";
`,
		"src/assets/all.ts":    `export * from "./index";`,
		"src/assets/deeper.ts": `export * from "./all";`,
		"src/assets/ns.ts":     `export * as icons from "./index";`,
		"src/pages/index.tsx": `import { logo, hero, plain, mark, Button } from "../assets";
import { logo as renamed, hero as heroTwo } from "../assets/deeper";
import dlogo from "../assets/default";
import viaDefault, { again } from "../assets/viaDefault";
import * as lib from "../assets";
import { icons } from "../assets/ns";
export default function P() {
  return <div>
    <img src={logo} /><img src={hero} /><img src={plain} /><img src={mark} />
    <img src={renamed} /><img src={heroTwo} /><img src={dlogo} /><img src={viaDefault} /><img src={again} />
    <img src={lib.logo} /><img src={lib["hero"]} /><img src={icons.logo} /><b>{Button()}</b>
  </div>;
}
`,
	})
	heroURL := bundle.AssetFiles[filepath.Join(filepath.Dir(filepath.Dir(bundlePageDir(bundle))), "src/assets/hero.png")]
	if logoURL == "" || heroURL == "" {
		t.Fatalf("assets not registered: %q %q", logoURL, heroURL)
	}
	page := assetModuleNamed(bundle, "index.tsx")
	if got := countLiteral(page, logoURL); got != 9 {
		t.Errorf("found %d references to the logo URL, want 9", got)
	}
	if got := countLiteral(page, heroURL); got != 3 {
		t.Errorf("found %d references to the hero URL, want 3", got)
	}
	if d := assetDiags(page); len(d) != 0 {
		t.Errorf("unexpected diagnostics: %v", d)
	}
}

func bundlePageDir(b *Bundle) string { return filepath.Dir(b.EntryPath) }

// A name the importing module or the barrel defines itself is not an asset,
// and a reread that cannot be replaced still warns.
func TestReexportedAssetImportEdges(t *testing.T) {
	bundle, url := bundleAssetProject(t, map[string]string{
		"src/assets/index.ts": `import logo from "./logo.png";
export { logo };
export const own = "mine";
`,
		// A local export wins over the same name from export *.
		"src/assets/shadow.ts": `export * from "./index";
export const logo = "local";
`,
		// A cycle between two barrels still resolves.
		"src/assets/a.ts": `export * from "./b";
import logo from "./logo.png";
export { logo as cyc };
`,
		"src/assets/b.ts": `export * from "./a";`,
		"src/pages/index.tsx": `import { own } from "../assets";
import { logo as shadowed } from "../assets/shadow";
import { cyc } from "../assets/b";
import { logo } from "../assets";
const byUrl = { [logo]: 1 };
export default function P() { return <p>{own}{shadowed}<img src={cyc} />{String(Object.keys(byUrl))}</p>; }
`,
	})
	page := assetModuleNamed(bundle, "index.tsx")
	// cyc is replaced; shadowed is the barrel's own constant, so it is not.
	if got := countLiteral(page, url); got != 1 {
		t.Errorf("found %d references to the logo URL, want 1 (cyc)", got)
	}
	d := assetDiags(page)
	if len(d) != 1 || !strings.Contains(d[0].Message, "`logo`") {
		t.Fatalf("want one warning for the computed key use of logo, got %v", d)
	}
}
