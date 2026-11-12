//go:build windows

package windows

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/toastscheme"
)

// Toast activation after the app quit. A real click on a toast cannot be made
// headless, so the test plays the part of Windows: it runs the test binary
// again with the activation URL as the argument, exactly what the registered
// protocol handler does.
//
//   - with the app running, the second process must hand the click over (the
//     first app gets the notification event, the second exits);
//   - without a running instance, the second process must keep the click and
//     deliver it as a launch event once a handler is set.

const toastHelperEnv = "KRATE_TEST_TOAST_HELPER"

// toastHelperMain runs in the second process (see TestMain).
func toastHelperMain() {
	appID := os.Getenv("KRATE_TEST_APPID")
	args := strings.Fields(os.Getenv(toastHelperEnv))
	a, err := New(Config{AppID: appID, AppName: "Krate toast helper", Handler: guiHandler(), Args: args})
	if err == ErrActivationForwarded {
		fmt.Println("RESULT forwarded")
		os.Exit(0)
	}
	if err != nil {
		fmt.Println("RESULT error", err)
		os.Exit(3)
	}
	got := make(chan caps.NotifyEvent, 1)
	a.OnNotify(func(ev caps.NotifyEvent) { got <- ev })
	select {
	case ev := <-got:
		fmt.Printf("RESULT launch id=%s action=%s launch=%v\n", ev.ID, ev.Action, ev.Launch)
	case <-time.After(5 * time.Second):
		fmt.Println("RESULT none")
	}
	os.Exit(0)
}

func runToastHelper(t *testing.T, appID, uri string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=NoSuchTest")
	cmd.Env = append(os.Environ(), toastHelperEnv+"="+uri, "KRATE_TEST_APPID="+appID)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("helper failed: %v\n%s", err, out.String())
		}
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("helper timed out\n%s", out.String())
	}
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(l, "RESULT ") {
			return strings.TrimSpace(strings.TrimPrefix(l, "RESULT "))
		}
	}
	t.Fatalf("helper printed no result:\n%s", out.String())
	return ""
}

func TestGUIToastActivationFromASecondProcess(t *testing.T) {
	needGUI(t)
	var evs notifyEvents
	guiApp.OnNotify(evs.add)
	scheme := toastscheme.Scheme(guiApp.cfg.AppID)

	// The running app gets a click handed over by a new process.
	got := runToastHelper(t, guiApp.cfg.AppID, toastscheme.URI(scheme, "cold1", "btn"))
	if got != "forwarded" {
		t.Fatalf("with the app running the second process should hand the click over, it reported %q", got)
	}
	waitFor(t, "the forwarded click to arrive", func() bool { return len(evs.snapshot()) > 0 })
	ev := evs.snapshot()[0]
	if ev.ID != "cold1" || ev.Action != "btn" || ev.Launch {
		t.Errorf("forwarded event = %+v", ev)
	}

	// With no instance running, the click is kept for the new process.
	other := guiApp.cfg.AppID + ".cold"
	got = runToastHelper(t, other, toastscheme.URI(toastscheme.Scheme(other), "cold2", ""))
	if got != "launch id=cold2 action= launch=true" {
		t.Errorf("cold start reported %q", got)
	}

	// A URL of another app is not a click for this one.
	got = runToastHelper(t, other, toastscheme.URI(scheme, "x", ""))
	if got != "none" {
		t.Errorf("a foreign URL was treated as a click: %q", got)
	}
}

func TestGUIToastProtocolRegistration(t *testing.T) {
	needGUI(t)
	if isPackaged() {
		t.Skip("a packaged app declares the scheme in its manifest")
	}
	scheme := toastscheme.Scheme(guiApp.cfg.AppID)
	defer deleteToastProtocol(scheme)
	defer deleteAUMID(aumidFor(guiApp.cfg.AppID))

	err := guiApp.Notify(caps.Notification{ID: "reg1", Title: "Registration test", Body: "x"})
	if err != nil && strings.Contains(err.Error(), "turned off") {
		t.Skipf("notifications are disabled on this machine: %v", err)
	}
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+scheme+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("the activation scheme is not registered: %v", err)
	}
	defer k.Close()
	cmd, _, err := k.GetStringValue("")
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if !strings.EqualFold(cmd, `"`+exe+`" "%1"`) {
		t.Errorf("handler command = %q, want the test executable %q with %%1", cmd, exe)
	}
	pk, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+scheme, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer pk.Close()
	if _, _, err := pk.GetStringValue("URL Protocol"); err != nil {
		t.Errorf("URL Protocol marker: %v", err)
	}
}
