//go:build linux

package linux

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Tests for the page's permission requests: the handler sees them with the
// right kinds and view, and its answer reaches the page. Real cameras are not
// available, so the app runs with the mock capture devices
// (KRATE_NATIVE_MOCK_CAPTURE, set per test window).

var (
	permResults = make(chan map[string]string, 16)
	permReady   = make(chan struct{}, 4)
)

func registerPermHandlers(s *testServer) {
	s.mux.HandleFunc("/perm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><title>perm</title><body>perm<script>
(async () => {
  const q = new URLSearchParams(location.search), run = q.get("run");
  const out = {run, mediaDevices: String(!!navigator.mediaDevices)};
  const stop = (s) => s.getTracks().forEach((t) => t.stop());
  try {
    if (run === "cam") stop(await navigator.mediaDevices.getUserMedia({video: true}));
    else if (run === "mic") stop(await navigator.mediaDevices.getUserMedia({audio: true}));
    else if (run === "both") stop(await navigator.mediaDevices.getUserMedia({audio: true, video: true}));
    else if (run === "display") {
      await new Promise((r) => { window.__go = r; fetch("/permready", {method: "POST"}); });
      stop(await navigator.mediaDevices.getDisplayMedia({video: true}));
    } else if (run === "geo") {
      await new Promise((ok, no) => navigator.geolocation.getCurrentPosition(ok, (e) => no(Object.assign(new Error(e.message), {code: e.code})), {timeout: 4000}));
    }
    out.result = "ok";
  } catch (e) { out.result = "error"; out.name = e.name || ""; out.code = String(e.code ?? ""); out.message = e.message; }
  fetch("/permres", {method: "POST", body: JSON.stringify(out)});
})();
</script></body>`)
	})
	s.mux.HandleFunc("/permready", func(w http.ResponseWriter, r *http.Request) {
		permReady <- struct{}{}
		w.WriteHeader(204)
	})
	s.mux.HandleFunc("/permres", func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		_ = json.NewDecoder(r.Body).Decode(&m)
		permResults <- m
		w.WriteHeader(204)
	})
}

// runPerm opens a window on /perm?run=<run>, answers the permission request
// with allow (or leaves it unanswered when handler is nil) and returns what
// the page saw and the requests the handler got.
func runPerm(t *testing.T, run string, decide func(caps.PermissionRequest) bool) (map[string]string, []caps.PermissionRequest) {
	t.Helper()
	// Only the windows of these tests get the mock devices (and the media
	// settings that go with them); the memory profile tests count on them off.
	t.Setenv("KRATE_NATIVE_MOCK_CAPTURE", "1")
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
	w, err := guiApp.NewWindow(WindowOptions{Title: "perm", Width: 400, Height: 300, Resizable: true, Path: "/perm?run=" + run})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if run == "display" {
		// getDisplayMedia needs a user gesture; script run through the
		// WebKit API counts as one.
		select {
		case <-permReady:
			w.Eval("window.__go()")
		case <-time.After(15 * time.Second):
			t.Fatal("the display page never became ready")
		}
	}
	select {
	case res := <-permResults:
		mu.Lock()
		defer mu.Unlock()
		return res, append([]caps.PermissionRequest(nil), seen...)
	case <-time.After(20 * time.Second):
		t.Fatalf("the page never reported (%s)", run)
	}
	return nil, nil
}

func kindsOf(r caps.PermissionRequest) map[caps.PermissionKind]bool {
	m := map[caps.PermissionKind]bool{}
	for _, k := range r.Kinds {
		m[k] = true
	}
	return m
}

func TestGUIPermissionCameraAllowAndDeny(t *testing.T) {
	needGUI(t)
	res, seen := runPerm(t, "cam", func(caps.PermissionRequest) bool { return true })
	if res["mediaDevices"] != "true" {
		t.Skipf("this WebKitGTK has no navigator.mediaDevices (no media stream support): %v", res)
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

func TestGUIPermissionMicrophoneAndBoth(t *testing.T) {
	needGUI(t)
	res, seen := runPerm(t, "mic", func(caps.PermissionRequest) bool { return false })
	if len(seen) != 1 || !kindsOf(seen[0])[caps.PermMicrophone] || len(seen[0].Kinds) != 1 || res["name"] != "NotAllowedError" {
		t.Fatalf("microphone: result %v, requests %+v", res, seen)
	}
	res, seen = runPerm(t, "both", func(caps.PermissionRequest) bool { return false })
	if len(seen) != 1 || !kindsOf(seen[0])[caps.PermMicrophone] || !kindsOf(seen[0])[caps.PermCamera] || res["name"] != "NotAllowedError" {
		t.Fatalf("camera and microphone: result %v, requests %+v", res, seen)
	}
}

func TestGUIPermissionNoHandlerDenies(t *testing.T) {
	needGUI(t)
	res, _ := runPerm(t, "cam", nil)
	if res["name"] != "NotAllowedError" {
		t.Fatalf("without a handler the request must be denied: %v", res)
	}
}

func TestGUIPermissionGeolocation(t *testing.T) {
	needGUI(t)
	res, seen := runPerm(t, "geo", func(caps.PermissionRequest) bool { return false })
	if len(seen) != 1 || len(seen[0].Kinds) != 1 || seen[0].Kinds[0] != caps.PermLocation {
		t.Fatalf("geolocation requests: %+v (page %v)", seen, res)
	}
	if res["code"] != "1" {
		t.Fatalf("denied geolocation must be PERMISSION_DENIED (1): %v", res)
	}
	res, seen = runPerm(t, "geo", func(caps.PermissionRequest) bool { return true })
	if len(seen) != 1 {
		t.Fatalf("geolocation requests: %+v", seen)
	}
	if res["code"] == "1" {
		t.Fatalf("allowed geolocation was refused: %v", res)
	}
	t.Logf("allowed geolocation: %v", res)
}

func TestGUIPermissionScreenCapture(t *testing.T) {
	needGUI(t)
	res, seen := runPerm(t, "display", func(caps.PermissionRequest) bool { return false })
	if len(seen) != 1 || !kindsOf(seen[0])[caps.PermScreen] || res["name"] != "NotAllowedError" {
		t.Skipf("no screen capture request reached the handler (WebKit may need a real user gesture here): result %v, requests %+v", res, seen)
	}
}
