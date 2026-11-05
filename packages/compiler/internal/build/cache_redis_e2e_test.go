package build

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// dockerRedis starts a real Redis in Docker on a free local port and returns its
// URL plus stop and start functions (the container keeps its port mapping, so a
// stop and start is "Redis went away and came back").
func dockerRedis(t *testing.T) (url, name string, stop, start func()) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		requireE2E(t, "docker is not installed")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		requireE2E(t, "the docker daemon is not reachable: %v", err)
	}
	port := freePort(t)
	name = fmt.Sprintf("krate-e2e-redis-%d-%d", os.Getpid(), port)
	if out, err := exec.Command("docker", "run", "-d", "--rm", "--name", name, "-p", fmt.Sprintf("127.0.0.1:%d:6379", port), "redis:7-alpine").CombinedOutput(); err != nil {
		requireE2E(t, "cannot start redis in docker: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	deadline := time.Now().Add(20 * time.Second)
	for {
		out, _ := exec.Command("docker", "exec", name, "redis-cli", "ping").CombinedOutput()
		if strings.Contains(string(out), "PONG") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("redis did not start: %s", out)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// --rm removes a stopped container, so "down" is a pause: connections hang
	// the way they do when a host drops off the network, and unpausing brings
	// the same data back.
	stop = func() {
		if out, err := exec.Command("docker", "pause", name).CombinedOutput(); err != nil {
			t.Fatalf("pausing redis: %v\n%s", err, out)
		}
	}
	start = func() {
		if out, err := exec.Command("docker", "unpause", name).CombinedOutput(); err != nil {
			t.Fatalf("unpausing redis: %v\n%s", err, out)
		}
	}
	return fmt.Sprintf("redis://127.0.0.1:%d/0", port), name, stop, start
}

type cacheResult struct {
	status  int
	headers http.Header
	body    string
}

func cacheGet(t *testing.T, url string, headers map[string]string) cacheResult {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return cacheResult{resp.StatusCode, resp.Header, string(b)}
}

// startKrateProcess runs `krate start` in root on port and waits until it answers.
func startKrateProcess(t *testing.T, bin, root string, port int, env ...string) {
	t.Helper()
	cmd := exec.Command(bin, "start", "--port", fmt.Sprint(port))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	var logs strings.Builder
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("krate start: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
		if t.Failed() {
			t.Logf("krate start :%d log:\n%s", port, logs.String())
		}
	})
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("krate start exited early:\n%s", logs.String())
		default:
		}
		if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port)); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("krate start on :%d never became ready:\n%s", port, logs.String())
}

