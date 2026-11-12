//go:build windows

package windows

import (
	"sync"
	"unsafe"
)

// Every step below runs for each window of the Windows CI GUI tests; see
// memory.go. A step that fails skips and logs once; none of them may fail
// window creation.

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
func (a *App) tuneMemory(v *view) {
	setVisibleMemoryTarget(v.webview, a.cfg.Memory)
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
