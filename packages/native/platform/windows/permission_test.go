package windows

import (
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestPermissionKindOf(t *testing.T) {
	want := map[uint32]caps.PermissionKind{
		1: caps.PermMicrophone, 2: caps.PermCamera, 3: caps.PermLocation, 4: caps.PermNotifications, 6: caps.PermClipboardRead,
	}
	for k := uint32(0); k <= 12; k++ {
		kind, ok := permissionKindOf(k)
		if w, managed := want[k]; managed != ok || (ok && kind != w) {
			t.Errorf("kind %d: %q %v", k, kind, ok)
		}
	}
	if permissionState(true) != 1 || permissionState(false) != 2 {
		t.Error("permission states must be ALLOW=1 and DENY=2")
	}
}

func TestOriginOfURI(t *testing.T) {
	for in, want := range map[string]string{
		"https://app.localhost/index.html?x=1": "https://app.localhost",
		"krate://app/":                         "krate://app",
		"":                                     Origin,
		"about:blank":                          Origin,
		"::bad":                                Origin,
	} {
		if got := originOfURI(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestConsentStatus(t *testing.T) {
	for _, c := range []struct {
		vals []string
		want caps.PrivacyStatus
	}{
		{nil, caps.PrivacyUnknown},
		{[]string{""}, caps.PrivacyUnknown},
		{[]string{"Allow"}, caps.PrivacyGranted},
		{[]string{"Allow", "Deny"}, caps.PrivacyDenied},
		{[]string{"", "Allow", ""}, caps.PrivacyGranted},
		{[]string{"Deny", "Allow"}, caps.PrivacyDenied},
	} {
		if got := consentStatus(c.vals...); got != c.want {
			t.Errorf("%v: %s, want %s", c.vals, got, c.want)
		}
	}
	for k, want := range map[caps.PermissionKind]string{caps.PermCamera: "webcam", caps.PermMicrophone: "microphone", caps.PermLocation: "location", caps.PermScreen: ""} {
		if consentKey(k) != want {
			t.Errorf("consentKey(%s) = %q", k, consentKey(k))
		}
	}
}

func TestWindowSources(t *testing.T) {
	got := windowSources([]winRow{
		{hwnd: 0x10, title: "Editor", app: "code", width: 800, height: 600, visible: true},
		{hwnd: 0x11, title: "Hidden", width: 800, height: 600},
		{hwnd: 0x12, title: "Other desktop", width: 800, height: 600, visible: true, cloaked: true},
		{hwnd: 0x13, title: "Tool", width: 800, height: 600, visible: true, toolWindow: true},
		{hwnd: 0x14, title: "", width: 800, height: 600, visible: true},
		{hwnd: 0x15, title: "tiny", width: 10, height: 10, visible: true},
	})
	if len(got) != 1 || got[0].ID != "window:16" || got[0].App != "code" || got[0].Kind != "window" {
		t.Fatalf("%+v", got)
	}
	if monitorName(1) != "Screen 1" {
		t.Error("monitor name")
	}
}
