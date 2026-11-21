package build

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// An import alias binds the local name to the exported constant in the entry
// and in the component modules it imports: a component module that writes
// `import { Layer as KLayer }` renders <KLayer>, so KLayer must be defined in
// the page's hydration code.
func TestMergeModuleConstantsImportAliases(t *testing.T) {
	t.Parallel()
	parse := func(name, src string) *ast.Program {
		prog, errs := tsstrip.Parse(name, src, tsstrip.Options{})
		if len(errs) > 0 {
			t.Fatalf("parse %s: %v", name, errs)
		}
		return prog
	}
	lib := parse("lib.ts", `export const Layer = "Layer"; export const upper = "UP";`)
	editor := parse("editor.tsx", `import { Layer as KLayer, upper } from "./lib"; export function Editor() { return <KLayer />; }`)
	entry := parse("page.tsx", `import { upper as shout } from "./lib"; import { Editor } from "./editor"; export default function P() { return <Editor />; }`)
	consts, inits, _ := mergeModuleConstants(entry, []annotator.ModuleSource{
		{Program: lib, Path: "src/lib.ts"},
		{Program: editor, Path: "src/editor.tsx"},
	})
	for _, name := range []string{"KLayer", "shout"} {
		if consts[name] == "" || inits[name] == nil {
			t.Errorf("%s is not bound (consts %q)", name, consts[name])
		}
	}
	if consts["KLayer"] != consts["Layer"] || consts["shout"] != consts["upper"] {
		t.Errorf("aliases bound to the wrong values: %v", consts)
	}
}
