//go:build linux

// Package guitest runs the UI half of the SDK against the real Linux
// platform (GTK3 + WebKitGTK through purego). It needs a display and the
// WebKitGTK libraries and runs only with KRATE_NATIVE_GUI=1; see
// ~/wt/kn-sysroot/README.md for a rootless setup. With KRATE_NATIVE_WM=1 and a
// window manager on the display (xvfb-start-wm.sh) the fullscreen, maximize
// and minimize checks use real window manager state.
package guitest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	sdkall "github.com/kratejs/krate/packages/native/sdk/all"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

var (
	plat platform.App
	host *sdk.Host
	ui   http.Handler
)

func TestMain(m *testing.M) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		os.Exit(m.Run())
	}
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(os.TempDir(), fmt.Sprintf("krate-guitest-%d", os.Getpid())))
	os.Setenv("KRATE_APP_ID", "dev.krate.sdkui")
	defer os.RemoveAll(os.Getenv("XDG_CONFIG_HOME"))
	host = sdk.NewHost()
	reg := scheme.NewRegistry()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>"+r.URL.Path+"</title><body>"+r.URL.Path)
	})
	set := permission.AllowAll()
	// The ids the view tests call: a data id the window scopes can narrow.
	permission.Register(reg, set, "krate/kv.get", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		out.String("secret")
		return nil
	})
	registerForgeHandlers(mux)
	registerChatHandlers(mux)
	appHandler := scheme.New(scheme.Options{Registry: reg, Identify: identify})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/__krate/") {
			appHandler.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	app, err := platform.New(platform.Options{AppID: "dev.krate.sdkui", Handler: handler, Debug: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "GUI test setup:", err)
		os.Exit(1)
	}
	plat = app
	host.Attach(app)
	sdkall.RegisterUI(reg, host, set, nil)
	ui = appHandler
	code := 0
	err = app.Run(func() {
		w, err := app.NewWindow(platform.WindowOptions{Title: "main", Width: 640, Height: 480, Resizable: true, Path: "/main"})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		host.AddWindow("main", w)
		go func() {
			code = m.Run()
			app.Quit()
		}()
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	host.Close()
	os.Exit(code)
}

func need(t *testing.T) {
	t.Helper()
	if plat == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

var _ = context.Background
var _ caps.WindowRef
