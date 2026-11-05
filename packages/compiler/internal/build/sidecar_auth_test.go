package build

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The Node API sidecar only answers the Krate host: a direct connection to its
// loopback port must not skip middleware, CSRF and the body limits.
func TestAPISidecarRequiresHostSecret(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node not available: %v", err)
		return
	}
	dir := t.TempDir()
	apiDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "hello.js"), []byte(`export function GET() { return Response.json({ secret: "TOP" }); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "sidecar.mjs")
	if err := os.WriteFile(script, []byte(apiServerScriptContent), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	cmd := exec.Command("node", script, apiDir, fmt.Sprint(port), "", filepath.Join(dir, "actions"))
	cmd.Env = append(os.Environ(), "KRATE_INTERNAL_SECRET=s3cret")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sidecar did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}

	do := func(path, secret string) int {
		req, _ := http.NewRequest("GET", "http://"+addr+path, nil)
		if secret != "" {
			req.Header.Set(internalSecretHeader, secret)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, path := range []string{"/api/hello", "/__krate/middleware", "/__krate/action/x"} {
		if got := do(path, ""); got != 403 {
			t.Errorf("%s without the secret = %d, want 403", path, got)
		}
		if got := do(path, "wrong"); got != 403 {
			t.Errorf("%s with a wrong secret = %d, want 403", path, got)
		}
	}
	if got := do("/api/hello", "s3cret"); got != 200 {
		t.Errorf("/api/hello with the secret = %d, want 200", got)
	}
}
