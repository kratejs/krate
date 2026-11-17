package build

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// The page server bundles of a run share their package resolutions and the
// sources their plugins derive from project files, and each bundle is the one
// the page gets when it is built alone. The pages import packages from several
// importers in one directory and from several directories, a workspace
// package that is bundled, a client boundary, and an async layout with an
// inline action.
func TestServerBundlesSharedMatchAlone(t *testing.T) {
	page := func(rel, imports, jsx string) string {
		return imports + `import { tag } from "fake-lib";
import * as lib from "fake-lib";
import { util } from "@ws/util";
import Counter from "` + rel + `components/Counter";
import { Shared } from "` + rel + `components/Shared";
export default async function Page() {
  const start = await Promise.resolve(1);
  return <main>{tag}{lib.other}{util()}<Shared /><Counter start={start} label="n" />` + jsx + `</main>;
}`
	}
	files := map[string]string{
		"node_modules/fake-lib/package.json": `{"name":"fake-lib","version":"1.0.0","main":"index.js"}`,
		"node_modules/fake-lib/index.js":     `exports.tag = "lib-tag"; exports.other = "lib-other";`,
		"packages/util/package.json":         `{"name":"@ws/util","version":"0.0.0","type":"module","main":"src/index.ts"}`,
		"packages/util/src/index.ts":         `import { tag } from "fake-lib"; export function util(): string { return "util-" + tag; }`,
		"src/components/Counter.tsx":         counterClientSrc,
		"src/components/Shared.tsx": `import { tag } from "fake-lib";
import { util } from "@ws/util";
export function Shared() { return <p>{tag}{util()}</p>; }`,
		"src/pages/_layout.tsx": `import { tag } from "fake-lib";
export default async function Layout({ children }: any) {
  const who = await Promise.resolve("ada");
  async function signOut() {
    "use server";
    return { out: who };
  }
  return <div><form action={signOut}><button>{tag}</button></form>{children}</div>;
}`,
		"src/pages/index.tsx": page("../", "", ""),
		"src/pages/a/b.tsx":   page("../../", "", ""),
		"src/pages/a/c.tsx":   page("../../", "", ""),
		"src/pages/d/e/f.tsx": page("../../../", "import { Alt } from \"../../../alt/Alt\";\n", "<Alt />"),
		// src/alt has its own copy of @ws/util, so its import must not share
		// the resolution of the other directories.
		"packages/util2/package.json": `{"name":"@ws/util","version":"0.0.1","type":"module","main":"src/index.ts"}`,
		"packages/util2/src/index.ts": `export function util(): string { return "util2-alt"; }`,
		"src/alt/Alt.tsx": `import { util } from "@ws/util";
export function Alt() { return <i>{util()}</i>; }`,
	}
	b, root := scProject(t, files)
	withPreact(t, root)
	for link, target := range map[string]string{"node_modules/@ws/util": "packages/util", "src/alt/node_modules/@ws/util": "packages/util2"} {
		link = filepath.Join(root, filepath.FromSlash(link))
		if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, filepath.FromSlash(target)), link); err != nil {
			t.Fatal(err)
		}
	}
	bundles := func() map[string][]byte {
		t.Helper()
		if err := New(root, b.Cfg).BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		dir := filepath.Join(b.Cfg.OutDir, ".krate", "server-bundles")
		out := map[string][]byte{}
		for name, data := range readArtifactTree(t, dir, root) {
			if filepath.Dir(name) != "client" {
				out[name] = data
			}
		}
		return out
	}
	shared := bundles()
	shareServerBundleWork = false
	alone := bundles()
	shareServerBundleWork = true

	names := slices.Sorted(maps.Keys(shared))
	if len(names) != 4 {
		t.Fatalf("want a server bundle for each of the 4 pages, got %v", names)
	}
	if got := slices.Sorted(maps.Keys(alone)); !slices.Equal(names, got) {
		t.Fatalf("bundles: shared %v, alone %v", names, got)
	}
	if f := shared["d/e/f.server.mjs"]; !bytes.Contains(f, []byte("util2-alt")) {
		t.Errorf("d/e/f.server.mjs does not bundle the copy of @ws/util in src/alt (bundles %v):\n%s", names, f)
	}
	for _, name := range names {
		if !bytes.Equal(shared[name], alone[name]) {
			t.Errorf("%s differs:\nshared:\n%s\nalone:\n%s", name, shared[name], alone[name])
		}
		for _, want := range []string{`from "fake-lib"`, `"util-"`, "clientBoundary", "__krateInlineAction"} {
			if !bytes.Contains(shared[name], []byte(want)) {
				t.Errorf("%s: missing %q, so the fixture does not exercise what it should", name, want)
			}
		}
	}
}

