package build

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// guardProject lays out root/.krate (optionally with middleware.js) and
// root/dist/.krate/manifest.json with the given middleware record ("" omits it).
func guardProject(t *testing.T, withModule bool, record string) (root, out string) {
	t.Helper()
	root = t.TempDir()
	out = filepath.Join(root, "dist")
	if err := os.MkdirAll(filepath.Join(out, ".krate"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"pages":[]}`
	if record != "" {
		manifest = `{"pages":[],"middleware":` + record + `}`
	}
	if err := os.WriteFile(filepath.Join(out, ".krate", "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if withModule {
		if err := os.MkdirAll(filepath.Join(root, ".krate"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".krate", "middleware.js"), []byte("export function middleware() {}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, out
}

func TestCheckMiddlewareDeployed(t *testing.T) {
	cases := []struct {
		name       string
		withModule bool
		record     string
		dev        bool
		wantErr    bool
		wantWarn   bool
		wantReq    bool
	}{
		{name: "recorded and present", withModule: true, record: "true", wantReq: true},
		{name: "recorded but missing", record: "true", wantErr: true},
		{name: "recorded absent and missing", record: "false"},
		{name: "old dist without module warns", wantWarn: true},
		{name: "old dist with module", withModule: true, wantReq: true},
		{name: "dev never refuses", record: "true", dev: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, out := guardProject(t, tc.withModule, tc.record)
			var warn bytes.Buffer
			req, err := checkMiddlewareDeployed(root, out, tc.dev, &warn)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if err != nil {
				msg := err.Error()
				if !strings.Contains(msg, filepath.Join(root, ".krate", "middleware.js")) || !strings.Contains(msg, "deploy") || !strings.Contains(msg, ".krate/") {
					t.Errorf("error must name the missing path and say to deploy .krate/: %q", msg)
				}
			}
			if req != tc.wantReq && !tc.wantErr {
				t.Errorf("required = %v, want %v", req, tc.wantReq)
			}
			if got := strings.Contains(warn.String(), "no middleware record"); got != tc.wantWarn {
				t.Errorf("warning = %v, want %v (%q)", got, tc.wantWarn, warn.String())
			}
		})
	}
}

// With middleware required, a vanished module must answer 500 and never reach
// the page handler; apps without middleware keep passing through.
func TestMiddlewareGateFailsClosedWhenModuleVanishes(t *testing.T) {
	nextCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalls++
		_, _ = io.WriteString(w, "secret page")
	})
	missing := filepath.Join(t.TempDir(), "gone", "middleware.js")
	for _, required := range []bool{true, false} {
		nextCalls = 0
		g := &middlewareGate{file: missing, required: required}
		rec := httptest.NewRecorder()
		g.wrap(next).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/admin", nil))
		if required {
			if rec.Code != http.StatusInternalServerError || nextCalls != 0 || strings.Contains(rec.Body.String(), "secret") {
				t.Errorf("required: code=%d next=%d body=%q, want 500 and no call to next", rec.Code, nextCalls, rec.Body)
			}
		} else if rec.Code != 200 || nextCalls != 1 {
			t.Errorf("not required: code=%d next=%d, want pass-through", rec.Code, nextCalls)
		}
	}
}

// Serve must refuse to start when the build recorded middleware that was not
// deployed with dist.
func TestServeRefusesWhenRecordedMiddlewareIsMissing(t *testing.T) {
	root, out := guardProject(t, false, "true")
	cfg := config.Default()
	cfg.OutDir = out
	done := make(chan error, 1)
	go func() { done <- Serve(root, cfg, time.Now()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "middleware.js") || !strings.Contains(err.Error(), ".krate/") {
			t.Fatalf("Serve error = %v, want an actionable missing-middleware error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve started instead of refusing")
	}
}

// The build records whether the app has middleware in the dist manifest.
func TestBuildRecordsMiddlewareInManifest(t *testing.T) {
	for _, with := range []bool{true, false} {
		root := t.TempDir()
		ensureRuntimeDist(t)
		if err := linkRuntimePackage(t, filepath.Join(root, "packages")); err != nil {
			t.Fatal(err)
		}
		writeFileRel(t, root, "src/pages/index.tsx", `export default function Home() { return <main><h1>Home</h1></main>; }`)
		if with {
			writeFileRel(t, root, "middleware.ts", `export function middleware() {}`)
		}
		cfg := config.Default()
		cfg.PagesDir = filepath.Join(root, "src", "pages")
		cfg.OutDir = filepath.Join(root, "dist")
		if err := New(root, cfg).BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		data, err := os.ReadFile(ManifestPath(cfg.OutDir))
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Middleware *bool `json:"middleware"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Middleware == nil || *m.Middleware != with {
			t.Errorf("middleware=%v: manifest record = %v", with, m.Middleware)
		}
		if has, known := recordedMiddleware(cfg.OutDir); !known || has != with {
			t.Errorf("middleware=%v: recordedMiddleware = %v, %v", with, has, known)
		}
	}
}

