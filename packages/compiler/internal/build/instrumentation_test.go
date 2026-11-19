package build

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// instFiles is a project with an SSR page, Node-served API routes and an
// instrumentation file whose register() appends one line per run to the file
// named by INST_MARKER. The helper it imports dynamically goes through the
// "@/" tsconfig alias.
func instFiles(register string) map[string]string {
	return map[string]string{
		"tsconfig.json":       `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["src/*"]}}}`,
		"src/pages/index.tsx": `export default function Home() { return <main><h1>home</h1></main>; }`,
		"src/pages/ssr.tsx": `export const dynamic = 'force-dynamic';
export default function S() { return <main><h1>ssr-page</h1></main>; }`,
		"src/server/mark.ts": `import { appendFileSync } from "node:fs";
export function mark(file: string, text: string) { appendFileSync(file, text + "\n"); }`,
		"src/api/pid.ts": `export const runtime = "nodejs";
export async function GET() {
  await new Promise((r) => setTimeout(r, 150));
  return Response.json({ pid: process.pid, state: (globalThis as any).__krateInst ?? null });
}`,
		"src/instrumentation.ts": register,
	}
}

const instRegisterMark = `export async function register() {
  const { mark } = await import("@/server/mark");
  mark(process.env.INST_MARKER!, "register worker=" + (process.env.NODE_UNIQUE_ID ?? "primary"));
  (globalThis as any).__krateInst = "ready";
}`

func markerLines(t *testing.T, file string) []string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func buildInstProject(t *testing.T, files map[string]string) (*Builder, string) {
	t.Helper()
	b, root := scProject(t, files)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	captureProgress(t)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return b, root
}

func TestFindInstrumentationSource(t *testing.T) {
	root := t.TempDir()
	if got := findInstrumentationSource(root); got != "" {
		t.Fatalf("empty project: %q", got)
	}
	_ = os.MkdirAll(filepath.Join(root, "src"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "src", "instrumentation.mjs"), []byte("export function register() {}"), 0o644)
	if got := findInstrumentationSource(root); got != filepath.Join(root, "src", "instrumentation.mjs") {
		t.Errorf("src/instrumentation.mjs: %q", got)
	}
	_ = os.WriteFile(filepath.Join(root, "instrumentation.ts"), []byte("export function register() {}"), 0o644)
	if got := findInstrumentationSource(root); got != filepath.Join(root, "instrumentation.ts") {
		t.Errorf("root file wins: %q", got)
	}
}

// register() runs exactly once per server: in the sidecar's primary process,
// before it forks the cluster workers, never in a worker, and not for the SSR
// sidecar or the requests it serves.
func TestInstrumentationRunsOncePerServerWithClusterWorkers(t *testing.T) {
	e2eEnv(t)
	marker := filepath.Join(t.TempDir(), "marker.txt")
	t.Setenv("INST_MARKER", marker)
	b, root := buildInstProject(t, instFiles(instRegisterMark))
	b.Cfg.SSR.NodeWorkers = 2
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	waitUntilTrue(t, 30*time.Second, "the API sidecar", func() bool {
		code, _ := getText(t, base+"/readyz")
		return code == 200
	})
	// Concurrent requests open several connections to the sidecar, which the
	// cluster spreads over its workers.
	var mu sync.Mutex
	pids := map[string]bool{}
	// Workers come up one after the other (slowly on a loaded machine), so keep
	// sending until both answered.
	deadline := time.Now().Add(20 * time.Second)
	for round := 0; len(pids) < 2 && time.Now().Before(deadline); round++ {
		if round > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		var wg sync.WaitGroup
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				code, body := getText(t, base+"/api/pid")
				if code != 200 {
					t.Errorf("GET /api/pid: %d %s", code, body)
					return
				}
				mu.Lock()
				pids[body[strings.Index(body, `"pid":`):strings.Index(body, `,"state"`)]] = true
				mu.Unlock()
			}()
		}
		wg.Wait()
	}
	for i := 0; i < 3; i++ {
		if code, body := getText(t, base+"/ssr/"); code != 200 || !strings.Contains(body, "ssr-page") {
			t.Fatalf("GET /ssr/: %d", code)
		}
	}
	if len(pids) < 2 {
		t.Errorf("requests should reach both cluster workers, saw %v", pids)
	}
	if lines := markerLines(t, marker); len(lines) != 1 || lines[0] != "register worker=primary" {
		t.Errorf("register() must run once, in the primary: %q", lines)
	}
}

