package jsruntime

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestAPIWorkersPreloadScannedFeatures checks pooled workers install the
// runner's features plus the scanned ones, isolated VMs only the runner's,
// and a build without feature lists everything.
func TestAPIWorkersPreloadScannedFeatures(t *testing.T) {
	dir := t.TempDir()
	code := `var __krate_route = (() => { function GET() { return Response.json({ ok: true }); } return { GET }; })();`
	if err := os.WriteFile(filepath.Join(dir, "r.js"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	req := APIRequest{Method: "GET", Path: "/api/r", Headers: map[string][]string{}}
	for _, tc := range []struct {
		name      string
		features  []string
		known     bool
		isolation bool
		want      []string
	}{
		{"scanned", []string{"crypto", "node"}, true, false, []string{"crypto", "events", "fetch", "url"}},
		{"unknown", nil, false, false, PreludeFeatures()},
		{"isolation", []string{"crypto"}, true, true, []string{"events", "fetch", "url"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAPIRouteRuntime(dir)
			a.SetPool(1, tc.isolation)
			a.SetFeatures(tc.features, tc.known)
			defer a.Close()
			w, err := a.acquireWorker()
			if err != nil {
				t.Fatal(err)
			}
			got := w.rt.LoadedFeatures()
			a.releaseWorker(w)
			if want := sorted(tc.want); !slices.Equal(got, want) {
				t.Errorf("worker features %v, want %v", got, want)
			}
			if res := a.Execute(req); res.Status != 200 {
				t.Errorf("status %d: %s", res.Status, res.Body)
			}
		})
	}
}

// TestMiddlewarePreloadsScannedFeatures checks middleware VMs read the
// feature list the build writes next to middleware.js.
func TestMiddlewarePreloadsScannedFeatures(t *testing.T) {
	for _, tc := range []struct {
		name string
		meta string
		want []string
	}{
		{"scanned", `{"features": ["url"]}`, []string{"events", "fetch", "url"}},
		{"empty", `{"features": []}`, []string{"events", "fetch", "url"}},
		{"unknown", `{}`, PreludeFeatures()},
		{"missing", "", PreludeFeatures()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "middleware.js")
			if err := os.WriteFile(p, []byte(`function middleware(req) { return; }`), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.meta != "" {
				if err := os.WriteFile(filepath.Join(dir, "middleware.json"), []byte(tc.meta), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			m, err := NewMiddlewareRuntime(p)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if got, want := m.idle[0].rt.LoadedFeatures(), sorted(tc.want); !slices.Equal(got, want) {
				t.Errorf("middleware features %v, want %v", got, want)
			}
			if res := m.Execute(MiddlewareRequest{Path: "/"}); res.Action != MiddlewareContinue {
				t.Errorf("action %s: %s", res.Action, res.Error)
			}
		})
	}
}
