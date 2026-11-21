package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

var windowAssignRe = regexp.MustCompile(`Object\.assign\(window,\{([^}]*)\}\)`)

// windowNames returns the names a shaken runtime assigns to window.
func windowNames(t *testing.T, js string) []string {
	t.Helper()
	var names []string
	for _, m := range windowAssignRe.FindAllStringSubmatch(js, -1) {
		for _, pair := range strings.Split(m[1], ",") {
			name, _, _ := strings.Cut(pair, ":")
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		t.Fatal("the runtime assigns no names to window")
	}
	sort.Strings(names)
	return names
}

func shakenPlan(t *testing.T, refs map[string]bool, features []string, suspense, all bool) (shakenRuntimeInput, func(string) string) {
	t.Helper()
	root, m := loadFeatureManifest(t)
	if !runtimeModulesAvailable(&m) {
		t.Skip("krate-features.json lists no runtime modules (rebuild @krate/runtime)")
	}
	load := func(name string) string { return loadNamedBundleFromDisk(root, name) }
	in, err := planShakenRuntime(&m, "router", features, suspense, refs, all, load, renderer.HydrationBootstrapJS)
	if err != nil {
		t.Fatal(err)
	}
	return in, load
}

// TestShakenRuntimeExposesReferencedNames checks that the generated entry
// exposes the names page code references, what the bootstrap helpers it
// calls and the runtime itself read from window, and the router API, but
// not the other exports.
func TestShakenRuntimeExposesReferencedNames(t *testing.T) {
	t.Parallel()
	in, load := shakenPlan(t, jsRefs(`createSignal(0); kbindText("a", () => 1);`), nil, false, false)
	exposed := map[string]bool{}
	for _, name := range in.Expose {
		exposed[name] = true
	}
	for _, name := range []string{
		"createSignal",                  // page code
		"createEffect",                  // kbindText's body
		"kratePortalScan",               // the bootstrap's load-time scan
		"disposeAll", "krateKeepScopes", // read by the router through globalThis
		"krateRouter", "navigate", // public router API
	} {
		if !exposed[name] {
			t.Errorf("entry does not expose %s", name)
		}
	}
	for _, name := range []string{"createResource", "preload", "krateSpread", "kbindText", "twMerge", "useId"} {
		if exposed[name] {
			t.Errorf("entry exposes %s, which nothing references", name)
		}
	}
	entry := in.entry()
	for _, mod := range in.Modules {
		if !strings.Contains(entry, `"./`+mod+`"`) && !strings.Contains(in.baseEntry(), `"./`+mod+`"`) {
			t.Errorf("entry does not load %s", mod)
		}
	}
	js, err := bundleShakenRuntime(in, load)
	if err != nil {
		t.Fatal(err)
	}
	if got := windowNames(t, js); strings.Join(got, ",") != strings.Join(in.Expose, ",") {
		t.Errorf("bundle defines %v on window, want %v", got, in.Expose)
	}
	if strings.Contains(js, "__export") || strings.Count(js, "Object.defineProperty") > 4 {
		t.Errorf("bundle still carries the per-bundle export helpers")
	}
}

// TestShakenRuntimeUnknownCodeGetsEveryExport checks that a site with code
// the build cannot see (an island, window[name]) gets every export of the
// modules it ships.
func TestShakenRuntimeUnknownCodeGetsEveryExport(t *testing.T) {
	t.Parallel()
	_, m := loadFeatureManifest(t)
	in, _ := shakenPlan(t, map[string]bool{}, optionalRuntimeFeatures, true, true)
	want := map[string]bool{}
	for _, name := range m.Core.Exports {
		want[name] = true
	}
	for _, f := range append(append([]string{}, optionalRuntimeFeatures...), "router") {
		for _, name := range m.Feature[f].Exports {
			want[name] = true
		}
	}
	for _, name := range m.Suspense.Exports {
		want[name] = true
	}
	if len(in.Expose) != len(want) {
		t.Errorf("exposes %d names, want %d", len(in.Expose), len(want))
	}
	for _, name := range in.Expose {
		delete(want, name)
	}
	if len(want) > 0 {
		t.Errorf("does not expose %v", want)
	}

	for _, src := range []string{`window[name]()`, `globalThis[k] = 1`, `const m = await import(url)`} {
		u := runtimeUsageOf([]*PageResult{{HydrationJS: src, RuntimeRefs: recordRuntimeRefs(src, "", nil)}})
		if !u.Unknown {
			t.Errorf("%q: usage is not unknown", src)
		}
	}
	for _, src := range []string{`window["navigate"]("/")`, `window.x = data[k]`} {
		u := runtimeUsageOf([]*PageResult{{HydrationJS: src, RuntimeRefs: recordRuntimeRefs(src, "", nil)}})
		if u.Unknown {
			t.Errorf("%q: usage is unknown", src)
		}
	}
}

// TestShakenRuntimeSuspenseRunsLast checks that the Suspense module loads
// after the other modules have assigned their exports, which it reads from
// window when it loads.
func TestShakenRuntimeSuspenseRunsLast(t *testing.T) {
	t.Parallel()
	in, _ := shakenPlan(t, map[string]bool{"__krateBoundary": true}, []string{"actions"}, true, false)
	exposed := strings.Join(in.Expose, ",")
	for _, name := range []string{"setActionTransitionRunner", "__krateBoundary", "krateCondActivity"} {
		if !strings.Contains(","+exposed+",", ","+name+",") {
			t.Errorf("entry does not expose %s", name)
		}
	}
	entry := in.entry()
	if !strings.HasPrefix(entry, `import "./`+baseEntryPath+`"`) || !strings.Contains(entry, `"./suspense.js"`) || strings.Contains(in.baseEntry(), "suspense.js") {
		t.Errorf("suspense does not load after the base entry:\n%s", entry)
	}
}

// TestShakenRuntimeHashIsStable checks that the runtime chunk depends only on
// the references, not on their order or on an earlier build.
func TestShakenRuntimeHashIsStable(t *testing.T) {
	t.Parallel()
	_, m := loadFeatureManifest(t)
	if !runtimeModulesAvailable(&m) {
		t.Skip("krate-features.json lists no runtime modules (rebuild @krate/runtime)")
	}
	repoRoot, _ := repoRootPath()
	write := func(refs map[string]bool) string {
		resetRuntimeBundleMemo()
		rel, err := writeRuntimeChunk(t.TempDir(), true, repoRoot, runtimeChunkOptions{SPA: true, Trim: true, Usage: runtimeUsage{Refs: refs}})
		if err != nil {
			t.Fatal(err)
		}
		return rel
	}
	a := write(map[string]bool{"createSignal": true, "createMemo": true, "krateBindList": true})
	b := write(map[string]bool{"krateBindList": true, "createMemo": true, "createSignal": true})
	if a != b {
		t.Errorf("same references, different chunks: %s and %s", a, b)
	}
	if c := write(map[string]bool{"createSignal": true}); c == a {
		t.Errorf("different references, same chunk %s", c)
	}
}

// TestShakenRuntimeFallsBackToPrebuiltPieces checks that an older runtime
// package, whose manifest lists no modules, or one missing a module, gets
// the concatenated prebuilt pieces.
func TestShakenRuntimeFallsBackToPrebuiltPieces(t *testing.T) {
	t.Parallel()
	repoRoot, m := loadFeatureManifest(t)
	if !runtimeModulesAvailable(&m) {
		t.Skip("krate-features.json lists no runtime modules (rebuild @krate/runtime)")
	}
	src := filepath.Join(repoRoot, "packages", "runtime", "dist")
	for name, edit := range map[string]func(root string, m map[string]any){
		"no modules": func(_ string, m map[string]any) { delete(m, "esm") },
		"missing module": func(root string, _ map[string]any) {
			if err := os.Remove(filepath.Join(root, "packages", "runtime", "dist", "router.js")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dist := filepath.Join(root, "packages", "runtime", "dist")
			if err := os.MkdirAll(dist, 0755); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(src)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") && !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(src, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dist, e.Name()), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			var manifest map[string]any
			raw, _ := os.ReadFile(filepath.Join(dist, "krate-features.json"))
			if err := json.Unmarshal(raw, &manifest); err != nil {
				t.Fatal(err)
			}
			edit(root, manifest)
			raw, _ = json.Marshal(manifest)
			if err := os.WriteFile(filepath.Join(dist, "krate-features.json"), raw, 0644); err != nil {
				t.Fatal(err)
			}
			resetRuntimeBundleMemo()
			defer resetRuntimeBundleMemo()
			js, features, _ := loadRuntimeFeatures(root, runtimeChunkOptions{SPA: true, Usage: runtimeUsage{Refs: map[string]bool{"krateBindList": true}}})
			if !strings.Contains(js, "__krate_exports") {
				t.Errorf("runtime is not the prebuilt pieces")
			}
			if strings.Join(features, ",") != "list" {
				t.Errorf("features = %v, want [list]", features)
			}
		})
	}
}

func TestGlobalReads(t *testing.T) {
	t.Parallel()
	src := `const g = globalThis;
if (typeof g.disposeAll === 'function') g.disposeAll();
const target = (api ?? globalThis);
target.setRunner(1);
window.krateA(); self["krateB"]; globalThis?.krateC;
const other = globalThis.krateD; other.notGlobal;
obj.g.krateE;`
	got := globalReads(src)
	for _, name := range []string{"disposeAll", "setRunner", "krateA", "krateB", "krateC", "krateD"} {
		if !got[name] {
			t.Errorf("missing %s", name)
		}
	}
	for _, name := range []string{"notGlobal", "krateE"} {
		if got[name] {
			t.Errorf("%s is not a global read", name)
		}
	}
}

func TestScriptRefsFollowsUsedHelpers(t *testing.T) {
	t.Parallel()
	script := "window.kbindA=function(){kbindB();createSignal()};\n" +
		"window.kbindB=function(){createEffect()};\n" +
		"window.kbindC=function(){createMemo()};\n" +
		"krateLoadTime();\n"
	got := scriptRefs(map[string]bool{"kbindA": true}, script)
	for _, name := range []string{"createSignal", "createEffect", "krateLoadTime"} {
		if !got[name] {
			t.Errorf("missing %s", name)
		}
	}
	if got["createMemo"] {
		t.Error("an unused helper's references count")
	}
}

// buildShakeProject builds pages and public files with the production
// runtime and returns the runtime chunk.
func buildShakeProject(t *testing.T, files map[string]string, edit func(*config.Config)) string {
	t.Helper()
	repoRoot, m := loadFeatureManifest(t)
	if !runtimeModulesAvailable(&m) {
		t.Skip("krate-features.json lists no runtime modules (rebuild @krate/runtime)")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	for name, src := range files {
		writeProjectFile(t, root, name, src)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.PublicDir = filepath.Join(root, "public")
	cfg.OutDir = filepath.Join(root, "dist")
	if edit != nil {
		edit(cfg)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "runtime.*.js"))
	if len(matches) != 1 {
		t.Fatalf("runtime chunks: %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const shakeCounterPage = `import { useState } from "react";
export default function Page() {
	const [c, setC] = useState(0);
	return <main><button onClick={() => setC(c + 1)}>{c}</button><script src="/app.js"></script><script src="https://cdn.example.com/x.js"></script></main>;
}
`

// TestShakenRuntimeScansPublicScripts checks that a same-origin script from
// public/ counts like page code: its references are defined, and a computed
// global in it ships the full surface.
func TestShakenRuntimeScansPublicScripts(t *testing.T) {
	t.Parallel()
	plain := buildShakeProject(t, map[string]string{
		"src/pages/index.tsx": shakeCounterPage,
		"public/app.js":       `createResource(() => 1); // createRoot in a comment does not count`,
	}, nil)
	if !strings.Contains(plain, "createResource:") {
		t.Error("runtime does not define createResource, which public/app.js calls")
	}
	if strings.Contains(plain, "createRoot:") || strings.Contains(plain, "twMerge:") {
		t.Error("runtime defines names nothing calls")
	}
	dynamic := buildShakeProject(t, map[string]string{
		"src/pages/index.tsx": shakeCounterPage,
		"public/app.js":       `const name = "create" + "Resource"; window[name](() => 1);`,
	}, nil)
	if !strings.Contains(dynamic, "twMerge:") || !strings.Contains(dynamic, "createRoot:") {
		t.Error("a script with window[expr] did not get the full runtime")
	}
}

// TestClientRuntimeExportsAll checks the clientRuntime.exports opt-out.
func TestClientRuntimeExportsAll(t *testing.T) {
	t.Parallel()
	js := buildShakeProject(t, map[string]string{
		"src/pages/index.tsx": strings.Replace(shakeCounterPage, `<script src="/app.js"></script>`, "", 1),
	}, func(c *config.Config) { c.ClientRuntime.Exports = "all" })
	for _, name := range []string{"twMerge", "createRoot", "createResource", "krateHighlight"} {
		if !strings.Contains(js, name+":") {
			t.Errorf("exports: all does not define %s", name)
		}
	}
}

func TestScriptSitePath(t *testing.T) {
	t.Parallel()
	for _, c := range [][3]string{
		{"index.html", "/app.js?v=1", "/app.js"},
		{"docs/a/index.html", "lib/x.js#h", "/docs/a/lib/x.js"},
		{"docs/a/index.html", "../x.js", "/docs/x.js"},
		{"index.html", "https://cdn.example.com/x.js", ""},
		{"index.html", "//cdn.example.com/x.js", ""},
		{"index.html", "data:text/javascript,1", ""},
	} {
		if got := scriptSitePath(c[0], c[1]); got != c[2] {
			t.Errorf("scriptSitePath(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
