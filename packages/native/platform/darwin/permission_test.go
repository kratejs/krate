package darwin

import (
	"reflect"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestCaptureKinds(t *testing.T) {
	for typ, want := range map[int][]caps.PermissionKind{
		wkMediaCaptureCamera:              {caps.PermCamera},
		wkMediaCaptureMicrophone:          {caps.PermMicrophone},
		wkMediaCaptureCameraAndMicrophone: {caps.PermCamera, caps.PermMicrophone},
		7:                                 nil,
	} {
		if got := captureKinds(typ); !reflect.DeepEqual(got, want) {
			t.Errorf("type %d: %v, want %v", typ, got, want)
		}
	}
}

func TestDecisions(t *testing.T) {
	if captureDecision(true) != wkPermissionGrant || captureDecision(false) != wkPermissionDeny {
		t.Error("capture decision")
	}
	if displayDecision(true) != wkDisplayScreenPrompt || displayDecision(false) != wkDisplayDeny {
		t.Error("display decision")
	}
	// Deny must never be the "prompt" value: that would hand the decision to
	// WebKit's own prompt.
	if captureDecision(false) == wkPermissionPrompt {
		t.Error("deny is prompt")
	}
}

func TestOriginString(t *testing.T) {
	for _, c := range []struct {
		proto, host string
		port        int
		want        string
	}{
		{"", "", 0, OriginURL},
		{"krate", "app", 0, "krate://app"},
		{"https", "example.com", 8443, "https://example.com:8443"},
	} {
		if got := originString(c.proto, c.host, c.port); got != c.want {
			t.Errorf("%v: %q", c, got)
		}
	}
}

func TestPrivacyMappings(t *testing.T) {
	for av, want := range map[int]caps.PrivacyStatus{0: caps.PrivacyNotDetermined, 1: caps.PrivacyRestricted, 2: caps.PrivacyDenied, 3: caps.PrivacyGranted, 9: caps.PrivacyUnknown} {
		if got := privacyFromAV(av); got != want {
			t.Errorf("AV %d: %s", av, got)
		}
	}
	for cl, want := range map[int]caps.PrivacyStatus{0: caps.PrivacyNotDetermined, 1: caps.PrivacyRestricted, 2: caps.PrivacyDenied, 3: caps.PrivacyGranted, 4: caps.PrivacyGranted, 5: caps.PrivacyUnknown} {
		if got := privacyFromCL(cl); got != want {
			t.Errorf("CL %d: %s", cl, got)
		}
	}
	if privacyFromScreen(true) != caps.PrivacyGranted || privacyFromScreen(false) != caps.PrivacyNotDetermined {
		t.Error("screen")
	}
}

func TestWindowSources(t *testing.T) {
	got := windowSources([]cgWindow{
		{number: 1, layer: 0, name: "Doc", owner: "Pages", width: 800, height: 600},
		{number: 2, layer: 25, name: "Menubar", owner: "Window Server", width: 1920, height: 24},
		{number: 3, layer: 0, name: "", owner: "Finder", width: 500, height: 400},
		{number: 4, layer: 0, name: "tiny", owner: "x", width: 10, height: 10},
		{number: 5, layer: 0, name: "", owner: "", width: 500, height: 400},
	})
	if len(got) != 2 || got[0].ID != "window:1" || got[0].App != "Pages" || got[1].Name != "Finder" || got[1].Width != 500 {
		t.Fatalf("%+v", got)
	}
	if screenName(2) != "Display 2" {
		t.Error("screen name")
	}
}
