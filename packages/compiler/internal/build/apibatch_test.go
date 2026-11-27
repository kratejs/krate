package build

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// readArtifactTree reads every file under dir, by slash path, with root normalized.
func readArtifactTree(t *testing.T, dir, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = normalizeRoot(data, root)
		return nil
	})
	return out
}

// relDeps is a route's recorded dependencies from root on, sorted. The paths
// esbuild prints are relative to the working directory, not to root, so a
// dependency is cut after root's base name.
func relDeps(b *Builder, root, route string) []string {
	var out []string
	marker := "/" + filepath.Base(root) + "/"
	for _, d := range b.pageDeps[route] {
		d = filepath.ToSlash(d)
		if i := strings.LastIndex(d, marker); i >= 0 {
			d = d[i+len(marker):]
		}
		out = append(out, d)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Routes compiled together in batches get the artifacts, metadata and dev
// dependencies they get compiled one at a time, nested routes included.
func TestAPIRouteBatchMatchesSeparateBuilds(t *testing.T) {
	t.Parallel()
	for _, runtime := range []string{"", "node"} {
		for _, sourcemap := range []bool{false, true} {
			sepRoot, batRoot := t.TempDir(), t.TempDir()
			sb := nodeOnlyProject(t, sepRoot)
			sb.Cfg.SSR.APIRuntime, sb.Cfg.Sourcemap = runtime, sourcemap
			for _, r := range nodeOnlyRoutes(sepRoot) {
				if err := sb.CompileAPIRoutes([]string{r}); err != nil {
					t.Fatal(err)
				}
			}
			bb := nodeOnlyProject(t, batRoot)
			bb.Cfg.SSR.APIRuntime, bb.Cfg.Sourcemap = runtime, sourcemap
			// Linking splits shared modules out of the batched bundles.
			bb.noLinkedNodeRoutes = true
			routes := nodeOnlyRoutes(batRoot)

			// Every route is served by the batches.
			apiSrcDir := filepath.Join(batRoot, "src", "api")
			plans := bb.planAPIRoutes(routes, apiSrcDir, &apiRouteFamily{}, newWorkerPool(4))
			for _, r := range routes {
				p := plans[r]
				name := filepath.Base(r)
				if p == nil {
					t.Fatalf("%s: no plan", name)
				}
				if runtime == "" && !p.detected {
					t.Errorf("runtime %q: %s: the batched detection did not decide it", runtime, name)
				}
				if wantNode := runtime == "node" || p.nodeOnly; wantNode != (p.node != nil) {
					t.Errorf("runtime %q: %s: batched Node bundle = %v, want %v", runtime, name, p.node != nil, wantNode)
				}
				if wantQuickJS := runtime == "" && !p.nodeOnly; wantQuickJS != (p.quickjs != nil) {
					t.Errorf("runtime %q: %s: batched QuickJS bundle = %v, want %v", runtime, name, p.quickjs != nil, wantQuickJS)
				}
			}

			if err := bb.CompileAPIRoutes(routes); err != nil {
				t.Fatal(err)
			}
			label := "runtime " + runtime
			if sourcemap {
				label += " with source maps"
			}
			if s, g := readRouteMetas(t, sepRoot), readRouteMetas(t, batRoot); !reflect.DeepEqual(s, g) {
				t.Errorf("%s: route metas differ:\nseparate %+v\nbatched  %+v", label, s, g)
			}
			for _, dir := range []func(string) string{apiBundleDir, apiNodeBundleDir} {
				s := readArtifactTree(t, dir(sb.Cfg.OutDir), sepRoot)
				g := readArtifactTree(t, dir(bb.Cfg.OutDir), batRoot)
				if !slices.Equal(slices.Sorted(maps.Keys(s)), slices.Sorted(maps.Keys(g))) {
					t.Errorf("%s: %s files: separate %v, batched %v", label, filepath.Base(dir("")), slices.Sorted(maps.Keys(s)), slices.Sorted(maps.Keys(g)))
					continue
				}
				for name := range s {
					if !bytes.Equal(s[name], g[name]) {
						t.Errorf("%s: %s/%s differs:\nseparate:\n%s\nbatched:\n%s", label, filepath.Base(dir("")), name, s[name], g[name])
					}
				}
			}
			for i, r := range routes {
				sr := nodeOnlyRoutes(sepRoot)[i]
				if s, g := relDeps(sb, sepRoot, sr), relDeps(bb, batRoot, r); !slices.Equal(s, g) {
					t.Errorf("%s: %s deps: separate %v, batched %v", label, filepath.Base(r), s, g)
				}
			}
			if t.Failed() {
				return
			}
		}
	}
}

// A route that does not build fails only itself: the batch falls back to
// building its routes one at a time, the error names the broken route, and
// the other routes still get their artifacts.
func TestAPIRouteBatchFallsBackOnBrokenRoute(t *testing.T) {
	t.Parallel()
	for _, runtime := range []string{"", "node"} {
		root := t.TempDir()
		b := nodeOnlyProject(t, root)
		b.Cfg.SSR.APIRuntime = runtime
		writeTree(t, root, map[string]string{
			"src/api/users/[id].ts":  "import { x } from \"./missing\";\nimport { hostname } from \"node:os\";\nexport function GET() { return Response.json({ x, h: hostname() }); }",
			"src/api/users/index.ts": "import { hostname } from \"node:os\";\nexport function GET() { return new Response(hostname()); }",
		})
		routes := nodeOnlyRoutes(root)
		err := b.CompileAPIRoutes(routes)
		if err == nil || !strings.Contains(err.Error(), "./missing") {
			t.Fatalf("runtime %q: err = %v", runtime, err)
		}
		qdir, ndir := apiBundleDir(b.Cfg.OutDir), apiNodeBundleDir(b.Cfg.OutDir)
		want := map[string]bool{"users/index.js": true, "crypto.js": true, "db.js": true, "items/special.js": true}
		if runtime == "node" {
			want["plain.js"], want["items/[id].js"] = true, true
		}
		for name := range want {
			if !artifactExists(t, ndir, filepath.FromSlash(name)) {
				t.Errorf("runtime %q: %s: no Node bundle next to a broken route", runtime, name)
			}
		}
		if runtime == "" && !artifactExists(t, qdir, filepath.FromSlash("items/[id].js")) {
			t.Errorf("items/[id].js: no QuickJS bundle next to a broken route")
		}
		if artifactExists(t, ndir, filepath.FromSlash("users/[id].js")) {
			t.Errorf("runtime %q: the broken route got a Node bundle", runtime)
		}
	}
}

func TestSplitBatches(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		n, limit int
		want     []int
	}{
		{0, 24, nil},
		{1, 24, []int{1}},
		{24, 24, []int{24}},
		{25, 24, []int{13, 12}},
		{98, 24, []int{20, 20, 20, 20, 18}},
	} {
		var got []int
		for _, part := range splitBatches(make([]int, c.n), c.limit) {
			got = append(got, len(part))
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("splitBatches(%d, %d) = %v, want %v", c.n, c.limit, got, c.want)
		}
	}
}

// esbuild links each entry of an unsplit batch on its own, so a module one
// entry loads lazily (which wraps it) is not wrapped for the others, and each
// entry keeps the code and imports of a separate build.
func TestFileBatchWrapsPerEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"lib/shared.ts": "import { hostname } from \"node:os\";\nexport const used = () => 1;\nexport const unused = () => hostname();\n",
		"lib/other.ts":  "export const o = 1;\nexport const p = 2;\n",
		"a.ts":          "import { used } from \"./lib/shared\";\nimport { o } from \"./lib/other\";\nexport const GET = () => used() + o;\n",
		"b.ts":          "export const GET = async () => (await import(\"./lib/shared\")).used();\n",
		"c.ts":          "const s = require(\"./lib/other\");\nexport const GET = () => s.p;\n",
		"d/e.ts":        "import { used } from \"../lib/shared\";\nexport const GET = used;\n",
	})
	for _, minify := range []bool{false, true} {
		base := api.BuildOptions{Bundle: true, Platform: api.PlatformNode, Format: api.FormatESModule, Metafile: true, AbsWorkingDir: root, MinifyIdentifiers: minify, MinifySyntax: minify, MinifyWhitespace: minify, Sourcemap: api.SourceMapLinked}
		var entries []fileBatchEntry
		for _, n := range []string{"a", "b", "c", "d/e"} {
			entries = append(entries, fileBatchEntry{In: filepath.Join(root, n+".ts"), Out: filepath.Join(root, "out", n+".js")})
		}
		outs, _ := esbuildBatchFiles(base, filepath.Join(root, "out"), entries)
		if outs == nil {
			t.Fatal("batch failed")
		}
		for i, e := range entries {
			o := base
			o.EntryPoints = []string{e.In}
			o.Outfile = e.Out
			o.Write = false
			one := esbuildBuild(o)
			if len(one.Errors) > 0 || len(outs[i].Files) != len(one.OutputFiles) {
				t.Fatalf("%s: files %d, separate %d (%v)", e.In, len(outs[i].Files), len(one.OutputFiles), one.Errors)
			}
			got := map[string][]byte{}
			for _, f := range outs[i].Files {
				got[f.Path] = f.Contents
			}
			for _, f := range one.OutputFiles {
				if g, ok := got[f.Path]; !ok || !bytes.Equal(g, f.Contents) {
					t.Errorf("minify %v: %s differs:\nbatch:\n%s\nseparate:\n%s", minify, f.Path, g, f.Contents)
				}
			}
			if s, g := relDepsOf(root, one.Metafile), relDepsOf(root, outs[i].Metafile); !slices.Equal(s, g) {
				t.Errorf("%s: inputs %v, separate %v", e.In, g, s)
			}
			if s, g := outputImports(one.Metafile), outputImports(outs[i].Metafile); !reflect.DeepEqual(s, g) {
				t.Errorf("%s: kept imports %v, separate %v", e.In, g, s)
			}
		}
		if !minify {
			js := func(o *batchOutput) string {
				for _, f := range o.Files {
					if strings.HasSuffix(f.Path, ".js") {
						return string(f.Contents)
					}
				}
				return ""
			}
			if a, b := js(outs[0]), js(outs[1]); strings.Contains(a, "__esm") || !strings.Contains(b, "__esm") {
				t.Errorf("want shared wrapped only for b:\na:\n%s\nb:\n%s", a, b)
			}
		}
	}
}

