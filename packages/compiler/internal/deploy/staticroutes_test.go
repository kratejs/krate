package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestStaticRoutesDeployHeadersAndRewrites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outDir := filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(outDir, ".krate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outDir, "cards"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outDir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{
		"index.html":     "<html></html>",
		"cards/a":        "png-a",
		"cards/b":        "png-b",
		"notes/index.md": "# Notes",
		"notes/intro":    "# Intro",
	} {
		if err := os.WriteFile(filepath.Join(outDir, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srData, _ := json.Marshal([]StaticRouteEntry{
		{URL: "/cards/a", File: "/cards/a", ContentType: "image/png"},
		{URL: "/cards/b", File: "/cards/b", ContentType: "image/png"},
		{URL: "/notes", File: "/notes/index.md", ContentType: "text/markdown; charset=utf-8"},
		{URL: "/notes/intro", File: "/notes/intro", ContentType: "text/markdown; charset=utf-8"},
	})
	if err := os.WriteFile(filepath.Join(outDir, ".krate", "static-routes.json"), srData, 0o644); err != nil {
		t.Fatal(err)
	}

	if feats := ServerFeatures(root, outDir); len(feats) != 0 {
		t.Fatalf("static routes should not count as server features: %v", feats)
	}

	cfg := config.Default()
	if _, err := Emit("cloudflare", cfg, root, outDir); err != nil {
		t.Fatalf("Emit cloudflare: %v", err)
	}
	headers, err := os.ReadFile(filepath.Join(outDir, "_headers"))
	if err != nil {
		t.Fatalf("read _headers: %v", err)
	}
	if !strings.Contains(string(headers), "/cards/*\n  Content-Type: image/png") {
		t.Errorf("_headers missing collapsed /cards/* rule:\n%s", string(headers))
	}
	if !strings.Contains(string(headers), "/notes/intro\n  Content-Type: text/markdown; charset=utf-8") {
		t.Errorf("_headers missing /notes/intro rule:\n%s", string(headers))
	}
	redirects, err := os.ReadFile(filepath.Join(outDir, "_redirects"))
	if err != nil {
		t.Fatalf("read _redirects: %v", err)
	}
	if !strings.Contains(string(redirects), "/notes\t/notes/index.md\t200") {
		t.Errorf("_redirects missing collision rewrite:\n%s", string(redirects))
	}
}
