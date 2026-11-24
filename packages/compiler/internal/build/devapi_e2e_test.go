package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func postDevAction(t *testing.T, base, body string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/__krate/dev/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// The overlay restarts the SSR renderer and the Node API sidecar: each comes
// back and answers again, and an asked-for restart is not reported as a
// crash. restart-server ends the server like the terminal's r key.
func TestDevAPIRestartsSidecars(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		skipOrFail(t, "node is not installed")
	}
	ensureRuntimeDist(t)
	root, cfg := secProject(t)
	cfg.Compress = config.CompressOff()
	cfg.Redirects = nil
	// API routes on the Node sidecar, the process restart-sidecar "api" stops.
	cfg.SSR.APIRuntime = "node"
	provideRuntime(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	port := freePort(t)
	cfg.Server.Port = port
	hub := NewDevHub()
	errCh := serveInProcess(t, func(ctx context.Context) error { return ServeDevContext(ctx, root, cfg, hub, time.Now()) })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	get := func(path string) (int, string) {
		resp, err := http.Get(base + path)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	answers := func(what, path, want string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			select {
			case err := <-errCh:
				t.Fatalf("dev server exited: %v", err)
			default:
			}
			code, body := get(path)
			if code == 200 && strings.Contains(body, want) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: GET %s = %d %.200s", what, path, code, body)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	answers("startup", "/dash/", "dash")
	answers("startup", "/api/hello", "api-source-marker")

	for _, target := range []string{"ssr", "api"} {
		if code := postDevAction(t, base, `{"action":"restart-sidecar","target":"`+target+`"}`); code != http.StatusAccepted {
			t.Fatalf("restart %s = %d", target, code)
		}
	}
	// The processes are going down and coming back; both answer again.
	time.Sleep(300 * time.Millisecond)
	answers("after the SSR restart", "/dash/", "dash")
	answers("after the API restart", "/api/hello", "api-source-marker")
	time.Sleep(crashTailDelay * 2)
	if errs := hub.ServerErrors(); len(errs) != 0 {
		t.Errorf("an asked-for restart was reported as a crash: %+v", errs)
	}
	if code := postDevAction(t, base, `{"action":"restart-sidecar","target":"go"}`); code != http.StatusConflict {
		t.Errorf("restart of a missing Go sidecar = %d, want 409", code)
	}

	if code := postDevAction(t, base, `{"action":"restart-server"}`); code != http.StatusAccepted {
		t.Fatalf("restart-server = %d", code)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrDevRestart) {
			t.Fatalf("ServeDev returned %v, want ErrDevRestart", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("restart-server did not stop the server")
	}
}

// The Go API sidecar restarts on request, also after it crashed and stopped
// routing.
func TestGoAPISupervisorRestart(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeGoAPIRoute(t, root, "hello.go", helloGoRoute)
	b := New(root, config.Default())
	if err := b.BuildAllGoAPI(); err != nil {
		t.Fatalf("BuildAllGoAPI: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	sup := newGoAPISupervisor(goAPIServerBinPath(root), filepath.Join(root, ".krate", "goapi-routes.json"), port)
	if err := sup.Start(); err != nil {
		t.Fatalf("starting supervisor: %v", err)
	}
	defer sup.Close()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForAPI(t, base)
	pid := func() int {
		sup.mu.Lock()
		defer sup.mu.Unlock()
		if sup.cmd == nil || sup.cmd.Process == nil {
			return 0
		}
		return sup.cmd.Process.Pid
	}
	first := pid()
	if err := sup.Restart(); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if second := pid(); second == 0 || second == first {
		t.Fatalf("pid %d -> %d: not restarted", first, second)
	}
	waitForAPI(t, base)
	if !sup.RouteMatches("GET", "/api/hello") {
		t.Fatal("routes lost across the restart")
	}

	// A crash stops routing; a restart brings the routes back.
	sup.mu.Lock()
	proc := sup.cmd.Process
	sup.mu.Unlock()
	_ = proc.Kill()
	deadline := time.Now().Add(10 * time.Second)
	for sup.RouteMatches("GET", "/api/hello") {
		if time.Now().After(deadline) {
			t.Fatal("a crashed sidecar kept its routes")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := sup.Restart(); err != nil {
		t.Fatalf("Restart after a crash: %v", err)
	}
	waitForAPI(t, base)
	if !sup.RouteMatches("GET", "/api/hello") {
		t.Fatal("routes not restored by the restart")
	}
}
