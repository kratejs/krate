package bundler

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

// sharedModule bundles a page importing lib.tsx on a module cache and returns
// lib.tsx's module, as a page build sees it before its renames.
func sharedModule(t *testing.T) (*Bundler, *Module) {
	t.Helper()
	dir := writeModules(t, map[string]string{
		"lib.tsx":  "import def from './dep';\nimport { pick } from './dep';\nfunction up() { return def + pick(); }\nexport function Lib() { return <p>{up()}</p>; }\n",
		"dep.tsx":  "export default 1;\nexport function pick() { return 2; }\n",
		"page.tsx": "import { Lib } from './lib';\nexport default function P() { return <Lib />; }\n",
	})
	b := New(dir)
	b.SetModuleCache(NewModuleCache())
	bundle, err := b.Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	mod := moduleNamed(t, bundle, "lib.tsx")
	if mod.IsEntry || mod.Rewritable {
		t.Fatal("lib.tsx is not a shared module")
	}
	return b, mod
}

func TestPrivateRenamesThatChangeNothingKeepTheProgram(t *testing.T) {
	b, mod := sharedModule(t)
	orig := mod.Program
	// No binding of lib.tsx is named "absent": nothing to copy or rename.
	b.applyPrivateRenames(mod, map[string]string{"absent": "absent$x"}, nil, map[string]string{"gone": "gone$x"}, moduleRewrites{})
	if mod.Program != orig {
		t.Fatal("a rename that changes nothing copied the program")
	}
}

func TestPrivateRenamesShareCopiesOfTheSameEdits(t *testing.T) {
	b, mod := sharedModule(t)
	orig := mod.Program
	before := astprint.Print(orig)

	b.applyPrivateRenames(mod, map[string]string{"up": "up$a"}, nil, nil, moduleRewrites{})
	first := mod.Program
	if first == orig {
		t.Fatal("the renamed module kept the shared program")
	}
	if got := astprint.Print(orig); got != before {
		t.Fatalf("the shared program changed:\n%s", got)
	}
	out := astprint.Print(first)
	if !strings.Contains(out, "function up$a()") || !strings.Contains(out, "up$a()") || strings.Contains(out, "up()") {
		t.Fatalf("binding not renamed:\n%s", out)
	}

	// Another page asks for the same rename plus one that does not apply:
	// the edits are the same, so it gets the same copy.
	mod.Program = orig
	b.applyPrivateRenames(mod, map[string]string{"up": "up$a", "absent": "absent$b"}, nil, nil, moduleRewrites{})
	if mod.Program != first {
		t.Fatal("the same edits made a second copy")
	}

	// A different rename is a different copy.
	mod.Program = orig
	b.applyPrivateRenames(mod, map[string]string{"up": "up$c"}, nil, nil, moduleRewrites{})
	if mod.Program == first || !strings.Contains(astprint.Print(mod.Program), "function up$c()") {
		t.Fatal("a different rename reused the copy")
	}
}

func TestPrivateRenamesEditImports(t *testing.T) {
	b, mod := sharedModule(t)
	orig := mod.Program
	dep := filepath.Join(filepath.Dir(mod.Path), "dep.tsx")
	rw := moduleRewrites{named: map[importKey]string{{dep, "pick"}: "pick$d"}}
	b.applyPrivateRenames(mod, nil, map[string]string{"def": "def$d"}, nil, rw)
	out := astprint.Print(mod.Program)
	for _, want := range []string{"import def$d from", "pick$d", "def$d + pick$d()"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(astprint.Print(orig), "$d") {
		t.Fatal("the shared program was edited")
	}
}

func TestDropPrograms(t *testing.T) {
	b, mod := sharedModule(t)
	orig := mod.Program
	b.applyPrivateRenames(mod, map[string]string{"up": "up$a"}, nil, nil, moduleRewrites{})
	dropped := b.cache.DropPrograms()
	has := func(p any) bool {
		for _, d := range dropped {
			if any(d) == p {
				return true
			}
		}
		return false
	}
	if !has(orig) || !has(mod.Program) {
		t.Fatalf("dropped %d programs, without lib.tsx or its renamed copy", len(dropped))
	}
	if len(b.cache.programs) != 0 || b.cache.renamed != nil {
		t.Fatal("the cache still holds programs")
	}
	// A later bundle parses again.
	if _, err := b.Bundle(filepath.Join(filepath.Dir(mod.Path), "page.tsx")); err != nil {
		t.Fatal(err)
	}
	if len(b.cache.programs) == 0 {
		t.Fatal("the next bundle did not repopulate the cache")
	}
}
