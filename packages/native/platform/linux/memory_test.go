//go:build linux

package linux

import (
	"os"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/internal/memory"
)

func TestPressureFor(t *testing.T) {
	if _, ok := pressureFor(memory.Performance); ok {
		t.Error("performance must not set memory pressure settings")
	}
	b, ok := pressureFor(memory.Balanced)
	if !ok {
		t.Fatal("no balanced settings")
	}
	m, ok := pressureFor(memory.Minimal)
	if !ok {
		t.Fatal("no minimal settings")
	}
	for name, p := range map[string]pressure{"balanced": b, "minimal": m} {
		if p.limit == 0 || p.conservative <= 0 || p.conservative >= p.strict || p.strict >= 1 || p.poll <= 0 {
			t.Errorf("%s: %+v", name, p)
		}
	}
	if m.limit >= b.limit || m.conservative > b.conservative {
		t.Errorf("minimal is not tighter than balanced: %+v vs %+v", m, b)
	}
}

func TestMemoryProfileParsing(t *testing.T) {
	if (Memory{}).profile() != memory.Performance {
		t.Error("the zero Memory must be the performance profile")
	}
	if (Memory{Profile: "minimal"}).profile() != memory.Minimal {
		t.Error("minimal")
	}
}

func TestApplyJSCOptions(t *testing.T) {
	t.Setenv("JSC_useJIT", "")
	applyJSCOptions(memory.Balanced)
	if os.Getenv("JSC_useJIT") != "" {
		t.Error("balanced must leave the JIT on")
	}
	applyJSCOptions(memory.Minimal)
	if os.Getenv("JSC_useJIT") != "false" {
		t.Errorf("minimal: JSC_useJIT = %q", os.Getenv("JSC_useJIT"))
	}
	t.Setenv("JSC_useJIT", "true")
	applyJSCOptions(memory.Minimal)
	if os.Getenv("JSC_useJIT") != "true" {
		t.Error("a value the user set must win")
	}
}

// guiMemory is the profile the GUI tests run under: KRATE_TEST_MEMORY
// (default "balanced") with every optional feature off.
func guiMemory() Memory {
	p := os.Getenv("KRATE_TEST_MEMORY")
	if p == "" {
		p = "balanced"
	}
	return Memory{Profile: p}
}

func TestGUIMemoryProfileSettings(t *testing.T) {
	needGUI(t)
	if guiApp.profile != guiMemory().profile() {
		t.Fatalf("profile = %q", guiApp.profile)
	}
	wk, err := purego.Dlopen("libwebkit2gtk-4.1.so.0", rtldNow|rtldGlobal)
	if err != nil {
		t.Fatal(err)
	}
	get := func(sym string) func(s uintptr) int32 {
		var fn func(s uintptr) int32
		if _, err := purego.Dlsym(wk, sym); err != nil {
			t.Fatalf("%s: %v", sym, err)
		}
		purego.RegisterLibFunc(&fn, wk, sym)
		return fn
	}
	getters := map[string]func(uintptr) int32{
		"enable-webgl":      get("webkit_settings_get_enable_webgl"),
		"enable-webaudio":   get("webkit_settings_get_enable_webaudio"),
		"enable-media":      get("webkit_settings_get_enable_media"),
		"enable-page-cache": get("webkit_settings_get_enable_page_cache"),
	}
	w, err := guiApp.NewWindow(WindowOptions{Title: "mem", Width: 200, Height: 200, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	got := map[string]int32{}
	guiApp.callMain(func() {
		s := guiApp.api.wkViewGetSettings(w.view)
		for k, fn := range getters {
			got[k] = fn(s)
		}
	})
	on := guiApp.profile == memory.Performance
	for _, k := range []string{"enable-webgl", "enable-webaudio", "enable-media"} {
		if (got[k] != 0) != on {
			t.Errorf("%s = %d under %s", k, got[k], guiApp.profile)
		}
	}
	if (got["enable-page-cache"] != 0) != on {
		t.Errorf("enable-page-cache = %d under %s", got["enable-page-cache"], guiApp.profile)
	}
}
