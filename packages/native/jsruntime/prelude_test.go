package jsruntime

import (
	"encoding/json"
	"sort"
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
