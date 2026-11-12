package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const authProbe = `declare const __KRATE_AUTH_CONFIG__: unknown;
const cfg = typeof __KRATE_AUTH_CONFIG__ === "undefined" ? null : __KRATE_AUTH_CONFIG__;
export function GET() {
  return Response.json({ cfg });
}
`

func TestAuthDefinesInactive(t *testing.T) {
	got := authDefines(config.Default())
	if got[authConfigGlobal] != "null" {
		t.Fatalf("inactive auth define = %q, want null", got[authConfigGlobal])
	}
	if authDefines(nil)[authConfigGlobal] != "null" {
		t.Fatal("nil config must define null")
	}
}

func TestAuthConfigReachesAPIRoutes(t *testing.T) {
	b, root := compatProject(t, map[string]string{"src/api/probe.ts": authProbe}, func(c *config.Config) {
		c.Auth = config.AuthConfig{
			Providers: []config.AuthProvider{{ID: "github"}},
			BasePath:  "/api/auth",
			Pages:     config.AuthPages{SignIn: "/login"},
		}
	})
	if err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "probe.ts")}); err != nil {
		t.Fatal(err)
	}
	dir := apiBundleDir(b.Cfg.OutDir)
	for _, name := range []string{"probe.js", "probe.mjs"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		s := string(data)
		if !strings.Contains(s, `"github"`) || !strings.Contains(s, `"/login"`) {
			t.Errorf("%s does not carry the auth config:\n%s", name, s)
		}
		if strings.Contains(s, authConfigGlobal) {
			t.Errorf("%s still references %s", name, authConfigGlobal)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "probe.js")); err != nil {
		t.Fatalf("route bundle missing: %v", err)
	}
}

func TestAuthConfigNullWhenInactive(t *testing.T) {
	b, root := compatProject(t, map[string]string{"src/api/probe.ts": authProbe}, nil)
	if err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "probe.ts")}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(apiBundleDir(b.Cfg.OutDir), "probe.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "github") {
		t.Errorf("inactive auth leaked config into the bundle:\n%s", data)
	}
}

func TestAuthConfigReachesMiddleware(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"middleware.ts": `declare const __KRATE_AUTH_CONFIG__: { providers: { id: string }[] } | null;
export function middleware() {
  return new Response(JSON.stringify(__KRATE_AUTH_CONFIG__));
}
`,
	}, func(c *config.Config) {
		c.Auth = config.AuthConfig{Providers: []config.AuthProvider{{ID: "discord"}}}
	})
	if err := b.BuildMiddleware(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".krate", "middleware.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"discord"`) {
		t.Errorf("middleware bundle lacks the auth config:\n%s", data)
	}
}
