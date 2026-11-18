package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A next.config style regex parameter becomes a route regex on Vercel's
// server output, with the destination pointing at its group.
func TestRuleRegexWithRegexParameter(t *testing.T) {
	src, dst := ruleRegex(`/store/:slug((?!compare$)[^/]+)`, "/templates/:slug")
	if dst != "/templates/$p0" {
		t.Errorf("dest = %q", dst)
	}
	// Go's regexp has no lookahead; check the shape and a lookahead-free case.
	if src != `^/store/(?<p0>(?!compare$)[^/]+)/?$` {
		t.Errorf("src = %q", src)
	}
	src, dst = ruleRegex(`/p/:id(\d+)/:path*`, "/q/:id/:path*")
	re := regexp.MustCompile(src)
	m := re.FindStringSubmatch("/p/42/a/b")
	if m == nil || m[re.SubexpIndex("p0")] != "42" || m[re.SubexpIndex("p1")] != "a/b" || dst != "/q/$p0/$p1" {
		t.Errorf("src %q dest %q match %q", src, dst, m)
	}
}

// A rule's statusCode reaches _redirects and vercel.json (where it replaces
// permanent), and a regex rule is left out of _redirects.
func TestRedirectStatusCodeOnHosts(t *testing.T) {
	cfg := testConfig()
	cfg.Redirects = append(cfg.Redirects,
		config.Redirect{Source: "/moved", Destination: "/new", Permanent: true, StatusCode: 308},
		config.Redirect{Source: "/store/:slug((?!compare$)[^/]+)", Destination: "/t/:slug"},
	)
	out := t.TempDir()
	if _, err := Emit("netlify", cfg, out, out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "_redirects"))
	if !strings.Contains(string(data), "/moved\t/new\t308") || strings.Contains(string(data), "/store/") {
		t.Errorf("_redirects:\n%s", data)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Emit("vercel", cfg, root, filepath.Join(root, "dist")); err != nil {
		t.Fatal(err)
	}
	vj, _ := os.ReadFile(filepath.Join(root, "vercel.json"))
	compact := strings.Join(strings.Fields(string(vj)), "")
	if !strings.Contains(compact, `"source":"/moved","destination":"/new","statusCode":308}`) || !strings.Contains(compact, `"source":"/temp","destination":"/x","permanent":false}`) {
		t.Errorf("vercel.json:\n%s", vj)
	}
}
