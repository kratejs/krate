//go:build windows

package windows

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
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

// The WebView2 Runtime missing: the app must say so in a message box, not
// exit silently. The helper process runs the app with the runtime hidden
// (KRATE_NATIVE_SIMULATE_NO_WEBVIEW2); the test finds the box, checks its text
// and presses No, which makes the helper exit with the error.

const runtimeHelperEnv = "KRATE_TEST_RUNTIME_HELPER"

func runtimeHelperMain() {
	a, err := New(Config{AppID: "dev.krate.native.runtimehelper", AppName: "Krate Runtime Helper", Handler: guiHandler(), Args: []string{}})
	if err != nil {
		fmt.Println("RESULT new", err)
		os.Exit(3)
	}
	err = a.Run(nil)
	var re *runtimeError
	if errors.As(err, &re) && re.missing {
		fmt.Println("RESULT runtime missing")
		os.Exit(0)
	}
	fmt.Println("RESULT other", err)
	os.Exit(4)
}
