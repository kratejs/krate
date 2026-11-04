package build

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// ensureHandlerDist makes sure the runtime dist has the portable server
// handler (older dist builds predate it).
func ensureHandlerDist(t *testing.T) {
	t.Helper()
	ensureRuntimeDist(t)
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "packages", "runtime", "dist", "server-handler.js")); err == nil {
		return
	}
	npm := "npm"
	if _, err := exec.LookPath(npm); err != nil {
		npm = "npm.cmd"
	}
	cmd := exec.Command(npm, "run", "build")
	cmd.Dir = filepath.Join(repoRoot, "packages", "runtime")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building packages/runtime: %v\n%s", err, out)
	}
}

// serverlessProject copies a fixture under testdata, links the runtime package
// and builds it. It returns the root and the loaded config.
func serverlessProject(t *testing.T, fixture string) (string, *config.Config) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	ensureHandlerDist(t)
	root := t.TempDir()
	if err := copyTree(t, filepath.Join("testdata", fixture), root); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	repoRoot, _ := repoRootPath()
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		if out, cerr := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(root, "packages", "runtime"), filepath.Join(repoRoot, "packages", "runtime")).CombinedOutput(); cerr != nil {
			t.Fatalf("linking runtime: %v %s", cerr, out)
		}
	}
	cfg := config.Default()
	if _, err := os.Stat(filepath.Join(root, "krate.config.ts")); err == nil {
		loaded, err := config.Load(root)
		if err != nil {
			t.Fatalf("loading config: %v", err)
		}
		cfg = loaded
	}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return root, cfg
}

// startNodeBundle runs a node-target server.mjs on a free port.
func startNodeBundle(t *testing.T, root string, cfg *config.Config, env ...string) string {
	t.Helper()
	port := freePort(t)
	bundle := filepath.Join(cfg.OutDir, ".krate", "server", "server.mjs")
	cmd := exec.Command("node", bundle)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port), "HOST=127.0.0.1")
	cmd.Env = append(cmd.Env, env...)
	var logs strings.Builder
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting node bundle: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
		if t.Failed() {
			t.Logf("node server log:\n%s", logs.String())
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("node server exited early:\n%s", logs.String())
		default:
		}
		if resp, err := http.Get(base + "/healthz"); err == nil {
			resp.Body.Close()
			return base
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("node server did not start:\n%s", logs.String())
	return ""
}

type matrixResult struct {
	status  int
	headers http.Header
	body    string
}

func doMatrix(t *testing.T, base, method, path string, hdr map[string]string, body string) matrixResult {
	t.Helper()
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return matrixResult{status: resp.StatusCode, headers: resp.Header, body: string(data)}
}

func actionIDs(t *testing.T, root string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(actionManifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var m actionManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, a := range m.Actions {
		out[a.Export] = a.ID
	}
	return out
}
