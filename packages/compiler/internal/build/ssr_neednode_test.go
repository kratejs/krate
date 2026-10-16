package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSSRServerNeedsNodeRenderer verifies the decision that gates starting the
// Node/bun SSR renderer: only SSR/ISR pages (coarse page region) or non-Go
// regions require it. A Go-only streaming site needs no Node renderer.
func TestSSRServerNeedsNodeRenderer(t *testing.T) {
	write := func(t *testing.T, pages []ManifestPage, regions map[string][]RegionMeta) *SSRServer {
		t.Helper()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
			t.Fatal(err)
		}
		m := ServerManifest{Pages: pages, Regions: regions}
		data, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(root, "dist", "server-manifest.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		s := NewSSRServer(root, 0, "node")
		if err := s.LoadManifest(); err != nil {
			t.Fatalf("LoadManifest: %v", err)
		}
		return s
	}

	streamingGo := write(t,
		[]ManifestPage{{Route: "/", Mode: "streaming"}},
		map[string][]RegionMeta{"/": {{ID: "r1", Runtime: "go"}}},
	)
	if streamingGo.NeedsNodeRenderer() {
		t.Error("a Go-only streaming site must not need the Node renderer")
	}

	streamingTsx := write(t,
		[]ManifestPage{{Route: "/", Mode: "streaming"}},
		map[string][]RegionMeta{"/": {{ID: "r1", Runtime: ""}}},
	)
	if !streamingTsx.NeedsNodeRenderer() {
		t.Error("a TSX runtime region must need the Node renderer")
	}

	ssrPage := write(t, []ManifestPage{{Route: "/", Mode: "ssr"}}, nil)
	if !ssrPage.NeedsNodeRenderer() {
		t.Error("an SSR page must need the Node renderer")
	}

	noManifest := NewSSRServer(t.TempDir(), 0, "node")
	if noManifest.HasManifest() {
		t.Error("HasManifest should be false without a manifest")
	}
	if noManifest.NeedsNodeRenderer() {
		t.Error("NeedsNodeRenderer should be false without a manifest")
	}
}
