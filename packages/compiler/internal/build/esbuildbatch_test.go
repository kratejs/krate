package build

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// For entries whose names do not collide and that load nothing lazily, a
// batched build prints each entry as a separate Stdin build would, and gives
// it the same metafile inputs: the client chunk checks scan these outputs.
func TestEsbuildBatchMatchesSeparateBuilds(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("lib/shared.js", "export const shared = (x) => x * 2;\nexport const unused = 1;\n")
	write("ui/a.js", "import { shared } from '../lib/shared.js';\nexport function A() { return shared(1); }\n")
	write("ui/b.js", "import { shared } from '../lib/shared.js';\nexport function B() { return shared(2) + 1; }\n")
	write("other/c.js", "export default function C() { return 'c'; }\n")

	entries := []batchEntry{
		{Source: "import * as __m from \"./a.js\";\nexport const m = __m;\n", ResolveDir: filepath.Join(root, "ui"), Sourcefile: "krate-client-ssr.js", Loader: api.LoaderJS},
		{Source: "import * as __m from \"./b.js\";\nexport const m = __m;\n", ResolveDir: filepath.Join(root, "ui"), Sourcefile: "krate-client-ssr.js", Loader: api.LoaderJS},
		{Source: "import C from \"./c.js\";\nexport const m = C;\n", ResolveDir: filepath.Join(root, "other"), Sourcefile: "krate-client-ssr.js", Loader: api.LoaderJS},
	}
	for _, minify := range []bool{false, true} {
		base := api.BuildOptions{
			AbsWorkingDir:     root,
			Bundle:            true,
			Format:            api.FormatESModule,
			Platform:          api.PlatformNode,
			MinifyWhitespace:  minify,
			MinifySyntax:      minify,
			MinifyIdentifiers: minify,
			LogLevel:          api.LogLevelSilent,
			Metafile:          true,
		}
		batch := base
		batch.Outdir = filepath.Join(root, "out")
		outs, res := esbuildBatch(batch, entries)
		if len(res.Errors) > 0 {
			t.Fatalf("batch errors: %v", res.Errors)
		}
		for i, e := range entries {
			one := base
			one.Stdin = &api.StdinOptions{Contents: e.Source, ResolveDir: e.ResolveDir, Sourcefile: e.Sourcefile, Loader: e.Loader}
			r := esbuildBuild(one)
			if len(r.Errors) > 0 || len(r.OutputFiles) != 1 {
				t.Fatalf("separate build %d: %v", i, r.Errors)
			}
			if len(outs[i].Files) != 1 {
				t.Fatalf("entry %d: %d batch outputs, want 1", i, len(outs[i].Files))
			}
			if got, want := string(outs[i].Files[0].Contents), string(r.OutputFiles[0].Contents); got != want {
				t.Errorf("minify=%v entry %d differs from its separate build:\n--- batch\n%s\n--- separate\n%s", minify, i, got, want)
			}
			// The entry's metafile names only the files it reaches.
			got := metafileInputs(root, outs[i].Metafile)
			want := metafileInputs(root, r.Metafile)
			for p := range want {
				if !got[p] {
					t.Errorf("entry %d: batch metafile misses input %s", i, p)
				}
			}
			for p := range got {
				if !want[p] {
					t.Errorf("entry %d: batch metafile has extra input %s", i, p)
				}
			}
		}
	}
}

// A module one entry loads with require() is wrapped for every entry of a
// batch, which changes how another entry that imports it is printed: the
// batch flags that entry, and only it, so it is built on its own.
func TestEsbuildBatchFlagsEntriesWrappedByOthers(t *testing.T) {
	root := t.TempDir()
	for name, src := range map[string]string{
		"m.js":    "export const used = () => 'used-mark';\nexport const unused = () => 'unused-mark';\n",
		"lazy.js": "export function load() { return require('./m.js').used(); }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	entries := []batchEntry{
		{Source: "import { load } from './lazy.js';\nexport const a = load;\n", ResolveDir: root, Sourcefile: "a.js", Loader: api.LoaderJS},
		{Source: "import { used } from './m.js';\nexport const b = used;\n", ResolveDir: root, Sourcefile: "b.js", Loader: api.LoaderJS},
	}
	opts := api.BuildOptions{AbsWorkingDir: root, Bundle: true, Format: api.FormatESModule, Platform: api.PlatformBrowser, LogLevel: api.LogLevelSilent, Outdir: filepath.Join(root, "out")}
	outs, res := esbuildBatch(opts, entries)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	if outs[0].Unsafe {
		t.Error("the entry that requires the module itself is printed as on its own, but was flagged")
	}
	if !outs[1].Unsafe {
		t.Error("the entry that imports a module another entry requires was not flagged")
	}
	one := opts
	one.Outdir = ""
	e := entries[1]
	one.Stdin = &api.StdinOptions{Contents: e.Source, ResolveDir: e.ResolveDir, Sourcefile: e.Sourcefile, Loader: e.Loader}
	single := esbuildBuild(one)
	if len(single.Errors) > 0 {
		t.Fatal(single.Errors)
	}
	if string(single.OutputFiles[0].Contents) == string(outs[1].Files[0].Contents) {
		t.Log("esbuild printed the flagged entry the same way; the check stays conservative")
	}
}

