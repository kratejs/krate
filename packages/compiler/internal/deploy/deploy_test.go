package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Redirects = []config.Redirect{
		{Source: "/old/", Destination: "/new/", Permanent: true},
		{Source: "/temp", Destination: "/x", Permanent: false},
	}
	cfg.Rewrites = []config.Rewrite{{Source: "/app/*", Destination: "/app.html"}}
	return cfg
}

func TestEmitNetlifyRedirects(t *testing.T) {
	out := t.TempDir()
	files, err := Emit("netlify", testConfig(), out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	data, err := os.ReadFile(filepath.Join(out, "_redirects"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"/old/\t/new/\t301", "/temp\t/x\t302", "/app/*\t/app.html\t200"} {
		if !strings.Contains(got, want) {
			t.Errorf("_redirects missing %q:\n%s", want, got)
		}
	}
}

func TestEmitVercel(t *testing.T) {
	out := t.TempDir()
	if _, err := Emit("vercel", testConfig(), out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "vercel.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Redirects []struct {
			Source      string `json:"source"`
			Destination string `json:"destination"`
			Permanent   bool   `json:"permanent"`
		} `json:"redirects"`
		Rewrites []struct {
			Source      string `json:"source"`
			Destination string `json:"destination"`
		} `json:"rewrites"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("vercel.json invalid: %v\n%s", err, data)
	}
	if len(doc.Redirects) != 2 || !doc.Redirects[0].Permanent {
		t.Fatalf("redirects = %+v", doc.Redirects)
	}
	if len(doc.Rewrites) != 1 || doc.Rewrites[0].Destination != "/app.html" {
		t.Fatalf("rewrites = %+v", doc.Rewrites)
	}
}

func TestEmitGitHubPages(t *testing.T) {
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, "404"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "404", "index.html"), []byte("<h1>404</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Emit("gh-pages", config.Default(), out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected .nojekyll + 404.html, got %v", files)
	}
	if _, err := os.Stat(filepath.Join(out, ".nojekyll")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "404.html"))
	if err != nil || string(data) != "<h1>404</h1>" {
		t.Fatalf("404.html = %q, err=%v", data, err)
	}
}

func TestEmitUnknownTarget(t *testing.T) {
	if _, err := Emit("nowhere", config.Default(), t.TempDir()); err == nil {
		t.Fatal("expected error for unknown target")
	}
}
