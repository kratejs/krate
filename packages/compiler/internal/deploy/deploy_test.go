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

// TestVercelTrailingSlashFromConfig maps the top-level trailingSlash setting
// onto vercel.json; an explicit deploy option still wins.
func TestVercelTrailingSlashFromConfig(t *testing.T) {
	read := func(cfg *config.Config) map[string]any {
		out := t.TempDir()
		if _, err := Emit("vercel", cfg, out, out); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(out, "vercel.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	for _, c := range []struct {
		mode string
		want any
	}{{"", nil}, {"ignore", nil}, {"always", true}, {"never", false}} {
		cfg := config.Default()
		cfg.TrailingSlash = c.mode
		if got := read(cfg)["trailingSlash"]; got != c.want {
			t.Errorf("trailingSlash %q: vercel.json trailingSlash = %v, want %v", c.mode, got, c.want)
		}
	}
	cfg := config.Default()
	cfg.TrailingSlash = "never"
	cfg.Deploy.Options = map[string]string{"trailingSlash": "true"}
	if read(cfg)["trailingSlash"] != true {
		t.Error("deploy.options.trailingSlash must override the top-level setting")
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
