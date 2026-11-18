package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

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
