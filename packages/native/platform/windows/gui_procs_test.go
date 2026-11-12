//go:build windows

package windows

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI checks for the process features: offscreen capture (CapturePreview),
// the DevTools protocol commands and browser extensions. They run with the
// other GUI tests under KRATE_NATIVE_GUI=1 in the CI native job.

// withProcPages wraps h with the pages these tests load.
func withProcPages(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redpage":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><title>red</title><body style="margin:0;background:#ff0000">red</body>`)
		case "/extpage":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><title>ext</title><body>ext</body>`)
		default:
			h.ServeHTTP(w, r)
		}
	})
}

func procsWindow(t *testing.T, path string, offscreen bool) *Window {
	t.Helper()
	needGUI(t)
	w, err := guiApp.NewWindow(WindowConfig{Title: "procs", Width: 320, Height: 200, Resizable: true, Path: path, Offscreen: offscreen})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w
}

func TestGUIOffscreenSnapshot(t *testing.T) {
	w := procsWindow(t, "/redpage", true)
	var snap caps.Snapshot
	deadline := time.Now().Add(30 * time.Second)
	for {
		s, err := guiApp.SnapshotView(w.PrimaryView(), caps.SnapshotOptions{})
		if err == nil && s.Width > 0 {
			i := (s.Height/2)*s.Stride + (s.Width/2)*4
			if s.Pix[i+2] > 250 && s.Pix[i] < 5 && s.Pix[i+1] < 5 {
				snap = s
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no red frame within 30s (last error %v)", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if snap.Width <= 0 || snap.Height <= 0 {
		t.Fatalf("snapshot %dx%d", snap.Width, snap.Height)
	}
}

func TestGUICDP(t *testing.T) {
	w := procsWindow(t, "/redpage", false)
	time.Sleep(time.Second)
	res, err := guiApp.CDP(w.PrimaryView(), "Runtime.evaluate", `{"expression":"1+2","returnByValue":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, `"value":3`) {
		t.Fatalf("Runtime.evaluate = %s", res)
	}
	if _, err := guiApp.CDP(w.PrimaryView(), "Page.enable", "{}"); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 4)
	cancel, err := guiApp.OnCDPEvent(w.PrimaryView(), "Page.frameNavigated", func(p string) { got <- p })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	w.Navigate("/extpage")
	select {
	case p := <-got:
		if !strings.Contains(p, "frame") {
			t.Fatalf("event params %s", p)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no Page.frameNavigated event")
	}
	if _, err := guiApp.CDP(w.PrimaryView(), "No.suchMethod", "{}"); err == nil {
		t.Fatal("an unknown method must fail")
	}
}

const extManifest = `{"manifest_version":3,"name":"Krate test extension","version":"1.0",
"content_scripts":[{"matches":["https://app.localhost/*"],"js":["c.js"],"run_at":"document_start"}]}`

func TestGUIExtension(t *testing.T) {
	needGUI(t)
	if !guiApp.ExtensionsSupported() {
		t.Skip("the test app was not started with extensions")
	}
	dir := filepath.Join(t.TempDir(), "ext")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(extManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.js"), []byte(`document.documentElement.dataset.krateExt = "1";`), 0o644); err != nil {
		t.Fatal(err)
	}
	w := procsWindow(t, "/extpage", false)
	info, err := guiApp.LoadExtension(dir)
	if err != nil {
		t.Skipf("this WebView2 runtime does not load the extension: %v", err)
	}
	if info.ID == "" || info.Name != "Krate test extension" {
		t.Fatalf("extension info %+v", info)
	}
	list, err := guiApp.ListExtensions()
	if err != nil || len(list) == 0 {
		t.Fatalf("list %v %v", list, err)
	}
	w.Navigate("/extpage?again")
	deadline := time.Now().Add(20 * time.Second)
	for {
		res, err := guiApp.CDP(w.PrimaryView(), "Runtime.evaluate", `{"expression":"document.documentElement.dataset.krateExt || ''","returnByValue":true}`)
		if err == nil && strings.Contains(res, `"value":"1"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the content script did not run (last %s, %v)", res, err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err := guiApp.RemoveExtension(info.ID); err != nil {
		t.Fatal(err)
	}
}
