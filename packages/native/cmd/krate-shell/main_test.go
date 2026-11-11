package main

import (
	"bufio"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/gohosttest"
)

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
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

// TestBinaryDevControl builds the real binary and drives it the way the
// krate CLI will: browser mode, --dev, commands on stdin, replies on stdout.
func TestBinaryDevControl(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	root, cleanup, err := gohosttest.TempDir("shellbin-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	exe := filepath.Join(root, "krate-shell")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	app := filepath.Join(root, "app")
	os.MkdirAll(filepath.Join(app, "dist"), 0o755)
	os.MkdirAll(filepath.Join(app, "host"), 0o755)
	os.WriteFile(filepath.Join(app, "dist", "index.html"), []byte("<!doctype html>bin ok"), 0o644)
	os.WriteFile(filepath.Join(app, "host", "host.js"), []byte(`(function(){ console.log("host booted"); })();`), 0o644)
	os.WriteFile(filepath.Join(app, "app.json"), []byte(`{"appId":"dev.krate.bin","hostjs":{"bundle":"host/host.js"}}`), 0o644)

	cmd := exec.Command(exe, "--browser", "--dev", app)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	stderr := &syncBuf{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
		}
	}()

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func(what string) string {
		t.Helper()
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stdout closed before %s; stderr:\n%s", what, stderr.String())
			}
			return l
		case <-time.After(30 * time.Second):
			t.Fatalf("timed out waiting for %s; stderr:\n%s", what, stderr.String())
			return ""
		}
	}
	url := next("url line")
	if !strings.HasPrefix(url, "url http://127.0.0.1:") {
		t.Fatalf("first line = %q", url)
	}
	if l := next("ready"); l != "ready" {
		t.Fatalf("second line = %q", l)
	}

	jar, _ := cookiejar.New(nil)
	resp, err := (&http.Client{Jar: jar}).Get(strings.TrimPrefix(url, "url "))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "bin ok") {
		t.Errorf("page = %q", body)
	}

	send := func(cmdline string) string {
		t.Helper()
		io.WriteString(stdin, cmdline+"\n")
		return next("reply to " + cmdline)
	}
	if got := send("reload-hostjs"); got != "ok" {
		t.Errorf("reload-hostjs: %q", got)
	}
	if got := send("restart-gohost"); got != "ok" {
		t.Errorf("restart-gohost without a Go host: %q", got)
	}
	if got := send("eval 1"); !strings.HasPrefix(got, "error ") {
		t.Errorf("eval in browser mode: %q", got)
	}
	if got := send("nonsense"); !strings.HasPrefix(got, "error unknown command") {
		t.Errorf("unknown: %q", got)
	}
	// Host console output stays off the control channel.
	if !strings.Contains(stderr.String(), "host booted") {
		t.Errorf("host console output missing from stderr: %q", stderr.String())
	}
	if got := send("quit"); got != "ok" {
		t.Errorf("quit: %q", got)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("exit: %v\n%s", err, stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("process did not exit after quit")
	}
}

// Closing stdin (the driving process died) also ends a --dev shell.
func TestBinaryExitsOnStdinEOF(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	root, cleanup, err := gohosttest.TempDir("shellbin2-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	exe := filepath.Join(root, "krate-shell")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	app := filepath.Join(root, "app")
	os.MkdirAll(filepath.Join(app, "dist"), 0o755)
	os.WriteFile(filepath.Join(app, "app.json"), []byte(`{"appId":"dev.krate.bin"}`), 0o644)

	cmd := exec.Command(exe, "--browser", "--dev", app)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Scan() // url
	sc.Scan() // ready
	stdin.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		cmd.Process.Kill()
		t.Fatal("process did not exit after stdin EOF")
	}
}
