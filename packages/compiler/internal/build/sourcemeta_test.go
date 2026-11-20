package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

func TestRewriteSourceMeta(t *testing.T) {
	t.Parallel()
	const rel = "packages/lib/src/a.ts"
	u := `__krateMetaURL("` + rel + `")`
	f := `__krateMetaFile("` + rel + `")`
	d := `__krateMetaDir("` + rel + `")`
	cases := []struct{ name, in, want string }{
		{"url", `const u = import.meta.url;`, `const u = ` + u + `;`},
		{"filename", `x(import.meta.filename)`, `x(` + f + `)`},
		{"dirname", `x(import.meta.dirname)`, `x(` + d + `)`},
		{"spaced", "import . meta\n  .url", u + "\n"},
		{"free dirname", `path.join(__dirname, "x")`, `path.join(` + d + `, "x")`},
		{"free filename", `const f = __filename;`, `const f = ` + f + `;`},
		{"member", `a.__dirname + b?.__filename`, `a.__dirname + b?.__filename`},
		{"object key", `const o = { __dirname: 1, b: 2 };`, `const o = { __dirname: 1, b: 2 };`},
		{"shorthand", `const o = { a, __dirname };`, `const o = { a, __dirname: ` + d + ` };`},
		{"shorthand first", `f({ __filename })`, `f({ __filename: ` + f + ` })`},
		{"spread", `f(...__dirname)`, `f(...` + d + `)`},
		{"ternary", `a ? __dirname : __filename`, `a ? ` + d + ` : ` + f},
		{"string", `const s = "import.meta.url __dirname";`, `const s = "import.meta.url __dirname";`},
		{"single quote", `const s = 'import.meta.url';`, `const s = 'import.meta.url';`},
		{"line comment", "// import.meta.url __dirname\nx", "// import.meta.url __dirname\nx"},
		{"block comment", "/* __dirname */ x", "/* __dirname */ x"},
		{"regex", `const r = /import.meta.url/;`, `const r = /import.meta.url/;`},
		{"template text", "const t = `import.meta.url __dirname`;", "const t = `import.meta.url __dirname`;"},
		{"template substitution", "const t = `${import.meta.url}/${__dirname}`;", "const t = `${" + u + "}/${" + d + "}`;"},
		{"env", `if (import.meta.env.DEV) {}`, `if (import.meta.env.DEV) {}`},
		{"resolve", `import.meta.resolve("x"); import.meta.hot; import.meta.main`, `import.meta.resolve("x"); import.meta.hot; import.meta.main`},
		{"bare", `const m = import.meta;`, `const m = import.meta;`},
		{"dynamic import", `await import("x")`, `await import("x")`},
		{"longer name", `const a = import.meta.urlish + __dirnames + my__dirname;`, `const a = import.meta.urlish + __dirnames + my__dirname;`},
		{"declared const", `const __dirname = dirname(fileURLToPath(import.meta.url)); use(__dirname);`, `const __dirname = dirname(fileURLToPath(` + u + `)); use(__dirname);`},
		{"declared function", `function __filename() {} use(__filename)`, `function __filename() {} use(__filename)`},
		{"assigned", `__dirname = "x"; use(__dirname)`, `__dirname = "x"; use(__dirname)`},
		{"declared destructuring", `const { __dirname } = ctx; use(__dirname)`, `const { __dirname } = ctx; use(__dirname)`},
		{"declared param", `function f(__dirname) { return __dirname }`, `function f(__dirname) { return __dirname }`},
		{"declared arrow param", `const f = (a, __filename) => __filename;`, `const f = (a, __filename) => __filename;`},
		{"other name rewritten", `const __filename = 1; use(__dirname)`, `const __filename = 1; use(` + d + `)`},
		{"call is not a param", `if (x) { f(__dirname) }`, `if (x) { f(` + d + `) }`},
		{"typeof", `typeof __dirname !== "undefined"`, `typeof ` + d + ` !== "undefined"`},
		{"keeps lines", "a(import.meta\n.url,\n__dirname)", "a(" + u + "\n,\n" + d + ")"},
	}
	for _, c := range cases {
		got, changed := rewriteSourceMeta(c.in, rel, false)
		if got != c.want {
			t.Errorf("%s:\n in:   %s\n got:  %s\n want: %s", c.name, c.in, got, c.want)
		}
		if changed != (got != c.in) {
			t.Errorf("%s: changed = %v for %q", c.name, changed, got)
		}
		if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
			t.Errorf("%s: line count changed", c.name)
		}
	}
}

