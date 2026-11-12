package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/native/wire"
)

// These tests debug the Go host of examples/native-tasks with a real Delve:
// set a breakpoint on a host function through the Delve JSON-RPC API, call
// the function from outside, and see the breakpoint hit with the right file,
// line and local variable. They need dlv (go install
// github.com/go-delve/delve/cmd/dlv@latest, or $KRATE_DLV), the built JS
// packages and a Go toolchain.

func e2eRequireDlv(t *testing.T) string {
	t.Helper()
	dlv, err := FindDlv()
	if err != nil {
		requireE2E(t, "%v", err)
	}
	return dlv
}

// e2eCopyExample copies an example into a temp dir, so a test can edit its
// sources: node_modules is linked, and the replace line of go.mod made
// absolute.
func e2eCopyExample(t *testing.T, ex string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "app")
	err := filepath.WalkDir(ex, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(ex, p)
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		switch d.Name() {
		case "node_modules":
			if d.IsDir() {
				if err := os.Symlink(p, filepath.Join(dst, rel)); err != nil {
					return err
				}
				return filepath.SkipDir
			}
		case ".krate", "dist", "dist-native":
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(dst, "go.mod")
	data, err := os.ReadFile(mod)
	if err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(repoRoot(t), "packages", "native")
	out := strings.ReplaceAll(string(data), "=> ../../packages/native", "=> "+filepath.ToSlash(native))
	if out == string(data) {
		t.Fatalf("go.mod has no replace line to make absolute:\n%s", data)
	}
	if err := os.WriteFile(mod, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	if sum, err := os.ReadFile(filepath.Join(ex, "go.sum")); err == nil {
		_ = os.WriteFile(filepath.Join(dst, "go.sum"), sum, 0o644)
	}
	return dst
}

// delve is a JSON-RPC client of a headless Delve server.
type delve struct {
	t    *testing.T
	addr string
	c    *rpc.Client
}

func dialDelve(t *testing.T, addr string, wait time.Duration) *delve {
	t.Helper()
	var last error
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			last = err
			continue
		}
		d := &delve{t: t, addr: addr, c: jsonrpc.NewClient(conn)}
		// A server that accepts the connection but is not Delve yet, or is
		// shutting down, fails here.
		var st map[string]any
		if err := d.rawCall("State", map[string]any{"NonBlocking": true}, &st, 10*time.Second); err != nil {
			_ = d.c.Close()
			last = err
			continue
		}
		return d
	}
	t.Fatalf("no Delve server at %s within %s: %v", addr, wait, last)
	return nil
}

// rawCall is a JSON-RPC call that gives up after wait: a Delve that is
// running the program does not answer calls that need it stopped.
func (d *delve) rawCall(method string, in any, out *map[string]any, wait time.Duration) error {
	call := d.c.Go("RPCServer."+method, in, out, make(chan *rpc.Call, 1))
	select {
	case <-call.Done:
		return call.Error
	case <-time.After(wait):
		return fmt.Errorf("no answer to %s within %s", method, wait)
	}
}

func (d *delve) call(method string, in any) map[string]any {
	d.t.Helper()
	out := map[string]any{}
	if err := d.rawCall(method, in, &out, 30*time.Second); err != nil {
		d.t.Fatalf("delve %s: %v", method, err)
	}
	return out
}

// pid stops the program (the process id needs it stopped) and returns it.
func (d *delve) pid() int {
	d.call("Command", map[string]any{"name": "halt"})
	return int(d.call("ProcessPid", map[string]any{})["Pid"].(float64))
}

// breakAt halts the program, sets a breakpoint and returns its id.
func (d *delve) breakAt(file string, line int) int {
	d.t.Helper()
	// The program runs from the start (dlv --continue): stop it to change
	// breakpoints, like an editor does.
	d.call("Command", map[string]any{"name": "halt"})
	out := d.call("CreateBreakpoint", map[string]any{"Breakpoint": map[string]any{"file": file, "line": line}})
	bp, _ := out["Breakpoint"].(map[string]any)
	if bp != nil {
		d.t.Logf("breakpoint %v at %v:%v in %v (addrs %v, disabled %v)", bp["id"], bp["file"], bp["line"], bp["functionName"], bp["addrs"], bp["disabled"])
	}
	if bp == nil {
		d.t.Fatalf("no breakpoint in %v", out)
	}
	return int(bp["id"].(float64))
}

// resume continues and returns the state at the next stop, asynchronously.
func (d *delve) resume() <-chan map[string]any {
	ch := make(chan map[string]any, 1)
	go func() {
		out := map[string]any{}
		if err := d.c.Call("RPCServer.Command", map[string]any{"name": "continue"}, &out); err != nil {
			out["error"] = err.Error()
		}
		if st, ok := out["State"].(map[string]any); ok {
			th, _ := st["currentThread"].(map[string]any)
			d.t.Logf("continue returned: running %v exited %v thread %v", st["Running"], st["exited"], th)
		} else {
			d.t.Logf("continue returned: %v", out)
		}
		ch <- out
	}()
	return ch
}

