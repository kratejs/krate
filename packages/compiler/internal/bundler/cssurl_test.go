package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCSSUrlAssetsRewritten verifies relative url(...) references in a CSS file
// are content-hashed, registered as assets, and rewritten to /assets/.
func TestCSSUrlAssetsRewritten(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "logo.png"), []byte("PNGDATA"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "styles.css"), []byte(".a{background:url(./assets/logo.png) no-repeat}"), 0644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "entry.tsx")
	if err := os.WriteFile(entry, []byte("import './styles.css';\nexport default function App(){return <div/>;}"), 0644); err != nil {
		t.Fatal(err)
	}

	b := New(dir)
	bundle, err := b.Bundle(entry)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	if !strings.Contains(bundle.CSS, "/assets/logo-") {
		t.Errorf("css url not rewritten:\n%s", bundle.CSS)
	}
	found := false
	for _, u := range bundle.AssetFiles {
		if strings.HasPrefix(u, "/assets/logo-") {
			found = true
		}
	}
	if !found {
		t.Errorf("css-referenced asset not registered: %v", bundle.AssetFiles)
	}
}
