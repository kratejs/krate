package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func writeServerManifest(t *testing.T, out, content string) string {
	t.Helper()
	p := ServerManifestPath(out)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A corrupt server manifest stops startup instead of being read as "no SSR
// pages", which would serve the frozen build HTML.
func TestServeRefusesCorruptServerManifest(t *testing.T) {
	root, out := guardProject(t, false, "false")
	p := writeServerManifest(t, out, `{"pages": [`)
	cfg := config.Default()
	cfg.OutDir = out
	done := make(chan error, 1)
	go func() { done <- Serve(root, cfg, time.Now()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), p) {
			t.Fatalf("Serve error = %v, want an error naming %s", err, p)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve started with a corrupt server manifest")
	}
}
