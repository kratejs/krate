//go:build windows

package windows

import (
	"strconv"
	"strings"
)

// Memory tuning for the WebView2 runtime (pure decisions; the COM plumbing is
// in memory_windows.go and envoptions_windows.go).
//
// Every switch and feature name below was checked against the Chromium
// sources (content_switches.cc, gpu_switches.cc, blink common switches.cc,
// content_features.cc) and Microsoft's WebView2 browser flag list; see
// TestBrowserArgumentNames. Whether the runtime honors each one is unmeasured.
//
// The arguments are applied to the WebView2 environment of every Windows CI
// GUI test, and the CI native job measures the example on windows-latest
// (informational). No test asserts that the runtime honors an argument or what
// it saves: the lists below are reasoned from Chromium and WebView2
// documentation. Every lever is guarded: a failed QueryInterface or HRESULT skips the
// lever and logs once; nothing here may stop the app from starting.
//
// Expected inputs, set from app.json by the shell: Config.Memory is the
// "memory" profile ("balanced" when empty or unknown) and Config.Features is
// the "features" object (media, webaudio, webgl, webrtc, spellcheck). A nil
// Features map means "not detected": no feature is switched off. A non-nil map
// switches off every feature whose key is missing or false.

// Memory profiles.
const (
	MemoryBalanced    = "balanced"
	MemoryMinimal     = "minimal"
	MemoryPerformance = "performance"
)

// minimalOldSpaceMB caps the V8 old generation in the minimal profile. The
// default on 64-bit is in the GB range; 512 MB only bites a runaway page,
// which then fails with an out-of-memory renderer crash instead of growing.
const minimalOldSpaceMB = 512

// Permission kinds (COREWEBVIEW2_PERMISSION_KIND, WebView2.h line 2724) and
// states (line 2743).
const (
	permKindMicrophone = 1
	permKindCamera     = 2
	permStateDeny      = 2
)

// memoryProfile normalizes a profile name; unknown values mean balanced.
func memoryProfile(p string) string {
	switch p {
	case MemoryMinimal, MemoryPerformance:
		return p
	}
	return MemoryBalanced
}

// featureOff reports whether a feature is known to be unused: detection ran
// (non-nil map) and did not find it.
func featureOff(features map[string]bool, name string) bool {
	return features != nil && !features[name]
}

// disabledFeatures lists Chromium features switched off for a profile.
// performance returns none. Each entry is justified; entries without a
// Microsoft or Chromium source were left out on purpose.
func disabledFeatures(profile string) []string {
	if memoryProfile(profile) == MemoryPerformance {
		return nil
	}
	return []string{
		// Chromium content feature: keeps one extra renderer process
		// warm for the next navigation. A single-origin app never needs
		// it, and it costs a whole process.
		"SpareRendererForSitePerProcess",
		// Chromium content feature: keeps whole previous pages alive for
		// instant back/forward. Costs a page worth of memory per entry;
		// an app shell navigates rarely.
		"BackForwardCache",
		// Edge-specific out-of-process UI surfaces for web content and
		// the PDF viewer. Not in Microsoft's flag list and not in
		// Chromium; included because Tauri/wry ships the same two names
		// in its default arguments. An unknown feature name is ignored.
		// The memory effect is unmeasured. msSmartScreenProtection, which
		// wry also disables, is a security feature and is NOT disabled
		// here.
		"msWebOOUI",
		"msPdfOOUI",
	}
}

// browserArguments builds the AdditionalBrowserArguments value (space
// separated switches) for a profile and detected features. An empty result
// means no options object is needed.
//
// Deliberately not included:
//   - --disable-background-networking: it also stops component updates
//     (certificate revocation sets, SmartScreen data) in the browser
//     process, which is a security trade the memory saving does not justify.
//   - --disable-gpu: halves GPU-process memory but moves all rendering to the
//     CPU. Left to apps that opt in through WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS.
//   - --enable-low-end-device-mode: reported to shrink caches broadly, but
//     its effect on WebView2 is unverified.
//   - AutofillServerCommunication, Translate: no memory effect in WebView2.
//
// The user data folder is shared by every window of the app and Chromium
// requires identical arguments for one folder, so these are set once for the
// single environment.
func browserArguments(profile string, features map[string]bool) []string {
	profile = memoryProfile(profile)
	if profile == MemoryPerformance {
		return nil
	}
	var args []string
	// Single-origin trusted app: one renderer process. Site isolation can
	// still add processes for cross-origin iframes; this is a limit, not a
	// guarantee.
	args = append(args, "--renderer-process-limit=1")
	if fs := disabledFeatures(profile); len(fs) > 0 {
		args = append(args, "--disable-features="+strings.Join(fs, ","))
	}
	if profile == MemoryMinimal {
		args = append(args,
			"--js-flags=--max-old-space-size="+strconv.Itoa(minimalOldSpaceMB),
			// Shader cache on disk: saves disk and a little memory
			// for the cache index, costs recompiling shaders.
			"--disable-gpu-shader-disk-cache")
	}
	if featureOff(features, "webgl") {
		// Chromium switches (content/public/common/content_switches.cc:
		// kDisableWebGL, kDisableWebGL2); WebGL contexts are a large
		// GPU-process cost. Canvas 2D is unaffected.
		args = append(args, "--disable-webgl", "--disable-webgl2")
	}
	// media, webaudio, webrtc and spellcheck have no safe Chromium switch.
	// webrtc is handled through permission denial (denyMediaCapture);
	// media and webaudio cost nothing until used; spellcheck is a
	// per-language dictionary loaded lazily by WebView2 itself.
	return args
}

// browserArgumentString joins browserArguments for the options object.
func browserArgumentString(profile string, features map[string]bool) string {
	return strings.Join(browserArguments(profile, features), " ")
}

// denyMediaCapture reports whether camera and microphone requests are denied
// without a prompt. It applies when detection ran and found no WebRTC or
// getUserMedia use (the webrtc feature covers both). This is defence rather
// than a memory saving: it keeps capture pipelines from being created by
// stray code.
func denyMediaCapture(profile string, features map[string]bool) bool {
	return memoryProfile(profile) != MemoryPerformance && featureOff(features, "webrtc")
}

// deniedPermission reports whether a permission request of kind is denied.
func deniedPermission(deny bool, kind uint32) bool {
	return deny && (kind == permKindMicrophone || kind == permKindCamera)
}

// COREWEBVIEW2_MEMORY_USAGE_TARGET_LEVEL (WebView2.h line 2631).
const (
	memTargetNormal = 0
	memTargetLow    = 1
)

// visibleMemoryTarget is the target level a visible webview should have.
// Only minimal asks for LOW while visible; a hide path that restores the level
// on show should restore this value, not NORMAL.
func visibleMemoryTarget(profile string) int {
	if memoryProfile(profile) == MemoryMinimal {
		return memTargetLow
	}
	return memTargetNormal
}