// stoppedAt checks a continue result: it stopped on a breakpoint at file:line
// in a function whose name contains fn.
func stoppedAt(t *testing.T, res map[string]any, file string, line int, fn string) {
	t.Helper()
	if e, ok := res["error"]; ok {
		t.Fatalf("continue failed: %v", e)
	}
	st, _ := res["State"].(map[string]any)
	th, _ := st["currentThread"].(map[string]any)
	if th == nil || th["breakPoint"] == nil {
		t.Fatalf("the program did not stop on a breakpoint: %v", res)
	}
	if got := th["file"]; got != file || int(th["line"].(float64)) != line {
		t.Fatalf("stopped at %v:%v, want %s:%d", got, th["line"], file, line)
	}
	f, _ := th["function"].(map[string]any)
	if f == nil || !strings.Contains(fmt.Sprint(f["name"]), fn) {
		t.Fatalf("stopped in %v, want a function containing %q", f, fn)
	}
}

// evalString reads a Go string variable in the selected frame.
func (d *delve) evalString(expr string) string {
	d.t.Helper()
	out := d.call("Eval", map[string]any{"Scope": map[string]any{"GoroutineID": -1, "Frame": 0}, "Expr": expr})
	v, _ := out["Variable"].(map[string]any)
	if v == nil {
		d.t.Fatalf("eval %s: %v", expr, out)
	}
	return fmt.Sprint(v["value"])
}

func fileLineOf(t *testing.T, file, needle string) int {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	t.Fatalf("%q is not in %s", needle, file)
	return 0
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

const taskAddMarker = "t := Task{ID: next, Title: title}"

// TestNativeDebugGoHostGUI debugs the Go host in a real window: the call that
// hits the breakpoint is the page's own, started through the dev control
// channel's query command.
func TestNativeDebugGoHostGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	dlv := e2eRequireDlv(t)
	ex := e2eRequireDeps(t)
	proj := e2eCopyExample(t, ex)
	work := t.TempDir()

	cfg, err := config.Load(proj)
	if err != nil {
		t.Fatal(err)
	}
	b := New(proj, cfg)
	b.DebugGo = true
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: filepath.Join(work, "dist-native"), Dev: true})
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	bin := e2eBuildShell(t, work)

	addr := freeAddr(t)
	t.Setenv(GoHostDebugEnv, addr)
	t.Setenv(GoHostDlvEnv, dlv)
	s := e2eStartShell(t, bin, res.AppDir)
	s.waitReady(60 * time.Second)

	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		var n int
		if err := s.query(`document.querySelectorAll("#list li").length`, &n); err == nil && n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the task list never appeared")
		}
	}

	d := dialDelve(t, addr, 30*time.Second)
	file := filepath.Join(proj, "src", "lib", "tasks.go")
	line := fileLineOf(t, file, taskAddMarker)
	d.breakAt(file, line)
	hit := d.resume()

	// The page adds a task: the click calls the Go function Add.
	if err := s.query(`(() => { const i = document.querySelector("#title"); i.value = "debugged task";
		i.dispatchEvent(new Event("input", {bubbles: true})); document.querySelector("#add").click(); return true })()`, nil); err != nil {
		t.Fatal(err)
	}
	var res1 map[string]any
	select {
	case res1 = <-hit:
	case <-time.After(30 * time.Second):
		t.Fatal("the breakpoint in Add was not hit")
	}
	stoppedAt(t, res1, file, line, "lib.Add")
	if got := d.evalString("title"); got != "debugged task" {
		t.Fatalf("title = %q", got)
	}

	// Let it go on: the page then shows the new task.
	d2 := dialDelve(t, addr, 5*time.Second)
	defer d2.c.Close()
	d2.resume()
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		var txt string
		if err := s.query(`document.querySelector("#list").textContent`, &txt); err == nil && strings.Contains(txt, "debugged task") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the task did not appear after continuing")
		}
	}
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}