// A middleware module that exists but fails to import must not read as "no
// middleware" in the Node sidecar.
func TestNodeSidecarMiddlewareLoadErrorFailsClosed(t *testing.T) {
	dir := t.TempDir()
	mwPath := filepath.Join(dir, "middleware.js")
	if err := os.WriteFile(mwPath, []byte("export function middleware( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(dir, "api")
	_ = os.MkdirAll(apiDir, 0o755)
	base := startNodeConformance(t, apiDir, mwPath)
	resp, err := http.Post(base+"/__krate/middleware", "application/json", strings.NewReader(`{"url":"http://x/admin","method":"GET","path":"/admin","headers":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Action string `json:"action"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%v (%s)", err, raw)
	}
	if out.Action != "error" || out.Error == "" {
		t.Errorf("load failure answered %s, want an error envelope", raw)
	}
}

// sidecarMiddlewareAnswer posts one request to a Node sidecar running source.
func sidecarMiddlewareAnswer(t *testing.T, source string) (action, errText string) {
	t.Helper()
	dir := t.TempDir()
	mwPath := filepath.Join(dir, "middleware.js")
	if err := os.WriteFile(mwPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(dir, "api")
	_ = os.MkdirAll(apiDir, 0o755)
	base := startNodeConformance(t, apiDir, mwPath)
	resp, err := http.Post(base+"/__krate/middleware", "application/json", strings.NewReader(`{"url":"http://x/admin","method":"GET","path":"/admin","headers":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Action string `json:"action"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%v (%s)", err, raw)
	}
	return out.Action, out.Error
}

func TestNodeSidecarMiddlewareExports(t *testing.T) {
	const deny = `(req) => new Response("no", { status: 403 })`
	for name, source := range map[string]string{
		"named":   "export const middleware = " + deny + ";\n",
		"default": "export default " + deny + ";\n",
	} {
		t.Run(name, func(t *testing.T) {
			if action, _ := sidecarMiddlewareAnswer(t, source); action != "response" {
				t.Errorf("action = %q, want the handler's response", action)
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		action, errText := sidecarMiddlewareAnswer(t, "export const other = 1;\n")
		if action != "error" || !strings.Contains(errText, "does not export") {
			t.Errorf("answered %q %q, want an error envelope", action, errText)
		}
	})
}

func TestBuildRejectsMiddlewareWithoutHandler(t *testing.T) {
	build := func(source string) error {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "middleware.ts"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := config.Default()
		cfg.OutDir = filepath.Join(root, "dist")
		return New(root, cfg).BuildMiddleware()
	}
	if err := build("export const other = 1;\n"); err == nil || !strings.Contains(err.Error(), string(diag.CodeMiddlewareExport)) {
		t.Errorf("no handler: err = %v, want %s", err, diag.CodeMiddlewareExport)
	}
	for _, source := range []string{
		"export function middleware(r: Request) { return r; }\n",
		"export default function (r: Request) { return r; }\n",
		"const m = (r: Request) => r;\nexport { m as middleware };\n",
	} {
		if err := build(source); err != nil {
			t.Errorf("%q: %v", source, err)
		}
	}
}

// A corrupt build record must stop the server, and comes before the middleware
// check because a corrupt manifest would otherwise read as "no record".
func TestServeRefusesCorruptBuildRecords(t *testing.T) {
	for name, rel := range map[string]string{
		"manifest":   "dist/.krate/manifest.json",
		"api routes": ".krate/api-routes.json",
	} {
		t.Run(name, func(t *testing.T) {
			root, out := guardProject(t, false, "true")
			if err := os.MkdirAll(filepath.Join(root, ".krate"), 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, filepath.FromSlash(rel))
			if rel == "dist/.krate/manifest.json" {
				path = ResolveManifestPath(out, "manifest.json")
			}
			if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := config.Default()
			cfg.OutDir = out
			done := make(chan error, 1)
			go func() { done <- Serve(root, cfg, time.Now()) }()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "corrupt") || !strings.Contains(err.Error(), filepath.Base(path)) {
					t.Fatalf("err = %v, want a corrupt-file error naming %s", err, path)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Serve started instead of refusing")
			}
		})
	}
	root := t.TempDir()
	if err := checkBuildRecords(root, filepath.Join(root, "dist")); err != nil {
		t.Errorf("missing records must stay valid: %v", err)
	}
}

func TestSegmentIndexKeepsLastGoodManifest(t *testing.T) {
	out := t.TempDir()
	path := ResolveManifestPath(out, "manifest.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"pages":[{"route":"/a","segDir":"seg"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newSegmentIndex(out)
	if _, dirs := s.load(); dirs[normalizeRoutePattern("/a")] != "seg" {
		t.Fatalf("first load = %v", dirs)
	}
	if err := os.WriteFile(path, []byte("{broken, and a different size"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, dirs := s.load(); dirs[normalizeRoutePattern("/a")] != "seg" {
		t.Errorf("load after corruption = %v, want the last good index", dirs)
	}
}
