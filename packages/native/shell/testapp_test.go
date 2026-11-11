package shell

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/wire"
)

// The platform layer needs the main goroutine on the main OS thread (GUI test).
func init() { runtime.LockOSThread() }

const hostJSv1 = `(function () {
  __krateHost.register("h.add", function (a, b) { return a + b; }, "func");
  __krateHost.register("h.tag", function () { return "host-v1"; }, "func");
})();
`

const hostJSv2 = `(function () {
  __krateHost.register("h.add", function (a, b) { return a + b + 1000; }, "func");
  __krateHost.register("h.tag", function () { return "host-v2"; }, "func");
  __krateHost.register("h.extra", function () { return "new"; }, "func");
})();
`

// testApp is an app directory built for tests.
type testApp struct {
	dir    string
	binary string // absolute path of the Go host binary
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// buildTestApp writes an app directory under root: dist/ with index.html (the
// given page), a tasks/[id] fallback page, a hostjs bundle and a Go host
// child (built now) that starts its signal at start.
func buildTestApp(root, page string, withGo bool, start int) (*testApp, error) {
	a := &testApp{dir: filepath.Join(root, "app")}
	files := map[string]string{
		"dist/index.html":            page,
		"dist/tasks/[id]/index.html": "<!doctype html><title>task</title>task page",
		"dist/404.html":              "<!doctype html>not found page",
		"host/host.js":               hostJSv1,
		"host/host.js.map":           `{"version":3,"sources":[],"names":[],"mappings":""}`,
		"dist/scoped.html":           scopedPage,
	}
	for name, content := range files {
		if err := writeFile(filepath.Join(a.dir, filepath.FromSlash(name)), content); err != nil {
			return nil, err
		}
	}
	man := map[string]any{
		"appId": "dev.krate.shelltest", "productName": "Shell Test", "version": "1.0.0",
		"window":    map[string]any{"title": "Shell Test", "width": 640, "height": 480, "resizable": true, "path": "/", "keepAlive": true},
		"assets":    "dist",
		"fallbacks": []map[string]string{{"pattern": "/tasks/[id]", "file": "tasks/[id]/index.html"}},
		"hostjs":    map[string]any{"bundle": "host/host.js", "map": "host/host.js.map"},
	}
	if withGo {
		bin, err := a.buildGoHost(start)
		if err != nil {
			return nil, err
		}
		a.binary = bin
		man["gohost"] = map[string]any{
			"binary":  "gohost/krate-gohost",
			"funcs":   []string{"t.Version", "t.Add", "t.Rev", "t.Wait", "t.Bump"},
			"streams": []string{"t.Count"},
			"signals": []string{"t.Count"},
		}
	}
	b, _ := json.MarshalIndent(man, "", "  ")
	if err := writeFile(filepath.Join(a.dir, ManifestFile), string(b)); err != nil {
		return nil, err
	}
	return a, nil
}

// buildGoHost (re)builds the child at gohost/krate-gohost with the given
// version number; the file is replaced by rename, as a rebuild would.
func (a *testApp) buildGoHost(start int) (string, error) {
	scratch := filepath.Join(filepath.Dir(a.dir), "build")
	out := filepath.Join(a.dir, "gohost", "krate-gohost")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	tmp, err := gohosttest.Build(scratch, "next", gohosttest.ChildSource(fmt.Sprintf("v%d", start), start))
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp, out); err != nil {
		return "", err
	}
	return out, nil
}

// frameReq is a one-call request frame with optional blobs.
func frameReq(fn string, build func(*wire.Encoder)) []byte {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String(fn)
	e.Key("args")
	e.BeginArray()
	if build != nil {
		build(e)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	return e.Frame()
}

// reportSink collects the reports the test page posts.
type reportSink struct {
	mu      sync.Mutex
	ch      chan map[string]any
	pending []map[string]any
	indexes int
}

func newReportSink() *reportSink { return &reportSink{ch: make(chan map[string]any, 256)} }

func (r *reportSink) handle(w http.ResponseWriter, req *http.Request) bool {
	switch {
	case req.URL.Path == "/__test/report" && req.Method == http.MethodPost:
		var m map[string]any
		if err := json.NewDecoder(req.Body).Decode(&m); err != nil {
			m = map[string]any{"kind": "decode-error", "error": err.Error()}
		}
		r.ch <- m
		w.WriteHeader(http.StatusNoContent)
		return true
	case req.URL.Path == "/__test/ping":
		fmt.Fprint(w, "pong")
		return true
	case req.URL.Path == "/" || req.URL.Path == "/index.html":
		r.mu.Lock()
		r.indexes++
		r.mu.Unlock()
	}
	return false
}

func (r *reportSink) indexLoads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.indexes
}

// wait returns the first report (pending or new) of the given kind that
// satisfies ok.
func (r *reportSink) wait(t testing.TB, kind string, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(45 * time.Second)
	for {
		for i, m := range r.pending {
			if m["kind"] == kind && (ok == nil || ok(m)) {
				r.pending = append(r.pending[:i], r.pending[i+1:]...)
				return m
			}
		}
		select {
		case m := <-r.ch:
			r.pending = append(r.pending, m)
		case <-deadline:
			t.Fatalf("timed out waiting for a %q report (pending %v)", kind, r.pending)
			return nil
		}
	}
}
