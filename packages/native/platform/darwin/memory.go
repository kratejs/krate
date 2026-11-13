//go:build darwin

package darwin

import "time"

// Memory tuning for WKWebView: the decision table, free of Objective-C. The
// Objective-C side is memory_darwin.go.
//
// The levers run on every window the macOS CI GUI tests open, and the CI
// native job measures the example on macos-latest (informational); no test
// asserts what a lever saves. The selector names were written from memory of
// Apple's WebKit headers. Every lever is guarded (respondsToSelector, class
// lookups that may be nil) so a missing selector skips the lever and logs
// once; nothing here may stop a window from opening. The "macOS and Windows"
// section of the memory docs repeats this.
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

// leverTarget names the object a lever's selector is sent to.
type leverTarget string

// Lever targets.
const (
	targetConfig leverTarget = "WKWebViewConfiguration"
	targetPrefs  leverTarget = "WKPreferences"
	targetStore  leverTarget = "WKWebsiteDataStore"
)

// lever is one way the plan touches WebKit. The Objective-C side
// (memory_darwin.go) applies levers from planLevers and nothing else, so the
// table is the complete list of selectors the memory code can send.
type lever struct {
	// Name is the log key and the plan field it implements.
	Name   string
	Target leverTarget
	// Selectors are tried in order; the first one the target answers is
	// used. Setters take one argument.
	Selectors []string
	// Feature, for private preferences, is the WebKit feature key tried
	// first through +[WKPreferences _features] and -_setEnabled:forFeature:.
	Feature string
	// Private levers are not public API and may vanish in any macOS release.
	Private bool
}

// Selectors the memory code sends besides the lever setters.
const (
	selPreferences  = "preferences"             // WKWebViewConfiguration, public
	selFeatures     = "_features"               // +[WKPreferences], private
	selFeatureKey   = "key"                     // -[_WKFeature], private class
	selSetFeature   = "_setEnabled:forFeature:" // WKPreferences, private
	selDefaultStore = "defaultDataStore"        // +[WKWebsiteDataStore], public
	selRemoveData   = "removeDataOfTypes:modifiedSince:completionHandler:"
)

// planLevers lists the levers of a plan in the order they are applied.
func planLevers(p memoryPlan) []lever {
	var out []lever
	if p.RequireUserActionForMedia {
		out = append(out, lever{Name: "media", Target: targetConfig, Selectors: []string{"setMediaTypesRequiringUserActionForPlayback:"}})
	}
	if p.NoAutoOpenWindows {
		out = append(out, lever{Name: "autoOpenWindows", Target: targetPrefs, Selectors: []string{"setJavaScriptCanOpenWindowsAutomatically:"}})
	}
	if p.NoWebGL {
		out = append(out, lever{Name: "webgl", Target: targetPrefs, Feature: "WebGLEnabled", Selectors: []string{"_setWebGLEnabled:"}, Private: true})
	}
	if p.NoWebAudio {
		out = append(out, lever{Name: "webaudio", Target: targetPrefs, Feature: "WebAudioEnabled", Selectors: []string{"_setWebAudioEnabled:"}, Private: true})
	}
	if p.NoMediaDevices {
		out = append(out, lever{Name: "mediadevices", Target: targetPrefs, Feature: "MediaDevicesEnabled", Selectors: []string{"_setMediaDevicesEnabled:"}, Private: true})
	}
	if p.NoPageCache {
		out = append(out, lever{Name: "pagecache", Target: targetPrefs, Feature: "UsesBackForwardCache",
			Selectors: []string{"_setUsesBackForwardCache:", "_setPageCacheEnabled:"}, Private: true})
	}
	return out
}
