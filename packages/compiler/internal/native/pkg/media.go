package pkg

import (
	"encoding/json"
	"os"
	"strings"
)

// MediaUsage are the sentences macOS shows in its consent prompts
// (native.media.usage).
type MediaUsage struct {
	Camera, Microphone, Location, ScreenCapture string
}

// ApplyAppJSON reads which webview permissions the built UI uses from the
// media block of app.json, so Info.plist, the entitlements and the MSIX
// capabilities cover them. A missing or unreadable file changes nothing.
func ApplyAppJSON(m *Metadata, appJSON string) {
	b, err := os.ReadFile(appJSON)
	if err != nil {
		return
	}
	var doc struct {
		Media struct {
			Uses []string `json:"uses"`
		} `json:"media"`
	}
	if json.Unmarshal(b, &doc) == nil {
		m.MediaUses = doc.Media.Uses
	}
}

func (m Metadata) usesMedia(kind string) bool {
	for _, u := range m.MediaUses {
		if u == kind {
			return true
		}
	}
	return false
}

// macUsageKeys returns the Info.plist usage description keys and sentences the
// app needs, in a fixed order. A kind the UI uses without a description gets
// a generic sentence, because macOS ends the process otherwise.
func macUsageKeys(m Metadata) [][2]string {
	var out [][2]string
	pick := func(set, fallback string) string {
		if strings.TrimSpace(set) != "" {
			return set
		}
		return m.ProductName + " " + fallback
	}
	if m.usesMedia("camera") || m.MediaUsage.Camera != "" {
		out = append(out, [2]string{"NSCameraUsageDescription", pick(m.MediaUsage.Camera, "uses the camera.")})
	}
	if m.usesMedia("microphone") || m.MediaUsage.Microphone != "" {
		out = append(out, [2]string{"NSMicrophoneUsageDescription", pick(m.MediaUsage.Microphone, "uses the microphone.")})
	}
	if m.usesMedia("location") || m.MediaUsage.Location != "" {
		d := pick(m.MediaUsage.Location, "uses your location.")
		out = append(out, [2]string{"NSLocationUsageDescription", d}, [2]string{"NSLocationWhenInUseUsageDescription", d})
	}
	if m.usesMedia("screen") || m.MediaUsage.ScreenCapture != "" {
		out = append(out, [2]string{"NSScreenCaptureUsageDescription", pick(m.MediaUsage.ScreenCapture, "captures the screen.")})
	}
	return out
}

// msixDeviceCapabilities returns the <DeviceCapability> names the app needs.
func msixDeviceCapabilities(m Metadata) []string {
	var out []string
	if m.usesMedia("camera") {
		out = append(out, "webcam")
	}
	if m.usesMedia("microphone") {
		out = append(out, "microphone")
	}
	if m.usesMedia("location") {
		out = append(out, "location")
	}
	return out
}
