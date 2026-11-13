package shell

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestManifestChrome(t *testing.T) {
	m, err := ParseManifest([]byte(`{"appId":"com.acme.app","window":{"chrome":{
		"titleBar":"hidden","transparent":true,"backdrop":"mica","vibrancy":"sidebar",
		"titleBarOverlay":{"color":"#101010","symbolColor":"#fff","height":40},
		"trafficLightPosition":{"x":14,"y":18},"tabbingIdentifier":"docs","tabbingMode":"preferred"}},
		"mac":{"agent":true}}`), "app.json")
	if err != nil {
		t.Fatal(err)
	}
	o := m.Window.Chrome.Options()
	want := caps.ChromeOptions{
		TitleBar: "hidden", Transparent: true, Backdrop: "mica", Vibrancy: "sidebar",
		Overlay:      &caps.TitleBarOverlay{Color: "#101010", SymbolColor: "#fff", Height: 40},
		TrafficLight: &caps.Point{X: 14, Y: 18}, TabbingID: "docs", TabbingMode: "preferred",
	}
	if o.TitleBar != want.TitleBar || !o.Transparent || o.Backdrop != want.Backdrop || o.Vibrancy != want.Vibrancy ||
		*o.Overlay != *want.Overlay || *o.TrafficLight != *want.TrafficLight || o.TabbingID != "docs" || o.TabbingMode != "preferred" {
		t.Fatalf("options %+v", o)
	}
	if !m.Mac.Agent {
		t.Fatal("mac.agent lost")
	}
	if !m.Window.Chrome.needsScript() {
		t.Fatal("a hidden title bar does not get the script")
	}
}

func TestManifestChromeNeedsScript(t *testing.T) {
	var none *ChromeSpec
	if none.needsScript() || (&ChromeSpec{}).needsScript() || (&ChromeSpec{TitleBar: "native"}).needsScript() {
		t.Error("a native window gets the script everywhere")
	}
	if !(&ChromeSpec{TitleBar: "none"}).needsScript() || !(&ChromeSpec{Transparent: true}).needsScript() {
		t.Error("a frameless or transparent window misses the script")
	}
}

func TestManifestChromeRejectsBadValues(t *testing.T) {
	for _, c := range []string{
		`"titleBar":"floating"`, `"backdrop":"glass"`, `"vibrancy":"jelly"`, `"tabbingMode":"many"`,
		`"titleBarOverlay":{"color":"red"}`, `"trafficLightPosition":{"x":-1,"y":0}`,
	} {
		_, err := ParseManifest([]byte(`{"appId":"com.acme.app","window":{"chrome":{`+c+`}}}`), "app.json")
		if err == nil || !strings.Contains(err.Error(), "window.chrome") {
			t.Errorf("%s: error %v", c, err)
		}
	}
}

func TestInjectSnippetsOrderAndChromeScript(t *testing.T) {
	got := string(injectSnippets([]byte("<html><head><title>x</title></head></html>"), "<a>", "<b>"))
	if got != "<html><head><a><b><title>x</title></head></html>" {
		t.Fatalf("got %s", got)
	}
}

func TestRunMainLeavesArgumentsAfterTheTerminatorToTheApp(t *testing.T) {
	var out strings.Builder
	// --new-window is not a shell flag: after "--" the shell does not read it,
	// which is how a jump list task passes it.
	if code := RunMain([]string{"--version", "--", "--new-window", "--open", "x"}, embeddedFS(false), nil, &out); code != 0 {
		t.Fatalf("exit code %d, output %q", code, out.String())
	}
	if strings.TrimSpace(out.String()) != "Embedded 2.3.4" {
		t.Fatalf("output %q", out.String())
	}
}
