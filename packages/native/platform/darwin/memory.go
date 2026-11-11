package darwin

import "time"

// Memory tuning for WKWebView: the decision table, free of Objective-C so it
// can be tested on any platform. The Objective-C side is memory_darwin.go.
//
// UNTESTED: none of this has run on macOS. It was written on Linux from
// memory of Apple's WebKit headers and is compile-checked only. Every lever
// is guarded (respondsToSelector, class lookups that may be nil) so a missing
// selector skips the lever and logs once; nothing here may stop a window from
// opening. The "macOS and Windows" section of the memory docs repeats this.
//
// Expected inputs, set from app.json by the shell: Options.Memory is the
// "memory" profile ("balanced" when empty or unknown) and Options.Features is
// the "features" object (media, webaudio, webgl, webrtc, spellcheck). A nil
// Features map means "not detected": no feature is switched off. A non-nil map
// switches off every feature whose key is missing or false.

// Memory profiles.
const (
	MemoryBalanced    = "balanced"
	MemoryMinimal     = "minimal"
	MemoryPerformance = "performance"
)

// memoryPlan is what to apply to a WKWebViewConfiguration and the process.
type memoryPlan struct {
	// Public API.
	//
	// RequireUserActionForMedia sets
	// WKWebViewConfiguration.mediaTypesRequiringUserActionForPlayback to
	// WKAudiovisualMediaTypeAll (public, macOS 10.12+) so nothing
	// auto-plays. Applied when the media feature was not detected.
	RequireUserActionForMedia bool
	// NoAutoOpenWindows sets WKPreferences.javaScriptCanOpenWindowsAutomatically
	// to NO (public). The macOS default is YES; windows opened by script
	// without a user gesture are never wanted by an app shell.
	NoAutoOpenWindows bool

	// Private API (WKPreferencesPrivate.h). Each is applied only when the
	// WebKit in the running macOS answers the selector or lists the feature.
	NoWebGL        bool // WebGL context creation fails
	NoWebAudio     bool // AudioContext is unavailable
	NoMediaDevices bool // navigator.mediaDevices is unavailable
	NoPageCache    bool // back/forward page cache off

	// PurgeMemoryCache removes WKWebsiteDataTypeMemoryCache (public API) a
	// few seconds after the first load and then every PurgeEvery, while a
	// window exists.
	PurgeMemoryCache bool
	PurgeEvery       time.Duration
}

// purgeFirstDelay is how long after the first window opens the first memory
// cache purge runs: enough for the initial load to settle.
const purgeFirstDelay = 5 * time.Second

// memoryProfile normalizes a profile name; unknown values mean balanced.
func memoryProfile(p string) string {
	switch p {
	case MemoryMinimal, MemoryPerformance:
		return p
	}
	return MemoryBalanced
}

func featureOff(features map[string]bool, name string) bool {
	return features != nil && !features[name]
}

// planMemory is the per-profile, per-feature decision table.
//
//	profile      media off  webgl/webaudio/webrtc off  page cache  memory cache purge
//	performance  no         no                         kept        never
//	balanced     yes        yes                        off         never
//	minimal      yes        yes                        off         first load + every 2 min
//
// "off" for a feature applies only when detection ran and did not find it.
// The default WKWebsiteDataStore is always kept so localStorage persists.
func planMemory(profile string, features map[string]bool) memoryPlan {
	profile = memoryProfile(profile)
	if profile == MemoryPerformance {
		return memoryPlan{}
	}
	p := memoryPlan{
		NoAutoOpenWindows:         true,
		RequireUserActionForMedia: featureOff(features, "media"),
		NoWebGL:                   featureOff(features, "webgl"),
		NoWebAudio:                featureOff(features, "webaudio"),
		NoMediaDevices:            featureOff(features, "webrtc"),
		NoPageCache:               true,
	}
	if profile == MemoryMinimal {
		p.PurgeMemoryCache = true
		p.PurgeEvery = 2 * time.Minute
	}
	return p
}
