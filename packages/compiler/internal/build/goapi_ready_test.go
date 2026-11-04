package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A Go API host that never starts listening must fail Start loudly instead of
// stalling and then reporting success.
func TestGoAPISupervisorFailsLoudlyWhenHostNeverListens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the host binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "host.sh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "routes.json")
	if err := os.WriteFile(manifest, []byte(`{"routes":[{"method":"GET","path":"/api/x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	g := newGoAPISupervisor(bin, manifest, freePort(t))
	g.readyTimeout = 600 * time.Millisecond
	g.SetSocket(filepath.Join(dir, "g.sock"))
	err := g.Start()
	defer g.Close()
	if err == nil || !strings.Contains(err.Error(), "did not accept connections") {
		t.Fatalf("Start() = %v, want a not-listening error", err)
	}
	if g.Active() || g.RouteMatches("GET", "/api/x") {
		t.Error("a host that never became ready must not receive routes")
	}
}
