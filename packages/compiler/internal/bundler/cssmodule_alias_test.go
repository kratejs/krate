package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

// A CSS module imported through a tsconfig paths alias gets its class
// references rewritten, the same as a relative import of the file.
func TestCSSModuleThroughPathAliasIsRewritten(t *testing.T) {
	for name, spec := range map[string]string{"alias": "@/components/box.module.css", "relative": "../components/box.module.css"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			write := func(rel, src string) {
				p := filepath.Join(root, filepath.FromSlash(rel))
				_ = os.MkdirAll(filepath.Dir(p), 0755)
				if err := os.WriteFile(p, []byte(src), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("components/box.module.css", ".box { color: red; }\n")
			write("pages/index.tsx", `import s from "`+spec+`";
export default function Page() { return <p class={s.box}>hi</p>; }`)
			b := New(root)
			b.SetPathAliases([]string{"@/*"}, [][]string{{"./*"}}, root)
			bundle, err := b.Bundle(filepath.Join(root, "pages", "index.tsx"))
			if err != nil {
				t.Fatalf("Bundle: %v", err)
			}
			var info *CSSModuleInfo
			for _, m := range bundle.CSSModules {
				info = m
			}
			if info == nil || info.Mappings["box"] == "" {
				t.Fatalf("no CSS module mapping collected: %#v", bundle.CSSModules)
			}
			var printed string
			for _, mod := range bundle.Modules {
				if strings.HasSuffix(filepath.ToSlash(mod.Path), "pages/index.tsx") && mod.Program != nil {
					printed = astprint.Print(mod.Program)
				}
			}
			if !strings.Contains(printed, info.Mappings["box"]) || strings.Contains(printed, "s.box") {
				t.Errorf("class reference was not rewritten to %q:\n%s", info.Mappings["box"], printed)
			}
		})
	}
}
