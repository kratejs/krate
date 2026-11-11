//go:build windows

package windows

import (
	"fmt"
	"unsafe"

	"github.com/kratejs/krate/packages/native/memstat"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// COREWEBVIEW2_PROCESS_KIND values from WebView2.h line 2854.
const (
	processKindBrowser  = 0
	processKindRenderer = 1
	processKindUtility  = 2
	processKindGPU      = 4
)

var _ caps.ProcessInfo = (*App)(nil)

// WebviewProcesses lists the WebView2 processes of the app's environment with
// ICoreWebView2Environment8::GetProcessInfos. Renderers map to webview:web and
// the GPU process to webview:gpu; the browser process and the utility
// processes (which include the network service) are webview:other, because
// WebView2 does not say which utility process is which. When the runtime is
// too old for Environment8, the browser process of a window
// (ICoreWebView2::get_BrowserProcessId) and its descendants are reported as
// webview:other.
func (a *App) WebviewProcesses() ([]caps.ProcessRef, []string) {
	var refs []caps.ProcessRef
	var notes []string
	a.callMain(func() {
		if a.env == nil {
			notes = append(notes, "the WebView2 environment does not exist yet")
			return
		}
		refs, notes = a.processInfos()
		if refs != nil {
			return
		}
		seen := map[int]bool{}
		for _, w := range a.windows {
			if w.webview == nil {
				continue
			}
			var pid uint32
			if hr := vcall(w.webview, wvGetBrowserProcessId, uintptr(unsafe.Pointer(&pid))); failed(hr) || pid == 0 {
				continue
			}
			kids, _ := memstat.Descendants(int(pid))
			for _, p := range append([]int{int(pid)}, kids...) {
				if !seen[p] {
					seen[p] = true
					refs = append(refs, caps.ProcessRef{PID: p, Role: memstat.RoleWebviewOther})
				}
			}
		}
		notes = append(notes, "WebView2 process kinds need runtime 1.0.1722 or later; every webview process is reported as webview:other")
	})
	return refs, notes
}

// processInfos runs on the main thread. It returns nil refs when the
// environment does not implement Environment8.
func (a *App) processInfos() ([]caps.ProcessRef, []string) {
	iid := mustGUID(iidEnvironment8)
	var env8 unsafe.Pointer
	if hr := vcall(a.env, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&env8))); failed(hr) || env8 == nil {
		return nil, nil
	}
	defer comRelease(env8)
	var col unsafe.Pointer
	if hr := vcall(env8, env8GetProcessInfos, uintptr(unsafe.Pointer(&col))); failed(hr) || col == nil {
		return nil, []string{fmt.Sprintf("GetProcessInfos failed: 0x%08x", uint32(hr))}
	}
	defer comRelease(col)
	var n uint32
	if hr := vcall(col, procColGetCount, uintptr(unsafe.Pointer(&n))); failed(hr) {
		return nil, []string{fmt.Sprintf("ProcessInfoCollection.get_Count failed: 0x%08x", uint32(hr))}
	}
	refs := []caps.ProcessRef{}
	for i := uint32(0); i < n; i++ {
		var info unsafe.Pointer
		if hr := vcall(col, procColGetValueAtIndex, uintptr(i), uintptr(unsafe.Pointer(&info))); failed(hr) || info == nil {
			continue
		}
		var pid, kind int32
		hr1 := vcall(info, procInfoGetProcessId, uintptr(unsafe.Pointer(&pid)))
		hr2 := vcall(info, procInfoGetKind, uintptr(unsafe.Pointer(&kind)))
		comRelease(info)
		if failed(hr1) || failed(hr2) || pid <= 0 {
			continue
		}
		role := memstat.RoleWebviewOther
		switch kind {
		case processKindRenderer:
			role = memstat.RoleWebviewWeb
		case processKindGPU:
			role = memstat.RoleWebviewGPU
		}
		refs = append(refs, caps.ProcessRef{PID: int(pid), Role: role})
	}
	return refs, nil
}
