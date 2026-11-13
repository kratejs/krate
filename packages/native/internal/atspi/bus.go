package atspi

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// registryd returns the path of at-spi2-registryd or "".
func registryd() string {
	if p := os.Getenv("KRATE_ATSPI_REGISTRYD"); p != "" {
		return p
	}
	cands := []string{
		"/usr/libexec/at-spi2-registryd",
		"/usr/lib/at-spi2-core/at-spi2-registryd",
		"/usr/lib/at-spi2/at-spi2-registryd",
		"/usr/lib/x86_64-linux-gnu/at-spi2-core/at-spi2-registryd",
		"/usr/lib/aarch64-linux-gnu/at-spi2-core/at-spi2-registryd",
	}
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ErrMissing is returned by Start when a required program is not installed.
type ErrMissing string

func (e ErrMissing) Error() string { return string(e) }

// Start runs a private accessibility bus with the AT-SPI registry on it and
// returns the bus address and a stop function. It returns an ErrMissing when
// dbus-daemon or at-spi2-registryd is not installed.
func Start() (addr string, stop func(), err error) {
	dbusd, err := exec.LookPath("dbus-daemon")
	if err != nil {
		return "", nil, ErrMissing("dbus-daemon not installed")
	}
	reg := registryd()
	if reg == "" {
		return "", nil, ErrMissing("at-spi2-registryd not installed (package at-spi2-core; or set KRATE_ATSPI_REGISTRYD)")
	}
	dir, err := os.MkdirTemp(os.Getenv("KRATE_TEST_TMP"), "krate-a11y-")
	if err != nil {
		return "", nil, err
	}
	var procs []*exec.Cmd
	stop = func() {
		for _, p := range procs {
			_ = p.Process.Kill()
			_ = p.Wait() // the kill error is the expected one
		}
		os.RemoveAll(dir)
	}
	fail := func(err error) (string, func(), error) {
		stop()
		return "", nil, err
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
		return fail(err)
	}
	cmd := exec.Command(dbusd, "--config-file="+conf, "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return fail(err)
	}
	if err := cmd.Start(); err != nil {
		return fail(fmt.Errorf("cannot start dbus-daemon: %w", err))
	}
	procs = append(procs, cmd)
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		return fail(fmt.Errorf("dbus-daemon printed no address: %w", err))
	}
	addr = strings.TrimSpace(line)

	rc := exec.Command(reg, "--use-gnome-session")
	rc.Env = append(os.Environ(), "AT_SPI_BUS_ADDRESS="+addr, "DBUS_SESSION_BUS_ADDRESS="+addr)
	rout, _ := rc.StdoutPipe()
	rc.Stderr = os.Stderr
	if err := rc.Start(); err != nil {
		return fail(fmt.Errorf("cannot start at-spi2-registryd: %w", err))
	}
	procs = append(procs, rc)
	if l, err := bufio.NewReader(rout).ReadString('\n'); err != nil || !strings.Contains(l, "running") {
		return fail(fmt.Errorf("at-spi2-registryd did not start: %q %v", l, err))
	}
	return addr, stop, nil
}

// StartBus is Start for a test: it skips when the programs are missing and
// stops the processes when the test ends.
func StartBus(t testing.TB) string {
	t.Helper()
	addr, stop, err := Start()
	if err != nil {
		if _, ok := err.(ErrMissing); ok {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return addr
}
