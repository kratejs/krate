package build

import (
	"encoding/json"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestChromeMetaOmitsPlainWindows(t *testing.T) {
	if m := chromeMeta(config.NativeWindow{TitleBar: "native"}); m != nil {
		t.Fatalf("a native window has chrome %+v", m)
	}
	if m := chromeMeta(config.NativeWindow{}); m != nil {
		t.Fatalf("an empty window has chrome %+v", m)
	}
}

func TestChromeMetaAppJSON(t *testing.T) {
	m := chromeMeta(config.NativeWindow{
		TitleBar: "hidden", Transparent: true, Backdrop: "acrylic", Vibrancy: "hudWindow",
		TitleBarOverlay:      &config.NativeTitleBarOverlay{Color: "#111", Height: 36},
		TrafficLightPosition: &config.NativePoint{X: 12, Y: 14},
		TabbingIdentifier:    "main", TabbingMode: "preferred",
	})
	if m == nil {
		t.Fatal("no chrome")
	}
	b, err := json.Marshal(NativeWindowMeta{Title: "t", Chrome: m})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Chrome map[string]any `json:"chrome"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	c := got.Chrome
	if c["titleBar"] != "hidden" || c["transparent"] != true || c["backdrop"] != "acrylic" || c["vibrancy"] != "hudWindow" ||
		c["tabbingIdentifier"] != "main" || c["tabbingMode"] != "preferred" {
		t.Fatalf("chrome %v", c)
	}
	if o := c["titleBarOverlay"].(map[string]any); o["color"] != "#111" || o["height"] != float64(36) {
		t.Fatalf("overlay %v", o)
	}
	if p := c["trafficLightPosition"].(map[string]any); p["x"] != float64(12) || p["y"] != float64(14) {
		t.Fatalf("traffic lights %v", p)
	}
}
