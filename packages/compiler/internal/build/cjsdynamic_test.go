package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// cjsDynamicFixture writes CommonJS and ESM packages an entry loads with
// import(): one compiled from ESM (__esModule, with and without a default),
// one plain CommonJS object, one whose module.exports is a function.
func cjsDynamicFixture(t *testing.T, root string) {
	t.Helper()
	pkgs := map[string]string{
		"esm-compiled": `"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.readPsd = function (b) { return "psd:" + b; };
exports.version = 34;`,
		"esm-compiled-default": `"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.default = "the default";
exports.named = "n";`,
		"plain-cjs": `module.exports = { parse: function (s) { return s.toUpperCase(); } };`,
		"fn-cjs":    `module.exports = function hi() { return "hi"; }; module.exports.extra = 1;`,
	}
	for name, src := range pkgs {
		writeProjectFile(t, root, "node_modules/"+name+"/package.json", `{"name":"`+name+`","main":"index.js"}`)
		writeProjectFile(t, root, "node_modules/"+name+"/index.js", src)
	}
	writeProjectFile(t, root, "node_modules/real-esm/package.json", `{"name":"real-esm","type":"module","main":"index.js"}`)
	writeProjectFile(t, root, "node_modules/real-esm/index.js", `export default { a: 1 }; export const b = 2;`)
}

// TestCJSDynamicImportNamedExports builds an entry that loads CommonJS
// packages with import() under code splitting, applies the interop, and runs
// the output in Node: the promise resolves to what an unsplit build gives.
func TestCJSDynamicImportNamedExports(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	root := t.TempDir()
	cjsDynamicFixture(t, root)
	writeProjectFile(t, root, "entry.js", `
export async function run() {
  const { readPsd, version } = await import("esm-compiled");
  const withDefault = await import("esm-compiled-default");
  const plain = await import("plain-cjs");
  const fn = await import("fn-cjs");
  const esm = await import("real-esm");
  const again = await import("esm-compiled");
  return {
    psd: readPsd("x"), version, psdDefault: typeof (await import("esm-compiled")).default,
    def: withDefault.default, named: withDefault.named,
    parse: plain.parse("a"), plainDefault: plain.default.parse("b"),
    fn: fn.default(), extra: fn.extra,
    esmA: esm.default.a, esmB: esm.b, esmKeys: Object.keys(esm).sort().join(","),
    same: again.readPsd === readPsd,
  };
}
`)
	for _, minify := range []bool{false, true} {
		outdir := filepath.Join(root, "out")
		if minify {
			outdir += "-min"
		}
		res := api.Build(api.BuildOptions{
			AbsWorkingDir:     root,
			EntryPoints:       []string{"entry.js"},
			Outdir:            outdir,
			Bundle:            true,
			Splitting:         true,
			Format:            api.FormatESModule,
			Platform:          api.PlatformBrowser,
			Metafile:          true,
			MinifyWhitespace:  minify,
			MinifyIdentifiers: minify,
			MinifySyntax:      minify,
			LogLevel:          api.LogLevelSilent,
		})
		if len(res.Errors) > 0 {
			t.Fatalf("build: %v", res.Errors)
		}
		interopCJSDynamicImports(res.OutputFiles, res.Metafile, root)
		wrapped := 0
		for _, f := range res.OutputFiles {
			wrapped += strings.Count(string(f.Contents), ".then("+cjsNamespaceJS+")")
			writeProjectFile(t, filepath.Dir(f.Path), filepath.Base(f.Path), string(f.Contents))
		}
		// Six import() calls of CommonJS chunks; the ESM one stays as it is.
		if wrapped != 6 {
			t.Fatalf("minify=%v: want 6 wrapped import() calls, got %d", minify, wrapped)
		}
		script := `import(process.argv[1]).then(m => m.run()).then(r => console.log(JSON.stringify(r)), e => { console.error(e); process.exit(1); });`
		cmd := exec.Command("node", "--input-type=module", "-e", script, "file://"+filepath.ToSlash(filepath.Join(outdir, "entry.js")))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("minify=%v: running the output: %v\n%s", minify, err, out)
		}
		want := `{"psd":"psd:x","version":34,"psdDefault":"undefined","def":"the default","named":"n","parse":"A","plainDefault":"B","fn":"hi","extra":1,"esmA":1,"esmB":2,"esmKeys":"b,default","same":true}`
		if got := strings.TrimSpace(string(out)); got != want {
			t.Fatalf("minify=%v:\n got %s\nwant %s", minify, got, want)
		}
	}
}

// TestCJSDynamicImportNoop leaves the outputs alone when nothing imports a
// CommonJS chunk.
func TestCJSDynamicImportNoop(t *testing.T) {
	files := []api.OutputFile{{Path: "/p/out/a.js", Contents: []byte(`import("./b.js")`)}}
	meta := `{"inputs":{"b.js":{"format":"esm"}},"outputs":{"out/a.js":{"imports":[{"path":"out/b.js","kind":"dynamic-import"}]},"out/b.js":{"entryPoint":"b.js","exports":["default"]}}}`
	interopCJSDynamicImports(files, meta, "/p")
	if got := string(files[0].Contents); got != `import("./b.js")` {
		t.Fatalf("an ESM chunk's import() was rewritten: %s", got)
	}
}

// TestChunkSpecifier matches how esbuild names one output from another.
func TestChunkSpecifier(t *testing.T) {
	for _, c := range []struct{ from, to, want string }{
		{"dist/chunks/a.js", "dist/chunks/isl-x.js", "./isl-x.js"},
		{"out/entry.mjs", "out/chunks/c-1.mjs", "./chunks/c-1.mjs"},
		{"out/chunks/c-1.mjs", "out/e.mjs", "../e.mjs"},
	} {
		if got := chunkSpecifier(c.from, c.to); got != c.want {
			t.Errorf("chunkSpecifier(%q, %q) = %q, want %q", c.from, c.to, got, c.want)
		}
	}
}

// TestIslandDynamicImportOfCommonJS builds a site whose island loads a
// CommonJS package with import() and destructures a named export, as
// `const { readPsd } = await import("ag-psd")` does: the island chunk's
// import() resolves to the package's named exports.
func TestIslandDynamicImportOfCommonJS(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	cjsDynamicFixture(t, root)
	writeProjectFile(t, root, "node_modules/fake-lib/index.js", `
		import { jsx } from 'react/jsx-runtime';
		export function Badge({ label }) {
			const open = async () => {
				const { readPsd } = await import('esm-compiled');
				return readPsd(label);
			};
			return jsx('span', { className: 'badge', onClick: open, children: label });
		}
	`)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	code := islandCode(t, cfg.OutDir)
	if !strings.Contains(code, "exports.readPsd") {
		t.Fatalf("the CommonJS package is not in the island chunks:\n%.2000s", code)
	}
	if !strings.Contains(code, ".then("+cjsNamespaceJS+")") {
		t.Fatalf("the island's import() of a CommonJS chunk is not wrapped:\n%.3000s", code)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, "chunks")); err != nil {
		t.Fatal(err)
	}
}
