//go:build darwin

package darwin

import (
	"log"
	"sync"
	"sync/atomic"
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

// memDebug turns the "skipped" messages on; they are debug-level because a
// missing private selector is expected on some macOS versions.
var memDebug atomic.Bool

// memLogOnce logs a skipped lever once per key, and only in debug mode.
func memLogOnce(key, format string, a ...any) {
	if !memDebug.Load() {
		return
	}
	if _, dup := memLogged.LoadOrStore(key, true); !dup {
		log.Printf("krate/native: memory: "+format, a...)
	}
}

// canSend reports whether target is a live object that answers s. Every
// selector in this file goes through it (or findPrivateFeature) before it is
// sent: an unknown selector would raise an Objective-C exception, which Go
// cannot catch.
func canSend(target objc.ID, s objc.SEL) bool {
	return target != 0 && respondsTo(target, s)
}

// classAnswers reports whether the class exists and answers the class method.
func classAnswers(name, selector string) (objc.ID, bool) {
	c := cls(name)
	if c == 0 || !respondsTo(c, sel(selector)) {
		return 0, false
	}
	return c, true
}

// isKindOf reports whether obj is an instance of the named class; false when
// the class does not exist.
func isKindOf(obj objc.ID, class string) bool {
	c := cls(class)
	return obj != 0 && c != 0 && objc.Send[bool](obj, sel("isKindOfClass:"), c)
}

// applyMemoryConfig applies the plan to a WKWebViewConfiguration before the
// web view is created (main thread). Only the levers in planLevers are used.
func (a *App) applyMemoryConfig(config objc.ID) {
	levers := planLevers(planMemory(a.opts.Memory, a.opts.Features))
	if len(levers) == 0 || config == 0 {
		return
	}
	var prefs objc.ID
	prefsTried := false
	for _, l := range levers {
		target := config
		if l.Target == targetPrefs {
			if !prefsTried {
				prefsTried = true
				if ps := sel(selPreferences); canSend(config, ps) {
					prefs = config.Send(ps)
				}
			}
			target = prefs
			if target == 0 {
				memLogOnce("prefs", "WKWebViewConfiguration.preferences unavailable; preference levers skipped")
				continue
			}
		}
		applyLever(target, l)
	}
}

// applyLever sets one lever on its target. Private levers try the WebKit
// feature table first (newer WebKit exposes preferences that way), then each
// direct setter in order. It logs once and does nothing when none is there.
func applyLever(target objc.ID, l lever) {
	value := leverValue(l)
	if l.Feature != "" {
		if f := findPrivateFeature(l.Feature); f != 0 {
			if s := sel(selSetFeature); canSend(target, s) {
				target.Send(s, false, f)
				return
			}
		}
	}
	for _, name := range l.Selectors {
		if s := sel(name); canSend(target, s) {
			target.Send(s, value)
			return
		}
	}
	memLogOnce(l.Name, "%s: none of %v is available on this macOS; skipped", l.Name, l.Selectors)
}

// leverValue is the argument of a lever's setter: all media types for the
// playback restriction, NO for every other lever.
func leverValue(l lever) any {
	if l.Name == "media" {
		return wkAudiovisualMediaTypeAll
	}
	return false
}

// findPrivateFeature returns the _WKFeature whose key is key, or 0. Private:
// +[WKPreferences _features] (an NSArray) and -[_WKFeature key].
func findPrivateFeature(key string) objc.ID {
	c, ok := classAnswers("WKPreferences", selFeatures)
	if !ok {
		memLogOnce("features", "+[WKPreferences _features] unavailable; the feature table is not used")
		return 0
	}
	list := c.Send(sel(selFeatures))
	if !isKindOf(list, "NSArray") {
		memLogOnce("features-type", "+[WKPreferences _features] did not return an NSArray; the feature table is not used")
		return 0
	}
	n := objc.Send[uint](list, sel("count"))
	for i := uint(0); i < n; i++ {
		f := list.Send(sel("objectAtIndex:"), i)
		if !canSend(f, sel(selFeatureKey)) {
			continue
		}
		k := f.Send(sel(selFeatureKey))
		if isKindOf(k, "NSString") && goString(k) == key {
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
	store, ok := classAnswers("WKWebsiteDataStore", selDefaultStore)
	if !ok {
		memLogOnce("store", "WKWebsiteDataStore unavailable; memory cache purge skipped")
		return
	}
	ds := store.Send(sel(selDefaultStore))
	s := sel(selRemoveData)
	if !canSend(ds, s) {
		memLogOnce("removeData", "%s unavailable; memory cache purge skipped", selRemoveData)
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
