package bundler

import (
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func exportedPairs(prog *ast.Program) map[string]string {
	out := map[string]string{}
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			for _, spec := range exp.Named {
				out[spec.Remote] = spec.Local
			}
		}
	}
	return out
}

func TestReservedNamesKeepTheirExportAndImportNames(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"theme.ts": "export const krateTheme = { name: 'dark' };\nexport function h() { return 'H'; }\nconst useRef = 5;\nexport { useRef as ref };\nexport const fine = 1;\n",
		"page.tsx": "import { krateTheme, h, ref, fine } from './theme';\nexport default function P() { return <p>{krateTheme.name}{h()}{ref}{fine}</p>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	theme := moduleNamed(t, bundle, "theme.ts").Program
	pairs := exportedPairs(theme)
	want := map[string]string{"krateTheme": "krateTheme$u", "h": "h$u", "ref": "useRef$u"}
	for remote, local := range want {
		if pairs[remote] != local {
			t.Errorf("export %q is bound to %q, want %q (all: %v)", remote, pairs[remote], local, pairs)
		}
	}
	if names := topNames(theme); names["krateTheme"] || names["h"] || !names["krateTheme$u"] || !names["fine"] {
		t.Errorf("declarations = %v", names)
	}

	page := moduleNamed(t, bundle, "page.tsx").Program
	got := map[string]string{}
	for _, stmt := range page.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			for _, spec := range imp.Named {
				got[spec.Remote] = spec.Local
			}
		}
	}
	for remote, local := range map[string]string{"krateTheme": "krateTheme$u", "h": "h$u", "ref": "ref", "fine": "fine"} {
		if got[remote] != local {
			t.Errorf("import %q binds %q, want %q (all: %v)", remote, got[remote], local, got)
		}
	}
}

func TestReservedNamesFromRuntimeAreNotRenamed(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"page.tsx": "import { createSignal } from '@krate/runtime';\nexport default function P() { const [n] = createSignal(1); return <p>{n()}</p>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range moduleNamed(t, bundle, "page.tsx").Program.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			for _, spec := range imp.Named {
				if spec.Local != "" && spec.Local != spec.Remote {
					t.Errorf("runtime import renamed: %+v", spec)
				}
			}
		}
	}
}
