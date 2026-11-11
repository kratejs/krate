//go:build darwin

package darwin

import (
	"log"
	"sync"
	"time"

	"github.com/ebitengine/purego/objc"
)

// UNTESTED: compile-checked only, see memory.go. Selector and feature names
// marked "private" come from WebKit's WKPreferencesPrivate.h as remembered
// while writing on Linux; they are not public API, may be renamed or removed
// in any macOS release, and are only called after respondsToSelector says yes.
// An Objective-C exception cannot be caught from Go, which is why nothing is
// sent without that check.

// WKAudiovisualMediaTypeAll is NSUIntegerMax (WKWebViewConfiguration.h).
const wkAudiovisualMediaTypeAll = ^uint(0)

var memLogged sync.Map

func memLogOnce(key, format string, a ...any) {
	if _, dup := memLogged.LoadOrStore(key, true); !dup {
		log.Printf("krate/native: memory: "+format, a...)
	}
}

// applyMemoryConfig applies the plan to a WKWebViewConfiguration before the
// web view is created (main thread).
func (a *App) applyMemoryConfig(config objc.ID) {
	plan := planMemory(a.opts.Memory, a.opts.Features)
	if plan == (memoryPlan{}) {
		return
	}
	if plan.RequireUserActionForMedia {
		// Public, macOS 10.12+.
		if s := sel("setMediaTypesRequiringUserActionForPlayback:"); respondsTo(config, s) {
			config.Send(s, wkAudiovisualMediaTypeAll)
		} else {
			memLogOnce("media", "setMediaTypesRequiringUserActionForPlayback: missing; skipped")
		}
	}
	prefs := config.Send(sel("preferences"))
	if prefs == 0 {
		memLogOnce("prefs", "WKWebViewConfiguration.preferences is nil; preference levers skipped")
		return
	}
	if plan.NoAutoOpenWindows {
		// Public.
		if s := sel("setJavaScriptCanOpenWindowsAutomatically:"); respondsTo(prefs, s) {
			prefs.Send(s, false)
		}
	}
	// Private levers. The _setEnabled:forFeature: path is tried first because
	// newer WebKit exposes preferences that way; the direct setters are the
	// older spelling.
	if plan.NoWebGL {
		setPrivatePreference(prefs, "WebGLEnabled", "_setWebGLEnabled:", false)
	}
	if plan.NoWebAudio {
		setPrivatePreference(prefs, "WebAudioEnabled", "_setWebAudioEnabled:", false)
	}
	if plan.NoMediaDevices {
		setPrivatePreference(prefs, "MediaDevicesEnabled", "_setMediaDevicesEnabled:", false)
	}
	if plan.NoPageCache {
		// Equivalent of the Linux document-viewer cache model. The setter
		// name is the least certain of all of these; three spellings are
		// tried, first match wins.
		setPrivatePreference(prefs, "UsesBackForwardCache", "_setUsesBackForwardCache:", false, "_setPageCacheEnabled:")
	}
}

// setPrivatePreference sets a private WKPreferences boolean. featureKey is the
// WebKit preference key (as listed by +[WKPreferences _features]); setters are
// direct selectors tried in order. It logs once and does nothing when neither
// route exists.
func setPrivatePreference(prefs objc.ID, featureKey, setter string, value bool, altSetters ...string) {
	if f := findPrivateFeature(featureKey); f != 0 {
		if s := sel("_setEnabled:forFeature:"); respondsTo(prefs, s) {
			prefs.Send(s, value, f)
			return
		}
	}
	for _, name := range append([]string{setter}, altSetters...) {
		if s := sel(name); respondsTo(prefs, s) {
			prefs.Send(s, value)
			return
		}
	}
	memLogOnce(featureKey, "private WKPreferences %s not available on this macOS; skipped", featureKey)
}

// findPrivateFeature returns the _WKFeature whose key is key, or 0. Private:
// +[WKPreferences _features] and -[_WKFeature key].
func findPrivateFeature(key string) objc.ID {
	c := cls("WKPreferences")
	if c == 0 || !respondsTo(c, sel("_features")) {
		return 0
	}
	list := c.Send(sel("_features"))
	if list == 0 {
		return 0
	}
	n := objc.Send[uint](list, sel("count"))
	for i := uint(0); i < n; i++ {
		f := list.Send(sel("objectAtIndex:"), i)
		if f == 0 || !respondsTo(f, sel("key")) {
			continue
		}
		if goString(f.Send(sel("key"))) == key {
			return f
		}
	}
	return 0
}

// noopBlock is the completion handler for removeDataOfTypes:...: the header
// declares it non-null (no _Nullable), and WebKit calls it unconditionally, so
// nil would crash. One block lives for the whole process.
var (
	noopBlockOnce sync.Once
	noopBlock     objc.Block
)

func completionBlock() objc.Block {
	noopBlockOnce.Do(func() { noopBlock = objc.NewBlock(func(objc.Block) {}) })
	return noopBlock
}

// purgeMemoryCache drops WebKit's in-memory resource cache through the public
// WKWebsiteDataStore API. Only WKWebsiteDataTypeMemoryCache is requested, so
// localStorage, cookies and the disk cache are untouched (main thread).
func purgeMemoryCache() {
	store := cls("WKWebsiteDataStore")
	if store == 0 || !respondsTo(store, sel("defaultDataStore")) {
		memLogOnce("store", "WKWebsiteDataStore unavailable; memory cache purge skipped")
		return
	}
	ds := store.Send(sel("defaultDataStore"))
	s := sel("removeDataOfTypes:modifiedSince:completionHandler:")
	if ds == 0 || !respondsTo(ds, s) {
		return
	}
	types := cls("NSSet").Send(sel("setWithObject:"), nsString("WKWebsiteDataTypeMemoryCache"))
	since := cls("NSDate").Send(sel("distantPast"))
	ds.Send(s, types, since, objc.ID(completionBlock()))
}

// startMemoryPurge schedules the first purge after the first load and then
// the periodic ones. The chain ends when no window is left.
func (a *App) startMemoryPurge() {
	plan := planMemory(a.opts.Memory, a.opts.Features)
	if !plan.PurgeMemoryCache || !a.purgeStarted.CompareAndSwap(false, true) {
		return
	}
	var step func(d time.Duration)
	step = func(d time.Duration) {
		time.AfterFunc(d, func() {
			dispatch(func() {
				if len(a.windows) == 0 {
					a.purgeStarted.Store(false)
					return
				}
				withPool(purgeMemoryCache)
				step(plan.PurgeEvery)
			})
		})
	}
	step(purgeFirstDelay)
}
