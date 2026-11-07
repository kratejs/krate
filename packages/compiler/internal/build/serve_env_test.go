package build

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/environ"
)

// Routes and middleware must see the shell environment plus the .env files,
// the shell winning, on every runtime.
func TestServeEnvReachesRoutesAndMiddleware(t *testing.T) {
	for _, rt := range []string{"quickjs", "node"} {
		t.Run(rt, func(t *testing.T) {
			e2eEnv(t)
			t.Setenv("KRATE_TEST_SHELL", "from-shell")
			t.Setenv("KRATE_TEST_BOTH", "shell-wins")
			b, root := scProject(t, map[string]string{
				"src/pages/index.tsx": `export default function Home() { return <main><h1>home {process.env.KRATE_PUBLIC_PUB}</h1></main>; }`,
				"src/api/env.ts": `export function GET() {
  return Response.json({
    shell: process.env.KRATE_TEST_SHELL ?? null,
    file: process.env.KRATE_TEST_FILE ?? null,
    both: process.env.KRATE_TEST_BOTH ?? null,
    nodeEnv: process.env.NODE_ENV ?? null,
  });
}`,
				"middleware.ts": `export function middleware(request: Request) {
  if (new URL(request.url).pathname === "/mw-env") {
    return Response.json({ shell: process.env.KRATE_TEST_SHELL ?? null, file: process.env.KRATE_TEST_FILE ?? null, both: process.env.KRATE_TEST_BOTH ?? null });
  }
  return new Response(null, { headers: { "x-middleware-next": "1" } });
}`,
				".env": "KRATE_TEST_FILE=from-file\nKRATE_TEST_BOTH=file-loses\nKRATE_PUBLIC_PUB=pub\nSECRET_TOKEN=do-not-ship\n",
			})
			if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
				t.Fatal(err)
			}
			b.Cfg.SSR.APIRuntime = rt
			b.Cfg.SSR.MiddlewareRuntime = rt
			files, err := environ.Load(root, "production")
			if err != nil {
				t.Fatal(err)
			}
			prev := environ.Current
			environ.Current = files
			t.Cleanup(func() { environ.Current = prev })
			b.Env = files
			if err := b.BuildAll(); err != nil {
				t.Fatalf("BuildAll: %v", err)
			}
			base, err := startKrateServer(t, root, b.Cfg)
			if err != nil {
				t.Fatal(err)
			}
			get := func(path string) map[string]any {
				t.Helper()
				client := &http.Client{Timeout: 20 * time.Second}
				resp, err := client.Get(base + path)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				raw, _ := io.ReadAll(resp.Body)
				var out map[string]any
				if err := json.Unmarshal(raw, &out); err != nil {
					t.Fatalf("%s: %v (%s)", path, err, raw)
				}
				return out
			}
			for _, path := range []string{"/api/env", "/mw-env"} {
				got := get(path)
				if got["shell"] != "from-shell" || got["file"] != "from-file" || got["both"] != "shell-wins" {
					t.Errorf("%s: process.env = %v, want shell + file values with the shell winning", path, got)
				}
			}
			if got := get("/api/env"); got["nodeEnv"] != "production" {
				t.Errorf("NODE_ENV = %v, want production", got["nodeEnv"])
			}
			// Only KRATE_PUBLIC_ values may reach client output.
			err = filepath.Walk(b.Cfg.OutDir, func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return err
				}
				data, _ := os.ReadFile(p)
				for _, secret := range []string{"do-not-ship", "from-shell", "shell-wins"} {
					if strings.Contains(string(data), secret) {
						t.Errorf("%s contains %q", p, secret)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
