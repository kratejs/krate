//go:build linux

package a11y

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/internal/atspi"
	"github.com/kratejs/krate/packages/native/platform/linux"
)

// These tests run the real GTK/WebKitGTK app with the AT-SPI bridge on and
// read its accessibility tree over a private accessibility bus. They need
// KRATE_NATIVE_GUI=1, a display, and at-spi2-registryd (package at-spi2-core).

var (
	app    *linux.App
	client *atspi.Client
	mux    = http.NewServeMux()
	skip   string
)

const appID = "dev.krate.a11ytest"

func libcSetenv(k, v string) {
	lib, err := purego.Dlopen("libc.so.6", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	var set func(k, v string, overwrite int32) int32
	purego.RegisterLibFunc(&set, lib, "setenv")
	set(k, v, 1)
}

func libcUnsetenv(k string) {
	lib, err := purego.Dlopen("libc.so.6", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	var un func(k string) int32
	purego.RegisterLibFunc(&un, lib, "unsetenv")
	un(k)
}

func TestMain(m *testing.M) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		os.Exit(m.Run())
	}
	addr, stop, err := atspi.Start()
	if err != nil {
		if _, ok := err.(atspi.ErrMissing); ok {
			skip = err.Error()
			os.Exit(m.Run())
		}
		fmt.Fprintln(os.Stderr, "a11y bus:", err)
		os.Exit(1)
	}
	// The bridge reads these in gtk_init, through libc.
	libcUnsetenv("NO_AT_BRIDGE")
	libcSetenv("AT_SPI_BUS_ADDRESS", addr)
	libcSetenv("GTK_A11Y", "atspi")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	client, err = atspi.Dial(ctx, addr)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "a11y client:", err)
		stop()
		os.Exit(1)
	}
	registerPages(mux)
	app, err = linux.New(linux.Options{AppID: appID, Handler: mux, Debug: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "a11y app:", err)
		stop()
		os.Exit(1)
	}
	app.OnAllClosed(func() {})
	code := 0
	err = app.Run(func() {
		go func() {
			code = m.Run()
			app.Quit()
		}()
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	stop()
	os.Exit(code)
}

func need(t *testing.T) {
	t.Helper()
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	if skip != "" {
		t.Skip(skip)
	}
}

// waitFor polls fn for up to ten seconds.
func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s\n%s", what, treeDump())
}

// appNode is the accessibility node of this process.
func appNode() (atspi.Node, bool) {
	return client.AppByName(appID)
}

func treeDump() string {
	a, ok := appNode()
	if !ok {
		return "(application not on the accessibility bus)"
	}
	return a.Dump()
}

// topLevel returns the top-level node (frame, dialog, alert) with this name.
func topLevel(name string) (atspi.Node, bool) {
	a, ok := appNode()
	if !ok {
		return atspi.Node{}, false
	}
	for _, c := range a.Children() {
		if c.Name() == name {
			return c, true
		}
	}
	return atspi.Node{}, false
}

func waitTopLevel(t *testing.T, name string) atspi.Node {
	t.Helper()
	var n atspi.Node
	waitFor(t, "top-level "+name, func() bool {
		var ok bool
		n, ok = topLevel(name)
		return ok
	})
	return n
}

// documents lists the web documents under n.
func documents(n atspi.Node) []atspi.Node {
	return n.FindAll(func(c atspi.Node) bool { return c.Role() == atspi.RoleDocumentWeb })
}

// open opens a window and removes it (and the app menu) when the test ends.
func open(t *testing.T, o linux.WindowOptions) *linux.Window {
	t.Helper()
	if o.Width == 0 {
		o.Width, o.Height = 600, 400
	}
	o.Resizable = true
	if o.Path == "" {
		o.Path = "/"
	}
	w, err := app.NewWindow(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = app.SetAppMenu(nil)
		w.Close()
		if !o.Offscreen {
			waitGone(o.Title)
		}
	})
	return w
}

func waitGone(name string) {
	for i := 0; i < 50; i++ {
		if _, ok := topLevel(name); !ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// focusWindow raises the window and waits until it is the active one.
func focusWindow(t *testing.T, w *linux.Window, title string) atspi.Node {
	t.Helper()
	fr := waitTopLevel(t, title)
	w.Focus()
	waitFor(t, title+" to be active", func() bool {
		fr, _ = topLevel(title)
		return fr.HasState(atspi.StateActive)
	})
	return fr
}

func registerPages(mux *http.ServeMux) {
	mux.HandleFunc("/form", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Form</title><body><input aria-label="one"><input aria-label="two"><button>Go</button><script>addEventListener("keydown",function(e){document.title="key:"+e.key+" on "+(document.activeElement.getAttribute("aria-label")||document.activeElement.tagName)})</script></body>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Home page</title><body><h1>Hello</h1><button>Go</button></body>`)
	})
}