func TestRewriteSourceMetaTypeof(t *testing.T) {
	t.Parallel()
	in := "type T = typeof __dirname; const x: typeof __filename = 1; if (typeof __dirname === 'string') {}"
	got, _ := rewriteSourceMeta(in, "a.ts", true)
	want := "type T = typeof __dirname; const x: typeof __filename = 1; if (typeof __krateMetaDir(\"a.ts\") === 'string') {}"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestSourceMetaRelEscaping(t *testing.T) {
	t.Parallel()
	sm := newSourceMeta(t.TempDir(), "")
	got, ok := sm.relPath(sm.srcRoot + "/pkg/a b/[id]/x%y#z?.ts")
	if !ok || got != "pkg/a%20b/%5Bid%5D/x%25y%23z%3F.ts" {
		t.Errorf("relPath = %q, %v", got, ok)
	}
	out, _ := rewriteSourceMeta("import.meta.url", got, false)
	if out != `__krateMetaURL("pkg/a%20b/%5Bid%5D/x%25y%23z%3F.ts")` {
		t.Errorf("rewrite = %s", out)
	}
	up, ok := sm.relPath(sm.srcRoot + "/../sib/f.ts")
	if !ok || up != "../sib/f.ts" {
		t.Errorf("outside source root: %q, %v", up, ok)
	}
}

func TestMetaStaticTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sm := newSourceMeta(root, "")
	sm.collectTargets()
	src := `
const a = new URL("../data/x.txt", __krateMetaURL("packages/lib/src/meta.ts"));
const b = path.join(__krateMetaDir("packages/lib/src/meta.ts"), '..', 'data', "y.txt");
const c = path.resolve(path.dirname(fileURLToPath(__krateMetaURL("packages/lib/src/meta.ts"))), "../z");
const d = join(path.dirname(__krateMetaFile("packages/lib/src/meta.ts")), "w");
const e = path.join(__krateMetaDir("packages/lib/src/meta.ts"), name);
const f = new URL("https://x.test/a", __krateMetaURL("a.ts"));
`
	sm.recordTargets(src)
	var got []string
	for _, p := range sm.targetPaths() {
		got = append(got, strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), strings.ReplaceAll(sm.srcRoot, "\\", "/")+"/"))
	}
	want := []string{"packages/lib/data/x.txt", "packages/lib/data/y.txt", "packages/lib/src/w", "packages/lib/z"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("targets = %v, want %v", got, want)
	}
}

func TestSourceMetaBannerStrippedOnRebundle(t *testing.T) {
	t.Parallel()
	src := "export const a = 1;\n" + metaBanner + "\nconsole.log(__krateMetaURL(\"x.ts\"));\n"
	sm := newSourceMeta(t.TempDir(), "")
	out, ok := sm.rewrite(sm.srcRoot+"/b.mjs", src)
	if !ok || strings.Contains(out, "krate-meta") || !strings.Contains(out, `__krateMetaURL("x.ts")`) || strings.Count(out, "\n") != strings.Count(src, "\n") {
		t.Errorf("banner not stripped cleanly: %q", out)
	}
	stub := newSourceMeta(t.TempDir(), "")
	stub.mode = metaModeStub
	out, ok = stub.rewrite(stub.srcRoot+"/b.mjs", src)
	if !ok || !strings.Contains(out, "__krateMetaCwd") || strings.Contains(out, "import.meta") {
		t.Errorf("stub banner not substituted: %q", out)
	}
}

func TestSourceMetaPatchesOutputs(t *testing.T) {
	t.Parallel()
	for _, write := range []bool{true, false} {
		for _, existing := range []bool{false, true} {
			dir := t.TempDir()
			src := filepath.Join(dir, "src", "a.ts")
			if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(src, []byte("export const u = import.meta.url;\nexport const d = __dirname;\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "dist", ".krate", "api-node", "deep", "a.js")
			opts := api.BuildOptions{
				EntryPoints: []string{src}, Outfile: out, Bundle: true, Write: write, LogLevel: api.LogLevelSilent,
				Format: api.FormatESModule, Platform: api.PlatformNode, Sourcemap: api.SourceMapExternal,
			}
			if existing {
				jseval.ApplyNodeESM(&opts)
			}
			withSourceMeta(&opts, dir, "")
			res := esbuildBuild(opts)
			if len(res.Errors) > 0 {
				t.Fatalf("write=%v: %v", write, res.Errors)
			}
			var code, mapJSON string
			for _, f := range res.OutputFiles {
				switch filepath.Ext(f.Path) {
				case ".js":
					code = string(f.Contents)
				case ".map":
					mapJSON = string(f.Contents)
				}
			}
			if strings.Contains(code, metaPlaceholder) || !strings.Contains(code, `new URL("../../../../", import.meta.url)`) {
				t.Errorf("write=%v existing=%v: root not patched:\n%s", write, existing, code)
			}
			if write {
				disk, err := os.ReadFile(out)
				if err != nil || string(disk) != code {
					t.Errorf("write=%v: file on disk differs from the result (%v)", write, err)
				}
			}
			lines := strings.Split(code, "\n")
			use := -1
			for i, l := range lines {
				if strings.Contains(l, `__krateMetaURL("src/a.ts")`) {
					use = i
					break
				}
			}
			var m struct {
				Mappings string `json:"mappings"`
			}
			if err := json.Unmarshal([]byte(mapJSON), &m); err != nil {
				t.Fatalf("source map: %v", err)
			}
			if lead := len(m.Mappings) - len(strings.TrimLeft(m.Mappings, ";")); use < 0 || lead != use {
				t.Errorf("write=%v existing=%v: first mapped line %d, code line %d:\n%s", write, existing, lead, use, code)
			}
		}
	}
}

func TestSourceMetaSkipsOtherBuilds(t *testing.T) {
	t.Parallel()
	for _, o := range []api.BuildOptions{
		{Format: api.FormatIIFE, Platform: api.PlatformNode},
		{Format: api.FormatESModule, Platform: api.PlatformBrowser},
	} {
		opts := o
		if withSourceMeta(&opts, t.TempDir(), "") != nil || len(opts.Plugins) != 0 || opts.Banner != nil {
			t.Errorf("%v/%v build was changed", o.Format, o.Platform)
		}
	}
}
