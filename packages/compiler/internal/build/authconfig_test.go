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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	// A build writes the artifact of the runtime that serves the route.
	for label, runtime := range map[string]string{"quickjs": "", "node": "node"} {
		b, root := compatProject(t, map[string]string{"src/api/probe.ts": authProbe}, func(c *config.Config) {
			activeAuth(c)
			c.SSR.APIRuntime = runtime
		})
		if err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "probe.ts")}); err != nil {
			t.Fatal(err)
		}
		dir := apiBundleDir(b.Cfg.OutDir)
		if runtime != "" {
			dir = apiNodeBundleDir(b.Cfg.OutDir)
		}
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

// TestAuthConfigWithoutProvidersIsDelivered: a block that only tunes pages
// and sessions, for providers the app defines in code, still reaches routes.
func TestAuthConfigWithoutProvidersIsDelivered(t *testing.T) {
	t.Parallel()
	trust := true
	b, root := compatProject(t, map[string]string{"src/api/probe.ts": authProbe}, func(c *config.Config) {
		c.Auth = config.AuthConfig{TrustHost: &trust, Pages: config.AuthPages{SignIn: "/login"}, Session: config.AuthSession{MaxAge: 3600}}
	})
	if err := b.CompileAPIRoutes([]string{filepath.Join(root, "src", "api", "probe.ts")}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(apiBundleDir(b.Cfg.OutDir), "probe.js"))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, `globalThis.__KRATE_AUTH_CONFIG__={`) || !strings.Contains(s, `"/login"`) || !strings.Contains(s, `"maxAge":3600`) {
		t.Errorf("the auth block did not reach the route:\n%s", s)
	}
}

// TestAuthConfigReachesServerPages: auth() in a server-rendered page reads the
// same config as the routes.
func TestAuthConfigReachesServerPages(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <h1>home</h1>; }`,
		"src/pages/me.tsx": `import { headers } from "@krate/runtime/server";
export default async function Me() {
  const h = await headers();
  const cfg = (globalThis as { __KRATE_AUTH_CONFIG__?: { pages?: { signIn?: string } } }).__KRATE_AUTH_CONFIG__;
  return <p>{h.get("x") ?? ""}{cfg?.pages?.signIn ?? "none"}</p>;
}`,
	})
	b.Cfg.Auth = config.AuthConfig{Pages: config.AuthPages{SignIn: "/login"}}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(b.Cfg.OutDir, ".krate", "server-bundles", "me.server.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), `globalThis.__KRATE_AUTH_CONFIG__={`) || !strings.Contains(string(data), `"/login"`) {
		t.Errorf("the page bundle lacks the auth config:\n%.600s", data)
	}
}

func TestServerPageBanner(t *testing.T) {
	t.Parallel()
	if serverPageBanner("", "") != nil {
		t.Error("no base path and no auth must add no banner")
	}
	got := serverPageBanner("/docs", "A;\n")["js"]
	if !strings.HasPrefix(got, "A;\n") || !strings.Contains(got, `"basePath":"/docs"`) {
		t.Errorf("banner = %q", got)
	}
	if got := serverPageBanner("", "A;\n")["js"]; got != "A;\n" {
		t.Errorf("auth-only banner = %q", got)
	}
}

func TestAuthConfigAbsentWhenInactive(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
