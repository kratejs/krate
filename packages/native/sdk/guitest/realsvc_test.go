//go:build linux

package guitest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus/dbustest"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

// needServices skips the test unless the GUI tests are on and every named
// daemon binary is installed. KRATE_REQUIRE_E2E=1 turns a missing binary into
// a failure, so CI cannot silently lose these tests.
func needServices(t *testing.T, bins ...string) {
	t.Helper()
	need(t)
	for _, b := range bins {
		if _, err := exec.LookPath(b); err != nil {
			if os.Getenv("KRATE_REQUIRE_E2E") != "" {
				t.Fatalf("%s is required (KRATE_REQUIRE_E2E) but is not installed", b)
			}
			t.Skipf("%s is not installed", b)
		}
	}
}

// privateBus starts a session bus for this test and points the SDK at it.
func privateBus(t *testing.T) string {
	t.Helper()
	addr := dbustest.Start(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	sessionbus.Reset()
	t.Cleanup(sessionbus.Reset)
	return addr
}

func busClient(t *testing.T, addr string) *dbusiface.Client {
	t.Helper()
	c, err := dbusiface.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// daemon is a real desktop service process started by a test.
type daemon struct {
	cmd  *exec.Cmd
	done chan struct{}
	out  *bytes.Buffer
}

func startDaemon(t *testing.T, bin string, args ...string) *daemon {
	t.Helper()
	d := &daemon{cmd: exec.Command(bin, args...), done: make(chan struct{}), out: &bytes.Buffer{}}
	d.cmd.Stdout, d.cmd.Stderr = d.out, d.out
	if err := d.cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	go func() { d.cmd.Wait(); close(d.done) }()
	t.Cleanup(d.stop)
	return d
}

func (d *daemon) stop() {
	select {
	case <-d.done:
		return
	default:
	}
	d.cmd.Process.Kill()
	<-d.done
}

func (d *daemon) exited() bool {
	select {
	case <-d.done:
		return true
	default:
		return false
	}
}

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func scratchDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.Getenv("KRATE_TEST_TMP"), "krate-svc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