// A native memo resolves an import once for every importer in its directory,
// also when the builds ask for it at the same time, and keeps imports from
// different directories, kinds and attributes apart.
func TestNativeResolveMemoSharesByDirectory(t *testing.T) {
	memo := newNativeResolveMemo()
	var calls atomic.Int64
	resolve := func(args api.OnResolveArgs) resolveMemoValue {
		return memo.resolve(args, func() api.ResolveResult {
			calls.Add(1)
			return api.ResolveResult{Path: args.ResolveDir + "/" + args.Path, Namespace: "file"}
		})
	}
	base := api.OnResolveArgs{Path: "pkg", ResolveDir: "/p/src", Kind: api.ResolveJSImportStatement, Namespace: "file"}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := base
			args.Importer = filepath.Join("/p/src", string(rune('a'+i%8))+".ts")
			if v := resolve(args); v.path != "/p/src/pkg" {
				t.Errorf("resolved to %q", v.path)
			}
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("one directory resolved %d times, want once", n)
	}
	for _, change := range []func(*api.OnResolveArgs){
		func(a *api.OnResolveArgs) { a.ResolveDir = "/p/lib" },
		func(a *api.OnResolveArgs) { a.Kind = api.ResolveJSRequireCall },
		func(a *api.OnResolveArgs) { a.With = map[string]string{"type": "json"} },
		func(a *api.OnResolveArgs) { a.Namespace = "other" },
		func(a *api.OnResolveArgs) { a.Path = "pkg/sub" },
	} {
		args := base
		change(&args)
		before := calls.Load()
		resolve(args)
		if calls.Load() != before+1 {
			t.Errorf("%+v was answered from another import's resolution", args)
		}
	}

	// A memo that is not native keeps importers apart.
	plain := &resolveMemo{}
	calls.Store(0)
	for _, importer := range []string{"/p/src/a.ts", "/p/src/b.ts", "/p/src/a.ts"} {
		args := base
		args.Importer = importer
		plain.resolve(args, func() api.ResolveResult { calls.Add(1); return api.ResolveResult{} })
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("a plain memo resolved %d times for two importers, want 2", n)
	}
}

// The cached file identity and sources are the ones read without the cache.
func TestServerBundleFilesMatchUncached(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "src/a.tsx", "export function A() { return <Head><title>x</title></Head>; }\n")
	writeProjectFile(t, root, "src/client.tsx", "\"use client\";\nexport default function C() { return null; }\nexport const n = 1;\n")
	a, client := filepath.Join(root, "src", "a.tsx"), filepath.Join(root, "src", "client.tsx")
	link := filepath.Join(root, "link.tsx")
	if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	files := &serverBundleFiles{}
	var none *serverBundleFiles
	for range 2 {
		for _, pair := range [][2]string{{a, link}, {a, client}, {a, filepath.Join(root, "missing.tsx")}} {
			if got, want := files.sameFile(pair[0], pair[1]), none.sameFile(pair[0], pair[1]); got != want {
				t.Errorf("sameFile(%s, %s) = %v, want %v", pair[0], pair[1], got, want)
			}
		}
		for _, p := range []string{a, client} {
			got, gerr := files.goModuleSource(root, p)
			want, werr := none.goModuleSource(root, p)
			if got != want || (gerr == nil) != (werr == nil) {
				t.Errorf("goModuleSource(%s) = %q, %v; want %q, %v", p, got, gerr, want, werr)
			}
			if got, want := files.unusedClientStub(p), none.unusedClientStub(p); got != want {
				t.Errorf("unusedClientStub(%s) = %q, want %q", p, got, want)
			}
		}
	}
	if files.unusedClientStub(client) == "" || files.unusedClientStub(a) != "" {
		t.Error("only the client module gets a stub")
	}
}
