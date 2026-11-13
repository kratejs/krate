package build

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// The native end-to-end tests build examples/native-tasks with the real
// pipeline, run the real krate-shell on the result and drive the page through
// the DOM. They need the JS packages built and installed (pnpm install,
// pnpm build) and a Go toolchain, and run only when asked:
//
//	KRATE_NATIVE_GUI=1      the shell in a real webview (needs a display and
//	                        the platform webview libraries)
//	KRATE_BROWSER=chromium  the shell with --browser, driven by Playwright

type e2eStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func e2eRequireDeps(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	ex := filepath.Join(root, "examples", "native-tasks")
	// examples/ may hold a generated go.work (other tests write one) that
	// does not list this module; the example stands on its own go.mod.
	t.Setenv("GOWORK", "off")
	for _, p := range []string{
		filepath.Join(ex, "node_modules", "@krate", "app", "dist", "index.js"),
		filepath.Join(ex, "node_modules", "@krate", "core", "dist", "index.js"),
	} {
		if _, err := os.Stat(p); err != nil {
			requireE2E(t, "%s is missing (run pnpm install and pnpm build)", p)
		}
	}
	if _, err := exec.LookPath("go"); err != nil {
		requireE2E(t, "go is not installed")
	}
	return ex
}

// e2eBuildApp runs the equivalent of `krate native build` on the example and
// returns the app directory.
func e2eBuildApp(t *testing.T, ex, out string) string {
	t.Helper()
	appDir := e2eBuildNative(t, ex, out)
	e2eCheckBaked(t, appDir)
	return appDir
}

// e2eBuildNative builds any example as a native app and returns the app
// directory.
func e2eBuildNative(t *testing.T, ex, out string) string {
	t.Helper()
	cfg, err := config.Load(ex)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	b := New(ex, cfg)
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: out})
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	return res.AppDir
}

// e2eCheckBaked asserts the build functions of the example were evaluated at
// build time: their values are in the prerendered HTML, and nothing in the
// manifest or the host bundle can be called for them.
func e2eCheckBaked(t *testing.T, appDir string) {
	t.Helper()
	html, err := os.ReadFile(filepath.Join(appDir, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	if m := regexp.MustCompile(`<p id="?build-ts"?[^>]*>(Built \d{4}-\d{2}-\d{2})</p>`).FindStringSubmatch(page); m == nil {
		t.Errorf("the @build TS value is not in the prerendered HTML:\n%s", page)
	}
	if !strings.Contains(page, ">Krate Native Tasks edition, built with go") {
		t.Errorf("the //krate:build Go value is not in the prerendered HTML:\n%s", page)
	}
	man, err := os.ReadFile(filepath.Join(appDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(man), "Edition") {
		t.Errorf("the Go build function must not be served by the Go host:\n%s", man)
	}
	host, err := os.ReadFile(filepath.Join(appDir, "host", "host.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(host), "buildInfo") {
		t.Errorf("the @build function must not be in the host bundle")
	}
}

// TestNativeE2EBaked builds the example and checks the baked values; it needs
// no shell and no browser.
func TestNativeE2EBaked(t *testing.T) {
	ex := e2eRequireDeps(t)
	e2eBuildApp(t, ex, filepath.Join(t.TempDir(), "dist-native"))
}

// e2eBuildShell compiles cmd/krate-shell from this checkout.
func e2eBuildShell(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "krate-shell")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/krate-shell")
	cmd.Dir = filepath.Join(repoRoot(t), "packages", "native")
	if runtime.GOOS == "linux" && os.Getenv("CGO_ENABLED") == "" {
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building krate-shell: %v\n%s", err, out)
	}
	return bin
}

// e2eShell is a running krate-shell with its dev control channel.
type e2eShell struct {
	t     *testing.T
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
	done  chan struct{}
	start time.Time
	// errs collects everything the shell wrote to stderr.
	errs *syncBuf
}

func e2eStartShell(t *testing.T, bin, appDir string, args ...string) *e2eShell {
	t.Helper()
	argv := append([]string{"--dev"}, args...)
	if appDir != "" {
		argv = append(argv, appDir)
	}
	cmd := exec.Command(bin, argv...)
	errs := &syncBuf{}
	cmd.Stderr = io.MultiWriter(&testLogWriter{t: t}, errs)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	s := &e2eShell{t: t, cmd: cmd, stdin: stdin, lines: make(chan string, 64), done: make(chan struct{}), start: time.Now(), errs: errs}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting krate-shell: %v", err)
	}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 16<<20)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
	}()
	go func() { _ = cmd.Wait(); close(s.done) }()
	t.Cleanup(s.stop)
	return s
}

