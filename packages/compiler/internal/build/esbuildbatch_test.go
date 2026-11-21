package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// For entries whose names do not collide and that load nothing lazily, a
// batched build prints each entry as a separate Stdin build would, and gives
// it the same metafile inputs: the client chunk checks scan these outputs.
func TestEsbuildBatchMatchesSeparateBuilds(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
