//go:build windows

package windows

import (
	"runtime"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// These tests need Windows but no display and no WebView2: they exercise the
// WinRT plumbing against the real system.

func withWinRT(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	initCallbacks()
	// RO_INIT_MULTITHREADED; the thread belongs to this test only.
	if hr, _, _ := pRoInitialize.Call(1); failed(hr) && uint32(hr) != rpcEChangedMode {
		t.Skipf("RoInitialize: 0x%08x", uint32(hr))
	}
}

func TestHStringRoundTrip(t *testing.T) {
	if pWindowsCreateString.Find() != nil {
		t.Skip("no WinRT on this system")
	}
	for _, s := range []string{"", "plain", "h\u00e9llo \U0001F600 \u4e16\u754c", "with\x00nul"} {
		h, err := newHString(s)
		if err != nil {
			t.Fatal(err)
		}
		want := s
		if s == "with\x00nul" {
			want = "withnul"
		}
		if got := hstringValue(h); got != want {
			t.Errorf("round trip %q = %q", s, got)
		}
		deleteHString(h)
	}
}

// The delegate objects answer the interfaces WinRT asks for and call back.
func TestToastDelegateInterfaces(t *testing.T) {
	withWinRT(t)
	var calls atomic.Int32
	h := newDelegate(iidToastDismissedHandler, func(sender, args uintptr) { calls.Add(1) })
	defer comRelease(h)
	for _, iid := range []string{iidIUnknown, iidAgileObject, iidToastDismissedHandler} {
		g := mustGUID(iid)
		var out uintptr
		if hr := vcall(h, iunknownSlotQI, uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&out))); failed(hr) || out == 0 {
			t.Errorf("QueryInterface(%s) = 0x%08x", iid, uint32(hr))
			continue
		}
		comRelease(ptr(out))
	}
	for _, iid := range []string{iidToastFailedHandler, iidToastActivatedHandler, "00000003-0000-0000-C000-000000000046"} {
		g := mustGUID(iid)
		var out uintptr
		if hr := vcall(h, iunknownSlotQI, uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&out))); uint32(hr) != eNoInterface || out != 0 {
			t.Errorf("QueryInterface(%s) = 0x%08x, want E_NOINTERFACE", iid, uint32(hr))
		}
	}
	if hr := vcall(h, handlerInvoke, 0, 0); failed(hr) || calls.Load() != 1 {
		t.Errorf("Invoke = 0x%08x, calls %d", uint32(hr), calls.Load())
	}
}

// The real XML parser accepts what buildToastXML writes, including hostile
// content.
func TestToastXMLAcceptedByWinRT(t *testing.T) {
	withWinRT(t)
	if pRoGetActivationFactory.Find() != nil {
		t.Skip("no WinRT on this system")
	}
	cases := []caps.Notification{
		{ID: "a", Title: "Hello", Body: "World"},
		{ID: "b", Title: `<b>"quoted" & 'apos'</b>`, Body: "line1\nline2\t\u00e9\U0001F600",
			Actions: []caps.NotificationAction{{ID: "x", Label: "A & B"}, {ID: "y&z", Label: `"C"`}}},
		{ID: "c", Title: "ctl \x01\x02 \ufffe", Silent: true, TimeoutMS: 30000},
		{ID: "d", Title: "t", Urgency: 2, Actions: []caps.NotificationAction{{ID: "a", Label: "A"}}},
		{},
	}
	for _, n := range cases {
		doc, err := toastDocument(buildToastXML(n, fileURI(`C:\Program Files\App\icon.png`)))
		if err != nil {
			t.Errorf("notification %+v: %v", n, err)
			continue
		}
		comRelease(doc)
	}
	if doc, err := toastDocument("<toast><unclosed>"); err == nil {
		comRelease(doc)
		t.Error("malformed XML was accepted; the test would prove nothing")
	}
}

func TestAUMIDRegistration(t *testing.T) {
	aumid := "dev.krate.native.regtest"
	defer deleteAUMID(aumid)
	if err := registerAUMID(aumid, "Reg Test", ""); err != nil {
		t.Fatal(err)
	}
	if got := readAUMIDName(aumid); got != "Reg Test" {
		t.Errorf("DisplayName = %q", got)
	}
	// Registering again updates in place.
	if err := registerAUMID(aumid, "Renamed", ""); err != nil {
		t.Fatal(err)
	}
	if got := readAUMIDName(aumid); got != "Renamed" {
		t.Errorf("DisplayName after update = %q", got)
	}
}

func TestIsPackagedFalseInTests(t *testing.T) {
	if isPackaged() {
		t.Skip("running with package identity")
	}
	if pSetCurrentProcessAUMID.Find() != nil {
		t.Skip()
	}
}