// syncBuf is a bytes.Buffer safe for a writing process and a reading test.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type testLogWriter struct{ t *testing.T }

func (w *testLogWriter) Write(p []byte) (int, error) {
	defer func() { _ = recover() }() // a late write after the test ended
	w.t.Logf("shell: %s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// next returns the next protocol line.
func (s *e2eShell) next(timeout time.Duration) string {
	s.t.Helper()
	select {
	case l, ok := <-s.lines:
		if !ok {
			s.t.Fatal("krate-shell closed its control channel")
		}
		return l
	case <-time.After(timeout):
		// Show where the shell is stuck: its goroutine stacks go to stderr,
		// which the test log already carries.
		if s.cmd.Process != nil {
			logNativeStacks(s.t, s.cmd.Process)
		}
		if s.cmd.Process != nil && dumpGoStacks(s.cmd.Process) {
			select {
			case <-s.done:
			case <-time.After(5 * time.Second):
			}
		}
		s.t.Fatalf("no line from krate-shell within %s", timeout)
	}
	return ""
}

// waitReady reads protocol lines up to "ready" and returns the "url" line's
// value, if any.
func (s *e2eShell) waitReady(timeout time.Duration) (url string) {
	s.t.Helper()
	for {
		l := s.next(timeout)
		switch {
		case strings.HasPrefix(l, "url "):
			url = strings.TrimPrefix(l, "url ")
		case l == "ready":
			return url
		default:
			s.t.Fatalf("unexpected line before ready: %q", l)
		}
	}
}

// send runs one control command and returns the text after "ok".
func (s *e2eShell) send(cmd string, timeout time.Duration) (string, error) {
	s.t.Helper()
	if _, err := fmt.Fprintln(s.stdin, cmd); err != nil {
		return "", err
	}
	l := s.next(timeout)
	switch {
	case l == "ok":
		return "", nil
	case strings.HasPrefix(l, "ok "):
		return strings.TrimPrefix(l, "ok "), nil
	case strings.HasPrefix(l, "error "):
		return "", fmt.Errorf("%s", strings.TrimPrefix(l, "error "))
	}
	return "", fmt.Errorf("unexpected reply %q", l)
}

func (s *e2eShell) stop() {
	select {
	case <-s.done:
		return
	default:
	}
	_ = s.stdin.Close()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.done
	}
}

// query evaluates a JavaScript expression in the page and decodes its JSON
// value into v.
func (s *e2eShell) query(js string, v any) error {
	s.t.Helper()
	js = strings.Join(strings.Fields(strings.ReplaceAll(js, "\n", " ")), " ")
	out, err := s.send("query "+js, 60*time.Second)
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	return json.Unmarshal([]byte(out), v)
}

func e2eReadFlow(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "native_e2e", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func e2eCheckSteps(t *testing.T, steps []e2eStep, want int) {
	t.Helper()
	for _, st := range steps {
		if !st.OK {
			t.Errorf("flow step failed: %s\n  page: %s", st.Name, st.Detail)
		} else {
			t.Logf("ok: %s", st.Name)
		}
	}
	if len(steps) < want {
		t.Errorf("only %d flow steps ran, want at least %d", len(steps), want)
	}
}

// e2eFootprint logs the memory of the shell and its helper processes (PSS and
// private dirty per role, from the shell's memory control command).
func e2eFootprint(t *testing.T, s *e2eShell) {
	t.Helper()
	out, err := s.send("memory", 30*time.Second)
	if err != nil {
		t.Logf("footprint: memory command: %v", err)
		return
	}
	var r shellMemoryReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Logf("footprint: %v", err)
		return
	}
	for role, v := range memPointOf(&r).Roles {
		t.Logf("footprint: %-16s PSS %6d KB, private dirty %6d KB", role, v.PSS>>10, v.PrivateDirty>>10)
	}
}

func e2eDirSize(t *testing.T, dir string) int64 {
	t.Helper()
	var n int64
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// TestNativeE2EGUI runs the example in the real webview and drives it through
// the shell's dev control channel.
func TestNativeE2EGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	ex := e2eRequireDeps(t)
	work := t.TempDir()
	appDir := e2eBuildApp(t, ex, filepath.Join(work, "dist-native"))
	bin := e2eBuildShell(t, work)

	s := e2eStartShell(t, bin, appDir)
	s.waitReady(60 * time.Second)

	e2eGUIFlows(t, s)
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
	t.Logf("footprint: app directory %d KB, Go host %d KB, krate-shell %d KB",
		e2eDirSize(t, appDir)/1024, e2eDirSize(t, filepath.Join(appDir, "gohost"))/1024, e2eDirSize(t, bin)/1024)
}

// TestNativeE2EBrowser runs the example with krate-shell --browser and drives
// it with Playwright, so it needs no windowing system.
func TestNativeE2EBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the native browser end-to-end test")
	}
	switch browser {
	case "chromium", "firefox", "webkit":
	default:
		t.Fatalf("unsupported KRATE_BROWSER %q", browser)
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	root := repoRoot(t)
	if err := exec.Command("node", "-e", "require('"+filepath.ToSlash(filepath.Join(root, "node_modules", "playwright"))+"')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	ex := e2eRequireDeps(t)
	work := t.TempDir()
	appDir := e2eBuildApp(t, ex, filepath.Join(work, "dist-native"))
	bin := e2eBuildShell(t, work)

	s := e2eStartShell(t, bin, appDir, "--browser")
	url := s.waitReady(60 * time.Second)
	if url == "" {
		t.Fatal("browser mode printed no url")
	}

	script, err := filepath.Abs(filepath.Join("testdata", "native_e2e", "browser.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	mainFlow, _ := filepath.Abs(filepath.Join("testdata", "native_e2e", "flow_main.js"))
	taskFlow, _ := filepath.Abs(filepath.Join("testdata", "native_e2e", "flow_task.js"))
	cmd := exec.Command("node", script, url, browser, mainFlow, taskFlow)
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(stderr.String(), "Executable doesn't exist") || strings.Contains(stderr.String(), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, stderr.String())
		}
		t.Fatalf("browser driver failed: %v\n%s\n%s", err, out, stderr.String())
	}
	var res struct {
		Reports []e2eStep `json:"reports"`
		Errors  []string  `json:"errors"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("driver output: %v\n%s", err, out)
	}
	e2eCheckSteps(t, res.Reports, 19)
	for _, e := range res.Errors {
		t.Errorf("browser error: %s", e)
	}
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}

// e2eGUIFlows drives the example in a running window through the dev control
// channel: the first page, the main flow and the dynamic task pages.
func e2eGUIFlows(t *testing.T, s *e2eShell) {
	t.Helper()
	ready := time.Since(s.start)
	// First page load: the Go host's task list is on screen.
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		var n int
		err := s.query(`document.querySelectorAll("#list li").length`, &n)
		if err == nil && n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the task list never rendered (n=%d, last query error: %v)", n, err)
		}
	}
	// Process start to the first document's load event, from the page's own
	// clock (the query polling above would add up to its retry interval).
	var loadedAt float64
	if err := s.query(`performance.timeOrigin + performance.getEntriesByType("navigation")[0].loadEventEnd`, &loadedAt); err != nil {
		t.Fatal(err)
	}
	firstLoad := time.Duration((loadedAt - float64(s.start.UnixMilli())) * float64(time.Millisecond))
	t.Logf("cold start: window ready %s, first page load event %s", ready.Round(time.Millisecond), firstLoad.Round(time.Millisecond))

	var steps []e2eStep
	if err := s.query(e2eReadFlow(t, "flow_main.js"), &steps); err != nil {
		t.Fatalf("main flow: %v", err)
	}
	e2eCheckSteps(t, steps, 17)
	e2eFootprint(t, s)

	for _, path := range []string{"/tasks/2", "/tasks/77"} {
		// Navigate from a timer so the query that starts it still answers,
		// then give the new document time to load before the next query:
		// a query that races the unload may never be answered.
		var started bool
		if err := s.query(fmt.Sprintf(`(setTimeout(() => location.assign(%q), 50), true)`, path), &started); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(100 * time.Millisecond) {
			var p string
			err := s.query(`location.pathname`, &p)
			if err == nil && p == path {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("never reached %s (path %q, last query error: %v)", path, p, err)
			}
		}
		var task []e2eStep
		if err := s.query(e2eReadFlow(t, "flow_task.js"), &task); err != nil {
			t.Fatalf("task flow %s: %v", path, err)
		}
		e2eCheckSteps(t, task, 1)
	}
}
