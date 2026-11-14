package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const authProbe = `export function GET() {
  const cfg = (globalThis as { __KRATE_AUTH_CONFIG__?: unknown }).__KRATE_AUTH_CONFIG__ ?? null;
  return Response.json({ cfg });
}
`

func activeAuth(c *config.Config) {
	c.Auth = config.AuthConfig{
		Providers: []config.AuthProvider{{ID: "github"}},
		BasePath:  "/api/auth",
		Pages:     config.AuthPages{SignIn: "/login"},
	}
}

func TestAuthBanner(t *testing.T) {
	if authBanner(nil) != "" || authBanner(config.Default()) != "" {
		t.Fatal("inactive auth must add no banner")
	}
	cfg := config.Default()
	activeAuth(cfg)
	b := authBanner(cfg)
	if !strings.HasPrefix(b, "globalThis.__KRATE_AUTH_CONFIG__={") || !strings.Contains(b, `"github"`) || !strings.HasSuffix(b, ";\n") {
		t.Fatalf("banner = %q", b)
	}
}

func TestAuthBannerKeepsExistingBanner(t *testing.T) {
	cfg := config.Default()
	activeAuth(cfg)
	opts := apiBuildOptionsForTest("const keep = 1;")
	withAuthConfig(&opts, cfg)
	js := opts.Banner["js"]
	if !strings.Contains(js, "__KRATE_AUTH_CONFIG__") || !strings.HasSuffix(js, "const keep = 1;") {
		t.Fatalf("banner = %q", js)
	}
}

func TestAuthConfigReachesBothAPIArtifacts(t *testing.T) {
	b, root := compatProject(t, map[string]string{"src/api/probe.ts": authProbe}, activeAuth)
	if err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "probe.ts")}); err != nil {
		t.Fatal(err)
	}
	for label, dir := range map[string]string{"quickjs": apiBundleDir(b.Cfg.OutDir), "node": apiNodeBundleDir(b.Cfg.OutDir)} {
		data, err := os.ReadFile(filepath.Join(dir, "probe.js"))
		if err != nil {
			t.Fatalf("%s artifact: %v", label, err)
		}
		s := string(data)
		if !strings.Contains(s, `globalThis.__KRATE_AUTH_CONFIG__={`) || !strings.Contains(s, `"/login"`) {
			t.Errorf("%s artifact does not carry the auth config:\n%s", label, s)
		}
	}
}

func TestAuthConfigAbsentWhenInactive(t *testing.T) {
	b, root := compatProject(t, map[string]string{"src/api/probe.ts": authProbe}, nil)
	if err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "probe.ts")}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(apiBundleDir(b.Cfg.OutDir), "probe.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "globalThis.__KRATE_AUTH_CONFIG__=") {
		t.Errorf("inactive auth leaked a config banner:\n%s", data)
	}
}

func TestAuthConfigReachesMiddleware(t *testing.T) {
	b, root := compatProject(t, map[string]string{
		"middleware.ts": `export function middleware() {
  return new Response(JSON.stringify((globalThis as { __KRATE_AUTH_CONFIG__?: unknown }).__KRATE_AUTH_CONFIG__));
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
	if !strings.Contains(string(data), `globalThis.__KRATE_AUTH_CONFIG__={`) || !strings.Contains(string(data), `"discord"`) {
		t.Errorf("middleware bundle lacks the auth config:\n%s", data)
	}
}

func apiBuildOptionsForTest(banner string) api.BuildOptions {
	return api.BuildOptions{Banner: map[string]string{"js": banner}}
}
