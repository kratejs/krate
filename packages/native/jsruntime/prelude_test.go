package jsruntime

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
)

func mustRuntime(t *testing.T, o Options) *Runtime {
	t.Helper()
	rt, err := NewWithOptions(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

func execString(t *testing.T, rt *Runtime, code string) string {
	t.Helper()
	v, err := rt.Execute(code)
	if err != nil {
		t.Fatalf("%s: %v", code, err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%s: got %T %v, want a string", code, v, v)
	}
	return s
}

// preludeGlobals lists every global the prelude features define.
func preludeGlobals(t *testing.T) []string {
	t.Helper()
	s, err := prelude()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, name := range s.order {
		out = append(out, s.mods[name].globals...)
	}
	sort.Strings(out)
	return out
}

// describeGlobal reports how a global looks to user code.
const describeGlobal = `(function (name) {
  var t = typeof globalThis[name];
  var d = Object.getOwnPropertyDescriptor(globalThis, name);
  if (!d) return "absent";
  return [t, name in globalThis, "value" in d, d.writable, d.enumerable, d.configurable, Object.keys(globalThis).indexOf(name) >= 0].join(",");
})`

// TestLazyGlobalsLookLikePreloaded checks that typeof, the in operator, the
// read value and the property attributes of each prelude global are the same
// whether its feature loaded on first use or when the VM was created.
func TestLazyGlobalsLookLikePreloaded(t *testing.T) {
	eager := mustRuntime(t, Options{PreloadAll: true})
	for _, name := range preludeGlobals(t) {
		lazy := mustRuntime(t, Options{})
		probe := describeGlobal + `(` + jsString(name) + `)`
		if got := execString(t, lazy, `[`+jsString(name)+` in globalThis, Object.keys(globalThis).indexOf(`+jsString(name)+`) >= 0].join()`); got != "true,false" {
			t.Errorf("%s before loading: %s", name, got)
		}
		if loaded := lazy.LoadedFeatures(); len(loaded) != 0 {
			t.Errorf("checking %s in globalThis loaded %v", name, loaded)
		}
		if got, want := execString(t, lazy, probe), execString(t, eager, probe); got != want {
			t.Errorf("%s: lazily loaded %s, preloaded %s", name, got, want)
		}
	}
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestLazyLoadsOnlyWhatIsUsed checks that reading a global loads its feature
// and the features it requires, and nothing else.
func TestLazyLoadsOnlyWhatIsUsed(t *testing.T) {
	for _, tc := range []struct {
		code string
		want []string
	}{
		{`"ok"`, nil},
		{`"fetch" in globalThis && typeof fetch === "function" ? "ok" : "bad"`, []string{"events", "fetch", "url"}},
		{`String(Response.json({ a: 1 }).status)`, []string{"events", "fetch", "url"}},
		{`typeof structuredClone`, []string{"clone"}},
		{`String(new TextEncoder().encode("hi").length)`, []string{"encoding"}},
		{`typeof crypto.randomUUID()`, []string{"crypto"}},
		{`typeof new Blob(["a"]).stream()`, []string{"formdata", "streams"}},
		{`new Response(new Blob(["a"])).text().then(String) && "ok"`, []string{"events", "fetch", "formdata", "url"}},
		{`typeof AbortSignal.timeout(10)`, []string{"events", "timers"}},
		{`typeof __krate_timers_next() + typeof __krate_timers_clear()`, nil},
	} {
		rt := mustRuntime(t, Options{})
		if _, err := rt.Execute(tc.code); err != nil {
			t.Fatalf("%s: %v", tc.code, err)
		}
		if got := rt.LoadedFeatures(); !slices.Equal(got, tc.want) {
			t.Errorf("%s loaded %v, want %v", tc.code, got, tc.want)
		}
	}
}

// TestLazyUserValuesWin checks that a global user code assigns, declares or
// deletes before its feature loads stays that way when the feature loads
// through another of its globals.
func TestLazyUserValuesWin(t *testing.T) {
	for _, tc := range []struct{ setup, check, want string }{
		{`globalThis.URL = 1`, `typeof URLSearchParams + "," + URL`, "function,1"},
		{`URL = "mine"`, `typeof URLSearchParams + "," + URL`, "function,mine"},
		{`var fetch = "mine"`, `typeof Headers + "," + fetch`, "function,mine"},
		{`function Headers() { return 1 }`, `typeof fetch + "," + Headers()`, "function,1"},
		{`Object.defineProperty(globalThis, "crypto", { value: 7, configurable: true })`, `typeof CryptoKey + "," + crypto`, "function,7"},
		{`delete globalThis.TextEncoder`, `typeof TextDecoder + "," + typeof TextEncoder`, "function,undefined"},
		{`globalThis.console = { log() { return "mine" } }`, `console.log()`, "mine"},
	} {
		rt := mustRuntime(t, Options{})
		if _, err := rt.Execute(tc.setup); err != nil {
			t.Fatalf("%s: %v", tc.setup, err)
		}
		if got := execString(t, rt, tc.check); got != tc.want {
			t.Errorf("after %s: %s = %q, want %q", tc.setup, tc.check, got, tc.want)
		}
	}
	// A polyfill read back after it is assigned keeps its value.
	rt := mustRuntime(t, Options{})
	if got := execString(t, rt, `globalThis.structuredClone = (v) => "polyfill"; structuredClone(1)`); got != "polyfill" {
		t.Errorf("structuredClone polyfill: %q", got)
	}
	if desc := execString(t, rt, describeGlobal+`("structuredClone")`); desc != "function,true,true,true,false,true,false" {
		t.Errorf("structuredClone polyfill descriptor: %s", desc)
	}
}

// TestFeatureGlobalsMatchLedger checks each module defines exactly the
// globals compat.json lists for its feature (plus those of the features it
// requires).
func TestFeatureGlobalsMatchLedger(t *testing.T) {
	s, err := prelude()
	if err != nil {
		t.Fatal(err)
	}
	own := `Object.getOwnPropertyNames(globalThis).filter(function (n) { return !/^(__go_|console_|_go_)/.test(n); }).sort().join(" ")`
	coreOnly := func() *Runtime {
		r, err := newRuntime(Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close() })
		if err := r.evalModule(s.core); err != nil {
			t.Fatal(err)
		}
		return r
	}
	base := strings.Fields(execString(t, coreOnly(), own))
	for _, name := range s.order {
		r := coreOnly()
		if err := r.loadFeature(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var want []string
		for _, f := range r.LoadedFeatures() {
			want = append(want, s.mods[f].globals...)
		}
		sort.Strings(want)
		var got []string
		for _, g := range strings.Fields(execString(t, r, own)) {
			if !slices.Contains(base, g) {
				got = append(got, g)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s defines %v, compat.json lists %v", name, got, want)
		}
	}
}

// TestHostFunctionsRegisterWithTheirFeature checks a VM gets the Go host
// functions only for the features it loaded.
func TestHostFunctionsRegisterWithTheirFeature(t *testing.T) {
	rt := mustRuntime(t, Options{})
	probe := `["__go_fetch", "__go_random", "__go_digest", "__go_url_parse", "__go_parse_multipart", "console_log", "__krate_load_feature"].map(function (n) { return typeof globalThis[n]; }).join()`
	if got := execString(t, rt, probe); got != "undefined,undefined,undefined,undefined,undefined,undefined,undefined" {
		t.Errorf("fresh VM: %s", got)
	}
	execString(t, rt, `typeof crypto`)
	if got := execString(t, rt, probe); got != "undefined,function,function,undefined,undefined,undefined,undefined" {
		t.Errorf("after crypto: %s", got)
	}
}

// TestLoaderFailureThrows covers the status the loader host function returns
// instead of a Go error.
func TestLoaderFailureThrows(t *testing.T) {
	rt := mustRuntime(t, Options{})
	got := execString(t, rt, `try { globalThis[Symbol.for("krate.web")].need("nope"); "no error" } catch (e) { e.message }`)
	if !strings.Contains(got, `unknown web API feature "nope"`) {
		t.Errorf("got %q", got)
	}
	if got := execString(t, rt, `typeof URL`); got != "function" {
		t.Errorf("VM unusable after a failed load: typeof URL = %s", got)
	}
}

// TestPreloadOptions checks which features a VM installs up front.
func TestPreloadOptions(t *testing.T) {
	all := PreludeFeatures()
	if got := mustRuntime(t, Options{PreloadAll: true}).LoadedFeatures(); !slices.Equal(got, sorted(all)) {
		t.Errorf("PreloadAll: %v, want %v", got, sorted(all))
	}
	// Scanned lists also carry groups outside the prelude.
	got := mustRuntime(t, Options{Preload: []string{"crypto", "node", "engine"}}).LoadedFeatures()
	if !slices.Equal(got, []string{"crypto"}) {
		t.Errorf("Preload crypto: %v", got)
	}
	got = mustRuntime(t, runnerOptions([]string{"clone"}, true)).LoadedFeatures()
	if !slices.Equal(got, []string{"clone", "events", "fetch", "url"}) {
		t.Errorf("runner with clone: %v", got)
	}
	if got := mustRuntime(t, runnerOptions(nil, false)).LoadedFeatures(); !slices.Equal(got, sorted(all)) {
		t.Errorf("runner with unknown features: %v", got)
	}
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	sort.Strings(s)
	return s
}

// TestPreludeFeatureOrder checks every feature comes after the ones it
// requires.
func TestPreludeFeatureOrder(t *testing.T) {
	s, err := prelude()
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, name := range s.order {
		pos[name] = i
	}
	for _, name := range s.order {
		for _, req := range s.mods[name].requires {
			if pos[req] >= pos[name] {
				t.Errorf("%s comes before %s, which it requires", name, req)
			}
		}
	}
	if len(s.order) != len(s.mods) {
		t.Errorf("order %v misses modules", s.order)
	}
}
