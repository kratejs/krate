package build

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// guardProject lays out root/.krate (optionally with middleware.js) and
// root/dist/.krate/manifest.json with the given middleware record ("" omits it).
func guardProject(t *testing.T, withModule bool, record string) (root, out string) {
	t.Helper()
	root = t.TempDir()
	out = filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(out, ".krate"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"pages":[]}`
	if record != "" {
		manifest = `{"pages":[],"middleware":` + record + `}`
	}
	if err := os.WriteFile(filepath.Join(out, ".krate", "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if withModule {
		if err := os.MkdirAll(filepath.Join(root, ".krate"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".krate", "middleware.js"), []byte("export function middleware() {}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, out
}

// Serve must refuse to start when the build recorded middleware that was not
// deployed with dist.
func TestServeRefusesWhenRecordedMiddlewareIsMissing(t *testing.T) {
	root, out := guardProject(t, false, "true")
	cfg := config.Default()
	cfg.OutDir = out
	done := make(chan error, 1)
	go func() { done <- Serve(root, cfg, time.Now()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "middleware.js") || !strings.Contains(err.Error(), ".krate/") {
			t.Fatalf("Serve error = %v, want an actionable missing-middleware error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve started instead of refusing")
	}
}

// A middleware module that exists but fails to import must not read as "no
// middleware" in the Node sidecar.
func TestNodeSidecarMiddlewareLoadErrorFailsClosed(t *testing.T) {
	dir := t.TempDir()
	mwPath := filepath.Join(dir, "middleware.js")
	if err := os.WriteFile(mwPath, []byte("export function middleware( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(dir, "api")
	_ = os.MkdirAll(apiDir, 0o755)
	base := startNodeConformance(t, apiDir, mwPath)
	resp, err := http.Post(base+"/__krate/middleware", "application/json", strings.NewReader(`{"url":"http://x/admin","method":"GET","path":"/admin","headers":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Action string `json:"action"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%v (%s)", err, raw)
	}
	if out.Action != "error" || out.Error == "" {
		t.Errorf("load failure answered %s, want an error envelope", raw)
	}
}
