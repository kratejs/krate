//go:build windows

package windows

import (
	"bytes"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestGUIClipboardRich(t *testing.T) {
	needGUI(t)
	a := guiApp
	d := caps.ClipboardData{Text: "héllo plain", HTML: "<p>héllo <b>rich</b></p>", RTF: "{\\rtf1\\ansi hello}"}
	if err := a.WriteRich(d); err != nil {
		t.Fatal(err)
	}
	if s, err := a.ReadText(); err != nil || s != d.Text {
		t.Fatalf("text %q %v", s, err)
	}
	if h, err := a.ReadHTML(); err != nil || h != d.HTML {
		t.Fatalf("html %q %v", h, err)
	}
	if r, err := a.ReadRTF(); err != nil || r != d.RTF {
		t.Fatalf("rtf %q %v", r, err)
	}
	// The raw entry is the CF_HTML envelope.
	raw, err := a.ReadFormat("HTML Format")
	if err != nil || !bytes.HasPrefix(raw, []byte("Version:0.9\r\n")) {
		t.Fatalf("raw HTML Format %q %v", raw, err)
	}
	fm, err := a.ClipboardFormats()
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, f := range fm {
		has[f] = true
	}
	if !has["text/plain"] || !has["text/html"] || !has["text/rtf"] {
		t.Fatalf("formats %v", fm)
	}
	payload := []byte{0, 1, 2, 250, 'x'}
	if err := a.WriteFormat("application/x-krate-test", payload); err != nil {
		t.Fatal(err)
	}
	if b, err := a.ReadFormat("application/x-krate-test"); err != nil || !bytes.Equal(b, payload) {
		t.Fatalf("custom %v %v", b, err)
	}
	if h, _ := a.ReadHTML(); h != "" {
		t.Fatalf("html survived a replacing write: %q", h)
	}
	if err := a.WriteFormat("text/html", []byte("<i>only</i>")); err != nil {
		t.Fatal(err)
	}
	if h, _ := a.ReadHTML(); h != "<i>only</i>" {
		t.Fatalf("html %q", h)
	}
	if err := a.Clear(); err != nil {
		t.Fatal(err)
	}
}
