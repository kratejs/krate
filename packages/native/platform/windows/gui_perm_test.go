//go:build windows

package windows

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// The page's permission requests through the real WebView2 runtime. The tests
// run with Chromium's fake camera and microphone (--use-fake-device-for-media-stream,
// set in TestMain), because a CI runner has no devices.

var permResults = make(chan map[string]string, 16)

func withPermPages(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/perm":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><title>perm</title><body>perm<script>
(async () => {
  const run = new URLSearchParams(location.search).get("run");
  const out = {run, mediaDevices: String(!!navigator.mediaDevices)};
  const stop = (s) => s.getTracks().forEach((t) => t.stop());
  try {
    if (run === "cam") stop(await navigator.mediaDevices.getUserMedia({video: true}));
    else if (run === "mic") stop(await navigator.mediaDevices.getUserMedia({audio: true}));
    else if (run === "both") stop(await navigator.mediaDevices.getUserMedia({audio: true, video: true}));
    else if (run === "geo") {
      await new Promise((ok, no) => navigator.geolocation.getCurrentPosition(ok, (e) => no(Object.assign(new Error(e.message), {code: e.code})), {timeout: 5000}));
    }
    out.result = "ok";
  } catch (e) { out.result = "error"; out.name = e.name || ""; out.code = String(e.code ?? ""); out.message = e.message; }
  fetch("/permres", {method: "POST", body: JSON.stringify(out)});
})();
</script></body>`)
		case "/permres":
			var m map[string]string
			_ = json.NewDecoder(r.Body).Decode(&m)
			permResults <- m
			w.WriteHeader(204)
		default:
			h.ServeHTTP(w, r)
		}
	})
}

// runPerm opens /perm?run=<run> and answers the page's permission requests
// with decide (no handler when nil). It returns what the page saw and the
// requests the handler got.
func runPerm(t *testing.T, run string, decide func(caps.PermissionRequest) bool) (map[string]string, []caps.PermissionRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []caps.PermissionRequest
	if decide == nil {
		guiApp.SetPermissionHandler(nil)
	} else {
		guiApp.SetPermissionHandler(func(req caps.PermissionRequest, answer func(bool)) {
			mu.Lock()
			seen = append(seen, req)
			mu.Unlock()
			go answer(decide(req))
		})
	}
	defer guiApp.SetPermissionHandler(nil)
	for len(permResults) > 0 {
		<-permResults
	}
	w, err := guiApp.NewWindow(WindowConfig{Title: "perm", Width: 400, Height: 300, Resizable: true, Path: "/perm?run=" + run})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	select {
	case res := <-permResults:
		mu.Lock()
		defer mu.Unlock()
		return res, append([]caps.PermissionRequest(nil), seen...)
	case <-time.After(30 * time.Second):
		t.Fatalf("the page never reported (%s)", run)
	}
	return nil, nil
}

func TestGUIPermissionCamera(t *testing.T) {
	needGUI(t)
	res, seen := runPerm(t, "cam", func(caps.PermissionRequest) bool { return true })
	if res["mediaDevices"] != "true" {
		t.Fatalf("navigator.mediaDevices is missing: %v", res)
	}
	if len(seen) != 1 || len(seen[0].Kinds) != 1 || seen[0].Kinds[0] != caps.PermCamera || seen[0].View == 0 || seen[0].Origin != Origin {
		t.Fatalf("handler saw %+v", seen)
	}
	if res["name"] == "NotAllowedError" {
		t.Fatalf("allowed camera request was refused: %v", res)
	}
	t.Logf("allowed camera: %v", res)

	res, seen = runPerm(t, "cam", func(caps.PermissionRequest) bool { return false })
	if len(seen) != 1 || res["name"] != "NotAllowedError" {
		t.Fatalf("denied camera: result %v, requests %+v", res, seen)
	}
}

func TestGUIPermissionNoHandlerDenies(t *testing.T) {
	needGUI(t)
	res, _ := runPerm(t, "cam", nil)
	if res["name"] != "NotAllowedError" {
		t.Fatalf("without a handler the request must be denied: %v", res)
	}
}

func TestGUIPrivacyAndSources(t *testing.T) {
	needGUI(t)
	for _, k := range []caps.PermissionKind{caps.PermCamera, caps.PermMicrophone, caps.PermLocation} {
		st, err := guiApp.PrivacyStatus(k)
		if err != nil {
			t.Errorf("%s: %v", k, err)
		}
		t.Logf("%s: %s", k, st)
		if again, _ := guiApp.RequestPrivacy(k); again != st {
			t.Errorf("%s: RequestPrivacy changed the state from %s to %s", k, st, again)
		}
	}
	if _, err := guiApp.PrivacyStatus(caps.PermScreen); err == nil {
		t.Error("screen capture has no privacy switch on Windows")
	}
	list, err := guiApp.CaptureSources(96)
	if err != nil {
		t.Fatal(err)
	}
	screens := 0
	for _, s := range list {
		if s.ID == "" || s.Name == "" || s.Width <= 0 || s.Height <= 0 {
			t.Errorf("bad source %+v", s)
		}
		if s.Kind == "screen" {
			screens++
		}
	}
	if screens == 0 {
		t.Errorf("no screen listed among %d sources", len(list))
	}
	t.Logf("%d sources, %d screens", len(list), screens)
}