// Two `krate start` instances share one real Redis: a page rendered by one is
// served by the other, revalidateTag on one refreshes both, per-user pages never
// reach the shared store, and a Redis that goes away costs nothing but the sharing.
func TestSharedRedisCacheTwoInstances(t *testing.T) {
	if !canStopInProcessServer {
		t.Skip("needs a unix host")
	}
	redisURL, redisName, stopRedis, startRedis := dockerRedis(t)
	t.Setenv("REDIS_URL", redisURL)

	var version atomic.Int64
	version.Store(1)
	var upstream atomic.Int64
	data := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.Add(1)
		fmt.Fprintf(w, `{"version":%d}`, version.Load())
	}))
	defer data.Close()
	t.Setenv("DATA_URL", data.URL)

	bin := filepath.Join(t.TempDir(), "krate")
	build := exec.Command("go", "build", "-o", bin, "./cmd/krate")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building krate: %v\n%s", err, out)
	}

	rootA, _ := serverlessProject(t, "cache_app")
	rootB, _ := serverlessProject(t, "cache_app")
	portA, portB := 5684, 5686
	env := []string{"REDIS_URL=" + redisURL, "DATA_URL=" + data.URL}
	startKrateProcess(t, bin, rootA, portA, env...)
	startKrateProcess(t, bin, rootB, portB, env...)
	a := fmt.Sprintf("http://127.0.0.1:%d", portA)
	b := fmt.Sprintf("http://127.0.0.1:%d", portB)

	// A renders; B serves A's page from the shared store.
	first := cacheGet(t, a+"/items/", nil)
	if first.status != 200 || !strings.Contains(first.body, "version 1") {
		t.Fatalf("A /items/ = %d\n%s", first.status, first.body)
	}
	if got := first.headers.Get("X-Krate-Cache"); got != "MISS" {
		t.Errorf("first render on A: X-Krate-Cache = %q, want MISS", got)
	}
	second := cacheGet(t, b+"/items/", nil)
	if second.status != 200 || !strings.Contains(second.body, "version 1") {
		t.Fatalf("B /items/ = %d\n%s", second.status, second.body)
	}
	if got := second.headers.Get("X-Krate-Cache"); got != "HIT" {
		t.Errorf("B must serve the page A rendered: X-Krate-Cache = %q, want HIT", got)
	}
	if n := upstream.Load(); n != 1 {
		t.Errorf("the data source was read %d times, want 1 (A fetched, B reused the page)", n)
	}

	// Revalidate on A; B must show the new data.
	version.Store(2)
	resp, err := http.Post(a+"/api/revalidate", "application/json", strings.NewReader("{}"))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("revalidate on A: %v %v", resp, err)
	}
	resp.Body.Close()
	afterB := cacheGet(t, b+"/items/", nil)
	if !strings.Contains(afterB.body, "version 2") {
		t.Errorf("B still serves the old page after revalidateTag on A:\n%s", afterB.body)
	}
	// The invalidation reaches B by pub/sub, so it can land while B renders. B
	// then serves the fresh page without caching it, and leaves no cache header.
	if got := afterB.headers.Get("X-Krate-Cache"); got != "MISS" && got != "" {
		t.Errorf("B re-rendered after the revalidation: X-Krate-Cache = %q, want MISS or uncached", got)
	}
	var afterA cacheResult
	settle := time.Now().Add(5 * time.Second)
	for {
		afterA = cacheGet(t, a+"/items/", nil)
		if afterA.headers.Get("X-Krate-Cache") == "HIT" || time.Now().After(settle) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(afterA.body, "version 2") || afterA.headers.Get("X-Krate-Cache") != "HIT" {
		t.Errorf("A serves the fresh page from the shared cache: %q %q", afterA.headers.Get("X-Krate-Cache"), afterA.body)
	}

	// Per-user pages are rendered per request and never shared.
	ada := cacheGet(t, a+"/me/", map[string]string{"Cookie": "sid=ada"})
	bob := cacheGet(t, b+"/me/", map[string]string{"Cookie": "sid=bob"})
	adaAgain := cacheGet(t, b+"/me/", map[string]string{"Cookie": "sid=ada2"})
	if !strings.Contains(ada.body, "Hello ada") || !strings.Contains(bob.body, "Hello bob") || !strings.Contains(adaAgain.body, "Hello ada2") {
		t.Errorf("per-user pages leaked between users:\nada: %s\nbob: %s\nada2: %s", ada.body, bob.body, adaAgain.body)
	}
	for _, r := range []cacheResult{ada, bob, adaAgain} {
		if cc := r.headers.Get("Cache-Control"); strings.Contains(cc, "public") {
			t.Errorf("a per-user page must not be publicly cacheable: %q", cc)
		}
	}
	for _, k := range redisKeys(t, redisName) {
		if strings.Contains(k, "/me") {
			t.Errorf("a per-user page reached the shared store: %s", k)
		}
	}

	// Redis goes away: every page still serves, no 500s.
	stopRedis()
	for _, base := range []string{a, b} {
		for _, path := range []string{"/items/", "/me/"} {
			r := cacheGet(t, base+path, map[string]string{"Cookie": "sid=x"})
			if r.status != 200 {
				t.Errorf("%s%s with Redis down = %d, want 200:\n%.300s", base, path, r.status, r.body)
			}
		}
	}
	version.Store(3)
	resp, err = http.Post(a+"/api/revalidate", "application/json", strings.NewReader("{}"))
	if err != nil || resp.StatusCode != 200 {
		t.Errorf("revalidating with Redis down must still answer 200: %v %v", resp, err)
	} else {
		resp.Body.Close()
	}
	if r := cacheGet(t, a+"/items/", nil); r.status != 200 || !strings.Contains(r.body, "version 3") {
		t.Errorf("A after revalidating with Redis down: %d %.200s", r.status, r.body)
	}

	// Redis comes back: sharing resumes without a restart.
	startRedis()
	deadline := time.Now().Add(20 * time.Second)
	for {
		cacheGet(t, a+"/items/", nil)
		r := cacheGet(t, b+"/items/", nil)
		if r.status == 200 && strings.Contains(r.body, "version 3") && r.headers.Get("X-Krate-Cache") == "HIT" {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("sharing did not resume after Redis came back: %d %q\n%.200s", r.status, r.headers.Get("X-Krate-Cache"), r.body)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// redisKeys lists the keys of the e2e prefix through the container.
func redisKeys(t *testing.T, name string) []string {
	t.Helper()
	out, err := exec.Command("docker", "exec", name, "redis-cli", "--scan", "--pattern", "e2e:*").Output()
	if err != nil {
		t.Fatalf("listing redis keys: %v", err)
	}
	return strings.Fields(string(out))
}
