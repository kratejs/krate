package build

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestReadAPIMaxDuration(t *testing.T) {
	cases := []struct {
		name, src string
		want      int
		warn      string
	}{
		{"literal", "export const maxDuration = 60;\nexport function GET() {}", 60, ""},
		{"folded", "export const maxDuration = 5 * 60;", 300, ""},
		{"absent", "export function GET() {}", 0, ""},
		{"non-literal", "const n = Number(process.env.N);\nexport const maxDuration = n;", 0, "constant, positive whole number"},
		{"fractional", "export const maxDuration = 1.5;", 0, "positive whole number"},
		{"zero", "export const maxDuration = 0;", 0, "positive whole number"},
		{"negative", "export const maxDuration = -5;", 0, "positive whole number"},
		{"capped", "export const maxDuration = 99999;", maxAPIDurationSeconds, "capped at"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "route.ts")
			if err := os.WriteFile(f, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			got, warns := readAPIMaxDuration(f)
			if got != c.want {
				t.Errorf("maxDuration = %d, want %d", got, c.want)
			}
			if c.warn == "" {
				if len(warns) != 0 {
					t.Errorf("unexpected warnings: %v", warns)
				}
				return
			}
			if len(warns) != 1 || !strings.Contains(warns[0].Error(), c.warn) || warns[0].Code != diag.CodeSegmentExportValue {
				t.Errorf("want one KR8601 warning containing %q, got %v", c.warn, warns)
			}
		})
	}
}

func apiTimeoutProject(t *testing.T, runtime string) (string, *config.Config) {
	t.Helper()
	root := t.TempDir()
	const sleep = "await new Promise((r) => setTimeout(r, 2000));\n  return Response.json({ done: true });"
	writeTree(t, root, map[string]string{
		"src/api/limited.ts":  "export const maxDuration = 4;\nexport async function GET() {\n  " + sleep + "\n}",
		"src/api/plain.ts":    "export async function GET() {\n  " + sleep + "\n}",
		"src/api/bad.ts":      "export const maxDuration = Number('9');\nexport async function GET() {\n  " + sleep + "\n}",
		"src/pages/index.tsx": "export default function P() { return <main>home</main>; }",
		"src/api/fast.ts":     "export function GET() { return Response.json({ fast: true }); }",
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PublicDir = filepath.Join(root, "public")
	cfg.SSR.APIRuntime = runtime
	return root, cfg
}

func TestBuildRecordsAPIMaxDuration(t *testing.T) {
	root, cfg := apiTimeoutProject(t, "")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".krate", "api-routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var list []apiRouteMeta
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, m := range list {
		got[m.File] = m.MaxDuration
	}
	want := map[string]int{"limited.js": 4, "plain.js": 0, "bad.js": 0, "fast.js": 0}
	for f, w := range want {
		if g, ok := got[f]; !ok || g != w {
			t.Errorf("%s maxDuration = %d (present %v), want %d", f, g, ok, w)
		}
	}
	if b := apiRouteBudgets(root); len(b) != 1 || b["limited.js"] != 4*time.Second {
		t.Errorf("budgets = %v", b)
	}
}

// TestAPIMaxDurationE2E serves routes on each API runtime with the default
// budget cut to 1s: a route that sleeps 2s fails with 504 unless it raises its
// budget with maxDuration.
func TestAPIMaxDurationE2E(t *testing.T) {
	for _, rt := range []string{"quickjs", "node"} {
		t.Run(rt, func(t *testing.T) {
			if rt == "node" {
				if _, err := exec.LookPath("node"); err != nil {
					requireE2E(t, "node not available: %v", err)
				}
			}
			t.Setenv("KRATE_API_TIMEOUT", "1s")
			root, cfg := apiTimeoutProject(t, rt)
			if err := New(root, cfg).BuildAll(); err != nil {
				t.Fatalf("BuildAll: %v", err)
			}
			base, err := startKrateServer(t, root, cfg)
			if err != nil {
				t.Fatal(err)
			}
			get := func(path string) (int, string, time.Duration) {
				start := time.Now()
				resp, err := http.Get(base + path)
				if err != nil {
					t.Fatalf("GET %s: %v", path, err)
				}
				defer resp.Body.Close()
				var sb strings.Builder
				buf := make([]byte, 512)
				for {
					n, rerr := resp.Body.Read(buf)
					sb.Write(buf[:n])
					if rerr != nil {
						break
					}
				}
				return resp.StatusCode, sb.String(), time.Since(start)
			}

			// The Node sidecar may still be starting.
			var code int
			var body string
			for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
				if code, body, _ = get("/api/fast"); code == 200 {
					break
				}
			}
			if code != 200 || !strings.Contains(body, "fast") {
				t.Fatalf("fast: %d %s", code, body)
			}
			code, body, took := get("/api/limited")
			if code != 200 || !strings.Contains(body, "done") {
				t.Errorf("limited (maxDuration 4, sleeps 2s): %d %s, want 200", code, body)
			}
			if took < 1900*time.Millisecond {
				t.Errorf("limited returned after %v, before its 2s sleep", took)
			}
			for _, route := range []string{"/api/plain", "/api/bad"} {
				code, body, took = get(route)
				if code != http.StatusGatewayTimeout || !strings.Contains(body, "Gateway Timeout") {
					t.Errorf("%s (default budget 1s, sleeps 2s): %d %s, want 504", route, code, body)
				}
				if took > 1900*time.Millisecond {
					t.Errorf("%s answered after %v, want about the 1s budget", route, took)
				}
			}
		})
	}
}