// callHost calls a host function over the shell's browser-mode HTTP endpoint.
func callHost(client *http.Client, base, fn string, args ...string) error {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String(fn)
	e.Key("args")
	e.BeginArray()
	for _, a := range args {
		e.String(a)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	resp, err := client.Post(base+"/__krate/call", "application/octet-stream", bytes.NewReader(e.Frame()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", resp.Status, body)
	}
	// A failed call is still an HTTP 200; the error is in the frame.
	if bytes.Contains(body, []byte(`"err":`)) {
		return fmt.Errorf("%s: %s", fn, bytes.TrimSpace(bytes.Trim(body, "\x00")))
	}
	return nil
}

// TestNativeDebugGoHostRestart runs the whole dev loop (krate native dev
// --debug-go) in browser mode: a breakpoint is hit, a Go edit rebuilds and
// restarts the host under a fresh Delve on the same address, and the
// breakpoint can be set and hit again after attaching again.
func TestNativeDebugGoHostRestart(t *testing.T) {
	dlv := e2eRequireDlv(t)
	ex := e2eRequireDeps(t)
	proj := e2eCopyExample(t, ex)
	work := t.TempDir()
	bin := e2eBuildShell(t, work)

	var listening atomic.Int32
	urlCh := make(chan string, 1)
	addrCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		done <- RunNativeDev(NativeDevOptions{
			LoadConfig: func() (string, *config.Config, map[string]string, error) {
				c, err := config.Load(proj)
				return proj, c, nil, err
			},
			Browser: true,
			Shell:   bin,
			Debug:   &NativeDebugOptions{GoHost: true, Addr: "127.0.0.1:0", Dlv: dlv},
			OnURL:   func(u string) { urlCh <- u },
			OnDebug: func(a string) { addrCh <- a },
			Stderr:  &countingLog{testLogWriter: testLogWriter{t: t}, n: &listening},
			Ctx:     ctx,
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("native dev did not stop")
		}
		wg.Wait()
	})

	var addr, url string
	for addr == "" || url == "" {
		select {
		case addr = <-addrCh:
		case url = <-urlCh:
		case err := <-done:
			t.Fatalf("native dev ended early: %v", err)
		case <-time.After(180 * time.Second):
			t.Fatalf("native dev is not up (delve %q, url %q)", addr, url)
		}
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 60 * time.Second}
	if resp, err := client.Get(url); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
	}
	base := url[:strings.Index(url, "/?")]

	file := filepath.Join(proj, "src", "lib", "tasks.go")
	line := fileLineOf(t, file, taskAddMarker)

	d := dialDelve(t, addr, 60*time.Second)
	_ = d.pid()
	d.breakAt(file, line)
	hit := d.resume()
	callErr := make(chan error, 1)
	go func() { callErr <- callHost(client, base, "src/lib/tasks.Add", "first") }()
	var res map[string]any
	select {
	case res = <-hit:
	case <-time.After(30 * time.Second):
		t.Fatal("the breakpoint in Add was not hit")
	}
	stoppedAt(t, res, file, line, "lib.Add")
	if got := d.evalString("title"); got != "first" {
		t.Fatalf("title = %q", got)
	}
	d2 := dialDelve(t, addr, 5*time.Second)
	d2.resume()
	if err := <-callErr; err != nil {
		t.Fatalf("the call did not finish after continuing: %v", err)
	}
	_ = d.c.Close()
	_ = d2.c.Close()

	// Edit the Go source: a comment line above the breakpoint moves it down.
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(src), "\t"+taskAddMarker, "\t// edited while debugging\n\t"+taskAddMarker, 1)
	if edited == string(src) {
		t.Fatal("the edit did not apply")
	}
	if err := os.WriteFile(file, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// The old Delve goes away; a new one comes up on the same address and
	// debugs a new process. The edit also shifted the line.
	newLine := fileLineOf(t, file, taskAddMarker)
	if newLine != line+1 {
		t.Fatalf("line %d after the edit, want %d", newLine, line+1)
	}
	// Wait for the second "API server listening" line: that is the new
	// Delve. The program is not halted here: a halted program never reports
	// ready, and the restart waits for that.
	for deadline := time.Now().Add(180 * time.Second); listening.Load() < 2; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no new Delve server after the Go edit")
		}
	}
	// The new host reports ready once it runs; until then calls are refused.
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		if err := callHost(client, base, "src/lib/tasks.List"); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("the restarted host never became ready: %v", err)
		}
	}
	d3 := dialDelve(t, addr, 30*time.Second)
	d3.breakAt(file, newLine)
	hit = d3.resume()
	go func() { callErr <- callHost(client, base, "src/lib/tasks.Add", "second") }()
	select {
	case res = <-hit:
	case <-time.After(30 * time.Second):
		var st, bps map[string]any
		_ = d3.rawCall("State", map[string]any{"NonBlocking": true}, &st, 5*time.Second)
		_ = d3.rawCall("ListBreakpoints", map[string]any{}, &bps, 5*time.Second)
		select {
		case err := <-callErr:
			t.Logf("the call returned: %v", err)
		default:
		}
		if out, err := exec.Command("ps", "-eo", "pid,ppid,args").CombinedOutput(); err == nil {
			for _, l := range strings.Split(string(out), "\n") {
				if strings.Contains(l, filepath.Base(proj)) || strings.Contains(l, "krate-gohost") {
					t.Logf("process: %s", l)
				}
			}
		}
		t.Fatalf("the breakpoint was not hit after the restart (pid-less diagnostics)\n state: %v\n breakpoints: %v", st, bps)
	}
	stoppedAt(t, res, file, newLine, "lib.Add")
	if got := d3.evalString("title"); got != "second" {
		t.Fatalf("title = %q", got)
	}
	d4 := dialDelve(t, addr, 5*time.Second)
	d4.resume()
	if err := <-callErr; err != nil {
		t.Fatalf("the call after the restart: %v", err)
	}
}

// countingLog logs like testLogWriter and counts Delve start-up lines.
type countingLog struct {
	testLogWriter
	n *atomic.Int32
}

func (w *countingLog) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "API server listening") {
		w.n.Add(1)
	}
	return w.testLogWriter.Write(p)
}
