package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func baseCfg() *config.Config {
	off := false
	cfg := config.Default()
	cfg.BasePath = "/repo"
	cfg.Redirects = []config.Redirect{
		{Source: "/old", Destination: "/new", Permanent: true},
		{Source: "/ext", Destination: "https://example.com/x"},
		{Source: "/legacy/*", Destination: "/docs/:splat"},
		{Source: "/outside", Destination: "/elsewhere", BasePath: &off},
	}
	cfg.Rewrites = []config.Rewrite{{Source: "/api/v1/*", Destination: "/api/:splat"}}
	cfg.Headers = []config.HeaderRule{{Source: "/chunks/*", Headers: map[string]string{"Cache-Control": "public, max-age=31536000, immutable"}}}
	return cfg
}

func TestNetlifyRulesUnderBasePath(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Emit("netlify", baseCfg(), root, out); err != nil {
		t.Fatal(err)
	}
	red, _ := os.ReadFile(filepath.Join(out, "_redirects"))
	for _, want := range []string{
		"/repo/old\t/repo/new\t301\n",
		"/repo/ext\thttps://example.com/x\t302\n",
		"/repo/legacy/*\t/repo/docs/:splat\t302\n",
		"/repo/api/v1/*\t/repo/api/:splat\t200\n",
		"\n/outside\t/elsewhere\t302\n",
	} {
		if !strings.Contains(string(red), want) {
			t.Errorf("_redirects lacks %q:\n%s", want, red)
		}
	}
	hdr, _ := os.ReadFile(filepath.Join(out, "_headers"))
	if !strings.HasPrefix(string(hdr), "/repo/chunks/*\n") {
		t.Errorf("_headers should match the prefixed path:\n%s", hdr)
	}
}

func TestVercelStaticRulesUnderBasePath(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := Emit("vercel", baseCfg(), root, out)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(files[0])
	var doc struct {
		Redirects []struct{ Source, Destination string } `json:"redirects"`
		Rewrites  []struct{ Source, Destination string } `json:"rewrites"`
		Headers   []struct{ Source string }              `json:"headers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Redirects[0].Source != "/repo/old" || doc.Redirects[0].Destination != "/repo/new" ||
		doc.Redirects[1].Destination != "https://example.com/x" ||
		doc.Redirects[2].Source != "/repo/legacy/:path*" || doc.Redirects[2].Destination != "/repo/docs/:path*" ||
		doc.Redirects[3].Source != "/outside" || doc.Redirects[3].Destination != "/elsewhere" {
		t.Errorf("redirects: %+v", doc.Redirects)
	}
	if doc.Rewrites[0].Source != "/repo/api/v1/:path*" || doc.Rewrites[0].Destination != "/repo/api/:path*" {
		t.Errorf("rewrites: %+v", doc.Rewrites)
	}
	if doc.Headers[0].Source != "/repo/chunks/:path*" && doc.Headers[0].Source != "/repo/chunks/*" {
		t.Errorf("headers: %+v", doc.Headers)
	}
}

func TestVercelServerRoutesUnderBasePath(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(out, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "assets", "a.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &ServerPlan{ServerPaths: []string{"/blog/*", "/api/*"}}
	if _, err := emitVercelServer(baseCfg(), root, out, plan, []byte("//bundle")); err != nil {
		t.Fatal(err)
	}
	vout := filepath.Join(root, ".vercel", "output")
	if _, err := os.Stat(filepath.Join(vout, "static", "repo", "assets", "a.js")); err != nil {
		t.Errorf("static files belong under the base path: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(vout, "config.json"))
	var conf struct {
		Routes []map[string]any `json:"routes"`
	}
	if err := json.Unmarshal(raw, &conf); err != nil {
		t.Fatal(err)
	}
	var srcs []string
	for _, r := range conf.Routes {
		if s, ok := r["src"].(string); ok {
			srcs = append(srcs, s)
		}
	}
	all := strings.Join(srcs, "\n")
	for _, want := range []string{"^/repo/chunks/.*$", "^/repo/old$", "^/repo/blog/.*$", "^/repo/api(?:/.*)?$", "^/__krate(?:/.*)?$", "^/outside$"} {
		if !strings.Contains(all, want) {
			t.Errorf("routes lack %q:\n%s", want, all)
		}
	}
	for _, r := range conf.Routes {
		if r["src"] == "^/repo/old$" {
			loc := r["headers"].(map[string]any)["Location"]
			if loc != "/repo/new" {
				t.Errorf("redirect target %v", loc)
			}
		}
		if r["src"] == "^/outside$" {
			if loc := r["headers"].(map[string]any)["Location"]; loc != "/elsewhere" {
				t.Errorf("basePath: false redirect target %v", loc)
			}
		}
	}
}

func TestNoBasePathLeavesRulesAlone(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := baseCfg()
	cfg.BasePath = ""
	if _, err := Emit("netlify", cfg, root, out); err != nil {
		t.Fatal(err)
	}
	red, _ := os.ReadFile(filepath.Join(out, "_redirects"))
	if !strings.Contains(string(red), "/old\t/new\t301\n") || strings.Contains(string(red), "/repo") {
		t.Errorf("a root site keeps its rules:\n%s", red)
	}
}

func TestGitHubPagesWarnsOnCustomDomainWithBasePath(t *testing.T) {
	out := t.TempDir()
	if w := GitHubPagesWarnings(&config.Config{BasePath: "/repo"}, out); len(w) != 0 {
		t.Errorf("no CNAME should not warn: %v", w)
	}
	if err := os.WriteFile(filepath.Join(out, "CNAME"), []byte("example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := GitHubPagesWarnings(&config.Config{}, out); len(w) != 0 {
		t.Errorf("a root site should not warn: %v", w)
	}
	for _, cfg := range []*config.Config{{BasePath: "/repo"}, {Site: config.SiteConfig{URL: "https://example.com/repo"}}} {
		w := GitHubPagesWarnings(cfg, out)
		if len(w) != 1 || !strings.Contains(w[0], `"/repo"`) {
			t.Errorf("want one warning naming the base path, got %v", w)
		}
	}
}
