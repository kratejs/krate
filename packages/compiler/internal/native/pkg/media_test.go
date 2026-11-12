package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInfoPlistUsageDescriptions(t *testing.T) {
	m := Metadata{AppID: "com.acme.cam", ProductName: "Cam", Version: "1.0.0", MacCategory: "public.app-category.video",
		MediaUses: []string{"camera", "microphone", "location"},
		MediaUsage: MediaUsage{Camera: "Cam shows your video call."}}
	got := string(InfoPlist(m, "Cam"))
	for _, want := range []string{
		"<key>NSCameraUsageDescription</key>\n\t<string>Cam shows your video call.</string>",
		// A kind without a sentence gets a generic one: macOS ends the process otherwise.
		"<key>NSMicrophoneUsageDescription</key>\n\t<string>Cam uses the microphone.</string>",
		"<key>NSLocationUsageDescription</key>",
		"<key>NSLocationWhenInUseUsageDescription</key>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Info.plist lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "NSScreenCaptureUsageDescription") {
		t.Error("screen capture was not used")
	}
	plain := string(InfoPlist(Metadata{AppID: "a.b", ProductName: "P", Version: "1.0.0"}, "P"))
	if strings.Contains(plain, "UsageDescription") {
		t.Errorf("an app that uses no device got usage descriptions:\n%s", plain)
	}
	// A sentence in the config is written even before the scan sees a use.
	cfgOnly := string(InfoPlist(Metadata{AppID: "a.b", ProductName: "P", Version: "1.0.0", MediaUsage: MediaUsage{ScreenCapture: "Share your screen."}}, "P"))
	if !strings.Contains(cfgOnly, "NSScreenCaptureUsageDescription") {
		t.Error("a configured sentence was dropped")
	}
}

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

func TestMSIXDeviceCapabilities(t *testing.T) {
	m := Metadata{AppID: "com.acme.cam", ProductName: "Cam", Version: "1.0.0", Publisher: "CN=Acme", MediaUses: []string{"camera", "microphone", "screen"}}
	got := string(AppxManifest(m, Payload{}))
	if !strings.Contains(got, `<DeviceCapability Name="webcam"/>`) || !strings.Contains(got, `<DeviceCapability Name="microphone"/>`) {
		t.Errorf("manifest lacks the device capabilities:\n%s", got)
	}
	if strings.Contains(got, `Name="location"`) {
		t.Error("location was not used")
	}
	if plain := string(AppxManifest(Metadata{AppID: "a.b", ProductName: "P", Version: "1.0.0", Publisher: "CN=A"}, Payload{})); strings.Contains(plain, "DeviceCapability") {
		t.Error("an app that uses no device got device capabilities")
	}
}

func TestApplyAppJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.json")
	if err := os.WriteFile(p, []byte(`{"appId":"a.b","media":{"policy":{"camera":"allow"},"uses":["camera","location"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var m Metadata
	ApplyAppJSON(&m, p)
	if len(m.MediaUses) != 2 || m.MediaUses[0] != "camera" {
		t.Fatalf("%v", m.MediaUses)
	}
	m = Metadata{}
	ApplyAppJSON(&m, filepath.Join(dir, "missing.json"))
	if m.MediaUses != nil {
		t.Fatal("a missing file must change nothing")
	}
}
