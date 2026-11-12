//go:build darwin

package darwin

import (
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// The permission request path on macOS. A CI runner has no camera or
// microphone, so WebKit would reject a page's getUserMedia call before it
// asks the delegate; these tests call the delegate methods of the real
// WKWebView's UIDelegate directly, with blocks of their own, exactly as
// WebKit does. They also read the privacy states and list the sources.

const (
	selMedia   = "webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:decisionHandler:"
	selDisplay = "_webView:requestDisplayCapturePermissionForOrigin:initiatedByFrame:withSystemAudio:decisionHandler:"
	selGeo     = "webView:requestGeolocationPermissionForOrigin:initiatedByFrame:decisionHandler:"
)

// webOf returns the WKWebView of a view.
func webOf(t *testing.T, ref caps.ViewRef) objc.ID {
	t.Helper()
	var web objc.ID
	runOnMain(func() {
		for w, v := range guiApp.byWeb {
			if v.ref == ref {
				web = w
			}
		}
	})
	if web == 0 {
		t.Fatalf("no web view for %d", ref)
	}
	return web
}

// callDelegate invokes a delegate method with a decision block and returns
// the decision the block received.
func callDelegate(t *testing.T, web objc.ID, selName string, extra ...any) int {
	t.Helper()
	got := make(chan int, 1)
	var block objc.Block
	runOnMain(func() {
		withPool(func() {
			block = objc.NewBlock(func(_ objc.Block, d int) { got <- d })
			args := []any{web, objc.ID(0), objc.ID(0)}
			args = append(args, extra...)
			args = append(args, objc.ID(block))
			guiApp.uiDelegate.Send(sel(selName), args...)
		})
	})
	defer block.Release()
	select {
	case d := <-got:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("the decision handler was not called")
		return -1
	}
}

func TestGUIPermissionDelegate(t *testing.T) {
	requireGUI(t)
	w, err := guiApp.NewWindow(WindowOptions{Title: "perm", Width: 400, Height: 300, Resizable: true, Path: "/viewping?tag=perm"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	waitViewPing(t, "perm")
	ref := w.PrimaryView()
	web := webOf(t, ref)

	runOnMain(func() {
		if got := web.Send(sel("UIDelegate")); got != guiApp.uiDelegate {
			t.Errorf("the web view's UIDelegate is %v, want the app's", got)
		}
		for _, s := range []string{selMedia, selDisplay, selGeo} {
			if !respondsTo(guiApp.uiDelegate, sel(s)) {
				t.Errorf("the UI delegate does not respond to %s", s)
			}
		}
	})

	var seen []caps.PermissionRequest
	guiApp.SetPermissionHandler(func(req caps.PermissionRequest, answer func(bool)) {
		seen = append(seen, req)
		answer(req.Kinds[0] != caps.PermMicrophone)
	})
	defer guiApp.SetPermissionHandler(nil)

	// camera: allowed
	if d := callDelegate(t, web, selMedia, wkMediaCaptureCamera); d != wkPermissionGrant {
		t.Errorf("camera decision %d", d)
	}
	// microphone: denied by the handler
	if d := callDelegate(t, web, selMedia, wkMediaCaptureMicrophone); d != wkPermissionDeny {
		t.Errorf("microphone decision %d", d)
	}
	// camera and microphone is one request with two kinds; the handler looks at the first
	if d := callDelegate(t, web, selMedia, wkMediaCaptureCameraAndMicrophone); d != wkPermissionGrant {
		t.Errorf("both decision %d", d)
	}
	// screen capture through the private selector
	if d := callDelegate(t, web, selDisplay, false); d != wkDisplayScreenPrompt {
		t.Errorf("display decision %d", d)
	}
	if d := callDelegate(t, web, selGeo); d != wkPermissionGrant {
		t.Errorf("geolocation decision %d", d)
	}
	if len(seen) != 5 {
		t.Fatalf("handler saw %d requests: %+v", len(seen), seen)
	}
	for i, want := range [][]caps.PermissionKind{
		{caps.PermCamera}, {caps.PermMicrophone}, {caps.PermCamera, caps.PermMicrophone}, {caps.PermScreen}, {caps.PermLocation},
	} {
		got := seen[i]
		if got.View != ref || got.Origin != OriginURL || len(got.Kinds) != len(want) || got.Kinds[0] != want[0] {
			t.Errorf("request %d: %+v, want kinds %v from view %d", i, got, want, ref)
		}
	}

	// An unknown capture type is denied without asking.
	n := len(seen)
	if d := callDelegate(t, web, selMedia, 9); d != wkPermissionDeny || len(seen) != n {
		t.Errorf("unknown type: decision %d, handler calls %d", d, len(seen)-n)
	}
	// Without a handler everything is denied.
	guiApp.SetPermissionHandler(nil)
	if d := callDelegate(t, web, selMedia, wkMediaCaptureCamera); d != wkPermissionDeny {
		t.Errorf("no handler: decision %d", d)
	}
}

func TestGUIPrivacyAndSources(t *testing.T) {
	requireGUI(t)
	known := map[caps.PrivacyStatus]bool{
		caps.PrivacyGranted: true, caps.PrivacyDenied: true, caps.PrivacyNotDetermined: true, caps.PrivacyRestricted: true,
	}
	for _, k := range []caps.PermissionKind{caps.PermCamera, caps.PermMicrophone, caps.PermScreen, caps.PermLocation} {
		st, err := guiApp.PrivacyStatus(k)
		if err != nil || !known[st] {
			t.Errorf("%s: %q %v", k, st, err)
		}
		t.Logf("%s: %s", k, st)
	}
	if _, err := guiApp.PrivacyStatus(caps.PermClipboardRead); err == nil {
		t.Error("clipboard has no privacy switch")
	}
	list, err := guiApp.CaptureSources(0)
	if err != nil {
		t.Fatal(err)
	}
	screens := 0
	for _, s := range list {
		if s.ID == "" || s.Name == "" || (s.Kind != "screen" && s.Kind != "window") || s.Thumbnail != nil {
			t.Errorf("bad source %+v", s)
		}
		if s.Kind == "screen" {
			screens++
		}
	}
	t.Logf("%d sources, %d screens", len(list), screens)
	withThumbs, err := guiApp.CaptureSources(120)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range withThumbs {
		if s.Thumbnail != nil && len(s.Thumbnail) < 8 {
			t.Errorf("thumbnail of %s is %d bytes", s.ID, len(s.Thumbnail))
		}
	}
}
