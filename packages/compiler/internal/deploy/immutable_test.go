package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// hashedSite writes a small build output with a manifest listing its hashed
// assets: one all-hashed directory, hashed files beside HTML, and an unhashed
// user file that merely looks hashed.
func hashedSite(t *testing.T) string {
	t.Helper()
	out := t.TempDir()
	files := []string{
		"index.html", "styles.abc12345.css", "styles.abc12345.css.br",
		"chunks/a.deadbeef.js", "chunks/b.deadbeef.js", "chunks/a.deadbeef.js.map",
		"docs/index.html", "docs/index.cafe1234.js",
		"team-leader.png", "robots.txt",
	}
	for _, f := range files {
		p := filepath.Join(out, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := json.Marshal(map[string]any{"hashedAssets": []string{
		"/styles.abc12345.css", "/chunks/a.deadbeef.js", "/chunks/b.deadbeef.js",
		"/chunks/a.deadbeef.js.map", "/docs/index.cafe1234.js",
	}})
	if err := os.MkdirAll(filepath.Join(out, ".krate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, ".krate", "manifest.json"), m, 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestImmutableRulesCompact(t *testing.T) {
	out := hashedSite(t)
	rules := immutableRules(config.Default(), out, LoadHashedAssets(out)).rules
	var got []string
	for _, r := range rules {
		if r.Headers["Cache-Control"] != ImmutableCacheControl {
			t.Errorf("rule %q has headers %v", r.Source, r.Headers)
		}
		got = append(got, r.Source)
	}
	want := []string{"/chunks/*", "/docs/index.cafe1234.js", "/styles.abc12345.css"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sources = %v, want %v", got, want)
	}
}

func TestNetlifyHeadersImmutableAndUserRules(t *testing.T) {
	out := hashedSite(t)
	cfg := config.Default()
	cfg.Headers = []config.HeaderRule{
		{Source: "/*", Headers: map[string]string{"X-Frame-Options": "DENY"}},
		{Source: "/chunks/b.deadbeef.js", Headers: map[string]string{"Cache-Control": "no-store"}},
	}
	if _, err := Emit("netlify", cfg, out, out, Options{StaticOnly: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "_headers"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"/styles.abc12345.css\n  Cache-Control: " + ImmutableCacheControl,
		"/docs/index.cafe1234.js\n  Cache-Control: " + ImmutableCacheControl,
		"/*\n  X-Frame-Options: DENY",
		"/chunks/b.deadbeef.js\n  Cache-Control: no-store",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("_headers missing %q:\n%s", want, text)
		}
	}
	// The user's Cache-Control covers b.js, so the directory rule is expanded
	// and leaves that file out.
	if strings.Contains(text, "/chunks/*") || strings.Contains(text, "/chunks/b.deadbeef.js\n  Cache-Control: "+ImmutableCacheControl) {
		t.Errorf("immutable rule must not cover a file with a user Cache-Control:\n%s", text)
	}
	if !strings.Contains(text, "/chunks/a.deadbeef.js\n  Cache-Control: "+ImmutableCacheControl) {
		t.Errorf("a.js should stay immutable:\n%s", text)
	}
	assertNoImmutableOnHTML(t, text)
}

func assertNoImmutableOnHTML(t *testing.T, text string) {
	t.Helper()
	for _, block := range strings.Split(text, "\n\n") {
		first := strings.SplitN(block, "\n", 2)[0]
		if strings.Contains(block, "immutable") && (strings.HasSuffix(first, ".html") || first == "/*" || first == "/" || strings.HasSuffix(first, "/")) {
			t.Errorf("html-reachable path %q is immutable:\n%s", first, text)
		}
	}
}

func TestHeadersUnderBasePath(t *testing.T) {
	out := hashedSite(t)
	cfg := config.Default()
	cfg.BasePath = "/app"
	if _, err := Emit("cloudflare", cfg, out, out, Options{StaticOnly: true}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "_headers"))
	if !strings.Contains(string(data), "/app/chunks/*\n  Cache-Control: "+ImmutableCacheControl) {
		t.Errorf("rules must sit under the base path:\n%s", data)
	}
}

func TestVercelImmutableHeaders(t *testing.T) {
	out := hashedSite(t)
	root := t.TempDir()
	cfg := config.Default()
	cfg.Headers = []config.HeaderRule{{Source: "/chunks/*", Headers: map[string]string{"Cache-Control": "no-cache"}}}
	if _, err := Emit("vercel", cfg, root, out, Options{StaticOnly: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "vercel.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Headers []struct {
			Source  string
			Headers []struct{ Key, Value string }
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	cc := map[string]string{}
	for _, h := range doc.Headers {
		for _, kv := range h.Headers {
			if kv.Key == "Cache-Control" {
				cc[h.Source] = kv.Value
			}
		}
	}
	if cc["/styles.abc12345.css"] != ImmutableCacheControl || cc["/docs/index.cafe1234.js"] != ImmutableCacheControl {
		t.Errorf("hashed files must be immutable: %v", cc)
	}
	if cc["/chunks/:path*"] != "no-cache" {
		t.Errorf("user rule must be kept: %v", cc)
	}
	for src, v := range cc {
		if strings.Contains(src, "chunks/a.") || strings.Contains(src, "chunks/b.") {
			t.Errorf("chunks files are covered by the user rule, got %s = %s", src, v)
		}
		if v == ImmutableCacheControl && (strings.HasSuffix(src, ".html") || src == "/:path*") {
			t.Errorf("html must not be immutable: %s", src)
		}
	}
}

func TestNoManifestNoImmutableRules(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "app.abc12345.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Emit("netlify", config.Default(), out, out, Options{StaticOnly: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "_headers")); err == nil {
		t.Error("a file that merely looks hashed must not get a rule without a manifest")
	}
}

func TestCloudflareHeaderRuleLimit(t *testing.T) {
	out := t.TempDir()
	var hashed []string
	for i := 0; i < 150; i++ {
		dir := filepath.Join(out, "p"+string(rune('a'+i%26))+string(rune('a'+i/26)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		name := "index.abcd" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".js"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		hashed = append(hashed, "/"+filepath.Base(dir)+"/"+name)
	}
	m, _ := json.Marshal(map[string]any{"hashedAssets": hashed})
	_ = os.MkdirAll(filepath.Join(out, ".krate"), 0o755)
	_ = os.WriteFile(filepath.Join(out, ".krate", "manifest.json"), m, 0o644)

	count := func(target string) (int, string) {
		cfg := config.Default()
		cfg.Headers = []config.HeaderRule{{Source: "/*", Headers: map[string]string{"X-Frame-Options": "DENY"}}}
		if _, err := Emit(target, cfg, out, out, Options{StaticOnly: true}); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(filepath.Join(out, "_headers"))
		return strings.Count(string(data), ": "+ImmutableCacheControl), string(data)
	}
	// Emit removes .krate, so restore the manifest for the second run.
	n, text := count("cloudflare")
	if n != 99 || !strings.Contains(text, "# 51 hashed assets are not listed") {
		t.Errorf("cloudflare: %d immutable rules, want 99 plus a note:\n%.300s", n, text)
	}
	_ = os.MkdirAll(filepath.Join(out, ".krate"), 0o755)
	_ = os.WriteFile(filepath.Join(out, ".krate", "manifest.json"), m, 0o644)
	if n, _ := count("netlify"); n != 150 {
		t.Errorf("netlify: %d immutable rules, want 150", n)
	}
}
