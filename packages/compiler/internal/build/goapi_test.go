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

	"krate-compiler/internal/config"
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

	"krate-goapi/runtime"
)

func GET(w http.ResponseWriter, r *http.Request) {
	runtime.WriteJSON(w, 200, map[string]interface{}{"route": "hello", "method": r.Method})
}
`

const userGoRoute = `package api

import (
	"net/http"

	"krate-goapi/runtime"
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

	// Route not registered on the Go sidecar → 404 from its mux
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
