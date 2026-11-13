package darwin

import (
	"testing"
	"time"
)

func TestMemoryProfileNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"": MemoryBalanced, "balanced": MemoryBalanced, "minimal": MemoryMinimal,
		"performance": MemoryPerformance, "bogus": MemoryBalanced,
	} {
		if got := memoryProfile(in); got != want {
			t.Errorf("memoryProfile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlanMemory(t *testing.T) {
	used := map[string]bool{"media": true, "webaudio": true, "webgl": true, "webrtc": true, "spellcheck": true}
	allOff := map[string]bool{}
	balancedUnknown := memoryPlan{NoAutoOpenWindows: true, NoPageCache: true}
	balancedNone := memoryPlan{
		NoAutoOpenWindows: true, NoPageCache: true, RequireUserActionForMedia: true,
		NoWebGL: true, NoWebAudio: true, NoMediaDevices: true,
	}
	minimalNone := balancedNone
	minimalNone.PurgeMemoryCache, minimalNone.PurgeEvery = true, 2*time.Minute

	cases := []struct {
		name     string
		profile  string
		features map[string]bool
		want     memoryPlan
	}{
		{"performance does nothing", "performance", allOff, memoryPlan{}},
		{"performance with nil", "performance", nil, memoryPlan{}},
		{"default is balanced, unknown features", "", nil, balancedUnknown},
		{"balanced, nothing detected", "balanced", allOff, balancedNone},
		{"balanced, all used", "balanced", used, balancedUnknown},
		{"balanced, only webgl used", "balanced", map[string]bool{"webgl": true}, memoryPlan{
			NoAutoOpenWindows: true, NoPageCache: true, RequireUserActionForMedia: true,
			NoWebAudio: true, NoMediaDevices: true,
		}},
		{"minimal, nothing detected", "minimal", allOff, minimalNone},
		{"minimal, unknown features", "minimal", nil, memoryPlan{
			NoAutoOpenWindows: true, NoPageCache: true, PurgeMemoryCache: true, PurgeEvery: 2 * time.Minute,
		}},
	}
	for _, c := range cases {
		if got := planMemory(c.profile, c.features); got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

// TestPlanKeepsPersistence documents that no profile asks for anything that
// clears persistent data: the plan has no data store switch at all, and the
// purge is limited to the memory cache.
func TestPlanKeepsPersistence(t *testing.T) {
	for _, p := range []string{"balanced", "minimal", "performance"} {
		plan := planMemory(p, map[string]bool{})
		if plan.PurgeMemoryCache && plan.PurgeEvery < time.Minute {
			t.Errorf("%s: purge interval %v is too aggressive", p, plan.PurgeEvery)
		}
	}
}

func TestMemorySelectors(t *testing.T) {
	media := "setMediaTypesRequiringUserActionForPlayback:"
	const (
		prefs   = "preferences"
		windows = "setJavaScriptCanOpenWindowsAutomatically:"
		feats   = "_features"
		key     = "key"
		setFeat = "_setEnabled:forFeature:"
		webgl   = "_setWebGLEnabled:"
		audio   = "_setWebAudioEnabled:"
		devices = "_setMediaDevicesEnabled:"
		cache1  = "_setUsesBackForwardCache:"
		cache2  = "_setPageCacheEnabled:"
		store   = "defaultDataStore"
		remove  = "removeDataOfTypes:modifiedSince:completionHandler:"
	)
	all := map[string]bool{"media": true, "webaudio": true, "webgl": true, "webrtc": true}
	none := map[string]bool{}
	cases := []struct {
		name     string
		profile  string
		features map[string]bool
		want     []string
	}{
		{"performance sends nothing", "performance", none, nil},
		{"performance, unknown features", "performance", nil, nil},
		{"balanced, unknown features: only the page cache is private", "balanced", nil,
			[]string{prefs, windows, feats, key, setFeat, cache1, cache2}},
		{"balanced, all features used", "balanced", all,
			[]string{prefs, windows, feats, key, setFeat, cache1, cache2}},
		{"balanced, nothing detected", "balanced", none,
			[]string{media, prefs, windows, feats, key, setFeat, webgl, audio, devices, cache1, cache2}},
		{"minimal adds the purge", "minimal", none,
			[]string{media, prefs, windows, feats, key, setFeat, webgl, audio, devices, cache1, cache2, store, remove}},
		{"minimal, unknown features", "minimal", nil,
			[]string{prefs, windows, feats, key, setFeat, cache1, cache2, store, remove}},
	}
	for _, c := range cases {
		got := memorySelectors(c.profile, c.features)
		if len(got) != len(c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
				break
			}
		}
	}
}

// TestLeversAreGuardable checks the invariants the guard code relies on:
// private setters start with an underscore, public ones do not, setters take
// one argument, and no lever is empty.
func TestLeversAreGuardable(t *testing.T) {
	for _, profile := range []string{"balanced", "minimal"} {
		plan := planMemory(profile, map[string]bool{})
		for _, l := range planLevers(plan) {
			if l.Name == "" || l.Target == "" || len(l.Selectors) == 0 {
				t.Errorf("%s: incomplete lever %+v", profile, l)
			}
			for _, s := range l.Selectors {
				if got := s[0] == '_'; got != l.Private {
					t.Errorf("%s/%s: selector %q private=%v", profile, l.Name, s, l.Private)
				}
				if s[len(s)-1] != ':' || len(s) < 2 || countColons(s) != 1 {
					t.Errorf("%s/%s: setter %q must take exactly one argument", profile, l.Name, s)
				}
			}
			if l.Feature != "" && !l.Private {
				t.Errorf("%s/%s: only private levers use the feature table", profile, l.Name)
			}
		}
	}
	if len(planLevers(planMemory("performance", nil))) != 0 {
		t.Error("performance must not apply a lever")
	}
}

func countColons(s string) int {
	n := 0
	for _, c := range s {
		if c == ':' {
			n++
		}
	}
	return n
}
