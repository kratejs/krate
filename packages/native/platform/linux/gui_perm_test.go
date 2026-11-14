//go:build linux

package linux

import (
	"encoding/json"
	"io"
	"net/http"
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