// A pool entry stands in for Stdin: its output matches a Stdin build byte for
// byte, collisions of top-level names included, and plugins see its imports
// from the same importer and namespace.
func TestEsbuildPoolMatchesStdinBuilds(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"a", "b", "c"} {
		write("lib/"+n+".js", "import { useState } from 'ext';\nfunction helper() { return useState('"+n+"'); }\nexport const "+n+" = () => helper();\n")
	}
	entries := []poolEntry{
		{Source: "import { a } from './a.js';\nimport { b } from './b.js';\nimport { useState } from 'ext';\nexport const m = [a, b, useState];\n", ResolveDir: filepath.Join(root, "lib"), Sourcefile: "krate-client-ssr.js", Loader: api.LoaderJS},
		{Source: "import { c } from './c.js';\nexport const m = [c];\n", ResolveDir: filepath.Join(root, "lib"), Sourcefile: "krate-client-ssr.js", Loader: api.LoaderJS},
	}
	type seen struct{ importer, namespace string }
	var mu sync.Mutex
	recorded := map[string][]seen{}
	recorder := func(tag string) api.Plugin {
		return api.Plugin{Name: "rec", Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^\./`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				mu.Lock()
				recorded[tag] = append(recorded[tag], seen{args.Importer, args.Namespace})
				mu.Unlock()
				return api.OnResolveResult{}, nil
			})
		}}
	}
	base := api.BuildOptions{
		AbsWorkingDir: root, Bundle: true, Format: api.FormatESModule, Platform: api.PlatformNode,
		External: []string{"ext"}, LogLevel: api.LogLevelSilent, Outfile: filepath.Join(root, "out", "x.mjs"),
	}
	for i, e := range entries {
		one := base
		one.Plugins = []api.Plugin{recorder("stdin")}
		one.Stdin = &api.StdinOptions{Contents: e.Source, ResolveDir: e.ResolveDir, Sourcefile: e.Sourcefile, Loader: e.Loader}
		want := esbuildBuild(one)
		pool := base
		pool.Plugins = []api.Plugin{recorder("pool")}
		var got api.BuildResult
		<-esbuildPoolStream("test", pool, e.Sourcefile, 1, func() (poolEntry, int, bool) {
			if got.OutputFiles != nil || len(got.Errors) > 0 {
				return poolEntry{}, 0, false
			}
			got.Errors = []api.Message{{}} // handed out
			return e, 0, true
		}, func(_ int, res api.BuildResult) { got = res })
		if len(want.Errors) > 0 || len(got.Errors) > 0 {
			t.Fatalf("entry %d: errors %v / %v", i, want.Errors, got.Errors)
		}
		if g, w := string(got.OutputFiles[0].Contents), string(want.OutputFiles[0].Contents); g != w {
			t.Errorf("entry %d differs from its Stdin build:\n--- pool\n%s\n--- stdin\n%s", i, g, w)
		}
	}
	if len(recorded["pool"]) != len(recorded["stdin"]) {
		t.Fatalf("plugins saw %d imports in the pool and %d with Stdin", len(recorded["pool"]), len(recorded["stdin"]))
	}
	for i := range recorded["stdin"] {
		if recorded["pool"][i] != recorded["stdin"][i] {
			t.Errorf("import %d: pool importer %+v, Stdin importer %+v", i, recorded["pool"][i], recorded["stdin"][i])
		}
	}
}