// With one worker the primary serves requests itself, so register()'s globals
// are visible and it ran before the first request was answered.
func TestInstrumentationRunsBeforeFirstRequest(t *testing.T) {
	e2eEnv(t)
	marker := filepath.Join(t.TempDir(), "marker.txt")
	t.Setenv("INST_MARKER", marker)
	b, root := buildInstProject(t, instFiles(`export async function register() {
  const { mark } = await import("@/server/mark");
  await new Promise((r) => setTimeout(r, 700));
  mark(process.env.INST_MARKER!, "register");
  (globalThis as any).__krateInst = "ready";
}`))
	b.Cfg.SSR.NodeWorkers = 1
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	// Before register() resolves the server is not ready.
	if code, body := getText(t, base+"/readyz"); code == 200 {
		t.Errorf("/readyz is ready before register() resolved: %d %s", code, body)
	}
	var body string
	waitUntilTrue(t, 30*time.Second, "the first API answer", func() bool {
		code, b := getText(t, base+"/api/pid")
		body = b
		return code == 200
	})
	if !strings.Contains(body, `"state":"ready"`) {
		t.Errorf("the first answered request must see register()'s state: %s", body)
	}
	if lines := markerLines(t, marker); len(lines) != 1 {
		t.Errorf("marker: %q", lines)
	}
	if code, _ := getText(t, base+"/readyz"); code != 200 {
		t.Errorf("/readyz after register(): %d", code)
	}
}

func TestInstrumentationNeverResolvingBlocksReadiness(t *testing.T) {
	e2eEnv(t)
	b, root := buildInstProject(t, instFiles(`export async function register() { await new Promise(() => {}); }`))
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	code, body := getText(t, base+"/readyz")
	if code != 503 || !strings.Contains(body, "instrumentation-pending") {
		t.Errorf("/readyz = %d %q, want 503 instrumentation-pending", code, body)
	}
	if code, _ := getText(t, base+"/healthz"); code != 200 {
		t.Errorf("/healthz = %d, liveness must not depend on register()", code)
	}
}

// failingServe runs krate serve on a project whose instrumentation fails. It
// returns once Serve returned, with its error, and the status and duration of
// a request to a sidecar-bound route made as soon as the server answered.
func failingServe(t *testing.T, root string, cfg *config.Config) (error, int, time.Duration) {
	t.Helper()
	port := freePort(t)
	cfg.Server.Port = port
	errCh := make(chan error, 1)
	go func() { errCh <- Serve(root, cfg, time.Now()) }()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 40 * time.Second}
	code, took := 0, time.Duration(0)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && code == 0 {
		select {
		case err := <-errCh:
			return err, 0, 0
		default:
		}
		if resp, err := client.Get(base + "/healthz"); err == nil {
			_ = resp.Body.Close()
			start := time.Now()
			if resp, err := client.Get(base + "/api/pid"); err == nil {
				_ = resp.Body.Close()
				code = resp.StatusCode
			} else {
				code = -1 // the server closed first
			}
			took = time.Since(start)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case err := <-errCh:
		return err, code, took
	case <-time.After(20 * time.Second):
		t.Fatal("krate serve kept running after instrumentation failed")
	}
	return nil, code, took
}

// checkStartupFailure asserts the shared outcome of failed instrumentation in
// krate serve: Serve fails with the file and the cause, the sidecar is not
// restarted, and the request made meanwhile did not wait for the 30s bound.
func checkStartupFailure(t *testing.T, err error, code int, took time.Duration, logs, cause string) {
	t.Helper()
	if err == nil {
		t.Fatal("Serve must fail when instrumentation fails")
	}
	for _, want := range []string{"instrumentation failed", "src/instrumentation.ts", cause} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Serve error lacks %q: %v", want, err)
		}
	}
	// 0: the server had stopped before the request; -1: it closed meanwhile.
	if code != 503 && code > 0 {
		t.Errorf("a sidecar-bound request answered %d, want 503", code)
	}
	if took > 10*time.Second {
		t.Errorf("a sidecar-bound request took %s; it must not wait for the start bound", took)
	}
	for _, want := range []string{instrumentationFailureMessage, cause, "hint:"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log lacks %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "restarting in") {
		t.Errorf("failed instrumentation must not restart the sidecar:\n%s", logs)
	}
}

