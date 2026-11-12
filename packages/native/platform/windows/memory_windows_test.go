//go:build windows

package windows

import (
	"testing"
	"unsafe"

	xw "golang.org/x/sys/windows"
)

// TestEnvironmentOptionsObject drives the Go options object through its
// vtable the way the WebView2 loader does. It needs no WebView2
// runtime or display and runs on windows-latest in CI.
func TestEnvironmentOptionsObject(t *testing.T) {
	if newEnvironmentOptions("") != nil {
		t.Fatal("empty arguments must produce no options object")
	}
	initCallbacks()
	o := newEnvironmentOptions("--renderer-process-limit=1")
	if o == nil {
		t.Fatal("no options object")
	}
	defer comRelease(o)

	get := func(slot int) string {
		var p uintptr
		if hr := vcall(o, slot, uintptr(unsafe.Pointer(&p))); failed(hr) {
			t.Fatalf("get slot %d: 0x%08x", slot, uint32(hr))
		}
		if p == 0 {
			return ""
		}
		s := xw.UTF16PtrToString((*uint16)(ptr(p)))
		xw.CoTaskMemFree(ptr(p))
		return s
	}
	const iface = "ICoreWebView2EnvironmentOptions"
	if got := get(vtIndex(iface, "get_AdditionalBrowserArguments")); got != "--renderer-process-limit=1" {
		t.Errorf("arguments: %q", got)
	}
	if got := get(vtIndex(iface, "get_TargetCompatibleBrowserVersion")); got != defaultTargetVersion {
		t.Errorf("target version: %q (the runtime rejects a null one)", got)
	}
	if got := get(vtIndex(iface, "get_Language")); got != "" {
		t.Errorf("language should be unset: %q", got)
	}
	vcall(o, vtIndex(iface, "put_Language"), uintptr(unsafe.Pointer(wstr("en-US"))))
	if got := get(vtIndex(iface, "get_Language")); got != "en-US" {
		t.Errorf("language: %q", got)
	}
	var sso uint32 = 7
	vcall(o, vtIndex(iface, "get_AllowSingleSignOnUsingOSPrimaryAccount"), uintptr(unsafe.Pointer(&sso)))
	if sso != 0 {
		t.Errorf("sso default: %d", sso)
	}

	// QueryInterface: the options interface and IUnknown succeed, a later
	// revision is refused.
	for iid, ok := range map[string]bool{
		iidIUnknown:           true,
		iidEnvironmentOptions: true,
		iidIStream:            false,
	} {
		q := queryInterface(o, iid)
		if (q != nil) != ok {
			t.Errorf("QueryInterface %s: got %v", iid, q != nil)
		}
		if q != nil {
			comRelease(q)
		}
	}
}
