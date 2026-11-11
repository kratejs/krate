// Package dbustest starts a private session bus for tests.
package dbustest

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Start runs dbus-daemon on a private socket and returns its address. The
// test is skipped when dbus-daemon is not installed. The daemon is stopped
// when the test ends.
func Start(t testing.TB) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	dir, err := os.MkdirTemp(os.Getenv("KRATE_TEST_TMP"), "krate-dbus")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "bus")
	cmd := exec.Command(bin, "--session", "--nofork", "--print-address=1", "--address=unix:path="+sock)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start dbus-daemon: ", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		os.RemoveAll(dir)
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon printed no address: %v", err)
	}
	return strings.TrimSpace(line)
}
