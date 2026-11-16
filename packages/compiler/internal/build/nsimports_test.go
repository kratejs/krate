package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// TestNamespaceImportRewrite: a namespace re-exported by a pure index is
// imported from its module, so esbuild tree shakes its members; an index that
// runs code of its own is left alone, and so is the text of a string.
func TestNamespaceImportRewrite(t *testing.T) {
	root := t.TempDir()
	// nslib's index re-exports impl's namespace as n and as the default, the
	// way zod's does.
	writeProjectFile(t, root, "node_modules/nslib/package.json", `{"name":"nslib","version":"1.0.0","type":"module","main":"index.js","sideEffects":false}`)
	writeProjectFile(t, root, "node_modules/nslib/index.js", `import * as n from "./impl.js";
export * from "./impl.js";
export { n, n as default };
`)
	writeProjectFile(t, root, "node_modules/nslib/impl.js", `export function used() { return "ns-used-mark"; }
export function unused() { return "ns-unused-mark"; }
export * as extra from "./extra.js";
`)
	writeProjectFile(t, root, "node_modules/nslib/extra.js", `export const locale = "ns-extra-mark";
`)
	// A second hop: an index that re-exports nslib's n.
	writeProjectFile(t, root, "node_modules/nswrap/package.json", `{"name":"nswrap","version":"1.0.0","type":"module","main":"index.js"}`)
	writeProjectFile(t, root, "node_modules/nswrap/index.js", `export { n as w } from "nslib";
`)
	// An index with a statement of its own is not skipped.
	writeProjectFile(t, root, "node_modules/nsimpure/package.json", `{"name":"nsimpure","version":"1.0.0","type":"module","main":"index.js"}`)
	writeProjectFile(t, root, "node_modules/nsimpure/index.js", `import * as p from "./impl.js";
globalThis.impureMark = "impure-effect-mark";
export { p };
`)
	writeProjectFile(t, root, "node_modules/nsimpure/impl.js", `export function used() { return "impure-used-mark"; }
export function unused() { return "impure-unused-mark"; }
`)
	writeProjectFile(t, root, "src/entry.ts", `import { n } from "nslib";
import d from "nslib";
import { n as m, type Unused } from "nslib";
import { w } from "nswrap";
import { p } from "nsimpure";
const text: string = "import { n } from 'nslib'";
// import { n } from "nslib"
let u: Unused | undefined;
console.log(n.used(), d.used(), m.used(), w.used(), p.used(), text, u);
`)
	opts := api.BuildOptions{
		EntryPoints:   []string{filepath.Join(root, "src", "entry.ts")},
		Bundle:        true,
		Write:         false,
		Format:        api.FormatESModule,
		Outdir:        filepath.Join(root, "out"),
		AbsWorkingDir: root,
		LogLevel:      api.LogLevelSilent,
	}
	withNamespaceImports(&opts)
	res := api.Build(opts)
	if len(res.Errors) > 0 {
		t.Fatalf("build: %v", res.Errors)
	}
	out := string(res.OutputFiles[0].Contents)
	for _, want := range []string{"ns-used-mark", "impure-effect-mark", "impure-used-mark", "import { n } from 'nslib'"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"ns-unused-mark", "ns-extra-mark"} {
		if strings.Contains(out, bad) {
			t.Errorf("an export the file never reads was bundled (%q):\n%s", bad, out)
		}
	}

	// The answer for an index is kept across builds only while the index is
	// unchanged: once it runs code of its own, the import is left alone.
	index := filepath.Join(root, "node_modules", "nslib", "index.js")
	writeProjectFile(t, root, "node_modules/nslib/index.js", `import * as n from "./impl.js";
globalThis.nsMark = "ns-index-effect-mark";
export * from "./impl.js";
export { n, n as default };
`)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(index, later, later); err != nil {
		t.Fatal(err)
	}
	resolver.ResetCaches()
	opts.Plugins = nil
	withNamespaceImports(&opts)
	res = api.Build(opts)
	if len(res.Errors) > 0 {
		t.Fatalf("second build: %v", res.Errors)
	}
	out = string(res.OutputFiles[0].Contents)
	for _, want := range []string{"ns-index-effect-mark", "ns-unused-mark"} {
		if !strings.Contains(out, want) {
			t.Errorf("after the index changed, output lacks %q:\n%s", want, out)
		}
	}
}