// relDepsOf is a metafile's inputs relative to root, sorted.
func relDepsOf(root, metafile string) []string {
	var out []string
	for _, d := range metafileDeps(root, metafile) {
		rel, _ := filepath.Rel(root, d)
		out = append(out, filepath.ToSlash(rel))
	}
	slices.Sort(out)
	return out
}

// A route whose package does not bundle passes the detection but fails its
// QuickJS batch: the batch's routes build on their own, the error names the
// route, and the others get the bundles separate builds give them.
func TestAPIRouteQuickJSBatchFallsBack(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"src/api/a.ts":                        `export function GET() { return Response.json({ a: 1 }); }`,
		"src/api/users/[id].ts":               "import { h } from \"../../lib/h\";\nexport function GET() { return Response.json({ h: h() }); }",
		"src/lib/h.ts":                        "export const h = () => 2;\n",
		"src/api/bad.ts":                      "import { v } from \"brokenpkg\";\nexport function GET() { return Response.json({ v }); }",
		"node_modules/brokenpkg/package.json": `{"name":"brokenpkg","version":"1.0.0","main":"index.js"}`,
		"node_modules/brokenpkg/index.js":     "export { v } from \"./missing.js\";\n",
	}
	var trees [2]map[string][]byte
	for k, batched := range []bool{true, false} {
		root := t.TempDir()
		writeTree(t, root, files)
		cfg := config.Default()
		cfg.OutDir = filepath.Join(root, "dist")
		b := New(root, cfg)
		routes := nodeOnlyRoutes(root)
		var err error
		if batched {
			plans := b.planAPIRoutes(routes, filepath.Join(root, "src", "api"), &apiRouteFamily{}, newWorkerPool(2))
			for _, r := range routes {
				if p := plans[r]; !p.detected || p.nodeOnly || p.quickjs != nil {
					t.Errorf("%s: plan %+v, want detected, not Node-only, no QuickJS bundle", filepath.Base(r), *p)
				}
			}
			err = b.CompileAPIRoutes(routes)
		} else {
			for _, r := range routes {
				if e := b.CompileAPIRoutes([]string{r}); e != nil {
					err = e
				}
			}
		}
		if err == nil || !strings.Contains(err.Error(), "missing.js") {
			t.Fatalf("batched %v: err = %v", batched, err)
		}
		trees[k] = readArtifactTree(t, apiBundleDir(cfg.OutDir), root)
	}
	if !slices.Equal(slices.Sorted(maps.Keys(trees[0])), []string{"a.js", "users/[id].js"}) {
		t.Fatalf("batched QuickJS bundles %v", slices.Sorted(maps.Keys(trees[0])))
	}
	for name, data := range trees[1] {
		if !bytes.Equal(trees[0][name], data) {
			t.Errorf("%s differs:\nbatched:\n%s\nseparate:\n%s", name, trees[0][name], data)
		}
	}
}
