package gohost_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

var (
	buildOnce sync.Once
	buildDir  string
	bin1      string
	bin2      string
	buildErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// binaries builds two versions of the test child once.
func binaries(t *testing.T) (string, string) {
	t.Helper()
	buildOnce.Do(func() {
		var err error
		buildDir, _, err = gohosttest.TempDir("gohost-")
		if err != nil {
			buildErr = err
			return
		}
		if bin1, err = gohosttest.Build(buildDir, "child-v1", gohosttest.ChildSource("v1", 10)); err != nil {
			buildErr = err
			return
		}
		bin2, buildErr = gohosttest.Build(buildDir, "child-v2", gohosttest.ChildSource("v2", 500))
	})
	if buildErr != nil {
		if strings.Contains(buildErr.Error(), "toolchain not found") {
			t.Skip(buildErr)
		}
		t.Fatal(buildErr)
	}
	return bin1, bin2
}

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

// frontend is the window-facing side: a scheme handler whose registry
// forwards to the child.
func frontend(t *testing.T, child *gohost.Child) *httptest.Server {
	t.Helper()
	reg := scheme.NewRegistry()
	child.Remote().RegisterInto(reg, []string{"t.Version", "t.Add", "t.Rev", "t.Wait", "t.Bump"}, []string{"t.Count"}, []string{"t.Count"})
	ts := httptest.NewServer(scheme.New(scheme.Options{Registry: reg}))
	t.Cleanup(ts.Close)
	return ts
}

func call(t *testing.T, ts *httptest.Server, fn string, args func(*wire.Encoder)) *wire.Decoder {
	t.Helper()
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
	if args != nil {
		args(e)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	resp, err := http.Post(ts.URL+"/__krate/call", "application/x-krate-frames", bytes.NewReader(e.Frame()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	d, err := wire.ReadFrame(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	for d.More() {
		k, _ := d.Key()
		if k == "ok" {
			sub, err := d.SubDecoder()
			if err != nil {
				t.Fatal(err)
			}
			return sub
		}
		if k == "err" {
			raw, _ := d.RawValue()
			t.Fatalf("call %s failed: %s", fn, raw)
		}
		if err := d.Skip(); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("call %s: no result", fn)
	return nil
}

func version(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	s, err := call(t, ts, "t.Version", nil).String()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func watch(t *testing.T, ts *httptest.Server) (<-chan int64, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/__krate/signals?names=t.Count", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan int64, 64)
	go func() {
		defer resp.Body.Close()
		for {
			d, err := wire.ReadFrame(resp.Body)
			if err != nil {
				return
			}
			if d.BeginObject() != nil {
				return
			}
			for d.More() {
				k, _ := d.Key()
				if k == "v" {
					v, _ := d.Int()
					out <- v
				} else {
					if d.Skip() != nil {
						return
					}
				}
			}
		}
	}()
	return out, cancel
}

func recv(t *testing.T, ch <-chan int64, what string) int64 {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return 0
	}
}

func TestChildLifecycle(t *testing.T) {
	b1, b2 := binaries(t)
	logs := &logSink{}
	child := gohost.NewChild(gohost.ChildOptions{Logf: logs.logf})
	if err := child.Start(context.Background(), b1, []string{"KRATE_TEST=1"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := child.Stop(); err != nil {
			t.Error(err)
		}
	}()
	ts := frontend(t, child)

	if v := version(t, ts); v != "v1" {
		t.Fatalf("version = %q", v)
	}
	sum := call(t, ts, "t.Add", func(e *wire.Encoder) { e.Int(20); e.Int(22) })
	if n, err := sum.Int(); err != nil || n != 42 {
		t.Fatalf("add = %d, %v", n, err)
	}
	rev := call(t, ts, "t.Rev", func(e *wire.Encoder) { e.Bytes([]byte{1, 2, 3}) })
	if b, err := rev.Bytes(); err != nil || !bytes.Equal(b, []byte{3, 2, 1}) {
		t.Fatalf("rev = %v, %v", b, err)
	}

	vals, stop := watch(t, ts)
	defer stop()
	if v := recv(t, vals, "initial signal"); v != 10 {
		t.Fatalf("signal = %d", v)
	}
	call(t, ts, "t.Bump", nil)
	if v := recv(t, vals, "bumped signal"); v != 11 {
		t.Fatalf("signal = %d", v)
	}

	// Restart with a changed binary: the window-facing handler and the open
	// signal connection survive; the signal shows the new child's value.
	oldExit := child.Exited()
	if err := child.Restart(b2); err != nil {
		t.Fatal(err)
	}
	select {
	case <-oldExit:
	default:
		t.Error("old child still running after Restart")
	}
	if v := recv(t, vals, "signal from restarted child"); v != 500 {
		t.Fatalf("signal after restart = %d", v)
	}
	if v := version(t, ts); v != "v2" {
		t.Fatalf("version after restart = %q", v)
	}
	call(t, ts, "t.Bump", nil)
	if v := recv(t, vals, "bump after restart"); v != 501 {
		t.Fatalf("signal = %d", v)
	}

	// Restart with the same binary keeps working too.
	if err := child.Restart(""); err != nil {
		t.Fatal(err)
	}
	if v := recv(t, vals, "signal after second restart"); v != 500 {
		t.Fatalf("signal = %d", v)
	}

	dir := filepath.Dir(sockOf(t, child))
	if err := child.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("socket directory %s remains after Stop", dir)
	}
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String("t.Version")
	e.EndObject()
	e.EndArray()
	e.EndObject()
	resp, err := http.Post(ts.URL+"/__krate/call", "application/x-krate-frames", bytes.NewReader(e.Frame()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.String(), `"unavailable"`) {
		t.Errorf("call after Stop: %q", body.String())
	}
}

// sockOf finds the socket directory through the Client's dialer.
func sockOf(t *testing.T, c *gohost.Child) string {
	t.Helper()
	tr := c.Client().Transport.(*http.Transport)
	conn, err := tr.DialContext(context.Background(), "unix", "")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	return conn.RemoteAddr().String()
}

type lockedWriter struct {
	mu *sync.Mutex
	b  *bytes.Buffer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

// Run on a tcp endpoint announces its port on the ready line and refuses
// requests without the token.
func TestRunTCPToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rmu sync.Mutex
	var ready bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- gohost.Run(ctx, nil, gohost.Config{Socket: "tcp:127.0.0.1:0", Token: "s3cret", Ready: lockedWriter{&rmu, &ready}})
	}()
	var addr string
	eventually(t, func() bool {
		rmu.Lock()
		defer rmu.Unlock()
		line := strings.TrimSpace(ready.String())
		if a, ok := strings.CutPrefix(line, "ready tcp:"); ok {
			addr = a
			return true
		}
		return false
	})
	get := func(token string) int {
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/__krate/nothing", nil)
		if token != "" {
			req.Header.Set("X-Krate-Token", token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := get(""); c != 401 {
		t.Errorf("no token: %d", c)
	}
	if c := get("wrong"); c != 401 {
		t.Errorf("wrong token: %d", c)
	}
	if c := get("s3cret"); c == 401 {
		t.Errorf("right token refused")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
}
