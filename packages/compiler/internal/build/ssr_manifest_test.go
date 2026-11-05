package build

import (
	"errors"
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

func TestReadServerManifestErrorKinds(t *testing.T) {
	out := t.TempDir()
	if _, err := readServerManifest(out); !errors.Is(err, errServerManifestMissing) {
		t.Errorf("missing manifest: err = %v, want errServerManifestMissing", err)
	}

	p := writeServerManifest(t, out, `{"pages": [`)
	_, err := readServerManifest(out)
	if err == nil || errors.Is(err, errServerManifestMissing) || !strings.Contains(err.Error(), p) || !strings.Contains(err.Error(), "krate build") {
		t.Errorf("corrupt manifest: err = %v, want a non-missing error naming %s with a hint", err, p)
	}

	// A directory where the file should be is unreadable, not absent.
	dirOut := t.TempDir()
	if err := os.MkdirAll(ServerManifestPath(dirOut), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readServerManifest(dirOut); err == nil || errors.Is(err, errServerManifestMissing) {
		t.Errorf("unreadable manifest: err = %v, want a non-missing error", err)
	}

	writeServerManifest(t, out, `{"pages":[{"route":"/a","mode":"ssr"}]}`)
	m, err := readServerManifest(out)
	if err != nil || len(m.Pages) != 1 {
		t.Errorf("valid manifest: %v, %+v", err, m)
	}
}

// A reload that hits a corrupt manifest keeps the last good one.
func TestSSRServerKeepsLastGoodManifest(t *testing.T) {
	out := t.TempDir()
	writeServerManifest(t, out, `{"pages":[{"route":"/a","mode":"ssr"}]}`)
	s := NewSSRServer(t.TempDir(), 0, "node")
	s.SetOutDir(out)
	if err := s.LoadManifest(); err != nil {
		t.Fatal(err)
	}
	if !s.NeedsNodeRenderer() {
		t.Fatal("expected the ssr page to need the renderer")
	}

	writeServerManifest(t, out, `{"pages": [`)
	err := s.LoadManifest()
	if err == nil || errors.Is(err, errServerManifestMissing) {
		t.Fatalf("LoadManifest on corrupt file: %v", err)
	}
	if !s.HasManifest() || !s.NeedsNodeRenderer() {
		t.Error("a failed reload must keep the last good manifest")
	}
	s.InvalidateDevRoutes(nil)
	if !s.HasManifest() || !s.NeedsNodeRenderer() {
		t.Error("InvalidateDevRoutes must keep the last good manifest")
	}
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
