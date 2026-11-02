package build

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInternalPortFallsBackWhenTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	taken := ln.Addr().(*net.TCPAddr).Port
	if got := internalPort(taken); got == taken || got == 0 {
		t.Fatalf("internalPort(%d) = %d, want a different free port", taken, got)
	}
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()
	if got := internalPort(port); got != port {
		t.Fatalf("internalPort(%d) = %d, want the preferred port when free", port, got)
	}
}

func TestInternalPortDoesNotHandOutTheSamePortTwice(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()
	first := internalPort(port)
	second := internalPort(port)
	if first != port {
		t.Fatalf("first internalPort(%d) = %d, want the preferred port", port, first)
	}
	if second == first || second == 0 {
		t.Fatalf("second internalPort(%d) = %d, want a port different from %d", port, second, first)
	}
}

// A bind can fail with EADDRINUSE after Go picked the port, because any
// socket holding it blocks the wildcard bind. The Node sidecar must survive a
// briefly busy port, both standalone (retries the bind) and clustered (the
// worker exits quietly and the primary respawns it after a delay; relistening
// in a cluster worker trips a Node assertion on Windows).

func TestAPISidecarNextRouteParams(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "api-server.mjs")
	if err := os.WriteFile(script, []byte(apiServerScriptContent), 0o644); err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(dir, "api", "items")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	route := `export async function GET(request, context) {
  return Response.json({ id: context.params.id });
}
`
	if err := os.WriteFile(filepath.Join(apiDir, "[id].js"), []byte(route), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	cmd := exec.Command(node, script, filepath.Join(dir, "api"), strconv.Itoa(port), filepath.Join(dir, "none.js"), filepath.Join(dir, "actions"))
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/items/42", port))
		if err == nil {
			body := make([]byte, 64)
			n, _ := resp.Body.Read(body)
			resp.Body.Close()
			if got := strings.TrimSpace(string(body[:n])); got != `{"id":"42"}` {
				t.Fatalf("dynamic API params: got %q\n%s", got, output.String())
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("API sidecar never served the dynamic route:\n%s", output.String())
}

func TestAPISidecarRetriesBusyPort(t *testing.T) {
	for _, workers := range []string{"1", "2"} {
		t.Run("workers="+workers, func(t *testing.T) {
			testAPISidecarBusyPort(t, workers)
		})
	}
}

func testAPISidecarBusyPort(t *testing.T, workers string) {
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "api-server.mjs")
	if err := os.WriteFile(script, []byte(apiServerScriptContent), 0o644); err != nil {
		t.Fatal(err)
	}
	apiDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	probe, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port

	// Hold the port while the sidecar starts, then release it.
	release := time.AfterFunc(1500*time.Millisecond, func() { probe.Close() })
	defer release.Stop()

	cmd := exec.Command(node, script, apiDir, strconv.Itoa(port), filepath.Join(dir, "none.js"), filepath.Join(dir, "actions"))
	cmd.Env = append(os.Environ(), "KRATE_API_WORKERS="+workers)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stop := func() string {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return out.String()
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/none", port))
		if err == nil {
			resp.Body.Close()
			// Without the retry, workers crash on EADDRINUSE and are respawned
			// until the port frees up; the sidecar still comes up, noisily.
			if log := stop(); strings.Contains(log, "EADDRINUSE") {
				t.Fatalf("workers crashed on the busy port instead of retrying the bind:\n%.2000s", log)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("sidecar never started serving after the port was released:\n%.2000s", stop())
}
