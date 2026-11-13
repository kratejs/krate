//go:build darwin

package darwin

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests of several views in one window. They run with the other GUI tests
// (KRATE_NATIVE_GUI=1) in the CI native job on macos-latest. The layout arithmetic they rely on is unit-tested in
// platform/caps.

type viewPing struct {
	tag  string
	view caps.ViewRef
	ok   bool
}

var viewPings = make(chan viewPing, 32)

// withViewPing wraps h: a request for /viewping answers with a small page and
// reports the native view it came from.
func withViewPing(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/viewping" {
			v, ok := caps.ViewOf(r)
			viewPings <- viewPing{tag: r.URL.Query().Get("tag"), view: v, ok: ok}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<!doctype html><title>viewping</title><body>ping")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func waitViewPing(t *testing.T, tag string) viewPing {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case p := <-viewPings:
			if p.tag == tag {
				return p
			}
		case <-deadline:
			t.Fatalf("no request from the view tagged %q within 30s", tag)
		}
	}
}
