package pkg

import (
	"strings"
	"testing"
)

func TestMacEntitlementsForDevices(t *testing.T) {
	got := string(MacEntitlements("quickjs", "camera", "microphone", "location"))
	for _, want := range []string{"com.apple.security.device.camera", "com.apple.security.device.audio-input", "com.apple.security.personal-information.location"} {
		if !strings.Contains(got, want) {
			t.Errorf("entitlements lack %s", want)
		}
	}
	if got := string(MacEntitlements("quickjs", "screen")); strings.Contains(got, "device.") {
		t.Errorf("screen capture needs no device entitlement:\n%s", got)
	}
	if got := string(MacEntitlements("quickjs")); strings.Contains(got, "com.apple.security.device") {
		t.Error("an app without devices got device entitlements")
	}
}