// Like a failed Next start, a throwing register() is logged with Next's
// message; krate serve does not restart the sidecar but stops with an error
// that names the file and the cause, and requests that need the sidecar
// answer 503 at once instead of waiting.
func TestInstrumentationThrowStopsServer(t *testing.T) {
	e2eEnv(t)
	marker := filepath.Join(t.TempDir(), "marker.txt")
	t.Setenv("INST_MARKER", marker)
	b, root := buildInstProject(t, instFiles(`import { appendFileSync } from "node:fs";
export async function register() {
  appendFileSync(process.env.INST_MARKER!, "attempt\n");
  await new Promise((r) => setTimeout(r, 800));
  throw new Error("boom");
}`))
	logs := captureStderrFile(t)
	err, code, took := failingServe(t, root, b.Cfg)
	checkStartupFailure(t, err, code, took, logs(), "Error: boom")
	time.Sleep(500 * time.Millisecond)
	if lines := markerLines(t, marker); len(lines) != 1 {
		t.Errorf("register() must run once: %q", lines)
	}
}

func TestInstrumentationLoadFailureStopsServer(t *testing.T) {
	e2eEnv(t)
	b, root := buildInstProject(t, instFiles(`if (!process.env.KRATE_TEST_NEVER_SET) throw new Error("load boom");
export function register() {}`))
	logs := captureStderrFile(t)
	err, code, took := failingServe(t, root, b.Cfg)
	checkStartupFailure(t, err, code, took, logs(), "Error: load boom")
}

