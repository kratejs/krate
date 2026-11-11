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
