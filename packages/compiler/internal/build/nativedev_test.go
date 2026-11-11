package build

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestPlanNativeChanges(t *testing.T) {
	root := filepath.Clean("/proj")
	c := NativeChangeClassifier{
		Root:      root,
		HostDeps:  map[string]bool{"/proj/src/lib/notes.host.ts": true, "/proj/src/lib/util.ts": true, "/proj/src/pages/inline.tsx": true},
		WholeHost: map[string]bool{"/proj/src/lib/notes.host.ts": true},
		IsUIGo:    func(p string) bool { return strings.Contains(p, "/components/") },
	}
	cases := []struct {
		name    string
		changed []string
		want    NativePlan
	}{
		{"config", []string{"/proj/krate.config.ts"}, NativePlan{Config: true}},
		{"env", []string{"/proj/.env.local"}, NativePlan{Config: true}},
		{"go", []string{"/proj/src/lib/tasks.go"}, NativePlan{GoHost: true}},
		{"go mod", []string{"/proj/go.mod"}, NativePlan{GoHost: true}},
		{"go test", []string{"/proj/src/lib/tasks_test.go"}, NativePlan{}},
		{"go component", []string{"/proj/src/components/card.go"}, NativePlan{GoHost: true, UI: []string{"/proj/src/components/card.go"}}},
		{"whole host ts", []string{"/proj/src/lib/notes.host.ts"}, NativePlan{HostJS: true}},
		{"host helper", []string{"/proj/src/lib/util.ts"}, NativePlan{HostJS: true, UI: []string{"/proj/src/lib/util.ts"}}},
		{"inline host", []string{"/proj/src/pages/inline.tsx"}, NativePlan{HostJS: true, UI: []string{"/proj/src/pages/inline.tsx"}}},
		{"page", []string{"/proj/src/pages/index.tsx", "/proj/src/styles.css"}, NativePlan{UI: []string{"/proj/src/pages/index.tsx", "/proj/src/styles.css"}}},
		{"nested go.mod is not the project's", []string{"/proj/sub/go.mod"}, NativePlan{UI: []string{"/proj/sub/go.mod"}}},
	}
	for _, tc := range cases {
		got := PlanNativeChanges(c, tc.changed)
		if got.Config != tc.want.Config || got.GoHost != tc.want.GoHost || got.HostJS != tc.want.HostJS || strings.Join(got.UI, ",") != strings.Join(tc.want.UI, ",") {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestNativeHMRMessage(t *testing.T) {
	var m map[string]any
	dec := func(ev DevEvent) map[string]any {
		m = nil
		if err := json.Unmarshal(NativeHMRMessage(ev), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := dec(DevEvent{Type: "reload", Routes: []string{"/a"}, CSSOnly: true}); m["type"] != "reload" || m["cssOnly"] != true || len(m["pages"].([]any)) != 1 {
		t.Errorf("reload: %v", m)
	}
	if m := dec(DevEvent{Type: "reload"}); m["type"] != "reload" || m["pages"] != nil {
		t.Errorf("full reload: %v", m)
	}
	if m := dec(DevEvent{Type: "client-error", ClientError: &ClientError{Message: "x"}}); m["type"] != "client-error" || m["clientError"].(map[string]any)["message"] != "x" {
		t.Errorf("client-error: %v", m)
	}
}

// devFrameClient talks the host protocol to a browser-mode shell.
type devFrameClient struct {
	t     *testing.T
	base  string
	token string
}

func (c *devFrameClient) do(method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Krate-Token", c.token)
	req.Header.Set("x-krate-window", "w1")
	return http.DefaultClient.Do(req)
}

func jsonFrame(js string) []byte {
	out := make([]byte, 4, len(js)+6)
	binary.BigEndian.PutUint32(out, uint32(len(js)))
	out = append(out, js...)
	return append(out, 0, 0)
}

func readFrameJSON(r io.Reader) (string, error) {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return "", err
	}
	body := make([]byte, binary.BigEndian.Uint32(n[:]))
	if _, err := io.ReadFull(r, body); err != nil {
		return "", err
	}
	var k [2]byte
	_, err := io.ReadFull(r, k[:])
	return string(body), err
}

// call runs one host function and returns the raw result frame JSON.
func (c *devFrameClient) call(fn, argsJSON string) (string, error) {
	resp, err := c.do(http.MethodPost, "/__krate/call", jsonFrame(`{"calls":[{"id":1,"fn":"`+fn+`","args":`+argsJSON+`}]}`))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return readFrameJSON(resp.Body)
}

func (c *devFrameClient) waitCall(fn, args, contains string, timeout time.Duration) string {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		r, err := c.call(fn, args)
		if err == nil && strings.Contains(r, contains) {
			return r
		}
		if err != nil {
			last = err.Error()
		} else {
			last = r
		}
		time.Sleep(200 * time.Millisecond)
	}
	c.t.Fatalf("%s never returned %q; last: %s", fn, contains, last)
	return ""
}

func replaceInFile(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func manifestID(t *testing.T, appDir, suffix string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(appDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m NativeManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.GoHost != nil {
		for _, id := range m.GoHost.Funcs {
			if strings.HasSuffix(id, suffix) {
				return id
			}
		}
	}
	return suffix
}

func TestNativeDevSession(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a shell and several builds")
	}
	if !hasGoHostPackage(t) {
		t.Skip("no native module in this checkout")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not found")
	}
	root := nativeExampleCopy(t)
	writeTree(t, root, map[string]string{
		"node_modules/@krate/app/host.js": "export function hostSignal(v) { const subs = new Set(); return { get: () => v, set: (n) => { v = n; subs.forEach((f) => f(n)); }, subscribe: (f) => { subs.add(f); return () => subs.delete(f); } }; }\n",
	})
	shellBin := filepath.Join(t.TempDir(), ShellFileName(runtime.GOOS))
	cmd := exec.Command("go", "build", "-o", shellBin, "./cmd/krate-shell")
	cmd.Dir = filepath.Join(repoRoot(t), "packages", "native")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build the shell: %v\n%s", err, out)
	}

	cfgLoad := func() (string, *config.Config, map[string]string, error) {
		cfg := config.Default()
		cfg.PagesDir = filepath.Join(root, "src", "pages")
		cfg.OutDir = filepath.Join(root, "dist")
		cfg.Native = &config.NativeConfig{AppID: "com.acme.tasks", ProductName: "Tasks", Version: "1.0.0"}
		cfg.Resolve(root)
		return root, cfg, nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	urlc := make(chan string, 1)
	done := make(chan error, 1)
	stderr := &lockedWriter{w: &bytes.Buffer{}}
	go func() {
		done <- RunNativeDev(NativeDevOptions{
			LoadConfig: cfgLoad, Browser: true, Shell: shellBin, Ctx: ctx,
			Stderr: stderr, OnURL: func(u string) { urlc <- u },
		})
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("RunNativeDev: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("RunNativeDev did not stop")
		}
	}()

	var u *url.URL
	select {
	case s := <-urlc:
		u, _ = url.Parse(s)
	case err := <-done:
		done <- err
		t.Fatalf("native dev ended early: %v\n%s", err, stderr.String())
	case <-time.After(180 * time.Second):
		t.Fatalf("no url; shell output:\n%s", stderr.String())
	}
	c := &devFrameClient{t: t, base: "http://" + u.Host, token: u.Query().Get("krate_token")}
	appDir := filepath.Join(root, ".krate", "native", "dev", "app")
	add := manifestID(t, appDir, "Add")
	greet := "src/lib/notes.host.greet"

	c.waitCall(add, `["x"]`, `"title":"x"`, 30*time.Second)
	c.waitCall(greet, `["w"]`, "from the host", 30*time.Second)

	// HMR stream: connected first, then a reload for a page edit.
	resp, err := c.do(http.MethodGet, "/__krate/hmr", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := make(chan string, 16)
	go func() {
		br := bufio.NewReader(resp.Body)
		for {
			s, err := readFrameJSON(br)
			if err != nil {
				return
			}
			frames <- s
		}
	}()
	waitFrame := func(substr string) {
		t.Helper()
		timeout := time.After(120 * time.Second)
		for {
			select {
			case f := <-frames:
				if strings.Contains(f, substr) {
					return
				}
			case <-timeout:
				t.Fatalf("no hmr frame containing %q", substr)
			}
		}
	}
	waitFrame(`"connected"`)

	// Go edit: the host restarts with new behavior.
	replaceInFile(t, filepath.Join(root, "src", "lib", "tasks.go"), "Task{ID: next, Title: title}", `Task{ID: next, Title: title + "-v2"}`)
	c.waitCall(add, `["y"]`, `"title":"y-v2"`, 120*time.Second)

	// A broken Go edit is reported over HMR and the old host keeps running.
	goFile := filepath.Join(root, "src", "lib", "tasks.go")
	good, _ := os.ReadFile(goFile)
	if err := os.WriteFile(goFile, append(append([]byte{}, good...), []byte("\nfunc broken( {\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFrame(`"type":"build-error"`)
	c.waitCall(add, `["z"]`, `"title":"z-v2"`, 30*time.Second)
	if err := os.WriteFile(goFile, good, 0o644); err != nil {
		t.Fatal(err)
	}
	waitFrame(`"type":"reload"`)

	// TS host edit: reloaded in place.
	replaceInFile(t, filepath.Join(root, "src", "lib", "notes.host.ts"), "from the host.", "from host v2.")
	c.waitCall(greet, `["w"]`, "from host v2", 60*time.Second)

	// Page edit: HMR reload frame.
	replaceInFile(t, filepath.Join(root, "src", "pages", "index.tsx"), "<h1>Native Tasks</h1>", "<h1>Native Tasks 2</h1>")
	waitFrame(`"type":"reload"`)
	r, err := c.do(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(page), "Native Tasks 2") {
		t.Errorf("the page was served stale")
	}

	// The inspector saw the calls.
	er, err := c.do(http.MethodGet, "/__krate/dev/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := io.ReadAll(er.Body)
	er.Body.Close()
	if !bytes.Contains(evs, []byte(`"fn":"`+greet+`"`)) {
		t.Errorf("no host events recorded: %q", evs)
	}
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "host")
	next := bin + ".next"
	for _, tc := range []struct{ old, new string }{{"", "v1"}, {"v1", "v2"}} {
		if tc.old != "" {
			if err := os.WriteFile(bin, []byte(tc.old), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(next, []byte(tc.new), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := replaceExecutable(next, bin); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(bin); string(got) != tc.new {
			t.Errorf("bin = %q, want %q", got, tc.new)
		}
		if _, err := os.Stat(bin + ".old"); err == nil {
			t.Error("left a .old file behind")
		}
	}
}