// krate dev stays up: the error is logged and shown in the overlay, requests
// that need the sidecar answer 503 with the reason at once, nothing restarts
// until the instrumentation is rebuilt, and then the sidecar starts again.
func TestInstrumentationFailureInDevWaitsForRebuild(t *testing.T) {
	e2eEnv(t)
	marker := filepath.Join(t.TempDir(), "marker.txt")
	t.Setenv("INST_MARKER", marker)
	b, root := buildInstProject(t, instFiles(`import { appendFileSync } from "node:fs";
export async function register() {
  appendFileSync(process.env.INST_MARKER!, "attempt\n");
  throw new Error("dev boom");
}`))
	logs := captureStderrFile(t)
	cfg := b.Cfg
	cfg.Server.Host = "127.0.0.1"
	port := freePort(t)
	cfg.DevServer.Port = port
	hub := NewDevHub()
	errc := make(chan error, 1)
	go func() { errc <- ServeDev(root, cfg, hub, time.Now()) }()
	t.Cleanup(func() {
		hub.RequestRestart()
		select {
		case <-errc:
		case <-time.After(15 * time.Second):
			t.Log("dev server did not stop")
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitUntilTrue(t, 30*time.Second, "register() to fail", func() bool { return len(markerLines(t, marker)) >= 1 })
	var body string
	waitUntilTrue(t, 10*time.Second, "the failure to be known", func() bool {
		start := time.Now()
		code, b := getText(t, base+"/api/pid")
		body = b
		if took := time.Since(start); took > 5*time.Second {
			t.Fatalf("a request took %s", took)
		}
		return code == 503 && strings.Contains(b, "dev boom")
	})
	if code, _ := getText(t, base+"/healthz"); code != 200 {
		t.Errorf("the dev server must stay up: /healthz %d", code)
	}
	waitUntilTrue(t, 5*time.Second, "the overlay entry", func() bool {
		for _, e := range hub.ServerErrors() {
			if strings.Contains(e.Message, "dev boom") {
				return true
			}
		}
		return false
	})
	time.Sleep(1500 * time.Millisecond)
	if lines := markerLines(t, marker); len(lines) != 1 {
		t.Fatalf("register() must not be retried before a rebuild: %q (body %q)", lines, body)
	}
	if strings.Contains(logs(), "restarting in") {
		t.Errorf("no restart loop expected:\n%s", logs())
	}

	if err := os.WriteFile(filepath.Join(root, "src", "instrumentation.ts"), []byte(instRegisterMark), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildInstrumentation(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		code, b := getText(t, base+"/api/pid")
		if code == 200 {
			break
		}
		if time.Now().After(deadline) {
			out, _ := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN").CombinedOutput()
			t.Fatalf("timed out waiting for the sidecar after the rebuild: %d %q\ndev port %d\nlisteners:\n%s\nserver log:\n%s", code, b, port, out, logs())
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lines := markerLines(t, marker); len(lines) != 2 || lines[1] != "register worker=primary" {
		t.Errorf("register() must run again once after the rebuild: %q", lines)
	}
}

func TestInstrumentationStartsSidecarForEmbeddedOnlyApp(t *testing.T) {
	e2eEnv(t)
	marker := filepath.Join(t.TempDir(), "marker.txt")
	t.Setenv("INST_MARKER", marker)
	files := instFiles(instRegisterMark)
	delete(files, "src/api/pid.ts")
	files["src/api/plain.ts"] = `export function GET() { return Response.json({ ok: true }); }`
	b, root := buildInstProject(t, files)
	var out syncBuffer
	SetOutput(&out)
	t.Cleanup(func() { SetOutput(os.Stdout) })
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	waitUntilTrue(t, 30*time.Second, "register() to run", func() bool { return len(markerLines(t, marker)) == 1 })
	if code, body := getText(t, base+"/api/plain"); code != 200 || !strings.Contains(body, "true") {
		t.Errorf("embedded route: %d %s", code, body)
	}
	if !strings.Contains(out.String(), "instrumentation runs in the Node API sidecar") {
		t.Errorf("missing info line:\n%s", out.String())
	}
}

func TestInstrumentationNodeDeployBundleRunsOnce(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	ensureHandlerDist(t)
	marker := filepath.Join(t.TempDir(), "marker.txt")
	b, root := scProject(t, instFiles(instRegisterMark))
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoRoot, _ := repoRootPath()
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime: %v", err)
	}
	captureProgress(t)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildServerBundle(root, b.Cfg, "node")
	if err != nil {
		t.Fatal(err)
	}
	bundle, _ := os.ReadFile(plan.Bundle)
	if !strings.Contains(string(bundle), "server/mark") && !strings.Contains(string(bundle), "appendFileSync") {
		t.Errorf("the alias-imported helper must be bundled")
	}
	base := startNodeBundle(t, root, b.Cfg, "INST_MARKER="+marker)
	for i := 0; i < 5; i++ {
		code, body := getText(t, base+"/api/pid")
		if code != 200 || !strings.Contains(body, `"state":"ready"`) {
			t.Fatalf("GET /api/pid: %d %s", code, body)
		}
	}
	if lines := markerLines(t, marker); len(lines) != 1 || lines[0] != "register worker=primary" {
		t.Errorf("register() must run once before the server listens: %q", lines)
	}
}

// The node deploy bundle refuses to start, with Next's message, both when
// register() throws and when the module fails to load.
func TestInstrumentationNodeDeployFailureExitsNonZero(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	ensureHandlerDist(t)
	for name, src := range map[string]string{
		"throw": `export async function register() { throw new Error("boom"); }`,
		"load": `if (!process.env.KRATE_TEST_NEVER_SET) throw new Error("boom");
export function register() {}`,
	} {
		t.Run(name, func(t *testing.T) {
			b, root := scProject(t, instFiles(src))
			if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
				t.Fatal(err)
			}
			repoRoot, _ := repoRootPath()
			if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
				t.Skipf("cannot link the runtime: %v", err)
			}
			captureProgress(t)
			if err := b.BuildAll(); err != nil {
				t.Fatal(err)
			}
			plan, err := BuildServerBundle(root, b.Cfg, "node")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("node", plan.Bundle)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", freePort(t)), "HOST=127.0.0.1")
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the server must exit non-zero:\n%s", out)
			}
			if !strings.Contains(string(out), "[krate] "+instrumentationFailureMessage) || !strings.Contains(string(out), "boom") {
				t.Errorf("missing failure message:\n%s", out)
			}
		})
	}
}

func TestInstrumentationOtherTargetsWarn(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	ensureHandlerDist(t)
	b, root := scProject(t, instFiles(instRegisterMark))
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoRoot, _ := repoRootPath()
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime: %v", err)
	}
	captureProgress(t)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildServerBundle(root, b.Cfg, "vercel")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "instrumentation.ts is not run on the vercel target") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings: %v", plan.Warnings)
	}
}
