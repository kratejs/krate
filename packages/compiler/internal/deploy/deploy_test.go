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
	files, err := Emit("netlify", testConfig(), out, out)
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
	if _, err := Emit("vercel", testConfig(), out, out); err != nil {
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
