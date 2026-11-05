package build

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func writeGoAPIRoute(t *testing.T, root, relPath, content string) {
	t.Helper()
	p := filepath.Join(root, "src", "api", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

const helloGoRoute = `package api

import (
	"net/http"

	"github.com/kratejs/krate/packages/compiler/goext/runtime"
)

func GET(w http.ResponseWriter, r *http.Request) {
	runtime.WriteJSON(w, 200, map[string]interface{}{"route": "hello", "method": r.Method})
}
`

const userGoRoute = `package api

import (
	"net/http"

	"github.com/kratejs/krate/packages/compiler/goext/runtime"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	runtime.WriteJSON(w, 200, map[string]interface{}{"id": r.PathValue("id")})
}
`

func TestBuildAllGoAPI(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	root := t.TempDir()
	writeGoAPIRoute(t, root, "hello.go", helloGoRoute)
	writeGoAPIRoute(t, root, "users/[id].go", userGoRoute)

	cfg := config.Default()
	b := New(root, cfg)
	if err := b.BuildAllGoAPI(); err != nil {
		t.Fatalf("BuildAllGoAPI: %v", err)
	}

	binPath := goAPIServerBinPath(root)
	if !fileExists(binPath) {
		t.Fatalf("sidecar binary not built at %s", binPath)
	}

	manifestPath := filepath.Join(root, ".krate", "goapi-routes.json")
	routes, _, err := loadGoAPIManifest(manifestPath)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if len(routes) != 2 {
		t.Errorf("expected 2 manifest routes, got %d: %+v", len(routes), routes)
	}

	// Match checks
	if !goAPIRouteMatches("GET", "/api/hello", routes) {
		t.Error("GET /api/hello should match")
	}
	if goAPIRouteMatches("DELETE", "/api/hello", routes) {
		t.Error("DELETE /api/hello should not match (only GET/POST registered)")
	}
	if !goAPIRouteMatches("POST", "/api/users/42", routes) {
		t.Error("POST /api/users/42 should match Handler (all methods)")
	}
	if goAPIRouteMatches("GET", "/api/hello/extra", routes) {
		t.Error("GET /api/hello/extra should not match")
	}
	if goAPIRouteMatches("GET", "/api/other", routes) {
		t.Error("GET /api/other should not match")
	}
}

func TestGoAPISidecarServesRequests(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	root := t.TempDir()
	writeGoAPIRoute(t, root, "hello.go", helloGoRoute)
	writeGoAPIRoute(t, root, "users/[id].go", userGoRoute)

	cfg := config.Default()
	b := New(root, cfg)
	if err := b.BuildAllGoAPI(); err != nil {
		t.Fatalf("BuildAllGoAPI: %v", err)
	}

	// Grab a free port for the sidecar
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	sup := newGoAPISupervisor(goAPIServerBinPath(root), filepath.Join(root, ".krate", "goapi-routes.json"), port)
	if err := sup.Start(); err != nil {
		t.Fatalf("starting supervisor: %v", err)
	}
	defer sup.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForAPI(t, base)

	get := func(path string) (int, string) {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	status, body := get("/api/hello")
	if status != 200 || !strings.Contains(body, `"route":"hello"`) {
		t.Errorf("GET /api/hello = %d %s", status, body)
	}

	status, body = get("/api/users/abc123")
	if status != 200 || !strings.Contains(body, `"id":"abc123"`) {
		t.Errorf("GET /api/users/abc123 = %d %s", status, body)
	}

	// Route not registered on the Go sidecar -> 404 from its mux
	status, _ = get("/api/nonexistent")
	if status != 404 {
		t.Errorf("GET /api/nonexistent = %d, want 404", status)
	}
}

func waitForAPI(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/api/hello")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Go API sidecar did not start in time")
}

// A component whose reads are not recorded is keyed on the whole request: any
// header can change what it renders (Referer, X-Forwarded-For, a cookie), so
// none of them may be left out of the key.
func TestFullRegionKeyVariesOnEveryHeader(t *testing.T) {
	req := func(headers string) goRenderRegion {
		return goRenderRegion{Name: "Card", Props: `{"a":1}`, Request: `{"method":"GET","url":"/p","params":{},"query":{},"headers":` + headers + `}`}
	}
	base := regionKey(req(`{"Cookie":"sid=1","Referer":"/a","X-Forwarded-For":"1.1.1.1"}`))
	if got := regionKey(req(`{"Cookie":"sid=1","Referer":"/a","X-Forwarded-For":"1.1.1.1"}`)); got != base {
		t.Error("the same request must give the same key")
	}
	for name, h := range map[string]string{
		"cookie":    `{"Cookie":"sid=2","Referer":"/a","X-Forwarded-For":"1.1.1.1"}`,
		"referer":   `{"Cookie":"sid=1","Referer":"/b","X-Forwarded-For":"1.1.1.1"}`,
		"forwarded": `{"Cookie":"sid=1","Referer":"/a","X-Forwarded-For":"2.2.2.2"}`,
		"extra":     `{"Cookie":"sid=1","Referer":"/a","X-Forwarded-For":"1.1.1.1","Authorization":"Bearer x"}`,
	} {
		if regionKey(req(h)) == base {
			t.Errorf("a different %s must change the key of an unrecorded component", name)
		}
	}
	if strings.Contains(base, "sid=1") {
		t.Errorf("the key must not hold raw cookie values: %q", base)
	}
	other := goRenderRegion{Name: "Card", Props: `{"a":1}`, Request: `{"method":"GET","url":"/q","headers":{"Cookie":"sid=1"}}`}
	if regionKey(other) == base {
		t.Error("a different URL must change the key")
	}
}
