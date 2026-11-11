// Package dbustest starts a private dbus-daemon for tests. Tests skip when the
// daemon binary is not installed.
package dbustest

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Start runs a private session bus and returns its address. The daemon stops
// when the test ends. The test is skipped when dbus-daemon is missing.
func Start(t testing.TB) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	dir, err := os.MkdirTemp(os.Getenv("KRATE_TEST_TMP"), "krate-dbus-")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "bus")
	conf := filepath.Join(dir, "bus.conf")
	cfg := fmt.Sprintf(`<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN" "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:path=%s</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>`, sock)
	if err := os.WriteFile(conf, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--config-file="+conf, "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start dbus-daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait() // reaps the daemon; the kill error is the expected one
		os.RemoveAll(dir)
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Skipf("dbus-daemon printed no address: %v", err)
	}
	return strings.TrimSpace(line)
}
