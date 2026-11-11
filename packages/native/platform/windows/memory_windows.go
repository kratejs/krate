//go:build windows

package windows

import (
	"sync"
	"unsafe"
)

// UNTESTED: compile-checked only, see memory.go. Every step below skips and
// logs once on failure; none of them may fail window creation.

var memLogged sync.Map

// logOnce logs a memory-tuning problem once per key per process.
func logOnce(key, format string, a ...any) {
	if _, dup := memLogged.LoadOrStore(key, true); !dup {
		logf(format, a...)
	}
}

// queryInterface asks obj for iid and returns an owned reference, or nil.
func queryInterface(obj unsafe.Pointer, iid string) unsafe.Pointer {
	g := mustGUID(iid)
	var out uintptr
	if failed(vcall(obj, iunknownSlotQI, uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&out)))) || out == 0 {
		return nil
	}
	return ptr(out)
}

// tuneMemory applies the per-webview memory levers at controller creation.
// It does not touch hide, show or release handling.
func (a *App) tuneMemory(w *Window) {
	if a.hPermission != nil {
		var token int64
		if hr := vcall(w.webview, wvAddPermissionRequested, uintptr(a.hPermission), uintptr(unsafe.Pointer(&token))); failed(hr) {
			logOnce("permission", "memory: add_PermissionRequested failed: 0x%08x; camera and microphone prompts stay on", uint32(hr))
		}
	}
	setVisibleMemoryTarget(w.webview, a.cfg.Memory)
}

// onPermissionRequested denies camera and microphone requests when the
// webrtc feature was not detected (see denyMediaCapture). Everything else
// keeps the WebView2 default (prompt).
func (a *App) onPermissionRequested(sender, args uintptr) uintptr {
	if args == 0 {
		return sOK
	}
	var kind uint32
	if failed(vcall(ptr(args), permArgsGetPermissionKind, uintptr(unsafe.Pointer(&kind)))) {
		return sOK
	}
	if deniedPermission(true, kind) {
		vcall(ptr(args), permArgsPutState, permStateDeny)
	}
	return sOK
}

// setVisibleMemoryTarget sets ICoreWebView2_19::MemoryUsageTargetLevel for a
// visible webview: LOW in the minimal profile, nothing otherwise. It is a
// separate helper so the hide path can reuse the QueryInterface logic. The
// interface needs a recent WebView2 runtime (it is the
// 19th ICoreWebView2 revision); on older runtimes QueryInterface fails and the
// lever is skipped.
func setVisibleMemoryTarget(webview unsafe.Pointer, profile string) {
	level := visibleMemoryTarget(profile)
	if level == memTargetNormal {
		return
	}
	setMemoryTarget(webview, level)
}

// setMemoryTarget calls put_MemoryUsageTargetLevel; false when unsupported.
func setMemoryTarget(webview unsafe.Pointer, level int) bool {
	wv19 := queryInterface(webview, iidWebView2_19)
	if wv19 == nil {
		logOnce("wv19", "memory: ICoreWebView2_19 not available (older WebView2 runtime); MemoryUsageTargetLevel skipped")
		return false
	}
	defer comRelease(wv19)
	if hr := vcall(wv19, wv19PutMemoryTargetLevel, uintptr(level)); failed(hr) {
		logOnce("wv19put", "memory: put_MemoryUsageTargetLevel failed: 0x%08x", uint32(hr))
		return false
	}
	return true
}
