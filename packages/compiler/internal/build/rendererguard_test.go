package build

import (
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// serveFaultyApp serves an app with a healthy page and the given files.
func serveFaultyApp(t *testing.T, files map[string]string) string {
	t.Helper()
	ensureRuntimeDist(t)
	all := map[string]string{
		"src/pages/index.tsx": `export default async function Home() { await Promise.resolve(); return <main id="home">home</main>; }`,
		"src/pages/ok.tsx":    `export default async function Ok() { await Promise.resolve(); return <main id="ok">fine</main>; }`,
	}
	for k, v := range files {
		all[k] = v
	}
	b, root := scProject(t, all)
	withPreact(t, root)
	linkRuntimeDist(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	return base
}

// healthyWhileFailing requests the failing route a few times, over a window
// long enough for a crashed renderer to show (it restarts after 100ms and
// answers 503 until it is back), while the other pages are polled: every one
// of them must answer 200 the whole time.
func healthyWhileFailing(t *testing.T, base, failing string, wantFail int, wantText string, polled ...string) {
	if len(polled) == 0 {
		polled = []string{"/ok/", "/"}
	}
	t.Helper()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	bad := make(chan string, 64)
	for _, p := range polled {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if status, _ := get(t, base+p); status != 200 {
					select {
					case bad <- p + " answered " + http.StatusText(status):
					default:
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(p)
	}
	for i := 0; i < 4; i++ {
		status, html := get(t, base+failing)
		if status != wantFail || (wantText != "" && !strings.Contains(html, wantText)) {
			t.Errorf("%s: status %d, want %d containing %q:\n%.600s", failing, status, wantFail, wantText, html)
		}
		time.Sleep(150 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond)
	close(stop)
	wg.Wait()
	close(bad)
	for msg := range bad {
		t.Errorf("another page failed while %s threw: %s", failing, msg)
	}
	if status, _ := get(t, base+polled[0]); status != 200 {
		t.Errorf("%s answered %d after the failures", polled[0], status)
	}
}

// TestRenderErrorsThatEscapeTheRequestKeepTheRendererUp: an async server
// layout that starts a promise nobody awaits (rejecting) or throws from a timer
// used to crash the whole renderer process, so every page answered 503 until
// it restarted.
func TestRenderErrorsThatEscapeTheRequestKeepTheRendererUp(t *testing.T) {
	t.Parallel()
	base := serveFaultyApp(t, map[string]string{
		"src/pages/lost/_layout.tsx": `export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  Promise.reject(new Error("lost rejection"));
  return <div>{children}</div>;
}`,
		"src/pages/lost/index.tsx": `export default async function Lost() { await Promise.resolve(); return <main>lost</main>; }`,
		"src/pages/late/_layout.tsx": `export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  setTimeout(() => { throw new Error("late throw"); }, 5);
  return <div>{children}</div>;
}`,
		"src/pages/late/index.tsx": `export default async function Late() { await Promise.resolve(); return <main>late</main>; }`,
	})
	t.Run("rejection", func(t *testing.T) { healthyWhileFailing(t, base, "/lost/", 200, "lost") })
	t.Run("timer", func(t *testing.T) { healthyWhileFailing(t, base, "/late/", 200, "late") })
}

// TestAPIRouteErrorsThatEscapeTheRequestKeepTheSidecarUp: an API route that
// starts a promise nobody awaits (rejecting) or throws from a timer used to
// exit the sidecar process, and every API route answered 503 until the host
// restarted it. Runs on every JS runtime that is installed.
func TestAPIRouteErrorsThatEscapeTheRequestKeepTheSidecarUp(t *testing.T) {
	t.Parallel()
	for _, runtime := range []string{"node", "bun", "deno"} {
		t.Run(runtime, func(t *testing.T) {
			if _, err := exec.LookPath(runtime); err != nil {
				t.Skipf("%s is not installed", runtime)
			}
			t.Parallel()
			ensureRuntimeDist(t)
			b, root := scProject(t, map[string]string{
				"src/pages/index.tsx": `export default function Home() { return <main>home</main>; }`,
				"src/api/ok.ts":       `export function GET() { return Response.json({ ok: true }); }`,
				"src/api/lost.ts": `export async function GET() {
  Promise.reject(new Error("lost rejection"));
  return Response.json({ lost: true });
}`,
				"src/api/late.ts": `export async function GET() {
  setTimeout(() => { throw new Error("late throw"); }, 5);
  return Response.json({ late: true });
}`,
			})
			b.Cfg.SSR.APIRuntime = runtime
			// One process, so a crash is not hidden by a sibling worker.
			b.Cfg.SSR.NodeWorkers = 1
			if err := b.BuildAll(); err != nil {
				t.Fatalf("BuildAll: %v", err)
			}
			base, err := startKrateServer(t, root, b.Cfg)
			if err != nil {
				t.Fatalf("serve: %v", err)
			}
			if status, body := get(t, base+"/api/ok"); status != 200 {
				t.Fatalf("/api/ok answered %d before any failure: %s", status, body)
			}
			t.Run("rejection", func(t *testing.T) { healthyWhileFailing(t, base, "/api/lost", 200, "lost", "/api/ok") })
			t.Run("timer", func(t *testing.T) { healthyWhileFailing(t, base, "/api/late", 200, "late", "/api/ok") })
		})
	}
}

// TestDenoAPISidecarOutlivesTheParentWatch: the sidecar checks every two
// seconds that the Krate host is alive with process.kill(pid, 0). Deno runs it
// without --allow-run, so the check throws a permission error, which the watch
// took for a dead host: the sidecar exited every two seconds and requests in
// the gap answered 502.
func TestDenoAPISidecarOutlivesTheParentWatch(t *testing.T) {
	if _, err := exec.LookPath("deno"); err != nil {
		t.Skip("deno is not installed")
	}
	t.Parallel()
	ensureRuntimeDist(t)
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <main>home</main>; }`,
		"src/api/pid.ts":      `export function GET() { return new Response(String(process.pid)); }`,
	})
	b.Cfg.SSR.APIRuntime = "deno"
	b.Cfg.SSR.NodeWorkers = 1
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	// A restart is quick enough that most requests never see the gap, so
	// compare the process id instead of only the status.
	status, first := get(t, base+"/api/pid")
	if status != 200 {
		t.Fatalf("/api/pid answered %d: %s", status, first)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if status, pid := get(t, base+"/api/pid"); status != 200 || pid != first {
			t.Fatalf("the Deno API sidecar was restarted: /api/pid answered %d %q, first %q", status, pid, first)
		}
	}
}
