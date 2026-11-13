//go:build linux

// Package guitest runs the UI half of the SDK against the real Linux
// platform (GTK3 + WebKitGTK through purego). It needs a display and the
// WebKitGTK libraries and runs only with KRATE_NATIVE_GUI=1; see
// ~/wt/kn-sysroot/README.md for a rootless setup. With KRATE_NATIVE_WM=1 and a
// window manager on the display (xvfb-start-wm.sh) the fullscreen, maximize
// and minimize checks use real window manager state.
package guitest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/platform/linux"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	sdkall "github.com/kratejs/krate/packages/native/sdk/all"
	"github.com/kratejs/krate/packages/native/sdk/clipboard"
	"github.com/kratejs/krate/packages/native/sdk/dialog"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/sdk/window"
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

func TestSecondWindowOnARoute(t *testing.T) {
	need(t)
	w, err := window.Open(window.Options{Title: "Settings", Path: "/settings", Width: 520, Height: 360})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	eventually(t, "window state", func() bool {
		st, err := w.State()
		return err == nil && st.Title == "Settings" && st.Width == 520 && st.Height == 360 && st.Visible
	})
	ids, _ := window.List()
	if len(ids) < 2 {
		t.Fatalf("windows %v", ids)
	}

	// Always on top and frameless reflect in the GTK state.
	if err := w.SetAlwaysOnTop(true); err != nil {
		t.Fatal(err)
	}
	if err := w.SetFrameless(true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "above+frameless", func() bool {
		st, _ := w.State()
		return st.AlwaysOnTop && !st.Decorated
	})
	if err := w.SetAlwaysOnTop(false); err != nil {
		t.Fatal(err)
	}
	if err := w.SetFrameless(false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "normal", func() bool {
		st, _ := w.State()
		return !st.AlwaysOnTop && st.Decorated
	})
	if err := w.SetTitle("Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetSize(444, 333); err != nil {
		t.Fatal(err)
	}
	eventually(t, "retitled", func() bool {
		st, _ := w.State()
		return st.Title == "Renamed" && st.Width == 444 && st.Height == 333
	})
	if os.Getenv("KRATE_NATIVE_WM") == "1" {
		if err := w.SetFullscreen(true); err != nil {
			t.Fatal(err)
		}
		eventually(t, "fullscreen", func() bool { st, _ := w.State(); return st.Fullscreen })
		if err := w.SetFullscreen(false); err != nil {
			t.Fatal(err)
		}
		eventually(t, "windowed", func() bool { st, _ := w.State(); return !st.Fullscreen })
		if err := w.Maximize(); err != nil {
			t.Fatal(err)
		}
		eventually(t, "maximized", func() bool { st, _ := w.State(); return st.Maximized })
		if err := w.Restore(); err != nil {
			t.Fatal(err)
		}
		eventually(t, "restored", func() bool { st, _ := w.State(); return !st.Maximized })
	}
	// Saved state round trip against the real window.
	if err := w.SetSize(480, 320); err != nil {
		t.Fatal(err)
	}
	eventually(t, "480x320", func() bool { st, _ := w.State(); return st.Width == 480 })
	if err := window.SaveState(w, "settings"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetSize(300, 300); err != nil {
		t.Fatal(err)
	}
	eventually(t, "300x300", func() bool { st, _ := w.State(); return st.Width == 300 })
	if ok, err := window.RestoreState(w, "settings"); err != nil || !ok {
		t.Fatalf("restore %v %v", ok, err)
	}
	eventually(t, "restored size", func() bool { st, _ := w.State(); return st.Width == 480 && st.Height == 320 })

	// Close requested: prevented, delivered to Go, then closed for real.
	closeReq := make(chan struct{}, 1)
	cancel, err := w.OnCloseRequested(func() { closeReq <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	w.SetPreventClose(true)
	time.Sleep(100 * time.Millisecond)
	linuxRequestClose(t, w.ID)
	select {
	case <-closeReq:
	case <-time.After(5 * time.Second):
		t.Fatal("close-requested was not delivered")
	}
	if st, err := w.State(); err != nil || !st.Visible {
		t.Fatalf("window went away despite prevent: %+v %v", st, err)
	}
	w.Close()
	eventually(t, "gone", func() bool {
		ids, _ := window.List()
		for _, id := range ids {
			if id == w.ID {
				return false
			}
		}
		return true
	})
}

// linuxRequestClose sends the window the delete-event a window manager would.
func linuxRequestClose(t *testing.T, id string) {
	t.Helper()
	if !linux.RequestClose(plat, host.WindowRef(id)) {
		t.Fatal("cannot request close")
	}
}

// The X CLIPBOARD selection belongs to the display, not to the process. Run
// alongside another GUI test process on the same display (go test without
// -p 1, or two packages at once) another clipboard test can take the selection
// between the write and the read here, so this test needs the display to
// itself. CI and the documented commands use -p 1.
func TestClipboardThroughTheSDK(t *testing.T) {
	need(t)
	if err := clipboard.WriteText("sdk text é"); err != nil {
		t.Fatal(err)
	}
	if s, err := clipboard.ReadText(); err != nil || s != "sdk text é" {
		t.Fatalf("text %q %v", s, err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(2, 1, color.RGBA{10, 20, 30, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := clipboard.WriteImage(b.Bytes()); err != nil {
		t.Fatal(err)
	}
	back, err := clipboard.ReadImage()
	if err != nil {
		t.Fatal(err)
	}
	dec, err := png.Decode(bytes.NewReader(back))
	if err != nil || dec.Bounds() != img.Bounds() {
		t.Fatalf("image %v %v", dec, err)
	}
	if r, g, bl, _ := dec.At(2, 1).RGBA(); r>>8 != 10 || g>>8 != 20 || bl>>8 != 30 {
		t.Fatalf("pixel %d %d %d", r>>8, g>>8, bl>>8)
	}
	files := []string{"/tmp/a b.txt", "/tmp/c.png"}
	if err := clipboard.WriteFiles(files); err != nil {
		t.Fatal(err)
	}
	if got, err := clipboard.ReadFiles(); err != nil || !reflect.DeepEqual(got, files) {
		t.Fatalf("files %v %v", got, err)
	}
	if err := clipboard.Clear(); err != nil {
		t.Fatal(err)
	}
}

func TestMenuClickReachesGoAndTheUI(t *testing.T) {
	need(t)
	goGot := make(chan menu.Event, 4)
	cancel, err := menu.OnItem("file.new", func(e menu.Event) { goGot <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	// The page side: open the stream the way @krate/app does.
	srv := httptest.NewServer(ui)
	defer srv.Close()
	stream := openStream(t, srv.URL, "krate/menu.events")
	defer stream.Body.Close()
	eventually(t, "subscribers", func() bool { return host.Subscribers("menu") >= 2 })

	if err := menu.SetApp(menu.New(menu.Sub("&File", menu.Action("file.new", "New", "CmdOrCtrl+N"), menu.Check("file.auto", "Autosave", true)))); err != nil {
		t.Fatal(err)
	}
	linuxActivate(t, "file.new")
	select {
	case e := <-goGot:
		if e.ID != "file.new" || e.Window != "main" {
			t.Fatalf("go event %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the click never reached Go")
	}
	fd, err := wire.ReadFrame(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := fd.RawValue()
	if string(raw) != `{"v":{"id":"file.new","window":"main","checked":false}}` {
		t.Fatalf("page event %s", raw)
	}
	if err := menu.SetApp(nil); err != nil {
		t.Fatal(err)
	}
}

func linuxActivate(t *testing.T, id string) {
	t.Helper()
	if !linux.ActivateMenuItem(plat, id) {
		t.Fatalf("menu item %q not found", id)
	}
}

// openStream calls a stream id over the page protocol and returns the open
// stream response.
func openStream(t *testing.T, base, id string) *http.Response {
	t.Helper()
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String(id)
	e.Key("args")
	e.BeginArray()
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	resp, err := http.Post(base+"/__krate/call", "application/x-krate-frames", bytes.NewReader(e.Frame()))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := wire.ReadFrame(resp.Body)
	raw, _ := d.RawValue()
	resp.Body.Close()
	var res struct {
		Stream string `json:"stream"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res.Stream == "" {
		t.Fatalf("no stream: %s", raw)
	}
	sresp, err := http.Get(base + "/__krate/stream/" + res.Stream)
	if err != nil {
		t.Fatal(err)
	}
	return sresp
}

func TestDialogsAreCreatedWithTheRightOptions(t *testing.T) {
	need(t)
	type seen struct {
		title    string
		action   int
		multiple bool
		filters  []string
	}
	got := make(chan seen, 1)
	linux.DialogHook = func(p *linux.DialogProbe) bool {
		got <- seen{p.Title(), p.Action(), p.Multiple(), p.FilterNames()}
		p.Cancel()
		return true
	}
	defer func() { linux.DialogHook = nil }()
	paths, err := dialog.OpenFile(krate.Background(), dialog.OpenOptions{
		Title: "Open images", Multiple: true,
		Filters: []dialog.Filter{{Name: "PNG", Patterns: []string{"*.png"}}, {Name: "All", Patterns: []string{"*"}}},
	})
	if err != nil || len(paths) != 0 {
		t.Fatalf("cancel: %v %v", paths, err)
	}
	s := <-got
	if s.title != "Open images" || s.action != 0 || !s.multiple || !reflect.DeepEqual(s.filters, []string{"PNG", "All"}) {
		t.Fatalf("dialog %+v", s)
	}
	p, err := dialog.SaveFile(krate.Background(), dialog.SaveOptions{Title: "Save as", Name: "x.txt"})
	if err != nil || p != "" {
		t.Fatalf("save cancel: %q %v", p, err)
	}
	if s = <-got; s.action != 1 || s.title != "Save as" {
		t.Fatalf("save dialog %+v", s)
	}
	if _, err := dialog.OpenFolder(krate.Background(), dialog.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	if s = <-got; s.action != 2 {
		t.Fatalf("folder dialog %+v", s)
	}
	linux.DialogHook = func(p *linux.DialogProbe) bool { p.Press(1); return true }
	n, err := dialog.Message(krate.Background(), dialog.MessageOptions{Message: "Delete?", Buttons: []string{"Cancel", "Delete"}})
	if err != nil || n != 1 {
		t.Fatalf("message %d %v", n, err)
	}
}

func TestCapabilitiesAreReported(t *testing.T) {
	need(t)
	for _, c := range []string{"dialog", "clipboard", "menu", "window", "window:fullscreen", "window:frameless"} {
		if !krate.Supports(c) {
			t.Errorf("%s should be supported on Linux", c)
		}
	}
	if krate.Supports("teleport") {
		t.Error("unknown capability supported")
	}
}

var _ = context.Background
var _ caps.WindowRef
